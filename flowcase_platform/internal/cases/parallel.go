package cases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/flowcase/internal/audit"
	"github.com/elenio/flowcase/internal/platform/observability"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

// runtimeGaps lists steps this deployment cannot execute (no agent runtime, connector missing).
func (e *Engine) runtimeGaps(p *pinned) []definition.Diagnostic {
	var out []definition.Diagnostic
	for i, n := range p.def.Nodes {
		path := fmt.Sprintf("/nodes/%d", i)
		switch n.Type {
		case definition.AgentTask:
			if e.Agents == nil || e.Agents.Provider() == "disabled" {
				out = append(out, definition.Errorf(path, n.ID, "agent_runtime_unavailable", "no AI agent runtime is configured for %s", n.ID))
			}
		case definition.ServiceTask:
			if e.Connectors == nil {
				out = append(out, definition.Errorf(path, n.ID, "connector_unavailable", "no connectors are configured"))
			} else if _, ok := e.Connectors.Get(n.ConfigString("connector_ref")); !ok {
				out = append(out, definition.Errorf(path, n.ID, "connector_unavailable", "connector %s is not available in this runtime", n.ConfigString("connector_ref")))
			}
		}
	}
	return out
}

// split completes the split and starts one branch token per outgoing edge. Tokens are keyed by the
// split execution, so a split re-run after rework gets fresh branches.
func (e *Engine) split(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, node *definition.Node) error {
	if err := completeExec(ctx, tx, x.ID, map[string]any{"branches": node.Next}); err != nil {
		return err
	}
	for _, branch := range node.Next {
		var tokenID string
		err := tx.QueryRow(ctx, `INSERT INTO execution_tokens (tenant_id, case_id, parent_token_id, split_node_id, split_execution_id, branch_key, current_node_id)
			VALUES ($1, $2, $3, $4, $5, $6, $6)
			ON CONFLICT (split_execution_id, branch_key) WHERE split_execution_id IS NOT NULL DO NOTHING RETURNING id::text`,
			c.TenantID, c.ID, x.TokenID, node.ID, x.ID, branch).Scan(&tokenID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT id::text FROM execution_tokens WHERE split_execution_id = $1 AND branch_key = $2`, x.ID, branch).Scan(&tokenID)
		}
		if err != nil {
			return err
		}
		if err := e.scheduleIn(ctx, tx, c, tokenID, x.ID, x.ID+"#"+branch, []string{branch}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE execution_tokens SET status = 'waiting', current_node_id = $2, updated_at = now() WHERE id = $1`, x.TokenID, node.ID); err != nil {
		return err
	}
	return audit.Record(ctx, tx, systemAudit(ctx, c, "parallel.split", "node", node.ID, map[string]any{"branches": node.Next}))
}

// join records a branch arrival. When every branch token of the split execution has arrived, the
// successor is scheduled in the parent token exactly once (keyed by the split execution), however
// the arrivals are ordered or redelivered.
func (e *Engine) join(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, node *definition.Node) error {
	var parentID, splitExecID *string
	if err := tx.QueryRow(ctx, `SELECT parent_token_id::text, split_execution_id::text FROM execution_tokens WHERE id = $1`, x.TokenID).Scan(&parentID, &splitExecID); err != nil {
		return err
	}
	if parentID == nil || splitExecID == nil {
		return fmt.Errorf("%w: join %s reached outside a parallel branch", errPermanent, node.ID)
	}
	if err := completeExec(ctx, tx, x.ID, map[string]any{"arrived": true}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE execution_tokens SET status = 'joined', current_node_id = $2, updated_at = now() WHERE id = $1`, x.TokenID, node.ID); err != nil {
		return err
	}
	var joined, total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'joined'), count(*) FROM execution_tokens WHERE split_execution_id = $1`,
		*splitExecID).Scan(&joined, &total); err != nil {
		return err
	}
	if joined < total {
		return audit.Record(ctx, tx, systemAudit(ctx, c, "parallel.branch_arrived", "node", node.ID, map[string]any{"arrived": joined, "expected": total}))
	}
	if _, err := tx.Exec(ctx, `UPDATE execution_tokens SET status = 'active', current_node_id = $2, updated_at = now() WHERE id = $1`, *parentID, node.ID); err != nil {
		return err
	}
	if err := e.scheduleIn(ctx, tx, c, *parentID, x.ID, "join:"+*splitExecID, node.Next); err != nil {
		return err
	}
	return audit.Record(ctx, tx, systemAudit(ctx, c, "parallel.joined", "node", node.ID, map[string]any{"branches": total}))
}

// timer persists its wake-up time; the worker claims it when due (surviving restarts) and fires
// once. Timers only continue the flow or signal escalation; they never decide anything.
func (e *Engine) timer(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, node *definition.Node) error {
	if x.WakeAt == nil {
		wake := time.Now()
		if d := dueAt(&definition.Node{Config: map[string]any{"due_in": node.Config["duration"]}}); d != nil {
			wake = *d
		} else if f := node.ConfigString("wait_until_field"); f != "" {
			raw, _ := lookupField(decodeVars(c.Variables), f).(string)
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return fmt.Errorf("%w: %s is not an RFC 3339 date-time", errPermanent, f)
			}
			wake = t
		}
		if _, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'waiting_external', wake_at = $2, lease_owner = NULL, lease_expires_at = NULL,
			updated_at = now() WHERE id = $1`, x.ID, wake); err != nil {
			return err
		}
		return audit.Record(ctx, tx, systemAudit(ctx, c, "timer.scheduled", "node", node.ID, map[string]any{"wake_at": wake.UTC().Format(time.RFC3339)}))
	}
	if err := completeExec(ctx, tx, x.ID, map[string]any{"fired_at": time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	data := map[string]any{"escalate_to_role": node.ConfigString("escalate_to_role")}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "timer.fired", "node", node.ID, data)); err != nil {
		return err
	}
	if err := outbox(ctx, tx, c, "timer.fired", "node_execution", x.ID, data); err != nil {
		return err
	}
	return e.schedule(ctx, tx, c, x, node.Next)
}

// serviceTask calls an allowlisted connector with the execution id as idempotency key, so a retry
// after a crash between the call and the commit cannot apply the side effect twice.
func (e *Engine) serviceTask(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, node *definition.Node) error {
	ref, op := node.ConfigString("connector_ref"), node.ConfigString("operation")
	if e.Connectors == nil {
		return fmt.Errorf("%w: no connectors configured", errPermanent)
	}
	conn, ok := e.Connectors.Get(ref)
	if !ok {
		return fmt.Errorf("%w: connector %s unavailable", errPermanent, ref)
	}
	params, _ := node.Config["params"].(map[string]any)
	out, err := conn.Invoke(ctx, connectorCall(c, ref, op, x.ID, params))
	if err != nil {
		return fmt.Errorf("connector %s.%s: %w", ref, op, err)
	}
	if e.FailAfterSideEffect != nil {
		if err := e.FailAfterSideEffect(ExecInfo{CaseID: c.ID, NodeID: x.NodeID, NodeType: node.Type, Attempt: x.Attempt}); err != nil {
			return err
		}
	}
	redacted := map[string]any{}
	for k, v := range params {
		if observability.IsSensitiveKey(k) {
			v = observability.Redacted
		}
		redacted[k] = v
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tool_invocations (tenant_id, case_id, node_execution_id, connector_ref, operation, idempotency_key, params_redacted, output)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (idempotency_key) DO NOTHING`,
		c.TenantID, c.ID, x.ID, ref, op, x.ID, RawJSON(redacted), RawJSON(out)); err != nil {
		return err
	}
	if err := completeExec(ctx, tx, x.ID, map[string]any{"connector": ref, "operation": op}); err != nil {
		return err
	}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "service.invoked", "node", node.ID, map[string]any{"connector": ref, "operation": op})); err != nil {
		return err
	}
	return e.schedule(ctx, tx, c, x, node.Next)
}
