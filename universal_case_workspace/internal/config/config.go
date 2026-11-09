// Package config reads UCW_* environment variables. Secrets are read from files, never from the
// environment directly, and never logged.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env         string // development | production
	Addr        string
	LogLevel    string
	DatabaseURL string
	PacksDir    string
	WebDir      string

	CPIssuer       string
	CPJWKSURL      string
	CPAudience     string
	TenantKey      string
	CPMaxTokenTTL  time.Duration
	CPClockSkew    time.Duration
	CPInternalURL  string
	CPClientID     string
	CPClientSecret string

	FlowcaseURL     string
	FlowcaseTimeout time.Duration
	FlowcaseUIPath  string

	AgentStudioURL     string
	AgentStudioToken   string
	AgentStudioTimeout time.Duration
	IntakeTurnTimeout  time.Duration

	// Docling converts uploaded intake documents; empty: only TXT/MD uploads work.
	DoclingURL      string
	DoclingAPIKey   string
	DocumentTimeout time.Duration
}

func duration(get func(string) string, key string, def, min, max time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(get(key))
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < min || d > max {
		return 0, fmt.Errorf("%s must be a duration between %s and %s", key, min, max)
	}
	return d, nil
}

func secretFile(get func(string) string, key string, required bool) (string, error) {
	path := strings.TrimSpace(get(key))
	if path == "" {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: cannot read file", key)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("%s: file is empty", key)
	}
	return v, nil
}

// checkURL requires an absolute http(s) URL without credentials, query or fragment; plain http only to
// loopback or in development.
func checkURL(key, raw string, dev bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must be an absolute URL without credentials, query or fragment", key)
	}
	loopback := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && (dev || loopback)) {
		return fmt.Errorf("%s must use https unless UCW_ENV=development", key)
	}
	return nil
}

func Load(get func(string) string) (Config, error) {
	c := Config{
		Env:            strings.TrimSpace(get("UCW_ENV")),
		Addr:           strings.TrimSpace(get("UCW_ADDR")),
		LogLevel:       strings.TrimSpace(get("UCW_LOG_LEVEL")),
		DatabaseURL:    strings.TrimSpace(get("UCW_DATABASE_URL")),
		PacksDir:       strings.TrimSpace(get("UCW_PACKS_DIR")),
		WebDir:         strings.TrimSpace(get("UCW_WEB_DIR")),
		CPIssuer:       strings.TrimRight(strings.TrimSpace(get("UCW_CP_ISSUER")), "/"),
		CPJWKSURL:      strings.TrimSpace(get("UCW_CP_JWKS_URL")),
		CPAudience:     strings.TrimSpace(get("UCW_CP_AUDIENCE")),
		TenantKey:      strings.TrimSpace(get("UCW_TENANT_KEY")),
		CPInternalURL:  strings.TrimRight(strings.TrimSpace(get("UCW_CP_INTERNAL_URL")), "/"),
		CPClientID:     strings.TrimSpace(get("UCW_CP_CLIENT_ID")),
		FlowcaseURL:    strings.TrimRight(strings.TrimSpace(get("UCW_FLOWCASE_URL")), "/"),
		FlowcaseUIPath: strings.TrimRight(strings.TrimSpace(get("UCW_FLOWCASE_UI_PATH")), "/"),
		AgentStudioURL: strings.TrimRight(strings.TrimSpace(get("UCW_AGENT_STUDIO_URL")), "/"),
		DoclingURL:     strings.TrimRight(strings.TrimSpace(get("UCW_DOCLING_URL")), "/"),
	}
	if c.Env == "" {
		c.Env = "production"
	}
	if c.Env != "production" && c.Env != "development" {
		return c, errors.New("UCW_ENV must be production or development")
	}
	dev := c.Env == "development"
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8480"
	}
	if c.PacksDir == "" {
		c.PacksDir = "domain-packs"
	}
	if c.CPAudience == "" {
		c.CPAudience = "case-workspace"
	}
	if c.CPClientID == "" {
		c.CPClientID = "case-workspace"
	}
	if c.FlowcaseUIPath == "" {
		c.FlowcaseUIPath = "/apps/flowcase"
	}
	if !strings.HasPrefix(c.FlowcaseUIPath, "/") || strings.HasPrefix(c.FlowcaseUIPath, "//") {
		return c, errors.New("UCW_FLOWCASE_UI_PATH must be a same-origin path such as /apps/flowcase")
	}
	for k, v := range map[string]string{"UCW_DATABASE_URL": c.DatabaseURL, "UCW_CP_ISSUER": c.CPIssuer, "UCW_CP_JWKS_URL": c.CPJWKSURL,
		"UCW_TENANT_KEY": c.TenantKey, "UCW_CP_INTERNAL_URL": c.CPInternalURL, "UCW_FLOWCASE_URL": c.FlowcaseURL} {
		if v == "" {
			return c, fmt.Errorf("%s is required", k)
		}
	}
	for k, v := range map[string]string{"UCW_CP_ISSUER": c.CPIssuer, "UCW_CP_JWKS_URL": c.CPJWKSURL, "UCW_CP_INTERNAL_URL": c.CPInternalURL,
		"UCW_FLOWCASE_URL": c.FlowcaseURL} {
		if err := checkURL(k, v, dev); err != nil {
			return c, err
		}
	}
	var err error
	if c.CPClientSecret, err = secretFile(get, "UCW_CP_CLIENT_SECRET_FILE", true); err != nil {
		return c, err
	}
	if c.CPMaxTokenTTL, err = duration(get, "UCW_CP_MAX_TOKEN_TTL", 15*time.Minute, time.Minute, 15*time.Minute); err != nil {
		return c, err
	}
	if c.CPClockSkew, err = duration(get, "UCW_CP_CLOCK_SKEW", 30*time.Second, 0, 2*time.Minute); err != nil {
		return c, err
	}
	if c.FlowcaseTimeout, err = duration(get, "UCW_FLOWCASE_TIMEOUT", 10*time.Second, time.Second, time.Minute); err != nil {
		return c, err
	}
	if c.AgentStudioTimeout, err = duration(get, "UCW_AGENT_STUDIO_TIMEOUT", 15*time.Second, time.Second, 2*time.Minute); err != nil {
		return c, err
	}
	if c.IntakeTurnTimeout, err = duration(get, "UCW_INTAKE_TURN_TIMEOUT", 3*time.Minute, 30*time.Second, 15*time.Minute); err != nil {
		return c, err
	}
	if c.DocumentTimeout, err = duration(get, "UCW_DOCUMENT_TIMEOUT", 10*time.Minute, time.Minute, 30*time.Minute); err != nil {
		return c, err
	}
	if c.DoclingURL != "" {
		if err := checkURL("UCW_DOCLING_URL", c.DoclingURL, dev); err != nil {
			return c, err
		}
		if c.DoclingAPIKey, err = secretFile(get, "UCW_DOCLING_API_KEY_FILE", false); err != nil {
			return c, err
		}
	}
	if c.AgentStudioURL != "" {
		if err := checkURL("UCW_AGENT_STUDIO_URL", c.AgentStudioURL, dev); err != nil {
			return c, err
		}
		if c.AgentStudioToken, err = secretFile(get, "UCW_AGENT_STUDIO_TOKEN_FILE", true); err != nil {
			return c, err
		}
	}
	return c, nil
}
