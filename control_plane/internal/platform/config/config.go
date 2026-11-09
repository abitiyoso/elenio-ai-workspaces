// Package config loads and validates Control Plane configuration from the environment.
// Secrets are read from files referenced by *_FILE variables; production refuses insecure or
// development-only settings at startup.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Upstream is one trusted, deployment-configured mini-app target. The registry never supplies these.
type Upstream struct {
	// URL the BFF forwards same-origin /apps/<key>/ traffic to. Only used when Mode == "proxy".
	URL string `json:"url"`
	// HealthURL is probed server-side for route-health display. Optional.
	HealthURL string `json:"health_url"`
	// Audience placed in the audience-bound service token for this app.
	Audience string `json:"audience"`
	// Mode "proxy" forwards traffic; "pending" authorises the route but answers with an honest
	// integration-pending page because the service has not adopted the identity contract.
	Mode string `json:"mode"`
	// Contract and PendingReason are human-readable metadata for the integrations page.
	Contract      string `json:"contract"`
	PendingReason string `json:"pending_reason"`
	// StripPrefix removes /apps/<key> before forwarding (services mounted at their own root).
	StripPrefix bool `json:"strip_prefix"`
}

// TokenExchangeClient is one experience app allowed to exchange its own Control Plane token for a user-scoped
// token towards other trusted upstreams (POST /api/v1/token-exchange). Deployment config only.
type TokenExchangeClient struct {
	// SecretSHA256 is the lowercase hex SHA-256 of the client secret; the plaintext never reaches the CP.
	SecretSHA256 string `json:"secret_sha256"`
	// Audiences are trusted-upstream app keys this client may request tokens for (no wildcards).
	Audiences []string `json:"audiences"`
}

// emptySecretSHA256 is sha256(""): a client configured with an empty secret.
const emptySecretSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type Config struct {
	Env             string
	HTTPAddress     string
	MetricsAddress  string
	PublicBaseURL   *url.URL
	DatabaseURL     string
	LogLevel        string
	WebDir          string
	MigrateOnStart  bool
	TenantKey       string
	TenantName      string
	CookieSecure    bool
	SessionIdleTTL  time.Duration
	SessionAbsTTL   time.Duration
	AuthFlowTTL     time.Duration
	OIDCIssuer      string
	OIDCClientID    string
	OIDCSecret      string
	OIDCSecretRef   string
	OIDCRedirectURL string
	OIDCScopes      []string
	OIDCPostLogout  string
	CSRFKey         []byte
	TokenEncKey     []byte
	SigningKeyPEM   []byte // ES256 private key for service tokens; generated in development if absent
	PreviousJWKS    []byte // optional previous public keys kept in JWKS during rotation
	ServiceTokenTTL time.Duration
	Upstreams       map[string]Upstream
	// TokenExchangeClients keyed by client id (= the client's registered app key and token audience).
	TokenExchangeClients map[string]TokenExchangeClient
	HealthInterval       time.Duration
	RateLimitAuth        float64 // requests per second per client for /auth/*
	RateLimitAPI         float64 // per session for /api/*
	RateLimitAdminW      float64 // per session for admin writes
	DevSeed              bool
	DevSeedFile          string
	RegistryEnv          string // manifest environment accepted by this deployment: dev | staging | production
}

var appKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`)

// Load reads configuration using getenv (os.Getenv in production code, a map in tests).
func Load(getenv func(string) string) (*Config, error) {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	dur := func(k string, def time.Duration) time.Duration {
		raw := get(k, "")
		if raw == "" {
			return def
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			fail("%s must be a positive duration", k)
			return def
		}
		return d
	}
	rate := func(k string, def float64) float64 {
		raw := get(k, "")
		if raw == "" {
			return def
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || f <= 0 {
			fail("%s must be a positive number", k)
			return def
		}
		return f
	}
	secret := func(k string, required bool) ([]byte, string) {
		path := get(k+"_FILE", "")
		if path == "" {
			if required {
				fail("%s_FILE is required", k)
			}
			return nil, ""
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fail("%s_FILE cannot be read", k)
			return nil, path
		}
		return []byte(strings.TrimSpace(string(data))), path
	}

	c := &Config{
		Env:             get("APP_ENV", EnvProduction),
		HTTPAddress:     get("HTTP_ADDRESS", ":8200"),
		MetricsAddress:  get("METRICS_ADDRESS", "127.0.0.1:9200"),
		DatabaseURL:     get("DATABASE_URL", ""),
		LogLevel:        get("LOG_LEVEL", "info"),
		WebDir:          get("WEB_DIR", "web/dist"),
		MigrateOnStart:  get("MIGRATE_ON_START", "true") == "true",
		TenantKey:       get("TENANT_KEY", ""),
		TenantName:      get("TENANT_NAME", ""),
		SessionIdleTTL:  dur("SESSION_IDLE_TTL", 30*time.Minute),
		SessionAbsTTL:   dur("SESSION_ABSOLUTE_TTL", 8*time.Hour),
		AuthFlowTTL:     dur("AUTH_FLOW_TTL", 10*time.Minute),
		OIDCIssuer:      get("OIDC_ISSUER_URL", ""),
		OIDCClientID:    get("OIDC_CLIENT_ID", ""),
		OIDCRedirectURL: get("OIDC_REDIRECT_URI", ""),
		OIDCPostLogout:  get("OIDC_POST_LOGOUT_REDIRECT_URI", ""),
		ServiceTokenTTL: dur("SERVICE_TOKEN_TTL", 5*time.Minute),
		HealthInterval:  dur("APP_HEALTH_INTERVAL", 30*time.Second),
		RateLimitAuth:   rate("RATE_LIMIT_AUTH_RPS", 2),
		RateLimitAPI:    rate("RATE_LIMIT_API_RPS", 20),
		RateLimitAdminW: rate("RATE_LIMIT_ADMIN_WRITE_RPS", 2),
		DevSeed:         get("DEV_SEED", "false") == "true",
		DevSeedFile:     get("DEV_SEED_FILE", "deploy/dev-seed.json"),
	}
	defRegistry := "production"
	if c.Env == EnvDevelopment {
		defRegistry = "dev"
	}
	c.RegistryEnv = get("APP_REGISTRY_ENVIRONMENT", defRegistry)
	if c.RegistryEnv != "dev" && c.RegistryEnv != "staging" && c.RegistryEnv != "production" {
		fail("APP_REGISTRY_ENVIRONMENT must be dev, staging or production")
	}
	c.OIDCScopes = strings.Fields(strings.ReplaceAll(get("OIDC_SCOPES", "openid profile email"), ",", " "))

	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		fail("APP_ENV must be development or production")
	}
	if c.DatabaseURL == "" {
		if b, _ := secret("DATABASE_URL", false); b != nil {
			c.DatabaseURL = string(b)
		} else {
			fail("DATABASE_URL or DATABASE_URL_FILE is required")
		}
	}
	if c.TenantKey == "" || !regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`).MatchString(c.TenantKey) {
		fail("TENANT_KEY is required (lowercase key)")
	}
	if c.TenantName == "" {
		c.TenantName = c.TenantKey
	}
	if c.SessionIdleTTL > c.SessionAbsTTL {
		fail("SESSION_IDLE_TTL must not exceed SESSION_ABSOLUTE_TTL")
	}
	if c.ServiceTokenTTL > 15*time.Minute {
		fail("SERVICE_TOKEN_TTL must be at most 15m (downstream verifiers reject longer tokens)")
	}

	base, err := url.Parse(get("PUBLIC_BASE_URL", ""))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.Path != "" && base.Path != "/" || base.RawQuery != "" || base.User != nil {
		fail("PUBLIC_BASE_URL must be an origin such as https://workspace.example")
	} else {
		base.Path = ""
		c.PublicBaseURL = base
	}

	// OIDC: exact redirect URI on the public origin.
	if c.OIDCIssuer == "" || c.OIDCClientID == "" {
		fail("OIDC_ISSUER_URL and OIDC_CLIENT_ID are required")
	}
	var sec []byte
	sec, c.OIDCSecretRef = secret("OIDC_CLIENT_SECRET", true)
	c.OIDCSecret = string(sec)
	if c.PublicBaseURL != nil {
		want := c.PublicBaseURL.String() + "/auth/callback"
		if c.OIDCRedirectURL == "" {
			c.OIDCRedirectURL = want
		} else if c.OIDCRedirectURL != want {
			fail("OIDC_REDIRECT_URI must be exactly %s", want)
		}
		if c.OIDCPostLogout == "" {
			c.OIDCPostLogout = c.PublicBaseURL.String() + "/signed-out"
		}
	}
	if !contains(c.OIDCScopes, "openid") {
		fail("OIDC_SCOPES must include openid")
	}

	c.CSRFKey, _ = secret("CSRF_HMAC_KEY", c.Env == EnvProduction)
	c.TokenEncKey, _ = secret("TOKEN_ENCRYPTION_KEY", c.Env == EnvProduction)
	c.SigningKeyPEM, _ = secret("SERVICE_TOKEN_SIGNING_KEY", c.Env == EnvProduction)
	c.PreviousJWKS, _ = secret("SERVICE_TOKEN_PREVIOUS_JWKS", false)
	for name, key := range map[string][]byte{"CSRF_HMAC_KEY": c.CSRFKey, "TOKEN_ENCRYPTION_KEY": c.TokenEncKey} {
		if key != nil && len(key) < 32 {
			fail("%s must contain at least 32 bytes", name)
		}
	}

	c.Upstreams = map[string]Upstream{}
	if raw, _ := secret("TRUSTED_APP_UPSTREAMS", false); raw != nil {
		c.Upstreams, err = parseUpstreams(raw, c.Env)
		if err != nil {
			fail("TRUSTED_APP_UPSTREAMS: %v", err)
		}
	} else if raw := get("TRUSTED_APP_UPSTREAMS", ""); raw != "" {
		c.Upstreams, err = parseUpstreams([]byte(raw), c.Env)
		if err != nil {
			fail("TRUSTED_APP_UPSTREAMS: %v", err)
		}
	}

	c.TokenExchangeClients = map[string]TokenExchangeClient{}
	if raw, _ := secret("TOKEN_EXCHANGE_CLIENTS", false); raw != nil {
		c.TokenExchangeClients, err = parseExchangeClients(raw, c.Upstreams, c.Env)
		if err != nil {
			fail("TOKEN_EXCHANGE_CLIENTS: %v", err)
		}
	} else if raw := get("TOKEN_EXCHANGE_CLIENTS", ""); raw != "" {
		c.TokenExchangeClients, err = parseExchangeClients([]byte(raw), c.Upstreams, c.Env)
		if err != nil {
			fail("TOKEN_EXCHANGE_CLIENTS: %v", err)
		}
	}

	insecureCookie := get("COOKIE_INSECURE_DEV_ACKNOWLEDGE", "")
	c.CookieSecure = true
	if c.PublicBaseURL != nil && c.PublicBaseURL.Scheme == "http" {
		// Plain HTTP is a development-only escape hatch (e.g. a tailnet VM without TLS certificates).
		if c.Env != EnvDevelopment || insecureCookie != "i-understand-cookies-are-not-secure" {
			fail("PUBLIC_BASE_URL must be https outside development (http requires APP_ENV=development and COOKIE_INSECURE_DEV_ACKNOWLEDGE)")
		}
		c.CookieSecure = false
	}

	if c.Env == EnvProduction {
		if c.DevSeed {
			fail("DEV_SEED is refused in production")
		}
		if strings.HasPrefix(c.OIDCIssuer, "http://") {
			fail("OIDC_ISSUER_URL must be https in production")
		}
		if getenv("DEV_MOCK_AUTH") != "" || getenv("DEV_LOGIN") != "" {
			fail("development mock authentication settings are refused in production")
		}
		if len(c.Upstreams) == 0 {
			fail("TRUSTED_APP_UPSTREAMS is required in production")
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

func parseUpstreams(raw []byte, env string) (map[string]Upstream, error) {
	out := map[string]Upstream{}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	for key, u := range out {
		if !appKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid app key %q", key)
		}
		switch u.Mode {
		case "proxy":
			if err := checkUpstreamURL(u.URL, env); err != nil {
				return nil, fmt.Errorf("%s.url: %w", key, err)
			}
			if u.Audience == "" {
				return nil, fmt.Errorf("%s.audience is required for proxy mode", key)
			}
		case "pending":
			if u.PendingReason == "" {
				return nil, fmt.Errorf("%s.pending_reason is required for pending mode", key)
			}
		default:
			return nil, fmt.Errorf("%s.mode must be proxy or pending", key)
		}
		if u.HealthURL != "" {
			if err := checkUpstreamURL(u.HealthURL, env); err != nil {
				return nil, fmt.Errorf("%s.health_url: %w", key, err)
			}
		}
		out[key] = u
	}
	return out, nil
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// parseExchangeClients validates the token-exchange allowlist against the trusted upstreams: a client must be
// a proxied app whose token audience is its own key (subject tokens are minted for it by the app route), and
// every requested audience must be another proxied upstream with an audience. Fail closed on anything else.
func parseExchangeClients(raw []byte, upstreams map[string]Upstream, env string) (map[string]TokenExchangeClient, error) {
	out := map[string]TokenExchangeClient{}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	for id, cl := range out {
		if !appKeyPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid client id %q (must be a registered app key)", id)
		}
		up, ok := upstreams[id]
		if !ok || up.Mode != "proxy" || up.Audience != id {
			return nil, fmt.Errorf("%s: client must be a proxied trusted upstream whose audience is %q", id, id)
		}
		cl.SecretSHA256 = strings.ToLower(strings.TrimSpace(cl.SecretSHA256))
		if !sha256Hex.MatchString(cl.SecretSHA256) {
			return nil, fmt.Errorf("%s.secret_sha256 must be 64 hex characters (sha256 of the client secret)", id)
		}
		if env == EnvProduction && cl.SecretSHA256 == emptySecretSHA256 {
			return nil, fmt.Errorf("%s.secret_sha256 is the hash of an empty secret", id)
		}
		if len(cl.Audiences) == 0 {
			return nil, fmt.Errorf("%s.audiences must list at least one app key", id)
		}
		seen := map[string]bool{}
		for _, aud := range cl.Audiences {
			if strings.ContainsAny(aud, "*?") || !appKeyPattern.MatchString(aud) {
				return nil, fmt.Errorf("%s.audiences: %q is not an app key (wildcards are not allowed)", id, aud)
			}
			if aud == id {
				return nil, fmt.Errorf("%s.audiences: a client cannot exchange for its own audience", id)
			}
			if seen[aud] {
				return nil, fmt.Errorf("%s.audiences: duplicate %q", id, aud)
			}
			seen[aud] = true
			target, ok := upstreams[aud]
			if !ok || target.Mode != "proxy" || target.Audience == "" {
				return nil, fmt.Errorf("%s.audiences: %q is not a proxied trusted upstream with an audience", id, aud)
			}
		}
		out[id] = cl
	}
	return out, nil
}

func checkUpstreamURL(raw, env string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("must be an absolute URL without credentials, query or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (env == EnvDevelopment || isInternalHost(u.Hostname()))) {
		return errors.New("must be https (http only for internal hosts or development)")
	}
	return nil
}

// isInternalHost accepts loopback and single-label service names (cluster-internal DNS).
func isInternalHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || !strings.Contains(h, ".")
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Origin returns the scheme://host of the public base URL.
func (c *Config) Origin() string { return c.PublicBaseURL.String() }

// CookieName returns the session cookie name; the __Host- prefix requires Secure.
func (c *Config) CookieName() string {
	if c.CookieSecure {
		return "__Host-cp_session"
	}
	return "cp_session"
}

func (c *Config) FlowCookieName() string {
	if c.CookieSecure {
		return "__Host-cp_auth_flow"
	}
	return "cp_auth_flow"
}
