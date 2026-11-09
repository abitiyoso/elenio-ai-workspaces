package config

import (
	"os"
	"testing"
	"time"
)

func TestMockAuthRejectedOutsideDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_MODE", "mock")
	if _, err := Load(); err == nil {
		t.Fatal("expected production mock auth to be rejected")
	}
}

func TestTrustedAuthAcceptedOutsideDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_MODE", "trusted")
	if _, err := Load(); err != nil {
		t.Fatalf("trusted auth rejected: %v", err)
	}
}

func TestMockAuthRejectedWhenEnvironmentIsUnset(t *testing.T) {
	previous, existed := os.LookupEnv("APP_ENV")
	if err := os.Unsetenv("APP_ENV"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("APP_ENV", previous)
		} else {
			_ = os.Unsetenv("APP_ENV")
		}
	})
	t.Setenv("AUTH_MODE", "mock")
	if _, err := Load(); err == nil {
		t.Fatal("expected mock auth to be rejected when APP_ENV is unset")
	}
}

func TestMockAuthRejectedWhenEnvironmentIsEmpty(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("AUTH_MODE", "mock")
	if _, err := Load(); err == nil {
		t.Fatal("expected mock auth to be rejected when APP_ENV is empty")
	}
}

func TestDevelopmentEnvironmentAllowsMockAuth(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "mock")
	if _, err := Load(); err != nil {
		t.Fatalf("explicit development mock auth rejected: %v", err)
	}
}

func TestInvalidEmbeddingDimensionsRejected(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "mock")
	t.Setenv("EMBEDDING_DIMENSIONS", "768x")
	if _, err := Load(); err == nil {
		t.Fatal("expected dimension validation error")
	}
}

func setControlPlaneEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_MODE", "control_plane_jwt")
	t.Setenv("AUTH_ISSUER", "https://cp.example.test")
	t.Setenv("AUTH_JWKS_URL", "https://cp.example.test/.well-known/jwks.json")
	t.Setenv("AUTH_TENANT_MAP", "acme-demo=0D6F1C2E-1111-4222-8333-444455556666, beta=0d6f1c2e-1111-4222-8333-444455556667")
	for _, key := range []string{"AUTH_AUDIENCE", "AUTH_MAX_TOKEN_TTL", "AUTH_CLOCK_SKEW", "AUTH_JWKS_CACHE_TTL"} {
		t.Setenv(key, "")
	}
}

func TestControlPlaneAuthDefaults(t *testing.T) {
	setControlPlaneEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("control plane auth rejected: %v", err)
	}
	cp := c.ControlPlaneAuth
	if c.AuthMode != AuthModeControlPlaneJWT || cp.Audience != "kms" || cp.MaxTokenTTL != 15*time.Minute || cp.ClockSkew != time.Minute || cp.JWKSCacheTTL != 10*time.Minute {
		t.Fatalf("unexpected defaults: %+v", cp)
	}
	if cp.TenantMap["acme-demo"] != "0d6f1c2e-1111-4222-8333-444455556666" || cp.TenantMap["beta"] != "0d6f1c2e-1111-4222-8333-444455556667" || len(cp.TenantMap) != 2 {
		t.Fatalf("tenant map = %v", cp.TenantMap)
	}
}

func TestControlPlaneAuthOverrides(t *testing.T) {
	setControlPlaneEnv(t)
	t.Setenv("AUTH_AUDIENCE", "kms-staging")
	t.Setenv("AUTH_MAX_TOKEN_TTL", "1h")
	t.Setenv("AUTH_CLOCK_SKEW", "0s")
	t.Setenv("AUTH_JWKS_CACHE_TTL", "1m")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cp := c.ControlPlaneAuth; cp.Audience != "kms-staging" || cp.MaxTokenTTL != time.Hour || cp.ClockSkew != 0 || cp.JWKSCacheTTL != time.Minute {
		t.Fatalf("overrides not applied: %+v", cp)
	}
}

func TestControlPlaneAuthAllowsHTTPJWKSOnlyInDevelopment(t *testing.T) {
	setControlPlaneEnv(t)
	t.Setenv("AUTH_JWKS_URL", "http://100.126.146.52:5200/.well-known/jwks.json")
	if _, err := Load(); err == nil {
		t.Fatal("expected http JWKS URL to be rejected outside development")
	}
	t.Setenv("APP_ENV", "development")
	if _, err := Load(); err != nil {
		t.Fatalf("http JWKS URL rejected in development: %v", err)
	}
}

func TestControlPlaneAuthRejectsInvalidSettings(t *testing.T) {
	cases := map[string][2]string{
		"missing issuer":        {"AUTH_ISSUER", ""},
		"relative issuer":       {"AUTH_ISSUER", "cp.example.test"},
		"missing jwks":          {"AUTH_JWKS_URL", ""},
		"bad jwks":              {"AUTH_JWKS_URL", "ftp://cp.example.test/jwks"},
		"missing tenant map":    {"AUTH_TENANT_MAP", ""},
		"non-uuid tenant":       {"AUTH_TENANT_MAP", "acme-demo=not-a-uuid"},
		"entry without equals":  {"AUTH_TENANT_MAP", "acme-demo"},
		"empty key":             {"AUTH_TENANT_MAP", "=0d6f1c2e-1111-4222-8333-444455556666"},
		"duplicate key":         {"AUTH_TENANT_MAP", "a=0d6f1c2e-1111-4222-8333-444455556666,a=0d6f1c2e-1111-4222-8333-444455556667"},
		"duplicate tenant uuid": {"AUTH_TENANT_MAP", "a=0d6f1c2e-1111-4222-8333-444455556666,b=0d6f1c2e-1111-4222-8333-444455556666"},
		"ttl above max":         {"AUTH_MAX_TOKEN_TTL", "61m"},
		"ttl not duration":      {"AUTH_MAX_TOKEN_TTL", "15"},
		"negative skew":         {"AUTH_CLOCK_SKEW", "-1s"},
		"skew too large":        {"AUTH_CLOCK_SKEW", "10m"},
		"cache ttl too small":   {"AUTH_JWKS_CACHE_TTL", "1s"},
	}
	for name, setting := range cases {
		t.Run(name, func(t *testing.T) {
			setControlPlaneEnv(t)
			t.Setenv(setting[0], setting[1])
			if _, err := Load(); err == nil {
				t.Fatalf("expected %s=%q to be rejected", setting[0], setting[1])
			}
		})
	}
}

func TestUnknownAuthModeRejected(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "jwt")
	if _, err := Load(); err == nil {
		t.Fatal("expected unknown auth mode to be rejected")
	}
}

func TestTrustedAndMockModesIgnoreControlPlaneSettings(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "mock")
	t.Setenv("AUTH_TENANT_MAP", "garbage")
	c, err := Load()
	if err != nil {
		t.Fatalf("mock mode must not validate control plane settings: %v", err)
	}
	if c.ControlPlaneAuth.TenantMap != nil {
		t.Fatal("control plane settings must stay empty outside control_plane_jwt")
	}
	t.Setenv("AUTH_MODE", "trusted")
	if _, err := Load(); err != nil {
		t.Fatalf("trusted mode must not validate control plane settings: %v", err)
	}
}

func TestSelfApprovalExemptionIsDevelopmentOnly(t *testing.T) {
	const demo = "45C7A958-EA88-490C-9B89-ECCF95294CBE"
	t.Setenv("AUTH_MODE", "trusted")
	t.Setenv("SELF_APPROVAL_EXEMPT_SUBJECTS", demo+", 45c7a958-ea88-490c-9b89-eccf95294cbe")
	t.Setenv("APP_ENV", "production")
	if _, err := Load(); err == nil {
		t.Fatal("self-approval exemption must be refused outside development")
	}
	t.Setenv("APP_ENV", "development")
	cfg, err := Load()
	if err != nil || len(cfg.SelfApprovalExempt) != 1 || cfg.SelfApprovalExempt[0] != "45c7a958-ea88-490c-9b89-eccf95294cbe" {
		t.Fatalf("development exemption = %v, %v", cfg.SelfApprovalExempt, err)
	}
	t.Setenv("SELF_APPROVAL_EXEMPT_SUBJECTS", "demo")
	if _, err := Load(); err == nil {
		t.Fatal("non-UUID subjects must be rejected")
	}
	t.Setenv("SELF_APPROVAL_EXEMPT_SUBJECTS", "")
	t.Setenv("APP_ENV", "production")
	if cfg, err := Load(); err != nil || len(cfg.SelfApprovalExempt) != 0 {
		t.Fatalf("unset exemption must not change production: %v %v", cfg.SelfApprovalExempt, err)
	}
}
