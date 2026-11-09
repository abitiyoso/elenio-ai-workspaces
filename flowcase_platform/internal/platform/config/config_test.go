package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookupFrom(m map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func devMockEnv() map[string]string {
	return map[string]string{
		"APP_ENV":              "development",
		"DATABASE_URL":         "postgres://localhost/flowcase",
		"AUTH_MODE":            "dev_mock",
		"DEV_AUTH_ACKNOWLEDGE": DevAuthAcknowledgement,
		"DEV_AUTH_USER_ID":     "00000000-0000-4000-8000-000000000101",
		"DEV_AUTH_TENANT_ID":   "00000000-0000-4000-8000-000000000001",
	}
}

func trustedEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":  "postgres://db/flowcase",
		"AUTH_MODE":     "trusted_jwt",
		"AUTH_ISSUER":   "https://host.example",
		"AUTH_AUDIENCE": "flowcase",
		"AUTH_JWKS_URL": "https://host.example/.well-known/jwks.json",
	}
}

func TestDevMockAcceptedOnlyWithExplicitOptIn(t *testing.T) {
	cfg, err := Load(lookupFrom(devMockEnv()))
	if err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
	if cfg.Auth.Mode != AuthModeDevMock || len(cfg.Auth.DevRoles) != 1 {
		t.Fatalf("unexpected auth config: %+v", cfg.Auth)
	}
}

func TestFailsClosed(t *testing.T) {
	cases := map[string]struct {
		base   func() map[string]string
		mutate func(map[string]string)
		want   string
	}{
		"default environment is production, mock rejected": {devMockEnv, func(m map[string]string) { delete(m, "APP_ENV") }, "only when APP_ENV=development"},
		"mock in staging":              {devMockEnv, func(m map[string]string) { m["APP_ENV"] = "staging" }, "only when APP_ENV=development"},
		"mock without acknowledgement": {devMockEnv, func(m map[string]string) { delete(m, "DEV_AUTH_ACKNOWLEDGE") }, "DEV_AUTH_ACKNOWLEDGE"},
		"mock with non uuid ids":       {devMockEnv, func(m map[string]string) { m["DEV_AUTH_USER_ID"] = "admin" }, "UUIDs"},
		"missing auth mode":            {trustedEnv, func(m map[string]string) { delete(m, "AUTH_MODE") }, "AUTH_MODE is required"},
		"unknown auth mode":            {trustedEnv, func(m map[string]string) { m["AUTH_MODE"] = "header" }, "AUTH_MODE must be"},
		"trusted missing audience":     {trustedEnv, func(m map[string]string) { delete(m, "AUTH_AUDIENCE") }, "AUTH_AUDIENCE"},
		"trusted http jwks in prod":    {trustedEnv, func(m map[string]string) { m["AUTH_JWKS_URL"] = "http://host/jwks" }, "https"},
		"token ttl too long":           {trustedEnv, func(m map[string]string) { m["AUTH_MAX_TOKEN_TTL"] = "2h" }, "AUTH_MAX_TOKEN_TTL"},
		"missing database":             {trustedEnv, func(m map[string]string) { delete(m, "DATABASE_URL") }, "DATABASE_URL"},
		"kms stub in production":       {trustedEnv, func(m map[string]string) { m["KMS_MODE"] = "stub" }, "KMS_MODE=stub"},
		"unconfirmed agent adapter":    {trustedEnv, func(m map[string]string) { m["AGENT_RUNTIME_MODE"] = "http" }, "AGENT_RUNTIME_MODE must be"},
		"invalid frame ancestor":       {trustedEnv, func(m map[string]string) { m["FRAME_ANCESTORS"] = "http://host" }, "FRAME_ANCESTORS"},
		"bad log level":                {trustedEnv, func(m map[string]string) { m["LOG_LEVEL"] = "trace" }, "LOG_LEVEL"},
		"bad duration":                 {trustedEnv, func(m map[string]string) { m["AUTH_CLOCK_SKEW"] = "soon" }, "AUTH_CLOCK_SKEW"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := tc.base()
			tc.mutate(env)
			_, err := Load(lookupFrom(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTrustedDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(trustedEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != EnvProduction || cfg.Auth.TenantClaim != "tenant_id" || cfg.Auth.MaxTokenTTL != 15*time.Minute {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.KMSMode != IntegrationDisabled || cfg.AgentMode != IntegrationDisabled {
		t.Fatalf("integrations must default to disabled")
	}
}

func TestDevPersonasValidated(t *testing.T) {
	env := devMockEnv()
	env["DEV_AUTH_PERSONAS"] = `[{"key":"approver","user_id":"00000000-0000-4000-8000-000000000102","name":"Dian","roles":["domain_approver"]}]`
	cfg, err := Load(lookupFrom(env))
	if err != nil || len(cfg.Auth.DevPersonas) != 1 {
		t.Fatalf("valid personas rejected: %v", err)
	}
	for name, raw := range map[string]string{
		"not json":     `approver`,
		"same as main": `[{"key":"x","user_id":"00000000-0000-4000-8000-000000000101","name":"X"}]`,
		"bad key":      `[{"key":"Default","user_id":"00000000-0000-4000-8000-000000000109","name":"X"}]`,
	} {
		env["DEV_AUTH_PERSONAS"] = raw
		if _, err := Load(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "DEV_AUTH_PERSONAS") {
			t.Errorf("%s: expected DEV_AUTH_PERSONAS error, got %v", name, err)
		}
	}
	trusted := trustedEnv()
	trusted["DEV_AUTH_PERSONAS"] = `[]`
	if _, err := Load(lookupFrom(trusted)); err == nil {
		t.Error("personas must be refused outside dev_mock")
	}
}

func agentStudioEnv(t *testing.T, base func() map[string]string) map[string]string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-studio.token")
	if err := os.WriteFile(path, []byte("  as-service-token-0123456789abcdefghijklmnop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := base()
	env["AGENT_RUNTIME_MODE"] = "agent_studio"
	env["AGENT_STUDIO_BASE_URL"] = "https://agent-studio.example"
	env["AGENT_STUDIO_SERVICE_TOKEN_FILE"] = path
	env["AGENT_CALLBACK_SECRET"] = "callback-secret-0123456789abcdefghij"
	return env
}

func TestAgentStudioModeAccepted(t *testing.T) {
	cfg, err := Load(lookupFrom(agentStudioEnv(t, trustedEnv)))
	if err != nil {
		t.Fatalf("agent_studio in production must be accepted: %v", err)
	}
	if cfg.AgentMode != AgentModeAgentStudio || cfg.AgentStudio.Token != "as-service-token-0123456789abcdefghijklmnop" ||
		cfg.AgentStudio.BaseURL != "https://agent-studio.example" || cfg.AgentStudio.Timeout != 15*time.Second {
		t.Fatalf("unexpected agent studio config: mode=%s url=%s timeout=%s token-len=%d", cfg.AgentMode, cfg.AgentStudio.BaseURL, cfg.AgentStudio.Timeout, len(cfg.AgentStudio.Token))
	}
	for _, u := range []string{"http://agent-studio:8080", "http://127.0.0.1:8090", "http://10.0.0.5", "http://100.101.102.103:8090", "http://as.internal", "https://as.example/base/"} {
		env := agentStudioEnv(t, trustedEnv)
		env["AGENT_STUDIO_BASE_URL"] = u
		if _, err := Load(lookupFrom(env)); err != nil {
			t.Errorf("%s should be accepted outside development: %v", u, err)
		}
	}
	dev := agentStudioEnv(t, devMockEnv)
	dev["AGENT_STUDIO_BASE_URL"] = "http://studio.example.com"
	if _, err := Load(lookupFrom(dev)); err != nil {
		t.Errorf("http to any host is allowed in development: %v", err)
	}
}

func TestAgentStudioModeFailsClosed(t *testing.T) {
	short := filepath.Join(t.TempDir(), "short.token")
	_ = os.WriteFile(short, []byte("too-short\n"), 0o600)
	spaced := filepath.Join(t.TempDir(), "spaced.token")
	_ = os.WriteFile(spaced, []byte("as-service-token 0123456789abcdefghijklmnop"), 0o600)
	for name, tc := range map[string]struct {
		mutate func(map[string]string)
		want   string
	}{
		"missing base url":     {func(m map[string]string) { delete(m, "AGENT_STUDIO_BASE_URL") }, "requires AGENT_STUDIO_BASE_URL"},
		"public http":          {func(m map[string]string) { m["AGENT_STUDIO_BASE_URL"] = "http://studio.example.com" }, "AGENT_STUDIO_BASE_URL must be"},
		"url with credentials": {func(m map[string]string) { m["AGENT_STUDIO_BASE_URL"] = "https://u:p@studio.example.com" }, "AGENT_STUDIO_BASE_URL must be"},
		"url with query":       {func(m map[string]string) { m["AGENT_STUDIO_BASE_URL"] = "https://studio.example.com/?x=1" }, "AGENT_STUDIO_BASE_URL must be"},
		"missing token file":   {func(m map[string]string) { delete(m, "AGENT_STUDIO_SERVICE_TOKEN_FILE") }, "requires AGENT_STUDIO_SERVICE_TOKEN_FILE"},
		"unreadable token":     {func(m map[string]string) { m["AGENT_STUDIO_SERVICE_TOKEN_FILE"] = "/nonexistent/token" }, "not readable"},
		"short token":          {func(m map[string]string) { m["AGENT_STUDIO_SERVICE_TOKEN_FILE"] = short }, "at least 32 characters"},
		"token with space":     {func(m map[string]string) { m["AGENT_STUDIO_SERVICE_TOKEN_FILE"] = spaced }, "at least 32 characters"},
		"no callback secret":   {func(m map[string]string) { delete(m, "AGENT_CALLBACK_SECRET") }, "requires AGENT_CALLBACK_SECRET"},
		"bad timeout":          {func(m map[string]string) { m["AGENT_STUDIO_TIMEOUT"] = "10m" }, "AGENT_STUDIO_TIMEOUT"},
		"unknown agent mode":   {func(m map[string]string) { m["AGENT_RUNTIME_MODE"] = "agentstudio" }, "AGENT_RUNTIME_MODE must be disabled, stub or agent_studio"},
	} {
		t.Run(name, func(t *testing.T) {
			env := agentStudioEnv(t, trustedEnv)
			tc.mutate(env)
			_, err := Load(lookupFrom(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
			if err != nil && strings.Contains(err.Error(), "0123456789abcdefghijklmnop") {
				t.Fatal("errors must never echo the token")
			}
		})
	}
}

func TestSelfApprovalExemptionIsDevelopmentOnly(t *testing.T) {
	const demo = "45c7a958-ea88-490c-9b89-eccf95294cbe"
	dev := trustedEnv()
	dev["APP_ENV"] = "development"
	dev["SELF_APPROVAL_EXEMPT_SUBJECTS"] = demo + ", 6eac36dd-3278-4b7b-aeb1-6431cce2d4a2"
	cfg, err := Load(lookupFrom(dev))
	if err != nil || len(cfg.Auth.SelfApprovalExemptSubjects) != 2 || cfg.Auth.SelfApprovalExemptSubjects[0] != demo {
		t.Fatalf("development exemption list rejected: %v %v", cfg.Auth.SelfApprovalExemptSubjects, err)
	}
	for name, env := range map[string]map[string]string{
		"production": func() map[string]string { e := trustedEnv(); e["SELF_APPROVAL_EXEMPT_SUBJECTS"] = demo; return e }(),
		"staging": func() map[string]string {
			e := trustedEnv()
			e["APP_ENV"], e["SELF_APPROVAL_EXEMPT_SUBJECTS"] = "staging", demo
			return e
		}(),
		"not a uuid": func() map[string]string {
			e := trustedEnv()
			e["APP_ENV"], e["SELF_APPROVAL_EXEMPT_SUBJECTS"] = "development", "demo"
			return e
		}(),
		"upper case": func() map[string]string {
			e := trustedEnv()
			e["APP_ENV"], e["SELF_APPROVAL_EXEMPT_SUBJECTS"] = "development", strings.ToUpper(demo)
			return e
		}(),
	} {
		if _, err := Load(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "SELF_APPROVAL_EXEMPT_SUBJECTS") {
			t.Errorf("%s: expected SELF_APPROVAL_EXEMPT_SUBJECTS to be refused, got %v", name, err)
		}
	}
}
