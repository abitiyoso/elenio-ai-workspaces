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

var toolDraftAdapterRefPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

type ToolDraftStore interface {
	ListToolDrafts(context.Context, string, int, int) ([]postgres.ToolDraft, int, error)
	GetToolDraft(context.Context, string, string) (postgres.ToolDraft, error)
	CreateToolDraft(context.Context, string, postgres.ToolDraftInput) (postgres.ToolDraft, error)
	UpdateToolDraft(context.Context, string, string, postgres.ToolDraftInput, string) (postgres.ToolDraft, error)
	ArchiveToolDraft(context.Context, string, string, string) error
	ListToolDraftVersions(context.Context, string, string, int, int) ([]postgres.ToolDraftVersion, int, error)
	CreateToolDraftVersion(context.Context, string, string, postgres.ToolDraftVersionInput) (postgres.ToolDraftVersion, error)
	UpdateToolDraftVersion(context.Context, string, string, postgres.ToolDraftVersionInput, string) (postgres.ToolDraftVersion, error)
	DeleteToolDraftVersion(context.Context, string, string, string) error
}

// ToolDraftHandler owns tenant-scoped, non-executable tool drafts. It never
// writes the platform-global `tools` or `tool_versions` catalog.
type ToolDraftHandler struct {
	Registry   ToolDraftStore
	Auth       ports.AuthAdapter
	Authorizer ports.Authorizer
}

func RegisterToolDraftRoutes(mux *http.ServeMux, registry ToolDraftStore, auth ports.AuthAdapter, authorizer ports.Authorizer) {
	handler := ToolDraftHandler{Registry: registry, Auth: auth, Authorizer: authorizer}
	mux.Handle("/api/v1/catalog/tool-drafts", handler)
	mux.Handle("/api/v1/catalog/tool-drafts/", handler)
	mux.Handle("/api/v1/catalog/tool-draft-versions/", handler)
}

func (h ToolDraftHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil || h.Auth == nil || h.Authorizer == nil {
		writeError(w, http.StatusServiceUnavailable, "tool draft registry is not configured")
		return
	}
	p, err := h.Auth.VerifyRequest(r.Context(), r)
	if err != nil || strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.Subject) == "" {
		writeError(w, http.StatusUnauthorized, "verified tenant identity is required")
		return
	}
	resource := "tenant:" + p.TenantID
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/catalog/"), "/")
	parts := strings.Split(path, "/")
	switch {
	case path == "tool-drafts":
		h.collection(w, r, p, resource)
	case len(parts) == 2 && parts[0] == "tool-drafts":
		h.detail(w, r, p, resource, parts[1])
	case len(parts) == 3 && parts[0] == "tool-drafts" && parts[2] == "versions":
		h.versions(w, r, p, resource, parts[1])
	case len(parts) == 2 && parts[0] == "tool-draft-versions":
		h.version(w, r, p, resource, parts[1])
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h ToolDraftHandler) collection(w http.ResponseWriter, r *http.Request, p ports.Principal, resource string) {
	switch r.Method {
	case http.MethodGet:
		if !h.allow(w, r, p, "tool.draft.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		items, total, err := h.Registry.ListToolDrafts(r.Context(), p.TenantID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list tenant tool drafts")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.allow(w, r, p, "tool.draft.create", resource) {
			return
		}
		var body toolDraftRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Key = strings.TrimSpace(body.Key)
		body.DisplayName = strings.TrimSpace(body.DisplayName)
		body.Kind = strings.TrimSpace(body.Kind)
		body.RiskLevel = strings.TrimSpace(body.RiskLevel)
		if !validToolDraft(body.Key, body.DisplayName, body.Kind, body.RiskLevel) {
			writeError(w, http.StatusBadRequest, "key, display_name, kind, and valid risk_level are required")
			return
		}
		item, err := h.Registry.CreateToolDraft(r.Context(), p.TenantID, postgres.ToolDraftInput{
			Key: body.Key, DisplayName: body.DisplayName, Kind: body.Kind, RiskLevel: body.RiskLevel, CreatedBy: p.Subject,
		})
		if err != nil {
			writeError(w, http.StatusConflict, "could not create tenant tool draft")
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h ToolDraftHandler) detail(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, id string) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "tool draft id must be a UUID")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.allow(w, r, p, "tool.draft.read", resource) {
			return
		}
		item, err := h.Registry.GetToolDraft(r.Context(), p.TenantID, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tool draft not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load tool draft")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		if !h.allow(w, r, p, "tool.draft.update", resource) {
			return
		}
		var body toolDraftRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Key = strings.TrimSpace(body.Key)
		body.DisplayName = strings.TrimSpace(body.DisplayName)
		body.Kind = strings.TrimSpace(body.Kind)
		body.RiskLevel = strings.TrimSpace(body.RiskLevel)
		if !validToolDraft(body.Key, body.DisplayName, body.Kind, body.RiskLevel) {
			writeError(w, http.StatusBadRequest, "key, display_name, kind, and valid risk_level are required")
			return
		}
		item, err := h.Registry.UpdateToolDraft(r.Context(), p.TenantID, id, postgres.ToolDraftInput{
			Key: body.Key, DisplayName: body.DisplayName, Kind: body.Kind, RiskLevel: body.RiskLevel,
		}, p.Subject)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft tool not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusConflict, "could not update tool draft")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if !h.allow(w, r, p, "tool.draft.delete", resource) {
			return
		}
		if err := h.Registry.ArchiveToolDraft(r.Context(), p.TenantID, id, p.Subject); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft tool not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "could not archive tool draft")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}

func (h ToolDraftHandler) versions(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, toolID string) {
	if !validCatalogID(toolID) {
		writeError(w, http.StatusBadRequest, "tool draft id must be a UUID")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.allow(w, r, p, "tool.draft.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		items, total, err := h.Registry.ListToolDraftVersions(r.Context(), p.TenantID, toolID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list tool draft versions")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.allow(w, r, p, "tool.draft.version.create", resource) {
			return
		}
		var body toolDraftVersionRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Semver = strings.TrimSpace(body.Semver)
		body.AdapterRef = strings.TrimSpace(body.AdapterRef)
		if !validToolDraftVersion(body) {
			writeError(w, http.StatusBadRequest, "valid semver, named adapter_ref, and object schemas are required")
			return
		}
		item, err := h.Registry.CreateToolDraftVersion(r.Context(), p.TenantID, toolID, body.input(p.Subject))
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft tool not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusConflict, "could not create tool draft version")
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h ToolDraftHandler) version(w http.ResponseWriter, r *http.Request, p ports.Principal, resource, id string) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "tool draft version id must be a UUID")
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !h.allow(w, r, p, "tool.draft.version.update", resource) {
			return
		}
		var body toolDraftVersionRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Semver = strings.TrimSpace(body.Semver)
		body.AdapterRef = strings.TrimSpace(body.AdapterRef)
		if !validToolDraftVersion(body) {
			writeError(w, http.StatusBadRequest, "valid semver, named adapter_ref, and object schemas are required")
			return
		}
		item, err := h.Registry.UpdateToolDraftVersion(r.Context(), p.TenantID, id, body.input(p.Subject), p.Subject)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft tool version not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusConflict, "could not update tool draft version")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if !h.allow(w, r, p, "tool.draft.version.delete", resource) {
			return
		}
		if err := h.Registry.DeleteToolDraftVersion(r.Context(), p.TenantID, id, p.Subject); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft tool version not found")
			return
		} else if err != nil {
			writeError(w, http.StatusConflict, "could not delete tool draft version")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

type toolDraftRequest struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind"`
	RiskLevel   string `json:"risk_level"`
}

type toolDraftVersionRequest struct {
	Semver       string         `json:"semver"`
	AdapterRef   string         `json:"adapter_ref"`
	InputSchema  jsonRawMessage `json:"input_schema"`
	OutputSchema jsonRawMessage `json:"output_schema"`
	Policy       jsonRawMessage `json:"policy"`
}

// jsonRawMessage is an alias to keep the request shape explicit while using
// the standard decoder's raw JSON handling.
type jsonRawMessage = json.RawMessage

func (b toolDraftVersionRequest) input(actor string) postgres.ToolDraftVersionInput {
	return postgres.ToolDraftVersionInput{
		Semver: b.Semver, AdapterRef: b.AdapterRef, InputSchema: b.InputSchema,
		OutputSchema: b.OutputSchema, Policy: b.Policy, CreatedBy: actor,
	}
}

func validToolDraft(key, displayName, kind, riskLevel string) bool {
	if !validAgentKey(strings.TrimSpace(key)) || strings.TrimSpace(displayName) == "" || len(displayName) > 200 || strings.TrimSpace(kind) == "" || len(kind) > 64 {
		return false
	}
	switch riskLevel {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func validToolDraftVersion(body toolDraftVersionRequest) bool {
	return catalogSemverPattern.MatchString(body.Semver) && toolDraftAdapterRefPattern.MatchString(body.AdapterRef) &&
		isJSONObject(body.InputSchema) && isJSONObject(body.OutputSchema) && (len(body.Policy) == 0 || isJSONObject(body.Policy))
}

func isJSONObject(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]jsonRawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func (h ToolDraftHandler) allow(w http.ResponseWriter, r *http.Request, p ports.Principal, action, resource string) bool {
	if err := h.Authorizer.Authorize(r.Context(), p, action, resource); err != nil {
		writeError(w, http.StatusForbidden, "action is not authorized")
		return false
	}
	return true
}
