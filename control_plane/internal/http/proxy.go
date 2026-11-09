package http

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/observability"
)

var appRouteKey = regexp.MustCompile(`^/apps/([a-z][a-z0-9-]{2,63})(/.*)?$`)

// strippedRequestHeaders are never forwarded: identity must come only from the minted token.
var strippedRequestHeaders = []string{"Cookie", "Authorization", "Proxy-Authorization", "Forwarded", "X-Forwarded-For",
	"X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip", "X-Csrf-Token", "X-Request-Id"}

var strippedPrefixes = []string{"X-User", "X-Tenant", "X-Workspace", "X-Role", "X-Auth", "X-Forwarded-User", "X-Remote-User",
	"Tailscale-", "X-Flowcase-Dev", "X-Kms-", "X-Agent-Studio", "X-Principal", "X-Subject"}

func stripIdentityHeaders(h http.Header) {
	for _, k := range strippedRequestHeaders {
		h.Del(k)
	}
	for k := range h {
		for _, p := range strippedPrefixes {
			if strings.HasPrefix(strings.ToLower(k), strings.ToLower(p)) {
				h.Del(k)
				break
			}
		}
	}
}

type proxyCtxKey struct{}

type proxyContext struct {
	token string
	cid   string
}

// buildProxies creates one reverse proxy per trusted upstream in "proxy" mode. Targets are fixed at start
// from deployment config; nothing in a request or manifest can change them.
func (s *Server) buildProxies() map[string]http.Handler {
	out := map[string]http.Handler{}
	for _, key := range s.upstreams.Keys() {
		up, _ := s.upstreams.Get(key)
		if up.Mode != "proxy" {
			continue
		}
		target, err := url.Parse(up.URL)
		if err != nil {
			continue
		}
		prefix := "/apps/" + key
		strip := up.StripPrefix
		rp := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pc, _ := pr.In.Context().Value(proxyCtxKey{}).(proxyContext)
				path := pr.In.URL.Path
				if strip {
					path = strings.TrimPrefix(path, prefix)
					if path == "" {
						path = "/"
					}
				}
				pr.Out.URL.Scheme, pr.Out.URL.Host = target.Scheme, target.Host
				pr.Out.URL.Path = singleJoin(target.Path, path)
				pr.Out.URL.RawPath = ""
				pr.Out.URL.RawQuery = pr.In.URL.RawQuery
				pr.Out.Host = target.Host
				stripIdentityHeaders(pr.Out.Header)
				pr.SetXForwarded()
				pr.Out.Header.Set("Authorization", "Bearer "+pc.token)
				pr.Out.Header.Set("X-Request-ID", pc.cid)
				pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
			},
			ModifyResponse: func(resp *http.Response) error {
				// Upstreams may not set cookies that collide with Control Plane session cookies.
				cookies := resp.Header.Values("Set-Cookie")
				resp.Header.Del("Set-Cookie")
				for _, c := range cookies {
					name := strings.TrimSpace(strings.SplitN(c, "=", 2)[0])
					if strings.HasPrefix(name, "cp_") || strings.HasPrefix(name, "__Host-cp_") {
						continue
					}
					resp.Header.Add("Set-Cookie", c)
				}
				if resp.Header.Get("Content-Security-Policy") == "" {
					resp.Header.Set("Content-Security-Policy", "frame-ancestors 'self'; object-src 'none'; base-uri 'self'")
				}
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				s.log.Warn("app_proxy_upstream_error", "app", key, "request_id", observability.RequestIDFrom(r.Context()), "error", err.Error())
				s.proxyMetric(key, "upstream_error")
				writePage(w, r, http.StatusBadGateway, page{Title: "The app is not responding",
					Lines: []string{"Your access was verified, but the service did not respond. Try again shortly."}, Code: "UPSTREAM_UNAVAILABLE"})
			},
		}
		out[key] = rp
	}
	return out
}

func singleJoin(base, p string) string {
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

func (s *Server) proxyMetric(app, outcome string) {
	if s.metrics != nil {
		s.metrics.ProxyRequests.WithLabelValues(app, outcome).Inc()
	}
}

// handleAppRoute is the authorised same-origin mini-app route. It applies the same decision as the catalog
// and launch endpoints on every request, so revocation and app disable take effect immediately.
func (s *Server) handleAppRoute(w http.ResponseWriter, r *http.Request) {
	m := appRouteKey.FindStringSubmatch(r.URL.Path)
	lowerRaw := strings.ToLower(r.URL.EscapedPath())
	if m == nil || strings.Contains(r.URL.Path, "/../") || strings.HasSuffix(r.URL.Path, "/..") || strings.Contains(r.URL.Path, "//") ||
		strings.Contains(lowerRaw, "%2e") || strings.Contains(lowerRaw, "%2f") || strings.Contains(lowerRaw, "%5c") || strings.Contains(r.URL.Path, "\\") {
		writePage(w, r, http.StatusNotFound, page{Title: "Unknown app route", Lines: []string{"This path is not a registered mini-app route."}, Code: "ROUTE_NOT_FOUND"})
		return
	}
	key := m[1]
	if m[2] == "" {
		http.Redirect(w, r, "/apps/"+key+"/", http.StatusFound)
		return
	}
	p, ok, err := s.resolve(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.proxyMetric(key, "unauthenticated")
		if safeMethod(r.Method) && wantsHTML(r) {
			http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in required")
		return
	}
	if s.limited(w, r, s.apiLimit, "app_route", "s:"+p.SessionID) {
		return
	}
	// Cookie-authenticated unsafe requests through the shell must be same-origin (CSRF on mini-app APIs).
	if !safeMethod(r.Method) && !s.sameOrigin(r) {
		writeError(w, r, http.StatusForbidden, "ORIGIN_REJECTED", "cross-origin request rejected")
		return
	}
	cid := observability.RequestIDFrom(r.Context())
	d, err := s.engine.App(r.Context(), p.UserID, p.WorkspaceID, key, cid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p.WorkspaceID == "" {
		d.Effect, d.ReasonCode = "deny", authorization.ReasonNotMember
	}
	s.countDecision(&d)
	if !d.Allowed() {
		s.proxyMetric(key, "denied")
		_ = audit.Record(r.Context(), s.pool, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID,
			Action: "authz.denied", TargetType: "mini_app", TargetID: key, Decision: "deny", ReasonCode: d.ReasonCode,
			PolicyVersion: d.PolicyVersion, Metadata: map[string]any{"action": "app:open", "route": "/apps/" + key + "/", "method": r.Method}})
		if wantsHTML(r) {
			writePage(w, r, http.StatusForbidden, page{Title: "You don't have access to this app",
				Lines: []string{"Access is decided by your workspace membership and app entitlements. Ask a workspace admin if you need access."},
				Code:  d.ReasonCode})
			return
		}
		writeError(w, r, http.StatusForbidden, d.ReasonCode, "app is not available in this workspace")
		return
	}
	proxy, ok := s.proxies[key]
	if !ok {
		s.proxyMetric(key, "pending")
		reason := "No trusted upstream is configured for this app in this deployment."
		if up, found := s.upstreams.Get(key); found && up.PendingReason != "" {
			reason = up.PendingReason
		}
		writePage(w, r, http.StatusServiceUnavailable, page{Title: "Integration pending",
			Lines: []string{"Your access to this app is authorised, but the Control Plane does not forward traffic to it yet.", reason,
				"Nothing is simulated: the service must verify Control Plane identity tokens before its route is enabled."},
			Code: "INTEGRATION_PENDING"})
		return
	}
	up, _ := s.upstreams.Get(key)
	var wsKey string
	_ = s.pool.QueryRow(r.Context(), `SELECT key FROM workspaces WHERE id = $1::uuid`, p.WorkspaceID).Scan(&wsKey)
	token, _, err := s.tokens.Mint(up.Audience, integrations.Subject{UserID: p.UserID, TenantKey: p.TenantKey, WorkspaceKey: wsKey,
		IdPIssuer: p.Issuer, IdPSubject: p.Subject, PrincipalType: principal.TypeHuman, SessionID: p.SessionID,
		Scopes: appScopes(d), PolicyVersion: d.PolicyVersion, CorrelationID: cid, AuthTime: p.AuthTime})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.proxyMetric(key, "forwarded")
	ctx := r.Context()
	r = r.WithContext(contextWith(ctx, proxyContext{token: token, cid: cid}))
	r.Header.Del("Cookie") // belt and braces: never visible to the proxy director
	proxy.ServeHTTP(w, r)
}

// appScopes are the platform entitlements relevant to the app (never admin permissions).
func appScopes(d authorization.Decision) []string {
	return append([]string{"app:open"}, d.EffectiveRoles...)
}
