package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunnerTicksUntilCancelledAndTracksHealth(t *testing.T) {
	var calls atomic.Int32
	fail := atomic.Bool{}
	r := &Runner{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval: 10 * time.Millisecond,
		Tick: func(context.Context) error {
			calls.Add(1)
			if fail.Load() {
				return errors.New("db down")
			}
			return nil
		},
	}
	if r.Healthy(time.Now()) {
		t.Fatal("runner must not be healthy before its first tick")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	time.Sleep(35 * time.Millisecond)
	if !r.Healthy(time.Now()) {
		t.Fatal("runner should be healthy after successful ticks")
	}
	fail.Store(true)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
	if calls.Load() < 2 {
		t.Fatalf("expected repeated ticks, got %d", calls.Load())
	}
	if r.Healthy(time.Now().Add(time.Second)) {
		t.Fatal("runner must become unhealthy when ticks stop succeeding")
	}
}
