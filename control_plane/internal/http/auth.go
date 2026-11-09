package http

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/identity/oidc"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/identity/provisioning"
	"github.com/elenio/control-plane/internal/platform/postgres"
)

// safeReturnTo accepts only same-origin relative paths; anything else falls back to "/" (no open redirect).
func safeReturnTo(raw string) string {
	if raw == "" || len(raw) > 512 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.ContainsAny(raw, "\\\r\n\t\x00") || strings.HasPrefix(raw, "/auth/") {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || strings.HasPrefix(u.Path, "//") {
		return "/"
	}
	if strings.Contains(u.Path, "/../") || strings.HasSuffix(u.Path, "/..") || strings.Contains(strings.ToLower(u.RawPath), "%2f") {
		return "/"
	}
	return u.RequestURI()
}

func (s *Server) cookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode} // Lax: the IdP redirect back is a cross-site top-level GET
}

func (s *Server) authMetric(event, outcome string) {
	if s.metrics != nil {
		s.metrics.AuthEvents.WithLabelValues(event, outcome).Inc()
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, s.authLimit, "login", "login:"+clientIP(r)) {
		return
	}
	verifier := oauth2.GenerateVerifier()
	flow, err := s.sessions.BeginFlow(r.Context(), verifier, safeReturnTo(r.URL.Query().Get("return_to")))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	target, err := s.oidc.AuthCodeURL(r.Context(), flow.State, flow.Nonce, flow.Verifier)
	if err != nil {
		s.authMetric("login", "idp_unavailable")
		s.log.Warn("idp_unavailable", "error", err.Error())
		writePage(w, r, http.StatusServiceUnavailable, page{Title: "Sign-in is temporarily unavailable",
			Lines: []string{"The identity provider cannot be reached, so new sign-ins are paused. Existing sessions are unaffected.",
				"Please try again in a few minutes."}, Code: "idp_unavailable", Href: "/auth/login", Action: "Try again"})
		return
	}
	http.SetCookie(w, s.cookie(s.cfg.FlowCookieName(), flow.Binding, int(s.cfg.AuthFlowTTL.Seconds())))
	s.authMetric("login", "redirected")
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) loginFailed(w http.ResponseWriter, r *http.Request, code string, meta map[string]any) {
	s.authMetric("callback", code)
	_ = audit.Record(r.Context(), s.pool, audit.Event{ActorType: "system", TenantID: s.tenantID, Action: "auth.login_failed",
		TargetType: "session", TargetID: "-", Decision: "deny", ReasonCode: strings.ToUpper(code), Metadata: meta})
	http.SetCookie(w, s.cookie(s.cfg.FlowCookieName(), "", -1))
	http.Redirect(w, r, "/signed-out?error="+url.QueryEscape(code), http.StatusFound)
}

// handleCallback validates state (one-time, browser-bound), the RFC 9207 iss parameter, exchanges the code
// with the PKCE verifier, verifies the ID token and nonce, binds the directory user and issues a fresh session.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, s.authLimit, "callback", "cb:"+clientIP(r)) {
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		s.loginFailed(w, r, "idp_error", map[string]any{"idp_error": clip(q.Get("error"), 64)})
		return
	}
	if iss := q.Get("iss"); iss != "" && iss != s.oidc.Issuer() {
		s.loginFailed(w, r, "wrong_issuer", nil)
		return
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" || len(code) > 2048 {
		s.loginFailed(w, r, "invalid_request", nil)
		return
	}
	binding := ""
	if c, err := r.Cookie(s.cfg.FlowCookieName()); err == nil {
		binding = c.Value
	}
	flow, err := s.sessions.ConsumeFlow(r.Context(), state, binding)
	if err != nil {
		s.loginFailed(w, r, "state_invalid", nil)
		return
	}
	id, tok, err := s.oidc.Exchange(r.Context(), code, flow.Verifier, flow.Nonce)
	if err != nil {
		s.loginFailed(w, r, oidc.Code(err), nil)
		return
	}
	if id.Issuer != s.oidc.Issuer() {
		s.loginFailed(w, r, "wrong_issuer", nil)
		return
	}
	var raw, userID string
	err = postgres.WithTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		u, err := provisioning.BindLogin(r.Context(), tx, s.tenantID, id, true)
		if err != nil {
			return err
		}
		userID = u.ID
		// Session fixation defence: any session presented with this login is revoked; a new id is issued.
		if c, err := r.Cookie(s.cfg.CookieName()); err == nil {
			if old, err := s.sessions.Lookup(r.Context(), c.Value); err == nil {
				if _, err := s.sessions.Revoke(r.Context(), tx, old.ID, "replaced_by_login"); err != nil {
					return err
				}
			}
		}
		var ws string
		_ = tx.QueryRow(r.Context(), `SELECT w.id::text FROM workspace_memberships m JOIN workspaces w ON w.id = m.workspace_id
WHERE m.user_id = $1::uuid AND m.state = 'active' AND w.status = 'active' ORDER BY w.name LIMIT 1`, u.ID).Scan(&ws)
		refresh := ""
		if tok != nil {
			refresh = tok.RefreshToken
		}
		var sessID string
		raw, sessID, err = s.createSession(r.Context(), tx, u.ID, ws, id.Issuer, id.AuthTime, refresh, r.UserAgent())
		if err != nil {
			return err
		}
		action := "session.created"
		if u.Created {
			action = "session.created_new_user"
		}
		return audit.Record(r.Context(), tx, audit.Event{ActorUserID: u.ID, TenantID: s.tenantID, WorkspaceID: ws, Action: action,
			TargetType: "session", TargetID: sessID, Decision: "allow", Metadata: map[string]any{"issuer": id.Issuer}})
	})
	switch {
	case errors.Is(err, provisioning.ErrSuspended):
		s.loginFailed(w, r, "subject_disabled", map[string]any{"user_id": userID})
		return
	case errors.Is(err, provisioning.ErrNotProvisioned), errors.Is(err, provisioning.ErrWrongTenant):
		s.loginFailed(w, r, "not_provisioned", nil)
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	s.authMetric("callback", "success")
	http.SetCookie(w, s.cookie(s.cfg.FlowCookieName(), "", -1))
	http.SetCookie(w, s.cookie(s.cfg.CookieName(), raw, int(s.cfg.SessionAbsTTL.Seconds())))
	http.Redirect(w, r, flow.ReturnTo, http.StatusFound)
}

func (s *Server) createSession(ctx context.Context, tx pgx.Tx, userID, ws, issuer string, authTime time.Time, refresh, ua string) (string, string, error) {
	raw, sess, err := s.sessions.Create(ctx, tx, userID, s.tenantID, ws, issuer, authTime, refresh, ua)
	return raw, sess.ID, err
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var refresh string
	err := postgres.WithTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		var err error
		if refresh, err = s.sessions.Revoke(r.Context(), tx, p.SessionID, "logout"); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "session.logout",
			TargetType: "session", TargetID: p.SessionID})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if refresh != "" {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		if err := s.oidc.Revoke(ctx, refresh); err != nil {
			s.log.Warn("idp_token_revocation_failed", "error", err.Error())
		}
		cancel()
	}
	s.authMetric("logout", "success")
	http.SetCookie(w, s.cookie(s.cfg.CookieName(), "", -1))
	redirect := s.oidc.EndSessionURL()
	if redirect == "" {
		redirect = "/signed-out"
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": redirect})
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
