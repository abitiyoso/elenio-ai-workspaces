// Package config loads and validates process configuration from the environment.
// Validation fails closed: insecure development modes are rejected outside development.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvStaging     = "staging"
	EnvProduction  = "production"

	AuthModeTrustedJWT = "trusted_jwt"
	AuthModeDevMock    = "dev_mock"

	IntegrationDisabled = "disabled"
	IntegrationStub     = "stub"

	// AgentModeAgentStudio calls the AI Agent Studio runtime (AGENT_RUNTIME_MODE only).
	AgentModeAgentStudio = "agent_studio"

	// DevAuthAcknowledgement must be set verbatim in DEV_AUTH_ACKNOWLEDGE to enable the mock identity.
	DevAuthAcknowledgement = "i-understand-this-is-not-secure"
)

var personaKey = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Config struct {
	Environment         string
	HTTPAddress         string
	WorkerHealthAddress string
	LogLevel            slog.Level
	DatabaseURL         string
	DatabaseMaxConns    int32
	ShutdownTimeout     time.Duration
	FrameAncestors      []string
	Auth                AuthConfig
	KMSMode             string
	AgentMode           string
	AgentCallbackSecret string
	AgentStudio         AgentStudioConfig
	ConnectorsMode      string
	PacksConfig         string
	MetricsAddress      string
}

// AgentStudioConfig is used when AGENT_RUNTIME_MODE=agent_studio. Token is read from
// AGENT_STUDIO_SERVICE_TOKEN_FILE and must never be logged.
type AgentStudioConfig struct {
	BaseURL   string
	TokenFile string
	Token     string
	Timeout   time.Duration
}

type AuthConfig struct {
	Mode         string
	Issuer       string
	Audience     string
	JWKSURL      string
	TenantClaim  string
	ClockSkew    time.Duration
	MaxTokenTTL  time.Duration
	JWKSCacheTTL time.Duration

	DevUserID      string
	DevTenantID    string
	DevDisplayName string
	DevRoles       []string
	DevPersonas    []DevPersona

	// SelfApprovalExemptSubjects (SELF_APPROVAL_EXEMPT_SUBJECTS, development only) lists host token
	// subjects that segregation-of-duties rules do not stop from approving their own cases.
	SelfApprovalExemptSubjects []string
}

// DevPersona is an extra development identity (DEV_AUTH_PERSONAS, JSON array).
type DevPersona struct {
	Key    string   `json:"key"`
	UserID string   `json:"user_id"`
	Name   string   `json:"name"`
	Roles  []string `json:"roles"`
}

// IsDevelopment reports whether insecure local conveniences may be enabled.
func (c Config) IsDevelopment() bool { return c.Environment == EnvDevelopment }

// Lookup matches os.LookupEnv so tests can supply a map.
type Lookup func(key string) (string, bool)

func FromEnv() (Config, error) { return Load(os.LookupEnv) }

func Load(lookup Lookup) (Config, error) {
	r := reader{lookup: lookup}
	c := Config{
		Environment:         r.str("APP_ENV", EnvProduction),
		HTTPAddress:         r.str("HTTP_ADDRESS", ":8080"),
		WorkerHealthAddress: r.str("WORKER_HEALTH_ADDRESS", ":8081"),
		DatabaseURL:         r.str("DATABASE_URL", ""),
		DatabaseMaxConns:    int32(r.integer("DATABASE_MAX_CONNS", 10)),
		ShutdownTimeout:     r.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
		FrameAncestors:      r.list("FRAME_ANCESTORS"),
		KMSMode:             r.str("KMS_MODE", IntegrationDisabled),
		AgentMode:           r.str("AGENT_RUNTIME_MODE", IntegrationDisabled),
		AgentCallbackSecret: r.str("AGENT_CALLBACK_SECRET", ""),
		AgentStudio: AgentStudioConfig{
			BaseURL:   r.str("AGENT_STUDIO_BASE_URL", ""),
			TokenFile: r.str("AGENT_STUDIO_SERVICE_TOKEN_FILE", ""),
			Timeout:   r.duration("AGENT_STUDIO_TIMEOUT", 15*time.Second),
		},
		ConnectorsMode: r.str("CONNECTORS_MODE", IntegrationDisabled),
		PacksConfig:    r.str("PACKS_CONFIG", ""),
		MetricsAddress: r.str("METRICS_ADDRESS", ""),
		Auth: AuthConfig{
			Mode:           r.str("AUTH_MODE", ""),
			Issuer:         r.str("AUTH_ISSUER", ""),
			Audience:       r.str("AUTH_AUDIENCE", ""),
			JWKSURL:        r.str("AUTH_JWKS_URL", ""),
			TenantClaim:    r.str("AUTH_TENANT_CLAIM", "tenant_id"),
			ClockSkew:      r.duration("AUTH_CLOCK_SKEW", 60*time.Second),
			MaxTokenTTL:    r.duration("AUTH_MAX_TOKEN_TTL", 15*time.Minute),
			JWKSCacheTTL:   r.duration("AUTH_JWKS_CACHE_TTL", 10*time.Minute),
			DevUserID:      r.str("DEV_AUTH_USER_ID", ""),
			DevTenantID:    r.str("DEV_AUTH_TENANT_ID", ""),
			DevDisplayName: r.str("DEV_AUTH_DISPLAY_NAME", "Local Developer"),
			DevRoles:       r.list("DEV_AUTH_ROLES"),

			SelfApprovalExemptSubjects: r.list("SELF_APPROVAL_EXEMPT_SUBJECTS"),
		},
	}
	level, err := parseLevel(r.str("LOG_LEVEL", "info"))
	if err != nil {
		r.errs = append(r.errs, err)
	}
	c.LogLevel = level
	devAck := r.str("DEV_AUTH_ACKNOWLEDGE", "")
	personasRaw := r.str("DEV_AUTH_PERSONAS", "")

	errs := r.errs
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	switch c.Environment {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
	default:
		add("APP_ENV must be one of development, test, staging, production")
	}
	if c.DatabaseURL == "" {
		add("DATABASE_URL is required")
	}
	if c.DatabaseMaxConns < 1 || c.DatabaseMaxConns > 200 {
		add("DATABASE_MAX_CONNS must be between 1 and 200")
	}
	for _, origin := range c.FrameAncestors {
		if origin != "'self'" && !validOrigin(origin, c.IsDevelopment()) {
			add("FRAME_ANCESTORS entry %q must be an https origin ('self' or http allowed only in development)", origin)
		}
	}

	if len(c.Auth.SelfApprovalExemptSubjects) > 0 && !c.IsDevelopment() {
		add("SELF_APPROVAL_EXEMPT_SUBJECTS is allowed only when APP_ENV=development")
	}
	for _, s := range c.Auth.SelfApprovalExemptSubjects {
		if !uuidPattern.MatchString(s) {
			add("SELF_APPROVAL_EXEMPT_SUBJECTS entries must be lower-case UUIDs")
			break
		}
	}
	switch c.Auth.Mode {
	case AuthModeDevMock:
		if !c.IsDevelopment() {
			add("AUTH_MODE=dev_mock is allowed only when APP_ENV=development")
		}
		if devAck != DevAuthAcknowledgement {
			add("AUTH_MODE=dev_mock requires DEV_AUTH_ACKNOWLEDGE=%s", DevAuthAcknowledgement)
		}
		if !uuidPattern.MatchString(c.Auth.DevUserID) || !uuidPattern.MatchString(c.Auth.DevTenantID) {
			add("DEV_AUTH_USER_ID and DEV_AUTH_TENANT_ID must be lowercase UUIDs")
		}
		if len(c.Auth.DevRoles) == 0 {
			c.Auth.DevRoles = []string{"workspace_member"}
		}
		if personasRaw != "" {
			if err := json.Unmarshal([]byte(personasRaw), &c.Auth.DevPersonas); err != nil {
				add("DEV_AUTH_PERSONAS must be a JSON array of {key, user_id, name, roles}")
			}
			seen := map[string]bool{"default": true}
			for _, p := range c.Auth.DevPersonas {
				if !personaKey.MatchString(p.Key) || seen[p.Key] || !uuidPattern.MatchString(p.UserID) || p.UserID == c.Auth.DevUserID || p.Name == "" {
					add("DEV_AUTH_PERSONAS entry %q needs a unique key, a distinct lowercase UUID user_id and a name", p.Key)
				}
				seen[p.Key] = true
			}
		}
	case AuthModeTrustedJWT:
		if personasRaw != "" {
			add("DEV_AUTH_PERSONAS is only valid with AUTH_MODE=dev_mock")
		}
		if c.Auth.Issuer == "" || c.Auth.Audience == "" || c.Auth.JWKSURL == "" {
			add("AUTH_MODE=trusted_jwt requires AUTH_ISSUER, AUTH_AUDIENCE and AUTH_JWKS_URL")
		}
		if c.Auth.JWKSURL != "" && !validURL(c.Auth.JWKSURL, c.IsDevelopment()) {
			add("AUTH_JWKS_URL must be an https URL (http allowed only in development)")
		}
		if c.Auth.TenantClaim == "" {
			add("AUTH_TENANT_CLAIM must not be empty")
		}
		if c.Auth.MaxTokenTTL <= 0 || c.Auth.MaxTokenTTL > time.Hour {
			add("AUTH_MAX_TOKEN_TTL must be greater than 0 and at most 1h")
		}
		if c.Auth.ClockSkew < 0 || c.Auth.ClockSkew > 5*time.Minute {
			add("AUTH_CLOCK_SKEW must be between 0 and 5m")
		}
		if c.Auth.JWKSCacheTTL < time.Minute || c.Auth.JWKSCacheTTL > 24*time.Hour {
			add("AUTH_JWKS_CACHE_TTL must be between 1m and 24h")
		}
	case "":
		if personasRaw != "" {
			add("DEV_AUTH_PERSONAS is only valid with AUTH_MODE=dev_mock")
		}
		add("AUTH_MODE is required (trusted_jwt, or dev_mock in development)")
	default:
		add("AUTH_MODE must be trusted_jwt or dev_mock")
	}

	if c.AgentCallbackSecret != "" && len(c.AgentCallbackSecret) < 32 {
		add("AGENT_CALLBACK_SECRET must have at least 32 characters")
	}
	for name, mode := range map[string]string{"KMS_MODE": c.KMSMode, "AGENT_RUNTIME_MODE": c.AgentMode, "CONNECTORS_MODE": c.ConnectorsMode} {
		switch {
		case mode == IntegrationDisabled:
		case mode == IntegrationStub:
			if !c.IsDevelopment() && c.Environment != EnvTest {
				add("%s=stub is allowed only in development or test", name)
			}
		case name == "AGENT_RUNTIME_MODE" && mode == AgentModeAgentStudio:
			c.AgentStudio.Token = validateAgentStudio(&c, add)
		case name == "AGENT_RUNTIME_MODE":
			add("AGENT_RUNTIME_MODE must be disabled, stub or agent_studio")
		default:
			add("%s must be disabled or stub; no production adapter exists until the external contract is confirmed", name)
		}
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

// validateAgentStudio checks the agent_studio settings and returns the service token read from
// AGENT_STUDIO_SERVICE_TOKEN_FILE.
func validateAgentStudio(c *Config, add func(string, ...any)) string {
	as := c.AgentStudio
	switch {
	case as.BaseURL == "":
		add("AGENT_RUNTIME_MODE=agent_studio requires AGENT_STUDIO_BASE_URL")
	case !validServiceURL(as.BaseURL, c.IsDevelopment()):
		add("AGENT_STUDIO_BASE_URL must be an https URL without query or credentials (http only to a loopback/internal host, or in development)")
	}
	if c.AgentCallbackSecret == "" {
		add("AGENT_RUNTIME_MODE=agent_studio requires AGENT_CALLBACK_SECRET (>= 32 characters) to verify Agent Studio callbacks")
	}
	if as.Timeout < time.Second || as.Timeout > 2*time.Minute {
		add("AGENT_STUDIO_TIMEOUT must be between 1s and 2m")
	}
	if as.TokenFile == "" {
		add("AGENT_RUNTIME_MODE=agent_studio requires AGENT_STUDIO_SERVICE_TOKEN_FILE")
		return ""
	}
	raw, err := os.ReadFile(as.TokenFile)
	if err != nil {
		add("AGENT_STUDIO_SERVICE_TOKEN_FILE is not readable")
		return ""
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 || strings.ContainsFunc(token, func(r rune) bool { return r <= ' ' || r > '~' }) {
		add("AGENT_STUDIO_SERVICE_TOKEN_FILE must contain a single printable token of at least 32 characters")
		return ""
	}
	return token
}

// validServiceURL accepts https, or http to a loopback/internal host (or any host in development).
func validServiceURL(raw string, allowHTTP bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (allowHTTP || internalHost(u.Hostname())))
}

// internalHost reports loopback, private, CGNAT (tailnet) and link-local addresses, single-label
// names (container service names) and .localhost/.internal/.local names.
func internalHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if ip := net.ParseIP(h); ip != nil {
		_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
	}
	if h == "" {
		return false
	}
	return h == "localhost" || !strings.Contains(h, ".") ||
		strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".local")
}

type reader struct {
	lookup Lookup
	errs   []error
}

func (r *reader) str(key, fallback string) string {
	if v, ok := r.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func (r *reader) integer(key string, fallback int) int {
	raw := r.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be an integer", key))
		return fallback
	}
	return v
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	raw := r.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be a Go duration such as 30s or 5m", key))
		return fallback
	}
	return v
}

func (r *reader) list(key string) []string {
	var out []string
	for _, part := range strings.Split(r.str(key, ""), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseLevel(v string) (slog.Level, error) {
	switch v {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, errors.New("LOG_LEVEL must be debug, info, warn, or error")
}

func validURL(raw string, allowHTTP bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || (allowHTTP && u.Scheme == "http")
}

func validOrigin(raw string, allowHTTP bool) bool {
	u, err := url.Parse(raw)
	return err == nil && validURL(raw, allowHTTP) && (u.Path == "" || u.Path == "/") && u.RawQuery == ""
}
