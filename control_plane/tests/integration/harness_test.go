// Package integration runs the real BFF (HTTP handler, PostgreSQL, OIDC client) against an in-process mock
// OIDC provider. Tests skip without CP_TEST_DATABASE_URL; CP_REQUIRE_DB=1 turns the skip into a failure.
package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/control-plane/internal/apps"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/devseed"
	cphttp "github.com/elenio/control-plane/internal/http"
	"github.com/elenio/control-plane/internal/identity/oidc"
	"github.com/elenio/control-plane/internal/identity/provisioning"
	"github.com/elenio/control-plane/internal/identity/session"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/elenio/control-plane/internal/platform/postgres"
	"github.com/elenio/control-plane/internal/platform/secrets"
	"github.com/elenio/control-plane/internal/workspaces"
	"github.com/elenio/control-plane/migrations"
)

// Subjects from deploy/dev-seed.json.
const (
	subRafi  = "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c01"
	subNadia = "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c02"
	subMaya  = "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c03"
	subDimas = "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c04"
	subSari  = "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c05"
	subDemo  = "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c06" // all-access walkthrough account
)

// ---------- mock IdP ----------

type pendingCode struct {
	sub, nonce, challenge, redirect string
}

type mockIdP struct {
	srv     *httptest.Server
	key     *rsa.PrivateKey
	mu      sync.Mutex
	codes   map[string]pendingCode
	revoked []string
	// Tampering knobs for negative tests.
	tokenIssuer   string
	tokenAudience string
	tokenTTL      time.Duration
	tokenNonce    string
}

func newMockIdP(t *testing.T) *mockIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	m := &mockIdP{key: key, codes: map[string]pendingCode{}}
	mux := http.NewServeMux()
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	iss := m.srv.URL
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": iss, "authorization_endpoint": iss + "/authorize", "token_endpoint": iss + "/token",
			"jwks_uri": iss + "/jwks", "end_session_endpoint": iss + "/logout", "revocation_endpoint": iss + "/revoke",
			"id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		e := big.NewInt(int64(key.PublicKey.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e)}}})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		m.mu.Lock()
		m.revoked = append(m.revoked, r.PostForm.Get("token"))
		m.mu.Unlock()
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok || id != "control-plane" || secret != "test-client-secret" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
			return
		}
		m.mu.Lock()
		pc, found := m.codes[r.PostForm.Get("code")]
		delete(m.codes, r.PostForm.Get("code")) // one-time codes
		m.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !found || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != pc.redirect ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != pc.challenge {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		now := time.Now()
		ttl := 5 * time.Minute
		if m.tokenTTL != 0 {
			ttl = m.tokenTTL
		}
		claims := jwt.MapClaims{"iss": or(m.tokenIssuer, iss), "aud": or(m.tokenAudience, "control-plane"), "sub": pc.sub,
			"nonce": or(m.tokenNonce, pc.nonce), "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(ttl).Unix(),
			"name": "Test " + pc.sub[len(pc.sub)-2:], "email": "u" + pc.sub[len(pc.sub)-2:] + "@example.invalid", "email_verified": true}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = "k1"
		signed, _ := tok.SignedString(key)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-" + pc.sub, "token_type": "Bearer", "expires_in": 300,
			"refresh_token": "rt-" + pc.sub, "id_token": signed})
	})
	return m
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// authorize simulates the user authenticating at the IdP and returns the code.
func (m *mockIdP) authorize(t *testing.T, authURL, sub string) (code, state string) {
	u, err := url.Parse(authURL)
	if err != nil || !strings.HasPrefix(authURL, m.srv.URL+"/authorize") {
		t.Fatalf("unexpected authorization URL %q", authURL)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" || q.Get("state") == "" {
		t.Fatalf("authorization request lacks PKCE/state/nonce: %v", q)
	}
	code = secrets.RandomToken(16)
	m.mu.Lock()
	m.codes[code] = pendingCode{sub: sub, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	m.mu.Unlock()
	return code, q.Get("state")
}

// ---------- environment ----------

type env struct {
	t        *testing.T
	pool     *pgxpool.Pool
	idp      *mockIdP
	srv      *httptest.Server
	cfg      *config.Config
	tokens   *integrations.Issuer
	tenantID string
	sessions *session.Store
	// signingKey is the service-token key (lets tests forge expired/tampered tokens under the real kid).
	signingKey *ecdsa.PrivateKey
}

type envOptions struct {
	upstreams map[string]config.Upstream
	// exchangeClients is raw TOKEN_EXCHANGE_CLIENTS JSON (empty: token exchange disabled).
	exchangeClients string
}

func dbURL(t *testing.T) string {
	u := os.Getenv("CP_TEST_DATABASE_URL")
	if u == "" {
		if os.Getenv("CP_REQUIRE_DB") == "1" {
			t.Fatal("CP_TEST_DATABASE_URL is required (CP_REQUIRE_DB=1)")
		}
		t.Skip("integration test skipped: CP_TEST_DATABASE_URL not set")
	}
	return u
}

func newEnv(t *testing.T, opts envOptions) *env {
	t.Helper()
	ctx := context.Background()
	base := dbURL(t)
	schema := "t_" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(secrets.RandomToken(8)))
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	pool, err := postgres.Open(ctx, base+sep+"search_path="+schema+",public")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}

	idp := newMockIdP(t)
	ts := httptest.NewUnstartedServer(nil)
	origin := "http://" + ts.Listener.Addr().String()
	dir := t.TempDir()
	secretFile := dir + "/client-secret"
	_ = os.WriteFile(secretFile, []byte("test-client-secret"), 0o600)
	upJSON, _ := json.Marshal(opts.upstreams)
	envVars := map[string]string{
		"APP_ENV": "development", "COOKIE_INSECURE_DEV_ACKNOWLEDGE": "i-understand-cookies-are-not-secure",
		"DATABASE_URL": "unused", "TENANT_KEY": "acme-demo", "PUBLIC_BASE_URL": origin,
		"OIDC_ISSUER_URL": idp.srv.URL, "OIDC_CLIENT_ID": "control-plane", "OIDC_CLIENT_SECRET_FILE": secretFile,
		"RATE_LIMIT_AUTH_RPS": "1000", "RATE_LIMIT_API_RPS": "1000", "RATE_LIMIT_ADMIN_WRITE_RPS": "1000",
		"WEB_DIR": dir, "TRUSTED_APP_UPSTREAMS": string(upJSON), "TOKEN_EXCHANGE_CLIENTS": opts.exchangeClients,
	}
	cfg, err := config.Load(func(k string) string { return envVars[k] })
	if err != nil {
		t.Fatal(err)
	}
	tenantID, err := provisioning.EnsureTenant(ctx, pool, cfg.TenantKey, "Example Enterprise")
	if err != nil {
		t.Fatal(err)
	}
	if err := devseed.Apply(ctx, pool, "../../deploy/dev-seed.json", tenantID, idp.srv.URL, cfg.RegistryEnv); err != nil {
		t.Fatal(err)
	}
	sealer, _ := secrets.NewSealer(secrets.RandomBytes(32))
	signingKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(signingKey)
	tokens, err := integrations.NewIssuer(origin, cfg.ServiceTokenTTL, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := authorization.NewEngine(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	store := &session.Store{Pool: pool, IdleTTL: cfg.SessionIdleTTL, AbsTTL: cfg.SessionAbsTTL, FlowTTL: cfg.AuthFlowTTL, Sealer: sealer, CSRFKey: secrets.RandomBytes(32)}
	client := oidc.New(oidc.Config{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCSecret,
		RedirectURL: cfg.OIDCRedirectURL, Scopes: cfg.OIDCScopes, PostLogout: cfg.OIDCPostLogout})
	upstreams := integrations.NewUpstreams(cfg.Upstreams, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := cphttp.New(cphttp.Deps{Config: cfg, Logger: logger, Pool: pool, OIDC: client, Sessions: store, Engine: engine,
		Workspaces: &workspaces.Service{Pool: pool, Engine: engine, Sessions: store},
		Apps:       &apps.Service{Pool: pool, Engine: engine, Upstreams: upstreams, Environment: cfg.RegistryEnv},
		Tokens:     tokens, Upstreams: upstreams, TenantID: tenantID,
		Ready: func(context.Context) map[string]string {
			return map[string]string{"database": "ok", "migrations": "ok", "idp": "ok"}
		}})
	ts.Config.Handler = srv.Handler()
	ts.Start()
	t.Cleanup(ts.Close)
	return &env{t: t, pool: pool, idp: idp, srv: ts, cfg: cfg, tokens: tokens, tenantID: tenantID, sessions: store, signingKey: signingKey}
}

// ---------- browser-like client ----------

type browser struct {
	e    *env
	c    *http.Client
	csrf string
}

func (e *env) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{e: e, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (b *browser) do(method, path string, body any, hdr map[string]string) *http.Response {
	b.e.t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, b.e.srv.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", b.e.cfg.Origin())
		if b.csrf != "" {
			req.Header.Set("X-CSRF-Token", b.csrf)
		}
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.e.t.Fatal(err)
	}
	return resp
}

func readJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if v != nil {
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatalf("decode %s: %v (%s)", resp.Request.URL.Path, err, raw)
		}
	}
}

func expectStatus(t *testing.T, resp *http.Response, want int) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d: %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// startLogin returns the IdP authorization URL produced by /auth/login.
func (b *browser) startLogin(returnTo string) string {
	b.e.t.Helper()
	path := "/auth/login"
	if returnTo != "" {
		path += "?return_to=" + url.QueryEscape(returnTo)
	}
	resp := b.do(http.MethodGet, path, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		b.e.t.Fatalf("login status %d", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

// callback completes the flow and returns the redirect location.
func (b *browser) callback(code, state string, extra url.Values) string {
	b.e.t.Helper()
	q := url.Values{"code": {code}, "state": {state}, "iss": {b.e.idp.srv.URL}}
	for k, v := range extra {
		q[k] = v
	}
	resp := b.do(http.MethodGet, "/auth/callback?"+q.Encode(), nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		b.e.t.Fatalf("callback status %d", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

func (b *browser) login(sub string) *browser {
	b.e.t.Helper()
	code, state := b.e.idp.authorize(b.e.t, b.startLogin(""), sub)
	if loc := b.callback(code, state, nil); loc != "/" {
		b.e.t.Fatalf("login for %s redirected to %q", sub, loc)
	}
	b.refreshCSRF()
	return b
}

func (b *browser) refreshCSRF() map[string]any {
	me := expectStatus(b.e.t, b.do(http.MethodGet, "/api/v1/me", nil, nil), 200)
	b.csrf, _ = me["csrf_token"].(string)
	return me
}

func (e *env) as(sub string) *browser { return e.browser().login(sub) }

func (e *env) userID(sub string) string {
	var id string
	if err := e.pool.QueryRow(context.Background(), `SELECT id::text FROM users WHERE idp_subject = $1`, sub).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) workspaceID(key string) string {
	var id string
	if err := e.pool.QueryRow(context.Background(), `SELECT id::text FROM workspaces WHERE key = $1`, key).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) membershipID(sub, wsKey string) string {
	var id string
	if err := e.pool.QueryRow(context.Background(), `SELECT m.id::text FROM workspace_memberships m JOIN users u ON u.id = m.user_id
JOIN workspaces w ON w.id = m.workspace_id WHERE u.idp_subject = $1 AND w.key = $2 AND m.state <> 'revoked'`, sub, wsKey).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) auditCount(action string) int {
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM platform_audit_events WHERE action = $1`, action).Scan(&n)
	return n
}

func appKeys(me map[string]any) []string {
	var out []string
	list, _ := me["apps"].([]any)
	for _, a := range list {
		out = append(out, a.(map[string]any)["app_key"].(string))
	}
	return out
}

func mustEqual[T comparable](t *testing.T, got, want T, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

var _ = fmt.Sprintf

func parseECJWK(raw []byte) (*ecdsa.PublicKey, error) {
	var k struct {
		Kty, Crv, X, Y string
	}
	if err := json.Unmarshal(raw, &k); err != nil || k.Kty != "EC" || k.Crv != "P-256" {
		return nil, fmt.Errorf("unsupported jwk")
	}
	x, err1 := base64.RawURLEncoding.DecodeString(k.X)
	y, err2 := base64.RawURLEncoding.DecodeString(k.Y)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("bad jwk coordinates")
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
}
