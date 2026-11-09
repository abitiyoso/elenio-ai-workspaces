package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
)

var (
	catalogIDPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	catalogSemverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

type CatalogStore interface {
	CreateSkill(context.Context, string, string, postgres.SkillInput) (postgres.Skill, postgres.SkillVersion, error)
	ListSkills(context.Context, string, string) ([]postgres.Skill, error)
	ListSkillsPage(context.Context, string, string, int, int) ([]postgres.Skill, int, error)
	UpdateSkillDomain(context.Context, string, string, string, string) (postgres.Skill, error)
	DeleteDraftSkill(context.Context, string, string, string) error
	ListSkillVersions(context.Context, string, string) ([]postgres.SkillVersion, error)
	ListSkillVersionsPage(context.Context, string, string, int, int) ([]postgres.SkillVersion, int, error)
	CreateSkillVersion(context.Context, string, string, postgres.SkillVersionInput) (postgres.SkillVersion, error)
	UpdateSkillVersion(context.Context, string, string, postgres.SkillVersionInput, string) (postgres.SkillVersion, error)
	PublishSkillVersion(context.Context, string, string, string) (postgres.SkillVersion, error)
	ListAgentVersions(context.Context, string, string) ([]postgres.AgentVersion, error)
	ListAgentVersionsPage(context.Context, string, string, int, int) ([]postgres.AgentVersion, int, error)
	CreateAgentVersion(context.Context, string, string, postgres.AgentVersionInput) (postgres.AgentVersion, error)
	UpdateAgentVersion(context.Context, string, string, postgres.AgentVersionInput, string) (postgres.AgentVersion, error)
	PublishAgentVersion(context.Context, string, string, string) (postgres.AgentVersion, error)
	ListTools(context.Context) ([]postgres.ToolVersion, error)
	ListToolsPage(context.Context, int, int) ([]postgres.ToolVersion, int, error)
	ListAgentSkillBindings(context.Context, string, string) ([]postgres.BoundSkill, error)
	BindSkill(context.Context, string, string, string, string) error
	UnbindSkill(context.Context, string, string, string, string) error
	ListAgentToolBindings(context.Context, string, string) ([]postgres.BoundTool, error)
	BindTool(context.Context, string, string, string, string, json.RawMessage) error
	UnbindTool(context.Context, string, string, string, string) error
}

// CatalogHandler serves tenant-owned skills and version/binding metadata. The
// global tool catalog is read-only here; the migration gives it no tenant/RLS
// boundary, so tenant writes to tool definitions would exceed this contract.
type CatalogHandler struct {
	Registry   CatalogStore
	Auth       ports.AuthAdapter
	Authorizer ports.Authorizer
}

func RegisterCatalogRoutes(mux *http.ServeMux, registry CatalogStore, auth ports.AuthAdapter, authorizer ports.Authorizer) {
	handler := CatalogHandler{Registry: registry, Auth: auth, Authorizer: authorizer}
	mux.Handle("/api/v1/catalog/", handler)
}

func (h CatalogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil || h.Auth == nil || h.Authorizer == nil {
		writeError(w, http.StatusServiceUnavailable, "catalog registry is not configured")
		return
	}
	principal, err := h.Auth.VerifyRequest(r.Context(), r)
	if err != nil || strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.Subject) == "" {
		writeError(w, http.StatusUnauthorized, "verified tenant identity is required")
		return
	}
	resource := "tenant:" + principal.TenantID
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/catalog/"), "/")
	parts := strings.Split(path, "/")

	switch {
	case path == "skills":
		h.skills(w, r, principal, resource)
	case len(parts) == 2 && parts[0] == "skills":
		h.skill(w, r, principal, resource, parts[1])
	case len(parts) == 3 && parts[0] == "skills" && parts[2] == "versions":
		h.skillVersions(w, r, principal, resource, parts[1])
	case len(parts) == 3 && parts[0] == "skill-versions" && parts[2] == "publish":
		h.publishSkill(w, r, principal, resource, parts[1])
	case len(parts) == 2 && parts[0] == "skill-versions":
		h.updateSkillVersion(w, r, principal, resource, parts[1])
	case len(parts) == 3 && parts[0] == "agents" && parts[2] == "versions":
		h.agentVersions(w, r, principal, resource, parts[1])
	case len(parts) == 3 && parts[0] == "agent-versions" && parts[2] == "publish":
		h.publishAgent(w, r, principal, resource, parts[1])
	case len(parts) == 2 && parts[0] == "agent-versions":
		h.updateAgentVersion(w, r, principal, resource, parts[1])
	case path == "tools":
		h.tools(w, r, principal)
	case len(parts) == 3 && parts[0] == "agent-versions" && parts[2] == "skills":
		h.agentSkills(w, r, principal, resource, parts[1], "")
	case len(parts) == 4 && parts[0] == "agent-versions" && parts[2] == "skills":
		h.agentSkills(w, r, principal, resource, parts[1], parts[3])
	case len(parts) == 3 && parts[0] == "agent-versions" && parts[2] == "tools":
		h.agentTools(w, r, principal, resource, parts[1], "")
	case len(parts) == 4 && parts[0] == "agent-versions" && parts[2] == "tools":
		h.agentTools(w, r, principal, resource, parts[1], parts[3])
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h CatalogHandler) skill(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, id string) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "skill id must be a UUID")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if !h.allow(w, r, p, "skill.update", resource) {
			return
		}
		var body struct {
			Domain string `json:"domain"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Domain = strings.TrimSpace(body.Domain)
		if body.Domain == "" || len(body.Domain) > 100 {
			writeError(w, http.StatusBadRequest, "domain is required and must be within limits")
			return
		}
		item, err := h.Registry.UpdateSkillDomain(r.Context(), p.TenantID, id, body.Domain, p.Subject)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not update skill")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if !h.allow(w, r, p, "skill.delete", resource) {
			return
		}
		err := h.Registry.DeleteDraftSkill(r.Context(), p.TenantID, id, p.Subject)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusConflict, "skill with published versions or immutable bindings cannot be deleted")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func (h CatalogHandler) skills(w http.ResponseWriter, r *http.Request, p ports.Principal, resource string) {
	agentKey := strings.TrimSpace(r.URL.Query().Get("agent_key"))
	if !validAgentKey(agentKey) {
		writeError(w, http.StatusBadRequest, "agent_key is required and must be valid")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.allow(w, r, p, "skill.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		items, total, err := h.Registry.ListSkillsPage(r.Context(), p.TenantID, agentKey, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list skills")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.allow(w, r, p, "skill.create", resource) {
			return
		}
		var body struct {
			Key    string `json:"key"`
			Domain string `json:"domain"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Key = strings.TrimSpace(body.Key)
		body.Domain = strings.TrimSpace(body.Domain)
		if !validAgentKey(body.Key) || body.Domain == "" || len(body.Domain) > 100 {
			writeError(w, http.StatusBadRequest, "key and domain are required and must be within limits")
			return
		}
		item, version, err := h.Registry.CreateSkill(r.Context(), p.TenantID, agentKey, postgres.SkillInput{Key: body.Key, Domain: body.Domain, CreatedBy: p.Subject})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "agent not found")
			} else {
				writeError(w, http.StatusConflict, "could not create skill")
			}
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"skill": item, "initial_version": version})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h CatalogHandler) skillVersions(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, skillID string) {
	if !validCatalogID(skillID) {
		writeError(w, http.StatusBadRequest, "skill id must be a UUID")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.allow(w, r, p, "skill.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		items, total, err := h.Registry.ListSkillVersionsPage(r.Context(), p.TenantID, skillID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list skill versions")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.allow(w, r, p, "skill.version.create", resource) {
			return
		}
		var body skillVersionRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if !validVersionRequest(body.Semver, body.Instructions, body.OutputSchema) {
			writeError(w, http.StatusBadRequest, "valid semver, instructions, and object output_schema are required")
			return
		}
		item, err := h.Registry.CreateSkillVersion(r.Context(), p.TenantID, skillID, postgres.SkillVersionInput{Semver: body.Semver, Instructions: body.Instructions, Procedures: body.Procedures, Examples: body.Examples, ForbiddenActions: body.ForbiddenActions, OutputSchema: body.OutputSchema, CreatedBy: p.Subject})
		if err != nil {
			writeError(w, http.StatusConflict, "could not create skill version")
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

type skillVersionRequest struct {
	Semver           string          `json:"semver"`
	Instructions     string          `json:"instructions"`
	Procedures       json.RawMessage `json:"procedures"`
	Examples         json.RawMessage `json:"examples"`
	ForbiddenActions json.RawMessage `json:"forbidden_actions"`
	OutputSchema     json.RawMessage `json:"output_schema"`
}

func (h CatalogHandler) updateSkillVersion(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, id string) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "skill version id must be a UUID")
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	if !h.allow(w, r, p, "skill.version.update", resource) {
		return
	}
	var body skillVersionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validVersionRequest(body.Semver, body.Instructions, body.OutputSchema) {
		writeError(w, http.StatusBadRequest, "valid semver, instructions, and object output_schema are required")
		return
	}
	item, err := h.Registry.UpdateSkillVersion(r.Context(), p.TenantID, id, postgres.SkillVersionInput{Semver: body.Semver, Instructions: body.Instructions, Procedures: body.Procedures, Examples: body.Examples, ForbiddenActions: body.ForbiddenActions, OutputSchema: body.OutputSchema}, p.Subject)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft skill version not found")
		} else {
			writeError(w, http.StatusConflict, "could not update skill version")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h CatalogHandler) publishSkill(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, id string) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "skill version id must be a UUID")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !h.allow(w, r, p, "skill.version.publish", resource) {
		return
	}
	item, err := h.Registry.PublishSkillVersion(r.Context(), p.TenantID, id, p.Subject)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "skill version not found")
		} else {
			writeError(w, http.StatusConflict, "skill version cannot be published")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h CatalogHandler) agentVersions(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, key string) {
	if !validAgentKey(key) {
		writeError(w, http.StatusBadRequest, "agent key is invalid")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.allow(w, r, p, "agent.version.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		items, total, err := h.Registry.ListAgentVersionsPage(r.Context(), p.TenantID, key, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list agent versions")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.allow(w, r, p, "agent.version.create", resource) {
			return
		}
		var body agentVersionRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if !validAgentVersionRequest(body) {
			writeError(w, http.StatusBadRequest, "valid semver and object schemas are required")
			return
		}
		item, err := h.Registry.CreateAgentVersion(r.Context(), p.TenantID, key, body.input(p.Subject))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "agent not found")
			} else {
				writeError(w, http.StatusConflict, "could not create agent version")
			}
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

type agentVersionRequest struct {
	Semver       string          `json:"semver"`
	Manifest     json.RawMessage `json:"manifest"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	ModelPolicy  json.RawMessage `json:"model_policy"`
	ToolPolicy   json.RawMessage `json:"tool_policy"`
}

func (b agentVersionRequest) input(actor string) postgres.AgentVersionInput {
	return postgres.AgentVersionInput{Semver: b.Semver, Manifest: b.Manifest, InputSchema: b.InputSchema, OutputSchema: b.OutputSchema, ModelPolicy: b.ModelPolicy, ToolPolicy: b.ToolPolicy, CreatedBy: actor}
}
func validAgentVersionRequest(b agentVersionRequest) bool {
	if !catalogSemverPattern.MatchString(b.Semver) {
		return false
	}
	for _, raw := range []json.RawMessage{b.Manifest, b.InputSchema, b.OutputSchema, b.ModelPolicy, b.ToolPolicy} {
		if !validJSONObject(raw) {
			return false
		}
	}
	return true
}
func (h CatalogHandler) updateAgentVersion(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, id string) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "agent version id must be a UUID")
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	if !h.allow(w, r, p, "agent.version.update", resource) {
		return
	}
	var body agentVersionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validAgentVersionRequest(body) {
		writeError(w, http.StatusBadRequest, "valid semver and object schemas are required")
		return
	}
	item, err := h.Registry.UpdateAgentVersion(r.Context(), p.TenantID, id, body.input(p.Subject), p.Subject)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft agent version not found")
		} else {
			writeError(w, http.StatusConflict, "could not update agent version")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (h CatalogHandler) publishAgent(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, id string) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "agent version id must be a UUID")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !h.allow(w, r, p, "agent.version.publish", resource) {
		return
	}
	// Agent versions are published only by an approved release request, so the
	// evaluation, domain review and AI Ops gates cannot be skipped.
	writeError(w, http.StatusConflict, "agent versions are published only through the release workflow")
}

func (h CatalogHandler) tools(w http.ResponseWriter, r *http.Request, p ports.Principal) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !h.allow(w, r, p, "tool.read", "global:tool-catalog") {
		return
	}
	limit, offset, ok := pagination(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
		return
	}
	items, total, err := h.Registry.ListToolsPage(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list tool catalog")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h CatalogHandler) agentSkills(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, versionID, skillVersionID string) {
	if !validCatalogID(versionID) || (skillVersionID != "" && !validCatalogID(skillVersionID)) {
		writeError(w, http.StatusBadRequest, "version ids must be UUIDs")
		return
	}
	if skillVersionID == "" && r.Method == http.MethodGet {
		if !h.allow(w, r, p, "agent.skill.read", resource) {
			return
		}
		items, err := h.Registry.ListAgentSkillBindings(r.Context(), p.TenantID, versionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list skill bindings")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	if skillVersionID == "" && r.Method == http.MethodPost {
		if !h.allow(w, r, p, "agent.skill.bind", resource) {
			return
		}
		var body struct {
			SkillVersionID string `json:"skill_version_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if !validCatalogID(body.SkillVersionID) {
			writeError(w, http.StatusBadRequest, "skill_version_id must be a UUID")
			return
		}
		if err := h.Registry.BindSkill(r.Context(), p.TenantID, versionID, body.SkillVersionID, p.Subject); err != nil {
			writeError(w, http.StatusConflict, "could not bind skill to draft agent version")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if skillVersionID != "" && r.Method == http.MethodDelete {
		if !h.allow(w, r, p, "agent.skill.unbind", resource) {
			return
		}
		if err := h.Registry.UnbindSkill(r.Context(), p.TenantID, versionID, skillVersionID, p.Subject); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "skill binding not found")
			} else {
				writeError(w, http.StatusConflict, "could not unbind skill")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	methodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodDelete)
}

func (h CatalogHandler) agentTools(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, versionID, toolVersionID string) {
	if !validCatalogID(versionID) || (toolVersionID != "" && !validCatalogID(toolVersionID)) {
		writeError(w, http.StatusBadRequest, "version ids must be UUIDs")
		return
	}
	if toolVersionID == "" && r.Method == http.MethodGet {
		if !h.allow(w, r, p, "agent.tool.read", resource) {
			return
		}
		items, err := h.Registry.ListAgentToolBindings(r.Context(), p.TenantID, versionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list tool bindings")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	if toolVersionID == "" && r.Method == http.MethodPost {
		if !h.allow(w, r, p, "agent.tool.bind", resource) {
			return
		}
		var body struct {
			ToolVersionID string          `json:"tool_version_id"`
			ActionScope   json.RawMessage `json:"action_scope"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if !validCatalogID(body.ToolVersionID) || !validJSONObject(body.ActionScope) {
			writeError(w, http.StatusBadRequest, "tool_version_id and object action_scope are required")
			return
		}
		if err := h.Registry.BindTool(r.Context(), p.TenantID, versionID, body.ToolVersionID, p.Subject, body.ActionScope); err != nil {
			writeError(w, http.StatusConflict, "tool version must be active and published and agent version draft")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if toolVersionID != "" && r.Method == http.MethodDelete {
		if !h.allow(w, r, p, "agent.tool.unbind", resource) {
			return
		}
		if err := h.Registry.UnbindTool(r.Context(), p.TenantID, versionID, toolVersionID, p.Subject); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "tool binding not found")
			} else {
				writeError(w, http.StatusConflict, "could not unbind tool")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	methodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodDelete)
}

func (h CatalogHandler) allow(w http.ResponseWriter, r *http.Request, p ports.Principal, action, resource string) bool {
	if err := h.Authorizer.Authorize(r.Context(), p, action, resource); err != nil {
		writeError(w, http.StatusForbidden, "action is not authorized")
		return false
	}
	return true
}
func validCatalogID(v string) bool { return catalogIDPattern.MatchString(v) }
func validJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return false
	}
	return true
}
func validVersionRequest(semver, instructions string, schema json.RawMessage) bool {
	return catalogSemverPattern.MatchString(semver) && len(instructions) <= 200000 && validJSONObject(schema)
}
