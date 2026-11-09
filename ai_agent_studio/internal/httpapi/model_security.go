package httpapi

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
)

const maxCredentialBody = 96 << 10

type ModelSecurityStore interface {
	GetProviderSecurity(context.Context, string, string) (postgres.ProviderSecurityRecord, error)
	SetProviderCredentialRef(context.Context, string, string, string, string) (string, error)
	DeleteProviderCredentialRef(context.Context, string, string, string) (string, error)
	GetProviderProbeTarget(context.Context, string, string, string) (postgres.ProviderProbeTarget, error)
	RecordProviderProbe(context.Context, string, string, string, string, string, json.RawMessage, string) (postgres.ProviderProbeRecord, error)
	ListModelApprovals(context.Context, string, string, string) ([]postgres.ModelApproval, error)
	SetModelApproval(context.Context, string, string, string, string, string, string) (postgres.ModelApproval, error)
	GetAgentModelBinding(context.Context, string, string) (postgres.AgentModelBinding, error)
	SetAgentModelBinding(context.Context, string, string, string, string, string, string, json.RawMessage) (postgres.AgentModelBinding, error)
	DeleteAgentModelBinding(context.Context, string, string, string) error
}

type ScopedCredentialVault interface {
	ports.SecretVault
	ResolveScoped(context.Context, string, string) ([]byte, error)
}

type ProviderProbeInput struct {
	ProviderID            string
	AdapterType           string
	BaseURL               string
	DeploymentID          string
	ProviderModelID       string
	Mode                  string
	RequestedCapabilities map[string]bool
}

type ProviderProbe interface {
	Probe(context.Context, ProviderProbeInput, []byte, *http.Client) (ProviderProbeResult, error)
}

type ProviderProbeResult struct {
	Capabilities      map[string]bool   `json:"capabilities"`
	Evidence          map[string]string `json:"evidence"`
	MayIncurTokenCost bool              `json:"may_incur_token_cost"`
}

type ProviderProbeError struct {
	Code string
}

func (e *ProviderProbeError) Error() string { return "provider probe failed: " + e.Code }

type ProviderHostResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ProviderEndpointPolicy struct {
	allowed  map[string]struct{}
	resolver ProviderHostResolver
}

// NewProviderEndpointPolicy accepts comma-separated exact HTTPS authorities
// through its caller, such as api.example.com or gateway.example.com:8443.
// Wildcards, URL paths, IP literals, and credentials are rejected.
func NewProviderEndpointPolicy(authorities []string) (ProviderEndpointPolicy, error) {
	policy := ProviderEndpointPolicy{allowed: make(map[string]struct{}), resolver: net.DefaultResolver}
	for _, raw := range authorities {
		authority, err := normalizeProviderAuthority(raw)
		if err != nil {
			return ProviderEndpointPolicy{}, err
		}
		policy.allowed[authority] = struct{}{}
	}
	return policy, nil
}

// NewClient pins the request to a vetted DNS result and disables proxies and
// redirects. The requested URL must exactly match a configured HTTPS authority.
func (p ProviderEndpointPolicy) NewClient(ctx context.Context, rawURL string) (*http.Client, error) {
	if len(p.allowed) == 0 || p.resolver == nil {
		return nil, errors.New("provider endpoint allowlist is not configured")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("provider endpoint must be an HTTPS URL without credentials, query, or fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || net.ParseIP(host) != nil {
		return nil, errors.New("provider endpoint must use an allowlisted hostname")
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, errors.New("provider endpoint port is invalid")
	}
	authority := net.JoinHostPort(host, port)
	if _, ok := p.allowed[authority]; !ok {
		return nil, errors.New("provider endpoint is not allowlisted")
	}
	addresses, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("provider endpoint DNS lookup failed")
	}
	pinned := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !isPublicProbeAddress(address) {
			return nil, errors.New("provider endpoint resolves to a non-public IP address")
		}
		pinned = append(pinned, address)
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host},
		ResponseHeaderTimeout: 8 * time.Second,
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			requestedHost, requestedPort, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(strings.TrimSuffix(requestedHost, "."), host) || requestedPort != port {
				return nil, errors.New("provider probe attempted an unapproved network destination")
			}
			var lastErr error
			for _, ip := range pinned {
				connection, err := dialer.DialContext(dialCtx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return connection, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func normalizeProviderAuthority(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "/*?#@\r\n") {
		return "", errors.New("provider allowlist entries must be exact host authorities")
	}
	u, err := url.Parse("https://" + raw)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("provider allowlist entry is invalid")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || net.ParseIP(host) != nil {
		return "", errors.New("provider allowlist entries must use DNS hostnames")
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", errors.New("provider allowlist port is invalid")
	}
	return net.JoinHostPort(host, port), nil
}

func isPublicProbeAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	blocked := []string{
		"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
		"2001:db8::/32", "3fff::/20",
	}
	for _, raw := range blocked {
		prefix, _ := netip.ParsePrefix(raw)
		if prefix.Contains(address) {
			return false
		}
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	return true
}

type ModelSecurityHandler struct {
	Registry       ModelSecurityStore
	Vault          ScopedCredentialVault
	Auth           ports.AuthAdapter
	Authorizer     ports.Authorizer
	EndpointPolicy ProviderEndpointPolicy
	Probe          ProviderProbe
}

func RegisterModelSecurityRoutes(mux *http.ServeMux, registry ModelSecurityStore, vault ScopedCredentialVault, auth ports.AuthAdapter, authorizer ports.Authorizer, endpointPolicy ProviderEndpointPolicy, probe ProviderProbe) {
	handler := ModelSecurityHandler{Registry: registry, Vault: vault, Auth: auth, Authorizer: authorizer, EndpointPolicy: endpointPolicy, Probe: probe}
	mux.Handle("/api/v1/model-providers/", handler)
	mux.Handle("/api/v1/model-deployments/", handler)
	mux.Handle("/api/v1/agent-versions/", handler)
}

func (h ModelSecurityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil || h.Auth == nil || h.Authorizer == nil {
		writeError(w, http.StatusServiceUnavailable, "model security API is not configured")
		return
	}
	principal, err := h.Auth.VerifyRequest(r.Context(), r)
	if err != nil || strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.Subject) == "" {
		writeError(w, http.StatusUnauthorized, "verified tenant identity is required")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	parts := strings.Split(path, "/")
	resource := "tenant:" + principal.TenantID
	switch {
	case len(parts) == 3 && parts[0] == "model-providers" && parts[2] == "credentials":
		h.credentials(w, r, principal, resource, parts[1])
	case len(parts) == 3 && parts[0] == "model-providers" && parts[2] == "probe":
		h.probe(w, r, principal, resource, parts[1])
	case len(parts) == 3 && parts[0] == "model-deployments" && parts[2] == "approvals":
		h.approvals(w, r, principal, resource, parts[1])
	case len(parts) == 3 && parts[0] == "agent-versions" && parts[2] == "model-binding":
		h.modelBinding(w, r, principal, resource, parts[1])
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

type credentialRequest struct {
	Credential string `json:"credential"`
}

func (h ModelSecurityHandler) credentials(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource, providerID string) {
	if !validCatalogID(providerID) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	action := map[string]string{
		http.MethodGet:    "model_provider.credential.read",
		http.MethodPut:    "model_provider.credential.write",
		http.MethodDelete: "model_provider.credential.delete",
	}[r.Method]
	if action == "" {
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
		return
	}
	if !h.authorize(w, r, principal, action, resource) {
		return
	}
	provider, err := h.Registry.GetProviderSecurity(r.Context(), principal.TenantID, providerID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load provider")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"credential_configured": provider.SecretRef != "",
			"provider_status":       provider.Status,
		})
	case http.MethodPut:
		if h.Vault == nil {
			writeError(w, http.StatusServiceUnavailable, "credential vault is not configured")
			return
		}
		var body credentialRequest
		if !decodeBoundedJSON(w, r, maxCredentialBody, &body) {
			return
		}
		if strings.TrimSpace(body.Credential) == "" || len(body.Credential) > maxCredentialBody {
			body.Credential = ""
			writeError(w, http.StatusBadRequest, "credential is required and exceeds the supported size")
			return
		}
		secret := []byte(body.Credential)
		body.Credential = ""
		defer clear(secret)
		secretRef, err := h.Vault.Put(r.Context(), providerScope(providerID), secret)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "credential could not be stored in the backend vault")
			return
		}
		oldRef, err := h.Registry.SetProviderCredentialRef(r.Context(), principal.TenantID, providerID, secretRef, principal.Subject)
		if err != nil {
			_ = h.Vault.Revoke(r.Context(), secretRef)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "provider not found")
			} else {
				writeError(w, http.StatusInternalServerError, "credential reference could not be updated")
			}
			return
		}
		cleanupPending := false
		if oldRef != "" && oldRef != secretRef {
			cleanupPending = h.Vault.Revoke(r.Context(), oldRef) != nil
		}
		updated, err := h.Registry.GetProviderSecurity(r.Context(), principal.TenantID, providerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "credential was stored but provider status is unavailable")
			return
		}
		status := http.StatusOK
		if cleanupPending {
			status = http.StatusAccepted
		}
		writeJSON(w, status, map[string]any{"credential_configured": true, "provider_status": updated.Status, "previous_secret_cleanup_pending": cleanupPending})
	case http.MethodDelete:
		if h.Vault == nil {
			writeError(w, http.StatusServiceUnavailable, "credential vault is not configured")
			return
		}
		if provider.SecretRef == "" {
			writeJSON(w, http.StatusOK, map[string]any{"credential_configured": false, "provider_status": provider.Status, "vault_cleanup_pending": false})
			return
		}
		oldRef, err := h.Registry.DeleteProviderCredentialRef(r.Context(), principal.TenantID, providerID, principal.Subject)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "provider credential not found")
			} else {
				writeError(w, http.StatusInternalServerError, "credential reference could not be removed")
			}
			return
		}
		cleanupPending := h.Vault.Revoke(r.Context(), oldRef) != nil
		status := http.StatusOK
		if cleanupPending {
			status = http.StatusAccepted
		}
		writeJSON(w, status, map[string]any{"credential_configured": false, "provider_status": "disabled", "vault_cleanup_pending": cleanupPending})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

type probeRequest struct {
	Mode             string          `json:"mode"`
	DeploymentID     string          `json:"deployment_id"`
	Capabilities     json.RawMessage `json:"capabilities"`
	ConfirmTokenCost bool            `json:"confirm_token_cost"`
}

func (h ModelSecurityHandler) probe(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource, providerID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !validCatalogID(providerID) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	if !h.authorize(w, r, principal, "model_provider.probe", resource) {
		return
	}
	if h.Probe == nil {
		writeError(w, http.StatusServiceUnavailable, "no provider protocol adapter is configured; no network request was made")
		return
	}
	if h.Vault == nil {
		writeError(w, http.StatusServiceUnavailable, "credential vault is not configured")
		return
	}
	var body probeRequest
	if !decodeBoundedJSON(w, r, maxCredentialBody, &body) {
		return
	}
	if !validCatalogID(body.DeploymentID) {
		writeError(w, http.StatusBadRequest, "deployment_id is required")
		return
	}
	body.Mode = strings.TrimSpace(body.Mode)
	var requested map[string]bool
	var err error
	switch body.Mode {
	case "connectivity":
		requested, err = connectionOnlyCapabilities(body.Capabilities)
	case "capabilities":
		requested, err = requestedCapabilities(body.Capabilities)
	default:
		err = errors.New("probe mode must be connectivity or capabilities")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "probe mode or requested capabilities are invalid")
		return
	}
	mayIncurCost := body.Mode == "capabilities"
	if mayIncurCost && !body.ConfirmTokenCost {
		writeJSON(w, http.StatusPreconditionRequired, map[string]any{
			"error":                "token_cost_acknowledgement_required",
			"message":              "JSON and tool capability checks send small inference requests and may incur provider token charges.",
			"mode":                 body.Mode,
			"may_incur_token_cost": true,
		})
		return
	}
	target, err := h.Registry.GetProviderProbeTarget(r.Context(), principal.TenantID, providerID, body.DeploymentID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "provider deployment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load provider deployment")
		return
	}
	if target.Provider.SecretRef == "" {
		writeError(w, http.StatusConflict, "configure a provider credential before running a connection test")
		return
	}
	secret, err := h.Vault.ResolveScoped(r.Context(), target.Provider.SecretRef, providerScope(providerID))
	if err != nil || len(secret) == 0 {
		writeError(w, http.StatusServiceUnavailable, "provider credential could not be resolved")
		return
	}
	defer clear(secret)
	client, err := h.EndpointPolicy.NewClient(r.Context(), target.Provider.BaseURL)
	if err != nil {
		writeError(w, http.StatusForbidden, "provider endpoint is not permitted by the HTTPS egress policy")
		return
	}
	defer client.CloseIdleConnections()
	probeResult, probeErr := h.Probe.Probe(r.Context(), ProviderProbeInput{
		ProviderID: providerID, AdapterType: target.Provider.AdapterType, BaseURL: target.Provider.BaseURL,
		DeploymentID: target.Deployment.ID, ProviderModelID: target.Deployment.ProviderModelID,
		Mode: body.Mode, RequestedCapabilities: requested,
	}, secret, client)
	verifiedJSON, err := verifiedCapabilities(probeResult.Capabilities, requested)
	outcome, redactedCode := "passed", ""
	if probeErr != nil || err != nil {
		outcome, redactedCode = "failed", "probe_failed"
		verifiedJSON = json.RawMessage(`{}`)
		if probeErr != nil {
			redactedCode = providerProbeErrorCode(probeErr)
		} else if err != nil {
			redactedCode = "capability_not_verified"
		}
	} else {
		verifiedJSON, err = json.Marshal(map[string]any{
			"capabilities": probeResult.Capabilities,
			"evidence":     probeResult.Evidence,
		})
		if err != nil {
			outcome, redactedCode = "failed", "probe_result_invalid"
			verifiedJSON = json.RawMessage(`{}`)
		}
	}
	result, recordErr := h.Registry.RecordProviderProbe(r.Context(), principal.TenantID, providerID, body.DeploymentID, principal.Subject, outcome, verifiedJSON, redactedCode)
	if recordErr != nil {
		writeError(w, http.StatusInternalServerError, "provider probe result could not be recorded")
		return
	}
	if outcome != "passed" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"id": result.ID, "status": "error", "mode": body.Mode, "error_code": redactedCode, "may_incur_token_cost": mayIncurCost})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": result.ID, "status": "active", "mode": body.Mode, "capabilities": probeResult.Capabilities,
		"evidence": probeResult.Evidence, "evidence_scope": "provider_endpoint",
		"checked_at": result.CheckedAt, "may_incur_token_cost": probeResult.MayIncurTokenCost,
	})
}

func providerProbeErrorCode(err error) string {
	var probeErr *ProviderProbeError
	if errors.As(err, &probeErr) {
		switch probeErr.Code {
		case "authentication_failed", "rate_limited", "timeout", "refusal", "truncated", "model_unavailable", "unsupported_capability", "provider_unavailable", "invalid_response":
			return probeErr.Code
		}
	}
	return "probe_failed"
}

func (h ModelSecurityHandler) approvals(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource, deploymentID string) {
	if !validCatalogID(deploymentID) {
		writeError(w, http.StatusNotFound, "model deployment not found")
		return
	}
	environment := r.URL.Query().Get("environment")
	if !isAllowedEnvironment(environment) {
		writeError(w, http.StatusBadRequest, "environment must be development, staging, or production")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.authorize(w, r, principal, "model_deployment.approval.read", resource) {
			return
		}
		items, err := h.Registry.ListModelApprovals(r.Context(), principal.TenantID, deploymentID, environment)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list model approvals")
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var body struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
		}
		if !decodeBoundedJSON(w, r, 8<<10, &body) {
			return
		}
		body.State = strings.TrimSpace(body.State)
		body.Reason = strings.TrimSpace(body.Reason)
		if body.State != "approved" && body.State != "rejected" && body.State != "revoked" || len(body.Reason) > 1000 {
			writeError(w, http.StatusBadRequest, "approval state or reason is invalid")
			return
		}
		action := map[string]string{"approved": "model_deployment.approve", "rejected": "model_deployment.revoke", "revoked": "model_deployment.revoke"}[body.State]
		if !h.authorize(w, r, principal, action, resource) {
			return
		}
		item, err := h.Registry.SetModelApproval(r.Context(), principal.TenantID, deploymentID, environment, body.State, body.Reason, principal.Subject)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "model deployment not found")
			return
		}
		if errors.Is(err, postgres.ErrCapabilityCheckRequired) {
			writeError(w, http.StatusConflict, "a current successful provider capability check is required before approval")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "model approval could not be updated")
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h ModelSecurityHandler) modelBinding(w http.ResponseWriter, r *http.Request, principal ports.Principal, resource, versionID string) {
	if !validCatalogID(versionID) {
		writeError(w, http.StatusNotFound, "agent version not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.authorize(w, r, principal, "agent.model_binding.read", resource) {
			return
		}
		item, err := h.Registry.GetAgentModelBinding(r.Context(), principal.TenantID, versionID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent version has no model binding")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read model binding")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		if !h.authorize(w, r, principal, "agent.model_binding.write", resource) {
			return
		}
		var body struct {
			DeploymentID string          `json:"model_deployment_id"`
			FallbackID   string          `json:"fallback_model_deployment_id"`
			Environment  string          `json:"environment"`
			Override     json.RawMessage `json:"override_policy"`
		}
		if !decodeBoundedJSON(w, r, 32<<10, &body) {
			return
		}
		if !validCatalogID(body.DeploymentID) || (body.FallbackID != "" && !validCatalogID(body.FallbackID)) || !isAllowedEnvironment(body.Environment) {
			writeError(w, http.StatusBadRequest, "model deployment, fallback, or environment is invalid")
			return
		}
		item, err := h.Registry.SetAgentModelBinding(r.Context(), principal.TenantID, versionID, body.DeploymentID, body.FallbackID, body.Environment, principal.Subject, body.Override)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent version not found or is not a draft")
			return
		}
		if errors.Is(err, postgres.ErrModelNotApproved) {
			writeError(w, http.StatusConflict, "model deployment is not approved for this tenant environment")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "model binding could not be updated")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if !h.authorize(w, r, principal, "agent.model_binding.delete", resource) {
			return
		}
		if err := h.Registry.DeleteAgentModelBinding(r.Context(), principal.TenantID, versionID, principal.Subject); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "draft agent model binding not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "model binding could not be deleted")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (h ModelSecurityHandler) authorize(w http.ResponseWriter, r *http.Request, principal ports.Principal, action, resource string) bool {
	if err := h.Authorizer.Authorize(r.Context(), principal, action, resource); err != nil {
		writeError(w, http.StatusForbidden, "action is not authorized")
		return false
	}
	return true
}

func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) bool {
	if r.ContentLength > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be one valid JSON object")
		return false
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func requestedCapabilities(raw json.RawMessage) (map[string]bool, error) {
	var result map[string]bool
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, errors.New("capabilities are required")
	}
	if len(result) == 0 || len(result) > 2 {
		return nil, errors.New("capability set is invalid")
	}
	requested := false
	for key, value := range result {
		if key != "json" && key != "tools" {
			return nil, errors.New("capability is unsupported")
		}
		requested = requested || value
	}
	if !requested {
		return nil, errors.New("at least one capability must be requested")
	}
	return result, nil
}

func connectionOnlyCapabilities(raw json.RawMessage) (map[string]bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]bool{}, nil
	}
	var values map[string]bool
	if err := json.Unmarshal(raw, &values); err != nil || values == nil || len(values) != 0 {
		return nil, errors.New("connectivity mode does not accept inference capabilities")
	}
	return values, nil
}

func verifiedCapabilities(actual, requested map[string]bool) (json.RawMessage, error) {
	// At most catalog + json + tools (a successful capability probe always reports catalog as well).
	if actual == nil || len(actual) > 3 {
		return nil, errors.New("probe returned no capabilities")
	}
	for key := range actual {
		if key != "catalog" && key != "json" && key != "tools" {
			return nil, errors.New("probe returned unsupported capability")
		}
	}
	for key, required := range requested {
		if required && !actual[key] {
			return nil, errors.New("probe did not verify requested capability")
		}
	}
	encoded, err := json.Marshal(actual)
	return encoded, err
}

func providerScope(providerID string) string { return "provider:" + providerID }

func isAllowedEnvironment(value string) bool {
	switch value {
	case "development", "staging", "production":
		return true
	default:
		return false
	}
}

var _ = strconv.IntSize
