package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/elenio/agent-studio/internal/auth"
	"github.com/elenio/agent-studio/internal/httpapi"
	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
	agentruntime "github.com/elenio/agent-studio/internal/runtime"
	"github.com/elenio/agent-studio/internal/vault"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const expectedSchemaVersion = "000006_run_usage"

func main() {
	if err := run(); err != nil {
		log.Printf("agent-studio stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	address := envOr("AGENT_STUDIO_HTTP_ADDR", "127.0.0.1:8190")
	databaseURL := strings.TrimSpace(os.Getenv("AGENT_STUDIO_DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("AGENT_STUDIO_DATABASE_URL is required")
	}
	databaseURL, err := addDatabasePassword(databaseURL, os.Getenv("AGENT_STUDIO_DATABASE_PASSWORD_FILE"))
	if err != nil {
		return err
	}
	environment, err := auth.Environment(os.Getenv("AGENT_STUDIO_ENV"))
	if err != nil {
		return err
	}
	human, err := buildHumanIdentity(os.Getenv, environment)
	if err != nil {
		return err
	}
	_, devIdentity := human.(*auth.DevIdentity)
	if err := requireBindAddress(address, devIdentity); err != nil {
		return err
	}
	serviceTokens, err := auth.LoadServiceTokens(os.Getenv("AGENT_STUDIO_SERVICE_TOKENS_FILE"))
	if err != nil {
		return err
	}
	identity := auth.Composite{Human: human, Service: serviceTokens}
	webDir := envOr("AGENT_STUDIO_WEB_DIR", "/app/web")
	if info, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil || info.IsDir() {
		return fmt.Errorf("frontend build is unavailable at %s", webDir)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open Agent Studio database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(startupCtx); err != nil {
		return fmt.Errorf("Agent Studio database is not ready: %w", err)
	}
	if err := checkSchema(startupCtx, db); err != nil {
		return err
	}
	registry, err := postgres.NewModelRegistry(db)
	if err != nil {
		return err
	}
	exempt, err := selfApprovalExemptSubjects(os.Getenv("AGENT_STUDIO_SELF_APPROVAL_EXEMPT_SUBJECTS"), environment)
	if err != nil {
		return err
	}
	if len(exempt) > 0 {
		registry.SetSelfApprovalExempt(exempt)
		log.Printf("WARNING: release separation of duties is waived for %d development subject(s)", len(exempt))
	}
	vaultDir := strings.TrimSpace(os.Getenv("AGENT_STUDIO_VAULT_DIR"))
	if vaultDir == "" {
		return errors.New("AGENT_STUDIO_VAULT_DIR is required")
	}
	credentialVault, err := vault.NewFileVault(vaultDir, os.Getenv("AGENT_STUDIO_VAULT_MASTER_KEY_FILE"))
	if err != nil {
		return fmt.Errorf("initialize credential vault: %w", err)
	}
	var endpointPolicy httpapi.ProviderEndpointPolicy
	if rawHosts := strings.TrimSpace(os.Getenv("AGENT_STUDIO_PROVIDER_HTTPS_HOSTS")); rawHosts != "" {
		endpointPolicy, err = httpapi.NewProviderEndpointPolicy(strings.Split(rawHosts, ","))
		if err != nil {
			return fmt.Errorf("configure provider endpoint policy: %w", err)
		}
	}

	mux := newRouter(registry, identity, credentialVault, endpointPolicy, db.PingContext, webDir)

	runtimeConfig, err := loadRuntimeConfig(environment)
	if err != nil {
		return err
	}
	runtimeConfig.status.ServiceClients = serviceTokens.ClientCount()
	mux.Handle("/api/v1/agents/bootstrap/sim/configure", httpapi.SIMPackHandler{Store: registry, Auth: identity, Authorizer: identity})
	httpapi.RegisterRuntimeRoutes(mux, httpapi.RuntimeHandler{
		Store: registry, Auth: identity, Authorizer: identity, AllowDevMock: runtimeConfig.allowDevMock,
		ReleaseEnvironment: runtimeConfig.releaseEnvironment, Status: runtimeConfig.status,
	})
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	workerDone := make(chan struct{})
	if runtimeConfig.workers > 0 {
		hostname, _ := os.Hostname()
		worker := &agentruntime.Worker{
			Store: registry,
			Executor: &agentruntime.Executor{Vault: credentialVault, Egress: endpointPolicy, KMS: runtimeConfig.kms,
				AllowDevMock: runtimeConfig.allowDevMock},
			Dispatcher: runtimeConfig.flowcase, ID: fmt.Sprintf("%s-%d", hostname, os.Getpid()),
			Concurrency: runtimeConfig.workers, Lease: 30 * time.Second, Retention: runtimeConfig.retention,
			Logger: log.Default(),
		}
		go func() {
			defer close(workerDone)
			worker.Run(workerCtx)
		}()
		log.Printf("Agent runtime worker started (concurrency %d, dev mock model %v)", runtimeConfig.workers, runtimeConfig.allowDevMock)
	} else {
		close(workerDone)
	}

	server := &http.Server{
		Addr:              address,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("Agent Studio listening on %s", address)
		serveErr <- server.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve Agent Studio: %w", err)
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopWorker()
		<-workerDone
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown Agent Studio: %w", err)
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve Agent Studio: %w", err)
		}
		return nil
	}
}

// buildHumanIdentity selects the human adapter from AGENT_STUDIO_HUMAN_AUTH.
// The default (tailscale) is the Tailscale Serve header identity, optionally
// replaced by the development login; control_plane_jwt verifies the delegation
// token injected by the Control Plane BFF. Both resolve tenants and actions
// through the same access-map file.
func buildHumanIdentity(getenv func(string) string, environment string) (auth.HumanIdentity, error) {
	mode, err := auth.HumanAuthMode(getenv("AGENT_STUDIO_HUMAN_AUTH"))
	if err != nil {
		return nil, err
	}
	devLogin := strings.TrimSpace(getenv("AGENT_STUDIO_DEV_LOGIN"))
	accessMapPath := getenv("AGENT_STUDIO_ACCESS_MAP_FILE")
	if mode == auth.HumanAuthControlPlaneJWT {
		if devLogin != "" {
			return nil, errors.New("AGENT_STUDIO_DEV_LOGIN cannot be combined with AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt")
		}
		cfg, err := auth.ControlPlaneConfigFromEnv(getenv, environment)
		if err != nil {
			return nil, err
		}
		cp, err := auth.LoadControlPlaneJWT(cfg, accessMapPath)
		if err != nil {
			return nil, err
		}
		log.Printf("Human auth mode: %s (issuer %s, audience %s, JWKS %s, max token TTL %s); identity headers are ignored",
			mode, cfg.Issuer, cfg.Audience, cfg.JWKSURL, cfg.MaxTokenTTL)
		return cp, nil
	}
	access, err := auth.LoadAccessMap(accessMapPath)
	if err != nil {
		return nil, err
	}
	var human auth.HumanIdentity = access
	if devLogin != "" {
		// NewDevIdentity refuses anything but AGENT_STUDIO_ENV=development.
		dev, err := auth.NewDevIdentity(environment, access, devLogin)
		if err != nil {
			return nil, err
		}
		human = dev
		log.Printf("DEVELOPMENT identity enabled: every request is treated as %s", devLogin)
	}
	log.Printf("Human auth mode: %s", mode)
	return human, nil
}

type identityAdapter interface {
	ports.AuthAdapter
	ports.Authorizer
}

func newRouter(registry *postgres.ModelRegistry, identity identityAdapter, credentialVault httpapi.ScopedCredentialVault, endpointPolicy httpapi.ProviderEndpointPolicy, ready func(context.Context) error, webDir string) *http.ServeMux {
	mux := http.NewServeMux()
	registryHandler := httpapi.ModelRegistryHandler{Registry: registry, Auth: identity, Authorizer: identity}
	mux.Handle("/api/v1/", registryHandler)
	// Exact collection routes keep ServeMux from redirecting them into the
	// model-security subtrees registered below.
	mux.Handle("/api/v1/model-providers", registryHandler)
	mux.Handle("/api/v1/model-deployments", registryHandler)
	var exempt func(string) bool
	if registry != nil {
		exempt = registry.IsSelfApprovalExempt
	}
	mux.Handle("/api/v1/session", sessionHandler(identity, exempt))
	httpapi.RegisterCatalogRoutes(mux, registry, identity, identity)
	httpapi.RegisterToolDraftRoutes(mux, registry, identity, identity)
	httpapi.RegisterModelSecurityRoutes(mux, registry, credentialVault, identity, identity, endpointPolicy, httpapi.NewOpenAICompatibleProbe())
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.Handle("/", spaHandler(webDir))
	return mux
}

type runtimeConfig struct {
	workers            int
	allowDevMock       bool
	releaseEnvironment string
	retention          time.Duration
	kms                agentruntime.EvidenceSource
	flowcase           *agentruntime.FlowcaseDispatcher
	status             httpapi.IntegrationStatus
}

// loadRuntimeConfig reads worker, KMS and Flowcase settings. Every external
// destination is operator configuration; nothing is taken from requests.
func loadRuntimeConfig(environment string) (runtimeConfig, error) {
	cfg := runtimeConfig{workers: 2, allowDevMock: environment == auth.EnvironmentDevelopment, releaseEnvironment: "production", retention: 30 * 24 * time.Hour}
	if raw := strings.TrimSpace(os.Getenv("AGENT_STUDIO_DEV_MOCK_MODEL")); raw != "" {
		allow, err := strconv.ParseBool(raw)
		if err != nil || (allow && environment != auth.EnvironmentDevelopment) {
			return cfg, errors.New("AGENT_STUDIO_DEV_MOCK_MODEL must be a boolean; true requires development")
		}
		cfg.allowDevMock = allow
	}
	if raw := strings.TrimSpace(os.Getenv("AGENT_STUDIO_WORKER_CONCURRENCY")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 16 {
			return cfg, errors.New("AGENT_STUDIO_WORKER_CONCURRENCY must be 0-16")
		}
		cfg.workers = n
	}
	if raw := strings.TrimSpace(os.Getenv("AGENT_STUDIO_RELEASE_ENVIRONMENT")); raw != "" {
		if raw != "staging" && raw != "production" {
			return cfg, errors.New("AGENT_STUDIO_RELEASE_ENVIRONMENT must be staging or production")
		}
		cfg.releaseEnvironment = raw
	}
	if raw := strings.TrimSpace(os.Getenv("AGENT_STUDIO_TRACE_RETENTION_DAYS")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 1 || days > 3650 {
			return cfg, errors.New("AGENT_STUDIO_TRACE_RETENTION_DAYS must be 1-3650")
		}
		cfg.retention = time.Duration(days) * 24 * time.Hour
	}
	kmsMode := "not_configured"
	if base := strings.TrimSpace(os.Getenv("AGENT_STUDIO_KMS_BASE_URL")); base != "" {
		if err := requireServiceURL(base); err != nil {
			return cfg, fmt.Errorf("AGENT_STUDIO_KMS_BASE_URL: %w", err)
		}
		token, err := readSecretFile(os.Getenv("AGENT_STUDIO_KMS_SERVICE_TOKEN_FILE"))
		if err != nil {
			return cfg, fmt.Errorf("KMS service token: %w", err)
		}
		userHeader, err := kmsUserHeader(os.Getenv("AGENT_STUDIO_KMS_USER_HEADER"))
		if err != nil {
			return cfg, fmt.Errorf("AGENT_STUDIO_KMS_USER_HEADER: %w", err)
		}
		forwardCaseID := false
		if raw := strings.TrimSpace(os.Getenv("AGENT_STUDIO_KMS_FORWARD_CASE_ID")); raw != "" {
			if forwardCaseID, err = strconv.ParseBool(raw); err != nil {
				return cfg, errors.New("AGENT_STUDIO_KMS_FORWARD_CASE_ID must be true or false")
			}
		}
		cfg.kms = agentruntime.KMSRetrieval{BaseURL: base, ServiceToken: string(token), UserHeader: userHeader, ForwardCaseID: forwardCaseID,
			Client: &http.Client{Timeout: 15 * time.Second}}
		kmsMode = "configured_requires_delegated_user"
	}
	if callback := strings.TrimSpace(os.Getenv("AGENT_STUDIO_FLOWCASE_CALLBACK_URL")); callback != "" {
		if err := requireServiceURL(callback); err != nil {
			return cfg, fmt.Errorf("AGENT_STUDIO_FLOWCASE_CALLBACK_URL: %w", err)
		}
		secret, err := readSecretFile(os.Getenv("AGENT_STUDIO_FLOWCASE_CALLBACK_SECRET_FILE"))
		if err != nil || len(secret) < 32 {
			return cfg, errors.New("AGENT_STUDIO_FLOWCASE_CALLBACK_SECRET_FILE must hold at least 32 bytes")
		}
		cfg.flowcase = &agentruntime.FlowcaseDispatcher{CallbackURL: callback, Secret: secret, Client: &http.Client{Timeout: 10 * time.Second}}
	}
	cfg.status = httpapi.IntegrationStatus{
		WorkerEnabled: cfg.workers > 0, DevMockModel: cfg.allowDevMock, KMSMode: kmsMode, FlowcaseCallback: cfg.flowcase.Configured(),
		ReleaseEnvironment: cfg.releaseEnvironment, TraceRetentionDays: int(cfg.retention.Hours() / 24),
		EinoVersion: "github.com/cloudwego/eino v0.9.22", ProviderAdapterFamily: "openai_compatible (eino-ext openai v0.1.13)",
	}
	return cfg, nil
}

var headerNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

var subjectUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// selfApprovalExemptSubjects parses AGENT_STUDIO_SELF_APPROVAL_EXEMPT_SUBJECTS:
// comma-separated Control Plane user ids that may take more than one step of
// their own release. It exists for development walkthroughs with a single
// all-access account and is refused in any other environment.
func selfApprovalExemptSubjects(raw, environment string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if environment != auth.EnvironmentDevelopment {
		return nil, errors.New("AGENT_STUDIO_SELF_APPROVAL_EXEMPT_SUBJECTS is only allowed with AGENT_STUDIO_ENV=development")
	}
	var subjects []string
	for _, part := range strings.Split(raw, ",") {
		subject := strings.TrimSpace(part)
		if !subjectUUIDPattern.MatchString(subject) {
			return nil, fmt.Errorf("AGENT_STUDIO_SELF_APPROVAL_EXEMPT_SUBJECTS: %q is not a lower-case user UUID", subject)
		}
		subjects = append(subjects, subject)
	}
	return subjects, nil
}

// kmsUserHeader validates the header that carries the delegated user to KMS.
// Authorization is refused: setting it would replace the KMS service token.
func kmsUserHeader(raw string) (string, error) {
	header := strings.TrimSpace(raw)
	if header == "" {
		// Retrieval stays fail-closed (evidence unavailable) without a header.
		return "", nil
	}
	if !headerNamePattern.MatchString(header) {
		return "", errors.New("must be an HTTP header name")
	}
	if canonical := http.CanonicalHeaderKey(header); canonical == "Authorization" || canonical == "Proxy-Authorization" {
		return "", errors.New("must not be an authorization header; it would replace the KMS service credential")
	}
	return header, nil
}

// requireServiceURL allows HTTPS anywhere, and plain HTTP only to loopback
// services on the same host (KMS and Flowcase on the VM).
func requireServiceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("must be an absolute URL without credentials, query or fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	if ip := net.ParseIP(u.Hostname()); u.Scheme == "http" && ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("must use HTTPS, or HTTP to a loopback address")
}

func readSecretFile(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("secret file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = []byte(strings.TrimRight(string(data), "\r\n"))
	if len(data) == 0 {
		return nil, errors.New("secret file is empty")
	}
	return data, nil
}

// sessionHandler reports the verified caller so the UI can label a
// development identity; it never accepts identity from the request body.
// sessionHandler reports who is signed in and what they may do; exempt (may be
// nil) tells the UI that release separation of duties is waived for them.
func sessionHandler(identity identityAdapter, exempt func(string) bool) http.Handler {
	mode := "tailscale-serve"
	if named, ok := identity.(interface{ IdentityMode() string }); ok {
		mode = named.IdentityMode()
	} else if dev, ok := identity.(interface{ IsDevelopment() bool }); ok && dev.IsDevelopment() {
		mode = "development"
	} else if _, ok := identity.(*auth.DevIdentity); ok {
		mode = "development"
	} else if _, ok := identity.(*auth.ControlPlaneJWT); ok {
		mode = "control-plane"
	}
	lister, _ := identity.(interface {
		PermissionsFor(ports.Principal) []string
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
			return
		}
		principal, err := identity.VerifyRequest(r.Context(), r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "verified tenant identity is required"})
			return
		}
		permissions := []string{}
		if lister != nil {
			permissions = lister.PermissionsFor(principal)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"subject": principal.Subject, "issuer": principal.Issuer, "tenant_id": principal.TenantID, "identity_mode": mode, "permissions": permissions,
			"self_approval_exempt": exempt != nil && exempt(principal.Subject),
		})
	})
}

func addDatabasePassword(databaseURL, passwordFile string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil {
		return "", errors.New("AGENT_STUDIO_DATABASE_URL must be a PostgreSQL URL with a username")
	}
	if _, exists := u.User.Password(); exists {
		return "", errors.New("database password must be provided through AGENT_STUDIO_DATABASE_PASSWORD_FILE")
	}
	if strings.TrimSpace(passwordFile) == "" {
		return "", errors.New("AGENT_STUDIO_DATABASE_PASSWORD_FILE is required")
	}
	data, err := os.ReadFile(passwordFile)
	if err != nil {
		return "", fmt.Errorf("read database password file: %w", err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", errors.New("database password file is empty")
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String(), nil
}

func checkSchema(ctx context.Context, db *sql.DB) error {
	var applied bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM public.agent_studio_schema_migrations WHERE version = $1
	)`, expectedSchemaVersion).Scan(&applied); err != nil {
		return fmt.Errorf("check Agent Studio schema: %w", err)
	}
	if !applied {
		return fmt.Errorf("Agent Studio schema migration %s is not applied", expectedSchemaVersion)
	}
	return nil
}

// requireBindAddress keeps the Tailscale Serve identity header unreachable
// from the network. The development identity ignores that header, so it may
// bind one specific interface (such as the tailnet IP) but never all of them.
func requireBindAddress(address string, devIdentity bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid AGENT_STUDIO_HTTP_ADDR: %w", err)
	}
	ip := net.ParseIP(host)
	switch {
	case ip != nil && ip.IsLoopback():
		return nil
	case !devIdentity:
		return errors.New("AGENT_STUDIO_HTTP_ADDR must bind only to a loopback address for Tailscale Serve authentication")
	case ip == nil || ip.IsUnspecified():
		return errors.New("development identity must bind to one specific interface IP, not all interfaces")
	default:
		return nil
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func spaHandler(directory string) http.Handler {
	root := os.DirFS(directory)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// URL paths are slash-separated on every OS. filepath.Clean("//assets/…")
		// interprets a normal request as a UNC path on Windows and hides valid assets.
		if strings.Contains(r.URL.Path, "\\") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "." || name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(root, name); err != nil {
			if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		if name != "index.html" {
			w.Header().Set("X-Content-Type-Options", "nosniff")
		}
		http.ServeFile(w, r, filepath.Join(directory, filepath.FromSlash(name)))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}
