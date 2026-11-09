// Package worker hosts the durable background loop: a ticker plus optional wake-ups, with health
// tracking. What a tick does (node execution, outbox dispatch) is supplied by the caller.
package worker

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type Runner struct {
	Logger   *slog.Logger
	Interval time.Duration
	// Tick runs once per interval, and immediately whenever Wake fires.
	Tick func(ctx context.Context) error
	// Wake (optional) triggers an early tick, e.g. from PostgreSQL LISTEN/NOTIFY.
	Wake <-chan struct{}

	lastOK atomic.Int64 // unix nanos of the last successful tick
}

// Run blocks until ctx is cancelled. A failing tick is logged and retried on the next interval.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	r.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			r.Logger.Info("worker stopping")
			return
		case <-ticker.C:
			r.tick(ctx)
		case <-r.Wake:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	if err := r.Tick(ctx); err != nil {
		if ctx.Err() == nil {
			r.Logger.Warn("worker tick failed", "error", err.Error())
		}
		return
	}
	r.lastOK.Store(time.Now().UnixNano())
}

// Healthy reports whether a tick succeeded within three intervals.
func (r *Runner) Healthy(now time.Time) bool {
	last := r.lastOK.Load()
	return last != 0 && now.Sub(time.Unix(0, last)) <= 3*r.Interval
}
