package cases

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/flowcase/internal/audit"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

// Author draft review: an agent step with config "author_review": true does not hand its result to
// the next step directly. The result becomes an artifact version as usual, the execution waits on the
// case owner, who submits the draft onward or sends it back to the agent with feedback. A draft review
// never decides a gate; the gate's own policy and segregation-of-duties rules still apply after it.

// maxDraftRevisions bounds how often the owner may send one step's draft back to the agent.
const maxDraftRevisions = 10

func authorReview(node *definition.Node) bool {
	v, _ := node.Config["author_review"].(bool)
	return v
}

// openDraftReview parks a completed agent run on the case owner instead of scheduling the next step.
func (e *Engine) openDraftReview(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, node *definition.Node, agentRunID, versionID string) error {
	if _, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'waiting_human', output = $2, lease_owner = NULL, lease_expires_at = NULL,
		last_error = NULL, updated_at = now() WHERE id = $1`, x.ID, RawJSON(map[string]any{"agent_run_id": agentRunID, "artifact_version_id": versionID})); err != nil {
		return err
	}
	var reviewID string
	var version int
	err := tx.QueryRow(ctx, `INSERT INTO draft_reviews (tenant_id, case_id, node_execution_id, node_id, iteration, artifact_id, artifact_version_id)
		SELECT $1, $2, $3, $4, $5, v.artifact_id, v.id FROM artifact_versions v WHERE v.id = $6
		ON CONFLICT (node_execution_id) DO NOTHING RETURNING id::text, (SELECT version FROM artifact_versions WHERE id = $6)`,
		c.TenantID, c.ID, x.ID, node.ID, x.Iteration, versionID).Scan(&reviewID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already open (redelivery)
	}
	if err != nil {
		return err
	}
	data := map[string]any{"node_id": node.ID, "iteration": x.Iteration, "artifact_type": node.ConfigString("output_artifact_type"), "artifact_version": version}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "draft.ready", "draft_review", reviewID, data)); err != nil {
		return err
	}
	return outbox(ctx, tx, c, "draft.ready", "draft_review", reviewID, data)
}

type draftRow struct {
	ID, ExecID, NodeID, ArtifactID, ArtifactVersionID, Status string
	Iteration, VersionNum                                     int
}

// lockDraft locks the case, then the review and its execution (the engine's lock order), and applies
// the checks shared by submit and revise: human case owner (or tenant admin), running case, pending
// review on a waiting step, matching version.
func (e *Engine) lockDraft(ctx context.Context, tx pgx.Tx, a auth.Identity, reviewID string, ifMatch int) (*caseRow, *draftRow, *execRow, error) {
	var caseID string
	err := tx.QueryRow(ctx, `SELECT case_id::text FROM draft_reviews WHERE tenant_id = $1 AND id = $2`, a.TenantID, reviewID).Scan(&caseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := lockCase(ctx, tx, a.TenantID, caseID)
	if err != nil {
		return nil, nil, nil, err
	}
	var d draftRow
	if err := tx.QueryRow(ctx, `SELECT id::text, node_execution_id::text, node_id, artifact_id::text, artifact_version_id::text, status, iteration, version_num
		FROM draft_reviews WHERE id = $1 FOR UPDATE`, reviewID).Scan(&d.ID, &d.ExecID, &d.NodeID, &d.ArtifactID, &d.ArtifactVersionID, &d.Status, &d.Iteration, &d.VersionNum); err != nil {
		return nil, nil, nil, err
	}
	if !isOwnerOrAdmin(a, c) || !a.IsHuman() {
		return nil, nil, nil, &DeniedError{Reason: "not_case_owner", Message: "Only the case owner can review agent drafts."}
	}
	if !c.running() {
		return nil, nil, nil, &ConflictError{Code: "case_not_active", Message: fmt.Sprintf("The case is %s.", c.Status)}
	}
	if d.Status != "pending" {
		return nil, nil, nil, &ConflictError{Code: "draft_not_pending", Message: "This draft has already been reviewed."}
	}
	if d.VersionNum != ifMatch {
		return nil, nil, nil, ErrPreconditionFailed
	}
	x, err := lockExec(ctx, tx, d.ExecID)
	if err != nil {
		return nil, nil, nil, err
	}
	if x.Status != "waiting_human" {
		return nil, nil, nil, &ConflictError{Code: "draft_not_pending", Message: "This step is no longer waiting for review."}
	}
	return c, &d, x, nil
}

// SubmitDraft hands the reviewed draft to the next step, exactly as an agent result without author
// review would have. The submitted version is the artifact's current one (the owner may have edited it).
func (e *Engine) SubmitDraft(ctx context.Context, a auth.Identity, reviewID string, ifMatch int, note string) error {
	if len([]rune(note)) > 1000 {
		return &InputError{Code: "invalid_note", Message: "note is limited to 1000 characters", Path: "note"}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		c, d, x, err := e.lockDraft(ctx, tx, a, reviewID, ifMatch)
		if err != nil {
			return err
		}
		var versionID string
		var version int
		if err := tx.QueryRow(ctx, `SELECT v.id::text, v.version FROM artifacts a JOIN artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
			WHERE a.id = $1`, d.ArtifactID).Scan(&versionID, &version); err != nil {
			return err
		}
		var feedback *string
		if note != "" {
			feedback = &note
		}
		if _, err := tx.Exec(ctx, `UPDATE draft_reviews SET status = 'submitted', feedback = $2, artifact_version_id = $3, decided_by = $4, decided_at = now(),
			updated_at = now(), version_num = version_num + 1 WHERE id = $1`, d.ID, feedback, versionID, a.UserID); err != nil {
			return err
		}
		var agentRunID *string
		if err := tx.QueryRow(ctx, `SELECT output->>'agent_run_id' FROM node_executions WHERE id = $1`, x.ID).Scan(&agentRunID); err != nil {
			return err
		}
		if err := completeExec(ctx, tx, x.ID, map[string]any{"agent_run_id": agentRunID, "artifact_version_id": versionID, "draft_review_id": d.ID}); err != nil {
			return err
		}
		p, err := e.pinnedVersion(ctx, tx, c.VersionID)
		if err != nil {
			return err
		}
		node := p.graph.Node(x.NodeID)
		if node == nil {
			return fmt.Errorf("%w: node %q is not in the pinned version", errPermanent, x.NodeID)
		}
		if err := e.schedule(ctx, tx, c, x, node.Next); err != nil {
			return err
		}
		data := map[string]any{"node_id": d.NodeID, "iteration": d.Iteration, "artifact_version": version}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, c, "draft.submitted", "draft_review", d.ID, data)); err != nil {
			return err
		}
		return outbox(ctx, tx, c, "draft.submitted", "draft_review", d.ID, data)
	})
}

// ReviseDraft sends the draft back to the agent: the waiting step is superseded and re-runs as a new
// iteration with the feedback as its revision note (and its previous draft among the upstream artifacts).
func (e *Engine) ReviseDraft(ctx context.Context, a auth.Identity, reviewID string, ifMatch int, feedback string) error {
	if l := len([]rune(feedback)); l < 5 || l > 1000 {
		return &InputError{Code: "feedback_required", Message: "feedback must have 5 to 1000 characters", Path: "feedback"}
	}
	return e.tx(ctx, func(tx pgx.Tx) error {
		c, d, x, err := e.lockDraft(ctx, tx, a, reviewID, ifMatch)
		if err != nil {
			return err
		}
		var revisions int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM draft_reviews WHERE case_id = $1 AND node_id = $2 AND status = 'revision_requested'`,
			c.ID, d.NodeID).Scan(&revisions); err != nil {
			return err
		}
		if revisions >= maxDraftRevisions {
			return &ConflictError{Code: "revision_limit", Message: fmt.Sprintf("This step's draft was already sent back %d times; submit it or edit the artifact.", revisions)}
		}
		if _, err := tx.Exec(ctx, `UPDATE draft_reviews SET status = 'revision_requested', feedback = $2, decided_by = $3, decided_at = now(),
			updated_at = now(), version_num = version_num + 1 WHERE id = $1`, d.ID, feedback, a.UserID); err != nil {
			return err
		}
		if err := e.rewind(ctx, tx, c, x, x.NodeID, feedback, a.UserID); err != nil {
			return err
		}
		data := map[string]any{"node_id": d.NodeID, "iteration": d.Iteration, "feedback": feedback, "revision": revisions + 1}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, c, "draft.revision_requested", "draft_review", d.ID, data)); err != nil {
			return err
		}
		return outbox(ctx, tx, c, "draft.revision_requested", "draft_review", d.ID, data)
	})
}

// --- read model ---------------------------------------------------------------------------------

// drafts lists draft reviews filtered by extra (uses $3). Content is included for pending reviews
// (the artifact's current version, which is what a submit hands on) or for all when withContent.
func (e *Engine) drafts(ctx context.Context, a auth.Identity, extra string, arg any, withContent bool) ([]DraftReview, error) {
	rows, err := e.DB.Query(ctx, `
		SELECT dr.id::text, c.id::text, c.reference, c.title, c.status, c.workflow_version_id::text, dr.node_id, dr.iteration, a.artifact_type,
		       v.version, v.id::text, CASE WHEN dr.status = 'pending' OR $4 THEN v.content END, dr.status, dr.feedback, du.display_name,
		       dr.decided_at, dr.created_at, dr.version_num, (c.owner_id = $2) AS is_owner,
		       (SELECT count(*) FROM draft_reviews r2 WHERE r2.case_id = dr.case_id AND r2.node_id = dr.node_id AND r2.status = 'revision_requested')::int
		FROM draft_reviews dr JOIN cases c ON c.id = dr.case_id JOIN artifacts a ON a.id = dr.artifact_id
		JOIN artifact_versions v ON v.id = CASE WHEN dr.status = 'pending'
			THEN (SELECT cv.id FROM artifact_versions cv WHERE cv.artifact_id = a.id AND cv.version = a.current_version) ELSE dr.artifact_version_id END
		LEFT JOIN users du ON du.id = dr.decided_by
		WHERE dr.tenant_id = $1 AND `+extra+`
		ORDER BY dr.created_at DESC, dr.id DESC LIMIT 200`, a.TenantID, a.UserID, arg, withContent)
	if err != nil {
		return nil, err
	}
	type row struct {
		d         DraftReview
		versionID string
		isOwner   bool
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.d.ID, &r.d.Case.ID, &r.d.Case.Reference, &r.d.Case.Title, &r.d.Case.Status, &r.versionID, &r.d.NodeID, &r.d.Iteration,
			&r.d.ArtifactType, &r.d.ArtifactVersion, &r.d.ArtifactVersionID, &r.d.Content, &r.d.Status, &r.d.Feedback, &r.d.DecidedByName,
			&r.d.DecidedAt, &r.d.CreatedAt, &r.d.VersionNum, &r.isOwner, &r.d.Revisions); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []DraftReview{}
	for _, r := range list {
		d := r.d
		if p, err := e.pinnedVersion(ctx, e.DB, r.versionID); err == nil {
			if n := p.graph.Node(d.NodeID); n != nil {
				d.NodeName = n.Name
			}
		}
		running := d.Case.Status == CaseActive || d.Case.Status == CaseUnderReview
		d.CanSubmit = (r.isOwner || a.HasRole(auth.RoleTenantAdmin)) && a.IsHuman() && d.Status == "pending" && running
		d.CanRevise = d.CanSubmit && d.Revisions < maxDraftRevisions
		out = append(out, d)
	}
	return out, nil
}

// ListDrafts is the case owner's review inbox: drafts of cases the caller owns (all for tenant admins).
func (e *Engine) ListDrafts(ctx context.Context, a auth.Identity, status string) ([]DraftReview, error) {
	if len(a.Roles) == 0 {
		return nil, ErrForbidden
	}
	if status == "" {
		status = "pending"
	}
	cond := map[string]string{
		"pending": `dr.status = 'pending'`,
		"decided": `dr.status IN ('submitted', 'revision_requested')`,
		"all":     `true`,
	}[status]
	if cond == "" {
		return nil, &InputError{Code: "invalid_filter", Message: "status must be pending, decided or all", Path: "status"}
	}
	return e.drafts(ctx, a, `(c.owner_id = $2 OR $3::boolean) AND `+cond, a.HasRole(auth.RoleTenantAdmin), false)
}

func (e *Engine) GetDraft(ctx context.Context, a auth.Identity, reviewID string) (DraftReview, error) {
	list, err := e.drafts(ctx, a, `dr.id = $3`, reviewID, true)
	if err != nil {
		return DraftReview{}, err
	}
	if len(list) == 0 {
		return DraftReview{}, ErrNotFound
	}
	if visible, err := e.canSeeCase(ctx, e.DB, a, list[0].Case.ID); err != nil || !visible {
		if err == nil {
			err = ErrNotFound
		}
		return DraftReview{}, err
	}
	return list[0], nil
}
