// Command workspace-api is the Universal Case Workspace BFF: /api/v1/workspace/*, /healthz, /readyz and
// the built SPA (UCW_WEB_DIR).
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

	"github.com/elenio/case-workspace/internal/agentstudio"
	"github.com/elenio/case-workspace/internal/audit"
	"github.com/elenio/case-workspace/internal/auth"
	"github.com/elenio/case-workspace/internal/config"
	"github.com/elenio/case-workspace/internal/controlplane"
	"github.com/elenio/case-workspace/internal/docling"
	"github.com/elenio/case-workspace/internal/flowcase"
	"github.com/elenio/case-workspace/internal/httpapi"
	"github.com/elenio/case-workspace/internal/intake"
	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/internal/store"
	"github.com/elenio/case-workspace/internal/webui"
	"github.com/elenio/case-workspace/internal/workspace"
)

func main() {
	level := slog.LevelInfo
	if os.Getenv("UCW_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", "case-workspace")
	if err := run(log); err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	migrateCtx, cancel := context.WithTimeout(ctx, time.Minute)
	err = st.Migrate(migrateCtx)
	cancel()
	if err != nil {
		return err
	}
	rec := &audit.Recorder{Sink: st, Tenant: cfg.TenantKey, Log: log}
	reg, err := packs.Load(ctx, cfg.PacksDir, st, func(ctx context.Context, action, resource string, meta map[string]any) {
		rec.Record(ctx, "system", action, resource, meta)
	}, log)
	if err != nil {
		return err
	}

	verifier, err := auth.NewVerifier(auth.Config{Issuer: cfg.CPIssuer, JWKSURL: cfg.CPJWKSURL, Audience: cfg.CPAudience, TenantKey: cfg.TenantKey,
		MaxTTL: cfg.CPMaxTokenTTL, ClockSkew: cfg.CPClockSkew})
	if err != nil {
		return err
	}
	exchange, err := controlplane.New(controlplane.Config{BaseURL: cfg.CPInternalURL, ClientID: cfg.CPClientID, ClientSecret: cfg.CPClientSecret})
	if err != nil {
		return err
	}
	fc, err := flowcase.New(cfg.FlowcaseURL, cfg.FlowcaseTimeout, nil)
	if err != nil {
		return err
	}
	var runner agentstudio.Runner = agentstudio.Disabled{}
	if cfg.AgentStudioURL != "" {
		if runner, err = agentstudio.New(cfg.AgentStudioURL, cfg.AgentStudioToken, cfg.AgentStudioTimeout, nil); err != nil {
			return err
		}
	} else {
		log.Warn("UCW_AGENT_STUDIO_URL not set: intake turns answer 503 agent_studio_unavailable")
	}

	var conv docling.Converter
	if cfg.DoclingURL != "" {
		c, err := docling.New(cfg.DoclingURL, cfg.DoclingAPIKey, time.Minute, nil)
		if err != nil {
			return err
		}
		conv = c
	} else {
		log.Warn("UCW_DOCLING_URL not set: intake uploads accept only TXT and MD files")
	}

	ws := &workspace.Service{Exchange: exchange, Flowcase: fc, Packs: reg, Bindings: st, Audit: rec, FlowcaseUI: cfg.FlowcaseUIPath, Log: log}
	in := &intake.Service{DB: st.DB, Runner: runner, Packs: reg, Gate: ws.Gate, Flowcase: fc, Token: ws.FlowcaseToken, Binder: st, Audit: rec,
		Log: log, PendingTimeout: cfg.IntakeTurnTimeout, Docling: conv, DocumentTimeout: cfg.DocumentTimeout}

	var web http.Handler
	if cfg.WebDir != "" {
		h, err := webui.New(cfg.WebDir)
		if err != nil {
			return err
		}
		web = h
	}
	handler := httpapi.New(httpapi.Deps{Log: log, Verifier: verifier, Workspace: ws, Intake: in, Web: web,
		Readiness: []httpapi.Check{{Name: "database", Run: st.Ping}}})

	srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Addr, "packs", len(reg.All()), "web", cfg.WebDir != "")
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}
	return nil
}
