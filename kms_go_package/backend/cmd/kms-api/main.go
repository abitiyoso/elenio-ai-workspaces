package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/config"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
	"github.com/elenio/knowledge-workspace/backend/internal/transport"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
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
	if err := deps.Database.Migrate(context.Background(), cfg.MigrationsDir); err != nil {
		logger.Error("database migrations failed", "error", err)
		os.Exit(1)
	}

	var adapter auth.AuthAdapter
	switch cfg.AuthMode {
	case config.AuthModeMock:
		adapter = auth.NewDevelopmentMock(cfg.MockUserID, cfg.MockTenantID)
	case config.AuthModeControlPlaneJWT:
		cp := cfg.ControlPlaneAuth
		verifier, err := auth.NewControlPlaneVerifier(auth.ControlPlaneConfig{
			Issuer: cp.Issuer, Audience: cp.Audience, JWKSURL: cp.JWKSURL, TenantMap: cp.TenantMap,
			MaxTokenTTL: cp.MaxTokenTTL, ClockSkew: cp.ClockSkew, JWKSCacheTTL: cp.JWKSCacheTTL,
			Directory: auth.NewSQLPrincipalDirectory(deps.Database.SQL()), Logger: logger,
		})
		if err != nil {
			logger.Error("control plane token verifier initialization failed", "error", err)
			os.Exit(1)
		}
		logger.Info("control plane token verification enabled", "issuer", cp.Issuer, "audience", cp.Audience, "jwks_url", cp.JWKSURL, "mapped_tenants", len(cp.TenantMap))
		adapter = auth.NewTrustedSessionAdapter(verifier)
	default:
		adapter = auth.NewTrustedSessionAdapter(auth.TrustedSessionVerifierFunc(func(context.Context, *http.Request) (auth.Identity, error) {
			return auth.Identity{}, auth.ErrVerifierNotConfigured
		}))
	}

	server := &http.Server{
		Addr: cfg.HTTPAddress,
		Handler: transport.NewServer(transport.Dependencies{
			Logger: logger, Database: deps.Database, Search: deps.Search, ObjectStore: deps.ObjectStore, Embedding: deps.Embedding, MaxUploadBytes: cfg.MaxUploadBytes, Auth: adapter, SelfApprovalExempt: cfg.SelfApprovalExempt,
			Operations: transport.OperationsConfig{Environment: cfg.Environment, AuthMode: cfg.AuthMode, DoclingBaseURL: cfg.DoclingBaseURL, MetricsToken: cfg.MetricsToken},
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	logger.Info("kms api starting", "address", cfg.HTTPAddress, "environment", cfg.Environment)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}
