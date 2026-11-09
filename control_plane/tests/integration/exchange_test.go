package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/config"
)

const exchangeSecret = "tx-test-secret-0123456789abcdef"

// exchangeEnv wires case-workspace (the client, behind a capturing upstream) plus flowcase and kms as
// proxied targets, and allowlists case-workspace -> {flowcase, kms}. agent-studio is proxied but not allowlisted.
func exchangeEnv(t *testing.T) (*env, func() string) {
	t.Helper()
	var mu sync.Mutex
	var last string
	ucw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Unlock()
		w.WriteHeader(204)
	}))
	t.Cleanup(ucw.Close)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	t.Cleanup(other.Close)
	sum := sha256.Sum256([]byte(exchangeSecret))
	e := newEnv(t, envOptions{
		upstreams: map[string]config.Upstream{
			"case-workspace": {URL: ucw.URL, Audience: "case-workspace", Mode: "proxy", StripPrefix: true},
			"flowcase":       {URL: other.URL, Audience: "flowcase", Mode: "proxy", StripPrefix: true},
			"kms":            {URL: other.URL, Audience: "kms", Mode: "proxy", StripPrefix: true},
			"agent-studio":   {URL: other.URL, Audience: "agent-studio", Mode: "proxy", StripPrefix: true},
		},
		exchangeClients: `{"case-workspace":{"secret_sha256":"` + hex.EncodeToString(sum[:]) + `","audiences":["flowcase","kms"]}}`,
	})
	return e, func() string {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

// subjectToken opens the UCW route as the browser's user and returns the token the proxy forwarded.
func subjectToken(t *testing.T, b *browser, captured func() string) string {
	t.Helper()
	resp := b.do(http.MethodGet, "/apps/case-workspace/api/v1/workspace/me/context", nil, nil)
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 204, "proxied UCW request")
	tok := captured()
	if tok == "" {
		t.Fatal("no subject token captured")
	}
	return tok
}

type exchangeResult struct {
	status int
	body   map[string]any
}

func (e *env) exchange(clientID, secret string, body any, hdr map[string]string) exchangeResult {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1/token-exchange", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if clientID != "" || secret != "" {
		req.SetBasicAuth(clientID, secret)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return exchangeResult{status: resp.StatusCode, body: out}
}

func expectExchange(t *testing.T, r exchangeResult, status int, code string) {
	t.Helper()
	if r.status != status || (code != "" && r.body["code"] != code) {
		t.Fatalf("exchange: got %d %v, want %d %s", r.status, r.body, status, code)
	}
	if code != "" {
		if _, leaked := r.body["access_token"]; leaked {
			t.Fatal("denied exchange returned a token")
		}
	}
}

func parseUnverified(t *testing.T, raw string) jwt.MapClaims {
	t.Helper()
	c := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(raw, c); err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *env) activeSessionID(sub string) string {
	var id string
	if err := e.pool.QueryRow(context.Background(), `SELECT s.id::text FROM sessions s JOIN users u ON u.id = s.user_id
WHERE u.idp_subject = $1 AND s.revoked_at IS NULL ORDER BY s.created_at DESC LIMIT 1`, sub).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// forge signs claims under the real kid with the given method/key (the default is the real signing key).
func (e *env) forge(method jwt.SigningMethod, key any, mut func(jwt.MapClaims), base jwt.MapClaims) string {
	e.t.Helper()
	c := jwt.MapClaims{}
	for k, v := range base {
		c[k] = v
	}
	mut(c)
	if method == nil {
		method, key = jwt.SigningMethodES256, e.signingKey
	}
	tk := jwt.NewWithClaims(method, c)
	tk.Header["kid"] = e.tokens.KeyID()
	s, err := tk.SignedString(key)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func TestTokenExchangeIssuesDelegatedTokenWithinUserAccess(t *testing.T) {
	e, captured := exchangeEnv(t)
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	subj := subjectToken(t, maya, captured)
	sc := parseUnverified(t, subj)

	r := e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": subj, "audience": "flowcase"},
		map[string]string{"X-Request-ID": "corr-exchange-0001"})
	if r.status != 200 {
		t.Fatalf("exchange failed: %d %v", r.status, r.body)
	}
	mustEqual(t, r.body["token_type"], any("Bearer"), "token_type")
	mustEqual(t, r.body["issued_token_type"], any("urn:ietf:params:oauth:token-type:jwt"), "issued_token_type")
	if ei, _ := r.body["expires_in"].(float64); ei <= 0 || ei > 300 {
		t.Fatalf("expires_in %v", r.body["expires_in"])
	}
	tok, _ := r.body["access_token"].(string)
	// The exchanged token passes a Flowcase-style verifier (unknown claims such as act are ignored there).
	claims, err := downstreamVerifier{issuer: e.cfg.Origin(), audience: "flowcase", jwksURL: e.cfg.Origin() + "/.well-known/jwks.json"}.verify(tok)
	if err != nil {
		t.Fatalf("downstream verification of exchanged token: %v", err)
	}
	if _, err := (downstreamVerifier{issuer: e.cfg.Origin(), audience: "case-workspace", jwksURL: e.cfg.Origin() + "/.well-known/jwks.json"}).verify(tok); err == nil {
		t.Fatal("exchanged token accepted for the client audience")
	}
	for _, k := range []string{"sub", "tenant_id", "workspace_id", "sid", "principal_type", "auth_time", "idp_sub", "idp_iss"} {
		if claims[k] != sc[k] {
			t.Fatalf("claim %s changed: %v -> %v", k, sc[k], claims[k])
		}
	}
	mustEqual(t, claims["sub"], any(e.userID(subMaya)), "sub")
	mustEqual(t, claims["workspace_id"], any("sim-b2b"), "workspace")
	mustEqual(t, claims["correlation_id"], any("corr-exchange-0001"), "correlation id")
	act, _ := claims["act"].(map[string]any)
	mustEqual(t, act["sub"], any("app:case-workspace"), "act.sub")
	if _, has := sc["act"]; has {
		t.Fatal("proxy-minted subject token carries act")
	}
	if !strings.Contains(claims["scope"].(string), "app:open") || !strings.Contains(claims["scope"].(string), "case_worker") {
		t.Fatalf("scope %v", claims["scope"])
	}
	iat, _ := claims.GetIssuedAt()
	exp, _ := claims.GetExpirationTime()
	if exp.Sub(iat.Time) > 5*time.Minute {
		t.Fatalf("exchanged token lifetime %v", exp.Sub(iat.Time))
	}
	// KMS is allowlisted and Maya may open KMS in sim-b2b.
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": subj, "audience": "kms",
		"grant_type": "urn:ietf:params:oauth:grant-type:token-exchange", "subject_token_type": "urn:ietf:params:oauth:token-type:jwt"}, nil), 200, "")

	// Audit: one allow row per exchange with client and audience.
	mustEqual(t, e.auditCount("token.exchanged"), 2, "token.exchanged rows")
	var actorType, serviceID, target, decision, clientMeta, corr string
	if err := e.pool.QueryRow(context.Background(), `SELECT actor_type, service_id, target_id, decision, metadata->>'client_id', correlation_id
FROM platform_audit_events WHERE action = 'token.exchanged' ORDER BY seq LIMIT 1`).Scan(&actorType, &serviceID, &target, &decision, &clientMeta, &corr); err != nil {
		t.Fatal(err)
	}
	if actorType != "service" || serviceID != "case-workspace" || target != "flowcase" || decision != "allow" || clientMeta != "case-workspace" || corr != "corr-exchange-0001" {
		t.Fatalf("audit row: %s %s %s %s %s %s", actorType, serviceID, target, decision, clientMeta, corr)
	}

	// The exchange is not cookie-authenticated: the browser session alone gets nothing (and needs no CSRF).
	resp := maya.do(http.MethodPost, "/api/v1/token-exchange", map[string]any{"subject_token": subj, "audience": "flowcase"}, nil)
	body := expectStatus(t, resp, 401)
	mustEqual(t, body["code"], any("INVALID_CLIENT"), "cookie-only exchange")
}

func TestTokenExchangeRejectsBadClientsTokensAndPolicy(t *testing.T) {
	e, captured := exchangeEnv(t)
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	subj := subjectToken(t, maya, captured)
	sc := parseUnverified(t, subj)
	ok := func(aud string) map[string]any { return map[string]any{"subject_token": subj, "audience": aud} }
	before := e.auditCount("token.exchange_denied")

	// Client authentication.
	expectExchange(t, e.exchange("case-workspace", "wrong-secret", ok("flowcase"), nil), 401, "INVALID_CLIENT")
	expectExchange(t, e.exchange("case-workspace", "", ok("flowcase"), nil), 401, "INVALID_CLIENT")
	expectExchange(t, e.exchange("flowcase", exchangeSecret, ok("kms"), nil), 401, "INVALID_CLIENT")
	expectExchange(t, e.exchange("", "", ok("flowcase"), nil), 401, "INVALID_CLIENT")

	// Request shape and audience allowlist.
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": subj}, nil), 400, "INVALID_REQUEST")
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": subj, "audience": "flowcase", "grant_type": "client_credentials"}, nil), 400, "INVALID_REQUEST")
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": subj, "audience": "flowcase", "scope": "admin"}, nil), 400, "INVALID_REQUEST")
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("agent-studio"), nil), 403, "AUDIENCE_NOT_ALLOWED")
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("case-workspace"), nil), 403, "AUDIENCE_NOT_ALLOWED")
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("*"), nil), 403, "AUDIENCE_NOT_ALLOWED")

	// Subject token integrity.
	sessID := e.activeSessionID(subMaya)
	mint := func(aud, sub, session, actor string) string {
		tok, _, err := e.tokens.Mint(aud, integrations.Subject{UserID: sub, TenantKey: "acme-demo", WorkspaceKey: "sim-b2b", PrincipalType: "human",
			SessionID: session, Scopes: []string{"app:open"}, PolicyVersion: "x", CorrelationID: "x", AuthTime: time.Now(), Actor: actor})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	subjectCase := func(name, tok, code string) {
		t.Helper()
		r := e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": tok, "audience": "flowcase"}, nil)
		if r.status != 401 || r.body["code"] != code {
			t.Fatalf("%s: got %d %v, want 401 %s", name, r.status, r.body, code)
		}
	}
	foreignKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	subjectCase("minted for another audience", mint("flowcase", e.userID(subMaya), sessID, ""), "SUBJECT_TOKEN_AUDIENCE_MISMATCH")
	subjectCase("chained (act claim)", mint("case-workspace", e.userID(subMaya), sessID, "app:case-workspace"), "SUBJECT_TOKEN_DELEGATED")
	subjectCase("expired", e.forge(nil, nil, func(c jwt.MapClaims) {
		past := time.Now().Add(-20 * time.Minute)
		c["iat"], c["nbf"], c["exp"] = past.Unix(), past.Unix(), past.Add(5*time.Minute).Unix()
	}, sc), "INVALID_SUBJECT_TOKEN")
	subjectCase("lifetime > 15m", e.forge(nil, nil, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(time.Hour).Unix() }, sc), "INVALID_SUBJECT_TOKEN")
	subjectCase("forged (foreign key, real kid)", e.forge(jwt.SigningMethodES256, foreignKey, func(jwt.MapClaims) {}, sc), "INVALID_SUBJECT_TOKEN")
	subjectCase("alg none", e.forge(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, func(jwt.MapClaims) {}, sc), "INVALID_SUBJECT_TOKEN")
	subjectCase("HS256 confusion", e.forge(jwt.SigningMethodHS256, []byte(e.tokens.KeyID()), func(jwt.MapClaims) {}, sc), "INVALID_SUBJECT_TOKEN")
	subjectCase("wrong issuer", e.forge(nil, nil, func(c jwt.MapClaims) { c["iss"] = "https://evil.example" }, sc), "INVALID_SUBJECT_TOKEN")
	subjectCase("other tenant", e.forge(nil, nil, func(c jwt.MapClaims) { c["tenant_id"] = "other-tenant" }, sc), "INVALID_SUBJECT_TOKEN")
	subjectCase("service principal", e.forge(nil, nil, func(c jwt.MapClaims) { c["principal_type"] = "service" }, sc), "SUBJECT_NOT_HUMAN")
	subjectCase("tampered subject", e.forge(nil, nil, func(c jwt.MapClaims) { c["sub"] = e.userID(subRafi) }, sc), "SESSION_INACTIVE")
	// Contract-check / devtoken style tokens are validly signed but have no live session behind them.
	subjectCase("no live session (contract-check)", mint("case-workspace", e.userID(subMaya), "contract-check", ""), "SESSION_INACTIVE")
	subjectCase("garbage", "not-a-jwt", "INVALID_SUBJECT_TOKEN")

	// Policy: Dimas may open the UCW but not KMS -> the exchange cannot widen his access.
	dimas := e.as(subDimas)
	dimasTok := subjectToken(t, dimas, captured)
	r := e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": dimasTok, "audience": "kms"}, nil)
	expectExchange(t, r, 403, "TARGET_APP_DENIED")
	if !strings.Contains(r.body["message"].(string), "MISSING_PERMISSION") {
		t.Fatalf("target denial reason: %v", r.body)
	}
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": dimasTok, "audience": "flowcase"}, nil), 200, "")
	// Sari cannot open the UCW at all: even a validly signed client token for her live session is refused.
	sari := e.as(subSari)
	expectStatus(t, sari.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	sariTok := mint("case-workspace", e.userID(subSari), e.activeSessionID(subSari), "")
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": sariTok, "audience": "flowcase"}, nil), 403, "CLIENT_APP_DENIED")

	// Revoking Maya's sim-b2b access applies to the very next exchange.
	rafi := e.as(subRafi)
	ovr := expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/workspaces/"+e.workspaceID("sim-b2b")+"/overrides",
		map[string]any{"user_id": e.userID(subMaya), "permission_key": "flowcase:open", "effect": "deny", "reason": "exchange test deny"}, nil), 201)
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("flowcase"), nil), 403, "TARGET_APP_DENIED")
	expectStatus(t, rafi.do(http.MethodDelete, "/api/v1/admin/overrides/"+ovr["id"].(string), map[string]any{"reason": "exchange test cleanup"}, nil), 204)
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("flowcase"), nil), 200, "")

	// Switching the session's workspace makes older subject tokens stale.
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("legal-ops")}, nil), 204)
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("flowcase"), nil), 401, "SESSION_WORKSPACE_CHANGED")
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("flowcase"), nil), 200, "")

	// Logout revokes the session: the still-unexpired subject token can no longer be exchanged.
	expectStatus(t, maya.do(http.MethodPost, "/auth/logout", nil, nil), 200)
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, ok("flowcase"), nil), 401, "SESSION_INACTIVE")

	// Every denial is audited with the client and a reason code.
	if got := e.auditCount("token.exchange_denied") - before; got < 25 {
		t.Fatalf("denied exchanges audited: %d", got)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM platform_audit_events WHERE action = 'token.exchange_denied'
AND reason_code = 'MISSING_PERMISSION' AND target_id = 'kms' AND metadata->>'client_id' = 'case-workspace' AND decision = 'deny'`).Scan(&n)
	mustEqual(t, n, 1, "target denial audit row")
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM platform_audit_events WHERE action = 'token.exchange_denied'
AND reason_code = 'SESSION_INACTIVE' AND actor_user_id = $1::uuid`, e.userID(subMaya)).Scan(&n)
	if n < 2 {
		t.Fatalf("session-inactive denials audited: %d", n)
	}
	var leaked int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM platform_audit_events WHERE metadata::text LIKE '%eyJ%' OR metadata::text LIKE '%`+exchangeSecret+`%'`).Scan(&leaked)
	mustEqual(t, leaked, 0, "token or secret material in audit metadata")
}

func TestTokenExchangeDisabledWithoutClients(t *testing.T) {
	e := newEnv(t, envOptions{})
	expectExchange(t, e.exchange("case-workspace", exchangeSecret, map[string]any{"subject_token": "x", "audience": "flowcase"}, nil), 401, "INVALID_CLIENT")
}
