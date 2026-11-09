package http

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/workspaces"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := s.ready(ctx)
	status := http.StatusOK
	for k, v := range checks {
		if k != "idp" && v != "ok" { // the IdP being down blocks new logins, not readiness
			status = http.StatusServiceUnavailable
		}
	}
	state := "ready"
	if status != http.StatusOK {
		state = "not_ready"
	}
	writeJSON(w, status, map[string]any{"status": state, "checks": checks})
}

func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	body, _ := json.Marshal(s.tokens.JWKS())
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(body)
}

type capabilities struct {
	ViewPeople        bool     `json:"view_people"`
	ManageWorkspaces  bool     `json:"manage_workspaces"`
	ManageMemberships []string `json:"manage_memberships"`
	ManageRegistry    bool     `json:"manage_registry"`
	ViewAudit         bool     `json:"view_audit"`
	ViewIntegrations  bool     `json:"view_integrations"`
	ApproveGrants     bool     `json:"approve_grants"`
	SuspendUsers      bool     `json:"suspend_users"`
}

// ensureWorkspace keeps the session's active workspace valid: if the stored one is no longer an active
// membership, the first available workspace is selected server-side (or none).
func (s *Server) ensureWorkspace(ctx context.Context, p *principal.Principal, list []workspaces.Workspace) *workspaces.Workspace {
	for i := range list {
		if list[i].ID == p.WorkspaceID {
			return &list[i]
		}
	}
	next := ""
	if len(list) > 0 {
		next = list[0].ID
	}
	if next != p.WorkspaceID {
		if next == "" {
			_, _ = s.pool.Exec(ctx, `UPDATE sessions SET workspace_id = NULL WHERE id = $1::uuid`, p.SessionID)
		} else {
			_ = s.sessions.SetWorkspace(ctx, s.pool, p.SessionID, next)
		}
		p.WorkspaceID = next
	}
	if next == "" {
		return nil
	}
	return &list[0]
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	ctx := r.Context()
	list, err := s.workspaces.UserWorkspaces(ctx, p.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	active := s.ensureWorkspace(ctx, &p, list)
	catalog, err := s.apps.Catalog(ctx, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	perms := []string{}
	if active != nil {
		facts, err := s.engine.Facts(ctx, nil, p.UserID, p.WorkspaceID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		perms = authorization.EffectivePermissions(facts)
	}
	caps, err := s.capabilities(ctx, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var email *string
	if p.Email != "" {
		email = &p.Email
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":           map[string]any{"id": p.UserID, "display_name": p.DisplayName, "email": email, "principal_type": p.PrincipalType},
		"tenant":         map[string]any{"id": p.TenantID, "key": p.TenantKey, "name": p.TenantName},
		"workspace":      active,
		"workspaces":     list,
		"apps":           catalog,
		"permissions":    perms,
		"capabilities":   caps,
		"csrf_token":     s.sessions.CSRFToken(p.SessionID),
		"session":        map[string]any{"expires_at": p.AbsExpiresAt.UTC(), "idle_expires_at": p.IdleExpiresAt.UTC()},
		"environment":    s.cfg.Env,
		"policy_version": s.engine.PolicyVersion,
	})
}

func (s *Server) capabilities(ctx context.Context, p principal.Principal) (capabilities, error) {
	c := capabilities{ManageMemberships: []string{}}
	f, err := s.engine.Facts(ctx, nil, p.UserID, "")
	if err != nil {
		return c, err
	}
	has := func(perm string) bool {
		return authorization.DecideTenantPermission(f, perm, "", time.Now()).Allowed()
	}
	managed, err := s.workspaces.ManagedWorkspaces(ctx, p)
	if err != nil {
		return c, err
	}
	c.ManageMemberships = managed
	c.ViewPeople = has("users:manage") || len(managed) > 0
	c.ManageWorkspaces = has("workspaces:create")
	c.ManageRegistry = has("registry:manage")
	c.ViewAudit = has("audit:view")
	c.ViewIntegrations = has("integrations:view")
	c.ApproveGrants = has("grants:approve")
	c.SuspendUsers = has("users:manage")
	return c, nil
}

func (s *Server) handleSwitchWorkspace(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.workspaces.SwitchWorkspace(r.Context(), p, in.WorkspaceID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	list, err := s.workspaces.UserWorkspaces(r.Context(), p.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	list, err := s.apps.Catalog(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	res, d, err := s.apps.Launch(r.Context(), p, r.PathValue("app_key"))
	s.countDecision(d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) countDecision(d *authorization.Decision) {
	if d != nil && s.metrics != nil {
		s.metrics.AuthzDecision.WithLabelValues(d.Effect, d.ReasonCode).Inc()
	}
}

// handleAuthzCheck evaluates an action for the verified caller only; the body cannot name a subject.
func (s *Server) handleAuthzCheck(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in struct {
		Action       string `json:"action"`
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx, corr := r.Context(), observability.RequestIDFrom(r.Context())
	var d authorization.Decision
	var err error
	switch {
	case in.ResourceType == authorization.ResourceMiniApp && in.Action == authorization.ActionAppOpen:
		d, err = s.engine.App(ctx, p.UserID, p.WorkspaceID, in.ResourceID, corr)
		if err == nil && p.WorkspaceID == "" {
			d.Effect, d.ReasonCode = "deny", authorization.ReasonNotMember
		}
	case in.ResourceType == authorization.ResourceWorkspace && in.Action == authorization.ActionWorkspaceSwitch && workspaces.ValidUUID(in.ResourceID):
		d, err = s.engine.Switch(ctx, p.UserID, in.ResourceID, corr)
	case in.ResourceType == authorization.ResourcePlatformAdmin && validPermission(in.Action):
		d, err = s.engine.Tenant(ctx, nil, p.UserID, in.Action, corr)
	default:
		s.fail(w, r, apperr.BadRequest("UNSUPPORTED_CHECK", "supported: mini_app/app:open, workspace/workspace:switch, platform_admin/<permission>"))
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d.Trace = nil
	s.countDecision(&d)
	if !d.Allowed() {
		_ = audit.Record(ctx, s.pool, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: d.WorkspaceID,
			Action: "authz.check_denied", TargetType: d.ResourceType, TargetID: clip(d.ResourceID, 128), Decision: "deny",
			ReasonCode: d.ReasonCode, PolicyVersion: d.PolicyVersion, Metadata: map[string]any{"action": d.Action}})
	}
	writeJSON(w, http.StatusOK, d)
}

var permissionPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}:[a-z][a-z0-9-]{0,40}$`)

func validPermission(p string) bool { return permissionPattern.MatchString(p) }

// ---------- integrations ----------

func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	d, err := s.engine.Tenant(r.Context(), nil, p.UserID, "integrations:view", observability.RequestIDFrom(r.Context()))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !d.Allowed() {
		s.fail(w, r, apperr.Forbidden(d.ReasonCode, "not permitted: integrations:view"))
		return
	}
	st := s.oidc.Status()
	idpState := st.State
	if idpState == "unknown" {
		idpState = "unavailable"
	}
	var checked *time.Time
	if !st.LastChecked.IsZero() {
		t := st.LastChecked.UTC()
		checked = &t
	}
	active, _ := s.sessions.CountActive(r.Context())
	registry, err := s.apps.List(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	adapters := []map[string]any{}
	seen := map[string]bool{}
	addAdapter := func(key, name string) {
		seen[key] = true
		up, ok := s.upstreams.Get(key)
		var aud, pending any
		contract := "not configured in TRUSTED_APP_UPSTREAMS"
		if ok {
			contract = "not documented in deployment config"
			if up.Audience != "" {
				aud = up.Audience
			}
			if up.Contract != "" {
				contract = up.Contract
			}
			if up.Mode == "pending" {
				pending = up.PendingReason
			}
		} else {
			pending = "no trusted upstream is configured for this app in this deployment"
		}
		adapters = append(adapters, map[string]any{"app_key": key, "display_name": name, "audience": aud,
			"integration": s.upstreams.Integration(key), "contract": contract, "pending_reason": pending, "health": s.upstreams.Health(key)})
	}
	for _, a := range registry {
		addAdapter(a.AppKey, a.DisplayName)
	}
	for _, k := range s.upstreams.Keys() {
		if !seen[k] {
			addAdapter(k, k)
		}
	}
	rows, err := s.pool.Query(r.Context(), `SELECT client_id, audience, COALESCE(ARRAY(SELECT jsonb_array_elements_text(scopes)), '{}'), status, description FROM service_identities ORDER BY client_id`)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	ids := []map[string]any{}
	for rows.Next() {
		var clientID, aud, status, desc string
		var scopes []string
		if err := rows.Scan(&clientID, &aud, &scopes, &status, &desc); err != nil {
			s.fail(w, r, err)
			return
		}
		ids = append(ids, map[string]any{"client_id": clientID, "audience": aud, "scopes": scopes, "status": status, "description": desc})
	}
	sameSite := "Lax"
	writeJSON(w, http.StatusOK, map[string]any{
		"identity_provider": map[string]any{"issuer": s.oidc.Issuer(), "client_id": s.oidc.ClientID(), "status": idpState,
			"last_checked_at": checked, "detail": st.Detail, "scopes": s.oidc.Scopes()},
		"session": map[string]any{"cookie_name": s.cfg.CookieName(), "cookie_secure": s.cfg.CookieSecure, "same_site": sameSite,
			"idle_ttl_seconds": int(s.cfg.SessionIdleTTL.Seconds()), "absolute_ttl_seconds": int(s.cfg.SessionAbsTTL.Seconds()), "active_sessions": active},
		"service_tokens": map[string]any{"issuer": s.tokens.IssuerURL(), "algorithm": "ES256", "key_ids": s.tokens.KeyIDs(),
			"ttl_seconds": int(s.tokens.TTL().Seconds()), "jwks_path": "/.well-known/jwks.json"},
		"adapters":           adapters,
		"service_identities": ids,
	})
}
