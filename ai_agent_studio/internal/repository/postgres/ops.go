package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RunMetrics are pilot baselines (PRD §12), not SLO targets.
type RunMetrics struct {
	WindowHours        int                    `json:"window_hours"`
	ByStatus           map[string]int         `json:"by_status"`
	ByKind             map[string]int         `json:"by_kind"`
	P50DurationMillis  *float64               `json:"p50_duration_ms"`
	P95DurationMillis  *float64               `json:"p95_duration_ms"`
	TokensIn           int                    `json:"tokens_in"`
	TokensOut          int                    `json:"tokens_out"`
	UsageByClass       map[string]UsageRollup `json:"usage_by_class"`
	ToolDenials        int                    `json:"tool_denials"`
	GroundedRate       *float64               `json:"grounded_rate"`
	EvalRunsCompleted  int                    `json:"eval_runs_completed"`
	EvalGateBlocked    int                    `json:"eval_gate_blocked"`
	Rollbacks          int                    `json:"rollbacks"`
	ActiveReleases     int                    `json:"active_releases"`
	PendingReleaseReqs int                    `json:"pending_release_requests"`
	OutboxPending      int                    `json:"outbox_pending"`
	OutboxDead         int                    `json:"outbox_dead"`
}
type UsageRollup struct {
	TokensIn       int `json:"tokens_in"`
	TokensOut      int `json:"tokens_out"`
	Runs           int `json:"runs"`
	IncompleteRuns int `json:"incomplete_runs"`
}

func (r *ModelRegistry) RunMetrics(ctx context.Context, tenantID string, window time.Duration) (RunMetrics, error) {
	m := RunMetrics{WindowHours: int(window.Hours()), ByStatus: map[string]int{}, ByKind: map[string]int{}, UsageByClass: map[string]UsageRollup{}}
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		since := time.Now().Add(-window)
		rows, err := tx.QueryContext(ctx, `SELECT status, run_kind, count(*) FROM agent_runs WHERE tenant_id=$1::uuid AND created_at >= $2 GROUP BY status, run_kind`, tenantID, since)
		if err != nil {
			return err
		}
		for rows.Next() {
			var status, kind string
			var n int
			if err := rows.Scan(&status, &kind, &n); err != nil {
				rows.Close()
				return err
			}
			m.ByStatus[status] += n
			m.ByKind[kind] += n
		}
		rows.Close()
		if err := tx.QueryRowContext(ctx, `SELECT
				percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM completed_at - started_at) * 1000),
				percentile_cont(0.95) WITHIN GROUP (ORDER BY extract(epoch FROM completed_at - started_at) * 1000),
				CASE WHEN count(*) FILTER (WHERE status IN ('succeeded','needs_review')) = 0 THEN NULL ELSE
					(count(*) FILTER (WHERE status IN ('succeeded','needs_review') AND NOT (validation_json->'risk_flags' ?| ARRAY['unsupported_claim','unknown_citation'])))::float8
					/ count(*) FILTER (WHERE status IN ('succeeded','needs_review')) END
			FROM agent_runs WHERE tenant_id=$1::uuid AND created_at >= $2 AND completed_at IS NOT NULL AND started_at IS NOT NULL`, tenantID, since).
			Scan(&m.P50DurationMillis, &m.P95DurationMillis, &m.GroundedRate); err != nil {
			return err
		}
		usageRows, err := tx.QueryContext(ctx, `SELECT CASE WHEN r.model_mode='dev_mock' THEN 'mock' WHEN r.run_kind='production' THEN 'actual' ELSE 'test' END,
         COALESCE(sum(COALESCE(u.tokens_in,r.tokens_in)),0),COALESCE(sum(COALESCE(u.tokens_out,r.tokens_out)),0),count(*),
         count(*) FILTER(WHERE u.records<r.attempts OR u.records=0 OR u.reports=0 OR u.missing>0 OR u.unavailable>0)
         FROM agent_runs r LEFT JOIN LATERAL (SELECT sum(tokens_in) AS tokens_in,sum(tokens_out) AS tokens_out,count(*) AS records,COALESCE(sum(usage_reports),0) AS reports,COALESCE(sum(missing_usage),0) AS missing,count(*) FILTER(WHERE usage_reports=0) AS unavailable FROM agent_run_usage WHERE tenant_id=r.tenant_id AND run_id=r.id)u ON true
         WHERE r.tenant_id=$1::uuid AND r.created_at>=$2 GROUP BY 1`, tenantID, since)
		if err != nil {
			return err
		}
		for usageRows.Next() {
			var class string
			var totals UsageRollup
			if err := usageRows.Scan(&class, &totals.TokensIn, &totals.TokensOut, &totals.Runs, &totals.IncompleteRuns); err != nil {
				usageRows.Close()
				return err
			}
			m.UsageByClass[class] = totals
			m.TokensIn += totals.TokensIn
			m.TokensOut += totals.TokensOut
		}
		usageErr := usageRows.Err()
		usageRows.Close()
		if usageErr != nil {
			return usageErr
		}
		queries := []struct {
			dest  *int
			query string
		}{
			{&m.ToolDenials, `SELECT count(*) FROM agent_run_events WHERE tenant_id=$1::uuid AND created_at >= $2 AND event_type='tool.denied'`},
			{&m.EvalRunsCompleted, `SELECT count(*) FROM eval_runs WHERE tenant_id=$1::uuid AND created_at >= $2 AND status='completed'`},
			{&m.EvalGateBlocked, `SELECT count(*) FROM eval_runs WHERE tenant_id=$1::uuid AND created_at >= $2 AND summary_json->>'gate'='blocked'`},
			{&m.Rollbacks, `SELECT count(*) FROM releases WHERE tenant_id=$1::uuid AND created_at >= $2 AND rolled_back_from IS NOT NULL`},
		}
		for _, q := range queries {
			if err := tx.QueryRowContext(ctx, q.query, tenantID, since).Scan(q.dest); err != nil {
				return err
			}
		}
		snapshot := []struct {
			dest  *int
			query string
		}{
			{&m.ActiveReleases, `SELECT count(*) FROM releases WHERE tenant_id=$1::uuid AND status='active'`},
			{&m.PendingReleaseReqs, `SELECT count(*) FROM release_requests WHERE tenant_id=$1::uuid AND status IN ('pending_review','pending_ops')`},
			{&m.OutboxPending, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1::uuid AND status='pending'`},
			{&m.OutboxDead, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1::uuid AND status='dead'`},
		}
		for _, q := range snapshot {
			if err := tx.QueryRowContext(ctx, q.query, tenantID).Scan(q.dest); err != nil {
				return err
			}
		}
		return nil
	})
	return m, err
}

type FeedbackItem struct {
	ID                string    `json:"id"`
	AgentKey          string    `json:"agent_key"`
	RunID             *string   `json:"run_id,omitempty"`
	Classification    string    `json:"classification"`
	Note              string    `json:"note"`
	ReusableCandidate bool      `json:"reusable_candidate"`
	Status            string    `json:"status"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
}

// CreateFeedback records a correction. It never edits a skill: reusable
// candidates go through a new draft, evaluation and release like any change.
func (r *ModelRegistry) CreateFeedback(ctx context.Context, tenantID, agentKey, runID, classification, note string, reusable bool, actor string) (FeedbackItem, error) {
	var item FeedbackItem
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var agentID string
		if err := tx.QueryRowContext(ctx, `SELECT id::text FROM agents WHERE tenant_id=$1::uuid AND agent_key=$2`, tenantID, agentKey).Scan(&agentID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrAgentNotFound
			}
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO feedback_items (tenant_id, agent_id, run_id, classification, note, reusable_candidate, created_by)
			VALUES ($1::uuid,$2::uuid,nullif($3,'')::uuid,$4,$5,$6,$7)
			RETURNING id::text, run_id::text, classification, note, reusable_candidate, status, created_by, created_at`,
			tenantID, agentID, runID, classification, note, reusable, actor).
			Scan(&item.ID, &item.RunID, &item.Classification, &item.Note, &item.ReusableCandidate, &item.Status, &item.CreatedBy, &item.CreatedAt); err != nil {
			return err
		}
		item.AgentKey = agentKey
		return audit(tx, tenantID, actor, "feedback.created", "feedback:"+item.ID)
	})
	return item, err
}

func (r *ModelRegistry) ListFeedback(ctx context.Context, tenantID, agentKey string) ([]FeedbackItem, error) {
	items := make([]FeedbackItem, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT f.id::text, a.agent_key, f.run_id::text, f.classification, f.note, f.reusable_candidate, f.status, f.created_by, f.created_at
			FROM feedback_items f JOIN agents a ON a.tenant_id=f.tenant_id AND a.id=f.agent_id
			WHERE f.tenant_id=$1::uuid AND ($2='' OR a.agent_key=$2) ORDER BY f.created_at DESC LIMIT 200`, tenantID, agentKey)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f FeedbackItem
			if err := rows.Scan(&f.ID, &f.AgentKey, &f.RunID, &f.Classification, &f.Note, &f.ReusableCandidate, &f.Status, &f.CreatedBy, &f.CreatedAt); err != nil {
				return err
			}
			items = append(items, f)
		}
		return rows.Err()
	})
	return items, err
}

type AuditEvent struct {
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *ModelRegistry) ListAuditEvents(ctx context.Context, tenantID string, limit, offset int) ([]AuditEvent, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]AuditEvent, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1::uuid`, tenantID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT actor_subject, action, resource_ref, created_at FROM audit_events WHERE tenant_id=$1::uuid
			ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e AuditEvent
			if err := rows.Scan(&e.Actor, &e.Action, &e.Resource, &e.CreatedAt); err != nil {
				return err
			}
			items = append(items, e)
		}
		return rows.Err()
	})
	return items, total, err
}
