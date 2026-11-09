package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrSelfApproval      = errors.New("separation of duties: the requester and reviewer cannot approve their own release")
	ErrEvaluationStale   = errors.New("agent version changed after evaluation; re-run the evaluation")
	ErrEvaluationBlocked = errors.New("evaluation gate did not pass")
	ErrNoRollbackTarget  = errors.New("no previous release to roll back to")
)

type ReleaseRequest struct {
	ID             string     `json:"id"`
	AgentKey       string     `json:"agent_key"`
	AgentVersionID string     `json:"agent_version_id"`
	AgentSemver    string     `json:"agent_semver"`
	EvalRunID      string     `json:"eval_run_id"`
	Environment    string     `json:"environment"`
	Status         string     `json:"status"`
	RequestedBy    string     `json:"requested_by"`
	RequestNote    string     `json:"request_note"`
	ReviewedBy     *string    `json:"reviewed_by,omitempty"`
	ReviewReason   *string    `json:"review_reason,omitempty"`
	ReviewedAt     *time.Time `json:"reviewed_at,omitempty"`
	DecidedBy      *string    `json:"decided_by,omitempty"`
	DecisionReason *string    `json:"decision_reason,omitempty"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
	// SelfApprovalExempt marks a request a development-exempt person reviewed or
	// decided although they also requested or reviewed it.
	SelfApprovalExempt bool      `json:"self_approval_exempt"`
	CreatedAt          time.Time `json:"created_at"`
}

type Release struct {
	ID               string     `json:"id"`
	AgentKey         string     `json:"agent_key"`
	AgentVersionID   string     `json:"agent_version_id"`
	AgentSemver      string     `json:"agent_semver"`
	Environment      string     `json:"environment"`
	ReleaseRequestID *string    `json:"release_request_id,omitempty"`
	Status           string     `json:"status"`
	RolledBackFrom   *string    `json:"rolled_back_from,omitempty"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
}

const requestColumns = `q.id::text, a.agent_key, q.agent_version_id::text, v.semver, q.eval_run_id::text, q.environment, q.status, q.requested_by,
	q.request_note, q.reviewed_by, q.review_reason, q.reviewed_at, q.decided_by, q.decision_reason, q.decided_at, q.self_approval_exempt, q.created_at`
const requestFrom = ` FROM release_requests q JOIN agents a ON a.tenant_id=q.tenant_id AND a.id=q.agent_id
	JOIN agent_versions v ON v.tenant_id=q.tenant_id AND v.id=q.agent_version_id`

func scanRequest(row rowScanner) (ReleaseRequest, error) {
	var q ReleaseRequest
	err := row.Scan(&q.ID, &q.AgentKey, &q.AgentVersionID, &q.AgentSemver, &q.EvalRunID, &q.Environment, &q.Status, &q.RequestedBy,
		&q.RequestNote, &q.ReviewedBy, &q.ReviewReason, &q.ReviewedAt, &q.DecidedBy, &q.DecisionReason, &q.DecidedAt, &q.SelfApprovalExempt, &q.CreatedAt)
	return q, err
}

const releaseColumns = `rel.id::text, a.agent_key, rel.agent_version_id::text, v.semver, rel.environment, rel.release_request_id::text,
	rel.status, rel.rolled_back_from::text, rel.created_by, rel.created_at, rel.ended_at`
const releaseFrom = ` FROM releases rel JOIN agents a ON a.tenant_id=rel.tenant_id AND a.id=rel.agent_id
	JOIN agent_versions v ON v.tenant_id=rel.tenant_id AND v.id=rel.agent_version_id`

func scanRelease(row rowScanner) (Release, error) {
	var rel Release
	err := row.Scan(&rel.ID, &rel.AgentKey, &rel.AgentVersionID, &rel.AgentSemver, &rel.Environment, &rel.ReleaseRequestID,
		&rel.Status, &rel.RolledBackFrom, &rel.CreatedBy, &rel.CreatedAt, &rel.EndedAt)
	return rel, err
}

// releaseGate re-checks, inside the deciding transaction, that the version is
// still a draft, unchanged since evaluation, and that the evaluation passed.
func releaseGate(ctx context.Context, tx *sql.Tx, tenantID, versionID, evalRunID, expectedFingerprint string) (AgentVersion, error) {
	version, err := loadAgentVersion(ctx, tx, tenantID, versionID)
	if err != nil {
		return version, err
	}
	if version.State != "draft" {
		return version, fmt.Errorf("%w: only draft versions can be released", ErrInvalidState)
	}
	var evalVersion, evalFingerprint, status string
	var summary json.RawMessage
	if err := tx.QueryRowContext(ctx, `SELECT agent_version_id::text, version_fingerprint, status, summary_json FROM eval_runs
		WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, evalRunID).Scan(&evalVersion, &evalFingerprint, &status, &summary); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return version, ErrEvaluationBlocked
		}
		return version, err
	}
	if evalVersion != versionID || status != "completed" {
		return version, ErrEvaluationBlocked
	}
	var gate struct {
		Gate string `json:"gate"`
	}
	_ = json.Unmarshal(summary, &gate)
	if gate.Gate != "pass" {
		return version, ErrEvaluationBlocked
	}
	current, _, _, err := versionFingerprint(ctx, tx, tenantID, version)
	if err != nil {
		return version, err
	}
	if current != evalFingerprint || (expectedFingerprint != "" && current != expectedFingerprint) {
		return version, ErrEvaluationStale
	}
	return version, nil
}

func (r *ModelRegistry) CreateReleaseRequest(ctx context.Context, tenantID, versionID, evalRunID, environment, note, actor string) (ReleaseRequest, error) {
	var item ReleaseRequest
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		version, err := releaseGate(ctx, tx, tenantID, versionID, evalRunID, "")
		if err != nil {
			return err
		}
		fingerprint, _, _, err := versionFingerprint(ctx, tx, tenantID, version)
		if err != nil {
			return err
		}
		var id string
		if err := tx.QueryRowContext(ctx, `INSERT INTO release_requests (tenant_id, agent_id, agent_version_id, eval_run_id, version_fingerprint, environment, requested_by, request_note)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8) RETURNING id::text`,
			tenantID, version.AgentID, versionID, evalRunID, fingerprint, environment, actor, note).Scan(&id); err != nil {
			return err
		}
		if err := audit(tx, tenantID, actor, "release.requested", "release_request:"+id); err != nil {
			return err
		}
		item, err = scanRequest(tx.QueryRowContext(ctx, `SELECT `+requestColumns+requestFrom+` WHERE q.tenant_id=$1::uuid AND q.id=$2::uuid`, tenantID, id))
		return err
	})
	return item, err
}

func lockRequest(ctx context.Context, tx *sql.Tx, tenantID, id string) (ReleaseRequest, string, error) {
	var fingerprint string
	if err := tx.QueryRowContext(ctx, `SELECT version_fingerprint FROM release_requests WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, id).Scan(&fingerprint); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReleaseRequest{}, "", ErrRunNotFound
		}
		return ReleaseRequest{}, "", err
	}
	q, err := scanRequest(tx.QueryRowContext(ctx, `SELECT `+requestColumns+requestFrom+` WHERE q.tenant_id=$1::uuid AND q.id=$2::uuid`, tenantID, id))
	return q, fingerprint, err
}

// sameActorAllowed enforces separation of duties for one release step: when
// the actor already holds another role on the request, only a
// development-exempt subject may continue, and that use is audited.
func (r *ModelRegistry) sameActorAllowed(tx *sql.Tx, tenantID, actor, requestID string, sameActor bool) (bool, error) {
	if !sameActor {
		return false, nil
	}
	if !r.IsSelfApprovalExempt(actor) {
		return false, ErrSelfApproval
	}
	return true, audit(tx, tenantID, actor, "release.self_approval_exempt", "release_request:"+requestID)
}

// ReviewRelease records the independent domain (semantic) signoff.
func (r *ModelRegistry) ReviewRelease(ctx context.Context, tenantID, id, actor string, approve bool, reason string) (ReleaseRequest, error) {
	var item ReleaseRequest
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		q, _, err := lockRequest(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if q.Status != "pending_review" {
			return ErrInvalidState
		}
		exempt, err := r.sameActorAllowed(tx, tenantID, actor, id, q.RequestedBy == actor)
		if err != nil {
			return err
		}
		status, action := "pending_ops", "release.domain_review_approved"
		if !approve {
			status, action = "rejected", "release.domain_review_rejected"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE release_requests SET status=$3, reviewed_by=$4, review_reason=$5, reviewed_at=now(),
			self_approval_exempt = self_approval_exempt OR $6 WHERE tenant_id=$1::uuid AND id=$2::uuid`,
			tenantID, id, status, actor, reason, exempt); err != nil {
			return err
		}
		if err := audit(tx, tenantID, actor, action, "release_request:"+id); err != nil {
			return err
		}
		item, _, err = lockRequest(ctx, tx, tenantID, id)
		return err
	})
	return item, err
}

// DecideRelease is the AI Ops decision. Approval re-checks every gate, then
// publishes the version and its draft skills and makes it the active release
// for new runs only; existing runs stay pinned to what they started with.
func (r *ModelRegistry) DecideRelease(ctx context.Context, tenantID, id, actor string, approve bool, reason string) (ReleaseRequest, error) {
	var item ReleaseRequest
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		q, fingerprint, err := lockRequest(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if q.Status != "pending_ops" {
			return ErrInvalidState
		}
		exempt, err := r.sameActorAllowed(tx, tenantID, actor, id, q.RequestedBy == actor || (q.ReviewedBy != nil && *q.ReviewedBy == actor))
		if err != nil {
			return err
		}
		if !approve {
			if _, err := tx.ExecContext(ctx, `UPDATE release_requests SET status='rejected', decided_by=$3, decision_reason=$4, decided_at=now(),
				self_approval_exempt = self_approval_exempt OR $5 WHERE tenant_id=$1::uuid AND id=$2::uuid`,
				tenantID, id, actor, reason, exempt); err != nil {
				return err
			}
			if err := audit(tx, tenantID, actor, "release.ops_rejected", "release_request:"+id); err != nil {
				return err
			}
			item, _, err = lockRequest(ctx, tx, tenantID, id)
			return err
		}
		version, err := releaseGate(ctx, tx, tenantID, q.AgentVersionID, q.EvalRunID, fingerprint)
		if err != nil {
			return err
		}
		skills, err := loadBoundSkills(ctx, tx, tenantID, version.ID)
		if err != nil {
			return err
		}
		for _, s := range skills {
			if s.State != "draft" {
				continue
			}
			checksum := hashParts(s.Semver, s.Instructions, string(s.Procedures), string(s.Examples), string(s.ForbiddenActions), string(s.OutputSchema))
			if _, err := tx.ExecContext(ctx, `UPDATE skill_versions SET state='published', checksum=$3, published_by=$4, published_at=now() WHERE tenant_id=$1::uuid AND id=$2::uuid`,
				tenantID, s.ID, checksum, actor); err != nil {
				return err
			}
			if err := audit(tx, tenantID, actor, "skill_version.published", "skill_version:"+s.ID); err != nil {
				return err
			}
		}
		checksum := hashParts(version.Semver, string(version.Manifest), string(version.InputSchema), string(version.OutputSchema), string(version.ModelPolicy), string(version.ToolPolicy))
		if _, err := tx.ExecContext(ctx, `UPDATE agent_versions SET state='published', checksum=$3, published_by=$4, published_at=now() WHERE tenant_id=$1::uuid AND id=$2::uuid AND state='draft'`,
			tenantID, version.ID, checksum, actor); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE releases SET status='superseded', ended_at=now() WHERE tenant_id=$1::uuid AND agent_id=$2::uuid AND environment=$3 AND status='active'`,
			tenantID, version.AgentID, q.Environment); err != nil {
			return err
		}
		var releaseID string
		if err := tx.QueryRowContext(ctx, `INSERT INTO releases (tenant_id, agent_id, agent_version_id, environment, release_request_id, created_by)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6) RETURNING id::text`, tenantID, version.AgentID, version.ID, q.Environment, id, actor).Scan(&releaseID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET lifecycle_status='active', updated_at=now() WHERE tenant_id=$1::uuid AND id=$2::uuid AND lifecycle_status='draft'`, tenantID, version.AgentID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE release_requests SET status='published', decided_by=$3, decision_reason=$4, decided_at=now(),
			self_approval_exempt = self_approval_exempt OR $5 WHERE tenant_id=$1::uuid AND id=$2::uuid`,
			tenantID, id, actor, reason, exempt); err != nil {
			return err
		}
		if err := audit(tx, tenantID, actor, "agent_version.published", "agent_version:"+version.ID); err != nil {
			return err
		}
		if err := audit(tx, tenantID, actor, "release.published", "release:"+releaseID); err != nil {
			return err
		}
		item, _, err = lockRequest(ctx, tx, tenantID, id)
		return err
	})
	return item, err
}

func (r *ModelRegistry) WithdrawReleaseRequest(ctx context.Context, tenantID, id, actor string) (ReleaseRequest, error) {
	var item ReleaseRequest
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		q, _, err := lockRequest(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if q.RequestedBy != actor || (q.Status != "pending_review" && q.Status != "pending_ops") {
			return ErrInvalidState
		}
		if _, err := tx.ExecContext(ctx, `UPDATE release_requests SET status='withdrawn' WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, id); err != nil {
			return err
		}
		if err := audit(tx, tenantID, actor, "release.withdrawn", "release_request:"+id); err != nil {
			return err
		}
		item, _, err = lockRequest(ctx, tx, tenantID, id)
		return err
	})
	return item, err
}

// RollbackRelease reactivates the previous release for new runs only.
func (r *ModelRegistry) RollbackRelease(ctx context.Context, tenantID, releaseID, actor, reason string) (Release, error) {
	var item Release
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var agentID, environment, status string
		var createdAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT agent_id::text, environment, status, created_at FROM releases WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`,
			tenantID, releaseID).Scan(&agentID, &environment, &status, &createdAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRunNotFound
			}
			return err
		}
		if status != "active" {
			return ErrInvalidState
		}
		var previousVersion string
		if err := tx.QueryRowContext(ctx, `SELECT agent_version_id::text FROM releases WHERE tenant_id=$1::uuid AND agent_id=$2::uuid AND environment=$3
			AND status='superseded' AND created_at < $4 AND agent_version_id <> (SELECT agent_version_id FROM releases WHERE id=$5::uuid)
			ORDER BY created_at DESC LIMIT 1`, tenantID, agentID, environment, createdAt, releaseID).Scan(&previousVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoRollbackTarget
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE releases SET status='rolled_back', ended_at=now() WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, releaseID); err != nil {
			return err
		}
		var newID string
		if err := tx.QueryRowContext(ctx, `INSERT INTO releases (tenant_id, agent_id, agent_version_id, environment, rolled_back_from, created_by)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6) RETURNING id::text`, tenantID, agentID, previousVersion, environment, releaseID, actor).Scan(&newID); err != nil {
			return err
		}
		if err := audit(tx, tenantID, actor, "release.rolled_back", "release:"+releaseID); err != nil {
			return err
		}
		var err error
		item, err = scanRelease(tx.QueryRowContext(ctx, `SELECT `+releaseColumns+releaseFrom+` WHERE rel.tenant_id=$1::uuid AND rel.id=$2::uuid`, tenantID, newID))
		return err
	})
	return item, err
}

func (r *ModelRegistry) ListReleaseRequests(ctx context.Context, tenantID, status string) ([]ReleaseRequest, error) {
	items := make([]ReleaseRequest, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+requestColumns+requestFrom+` WHERE q.tenant_id=$1::uuid AND ($2='' OR q.status=$2) ORDER BY q.created_at DESC LIMIT 200`, tenantID, status)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			q, err := scanRequest(rows)
			if err != nil {
				return err
			}
			items = append(items, q)
		}
		return rows.Err()
	})
	return items, err
}

func (r *ModelRegistry) ListReleases(ctx context.Context, tenantID, agentKey string) ([]Release, error) {
	items := make([]Release, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+releaseColumns+releaseFrom+` WHERE rel.tenant_id=$1::uuid AND ($2='' OR a.agent_key=$2) ORDER BY rel.created_at DESC LIMIT 200`, tenantID, agentKey)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			rel, err := scanRelease(rows)
			if err != nil {
				return err
			}
			items = append(items, rel)
		}
		return rows.Err()
	})
	return items, err
}
