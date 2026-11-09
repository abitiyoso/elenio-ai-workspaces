// Command api serves the Flowcase REST API.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elenio/flowcase/internal/api"
	"github.com/elenio/flowcase/internal/app"
	"github.com/elenio/flowcase/internal/platform/config"
	"github.com/elenio/flowcase/internal/platform/metrics"
	"github.com/elenio/flowcase/internal/platform/observability"
	"github.com/elenio/flowcase/internal/platform/postgres"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		observability.NewLogger(os.Stderr, 0, "flowcase-api").Error("invalid configuration", "error", err.Error())
		os.Exit(2)
	}
	logger := observability.NewLogger(os.Stdout, cfg.LogLevel, "flowcase-api")

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
	adapter, authChecks, err := app.NewAuth(cfg, pool)
	if err != nil {
		logger.Error("auth initialization failed", "error", err.Error())
		os.Exit(1)
	}
	if adapter.Mode() == config.AuthModeDevMock {
		logger.Warn("DEVELOPMENT MOCK IDENTITY ENABLED: every request is treated as the fixed local developer", "tenant_id", cfg.Auth.DevTenantID)
	}

	reg, _, err := app.NewRegistry(pool, cfg.PacksConfig)
	if err != nil {
		logger.Error("domain packs invalid", "error", err.Error())
		os.Exit(1)
	}

	m := metrics.New(logger)
	engine, err := app.NewEngine(cfg, pool, reg, logger, app.NewConnectors(cfg))
	if err != nil {
		logger.Error("agent runtime initialization failed", "error", err.Error())
		os.Exit(1)
	}
	engine.RegisterMetrics(m)
	if cfg.MetricsAddress != "" {
		ms := &http.Server{Addr: cfg.MetricsAddress, Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := ms.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics listener stopped", "error", err.Error())
			}
		}()
		defer ms.Close()
	}

	handler := api.NewServer(api.Dependencies{
		Logger:      logger,
		Environment: cfg.Environment,
		Auth:        adapter,
		Readiness:   append(app.DatabaseChecks(pool, migrator), authChecks...),
		Knowledge:   app.NewKnowledge(cfg),
		Agents:      engine.Agents,
		Registry:    reg,
		Cases:       engine,
		Metrics:     m,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	logger.Info("flowcase api starting", "address", cfg.HTTPAddress, "environment", cfg.Environment,
		"auth_mode", adapter.Mode(), "kms_mode", cfg.KMSMode, "agent_mode", cfg.AgentMode)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server stopped", "error", err.Error())
		os.Exit(1)
	}
}
