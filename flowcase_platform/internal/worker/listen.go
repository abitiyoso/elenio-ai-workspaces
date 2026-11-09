package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Listen forwards PostgreSQL notifications on channel to wake (non-blocking, coalesced) until ctx
// ends, reconnecting after errors. Notifications are an optimisation: the ticker still guarantees progress.
func Listen(ctx context.Context, pool *pgxpool.Pool, channel string, wake chan<- struct{}, logger *slog.Logger) {
	for ctx.Err() == nil {
		if err := listenOnce(ctx, pool, channel, wake); err != nil && ctx.Err() == nil {
			logger.Warn("notification listener restarting", "error", err.Error())
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, channel string, wake chan<- struct{}) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return err
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
