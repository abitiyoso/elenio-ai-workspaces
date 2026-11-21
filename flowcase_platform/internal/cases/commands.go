package cases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/flowcase/internal/audit"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

// CodeHumanOnly is raised by the database when a non-human principal records a decision.
const CodeHumanOnly = "FC003"

func (e *Engine) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	err := pgx.BeginFunc(ctx, e.DB, fn)
	switch {
	case postgres.HasCode(err, CodeHumanOnly):
		return &DeniedError{Reason: "service_principal", Message: "Only human principals can decide approvals."}
	case postgres.HasCode(err, postgres.CodeImmutable):
		return &ConflictError{Code: "immutable", Message: "This record can no longer change."}
	}
	return err
}

// hasDomainRole: tenant-wide or case-scoped assignment of a pack role such as poc-owner.
func hasDomainRole(ctx context.Context, q querier, a auth.Identity, role, caseID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM domain_role_assignments WHERE tenant_id = $1 AND user_id = $2 AND role = $3
		AND revoked_at IS NULL AND (case_id IS NULL OR case_id = $4))`, a.TenantID, a.UserID, role, caseID).Scan(&ok)
	return ok, err
}

func isOwnerOrAdmin(a auth.Identity, c *caseRow) bool {
	return a.UserID == c.OwnerID || a.HasRole(auth.RoleTenantAdmin)
}

type taskRow struct {
	ID, CaseID, ExecID, NodeID, AssigneeRole, Status string
	VersionNum                                       int
	FormSchema                                       []byte
	CompletedBy                                      *string
}

func lockTask(ctx context.Context, tx pgx.Tx, a auth.Identity, taskID string) (*caseRow, *taskRow, error) {
	var caseID string
	err := tx.QueryRow(ctx, `SELECT case_id::text FROM tasks WHERE tenant_id = $1 AND id = $2`, a.TenantID, taskID).Scan(&caseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	c, err := lockCase(ctx, tx, a.TenantID, caseID)
	if err != nil {
		return nil, nil, err
	}
	var t taskRow
	err = tx.QueryRow(ctx, `SELECT id::text, case_id::text, node_execution_id::text, node_id, assignee_role, status, version_num,
			form_schema, completed_by::text
		FROM tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&t.ID, &t.CaseID, &t.ExecID, &t.NodeID, &t.AssigneeRole, &t.Status, &t.VersionNum, &t.FormSchema, &t.CompletedBy)
	return c, &t, err
}

// CompleteTask submits a human task. Only holders of the task's domain role may complete it; the
// output is validated against the node's form_schema and its declared fields become case variables.
func (e *Engine) CompleteTask(ctx context.Context, a auth.Identity, taskID string, ifMatch int, in CompleteTaskInput) error {
	if !a.IsHuman() {
		return &DeniedError{Reason: "service_principal", Message: "Human tasks are completed by people."}
	}
	if len(in.Comment) > 4000 {
		return &InputError{Code: "invalid_comment", Message: "comment is limited to 4000 characters", Path: "comment"}
	}
	if in.Output == nil {
		in.Output = map[string]any{}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		c, t, err := lockTask(ctx, tx, a, taskID)
		if err != nil {
			return err
		}
		ok, err := hasDomainRole(ctx, tx, a, t.AssigneeRole, c.ID)
		if err != nil {
			return err
		}
		if !ok {
			return &DeniedError{Reason: "not_assignee_role", Message: fmt.Sprintf("This task is assigned to the %s role.", t.AssigneeRole)}
		}
		if !c.running() {
			return &ConflictError{Code: "case_not_active", Message: fmt.Sprintf("The case is %s.", c.Status)}
		}
		if t.Status != "assigned" {
			return &ConflictError{Code: "task_not_open", Message: "This task is no longer open."}
		}
		if t.VersionNum != ifMatch {
			return ErrPreconditionFailed
		}
		vars := decodeVars(c.Variables)
		merged := false
		if len(t.FormSchema) > 0 {
			schema := decodeVars(t.FormSchema)
			if diags := definition.ValidateData(schema, in.Output, "/output"); len(diags) > 0 {
				return &ValidationError{Diagnostics: diags}
			}
			props, _ := schema["properties"].(map[string]any)
			for k := range props {
				if v, ok := in.Output[k]; ok {
					vars[k], merged = v, true
				}
			}
		}
		out, _ := json.Marshal(in.Output)
		if len(out) > 64<<10 {
			return &InputError{Code: "output_too_large", Message: "task output is limited to 64 KiB", Path: "output"}
		}
		if _, err := tx.Exec(ctx, `UPDATE tasks SET status = 'submitted', output = $2, completed_by = $3, completed_at = now(), updated_at = now(),
			version_num = version_num + 1 WHERE id = $1`, t.ID, out, a.UserID); err != nil {
			return err
		}
		if in.Comment != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO task_comments (tenant_id, task_id, author_id, body) VALUES ($1, $2, $3, $4)`,
				a.TenantID, t.ID, a.UserID, in.Comment); err != nil {
				return err
			}
		}
		if merged {
			raw, _ := json.Marshal(vars)
			if _, err := tx.Exec(ctx, `UPDATE cases SET variables = $2, updated_at = now(), version_num = version_num + 1 WHERE id = $1`, c.ID, raw); err != nil {
				return err
			}
			c.Variables = raw
		}
		x, err := lockExec(ctx, tx, t.ExecID)
		if err != nil {
			return err
		}
		if x.Status != "waiting_human" {
			return &ConflictError{Code: "task_not_open", Message: "This step is no longer waiting for input."}
		}
		p, err := e.pinnedVersion(ctx, tx, c.VersionID)
		if err != nil {
			return err
		}
		node := p.graph.Node(x.NodeID)
		if _, err := e.produceArtifact(ctx, tx, c, p, produceInput{NodeID: node.ID, Type: node.ConfigString("output_artifact_type"), Kind: "human",
			ExecID: x.ID, UserID: a.UserID, Content: out}); err != nil {
			return err
		}
		if err := completeExec(ctx, tx, x.ID, map[string]any{"task_id": t.ID}); err != nil {
			return err
		}
		if err := e.schedule(ctx, tx, c, x, node.Next); err != nil {
			return err
		}
		data := map[string]any{"node_id": t.NodeID, "variables_updated": merged}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, c, "task.submitted", "task", t.ID, data)); err != nil {
			return err
		}
		return outbox(ctx, tx, c, "task.submitted", "task", t.ID, data)
	})
}

// successorWaiting returns the live execution scheduled after pred, if it is waiting on a person.
func successorWaiting(ctx context.Context, q querier, predExecID string) (string, bool, error) {
	var id, status string
	err := q.QueryRow(ctx, `SELECT id::text, status FROM node_executions WHERE predecessor_id = $1 AND status NOT IN ('superseded', 'cancelled')
		ORDER BY created_at DESC LIMIT 1`, predExecID).Scan(&id, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, status == "waiting_human", err
}

// RequestRevision sends a submitted task back for rework while its successor still waits on a
// person (a review or approval). The waiting step is superseded and the task re-opens as a new iteration.
func (e *Engine) RequestRevision(ctx context.Context, a auth.Identity, taskID string, ifMatch int, rationale string) error {
	if l := len([]rune(rationale)); l < 5 || l > 4000 {
		return &InputError{Code: "rationale_required", Message: "rationale must have 5 to 4000 characters", Path: "rationale"}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		c, t, err := lockTask(ctx, tx, a, taskID)
		if err != nil {
			return err
		}
		if !isOwnerOrAdmin(a, c) || !a.IsHuman() {
			return &DeniedError{Reason: "not_case_owner", Message: "Only the case owner can request a revision."}
		}
		if !c.running() {
			return &ConflictError{Code: "case_not_active", Message: fmt.Sprintf("The case is %s.", c.Status)}
		}
		if t.Status != "submitted" {
			return &ConflictError{Code: "task_not_submitted", Message: "Only submitted tasks can be sent back."}
		}
		if t.VersionNum != ifMatch {
			return ErrPreconditionFailed
		}
		succID, waiting, err := successorWaiting(ctx, tx, t.ExecID)
		if err != nil {
			return err
		}
		if !waiting {
			return &ConflictError{Code: "revision_not_possible", Message: "The case has already moved past the next step; use an approval return instead."}
		}
		succ, err := lockExec(ctx, tx, succID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE tasks SET status = 'revision_requested', revision_note = $2, updated_at = now(),
			version_num = version_num + 1 WHERE id = $1`, t.ID, rationale); err != nil {
			return err
		}
		if err := e.rewind(ctx, tx, c, succ, t.NodeID, rationale, a.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(ctx, a, c, "task.revision_requested", "task", t.ID, map[string]any{"node_id": t.NodeID, "rationale": rationale}))
	})
}

// AddComment appends a comment to a task the caller can see.
func (e *Engine) AddComment(ctx context.Context, a auth.Identity, taskID, body string) error {
	if l := len([]rune(body)); l < 1 || l > 4000 {
		return &InputError{Code: "invalid_comment", Message: "comment must have 1 to 4000 characters", Path: "body"}
	}
	if _, err := e.GetTask(ctx, a, taskID); err != nil {
		return err
	}
	_, err := e.DB.Exec(ctx, `INSERT INTO task_comments (tenant_id, task_id, author_id, body) VALUES ($1, $2, $3, $4)`, a.TenantID, taskID, a.UserID, body)
	return err
}

// Authority is recorded with every decision.
type Authority struct {
	Role         string `json:"role"`
	PolicyRef    string `json:"policy_ref"`
	DelegationID string `json:"delegation_id"`
	// SelfApprovalExempt is set when a segregation-of-duties rule would have denied the decision
	// and the identity's development exemption (SELF_APPROVAL_EXEMPT_SUBJECTS) allowed it.
	SelfApprovalExempt bool `json:"self_approval_exempt,omitempty"`
}

type approvalRow struct {
	ID, CaseID, ExecID, NodeID, PolicyRef, SubjectHash, Status, Kind string
	ReturnTo                                                         *string
	Iteration, VersionNum                                            int
}

// authority applies the approval rules: human principal, domain_approver role, an active delegation
// for the gate's policy, and default segregation of duties (no approving your own case or work).
// sodRules returns the segregation-of-duties rules configured for a policy (all rules by default).
func (e *Engine) sodRules(tenant, policyRef string) []string {
	if cat := catalog.ForTenant(e.Catalog, tenant); cat != nil {
		if p, ok := cat.Policy(policyRef); ok && p.SoD != nil {
			return p.SoD
		}
	}
	return catalog.SoDRules
}

func authority(ctx context.Context, q querier, a auth.Identity, c *caseRow, policyRef string, sod []string) (Authority, *DeniedError, error) {
	deny := func(reason, msg string) (Authority, *DeniedError, error) {
		return Authority{}, &DeniedError{Reason: reason, Message: msg}, nil
	}
	if !a.IsHuman() {
		return deny("service_principal", "AI agents and service identities never hold approval authority.")
	}
	if !a.HasRole(auth.RoleDomainApprover) {
		return deny("missing_role", "The domain_approver role is required.")
	}
	var delegation string
	err := q.QueryRow(ctx, `SELECT id::text FROM policy_delegations WHERE tenant_id = $1 AND user_id = $2 AND policy_ref = $3
		AND revoked_at IS NULL AND valid_from <= now() AND (valid_until IS NULL OR valid_until > now()) LIMIT 1`, a.TenantID, a.UserID, policyRef).Scan(&delegation)
	if errors.Is(err, pgx.ErrNoRows) {
		return deny("no_delegation", fmt.Sprintf("You hold no delegation for policy %s.", policyRef))
	}
	if err != nil {
		return Authority{}, nil, err
	}
	granted := Authority{Role: string(auth.RoleDomainApprover), PolicyRef: policyRef, DelegationID: delegation}
	if slices.Contains(sod, catalog.SoDNotCaseOwner) && (a.UserID == c.CreatedBy || a.UserID == c.OwnerID) {
		if !a.SoDExempt {
			return deny("sod_case_owner", "Segregation of duties: you cannot approve a case you own or created.")
		}
		granted.SelfApprovalExempt = true
	}
	if !slices.Contains(sod, catalog.SoDNotTaskSubmitter) {
		return granted, nil, nil
	}
	var submitted bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tasks WHERE case_id = $1 AND completed_by = $2)`, c.ID, a.UserID).Scan(&submitted); err != nil {
		return Authority{}, nil, err
	}
	if submitted {
		if !a.SoDExempt {
			return deny("sod_task_submitter", "Segregation of duties: you submitted work in this case.")
		}
		granted.SelfApprovalExempt = true
	}
	return granted, nil, nil
}

// Decide records an immutable, human-only decision bound to the approval's subject hash.
func (e *Engine) Decide(ctx context.Context, a auth.Identity, approvalID string, ifMatch int, in DecisionInput) error {
	if in.Decision != "approve" && in.Decision != "reject" && in.Decision != "return" {
		return &InputError{Code: "invalid_decision", Message: "decision must be approve, reject or return", Path: "decision"}
	}
	if l := len([]rune(in.Rationale)); l < 5 || l > 4000 {
		return &InputError{Code: "rationale_required", Message: "a rationale of 5 to 4000 characters is required", Path: "rationale"}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		var caseID string
		err := tx.QueryRow(ctx, `SELECT case_id::text FROM approval_requests WHERE tenant_id = $1 AND id = $2`, a.TenantID, approvalID).Scan(&caseID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		c, err := lockCase(ctx, tx, a.TenantID, caseID)
		if err != nil {
			return err
		}
		var ap approvalRow
		if err := tx.QueryRow(ctx, `SELECT id::text, case_id::text, node_execution_id::text, node_id, policy_ref, subject_hash, status, kind, return_to,
				iteration, version_num FROM approval_requests WHERE id = $1 FOR UPDATE`, approvalID).Scan(
			&ap.ID, &ap.CaseID, &ap.ExecID, &ap.NodeID, &ap.PolicyRef, &ap.SubjectHash, &ap.Status, &ap.Kind, &ap.ReturnTo, &ap.Iteration, &ap.VersionNum); err != nil {
			return err
		}
		authz, denied, err := authority(ctx, tx, a, c, ap.PolicyRef, e.sodRules(c.TenantID, ap.PolicyRef))
		if err != nil {
			return err
		}
		if denied != nil {
			return denied
		}
		if !c.running() {
			return &ConflictError{Code: "case_not_active", Message: fmt.Sprintf("The case is %s.", c.Status)}
		}
		if ap.Status != "pending" {
			return &ConflictError{Code: "approval_closed", Message: "This approval has already been decided or withdrawn."}
		}
		if ap.VersionNum != ifMatch {
			return ErrPreconditionFailed
		}
		if in.Decision == "return" && ap.ReturnTo == nil {
			return &ConflictError{Code: "return_not_allowed", Message: "This gate has no return_to step; approve or reject."}
		}
		p, err := e.pinnedVersion(ctx, tx, c.VersionID)
		if err != nil {
			return err
		}
		node := p.graph.Node(ap.NodeID)
		_, current, cov, err := e.approvalSubject(ctx, tx, c, p, ap.NodeID, ap.Iteration)
		if err != nil {
			return err
		}
		if current != ap.SubjectHash {
			return &ConflictError{Code: "stale_approval", Message: "The case changed after this approval was requested; it must be re-reviewed."}
		}
		var covered []string
		if in.Decision == "approve" {
			var stale []string
			for _, a := range cov {
				covered = append(covered, a.VersionID)
				if a.Status == "stale" {
					stale = append(stale, a.Type)
				}
			}
			if len(stale) > 0 {
				return &ConflictError{Code: "stale_artifacts", Message: fmt.Sprintf("Covered artifacts are stale and must be revised first: %v", stale)}
			}
			var rereviews int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM approval_requests WHERE case_id = $1 AND kind = 'rereview' AND status = 'pending' AND id <> $2`,
				c.ID, ap.ID).Scan(&rereviews); err != nil {
				return err
			}
			if ap.Kind == "gate" && rereviews > 0 {
				return &ConflictError{Code: "rereview_pending", Message: fmt.Sprintf("%d affected review(s) must be re-approved before this gate.", rereviews)}
			}
		}
		decidedAt := time.Now().UTC()
		record := map[string]any{"approval_id": ap.ID, "decision": in.Decision, "rationale": in.Rationale, "decided_by": a.UserID,
			"subject_hash": ap.SubjectHash, "decided_at": decidedAt.Format(time.RFC3339Nano)}
		authJSON, _ := json.Marshal(authz)
		decisionHash := sha(record)
		if _, err := tx.Exec(ctx, `INSERT INTO approval_decisions (tenant_id, approval_id, decided_by, decision, rationale, subject_hash, authority,
				decision_hash, decided_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			a.TenantID, ap.ID, a.UserID, in.Decision, in.Rationale, ap.SubjectHash, authJSON, decisionHash, decidedAt); err != nil {
			return err
		}
		status := map[string]string{"approve": "approved", "reject": "rejected", "return": "returned"}[in.Decision]
		if _, err := tx.Exec(ctx, `UPDATE approval_requests SET status = $2, decided_at = $3, updated_at = now(), version_num = version_num + 1
			WHERE id = $1`, ap.ID, status, decidedAt); err != nil {
			return err
		}
		x, err := lockExec(ctx, tx, ap.ExecID)
		if err != nil {
			return err
		}
		switch in.Decision {
		case "approve":
			if err := completeExec(ctx, tx, x.ID, map[string]any{"decision": "approve", "approval_id": ap.ID}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE artifact_versions SET status = 'approved' WHERE id::text = ANY($1) AND status = 'ready_for_review'`, covered); err != nil {
				return err
			}
			if err := refreshReviewStatus(ctx, tx, c.ID); err != nil {
				return err
			}
			if ap.Kind == "gate" { // a re-review restores confidence in the gate; the flow already moved on
				if err := e.schedule(ctx, tx, c, x, node.Next); err != nil {
					return err
				}
			}
		case "reject":
			if err := completeExec(ctx, tx, x.ID, map[string]any{"decision": "reject", "approval_id": ap.ID}); err != nil {
				return err
			}
			if err := closeCase(ctx, tx, c, CaseCancelled, "rejected_at_"+ap.NodeID); err != nil {
				return err
			}
		case "return":
			if err := e.rewind(ctx, tx, c, x, *ap.ReturnTo, in.Rationale, a.UserID); err != nil {
				return err
			}
		}
		data := map[string]any{"decision": in.Decision, "policy_ref": ap.PolicyRef, "subject_hash": ap.SubjectHash, "decision_hash": decisionHash,
			"node_id": ap.NodeID, "iteration": ap.Iteration}
		if authz.SelfApprovalExempt {
			data["self_approval_exempt"] = true
		}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, c, "approval.decided", "approval", ap.ID, data)); err != nil {
			return err
		}
		return outbox(ctx, tx, c, "approval.decided", "approval", ap.ID, data)
	})
}

// SetPaused pauses or resumes a case (owner or tenant admin). Paused cases accept no task or
// approval actions, and the worker does not advance them.
func (e *Engine) SetPaused(ctx context.Context, a auth.Identity, caseID string, ifMatch int, pause bool, reason string) error {
	if len([]rune(reason)) > 2000 {
		return &InputError{Code: "invalid_reason", Message: "reason is limited to 2000 characters", Path: "reason"}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		c, err := lockCase(ctx, tx, a.TenantID, caseID)
		if err != nil {
			return err
		}
		if !isOwnerOrAdmin(a, c) {
			return &DeniedError{Reason: "not_case_owner", Message: "Only the case owner or a tenant admin can pause or resume."}
		}
		if c.VersionNum != ifMatch {
			return ErrPreconditionFailed
		}
		action := "case.paused"
		if pause {
			if !c.running() {
				return &ConflictError{Code: "invalid_state", Message: fmt.Sprintf("A %s case cannot be paused.", c.Status)}
			}
			_, err = tx.Exec(ctx, `UPDATE cases SET status = 'paused', updated_at = now(), version_num = version_num + 1 WHERE id = $1`, c.ID)
		} else {
			if c.Status != CasePaused {
				return &ConflictError{Code: "invalid_state", Message: "Only paused cases can be resumed."}
			}
			action = "case.resumed"
			if _, err = tx.Exec(ctx, `UPDATE cases SET status = 'active', updated_at = now(), version_num = version_num + 1 WHERE id = $1`, c.ID); err == nil {
				if err = refreshReviewStatus(ctx, tx, c.ID); err == nil {
					err = notify(ctx, tx)
				}
			}
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(ctx, a, c, action, "case", c.ID, map[string]any{"reason": reason}))
	})
}

// AddNote records a structured business event on the case (the only event type in this phase).
func (e *Engine) AddNote(ctx context.Context, a auth.Identity, caseID, body string) error {
	if l := len([]rune(body)); l < 1 || l > 4000 {
		return &InputError{Code: "invalid_note", Message: "note must have 1 to 4000 characters", Path: "body"}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		visible, err := e.canSeeCase(ctx, tx, a, caseID)
		if err != nil {
			return err
		}
		if !visible {
			return ErrNotFound
		}
		c := &caseRow{ID: caseID, TenantID: a.TenantID}
		return audit.Record(ctx, tx, userAudit(ctx, a, c, "case.note", "case", caseID, map[string]any{"note": body}))
	})
}
