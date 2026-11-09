package integration

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginHappyPathMeHasNoTokens(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.browser()
	code, state := e.idp.authorize(t, b.startLogin("/catalog?x=1"), subMaya)
	q := url.Values{"code": {code}, "state": {state}, "iss": {e.idp.srv.URL}}
	resp := b.do(http.MethodGet, "/auth/callback?"+q.Encode(), nil, nil)
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, http.StatusFound, "callback status")
	mustEqual(t, resp.Header.Get("Location"), "/catalog?x=1", "return_to")
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "cp_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode || sessionCookie.Path != "/" || sessionCookie.Domain != "" {
		t.Fatalf("session cookie attributes wrong: %+v", sessionCookie)
	}
	raw := b.do(http.MethodGet, "/api/v1/me", nil, nil)
	defer raw.Body.Close()
	var body strings.Builder
	buf := make([]byte, 1<<16)
	for {
		n, err := raw.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}
	s := body.String()
	for _, leaked := range []string{"eyJ", "at-" + subMaya, "rt-" + subMaya, "refresh", "access_token", "id_token"} {
		if strings.Contains(s, leaked) {
			t.Fatalf("/me leaked token material %q", leaked)
		}
	}
	b.refreshCSRF()
	me := b.refreshCSRF()
	ws := me["workspace"].(map[string]any)
	mustEqual(t, ws["key"].(string), "legal-ops", "default workspace is first by name")
	// Session store keeps only a hash of the cookie value.
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE token_hash = convert_to($1, 'UTF8')`, sessionCookie.Value).Scan(&n)
	mustEqual(t, n, 0, "raw cookie value stored")
}

func loginError(t *testing.T, loc string) string {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil || u.Path != "/signed-out" {
		t.Fatalf("expected /signed-out redirect, got %q", loc)
	}
	return u.Query().Get("error")
}

func TestStateReplayAndLoginCSRF(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.browser()
	code, state := e.idp.authorize(t, b.startLogin(""), subMaya)
	if loc := b.callback(code, state, nil); loc != "/" {
		t.Fatalf("first callback failed: %s", loc)
	}
	// Replaying the same state (even with a fresh code) is refused: state is one-time.
	code2, _ := e.idp.authorize(t, e.browser().startLogin(""), subMaya)
	mustEqual(t, loginError(t, b.callback(code2, state, nil)), "state_invalid", "state replay")

	// Login CSRF: a valid state from another browser cannot be completed without its binding cookie.
	victim := e.browser()
	attacker := e.browser()
	code3, state3 := e.idp.authorize(t, attacker.startLogin(""), subDimas)
	mustEqual(t, loginError(t, victim.callback(code3, state3, nil)), "state_invalid", "state without binding cookie")
}

func TestCallbackRejectsMixUpTamperedTokensAndCodeReplay(t *testing.T) {
	e := newEnv(t, envOptions{})
	cases := []struct {
		name string
		set  func()
		want string
	}{
		{"wrong id_token issuer", func() { e.idp.tokenIssuer = "https://evil.example" }, "wrong_issuer"},
		{"wrong audience", func() { e.idp.tokenAudience = "another-client" }, "wrong_audience"},
		{"expired id token", func() { e.idp.tokenTTL = -2 * time.Minute }, "id_token_expired"},
		{"nonce mismatch", func() { e.idp.tokenNonce = "forged-nonce" }, "nonce_mismatch"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.idp.tokenIssuer, e.idp.tokenAudience, e.idp.tokenTTL, e.idp.tokenNonce = "", "", 0, ""
			c.set()
			b := e.browser()
			code, state := e.idp.authorize(t, b.startLogin(""), subMaya)
			mustEqual(t, loginError(t, b.callback(code, state, nil)), c.want, c.name)
		})
	}
	e.idp.tokenIssuer, e.idp.tokenAudience, e.idp.tokenTTL, e.idp.tokenNonce = "", "", 0, ""

	// RFC 9207 iss parameter mismatch (IdP mix-up).
	b := e.browser()
	code, state := e.idp.authorize(t, b.startLogin(""), subMaya)
	mustEqual(t, loginError(t, b.callback(code, state, url.Values{"iss": {"https://evil.example"}})), "wrong_issuer", "iss param")

	// Authorization code injection: a code bound to another PKCE challenge fails at the token endpoint.
	victim, attacker := e.browser(), e.browser()
	stolen, _ := e.idp.authorize(t, attacker.startLogin(""), subRafi)
	_, vState := e.idp.authorize(t, victim.startLogin(""), subMaya)
	mustEqual(t, loginError(t, victim.callback(stolen, vState, nil)), "code_rejected", "code injection")
	if e.auditCount("auth.login_failed") < 6 {
		t.Fatalf("login failures not audited")
	}
}

func TestOpenRedirectRejected(t *testing.T) {
	e := newEnv(t, envOptions{})
	for _, rt := range []string{"https://evil.example/", "//evil.example/x", "/\\evil.example", "/auth/logout", "javascript:alert(1)"} {
		b := e.browser()
		code, state := e.idp.authorize(t, b.startLogin(rt), subMaya)
		mustEqual(t, b.callback(code, state, nil), "/", "return_to "+rt)
	}
}

func TestSessionRotationLogoutAndExpiry(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subMaya)
	oldCookies := b.c.Jar.Cookies(mustURL(e.srv.URL))
	// Logging in again from the same browser issues a new session id and revokes the old one.
	b.login(subMaya)
	stale := e.browser()
	stale.c.Jar.SetCookies(mustURL(e.srv.URL), oldCookies)
	expectStatus(t, stale.do(http.MethodGet, "/api/v1/me", nil, nil), 401)

	// Logout requires CSRF + same origin, then revokes server-side and revokes the refresh token at the IdP.
	expectStatus(t, b.do(http.MethodPost, "/auth/logout", nil, map[string]string{"X-CSRF-Token": ""}), 403)
	expectStatus(t, b.do(http.MethodPost, "/auth/logout", nil, map[string]string{"Origin": "https://evil.example"}), 403)
	cookies := b.c.Jar.Cookies(mustURL(e.srv.URL))
	out := expectStatus(t, b.do(http.MethodPost, "/auth/logout", nil, nil), 200)
	if !strings.HasPrefix(out["redirect"].(string), e.idp.srv.URL+"/logout?") || strings.Contains(out["redirect"].(string), "eyJ") {
		t.Fatalf("unexpected logout redirect %v", out["redirect"])
	}
	replay := e.browser()
	replay.c.Jar.SetCookies(mustURL(e.srv.URL), cookies)
	expectStatus(t, replay.do(http.MethodGet, "/api/v1/me", nil, nil), 401)
	e.idp.mu.Lock()
	revoked := strings.Join(e.idp.revoked, ",")
	e.idp.mu.Unlock()
	if !strings.Contains(revoked, "rt-"+subMaya) {
		t.Fatalf("refresh token not revoked at IdP: %q", revoked)
	}

	// Idle and absolute expiry.
	idle := e.as(subDimas)
	_, _ = e.pool.Exec(context.Background(), `UPDATE sessions SET idle_expires_at = now() - interval '1 second' WHERE user_id = $1::uuid`, e.userID(subDimas))
	expectStatus(t, idle.do(http.MethodGet, "/api/v1/me", nil, nil), 401)
	abs := e.as(subSari)
	_, _ = e.pool.Exec(context.Background(), `UPDATE sessions SET absolute_expires_at = now() - interval '1 second' WHERE user_id = $1::uuid AND revoked_at IS NULL`, e.userID(subSari))
	expectStatus(t, abs.do(http.MethodGet, "/api/v1/me", nil, nil), 401)
}

func TestForgedHeadersAndCorruptCookiesNeverAuthenticate(t *testing.T) {
	e := newEnv(t, envOptions{})
	rafi := e.userID(subRafi)
	b := e.browser()
	forged := map[string]string{"X-User-ID": rafi, "X-Workspace-ID": e.workspaceID("sim-b2b"), "X-Role": "platform_admin",
		"X-Tenant-ID": e.tenantID, "Tailscale-User-Login": "rafi@example.invalid", "Authorization": "Bearer eyJhbGciOiJub25lIn0.e30."}
	expectStatus(t, b.do(http.MethodGet, "/api/v1/me", nil, forged), 401)
	expectStatus(t, b.do(http.MethodGet, "/api/v1/admin/users", nil, forged), 401)
	for _, v := range []string{"garbage", strings.Repeat("A", 43), "../../etc/passwd", strings.Repeat("x", 5000)} {
		c := e.browser()
		c.c.Jar.SetCookies(mustURL(e.srv.URL), []*http.Cookie{{Name: "cp_session", Value: v}})
		expectStatus(t, c.do(http.MethodGet, "/api/v1/me", nil, nil), 401)
	}
	// A real session with forged headers still acts as the real user only.
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodGet, "/api/v1/admin/users", nil, forged), 403)
}

func TestCSRFAndOriginOnMutations(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subMaya)
	body := map[string]string{"workspace_id": e.workspaceID("sim-b2b")}
	r := expectStatus(t, b.do(http.MethodPost, "/api/v1/session/workspace", body, map[string]string{"X-CSRF-Token": ""}), 403)
	mustEqual(t, r["code"].(string), "CSRF_INVALID", "missing csrf")
	r = expectStatus(t, b.do(http.MethodPost, "/api/v1/session/workspace", body, map[string]string{"X-CSRF-Token": "wrong"}), 403)
	mustEqual(t, r["code"].(string), "CSRF_INVALID", "wrong csrf")
	r = expectStatus(t, b.do(http.MethodPost, "/api/v1/session/workspace", body, map[string]string{"Origin": "https://evil.example"}), 403)
	mustEqual(t, r["code"].(string), "ORIGIN_REJECTED", "cross origin")
	r = expectStatus(t, b.do(http.MethodPost, "/api/v1/session/workspace", body, map[string]string{"Origin": "", "Referer": ""}), 403)
	mustEqual(t, r["code"].(string), "ORIGIN_REJECTED", "no origin")
	// Text/plain bodies (simple CORS requests) are refused.
	expectStatus(t, b.do(http.MethodPost, "/api/v1/session/workspace", body, map[string]string{"Content-Type": "text/plain"}), 415)
	// CSRF tokens are bound to one session.
	other := e.as(subDimas)
	r = expectStatus(t, b.do(http.MethodPost, "/api/v1/session/workspace", body, map[string]string{"X-CSRF-Token": other.csrf}), 403)
	mustEqual(t, r["code"].(string), "CSRF_INVALID", "foreign csrf")
	expectStatus(t, b.do(http.MethodPost, "/api/v1/session/workspace", body, nil), 204)
}

func TestIdPUnavailableFailsClosedForNewLogins(t *testing.T) {
	e := newEnv(t, envOptions{})
	e.idp.srv.Close() // no discovery has happened yet in this process
	resp := e.browser().do(http.MethodGet, "/auth/login", nil, map[string]string{"Accept": "text/html"})
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, http.StatusServiceUnavailable, "login while IdP down")
	// The server keeps serving and denies unauthenticated API calls (no silent bypass).
	expectStatus(t, e.browser().do(http.MethodGet, "/api/v1/me", nil, nil), 401)
}

func mustURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}
