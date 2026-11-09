package cases

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/flowcase/internal/platform/metrics"
)

// RegisterMetrics exposes runtime health gauges computed from PostgreSQL at scrape time.
func (e *Engine) RegisterMetrics(r *metrics.Registry) {
	grouped := func(sql string) func(ctx context.Context) ([]metrics.Sample, error) {
		return func(ctx context.Context) ([]metrics.Sample, error) {
			rows, err := e.DB.Query(ctx, sql)
			if err != nil {
				return nil, err
			}
			return pgx.CollectRows(rows, func(row pgx.CollectableRow) (metrics.Sample, error) {
				var label string
				var n float64
				err := row.Scan(&label, &n)
				return metrics.Sample{Labels: metrics.Labels{"status": label}, Value: n}, err
			})
		}
	}
	single := func(sql string) func(ctx context.Context) ([]metrics.Sample, error) {
		return func(ctx context.Context) ([]metrics.Sample, error) {
			var n float64
			if err := e.DB.QueryRow(ctx, sql).Scan(&n); err != nil {
				return nil, err
			}
			return []metrics.Sample{{Value: n}}, nil
		}
	}
	r.Collect("flowcase_cases", "Cases by status (all tenants).", grouped(`SELECT status, count(*) FROM cases GROUP BY status`))
	r.Collect("flowcase_node_executions", "Open or failed node executions by status.",
		grouped(`SELECT status, count(*) FROM node_executions WHERE status IN ('ready','running','waiting_human','waiting_external','failed') GROUP BY status`))
	r.Collect("flowcase_oldest_ready_execution_seconds", "Age of the oldest due ready execution (queue lag).",
		single(`SELECT coalesce(extract(epoch FROM now() - min(coalesce(next_attempt_at, created_at))), 0) FROM node_executions
			WHERE status = 'ready' AND coalesce(next_attempt_at, created_at) <= now()`))
	r.Collect("flowcase_outbox_events", "Outbox events not yet delivered, by status.",
		grouped(`SELECT status, count(*) FROM outbox_events WHERE status IN ('pending','processing','dead') GROUP BY status`))
	r.Collect("flowcase_agent_runs", "Agent runs by status.", grouped(`SELECT status, count(*) FROM agent_runs GROUP BY status`))
	r.Collect("flowcase_approvals", "Approval requests awaiting a decision, by kind.",
		grouped(`SELECT kind, count(*) FROM approval_requests WHERE status = 'pending' GROUP BY kind`))
	r.Collect("flowcase_stale_artifact_versions", "Current artifact versions marked stale.",
		single(`SELECT count(*) FROM artifacts a JOIN artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version WHERE v.status = 'stale'`))
}
