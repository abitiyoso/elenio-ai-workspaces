package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/elenio/knowledge-workspace/backend/internal/config"
	"github.com/elenio/knowledge-workspace/backend/internal/docling"
	"github.com/elenio/knowledge-workspace/backend/internal/ingestion"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	deps, err := platform.New(cfg, logger)
	if err != nil {
		logger.Error("dependency initialization failed", "error", err)
		os.Exit(1)
	}
	defer deps.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := deps.Database.Migrate(ctx, cfg.MigrationsDir); err != nil {
		logger.Error("database migrations failed", "error", err)
		os.Exit(1)
	}
	converter, err := docling.NewClient(cfg.DoclingBaseURL, cfg.DoclingAPIKey)
	if err != nil {
		logger.Error("Docling integration is not configured", "error", err)
		os.Exit(1)
	}
	worker := ingestion.NewWorker(deps.Database.SQL(), deps.ObjectStore, deps.Search, deps.Embedding, converter, logger)
	logger.Info("kms worker starting", "environment", cfg.Environment)
	if err := worker.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("kms worker stopped", "error", err)
		os.Exit(1)
	}
}
