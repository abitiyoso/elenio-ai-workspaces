package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sink delivers one outbox event. The only sink configured today is the structured log; a message bus or
// webhook sink can replace it without changing producers.
type Sink func(ctx context.Context, eventType string, payload []byte) error

func LogSink(logger *slog.Logger) Sink {
	return func(_ context.Context, eventType string, payload []byte) error {
		logger.Info("outbox_event", "event_type", eventType, "payload", string(payload))
		return nil
	}
}

// RunOutbox claims pending events with SKIP LOCKED (safe across instances) and delivers them with backoff.
func RunOutbox(ctx context.Context, pool *pgxpool.Pool, sink Sink, logger *slog.Logger, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := deliverBatch(ctx, pool, sink); err != nil && ctx.Err() == nil {
			logger.Warn("outbox_delivery_failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func deliverBatch(ctx context.Context, pool *pgxpool.Pool, sink Sink) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck
	rows, err := tx.Query(ctx, `SELECT id::text, event_type, payload::text FROM outbox_events
WHERE status = 'pending' AND retry_at <= now() ORDER BY created_at LIMIT 50 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	type ev struct{ id, typ, payload string }
	var batch []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.id, &e.typ, &e.payload); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, e)
	}
	rows.Close()
	for _, e := range batch {
		if err := sink(ctx, e.typ, []byte(e.payload)); err != nil {
			_, _ = tx.Exec(ctx, `UPDATE outbox_events SET attempts = attempts + 1, last_error = left($2, 500),
retry_at = now() + make_interval(secs => least(3600, 5 * power(2, attempts))),
status = CASE WHEN attempts + 1 >= 12 THEN 'failed' ELSE 'pending' END WHERE id = $1::uuid`, e.id, err.Error())
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox_events SET status = 'delivered', delivered_at = now(), attempts = attempts + 1 WHERE id = $1::uuid`, e.id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
