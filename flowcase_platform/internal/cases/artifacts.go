package cases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/flowcase/internal/audit"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

// isAncestor reports whether node a precedes node b in the pinned graph.
func (p *pinned) isAncestor(a, b string) bool { return a != b && p.graph.Reachable(a)[b] }

type currentArtifact struct {
	ID, Type, Producer, VersionID, Checksum, Status string
	Version                                         int
}

func currentArtifacts(ctx context.Context, q querier, caseID string) ([]currentArtifact, error) {
	rows, err := q.Query(ctx, `SELECT a.id::text, a.artifact_type, a.producer_node_id, v.id::text, v.checksum, v.status, v.version
		FROM artifacts a JOIN artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
		WHERE a.case_id = $1 ORDER BY a.artifact_type`, caseID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (currentArtifact, error) {
		var x currentArtifact
		return x, r.Scan(&x.ID, &x.Type, &x.Producer, &x.VersionID, &x.Checksum, &x.Status, &x.Version)
	})
}

// covered returns the current versions of artifacts produced upstream of the gate.
func (e *Engine) covered(ctx context.Context, q querier, c *caseRow, p *pinned, gate string) ([]currentArtifact, error) {
	all, err := currentArtifacts(ctx, q, c.ID)
	if err != nil {
		return nil, err
	}
	var out []currentArtifact
	for _, a := range all {
		if p.isAncestor(a.Producer, gate) {
			out = append(out, a)
		}
	}
	return out, nil
}

// approvalSubject binds a decision to the exact case state it covers: pinned definition, variables,
// gate/iteration and the checksums of every covered artifact version.
func (e *Engine) approvalSubject(ctx context.Context, q querier, c *caseRow, p *pinned, gate string, iteration int) (map[string]any, string, []currentArtifact, error) {
	cov, err := e.covered(ctx, q, c, p, gate)
	if err != nil {
		return nil, "", nil, err
	}
	arts := []map[string]any{}
	for _, a := range cov {
		arts = append(arts, map[string]any{"type": a.Type, "version": a.Version, "checksum": a.Checksum})
	}
	subject := map[string]any{
		"workflow_key": c.WorkflowKey, "workflow_version": c.WorkflowVersion, "definition_hash": p.hash,
		"gate": gate, "iteration": iteration, "variables_hash": sha(decodeVars(c.Variables)), "artifacts": arts,
	}
	return subject, sha(subject), cov, nil
}

func bindArtifacts(ctx context.Context, tx pgx.Tx, approvalID string, cov []currentArtifact) error {
	if _, err := tx.Exec(ctx, `DELETE FROM approval_artifacts WHERE approval_id = $1`, approvalID); err != nil {
		return err
	}
	for _, a := range cov {
		if _, err := tx.Exec(ctx, `INSERT INTO approval_artifacts (approval_id, artifact_version_id) VALUES ($1, $2)`, approvalID, a.VersionID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) openApproval(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, p *pinned, node *definition.Node) error {
	subject, hash, cov, err := e.approvalSubject(ctx, tx, c, p, node.ID, x.Iteration)
	if err != nil {
		return err
	}
	var returnTo *string
	if r := node.ConfigString("return_to"); r != "" {
		returnTo = &r
	}
	var approvalID string
	err = tx.QueryRow(ctx, `INSERT INTO approval_requests (tenant_id, case_id, node_execution_id, node_id, iteration, title, policy_ref,
			return_to, subject_hash, subject)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT (node_execution_id) DO NOTHING RETURNING id::text`,
		c.TenantID, c.ID, x.ID, node.ID, x.Iteration, node.Name, node.ConfigString("policy_ref"), returnTo, hash, RawJSON(subject)).Scan(&approvalID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'waiting_human', lease_owner = NULL, lease_expires_at = NULL,
		updated_at = now() WHERE id = $1`, x.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE cases SET status = 'under_review', updated_at = now(), version_num = version_num + 1
		WHERE id = $1 AND status = 'active'`, c.ID); err != nil {
		return err
	}
	if approvalID == "" {
		return nil
	}
	if err := bindArtifacts(ctx, tx, approvalID, cov); err != nil {
		return err
	}
	data := map[string]any{"node_id": node.ID, "iteration": x.Iteration, "policy_ref": node.ConfigString("policy_ref"), "subject_hash": hash, "artifacts": len(cov)}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "approval.requested", "approval", approvalID, data)); err != nil {
		return err
	}
	return outbox(ctx, tx, c, "approval.requested", "approval", approvalID, data)
}

type produceInput struct {
	NodeID, Type, Kind, ExecID, UserID, AgentRunID, Note string
	Content                                              json.RawMessage
	Evidence                                             []evidenceItem
}

// produceArtifact stores a new immutable version, supersedes the previous one, records dependencies
// on the current versions of upstream artifacts and the evidence it cites. A new version of an
// existing artifact triggers targeted invalidation downstream.
func (e *Engine) produceArtifact(ctx context.Context, tx pgx.Tx, c *caseRow, p *pinned, in produceInput) (string, error) {
	if in.Type == "" {
		return "", nil
	}
	checksum, err := agent.OutputChecksum(in.Content)
	if err != nil {
		return "", fmt.Errorf("artifact content: %w", err)
	}
	var artifactID string
	var current int
	if err := tx.QueryRow(ctx, `INSERT INTO artifacts (tenant_id, case_id, artifact_type, producer_node_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (case_id, artifact_type) DO UPDATE SET updated_at = now() RETURNING id::text, current_version`,
		c.TenantID, c.ID, in.Type, in.NodeID).Scan(&artifactID, &current); err != nil {
		return "", err
	}
	next := current + 1
	if current > 0 {
		if _, err := tx.Exec(ctx, `UPDATE artifact_versions SET status = 'superseded' WHERE artifact_id = $1 AND version = $2`, artifactID, current); err != nil {
			return "", err
		}
	}
	var versionID string
	if err := tx.QueryRow(ctx, `INSERT INTO artifact_versions (tenant_id, artifact_id, version, content, checksum, produced_by_kind, produced_by_exec,
			produced_by_user, agent_run_id, change_note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id::text`,
		c.TenantID, artifactID, next, in.Content, checksum, in.Kind, nullUUID(in.ExecID), nullUUID(in.UserID), nullUUID(in.AgentRunID), in.Note).Scan(&versionID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE artifacts SET current_version = $2, version_num = version_num + 1, updated_at = now() WHERE id = $1`, artifactID, next); err != nil {
		return "", err
	}
	others, err := currentArtifacts(ctx, tx, c.ID)
	if err != nil {
		return "", err
	}
	for _, o := range others {
		if o.ID != artifactID && p.isAncestor(o.Producer, in.NodeID) {
			if _, err := tx.Exec(ctx, `INSERT INTO artifact_dependencies (artifact_version_id, depends_on_version_id) VALUES ($1, $2)`, versionID, o.VersionID); err != nil {
				return "", err
			}
		}
	}
	for _, ev := range in.Evidence {
		if _, err := tx.Exec(ctx, `INSERT INTO artifact_evidence_refs (artifact_version_id, source_id, source_version_id, title, classification)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, versionID, ev.SourceID, ev.VersionID, ev.Title, ev.Classification); err != nil {
			return "", err
		}
	}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "artifact.version_created", "artifact", artifactID,
		map[string]any{"type": in.Type, "version": next, "checksum": checksum, "kind": in.Kind, "evidence": len(in.Evidence)})); err != nil {
		return "", err
	}
	if current > 0 {
		if err := e.invalidate(ctx, tx, c, p, artifactID, fmt.Sprintf("%s changed to v%d", in.Type, next)); err != nil {
			return "", err
		}
	}
	return versionID, nil
}

// invalidate marks artifacts downstream of the changed one stale, then re-opens only the reviews
// that covered an affected version. Upstream and sibling artifacts and their approvals stay valid.
func (e *Engine) invalidate(ctx context.Context, tx pgx.Tx, c *caseRow, p *pinned, changedID, reason string) error {
	all, err := currentArtifacts(ctx, tx, c.ID)
	if err != nil {
		return err
	}
	var producer string
	for _, a := range all {
		if a.ID == changedID {
			producer = a.Producer
		}
	}
	affected := []string{changedID}
	var staleTypes []string
	for _, a := range all {
		if a.ID == changedID || !p.isAncestor(producer, a.Producer) {
			continue
		}
		affected = append(affected, a.ID)
		staleTypes = append(staleTypes, a.Type)
		if _, err := tx.Exec(ctx, `UPDATE artifact_versions SET status = 'stale', stale_reason = $2 WHERE id = $1 AND status <> 'superseded'`, a.VersionID, reason); err != nil {
			return err
		}
	}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "artifact.invalidated", "artifact", changedID,
		map[string]any{"reason": reason, "stale_downstream": staleTypes})); err != nil {
		return err
	}
	return e.reviewImpact(ctx, tx, c, p, affected, reason)
}

func (e *Engine) reviewImpact(ctx context.Context, tx pgx.Tx, c *caseRow, p *pinned, affected []string, reason string) error {
	// 1. Approved decisions that covered a version which is no longer valid: invalidate and re-open.
	rows, err := tx.Query(ctx, `SELECT DISTINCT ar.id::text, ar.node_execution_id::text, ar.node_id, ar.title, ar.policy_ref
		FROM approval_requests ar JOIN approval_artifacts aa ON aa.approval_id = ar.id JOIN artifact_versions v ON v.id = aa.artifact_version_id
		WHERE ar.case_id = $1 AND ar.status = 'approved' AND v.artifact_id::text = ANY($2) AND v.status IN ('stale', 'superseded')`, c.ID, affected)
	if err != nil {
		return err
	}
	type approved struct{ ID, ExecID, NodeID, Title, Policy string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (approved, error) {
		var a approved
		return a, r.Scan(&a.ID, &a.ExecID, &a.NodeID, &a.Title, &a.Policy)
	})
	if err != nil {
		return err
	}
	for _, a := range list {
		if _, err := tx.Exec(ctx, `UPDATE approval_requests SET status = 'invalidated', invalidated_reason = $2, updated_at = now(),
			version_num = version_num + 1 WHERE id = $1`, a.ID, reason); err != nil {
			return err
		}
		var tokenID string
		if err := tx.QueryRow(ctx, `SELECT token_id::text FROM node_executions WHERE id = $1`, a.ExecID).Scan(&tokenID); err != nil {
			return err
		}
		var execID string
		var iteration int
		input := RawJSON(map[string]any{"rereview": true, "reason": reason, "supersedes": a.ID})
		err := tx.QueryRow(ctx, `INSERT INTO node_executions (tenant_id, case_id, token_id, node_id, node_type, iteration, status, input,
				idempotency_key, predecessor_id, max_attempts)
			SELECT $1, $2, $3, $4, 'human_approval', coalesce(max(iteration), 0) + 1, 'waiting_human', $5, $6, $7, $8
			FROM node_executions WHERE case_id = $2 AND token_id = $3 AND node_id = $4
			ON CONFLICT (idempotency_key) DO NOTHING RETURNING id::text, iteration`,
			c.TenantID, c.ID, tokenID, a.NodeID, input, "rereview:"+a.ID, a.ExecID, e.maxAttempts()).Scan(&execID, &iteration)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		subject, hash, cov, err := e.approvalSubject(ctx, tx, c, p, a.NodeID, iteration)
		if err != nil {
			return err
		}
		var newID string
		if err := tx.QueryRow(ctx, `INSERT INTO approval_requests (tenant_id, case_id, node_execution_id, node_id, iteration, title, policy_ref,
				subject_hash, subject, kind, supersedes_approval_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'rereview', $10) RETURNING id::text`,
			c.TenantID, c.ID, execID, a.NodeID, iteration, a.Title, a.Policy, hash, RawJSON(subject), a.ID).Scan(&newID); err != nil {
			return err
		}
		if err := bindArtifacts(ctx, tx, newID, cov); err != nil {
			return err
		}
		data := map[string]any{"node_id": a.NodeID, "invalidated_approval": a.ID, "reason": reason}
		if err := audit.Record(ctx, tx, systemAudit(ctx, c, "approval.invalidated", "approval", a.ID, data)); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, systemAudit(ctx, c, "approval.rereview_requested", "approval", newID, data)); err != nil {
			return err
		}
		if err := outbox(ctx, tx, c, "approval.rereview_requested", "approval", newID, data); err != nil {
			return err
		}
	}
	// 2. Pending approvals covering an affected artifact are re-bound to the current versions.
	rows, err = tx.Query(ctx, `SELECT DISTINCT ar.id::text, ar.node_id, ar.iteration FROM approval_requests ar
		JOIN approval_artifacts aa ON aa.approval_id = ar.id JOIN artifact_versions v ON v.id = aa.artifact_version_id
		WHERE ar.case_id = $1 AND ar.status = 'pending' AND v.artifact_id::text = ANY($2)`, c.ID, affected)
	if err != nil {
		return err
	}
	type pending struct {
		ID, NodeID string
		Iteration  int
	}
	plist, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pending, error) {
		var x pending
		return x, r.Scan(&x.ID, &x.NodeID, &x.Iteration)
	})
	if err != nil {
		return err
	}
	for _, a := range plist {
		subject, hash, cov, err := e.approvalSubject(ctx, tx, c, p, a.NodeID, a.Iteration)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE approval_requests SET subject = $2, subject_hash = $3, updated_at = now(), version_num = version_num + 1
			WHERE id = $1`, a.ID, RawJSON(subject), hash); err != nil {
			return err
		}
		if err := bindArtifacts(ctx, tx, a.ID, cov); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, systemAudit(ctx, c, "approval.rebound", "approval", a.ID, map[string]any{"reason": reason, "subject_hash": hash})); err != nil {
			return err
		}
	}
	return refreshReviewStatus(ctx, tx, c.ID)
}

// ReviseArtifact records a human revision (e.g. a changed requirement) as a new immutable version.
func (e *Engine) ReviseArtifact(ctx context.Context, a auth.Identity, caseID, artifactID string, ifMatch int, content json.RawMessage, note string) error {
	if l := len([]rune(note)); l < 5 || l > 2000 {
		return &InputError{Code: "change_note_required", Message: "change_note must have 5 to 2000 characters", Path: "change_note"}
	}
	var obj map[string]any
	if len(content) == 0 || len(content) > 256<<10 || json.Unmarshal(content, &obj) != nil {
		return &InputError{Code: "invalid_content", Message: "content must be a JSON object of at most 256 KiB", Path: "content"}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		c, err := lockCase(ctx, tx, a.TenantID, caseID)
		if err != nil {
			return err
		}
		if !isOwnerOrAdmin(a, c) || !a.IsHuman() {
			return &DeniedError{Reason: "not_case_owner", Message: "Only the case owner can revise artifacts."}
		}
		if !c.running() {
			return &ConflictError{Code: "case_not_active", Message: fmt.Sprintf("The case is %s.", c.Status)}
		}
		var typ, producer string
		var versionNum int
		err = tx.QueryRow(ctx, `SELECT artifact_type, producer_node_id, version_num FROM artifacts WHERE id = $1 AND case_id = $2 FOR UPDATE`,
			artifactID, caseID).Scan(&typ, &producer, &versionNum)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if versionNum != ifMatch {
			return ErrPreconditionFailed
		}
		p, err := e.pinnedVersion(ctx, tx, c.VersionID)
		if err != nil {
			return err
		}
		if _, err := e.produceArtifact(ctx, tx, c, p, produceInput{NodeID: producer, Type: typ, Kind: "revision", UserID: a.UserID, Note: note, Content: content}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(ctx, a, c, "artifact.revised", "artifact", artifactID, map[string]any{"type": typ, "note": note}))
	})
}

// HandleSourceChange re-checks every live artifact citing a KMS source (with the case owner's
// permissions). Withdrawn, expired, re-versioned or no-longer-permitted evidence makes the citing
// artifact stale and re-opens dependent reviews; stale evidence is never silently accepted.
func (e *Engine) HandleSourceChange(ctx context.Context, tenantID, sourceID string) (int, error) {
	if e.Knowledge == nil {
		return 0, nil
	}
	rows, err := e.DB.Query(ctx, `SELECT r.artifact_version_id::text, a.id::text, c.id::text, c.owner_id::text, r.source_version_id
		FROM artifact_evidence_refs r JOIN artifact_versions v ON v.id = r.artifact_version_id
		JOIN artifacts a ON a.id = v.artifact_id AND a.current_version = v.version JOIN cases c ON c.id = a.case_id
		WHERE c.tenant_id = $1 AND r.source_id = $2 AND r.status = 'valid' AND c.status IN ('active', 'under_review', 'paused')`, tenantID, sourceID)
	if err != nil {
		return 0, err
	}
	type ref struct{ VersionID, ArtifactID, CaseID, OwnerID, SourceVersion string }
	refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ref, error) {
		var x ref
		return x, r.Scan(&x.VersionID, &x.ArtifactID, &x.CaseID, &x.OwnerID, &x.SourceVersion)
	})
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, r := range refs {
		scope := kms.Scope{TenantID: tenantID, UserID: r.OwnerID, CaseID: r.CaseID}
		src, gerr := e.Knowledge.GetSource(ctx, scope, kms.SourceRef{SourceID: sourceID})
		status := ""
		switch {
		case errors.Is(gerr, kms.ErrForbidden) || (gerr == nil && kms.CheckTenant(scope, src) != nil):
			status = "forbidden"
		case gerr != nil || src.Status != kms.SourcePublished || src.Ref.VersionID != r.SourceVersion || (src.ExpiresAt != nil && src.ExpiresAt.Before(time.Now())):
			status = "stale"
		}
		if status == "" {
			if _, err := e.DB.Exec(ctx, `UPDATE artifact_evidence_refs SET checked_at = now() WHERE artifact_version_id = $1 AND source_id = $2`, r.VersionID, sourceID); err != nil {
				return changed, err
			}
			continue
		}
		err := e.tx(ctx, func(tx pgx.Tx) error {
			c, err := lockCase(ctx, tx, tenantID, r.CaseID)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE artifact_evidence_refs SET status = $3, checked_at = now() WHERE artifact_version_id = $1 AND source_id = $2`,
				r.VersionID, sourceID, status); err != nil {
				return err
			}
			reason := fmt.Sprintf("evidence %s is %s", sourceID, map[string]string{"stale": "withdrawn or outdated", "forbidden": "no longer permitted"}[status])
			if _, err := tx.Exec(ctx, `UPDATE artifact_versions SET status = 'stale', stale_reason = $2 WHERE id = $1 AND status <> 'superseded'`, r.VersionID, reason); err != nil {
				return err
			}
			p, err := e.pinnedVersion(ctx, tx, c.VersionID)
			if err != nil {
				return err
			}
			if err := audit.Record(ctx, tx, systemAudit(ctx, c, "evidence.invalidated", "artifact", r.ArtifactID, map[string]any{"source_id": sourceID, "status": status})); err != nil {
				return err
			}
			return e.invalidate(ctx, tx, c, p, r.ArtifactID, reason)
		})
		if err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// ReconcileEvidence re-checks sources not verified within olderThan (worker housekeeping; a KMS
// source-event feed can call HandleSourceChange directly once its contract exists).
func (e *Engine) ReconcileEvidence(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	rows, err := e.DB.Query(ctx, `SELECT DISTINCT c.tenant_id::text, r.source_id FROM artifact_evidence_refs r
		JOIN artifact_versions v ON v.id = r.artifact_version_id JOIN artifacts a ON a.id = v.artifact_id JOIN cases c ON c.id = a.case_id
		WHERE r.status = 'valid' AND r.checked_at < now() - make_interval(secs => $1) AND c.status IN ('active', 'under_review', 'paused')
		LIMIT $2`, olderThan.Seconds(), limit)
	if err != nil {
		return 0, err
	}
	type key struct{ Tenant, Source string }
	keys, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (key, error) {
		var k key
		return k, r.Scan(&k.Tenant, &k.Source)
	})
	if err != nil {
		return 0, err
	}
	total := 0
	for _, k := range keys {
		n, err := e.HandleSourceChange(ctx, k.Tenant, k.Source)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// --- read model ---------------------------------------------------------------------------------

type ArtifactRef struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
}

type EvidenceRef struct {
	SourceID       string `json:"source_id"`
	VersionID      string `json:"version_id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	Classification string `json:"classification"`
}

type ArtifactVersion struct {
	ID          string          `json:"id"`
	Version     int             `json:"version"`
	Status      string          `json:"status"`
	Checksum    string          `json:"checksum"`
	Kind        string          `json:"produced_by_kind"`
	ByName      *string         `json:"produced_by_name"`
	ChangeNote  string          `json:"change_note"`
	StaleReason *string         `json:"stale_reason"`
	Content     json.RawMessage `json:"content"`
	CreatedAt   time.Time       `json:"created_at"`
	DependsOn   []ArtifactRef   `json:"depends_on"`
	Evidence    []EvidenceRef   `json:"evidence"`
}

type Artifact struct {
	ID             string            `json:"id"`
	Type           string            `json:"artifact_type"`
	ProducerNodeID string            `json:"producer_node_id"`
	CurrentVersion int               `json:"current_version"`
	VersionNum     int               `json:"version_num"`
	Versions       []ArtifactVersion `json:"versions"`
}

func (e *Engine) artifacts(ctx context.Context, caseID string) ([]Artifact, error) {
	rows, err := e.DB.Query(ctx, `SELECT id::text, artifact_type, producer_node_id, current_version, version_num FROM artifacts WHERE case_id = $1 ORDER BY created_at`, caseID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Artifact, error) {
		var a Artifact
		return a, r.Scan(&a.ID, &a.Type, &a.ProducerNodeID, &a.CurrentVersion, &a.VersionNum)
	})
	if err != nil {
		return nil, err
	}
	for i := range list {
		rows, err := e.DB.Query(ctx, `SELECT v.id::text, v.version, v.status, v.checksum, v.produced_by_kind, u.display_name, v.change_note, v.stale_reason,
				v.content, v.created_at,
				coalesce((SELECT jsonb_agg(jsonb_build_object('type', a2.artifact_type, 'version', v2.version)) FROM artifact_dependencies d
					JOIN artifact_versions v2 ON v2.id = d.depends_on_version_id JOIN artifacts a2 ON a2.id = v2.artifact_id WHERE d.artifact_version_id = v.id), '[]'),
				coalesce((SELECT jsonb_agg(jsonb_build_object('source_id', r.source_id, 'version_id', r.source_version_id, 'title', r.title,
					'status', r.status, 'classification', r.classification)) FROM artifact_evidence_refs r WHERE r.artifact_version_id = v.id), '[]')
			FROM artifact_versions v LEFT JOIN users u ON u.id = v.produced_by_user WHERE v.artifact_id = $1 ORDER BY v.version DESC`, list[i].ID)
		if err != nil {
			return nil, err
		}
		list[i].Versions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ArtifactVersion, error) {
			var v ArtifactVersion
			var deps, ev []byte
			err := r.Scan(&v.ID, &v.Version, &v.Status, &v.Checksum, &v.Kind, &v.ByName, &v.ChangeNote, &v.StaleReason, &v.Content, &v.CreatedAt, &deps, &ev)
			_ = json.Unmarshal(deps, &v.DependsOn)
			_ = json.Unmarshal(ev, &v.Evidence)
			sort.Slice(v.DependsOn, func(a, b int) bool { return v.DependsOn[a].Type < v.DependsOn[b].Type })
			return v, err
		})
		if err != nil {
			return nil, err
		}
	}
	return list, nil
}

// ListArtifacts returns the case's artifacts with every version, dependency and evidence reference.
func (e *Engine) ListArtifacts(ctx context.Context, a auth.Identity, caseID string) ([]Artifact, error) {
	visible, err := e.canSeeCase(ctx, e.DB, a, caseID)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrNotFound
	}
	return e.artifacts(ctx, caseID)
}
