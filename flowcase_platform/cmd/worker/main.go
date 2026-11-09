// Command worker executes ready workflow steps and dispatches the transactional outbox.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elenio/flowcase/internal/app"
	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/platform/config"
	"github.com/elenio/flowcase/internal/platform/metrics"
	"github.com/elenio/flowcase/internal/platform/observability"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/worker"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		observability.NewLogger(os.Stderr, 0, "flowcase-worker").Error("invalid configuration", "error", err.Error())
		os.Exit(2)
	}
	logger := observability.NewLogger(os.Stdout, cfg.LogLevel, "flowcase-worker")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		logger.Error("database initialization failed", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()
	migrator, err := app.NewMigrator(pool)
	if err != nil {
		logger.Error("migrations unreadable", "error", err.Error())
		os.Exit(1)
	}

	reg, _, err := app.NewRegistry(pool, cfg.PacksConfig)
	if err != nil {
		logger.Error("domain packs invalid", "error", err.Error())
		os.Exit(1)
	}
	engine, err := app.NewEngine(cfg, pool, reg, logger, app.NewConnectors(cfg))
	if err != nil {
		logger.Error("agent runtime initialization failed", "error", err.Error())
		os.Exit(1)
	}
	m := metrics.New(logger)
	engine.RegisterMetrics(m)
	host, _ := os.Hostname()
	workerID := fmt.Sprintf("%s-%d-%s", host, os.Getpid(), observability.NewID()[:8])
	sink := cases.LogSink{Logger: logger}

	var lastReconcile time.Time
	wake := make(chan struct{}, 1)
	go worker.Listen(ctx, pool, cases.NotifyChannel, wake, logger)
	runner := &worker.Runner{
		Logger:   logger,
		Interval: 2 * time.Second,
		Wake:     wake,
		Tick: func(ctx context.Context) error {
			if err := migrator.Check(ctx); err != nil {
				return err
			}
			for range 20 { // bounded so one busy tenant cannot starve the health tick
				n, err := engine.ProcessReady(ctx, workerID, 25)
				if err != nil {
					m.Add("flowcase_worker_errors_total", "Worker tick errors.", nil, 1)
					return err
				}
				m.Add("flowcase_worker_claimed_total", "Node executions claimed by this worker.", nil, float64(n))
				if n == 0 {
					break
				}
			}
			if time.Since(lastReconcile) > time.Minute {
				if _, err := engine.ReconcileEvidence(ctx, 10*time.Minute, 50); err != nil {
					return err
				}
				lastReconcile = time.Now()
			}
			n, err := engine.DispatchOutbox(ctx, workerID, 100, sink)
			m.Add("flowcase_outbox_dispatched_total", "Outbox events claimed for delivery.", nil, float64(n))
			m.Add("flowcase_worker_ticks_total", "Worker ticks completed.", nil, 1)
			return err
		},
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if runner.Healthy(time.Now()) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	probe := &http.Server{Addr: cfg.WorkerHealthAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := probe.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker probe server stopped", "error", err.Error())
			stop()
		}
	}()

	logger.Info("flowcase worker starting", "probe_address", cfg.WorkerHealthAddress, "environment", cfg.Environment, "worker_id", workerID)
	runner.Run(ctx)

	shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = probe.Shutdown(shutdown)
}
