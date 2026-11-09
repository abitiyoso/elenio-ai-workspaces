package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/elenio/control-plane/internal/apps"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/workspaces"
)

type reasonBody struct {
	Reason string `json:"reason"`
}

// ---------- people ----------

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	users, err := s.workspaces.ListUsers(r.Context(), p, r.URL.Query().Get("q"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in workspaces.CreateUserInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.workspaces.CreateUser(r.Context(), p, in, s.oidc.Issuer())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUserStatus(status string) func(http.ResponseWriter, *http.Request, principal.Principal) {
	return func(w http.ResponseWriter, r *http.Request, p principal.Principal) {
		var in reasonBody
		if err := decode(r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
		u, err := s.workspaces.SetUserStatus(r.Context(), p, r.PathValue("id"), status, in.Reason)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	}
}

func (s *Server) handleRevokeSessions(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in reasonBody
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	n, err := s.workspaces.RevokeUserSessions(r.Context(), p, r.PathValue("id"), in.Reason)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
}

func grantStatus(res *workspaces.GrantResult) int {
	if res.Outcome == "pending_approval" {
		return http.StatusAccepted
	}
	return http.StatusCreated
}

func (s *Server) handleGrantTenantRole(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in workspaces.GrantInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.workspaces.GrantTenantRole(r.Context(), p, r.PathValue("id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, grantStatus(res), res)
}

// ---------- workspaces & memberships ----------

func (s *Server) handleAdminWorkspaces(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	list, err := s.workspaces.ListWorkspaces(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in workspaces.CreateWorkspaceInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ws, err := s.workspaces.CreateWorkspace(r.Context(), p, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	list, err := s.workspaces.ListMembers(r.Context(), p, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in struct {
		UserID string `json:"user_id"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	m, err := s.workspaces.AddMember(r.Context(), p, r.PathValue("id"), in.UserID, in.Reason)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleMembershipState(action string) func(http.ResponseWriter, *http.Request, principal.Principal) {
	return func(w http.ResponseWriter, r *http.Request, p principal.Principal) {
		var in reasonBody
		if err := decode(r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
		m, err := s.workspaces.SetMembershipState(r.Context(), p, r.PathValue("id"), action, in.Reason)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}

func (s *Server) handleGrantWorkspaceRole(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in workspaces.GrantInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.workspaces.GrantWorkspaceRole(r.Context(), p, r.PathValue("id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, grantStatus(res), res)
}

func (s *Server) handleRevokeBinding(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in reasonBody
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.workspaces.RevokeBinding(r.Context(), p, r.PathValue("id"), in.Reason); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListGrantRequests(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	list, err := s.workspaces.ListGrantRequests(r.Context(), p, r.URL.Query().Get("state"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleDecideGrant(approve bool) func(http.ResponseWriter, *http.Request, principal.Principal) {
	return func(w http.ResponseWriter, r *http.Request, p principal.Principal) {
		var in reasonBody
		if err := decode(r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
		g, err := s.workspaces.DecideGrantRequest(r.Context(), p, r.PathValue("id"), approve, in.Reason)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, g)
	}
}

func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	roles, err := s.workspaces.Roles(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": roles, "policy_version": s.engine.PolicyVersion})
}

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	perms, err := s.workspaces.Permissions(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": perms})
}

func (s *Server) handleListOverrides(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	list, err := s.workspaces.ListOverrides(r.Context(), p, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleCreateOverride(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in workspaces.OverrideInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	o, err := s.workspaces.CreateOverride(r.Context(), p, r.PathValue("id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) handleDeleteOverride(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in reasonBody
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.workspaces.DeleteOverride(r.Context(), p, r.PathValue("id"), in.Reason); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAccessPreview(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in struct {
		UserID      string `json:"user_id"`
		WorkspaceID string `json:"workspace_id"`
		AppKey      string `json:"app_key"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.workspaces.AccessPreview(r.Context(), p, in.UserID, in.WorkspaceID, in.AppKey)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// ---------- registry ----------

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	list, err := s.apps.List(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	a, err := s.apps.Get(r.Context(), p, r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in apps.DraftInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.apps.Create(r.Context(), p, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleAddVersion(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in apps.DraftInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.apps.AddVersion(r.Context(), p, r.PathValue("key"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in apps.PublishInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	rev, err := apps.ParseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.apps.Publish(r.Context(), p, r.PathValue("key"), rev, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleSetEnabled(enable bool) func(http.ResponseWriter, *http.Request, principal.Principal) {
	return func(w http.ResponseWriter, r *http.Request, p principal.Principal) {
		var in reasonBody
		if err := decode(r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
		rev, err := apps.ParseIfMatch(r.Header.Get("If-Match"))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		a, err := s.apps.SetEnabled(r.Context(), p, r.PathValue("key"), rev, enable, in.Reason)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, a)
	}
}

// ---------- audit ----------

type auditEvent struct {
	ID            string          `json:"id"`
	CreatedAt     time.Time       `json:"created_at"`
	ActorUserID   *string         `json:"actor_user_id"`
	ActorName     *string         `json:"actor_name"`
	ActorType     string          `json:"actor_type"`
	WorkspaceID   *string         `json:"workspace_id"`
	Action        string          `json:"action"`
	TargetType    string          `json:"target_type"`
	TargetID      string          `json:"target_id"`
	Decision      *string         `json:"decision"`
	ReasonCode    *string         `json:"reason_code"`
	PolicyVersion *string         `json:"policy_version"`
	CorrelationID string          `json:"correlation_id"`
	Rationale     *string         `json:"rationale"`
	Metadata      json.RawMessage `json:"metadata"`
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	d, err := s.engine.Tenant(r.Context(), nil, p.UserID, "audit:view", observability.RequestIDFrom(r.Context()))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !d.Allowed() {
		s.fail(w, r, apperr.Forbidden(d.ReasonCode, "not permitted: audit:view"))
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var cursor int64
	if c := q.Get("cursor"); c != "" {
		if cursor, err = strconv.ParseInt(c, 10, 64); err != nil || cursor < 0 {
			s.fail(w, r, apperr.BadRequest("INVALID_CURSOR", "cursor is invalid"))
			return
		}
	}
	actor, ws := q.Get("actor_user_id"), q.Get("workspace_id")
	if (actor != "" && !workspaces.ValidUUID(actor)) || (ws != "" && !workspaces.ValidUUID(ws)) {
		s.fail(w, r, apperr.BadRequest("INVALID_FILTER", "filters must be UUIDs"))
		return
	}
	action := clip(q.Get("action"), 80)
	rows, err := s.pool.Query(r.Context(), `SELECT e.seq, e.created_at, e.actor_user_id::text, u.display_name, e.actor_type, e.workspace_id::text,
  e.action, e.target_type, e.target_id, e.decision, e.reason_code, e.policy_version, e.correlation_id, e.rationale, e.metadata
FROM platform_audit_events e LEFT JOIN users u ON u.id = e.actor_user_id
WHERE (e.tenant_id = $1::uuid OR e.tenant_id IS NULL)
  AND ($2::bigint = 0 OR e.seq < $2) AND ($3 = '' OR e.action = $3 OR e.action LIKE $3 || '.%')
  AND ($4 = '' OR e.actor_user_id = NULLIF($4, '')::uuid) AND ($5 = '' OR e.workspace_id = NULLIF($5, '')::uuid)
ORDER BY e.seq DESC LIMIT $6`, p.TenantID, cursor, action, actor, ws, limit+1)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []auditEvent{}
	var seqs []int64
	for rows.Next() {
		var e auditEvent
		var seq int64
		if err := rows.Scan(&seq, &e.CreatedAt, &e.ActorUserID, &e.ActorName, &e.ActorType, &e.WorkspaceID, &e.Action, &e.TargetType,
			&e.TargetID, &e.Decision, &e.ReasonCode, &e.PolicyVersion, &e.CorrelationID, &e.Rationale, &e.Metadata); err != nil {
			s.fail(w, r, err)
			return
		}
		e.ID = strconv.FormatInt(seq, 10)
		items = append(items, e)
		seqs = append(seqs, seq)
	}
	var next any
	if len(items) > limit {
		items = items[:limit]
		next = strconv.FormatInt(seqs[limit-1], 10)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}
