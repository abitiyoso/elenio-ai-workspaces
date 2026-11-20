package http

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/control-plane/internal/apps"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/identity/oidc"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/identity/session"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/workspaces"
)

type Server struct {
	cfg        *config.Config
	log        *slog.Logger
	pool       *pgxpool.Pool
	oidc       *oidc.Client
	sessions   *session.Store
	engine     *authorization.Engine
	workspaces *workspaces.Service
	apps       *apps.Service
	tokens     *integrations.Issuer
	upstreams  *integrations.Upstreams
	metrics    *observability.Metrics
	tenantID   string
	ready      func(context.Context) map[string]string

	authLimit  *limiter
	apiLimit   *limiter
	writeLimit *limiter
	proxies    map[string]http.Handler
}

type Deps struct {
	Config     *config.Config
	Logger     *slog.Logger
	Pool       *pgxpool.Pool
	OIDC       *oidc.Client
	Sessions   *session.Store
	Engine     *authorization.Engine
	Workspaces *workspaces.Service
	Apps       *apps.Service
	Tokens     *integrations.Issuer
	Upstreams  *integrations.Upstreams
	Metrics    *observability.Metrics
	TenantID   string
	Ready      func(context.Context) map[string]string
}

func New(d Deps) *Server {
	s := &Server{cfg: d.Config, log: d.Logger, pool: d.Pool, oidc: d.OIDC, sessions: d.Sessions, engine: d.Engine,
		workspaces: d.Workspaces, apps: d.Apps, tokens: d.Tokens, upstreams: d.Upstreams, metrics: d.Metrics,
		tenantID: d.TenantID, ready: d.Ready,
		authLimit:  newLimiter(d.Config.RateLimitAuth, 10),
		apiLimit:   newLimiter(d.Config.RateLimitAPI, 60),
		writeLimit: newLimiter(d.Config.RateLimitAdminW, 10),
	}
	s.proxies = s.buildProxies()
	return s
}

// Handler builds the route table. Every /api/v1 route except health requires a session; mutating routes
// also pass the CSRF/origin check inside withSession.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.instrument(pattern, h)) }
	authed := func(pattern string, h func(http.ResponseWriter, *http.Request, principal.Principal)) {
		route(pattern, s.withSession(h))
	}

	route("GET /healthz", s.handleHealthz)
	route("GET /readyz", s.handleReadyz)
	route("GET /.well-known/jwks.json", s.handleJWKS)
	// Server-to-server delegated token exchange (ADR-004): Basic client auth, no cookie, so no CSRF/Origin check.
	route("POST /api/v1/token-exchange", s.handleTokenExchange)

	route("GET /auth/login", s.handleLogin)
	route("GET /auth/callback", s.handleCallback)
	authed("POST /auth/logout", s.handleLogout)

	authed("GET /api/v1/me", s.handleMe)
	authed("POST /api/v1/session/workspace", s.handleSwitchWorkspace)
	authed("GET /api/v1/workspaces", s.handleWorkspaces)
	authed("GET /api/v1/solutions", s.handleSolutions)
	authed("POST /api/v1/solutions", s.handleCreateSolution)
	authed("GET /api/v1/solutions/{id}", s.handleSolution)
	authed("GET /api/v1/solutions/{id}/readiness", s.handleSolutionReadiness)
	authed("GET /api/v1/solutions/{id}/executions", s.handleSolutionExecutions)
	authed("POST /api/v1/solutions/{id}/executions", s.handleCreateSolutionExecution)
	authed("GET /api/v1/solutions/{id}/candidates", s.handleCandidates)
	authed("POST /api/v1/solutions/{id}/candidates", s.handleCreateCandidate)
	authed("GET /api/v1/solutions/{id}/candidates/{candidate_id}", s.handleCandidate)
	authed("PATCH /api/v1/solutions/{id}", s.handleUpdateSolution)
	authed("GET /api/v1/apps", s.handleCatalog)
	authed("GET /api/v1/apps/{app_key}/launch", s.handleLaunch)
	authed("POST /api/v1/authz/check", s.handleAuthzCheck)

	authed("GET /api/v1/admin/users", s.handleListUsers)
	authed("POST /api/v1/admin/users", s.handleCreateUser)
	authed("POST /api/v1/admin/users/{id}/suspend", s.handleUserStatus("suspended"))
	authed("POST /api/v1/admin/users/{id}/activate", s.handleUserStatus("active"))
	authed("POST /api/v1/admin/users/{id}/sessions/revoke", s.handleRevokeSessions)
	authed("PUT /api/v1/admin/users/{id}/tenant-roles", s.handleGrantTenantRole)
	authed("GET /api/v1/admin/workspaces", s.handleAdminWorkspaces)
	authed("POST /api/v1/admin/workspaces", s.handleCreateWorkspace)
	authed("GET /api/v1/admin/workspaces/{id}/members", s.handleListMembers)
	authed("POST /api/v1/admin/workspaces/{id}/members", s.handleAddMember)
	authed("POST /api/v1/admin/memberships/{id}/suspend", s.handleMembershipState("suspend"))
	authed("POST /api/v1/admin/memberships/{id}/reinstate", s.handleMembershipState("reinstate"))
	authed("POST /api/v1/admin/memberships/{id}/revoke", s.handleMembershipState("revoke"))
	authed("PUT /api/v1/admin/memberships/{id}/roles", s.handleGrantWorkspaceRole)
	authed("DELETE /api/v1/admin/role-bindings/{id}", s.handleRevokeBinding)
	authed("GET /api/v1/admin/grant-requests", s.handleListGrantRequests)
	authed("POST /api/v1/admin/grant-requests/{id}/approve", s.handleDecideGrant(true))
	authed("POST /api/v1/admin/grant-requests/{id}/reject", s.handleDecideGrant(false))
	authed("GET /api/v1/admin/roles", s.handleRoles)
	authed("GET /api/v1/admin/permissions", s.handlePermissions)
	authed("GET /api/v1/admin/workspaces/{id}/overrides", s.handleListOverrides)
	authed("POST /api/v1/admin/workspaces/{id}/overrides", s.handleCreateOverride)
	authed("DELETE /api/v1/admin/overrides/{id}", s.handleDeleteOverride)
	authed("POST /api/v1/admin/access-preview", s.handleAccessPreview)
	authed("GET /api/v1/admin/apps", s.handleListApps)
	authed("POST /api/v1/admin/apps", s.handleCreateApp)
	authed("GET /api/v1/admin/apps/{key}", s.handleGetApp)
	authed("POST /api/v1/admin/apps/{key}/versions", s.handleAddVersion)
	authed("POST /api/v1/admin/apps/{key}/publish", s.handlePublish)
	authed("POST /api/v1/admin/apps/{key}/disable", s.handleSetEnabled(false))
	authed("POST /api/v1/admin/apps/{key}/enable", s.handleSetEnabled(true))
	authed("GET /api/v1/admin/audit", s.handleAudit)
	authed("GET /api/v1/admin/integrations", s.handleIntegrations)

	// Unknown API routes are JSON 404s, never the SPA.
	route("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "unknown API route")
	})
	route("/auth/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "unknown auth route")
	})
	route("/apps/", s.handleAppRoute)
	route("/", s.handleStatic)

	return s.correlate(s.securityHeaders(mux))
}

// ---------- middleware ----------

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) correlate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := observability.RequestID(r.Header.Get("X-Request-ID"))
		w.Header().Set("X-Request-ID", id)
		ctx := observability.WithRequestID(r.Context(), id)
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "request_id", id, "path", r.URL.Path, "panic", rec)
				writeError(sw, r.WithContext(ctx), http.StatusInternalServerError, "INTERNAL", "unexpected error")
			}
			// Paths only; query strings can carry OAuth codes and are never logged.
			s.log.Info("http_request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", sw.status,
				"duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

func (s *Server) instrument(pattern string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		h(sw, r)
		if s.metrics != nil {
			s.metrics.HTTPRequests.WithLabelValues(pattern, strconv.Itoa(sw.status/100)+"xx").Inc()
			s.metrics.HTTPDuration.WithLabelValues(pattern).Observe(time.Since(start).Seconds())
		}
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	csp := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; " +
		"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if !strings.HasPrefix(r.URL.Path, "/apps/") { // proxied mini-apps keep their own CSP (set in the proxy)
			h.Set("Content-Security-Policy", csp)
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if s.cfg.CookieSecure {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) limited(w http.ResponseWriter, r *http.Request, l *limiter, bucket, key string) bool {
	if l.allow(key) {
		return false
	}
	if s.metrics != nil {
		s.metrics.RateLimited.WithLabelValues(bucket).Inc()
	}
	w.Header().Set("Retry-After", "2")
	writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests; retry shortly")
	return true
}

// resolve returns the verified principal for the request's session cookie, or false. Only the cookie is
// consulted; X-User-*, X-Tenant-*, X-Workspace-* and similar headers are never read.
func (s *Server) resolve(r *http.Request) (principal.Principal, bool, error) {
	c, err := r.Cookie(s.cfg.CookieName())
	if err != nil || !session.WellFormed(c.Value) {
		return principal.Principal{}, false, nil
	}
	sess, err := s.sessions.Lookup(r.Context(), c.Value)
	if errors.Is(err, session.ErrNoSession) {
		return principal.Principal{}, false, nil
	}
	if err != nil {
		return principal.Principal{}, false, err
	}
	p := principal.Principal{UserID: sess.UserID, TenantID: sess.TenantID, SessionID: sess.ID, WorkspaceID: sess.WorkspaceID,
		Issuer: sess.Issuer, AuthTime: sess.AuthTime, IdleExpiresAt: sess.IdleExpiresAt, AbsExpiresAt: sess.AbsExpiresAt}
	var status, tenantStatus string
	var email *string
	err = s.pool.QueryRow(r.Context(), `SELECT u.display_name, u.email, u.principal_type, u.status, u.idp_subject, t.key, t.name, t.status
FROM users u JOIN tenants t ON t.id = u.tenant_id WHERE u.id = $1::uuid AND u.tenant_id = $2::uuid`, sess.UserID, sess.TenantID).
		Scan(&p.DisplayName, &email, &p.PrincipalType, &status, &p.Subject, &p.TenantKey, &p.TenantName, &tenantStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return principal.Principal{}, false, nil
	}
	if err != nil {
		return principal.Principal{}, false, err
	}
	if email != nil {
		p.Email = *email
	}
	if status != "active" || tenantStatus != "active" {
		// Fail closed and make sure no other session of a disabled subject survives.
		_, _ = s.sessions.RevokeUser(r.Context(), s.pool, sess.UserID, "subject_disabled")
		return principal.Principal{}, false, nil
	}
	return p, true, nil
}

// withSession enforces authentication, per-session rate limits, and CSRF + Origin for unsafe methods.
func (s *Server) withSession(h func(http.ResponseWriter, *http.Request, principal.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok, err := s.resolve(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !ok {
			if s.limited(w, r, s.authLimit, "unauthenticated", "anon:"+clientIP(r)) {
				return
			}
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in required")
			return
		}
		if s.limited(w, r, s.apiLimit, "api", "s:"+p.SessionID) {
			return
		}
		if !safeMethod(r.Method) {
			if !s.sameOrigin(r) {
				writeError(w, r, http.StatusForbidden, "ORIGIN_REJECTED", "cross-origin request rejected")
				return
			}
			if !s.sessions.ValidCSRF(p.SessionID, r.Header.Get("X-CSRF-Token")) {
				writeError(w, r, http.StatusForbidden, "CSRF_INVALID", "missing or invalid CSRF token")
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && s.limited(w, r, s.writeLimit, "admin_write", "w:"+p.SessionID) {
				return
			}
		}
		h(w, r.WithContext(principal.With(r.Context(), p)), p)
	}
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// sameOrigin requires Origin (or, when absent, Referer) to equal the configured public origin exactly.
func (s *Server) sameOrigin(r *http.Request) bool {
	want := s.cfg.Origin()
	if o := r.Header.Get("Origin"); o != "" {
		return o == want
	}
	ref, err := url.Parse(r.Header.Get("Referer"))
	if err != nil || ref.Host == "" {
		return false
	}
	return ref.Scheme+"://"+ref.Host == want
}
