// Package events writes integration events to the transactional outbox. The worker (Phase 2)
// delivers them at-least-once; the dedupe key makes re-enqueueing the same fact a no-op.
package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Outbox struct {
	TenantID      string
	Topic         string
	AggregateType string
	AggregateID   string
	DedupeKey     string
	Payload       map[string]any
	TraceID       string
}

func Enqueue(ctx context.Context, tx pgx.Tx, e Outbox) error {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events (tenant_id, topic, aggregate_type, aggregate_id, dedupe_key, payload, trace_id)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')) ON CONFLICT (dedupe_key) DO NOTHING`,
		e.TenantID, e.Topic, e.AggregateType, e.AggregateID, e.DedupeKey, payload, e.TraceID)
	if err != nil {
		return fmt.Errorf("enqueue %s: %w", e.Topic, err)
	}
	return nil
}
