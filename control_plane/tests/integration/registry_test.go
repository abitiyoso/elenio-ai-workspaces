package integration

import (
	"context"
	"crypto"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/elenio/control-plane/internal/platform/config"
)

func manifest(key, version string) map[string]any {
	return map[string]any{"schema_version": "workspace.app.v1", "app_key": key, "display_name": "Bid Insights", "description": "Example app",
		"category": "business", "icon": "chart", "owner_team": "Insights", "deployment_mode": "same_origin_route",
		"route_base": "/apps/" + key + "/", "environment": "dev", "version": version, "status": "draft",
		"required_entitlements": []string{"kms:open"}, "workspace_allowlist": []string{"sim-b2b"}, "health_route": "/readyz",
		"requires_domain_authorization": true}
}

func TestRegistryValidationLifecycleAndImmutability(t *testing.T) {
	e := newEnv(t, envOptions{})
	rafi := e.as(subRafi)
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)

	bad := map[string]func(m map[string]any){
		"traversal":         func(m map[string]any) { m["route_base"] = "/apps/insights/../admin/" },
		"encoded traversal": func(m map[string]any) { m["route_base"] = "/apps/%2e%2e/" },
		"protocol relative": func(m map[string]any) { m["route_base"] = "//evil.example/" },
		"absolute url":      func(m map[string]any) { m["route_base"] = "https://evil.example/apps/insights/" },
		"javascript":        func(m map[string]any) { m["route_base"] = "javascript:alert(1)" },
		"key mismatch":      func(m map[string]any) { m["route_base"] = "/apps/kms/" },
		"upstream field":    func(m map[string]any) { m["upstream_url"] = "http://10.0.0.5:8080" },
		"iframe":            func(m map[string]any) { m["deployment_mode"] = "iframe" },
		"script":            func(m map[string]any) { m["description"] = "<script>alert(1)</script>" },
		"icon url":          func(m map[string]any) { m["icon"] = "https://evil.example/x.svg" },
		"no domain authz":   func(m map[string]any) { m["requires_domain_authorization"] = false },
		"published status":  func(m map[string]any) { m["status"] = "published" },
	}
	for name, mutate := range bad {
		m := manifest("insights", "1.0.0")
		mutate(m)
		r := expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps", map[string]any{"manifest": m, "reason": "register insights"}, nil), 422)
		if r["code"] != "MANIFEST_INVALID" || r["issues"] == nil {
			t.Fatalf("%s: %v", name, r)
		}
	}
	// Duplicate route base / key.
	r := expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps", map[string]any{"manifest": manifest("kms", "9.0.0"), "reason": "dup"}, nil), 409)
	mustEqual(t, r["code"].(string), "APP_CONFLICT", "duplicate")

	app := expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps", map[string]any{"manifest": manifest("insights", "1.0.0"), "reason": "register insights"}, nil), 201)
	mustEqual(t, app["status"].(string), "draft", "new app status")
	if slices.Contains(appKeys(maya.refreshCSRF()), "insights") {
		t.Fatal("draft app visible in catalog")
	}
	rev := int(app["revision"].(float64))
	r = expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps/insights/publish", map[string]any{"version": "1.0.0", "reason": "go live"}, nil), 428)
	mustEqual(t, r["code"].(string), "IF_MATCH_REQUIRED", "missing If-Match")
	expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps/insights/publish", map[string]any{"version": "1.0.0", "reason": "go live"},
		map[string]string{"If-Match": `"999"`}), 412)
	app = expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps/insights/publish", map[string]any{"version": "1.0.0", "reason": "go live"},
		map[string]string{"If-Match": `"` + itoa(rev) + `"`}), 200)
	mustEqual(t, app["status"].(string), "published", "published")
	if !slices.Contains(appKeys(maya.refreshCSRF()), "insights") {
		t.Fatal("published app not visible to entitled user")
	}
	dimas := e.as(subDimas)
	if slices.Contains(appKeys(dimas.refreshCSRF()), "insights") {
		t.Fatal("published app visible to user without entitlement")
	}

	// Versions must increase and keep the route; the published manifest row is immutable.
	r = expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps/insights/versions", map[string]any{"manifest": manifest("insights", "0.9.0"), "reason": "downgrade"}, nil), 422)
	expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps/insights/versions", map[string]any{"manifest": manifest("insights", "1.1.0"), "reason": "new draft"}, nil), 201)
	if _, err := e.pool.Exec(context.Background(), `UPDATE mini_app_versions SET manifest = '{}'::jsonb WHERE version = '1.0.0'`); err == nil {
		t.Fatal("manifest update allowed")
	}
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM mini_app_versions WHERE version = '1.0.0'`); err == nil {
		t.Fatal("manifest delete allowed")
	}

	// Disable: catalog and direct route deny on the very next request.
	app = expectStatus(t, rafi.do(http.MethodGet, "/api/v1/admin/apps/insights", nil, nil), 200)
	rev = int(app["revision"].(float64))
	expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/apps/insights/disable", map[string]any{"reason": "incident"},
		map[string]string{"If-Match": `"` + itoa(rev) + `"`}), 200)
	if slices.Contains(appKeys(maya.refreshCSRF()), "insights") {
		t.Fatal("disabled app still in catalog")
	}
	r = expectStatus(t, maya.do(http.MethodGet, "/api/v1/apps/insights/launch", nil, nil), 403)
	mustEqual(t, r["code"].(string), "APP_DISABLED", "disabled launch")
	resp := maya.do(http.MethodGet, "/apps/insights/", nil, nil)
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 403, "disabled direct route")
	var enabledRoutes int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM mini_app_routes WHERE registered_path = '/apps/insights/' AND enabled`).Scan(&enabledRoutes)
	mustEqual(t, enabledRoutes, 0, "routes after disable")
	if e.auditCount("app.published") != 1 || e.auditCount("app.disabled") != 1 {
		t.Fatal("registry lifecycle not audited")
	}
}

// downstreamVerifier mirrors Flowcase's trusted_jwt AuthAdapter (internal/platform/auth/jwt.go): ES256/RS256
// only, exact issuer and audience, exp+iat required, lifetime <= 15m, kid from JWKS, tenant claim present.
type downstreamVerifier struct {
	issuer, audience, jwksURL string
}

func (v downstreamVerifier) verify(raw string) (jwt.MapClaims, error) {
	resp, err := http.Get(v.jwksURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, err
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "ES256"}), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(time.Minute))
	claims := jwt.MapClaims{}
	_, err = parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		for _, k := range set.Keys {
			var jwk struct {
				Kid string `json:"kid"`
			}
			_ = json.Unmarshal(k, &jwk)
			if jwk.Kid == kid {
				return parseECJWK(k)
			}
		}
		return nil, jwt.ErrTokenUnverifiable
	})
	if err != nil {
		return nil, err
	}
	iat, _ := claims.GetIssuedAt()
	exp, _ := claims.GetExpirationTime()
	if exp.Sub(iat.Time) > 15*time.Minute {
		return nil, jwt.ErrTokenInvalidClaims
	}
	if tenant, _ := claims["tenant_id"].(string); tenant == "" {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

func TestProxyForwardsAudienceBoundTokenAndStripsForgedIdentity(t *testing.T) {
	var mu sync.Mutex
	var seen []http.Header
	var verifyErr []error
	var cpOrigin string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Header.Clone())
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims, err := downstreamVerifier{issuer: cpOrigin, audience: "kms", jwksURL: cpOrigin + "/.well-known/jwks.json"}.verify(raw)
		verifyErr = append(verifyErr, err)
		// The same token presented to another service (wrong audience) must be rejected there.
		_, wrongAud := downstreamVerifier{issuer: cpOrigin, audience: "flowcase", jwksURL: cpOrigin + "/.well-known/jwks.json"}.verify(raw)
		verifyErr = append(verifyErr, boolErr(wrongAud == nil, "token accepted for wrong audience"))
		if err != nil {
			w.WriteHeader(401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "cp_session", Value: "hijack"})
		http.SetCookie(w, &http.Cookie{Name: "kms_pref", Value: "1"})
		_ = json.NewEncoder(w).Encode(map[string]any{"path": r.URL.Path, "sub": claims["sub"], "workspace": claims["workspace_id"]})
	}))
	t.Cleanup(upstream.Close)
	e := newEnv(t, envOptions{upstreams: map[string]config.Upstream{
		"kms":      {URL: upstream.URL, Audience: "kms", Mode: "proxy", StripPrefix: true, Contract: "test"},
		"flowcase": {Mode: "pending", PendingReason: "Flowcase trusted_jwt not enabled in this deployment"},
	}})
	cpOrigin = e.cfg.Origin()
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	l := expectStatus(t, maya.do(http.MethodGet, "/api/v1/apps/kms/launch", nil, nil), 200)
	mustEqual(t, l["integration"].(string), "proxy", "kms integration")

	resp := maya.do(http.MethodGet, "/apps/kms/api/v1/documents?q=1", nil, map[string]string{
		"X-User-ID": e.userID(subRafi), "X-Workspace-ID": "legal-ops", "X-Role": "platform_admin", "Authorization": "Bearer forged",
		"Tailscale-User-Login": "rafi@example.invalid", "X-Forwarded-For": "1.2.3.4"})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 200, "proxied status: "+string(body))
	for _, c := range resp.Cookies() {
		if c.Name == "cp_session" {
			t.Fatal("upstream was able to overwrite the session cookie")
		}
	}
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	mustEqual(t, out["path"].(string), "/api/v1/documents", "stripped prefix path")
	mustEqual(t, out["sub"].(string), e.userID(subMaya), "token subject")
	mustEqual(t, out["workspace"].(string), "sim-b2b", "token workspace")
	mu.Lock()
	h := seen[0]
	errs := verifyErr
	mu.Unlock()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("downstream verification: %v", err)
		}
	}
	for _, k := range []string{"X-User-Id", "X-Workspace-Id", "X-Role", "Tailscale-User-Login", "Cookie"} {
		if h.Get(k) != "" {
			t.Fatalf("forged/identity header %s reached upstream: %q", k, h.Get(k))
		}
	}
	if strings.Contains(h.Get("X-Forwarded-For"), "1.2.3.4") {
		t.Fatal("client-supplied X-Forwarded-For forwarded")
	}

	// Cross-origin unsafe request through the shell route is refused before reaching the upstream.
	resp = maya.do(http.MethodPost, "/apps/kms/api/v1/documents", map[string]string{"x": "y"}, map[string]string{"Origin": "https://evil.example"})
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 403, "cross-origin proxied POST")

	// Pending integration: authorised but honestly not forwarded.
	resp = maya.do(http.MethodGet, "/apps/flowcase/", nil, map[string]string{"Accept": "text/html"})
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 503, "pending integration")
	if !strings.Contains(string(page), "Integration pending") {
		t.Fatal("pending page missing explanation")
	}
	// Revocation applies to the proxied route on the next request.
	rafi := e.as(subRafi)
	expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/memberships/"+e.membershipID(subMaya, "sim-b2b")+"/revoke", map[string]any{"reason": "offboarded"}, nil), 200)
	resp = maya.do(http.MethodGet, "/apps/kms/api/v1/documents", nil, nil)
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 403, "proxied route after revoke")
	mu.Lock()
	mustEqual(t, len(seen), 1, "upstream calls")
	mu.Unlock()
}

func boolErr(cond bool, msg string) error {
	if cond {
		return &simpleErr{msg}
	}
	return nil
}

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

var _ crypto.PublicKey
