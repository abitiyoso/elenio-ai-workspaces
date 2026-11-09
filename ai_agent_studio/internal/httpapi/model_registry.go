package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
)

const maxRegistryBody = 1 << 20

// RegistryStore is the tenant-scoped model registry surface used by the API.
type RegistryStore interface {
	ListProviders(rctx context.Context, tenantID string) ([]postgres.Provider, error)
	ListProvidersPage(rctx context.Context, tenantID string, limit, offset int) ([]postgres.Provider, int, error)
	CreateProvider(rctx context.Context, tenantID string, input postgres.ProviderInput) (postgres.Provider, error)
	ListDeployments(rctx context.Context, tenantID string) ([]postgres.Deployment, error)
	ListDeploymentsPage(rctx context.Context, tenantID string, limit, offset int) ([]postgres.Deployment, int, error)
	CreateDeployment(rctx context.Context, tenantID string, input postgres.DeploymentInput) (postgres.Deployment, error)
	ListAgents(rctx context.Context, tenantID string, limit, offset int) ([]postgres.Agent, int, error)
	CreateAgent(rctx context.Context, tenantID string, input postgres.AgentInput) (postgres.Agent, error)
	UpdateAgentMetadata(rctx context.Context, tenantID, key, displayName, domain, actor string) (postgres.Agent, error)
	CloneAgent(rctx context.Context, tenantID, sourceKey string, input postgres.AgentInput) (postgres.Agent, error)
	ArchiveAgent(rctx context.Context, tenantID, key, actor string) error
	BootstrapSIMAgents(rctx context.Context, tenantID, actor string) (postgres.SIMBootstrapResult, error)
}

// ModelRegistryHandler exposes provider and deployment metadata. Every request
// requires a verified principal and an explicit action authorization check.
type ModelRegistryHandler struct {
	Registry   RegistryStore
	Auth       ports.AuthAdapter
	Authorizer ports.Authorizer
}

func (h ModelRegistryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil || h.Auth == nil || h.Authorizer == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry is not configured")
		return
	}

	principal, err := h.Auth.VerifyRequest(r.Context(), r)
	if err != nil || strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.Subject) == "" {
		writeError(w, http.StatusUnauthorized, "verified tenant identity is required")
		return
	}

	resource := "tenant:" + principal.TenantID
	switch r.URL.Path {
	case "/api/v1/model-providers":
		h.providers(w, r, principal, resource)
	case "/api/v1/model-deployments":
		h.deployments(w, r, principal, resource)
	case "/api/v1/agents":
		h.agents(w, r, principal, resource)
	case "/api/v1/agents/bootstrap/sim":
		h.bootstrapSIM(w, r, principal, resource)
	default:
		if strings.HasPrefix(r.URL.Path, "/api/v1/agents/") {
			path := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")
			switch {
			case strings.HasSuffix(path, "/archive"):
				h.archiveAgent(w, r, principal, resource)
				return
			case strings.HasSuffix(path, "/clone"):
				h.cloneAgent(w, r, principal, resource)
				return
			case !strings.Contains(path, "/"):
				h.updateAgent(w, r, principal, resource, path)
				return
			}
		}
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h ModelRegistryHandler) agents(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource string) {
	switch r.Method {
	case http.MethodGet:
		if !h.authorize(w, r, principal, "agent.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		items, total, err := h.Registry.ListAgents(r.Context(), principal.TenantID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list agents")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.authorize(w, r, principal, "agent.create", resource) {
			return
		}
		var input agentRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		input.Key = strings.TrimSpace(input.Key)
		input.DisplayName = strings.TrimSpace(input.DisplayName)
		input.Domain = strings.TrimSpace(input.Domain)
		if !validAgentKey(input.Key) || input.DisplayName == "" || len(input.DisplayName) > 200 || input.Domain == "" || len(input.Domain) > 100 {
			writeError(w, http.StatusBadRequest, "key, display_name, and domain are required and must be within limits")
			return
		}
		agent, err := h.Registry.CreateAgent(r.Context(), principal.TenantID, postgres.AgentInput{
			Key: input.Key, DisplayName: input.DisplayName, Domain: input.Domain, CreatedBy: principal.Subject,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not create agent")
			return
		}
		writeJSON(w, http.StatusCreated, agent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h ModelRegistryHandler) bootstrapSIM(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !h.authorize(w, r, principal, "agent.bootstrap", resource) {
		return
	}
	result, err := h.Registry.BootstrapSIMAgents(r.Context(), principal.TenantID, principal.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not bootstrap SIM agent templates")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h ModelRegistryHandler) archiveAgent(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !h.authorize(w, r, principal, "agent.archive", resource) {
		return
	}
	key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/agents/"), "/archive")
	if !validAgentKey(key) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if err := h.Registry.ArchiveAgent(r.Context(), principal.TenantID, key, principal.Subject); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not archive agent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h ModelRegistryHandler) updateAgent(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource, key string) {
	if r.Method != http.MethodPatch {
		methodNotAllowed(w, http.MethodPatch)
		return
	}
	if !validAgentKey(key) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !h.authorize(w, r, principal, "agent.update", resource) {
		return
	}
	var input agentRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Domain = strings.TrimSpace(input.Domain)
	if input.DisplayName == "" || len(input.DisplayName) > 200 || input.Domain == "" || len(input.Domain) > 100 {
		writeError(w, http.StatusBadRequest, "display_name and domain are required and must be within limits")
		return
	}
	agent, err := h.Registry.UpdateAgentMetadata(r.Context(), principal.TenantID, key, input.DisplayName, input.Domain, principal.Subject)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "draft agent not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "could not update agent")
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func (h ModelRegistryHandler) cloneAgent(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/agents/"), "/clone")
	if !validAgentKey(key) {
		writeError(w, http.StatusNotFound, "source agent not found")
		return
	}
	if !h.authorize(w, r, principal, "agent.clone", resource) {
		return
	}
	var input agentRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Key = strings.TrimSpace(input.Key)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Domain = strings.TrimSpace(input.Domain)
	if !validAgentKey(input.Key) || input.DisplayName == "" || len(input.DisplayName) > 200 || input.Domain == "" || len(input.Domain) > 100 {
		writeError(w, http.StatusBadRequest, "new key, display_name, and domain are required and must be within limits")
		return
	}
	agent, err := h.Registry.CloneAgent(r.Context(), principal.TenantID, key, postgres.AgentInput{
		Key: input.Key, DisplayName: input.DisplayName, Domain: input.Domain, CreatedBy: principal.Subject,
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "source agent not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "could not clone agent")
		return
	}
	writeJSON(w, http.StatusCreated, agent)
}

type agentRequest struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Domain      string `json:"domain"`
}

func validAgentKey(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, char := range value[1:] {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.') {
			return false
		}
	}
	return true
}

func pagination(r *http.Request) (int, int, bool) {
	limit := 25
	offset := 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, false
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return 0, 0, false
		}
	}
	return limit, offset, true
}

func (h ModelRegistryHandler) providers(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource string) {
	switch r.Method {
	case http.MethodGet:
		if !h.authorize(w, r, principal, "model_provider.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		providers, total, err := h.Registry.ListProvidersPage(r.Context(), principal.TenantID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list model providers")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": providers, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.authorize(w, r, principal, "model_provider.create", resource) {
			return
		}
		var input providerRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		input.Key = strings.TrimSpace(input.Key)
		input.DisplayName = strings.TrimSpace(input.DisplayName)
		input.BaseURL = strings.TrimSpace(input.BaseURL)
		if err := validateProvider(input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		provider, err := h.Registry.CreateProvider(r.Context(), principal.TenantID, postgres.ProviderInput{
			Key: input.Key, DisplayName: input.DisplayName, AdapterType: input.AdapterType,
			BaseURL: input.BaseURL, CreatedBy: principal.Subject,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not create model provider")
			return
		}
		writeJSON(w, http.StatusCreated, provider)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h ModelRegistryHandler) deployments(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource string) {
	switch r.Method {
	case http.MethodGet:
		if !h.authorize(w, r, principal, "model_deployment.read", resource) {
			return
		}
		limit, offset, ok := pagination(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "limit and offset must be valid non-negative integers")
			return
		}
		deployments, total, err := h.Registry.ListDeploymentsPage(r.Context(), principal.TenantID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list model deployments")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": deployments, "total": total, "limit": limit, "offset": offset})
	case http.MethodPost:
		if !h.authorize(w, r, principal, "model_deployment.create", resource) {
			return
		}
		var input deploymentRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		input.Key = strings.TrimSpace(input.Key)
		input.ProviderModelID = strings.TrimSpace(input.ProviderModelID)
		input.DisplayName = strings.TrimSpace(input.DisplayName)
		if err := validateDeployment(input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		deployment, err := h.Registry.CreateDeployment(r.Context(), principal.TenantID, postgres.DeploymentInput{
			ProviderID: input.ProviderID, Key: input.Key, ProviderModelID: input.ProviderModelID,
			DisplayName: input.DisplayName, Capabilities: input.Capabilities,
			MaxOutputTokens: input.MaxOutputTokens, TimeoutMillis: input.TimeoutMillis,
			CostPolicy: input.CostPolicy, CreatedBy: principal.Subject,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not create model deployment")
			return
		}
		writeJSON(w, http.StatusCreated, deployment)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h ModelRegistryHandler) authorize(w http.ResponseWriter, r *http.Request, principal ports.Principal, action, resource string) bool {
	if err := h.Authorizer.Authorize(r.Context(), principal, action, resource); err != nil {
		writeError(w, http.StatusForbidden, "action is not authorized")
		return false
	}
	return true
}

type providerRequest struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	AdapterType string `json:"adapter_type"`
	BaseURL     string `json:"base_url"`
}

type deploymentRequest struct {
	ProviderID      string          `json:"provider_id"`
	Key             string          `json:"key"`
	ProviderModelID string          `json:"provider_model_id"`
	DisplayName     string          `json:"display_name"`
	Capabilities    json.RawMessage `json:"capabilities"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	TimeoutMillis   int             `json:"timeout_ms"`
	CostPolicy      json.RawMessage `json:"cost_policy"`
}

func validateProvider(input providerRequest) error {
	if !validKey(input.Key) || input.DisplayName == "" || len(input.DisplayName) > 200 {
		return errors.New("key and display_name are required and must be within limits")
	}
	switch input.AdapterType {
	case "openai", "openai_compatible", "azure_openai", "ollama", "custom":
	default:
		return errors.New("adapter_type is unsupported")
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("base_url must be an HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

func validateDeployment(input deploymentRequest) error {
	if input.ProviderID == "" || !validKey(input.Key) || input.ProviderModelID == "" || len(input.ProviderModelID) > 256 || input.DisplayName == "" || len(input.DisplayName) > 200 {
		return errors.New("provider_id, key, provider_model_id, and display_name are required and must be within limits")
	}
	if input.MaxOutputTokens < 1 || input.MaxOutputTokens > 1000000 || input.TimeoutMillis < 100 || input.TimeoutMillis > 600000 {
		return errors.New("max_output_tokens or timeout_ms is outside the allowed range")
	}
	for _, raw := range []json.RawMessage{input.Capabilities, input.CostPolicy} {
		if len(raw) == 0 {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return errors.New("capabilities and cost_policy must be JSON objects")
		}
	}
	return nil
}

func validKey(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, char := range value[1:] {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.ContentLength > maxRegistryBody {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRegistryBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be a valid JSON object")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
