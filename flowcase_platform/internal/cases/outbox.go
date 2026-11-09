package cases

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// OutboxEvent is one integration event leaving Flowcase.
type OutboxEvent struct {
	ID, TenantID, Topic, AggregateType, AggregateID string
	Payload                                         json.RawMessage
	Attempts                                        int
}

// Sink delivers events; it must tolerate duplicates (delivery is at-least-once, keyed by event ID).
type Sink interface {
	Deliver(ctx context.Context, ev OutboxEvent) error
}

// LogSink is the Phase 2 sink: no external consumer contract exists yet, so events are logged.
type LogSink struct{ Logger *slog.Logger }

func (s LogSink) Deliver(ctx context.Context, ev OutboxEvent) error {
	s.Logger.InfoContext(ctx, "outbox event", "event_id", ev.ID, "topic", ev.Topic, "aggregate_type", ev.AggregateType, "aggregate_id", ev.AggregateID)
	return nil
}

const maxOutboxAttempts = 8

// DispatchOutbox claims pending events (and ones whose lease expired), delivers them, and marks
// them done, retries with backoff, or dead-letters them after maxOutboxAttempts.
func (e *Engine) DispatchOutbox(ctx context.Context, workerID string, limit int, sink Sink) (int, error) {
	rows, err := e.DB.Query(ctx, `
		UPDATE outbox_events o SET status = 'processing', lease_owner = $1, lease_expires_at = now() + make_interval(secs => $2), attempts = o.attempts + 1
		WHERE o.id IN (
			SELECT id FROM outbox_events
			WHERE (status = 'pending' AND available_at <= now()) OR (status = 'processing' AND lease_expires_at < now())
			ORDER BY seq FOR UPDATE SKIP LOCKED LIMIT $3)
		RETURNING o.id::text, o.tenant_id::text, o.topic, o.aggregate_type, o.aggregate_id, o.payload, o.attempts`, workerID, e.lease().Seconds(), limit)
	if err != nil {
		return 0, err
	}
	evs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OutboxEvent, error) {
		var ev OutboxEvent
		return ev, r.Scan(&ev.ID, &ev.TenantID, &ev.Topic, &ev.AggregateType, &ev.AggregateID, &ev.Payload, &ev.Attempts)
	})
	if err != nil {
		return 0, err
	}
	for _, ev := range evs {
		derr := sink.Deliver(ctx, ev)
		switch {
		case derr == nil:
			_, err = e.DB.Exec(ctx, `UPDATE outbox_events SET status = 'done', processed_at = now(), lease_owner = NULL, lease_expires_at = NULL, last_error = NULL
				WHERE id = $1 AND lease_owner = $2`, ev.ID, workerID)
		case ev.Attempts >= maxOutboxAttempts:
			_, err = e.DB.Exec(ctx, `UPDATE outbox_events SET status = 'dead', lease_owner = NULL, lease_expires_at = NULL, last_error = $3
				WHERE id = $1 AND lease_owner = $2`, ev.ID, workerID, derr.Error())
		default:
			_, err = e.DB.Exec(ctx, `UPDATE outbox_events SET status = 'pending', lease_owner = NULL, lease_expires_at = NULL, last_error = $3,
				available_at = now() + make_interval(secs => $4) WHERE id = $1 AND lease_owner = $2`, ev.ID, workerID, derr.Error(), min(1<<ev.Attempts, 600))
		}
		if err != nil {
			return len(evs), err
		}
	}
	return len(evs), nil
}
