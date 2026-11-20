package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// WorkItem is a claimed scheduling entry. It carries ids only; the worker
// re-enters the tenant's RLS scope before reading anything else.
type WorkItem struct {
	Kind     string
	TenantID string
	RefID    string
	Attempts int
}

// ClaimWork leases the oldest ready item of a kind. Expired leases are
// reclaimable, which is how a crashed worker's run is resumed.
func (r *ModelRegistry) ClaimWork(ctx context.Context, kind, worker string, lease time.Duration) (WorkItem, bool, error) {
	item := WorkItem{Kind: kind}
	err := r.db.QueryRowContext(ctx, `UPDATE work_queue q SET lease_owner=$2, lease_expires_at=now() + make_interval(secs => $3), attempts=q.attempts+1
		WHERE q.id = (
			SELECT id FROM work_queue WHERE kind=$1 AND available_at <= now() AND (lease_expires_at IS NULL OR lease_expires_at < now())
			ORDER BY available_at, id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING q.tenant_id::text, q.ref_id::text, q.attempts`, kind, worker, lease.Seconds()).Scan(&item.TenantID, &item.RefID, &item.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	return item, err == nil, err
}

// ExtendLease renews a lease held by this worker; false means it was lost.
func (r *ModelRegistry) ExtendLease(ctx context.Context, kind, refID, worker string, lease time.Duration) (bool, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE work_queue SET lease_expires_at=now() + make_interval(secs => $4)
		WHERE kind=$1 AND ref_id=$2::uuid AND lease_owner=$3`, kind, refID, worker, lease.Seconds())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (r *ModelRegistry) CompleteWork(ctx context.Context, kind, refID, worker string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM work_queue WHERE kind=$1 AND ref_id=$2::uuid AND (lease_owner=$3 OR lease_owner IS NULL)`, kind, refID, worker)
	return err
}

func (r *ModelRegistry) RescheduleWork(ctx context.Context, kind, refID, worker string, delay time.Duration) error {
	_, err := r.db.ExecContext(ctx, `UPDATE work_queue SET available_at=now() + make_interval(secs => $4), lease_owner=NULL, lease_expires_at=NULL
		WHERE kind=$1 AND ref_id=$2::uuid AND lease_owner=$3`, kind, refID, worker, delay.Seconds())
	return err
}

// QueueDepth reports one tenant's scheduling backlog.
func (r *ModelRegistry) QueueDepth(ctx context.Context, tenantID string) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT kind, count(*) FROM work_queue WHERE tenant_id=$1::uuid GROUP BY kind`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	depth := map[string]int{"run": 0, "outbox": 0}
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		depth[kind] = count
	}
	return depth, rows.Err()
}

// FlowcaseRunResult mirrors Flowcase's proposed agent callback run object.
// There is no approval field: Flowcase owns review and gate decisions.
type FlowcaseRunResult struct {
	RunID            string            `json:"run_id"`
	Status           string            `json:"status"`
	AgentID          string            `json:"agent_id"`
	SkillVersion     string            `json:"skill_version"`
	Output           json.RawMessage   `json:"output,omitempty"`
	OutputChecksum   string            `json:"output_checksum,omitempty"`
	Error            string            `json:"error,omitempty"`
	TraceIDs         []string          `json:"trace_ids,omitempty"`
	UsageObservation *UsageObservation `json:"usage_observation,omitempty"`
}

// FlowcaseStatus maps internal states onto Flowcase's run vocabulary.
// needs_review is still a completed draft; review state travels in output.review.
func FlowcaseStatus(status string) string {
	switch status {
	case "needs_review", "succeeded":
		return "succeeded"
	default:
		return status
	}
}

// FlowcaseSource is one evidence source the draft cites, with the title and
// locator reviewers need to recognise it without looking up raw ids.
type FlowcaseSource struct {
	SourceID  string `json:"source_id"`
	VersionID string `json:"version_id"`
	Title     string `json:"title"`
	Citation  string `json:"citation,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
}

// CitedFlowcaseSources keeps the cited refs, de-duplicated and in a stable order
// so the polling and callback paths produce byte-identical output (checksum).
func CitedFlowcaseSources(refs []EvidenceRef) []FlowcaseSource {
	// A document is often cited through several chunks; sorting on the full key first makes the
	// surviving citation/anchor independent of the order the refs were recorded in.
	sorted := append([]EvidenceRef(nil), refs...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.SourceVersion != b.SourceVersion {
			return a.SourceVersion < b.SourceVersion
		}
		if (a.Citation == "") != (b.Citation == "") {
			return b.Citation == "" // prefer a ref that carries a page/section locator
		}
		if a.Citation != b.Citation {
			return a.Citation < b.Citation
		}
		return a.Anchor < b.Anchor
	})
	out := make([]FlowcaseSource, 0, len(sorted))
	seen := map[string]bool{}
	for _, ref := range sorted {
		key := ref.SourceID + "\x00" + ref.SourceVersion
		if !ref.Cited || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, FlowcaseSource{SourceID: ref.SourceID, VersionID: ref.SourceVersion, Title: ref.Title, Citation: ref.Citation, Anchor: ref.Anchor})
	}
	return out
}

// FlowcaseOutput wraps a settled run's draft artifact with its review state,
// risk flags, evidence manifest and the cited sources (with titles).
func FlowcaseOutput(run Run, evidence []EvidenceRef) (json.RawMessage, error) {
	var validation map[string]any
	_ = json.Unmarshal(run.Validation, &validation)
	review := "ready_for_review"
	if run.Status == "needs_review" {
		review = "needs_review"
	}
	envelope := map[string]any{
		"artifact_draft": json.RawMessage(run.Output),
		"review":         map[string]any{"status": review, "validation": validation},
		"pinned":         json.RawMessage(run.Pinned),
		"model_mode":     run.ModelMode,
		"sources":        CitedFlowcaseSources(evidence),
	}
	if len(run.Output) == 0 {
		envelope["artifact_draft"] = nil
	}
	return json.Marshal(envelope)
}

func enqueueFlowcaseResult(ctx context.Context, tx *sql.Tx, tenantID string, run Run) error {
	if run.Kind != "production" || run.CallerKind != "service" {
		return nil
	}
	result := FlowcaseRunResult{RunID: run.ID, Status: FlowcaseStatus(run.Status), AgentID: run.AgentKey, SkillVersion: run.AgentSemver, TraceIDs: []string{run.ID}, UsageObservation: ObserveRunUsage(run)}
	if run.ErrorCode != nil {
		result.Error = *run.ErrorCode
	}
	if run.Status == "succeeded" || run.Status == "needs_review" {
		evidence, err := listRunEvidence(ctx, tx, tenantID, run.ID)
		if err != nil {
			return err
		}
		output, err := FlowcaseOutput(run, evidence)
		if err != nil {
			return err
		}
		checksum, err := OutputChecksum(output)
		if err != nil {
			return err
		}
		result.Output, result.OutputChecksum = output, checksum
	}
	payload, err := json.Marshal(map[string]any{"message_id": run.ID + ":final", "audience": "flowcase", "tenant_id": tenantID, "run": result})
	if err != nil {
		return err
	}
	var outboxID string
	err = tx.QueryRowContext(ctx, `INSERT INTO outbox_events (tenant_id, run_id, topic, message_id, payload_json)
		VALUES ($1::uuid,$2::uuid,'flowcase.run_result',$3,$4::jsonb) ON CONFLICT (message_id) DO NOTHING RETURNING id::text`,
		tenantID, run.ID, run.ID+":final", string(payload)).Scan(&outboxID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO work_queue (kind, tenant_id, ref_id) VALUES ('outbox',$1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, tenantID, outboxID)
	return err
}

type OutboxEvent struct {
	ID        string          `json:"id"`
	RunID     string          `json:"run_id"`
	MessageID string          `json:"message_id"`
	Payload   json.RawMessage `json:"payload"`
	Status    string          `json:"status"`
	Attempts  int             `json:"attempts"`
	LastError *string         `json:"last_error,omitempty"`
}

func (r *ModelRegistry) GetOutboxEvent(ctx context.Context, tenantID, id string) (OutboxEvent, error) {
	var e OutboxEvent
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id::text, run_id::text, message_id, payload_json, status, attempts, last_error
			FROM outbox_events WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, id).
			Scan(&e.ID, &e.RunID, &e.MessageID, &e.Payload, &e.Status, &e.Attempts, &e.LastError)
	})
	return e, err
}

// MarkOutbox records a delivery attempt. dead stops further retries.
func (r *ModelRegistry) MarkOutbox(ctx context.Context, tenantID, id, status, lastError string) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status=$3, attempts=attempts+1, last_error=nullif($4,''),
			delivered_at=CASE WHEN $3='delivered' THEN now() ELSE delivered_at END WHERE tenant_id=$1::uuid AND id=$2::uuid`,
			tenantID, id, status, lastError)
		return err
	})
}

// PurgeRunEvents removes settled trace rows older than the retention window.
func (r *ModelRegistry) PurgeRunEvents(ctx context.Context, tenantID string, olderThan time.Duration) (int64, error) {
	var removed int64
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('agent_studio.retention_purge', 'on', true)`); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM agent_run_events e USING agent_runs r
			WHERE e.tenant_id=$1::uuid AND r.tenant_id=e.tenant_id AND r.id=e.run_id AND r.completed_at IS NOT NULL
			AND e.created_at < now() - make_interval(secs => $2)`, tenantID, olderThan.Seconds())
		if err != nil {
			return err
		}
		removed, _ = res.RowsAffected()
		if removed > 0 {
			return audit(tx, tenantID, "system:retention", "run_events.purged", "tenant:"+tenantID)
		}
		return nil
	})
	return removed, err
}
