package cases

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/flowcase/internal/platform/auth"
)

// Case visibility: members, owner/creator, tenant admins, and people with work in the case (holders
// of an open task's domain role, or delegates of a pending approval's policy).
// Parameters: $1 tenant, $2 user, $3 is tenant admin.
const visibleSQL = `(c.tenant_id = $1 AND ($3 OR c.created_by = $2 OR c.owner_id = $2
	OR EXISTS (SELECT 1 FROM case_members m WHERE m.case_id = c.id AND m.user_id = $2)
	OR EXISTS (SELECT 1 FROM tasks t JOIN domain_role_assignments d ON d.tenant_id = t.tenant_id AND d.role = t.assignee_role
		AND d.user_id = $2 AND d.revoked_at IS NULL AND (d.case_id IS NULL OR d.case_id = t.case_id) WHERE t.case_id = c.id)
	OR EXISTS (SELECT 1 FROM approval_requests ar JOIN policy_delegations pd ON pd.tenant_id = ar.tenant_id AND pd.policy_ref = ar.policy_ref
		AND pd.user_id = $2 AND pd.revoked_at IS NULL WHERE ar.case_id = c.id)))`

func (e *Engine) canSeeCase(ctx context.Context, q querier, a auth.Identity, caseID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cases c WHERE c.id = $4 AND `+visibleSQL+`)`,
		a.TenantID, a.UserID, a.HasRole(auth.RoleTenantAdmin), caseID).Scan(&ok)
	return ok, err
}

const summaryCols = `c.id::text, c.reference, c.title, c.status, c.business_facets->>'outcome', c.workflow_key, d.name, c.workflow_version,
	c.definition_hash, u.display_name,
	coalesce((SELECT array_agg(DISTINCT n.node_id) FROM node_executions n WHERE n.case_id = c.id AND n.status IN ('ready', 'running', 'waiting_human', 'waiting_external')), '{}'),
	(SELECT count(*) FROM tasks t WHERE t.case_id = c.id AND t.status = 'assigned')::int,
	(SELECT count(*) FROM approval_requests ar WHERE ar.case_id = c.id AND ar.status = 'pending')::int,
	(SELECT count(DISTINCT n.node_id) FROM node_executions n WHERE n.case_id = c.id AND n.status = 'completed')::int,
	jsonb_array_length(v.definition->'nodes'), c.created_at, c.updated_at, c.version_num`

const summaryFrom = ` FROM cases c JOIN workflow_versions v ON v.id = c.workflow_version_id
	JOIN workflow_definitions d ON d.id = v.definition_id JOIN users u ON u.id = c.owner_id`

func scanSummary(row pgx.Row) (CaseSummary, error) {
	var s CaseSummary
	var done, total int
	err := row.Scan(&s.ID, &s.Reference, &s.Title, &s.Status, &s.Outcome, &s.WorkflowKey, &s.WorkflowName, &s.WorkflowVersion, &s.DefinitionHash,
		&s.OwnerName, &s.CurrentSteps, &s.OpenTasks, &s.PendingApprovals, &done, &total, &s.CreatedAt, &s.UpdatedAt, &s.VersionNum)
	if total > 0 {
		s.Progress = done * 100 / total
	}
	if s.Status == CaseCompleted {
		s.Progress = 100
	}
	return s, err
}

type ListFilter struct {
	Status string // all | active | under_review | paused | completed | closed
	Query  string
	Cursor string
	Limit  int
}

func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(c string) (time.Time, string, error) {
	if c == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", err
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, "", errors.New("bad cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	return t, id, err
}

func (e *Engine) ListCases(ctx context.Context, a auth.Identity, f ListFilter) ([]CaseSummary, string, error) {
	if len(a.Roles) == 0 {
		return nil, "", ErrForbidden
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	statuses := map[string][]string{
		"": nil, "all": nil, "active": {CaseActive}, "under_review": {CaseUnderReview}, "paused": {CasePaused},
		"completed": {CaseCompleted}, "closed": {CaseCompleted, CaseCancelled, CaseFailed},
	}
	want, ok := statuses[f.Status]
	if !ok {
		return nil, "", &InputError{Code: "invalid_filter", Message: "unknown status filter", Path: "status"}
	}
	after, afterID, err := decodeCursor(f.Cursor)
	if err != nil {
		return nil, "", &InputError{Code: "invalid_cursor", Message: "cursor is not valid", Path: "cursor"}
	}
	rows, err := e.DB.Query(ctx, `SELECT `+summaryCols+summaryFrom+` WHERE `+visibleSQL+`
		AND (coalesce(cardinality($4::text[]), 0) = 0 OR c.status = ANY($4)) -- a nil filter arrives as NULL
		AND ($5 = '' OR strpos(lower(c.title), lower($5)) > 0 OR strpos(lower(c.reference), lower($5)) > 0)
		AND ($6::timestamptz IS NULL OR (c.created_at, c.id) < ($6, $7::uuid))
		ORDER BY c.created_at DESC, c.id DESC LIMIT $8`,
		a.TenantID, a.UserID, a.HasRole(auth.RoleTenantAdmin), want, f.Query, nullTime(after), nullUUID(afterID), f.Limit+1)
	if err != nil {
		return nil, "", err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CaseSummary, error) { return scanSummary(r) })
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		last := out[len(out)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	if out == nil {
		out = []CaseSummary{}
	}
	return out, next, nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func nullUUID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (e *Engine) GetCase(ctx context.Context, a auth.Identity, caseID string) (CaseDetail, error) {
	visible, err := e.canSeeCase(ctx, e.DB, a, caseID)
	if err != nil || !visible {
		if err == nil {
			err = ErrNotFound
		}
		return CaseDetail{}, err
	}
	var d CaseDetail
	var versionID, ownerID string
	row := e.DB.QueryRow(ctx, `SELECT `+summaryCols+`, c.variables, c.business_facets, c.workflow_version_id::text, c.owner_id::text`+summaryFrom+` WHERE c.id = $1`, caseID)
	var done, total int
	if err := row.Scan(&d.ID, &d.Reference, &d.Title, &d.Status, &d.Outcome, &d.WorkflowKey, &d.WorkflowName, &d.WorkflowVersion, &d.DefinitionHash,
		&d.OwnerName, &d.CurrentSteps, &d.OpenTasks, &d.PendingApprovals, &done, &total, &d.CreatedAt, &d.UpdatedAt, &d.VersionNum,
		&d.Variables, &d.Facets, &versionID, &ownerID); err != nil {
		return CaseDetail{}, err
	}
	if total > 0 {
		d.Progress = done * 100 / total
	}
	if d.Status == CaseCompleted {
		d.Progress = 100
	}
	p, err := e.pinnedVersion(ctx, e.DB, versionID)
	if err != nil {
		return CaseDetail{}, err
	}
	names := map[string]string{}
	for _, n := range p.def.Nodes {
		names[n.ID] = n.Name
		d.Nodes = append(d.Nodes, GraphNode{ID: n.ID, Name: n.Name, Type: string(n.Type), Next: n.Next})
	}
	rows, err := e.DB.Query(ctx, `SELECT id::text, node_id, node_type, iteration, status, attempt, last_error, output, created_at, completed_at
		FROM node_executions WHERE case_id = $1 ORDER BY created_at, id`, caseID)
	if err != nil {
		return CaseDetail{}, err
	}
	d.Timeline, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Execution, error) {
		var x Execution
		err := r.Scan(&x.ID, &x.NodeID, &x.NodeType, &x.Iteration, &x.Status, &x.Attempt, &x.LastError, &x.Output, &x.CreatedAt, &x.CompletedAt)
		x.NodeName = names[x.NodeID]
		return x, err
	})
	if err != nil {
		return CaseDetail{}, err
	}
	if d.Tasks, err = e.tasks(ctx, a, `t.case_id = $3`, caseID); err != nil {
		return CaseDetail{}, err
	}
	if d.Approvals, err = e.approvals(ctx, a, `ar.case_id = $3`, caseID); err != nil {
		return CaseDetail{}, err
	}
	if d.Drafts, err = e.drafts(ctx, a, `dr.case_id = $3`, caseID, false); err != nil {
		return CaseDetail{}, err
	}
	rows, err = e.DB.Query(ctx, `SELECT node_id, expression, inputs, result, target, evaluated_at FROM condition_evaluations WHERE case_id = $1 ORDER BY evaluated_at`, caseID)
	if err != nil {
		return CaseDetail{}, err
	}
	d.Conditions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConditionEvaluation, error) {
		var c ConditionEvaluation
		return c, r.Scan(&c.NodeID, &c.Expression, &c.Inputs, &c.Result, &c.Target, &c.EvaluatedAt)
	})
	if err != nil {
		return CaseDetail{}, err
	}
	rows, err = e.DB.Query(ctx, `SELECT a.action, u.display_name, a.actor_type, a.occurred_at, a.data FROM audit_events a
		LEFT JOIN users u ON u.id = a.actor_id WHERE a.tenant_id = $1 AND a.case_id = $2 ORDER BY a.seq DESC LIMIT 200`, a.TenantID, caseID)
	if err != nil {
		return CaseDetail{}, err
	}
	d.History, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (HistoryEntry, error) {
		var h HistoryEntry
		return h, r.Scan(&h.Action, &h.ActorName, &h.ActorType, &h.OccurredAt, &h.Data)
	})
	if err != nil {
		return CaseDetail{}, err
	}
	if d.Artifacts, err = e.artifacts(ctx, caseID); err != nil {
		return CaseDetail{}, err
	}
	rows, err = e.DB.Query(ctx, `SELECT n.node_id, r.attempt, r.provider, r.agent_ref, r.skill_version, r.status, r.evidence, r.polls, r.created_at, r.completed_at,r.run_id,r.usage_observation,r.usage_received_at
		FROM agent_runs r JOIN node_executions n ON n.id = r.node_execution_id WHERE r.case_id = $1 ORDER BY r.created_at`, caseID)
	if err != nil {
		return CaseDetail{}, err
	}
	d.AgentRuns, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (AgentRunView, error) {
		var v AgentRunView
		return v, r.Scan(&v.NodeID, &v.Attempt, &v.Provider, &v.AgentRef, &v.SkillVersion, &v.Status, &v.Evidence, &v.Polls, &v.CreatedAt, &v.CompletedAt, &v.RuntimeRunID, &v.UsageObservation, &v.UsageReceivedAt)
	})
	if err != nil {
		return CaseDetail{}, err
	}
	owner := a.UserID == ownerID || a.HasRole(auth.RoleTenantAdmin)
	d.Actions = CaseActions{
		CanPause:  owner && (d.Status == CaseActive || d.Status == CaseUnderReview),
		CanResume: owner && d.Status == CasePaused,
		CanNote:   true,
	}
	return d, nil
}

// --- tasks --------------------------------------------------------------------------------------

// tasks lists tasks the caller may act on or has completed, filtered by extra (uses $3).
func (e *Engine) tasks(ctx context.Context, a auth.Identity, extra string, arg any) ([]Task, error) {
	rows, err := e.DB.Query(ctx, `
		SELECT t.id::text, c.id::text, c.reference, c.title, c.status, t.node_id, t.iteration, t.title, t.assignee_role, t.instructions,
		       t.form_schema, t.revision_note, t.status, t.output, cu.display_name, t.due_at, t.created_at, t.completed_at, t.version_num,
		       EXISTS (SELECT 1 FROM domain_role_assignments d WHERE d.tenant_id = t.tenant_id AND d.user_id = $2 AND d.role = t.assignee_role
		               AND d.revoked_at IS NULL AND (d.case_id IS NULL OR d.case_id = t.case_id)) AS eligible,
		       (c.owner_id = $2) AS is_owner
		FROM tasks t JOIN cases c ON c.id = t.case_id LEFT JOIN users cu ON cu.id = t.completed_by
		WHERE t.tenant_id = $1 AND `+extra+`
		ORDER BY t.created_at DESC LIMIT 200`, a.TenantID, a.UserID, arg)
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for rows.Next() {
		var t Task
		var eligible, isOwner bool
		if err := rows.Scan(&t.ID, &t.Case.ID, &t.Case.Reference, &t.Case.Title, &t.Case.Status, &t.NodeID, &t.Iteration, &t.Title, &t.AssigneeRole,
			&t.Instructions, &t.FormSchema, &t.RevisionNote, &t.Status, &t.Output, &t.CompletedByName, &t.DueAt, &t.CreatedAt, &t.CompletedAt,
			&t.VersionNum, &eligible, &isOwner); err != nil {
			rows.Close()
			return nil, err
		}
		caseRunning := t.Case.Status == CaseActive || t.Case.Status == CaseUnderReview
		t.CanComplete = eligible && a.IsHuman() && t.Status == "assigned" && caseRunning
		t.CanRequestRevision = (isOwner || a.HasRole(auth.RoleTenantAdmin)) && a.IsHuman() && t.Status == "submitted" && caseRunning
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTasks is the caller's inbox: open tasks for their domain roles, plus tasks they completed.
func (e *Engine) ListTasks(ctx context.Context, a auth.Identity, status string) ([]Task, error) {
	if len(a.Roles) == 0 {
		return nil, ErrForbidden
	}
	cond := map[string]string{
		"open":      `t.status = 'assigned'`,
		"completed": `t.status IN ('submitted', 'revision_requested') AND t.completed_by = $2`,
		"all":       `true`,
	}[status]
	if cond == "" {
		return nil, &InputError{Code: "invalid_filter", Message: "status must be open, completed or all", Path: "status"}
	}
	mine := `(t.completed_by = $2 OR EXISTS (SELECT 1 FROM domain_role_assignments d WHERE d.tenant_id = t.tenant_id AND d.user_id = $2
		AND d.role = t.assignee_role AND d.revoked_at IS NULL AND (d.case_id IS NULL OR d.case_id = t.case_id)))`
	return e.tasks(ctx, a, mine+` AND `+cond+` AND $3::text IS NULL`, nil)
}

func (e *Engine) GetTask(ctx context.Context, a auth.Identity, taskID string) (Task, error) {
	list, err := e.tasks(ctx, a, `t.id = $3`, taskID)
	if err != nil {
		return Task{}, err
	}
	if len(list) == 0 {
		return Task{}, ErrNotFound
	}
	t := list[0]
	if visible, err := e.canSeeCase(ctx, e.DB, a, t.Case.ID); err != nil || !visible {
		if err == nil {
			err = ErrNotFound
		}
		return Task{}, err
	}
	if t.CanRequestRevision {
		var execID string
		if err := e.DB.QueryRow(ctx, `SELECT node_execution_id::text FROM tasks WHERE id = $1`, t.ID).Scan(&execID); err != nil {
			return Task{}, err
		}
		_, waiting, err := successorWaiting(ctx, e.DB, execID)
		if err != nil {
			return Task{}, err
		}
		t.CanRequestRevision = waiting
	}
	rows, err := e.DB.Query(ctx, `SELECT u.display_name, c.body, c.created_at FROM task_comments c JOIN users u ON u.id = c.author_id
		WHERE c.task_id = $1 ORDER BY c.created_at`, taskID)
	if err != nil {
		return Task{}, err
	}
	t.Comments, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Comment, error) {
		var c Comment
		return c, r.Scan(&c.AuthorName, &c.Body, &c.CreatedAt)
	})
	return t, err
}

// --- approvals ----------------------------------------------------------------------------------

func (e *Engine) approvals(ctx context.Context, a auth.Identity, extra string, arg any) ([]Approval, error) {
	rows, err := e.DB.Query(ctx, `
		SELECT ar.id::text, c.id::text, c.reference, c.title, c.status, ar.node_id, ar.iteration, ar.title, ar.policy_ref, ar.return_to,
		       ar.subject_hash, ar.subject, ar.status, ar.created_at, ar.version_num,
		       dec.decision, dec.rationale, du.display_name, dec.decision_hash, dec.decided_at, ar.kind, ar.invalidated_reason,
		       coalesce((SELECT jsonb_agg(jsonb_build_object('type', a.artifact_type, 'version', v.version, 'status', v.status, 'checksum', v.checksum)
		                 ORDER BY a.artifact_type) FROM approval_artifacts aa JOIN artifact_versions v ON v.id = aa.artifact_version_id
		                 JOIN artifacts a ON a.id = v.artifact_id WHERE aa.approval_id = ar.id), '[]')
		FROM approval_requests ar JOIN cases c ON c.id = ar.case_id
		LEFT JOIN approval_decisions dec ON dec.approval_id = ar.id LEFT JOIN users du ON du.id = dec.decided_by
		WHERE ar.tenant_id = $1 AND `+extra+` AND $2::uuid IS NOT NULL
		ORDER BY ar.created_at DESC LIMIT 200`, a.TenantID, a.UserID, arg)
	if err != nil {
		return nil, err
	}
	type row struct {
		ap      Approval
		decided *string
	}
	var list []row
	for rows.Next() {
		var r row
		var rationale, decider, dhash *string
		var decidedAt *time.Time
		var covered []byte
		if err := rows.Scan(&r.ap.ID, &r.ap.Case.ID, &r.ap.Case.Reference, &r.ap.Case.Title, &r.ap.Case.Status, &r.ap.NodeID, &r.ap.Iteration,
			&r.ap.Title, &r.ap.PolicyRef, &r.ap.ReturnTo, &r.ap.SubjectHash, &r.ap.Subject, &r.ap.Status, &r.ap.CreatedAt, &r.ap.VersionNum,
			&r.decided, &rationale, &decider, &dhash, &decidedAt, &r.ap.Kind, &r.ap.InvalidatedReason, &covered); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(covered, &r.ap.Covered)
		if r.decided != nil {
			r.ap.Decision = &Decision{Decision: *r.decided, Rationale: *rationale, DecidedByName: deref(decider), DecisionHash: *dhash, DecidedAt: *decidedAt}
		}
		if e.Catalog != nil {
			if p, ok := e.Catalog.Policy(r.ap.PolicyRef); ok {
				r.ap.PolicyName, r.ap.Provisional = p.Name, p.Provisional
			}
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Approval{}
	for _, r := range list {
		ap := r.ap
		if ap.Status == "pending" {
			var c caseRow
			if err := e.DB.QueryRow(ctx, `SELECT id::text, owner_id::text, created_by::text, status FROM cases WHERE id = $1`, ap.Case.ID).Scan(
				&c.ID, &c.OwnerID, &c.CreatedBy, &c.Status); err != nil {
				return nil, err
			}
			_, denied, err := authority(ctx, e.DB, a, &c, ap.PolicyRef, e.sodRules(ap.PolicyRef))
			if err != nil {
				return nil, err
			}
			var rereviews int
			if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM approval_requests WHERE case_id = $1 AND kind = 'rereview' AND status = 'pending' AND id <> $2`,
				c.ID, ap.ID).Scan(&rereviews); err != nil {
				return nil, err
			}
			stale := false
			for _, cv := range ap.Covered {
				stale = stale || cv.Status == "stale"
			}
			reason := ""
			switch {
			case denied != nil:
				reason = denied.Reason
			case !c.running():
				reason = "case_" + c.Status
			case stale:
				reason = "stale_artifacts"
			case ap.Kind == "gate" && rereviews > 0:
				reason = "rereview_pending"
			default:
				ap.CanDecide = true
			}
			if reason != "" {
				ap.BlockedReason = &reason
			}
		}
		out = append(out, ap)
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// delegatedSQL limits the inbox to approvals the caller holds a delegation for, decided, or (admins) all.
const delegatedSQL = `(EXISTS (SELECT 1 FROM policy_delegations pd WHERE pd.tenant_id = ar.tenant_id AND pd.policy_ref = ar.policy_ref
	AND pd.user_id = $2 AND pd.revoked_at IS NULL) OR dec.decided_by = $2 OR $3::boolean)`

// ListApprovals is the server-filtered inbox; each row says whether the caller may decide and why not.
func (e *Engine) ListApprovals(ctx context.Context, a auth.Identity, status string) ([]Approval, error) {
	if len(a.Roles) == 0 {
		return nil, ErrForbidden
	}
	cond := map[string]string{"pending": `ar.status = 'pending'`, "decided": `ar.status IN ('approved', 'rejected', 'returned', 'invalidated')`, "all": `true`}[status]
	if cond == "" {
		return nil, &InputError{Code: "invalid_filter", Message: "status must be pending, decided or all", Path: "status"}
	}
	return e.approvals(ctx, a, delegatedSQL+` AND `+cond, a.HasRole(auth.RoleTenantAdmin))
}

func (e *Engine) GetApproval(ctx context.Context, a auth.Identity, approvalID string) (Approval, error) {
	list, err := e.approvals(ctx, a, `ar.id = $3`, approvalID)
	if err != nil {
		return Approval{}, err
	}
	if len(list) == 0 {
		return Approval{}, ErrNotFound
	}
	if visible, err := e.canSeeCase(ctx, e.DB, a, list[0].Case.ID); err != nil || !visible {
		if err == nil {
			err = ErrNotFound
		}
		return Approval{}, err
	}
	return list[0], nil
}
