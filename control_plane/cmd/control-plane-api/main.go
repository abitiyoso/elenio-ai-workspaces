// Command control-plane-api runs the AI Workspace Control Plane BFF.
//
//	control-plane-api serve              run the HTTP server (default)
//	control-plane-api migrate            apply migrations and exit
//	control-plane-api seed-dev           apply the development seed (APP_ENV=development only)
//	control-plane-api bootstrap-admin -subject <sub> -name <name> [-email e]
//	control-plane-api gen-signing-key    print a new ES256 PEM private key for service tokens
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/elenio/control-plane/internal/apps"
	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/devseed"
	cphttp "github.com/elenio/control-plane/internal/http"
	"github.com/elenio/control-plane/internal/identity/oidc"
	"github.com/elenio/control-plane/internal/identity/provisioning"
	"github.com/elenio/control-plane/internal/identity/session"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/platform/postgres"
	"github.com/elenio/control-plane/internal/platform/secrets"
	"github.com/elenio/control-plane/internal/workspaces"
	"github.com/elenio/control-plane/migrations"
)

func main() {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if cmd == "gen-signing-key" {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		must(err)
		der, err := x509.MarshalPKCS8PrivateKey(k)
		must(err)
		must(pem.Encode(os.Stdout, &pem.Block{Type: "PRIVATE KEY", Bytes: der}))
		return
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration refused:\n"+err.Error())
		os.Exit(2)
	}
	log := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	must(err)
	defer pool.Close()

	if cmd == "migrate" || cfg.MigrateOnStart {
		applied, err := postgres.Migrate(ctx, pool, migrations.FS)
		must(err)
		log.Info("migrations_applied", "count", len(applied), "names", applied)
		if cmd == "migrate" {
			return
		}
	}
	tenantID, err := provisioning.EnsureTenant(ctx, pool, cfg.TenantKey, cfg.TenantName)
	must(err)

	switch cmd {
	case "seed-dev":
		if cfg.Env != config.EnvDevelopment {
			fatal("seed-dev is refused outside APP_ENV=development")
		}
		must(devseed.Apply(ctx, pool, cfg.DevSeedFile, tenantID, cfg.OIDCIssuer, cfg.RegistryEnv))
		log.Info("dev_seed_applied", "file", cfg.DevSeedFile)
		return
	case "bootstrap-admin":
		fs := flag.NewFlagSet("bootstrap-admin", flag.ExitOnError)
		subject := fs.String("subject", "", "IdP subject (sub claim) of the first admin")
		name := fs.String("name", "", "display name")
		email := fs.String("email", "", "email (display only)")
		must(fs.Parse(args))
		if *subject == "" || *name == "" {
			fatal("bootstrap-admin requires -subject and -name")
		}
		must(devseed.BootstrapAdmin(ctx, pool, tenantID, cfg.OIDCIssuer, *subject, *name, *email))
		log.Info("bootstrap_admin_created", "subject", *subject)
		return
	case "serve":
	default:
		fatal("unknown command " + cmd)
	}

	if cfg.DevSeed {
		must(devseed.Apply(ctx, pool, cfg.DevSeedFile, tenantID, cfg.OIDCIssuer, cfg.RegistryEnv))
	}
	must(serve(ctx, cfg, log, pool, tenantID))
}

func serve(ctx context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, tenantID string) error {
	metrics := observability.NewMetrics()

	// Development may run without key files; ephemeral keys are generated and the log says so.
	csrfKey, encKey := cfg.CSRFKey, cfg.TokenEncKey
	if csrfKey == nil || encKey == nil || cfg.SigningKeyPEM == nil {
		log.Warn("ephemeral_keys", "detail", "development: missing key files replaced by per-process random keys")
	}
	if csrfKey == nil {
		csrfKey = secrets.RandomBytes(32)
	}
	if encKey == nil {
		encKey = secrets.RandomBytes(32)
	}
	sealer, err := secrets.NewSealer(encKey)
	if err != nil {
		return err
	}
	tokens, err := integrations.NewIssuer(cfg.Origin(), cfg.ServiceTokenTTL, cfg.SigningKeyPEM, cfg.PreviousJWKS)
	if err != nil {
		return err
	}
	engine, err := authorization.NewEngine(ctx, pool)
	if err != nil {
		return err
	}
	store := &session.Store{Pool: pool, IdleTTL: cfg.SessionIdleTTL, AbsTTL: cfg.SessionAbsTTL, FlowTTL: cfg.AuthFlowTTL, Sealer: sealer, CSRFKey: csrfKey}
	oidcClient := oidc.New(oidc.Config{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCSecret,
		RedirectURL: cfg.OIDCRedirectURL, Scopes: cfg.OIDCScopes, PostLogout: cfg.OIDCPostLogout})
	upstreams := integrations.NewUpstreams(cfg.Upstreams, metrics)

	if err := provisioning.SyncIdentityProvider(ctx, pool, tenantID, cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCSecretRef); err != nil {
		return err
	}
	for _, key := range upstreams.Keys() {
		up, _ := upstreams.Get(key)
		if up.Audience == "" {
			continue
		}
		if err := provisioning.SyncServiceIdentity(ctx, pool, "control-plane-bff:"+key, up.Audience, []string{"app:open"},
			"BFF-minted, audience-bound user delegation token for "+key+"; never a human approver"); err != nil {
			return err
		}
	}
	if err := oidcClient.Ensure(ctx); err != nil {
		log.Warn("idp_discovery_failed_at_start", "error", err.Error()) // fail closed for logins; retried on demand
	}

	wsSvc := &workspaces.Service{Pool: pool, Engine: engine, Sessions: store}
	appSvc := &apps.Service{Pool: pool, Engine: engine, Upstreams: upstreams, Environment: cfg.RegistryEnv}
	ready := func(ctx context.Context) map[string]string {
		checks := map[string]string{"database": "ok", "migrations": "ok", "idp": "ok"}
		if err := pool.Ping(ctx); err != nil {
			checks["database"] = "unavailable"
		}
		if n, err := postgres.Pending(ctx, pool, migrations.FS); err != nil || n > 0 {
			checks["migrations"] = "pending"
		}
		if oidcClient.Status().State != "ok" {
			checks["idp"] = "degraded"
		}
		return checks
	}
	srv := cphttp.New(cphttp.Deps{Config: cfg, Logger: log, Pool: pool, OIDC: oidcClient, Sessions: store, Engine: engine,
		Workspaces: wsSvc, Apps: appSvc, Tokens: tokens, Upstreams: upstreams, Metrics: metrics, TenantID: tenantID, Ready: ready})

	go upstreams.Run(ctx, cfg.HealthInterval)
	go audit.RunOutbox(ctx, pool, audit.LogSink(log), log, 5*time.Second)
	go maintenance(ctx, log, pool, store, oidcClient, metrics, cfg.OIDCIssuer)

	httpSrv := &http.Server{Addr: cfg.HTTPAddress, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10}
	metricsSrv := &http.Server{Addr: cfg.MetricsAddress, Handler: promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}),
		ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics_listener_failed", "error", err.Error())
		}
	}()
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	log.Info("control_plane_started", "address", cfg.HTTPAddress, "env", cfg.Env, "public_origin", cfg.Origin(),
		"tls_only", cfg.CookieSecure, "policy_version", engine.PolicyVersion, "upstreams_configured", len(cfg.Upstreams),
		"token_exchange_clients", len(cfg.TokenExchangeClients))
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = metricsSrv.Shutdown(shutdown)
	return httpSrv.Shutdown(shutdown)
}

// maintenance refreshes IdP status, session gauges and purges expired flows/sessions.
func maintenance(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool, store *session.Store, client *oidc.Client, m *observability.Metrics, issuer string) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		st := client.Probe(ctx)
		if st.State == "ok" {
			m.IdPUp.Set(1)
			_ = client.Ensure(ctx)
		} else {
			m.IdPUp.Set(0)
			log.Warn("idp_unavailable", "detail", st.Detail)
		}
		_ = provisioning.UpdateIdentityProviderStatus(ctx, pool, issuer, st.State, st.Detail, st.Hash)
		if n, err := store.CountActive(ctx); err == nil {
			m.ActiveSession.Set(float64(n))
		}
		if err := store.Purge(ctx); err != nil && ctx.Err() == nil {
			log.Warn("session_purge_failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func must(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "control-plane:", msg)
	os.Exit(1)
}
