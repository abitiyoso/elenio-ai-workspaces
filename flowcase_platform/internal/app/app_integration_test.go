package app_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/elenio/flowcase/internal/api"
	"github.com/elenio/flowcase/internal/app"
	"github.com/elenio/flowcase/internal/platform/config"
	"github.com/elenio/flowcase/internal/testutil/pgtest"
)

// TestTrustedHostTokenEndToEnd covers UAT-09: a valid host token opens Flowcase without a second login,
// while expired, foreign-tenant and header-forged requests are rejected.
func TestTrustedHostTokenEndToEnd(t *testing.T) {
	pool, migrator := pgtest.Migrated(t)
	ctx := context.Background()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{
			{"kty": "RSA", "kid": "host-1", "alg": "RS256", "use": "sig", "n": enc(key.N.Bytes()), "e": enc(big.NewInt(int64(key.E)).Bytes())},
		}})
	}))
	defer jwks.Close()

	const issuer = "https://workspace.example"
	var tenantID, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (external_ref, name) VALUES ('acme', 'Acme') RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (issuer, subject, display_name) VALUES ($1, 'sub-ada', 'Ada') RETURNING id::text`, issuer).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO memberships (tenant_id, user_id) VALUES ($1, $2)`, tenantID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_assignments (tenant_id, user_id, role) VALUES ($1, $2, 'case_owner')`, tenantID, userID); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Environment: config.EnvTest, KMSMode: config.IntegrationDisabled, AgentMode: config.IntegrationDisabled, Auth: config.AuthConfig{
		Mode: config.AuthModeTrustedJWT, Issuer: issuer, Audience: "flowcase", JWKSURL: jwks.URL, TenantClaim: "tenant_id",
		ClockSkew: 30 * time.Second, MaxTokenTTL: 15 * time.Minute, JWKSCacheTTL: 10 * time.Minute,
	}}
	adapter, authChecks, err := app.NewAuth(cfg, pool)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := app.NewAgents(cfg, pool)
	if err != nil {
		t.Fatal(err)
	}
	handler := api.NewServer(api.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Environment: cfg.Environment, Auth: adapter,
		Readiness: append(app.DatabaseChecks(pool, migrator), authChecks...),
		Knowledge: app.NewKnowledge(cfg), Agents: agents,
	})

	sign := func(mutate func(jwt.MapClaims)) string {
		now := time.Now()
		c := jwt.MapClaims{"iss": issuer, "aud": "flowcase", "sub": "sub-ada", "tenant_id": "acme", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}
		if mutate != nil {
			mutate(c)
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
		tok.Header["kid"] = "host-1"
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	call := func(token string, extra map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		for k, v := range extra {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	w := call(sign(nil), map[string]string{"X-Tenant-ID": "evil", "X-Roles": "tenant_admin"})
	if w.Code != http.StatusOK {
		t.Fatalf("valid token: expected 200, got %d %s", w.Code, w.Body)
	}
	var session struct {
		UserID   string   `json:"user_id"`
		TenantID string   `json:"tenant_id"`
		Roles    []string `json:"roles"`
		AuthMode string   `json:"auth_mode"`
	}
	if err := json.NewDecoder(w.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.TenantID != tenantID || session.UserID != userID || len(session.Roles) != 1 || session.Roles[0] != "case_owner" || session.AuthMode != "trusted_jwt" {
		t.Fatalf("session must come from the directory, not headers: %+v", session)
	}

	rejections := map[string]string{
		"expired token": sign(func(c jwt.MapClaims) {
			c["iat"] = time.Now().Add(-10 * time.Minute).Unix()
			c["exp"] = time.Now().Add(-2 * time.Minute).Unix()
		}),
		"foreign tenant":    sign(func(c jwt.MapClaims) { c["tenant_id"] = "globex" }),
		"unknown subject":   sign(func(c jwt.MapClaims) { c["sub"] = "sub-mallory" }),
		"other audience":    sign(func(c jwt.MapClaims) { c["aud"] = "kms" }),
		"no token, headers": "",
	}
	for name, tok := range rejections {
		if w := call(tok, map[string]string{"X-User-ID": userID, "X-Tenant-ID": tenantID}); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d", name, w.Code)
		}
	}

	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("readyz with postgres, schema and JWKS available: %d %s", ready.Code, ready.Body)
	}
}
