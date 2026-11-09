package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseEnv(t *testing.T) map[string]string {
	dir := t.TempDir()
	write := func(name, v string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://x", "TENANT_KEY": "acme", "PUBLIC_BASE_URL": "https://workspace.example",
		"OIDC_ISSUER_URL": "https://idp.example/realms/w", "OIDC_CLIENT_ID": "cp", "OIDC_CLIENT_SECRET_FILE": write("s", "secret"),
		"CSRF_HMAC_KEY_FILE": write("c", strings.Repeat("k", 32)), "TOKEN_ENCRYPTION_KEY_FILE": write("e", strings.Repeat("e", 32)),
		"SERVICE_TOKEN_SIGNING_KEY_FILE": write("p", "pem"),
		"TRUSTED_APP_UPSTREAMS":          `{"kms":{"mode":"pending","pending_reason":"verifier not implemented"}}`,
	}
}

func load(env map[string]string) (*Config, error) {
	return Load(func(k string) string { return env[k] })
}

func TestProductionAcceptsSecureConfig(t *testing.T) {
	c, err := load(baseEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if !c.CookieSecure || c.CookieName() != "__Host-cp_session" || c.OIDCRedirectURL != "https://workspace.example/auth/callback" {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestProductionRefusesInsecureOrDevSettings(t *testing.T) {
	cases := map[string]func(map[string]string){
		"dev seed":            func(e map[string]string) { e["DEV_SEED"] = "true" },
		"dev mock auth":       func(e map[string]string) { e["DEV_MOCK_AUTH"] = "true" },
		"http origin":         func(e map[string]string) { e["PUBLIC_BASE_URL"] = "http://workspace.example" },
		"http issuer":         func(e map[string]string) { e["OIDC_ISSUER_URL"] = "http://idp.example" },
		"no upstream config":  func(e map[string]string) { delete(e, "TRUSTED_APP_UPSTREAMS") },
		"missing csrf key":    func(e map[string]string) { delete(e, "CSRF_HMAC_KEY_FILE") },
		"missing signing key": func(e map[string]string) { delete(e, "SERVICE_TOKEN_SIGNING_KEY_FILE") },
		"redirect mismatch":   func(e map[string]string) { e["OIDC_REDIRECT_URI"] = "https://evil.example/auth/callback" },
		"origin with path":    func(e map[string]string) { e["PUBLIC_BASE_URL"] = "https://workspace.example/x" },
		"token ttl too long":  func(e map[string]string) { e["SERVICE_TOKEN_TTL"] = "1h" },
		"upstream credentials": func(e map[string]string) {
			e["TRUSTED_APP_UPSTREAMS"] = `{"kms":{"mode":"proxy","url":"https://u:p@kms.internal","audience":"kms"}}`
		},
		"upstream plain http external": func(e map[string]string) {
			e["TRUSTED_APP_UPSTREAMS"] = `{"kms":{"mode":"proxy","url":"http://kms.example.com","audience":"kms"}}`
		},
		"upstream unknown field": func(e map[string]string) {
			e["TRUSTED_APP_UPSTREAMS"] = `{"kms":{"mode":"proxy","url":"https://kms.internal","audience":"kms","target":"x"}}`
		},
	}
	for name, mutate := range cases {
		env := baseEnv(t)
		mutate(env)
		if _, err := load(env); err == nil {
			t.Errorf("%s: production config accepted", name)
		}
	}
}

func TestHTTPRequiresExplicitDevelopmentAcknowledgement(t *testing.T) {
	env := baseEnv(t)
	env["APP_ENV"], env["PUBLIC_BASE_URL"] = "development", "http://100.64.0.1:5200"
	if _, err := load(env); err == nil {
		t.Fatal("http accepted without acknowledgement")
	}
	env["COOKIE_INSECURE_DEV_ACKNOWLEDGE"] = "i-understand-cookies-are-not-secure"
	c, err := load(env)
	if err != nil {
		t.Fatal(err)
	}
	if c.CookieSecure || c.CookieName() != "cp_session" {
		t.Fatal("expected insecure development cookie")
	}
}

func exchangeEnv(t *testing.T) map[string]string {
	env := baseEnv(t)
	env["TRUSTED_APP_UPSTREAMS"] = `{"case-workspace":{"mode":"proxy","url":"http://127.0.0.1:8480","audience":"case-workspace"},` +
		`"flowcase":{"mode":"proxy","url":"http://127.0.0.1:5280","audience":"flowcase"},` +
		`"kms":{"mode":"pending","pending_reason":"later"}}`
	env["TOKEN_EXCHANGE_CLIENTS"] = `{"case-workspace":{"secret_sha256":"` + strings.Repeat("ab", 32) + `","audiences":["flowcase"]}}`
	return env
}

func TestTokenExchangeClientsAccepted(t *testing.T) {
	c, err := load(exchangeEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	cl, ok := c.TokenExchangeClients["case-workspace"]
	if !ok || len(cl.Audiences) != 1 || cl.Audiences[0] != "flowcase" {
		t.Fatalf("unexpected clients: %+v", c.TokenExchangeClients)
	}
}

func TestTokenExchangeClientsRefused(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	cases := map[string]string{
		"bad client id":         `{"Case_Workspace":{"secret_sha256":"` + hash + `","audiences":["flowcase"]}}`,
		"client not upstream":   `{"insights":{"secret_sha256":"` + hash + `","audiences":["flowcase"]}}`,
		"client is pending":     `{"kms":{"secret_sha256":"` + hash + `","audiences":["flowcase"]}}`,
		"missing secret":        `{"case-workspace":{"audiences":["flowcase"]}}`,
		"short secret hash":     `{"case-workspace":{"secret_sha256":"abc","audiences":["flowcase"]}}`,
		"empty secret in prod":  `{"case-workspace":{"secret_sha256":"` + emptySecretSHA256 + `","audiences":["flowcase"]}}`,
		"no audiences":          `{"case-workspace":{"secret_sha256":"` + hash + `","audiences":[]}}`,
		"wildcard audience":     `{"case-workspace":{"secret_sha256":"` + hash + `","audiences":["*"]}}`,
		"unknown audience":      `{"case-workspace":{"secret_sha256":"` + hash + `","audiences":["agent-studio"]}}`,
		"pending audience":      `{"case-workspace":{"secret_sha256":"` + hash + `","audiences":["kms"]}}`,
		"own audience":          `{"case-workspace":{"secret_sha256":"` + hash + `","audiences":["case-workspace"]}}`,
		"duplicate audience":    `{"case-workspace":{"secret_sha256":"` + hash + `","audiences":["flowcase","flowcase"]}}`,
		"unknown field":         `{"case-workspace":{"secret_sha256":"` + hash + `","audiences":["flowcase"],"secret":"x"}}`,
		"plaintext not allowed": `{"case-workspace":{"secret_sha256":"hunter2","audiences":["flowcase"]}}`,
	}
	for name, raw := range cases {
		env := exchangeEnv(t)
		env["TOKEN_EXCHANGE_CLIENTS"] = raw
		if _, err := load(env); err == nil {
			t.Errorf("%s: token exchange config accepted", name)
		}
	}
}
