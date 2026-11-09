package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
)

type probeHandlerAuth struct{}

func (probeHandlerAuth) VerifyRequest(context.Context, *http.Request) (ports.Principal, error) {
	return ports.Principal{TenantID: "00000000-0000-4000-8000-000000000001", Subject: "probe-test"}, nil
}

type probeHandlerAuthorizer struct{ actions []string }

func (a *probeHandlerAuthorizer) Authorize(_ context.Context, _ ports.Principal, action, _ string) error {
	a.actions = append(a.actions, action)
	return nil
}

type probeHandlerStore struct {
	provider        postgres.ProviderSecurityRecord
	probeTargetGets int
}

func (s *probeHandlerStore) GetProviderSecurity(context.Context, string, string) (postgres.ProviderSecurityRecord, error) {
	return s.provider, nil
}
func (*probeHandlerStore) SetProviderCredentialRef(context.Context, string, string, string, string) (string, error) {
	return "", errors.New("unexpected credential write")
}
func (*probeHandlerStore) DeleteProviderCredentialRef(context.Context, string, string, string) (string, error) {
	return "", errors.New("unexpected credential delete")
}
func (s *probeHandlerStore) GetProviderProbeTarget(context.Context, string, string, string) (postgres.ProviderProbeTarget, error) {
	s.probeTargetGets++
	return postgres.ProviderProbeTarget{}, sql.ErrNoRows
}
func (*probeHandlerStore) RecordProviderProbe(context.Context, string, string, string, string, string, json.RawMessage, string) (postgres.ProviderProbeRecord, error) {
	return postgres.ProviderProbeRecord{}, errors.New("unexpected probe result write")
}
func (*probeHandlerStore) ListModelApprovals(context.Context, string, string, string) ([]postgres.ModelApproval, error) {
	return nil, errors.New("unexpected approval read")
}
func (*probeHandlerStore) SetModelApproval(context.Context, string, string, string, string, string, string) (postgres.ModelApproval, error) {
	return postgres.ModelApproval{}, errors.New("unexpected approval write")
}
func (*probeHandlerStore) GetAgentModelBinding(context.Context, string, string) (postgres.AgentModelBinding, error) {
	return postgres.AgentModelBinding{}, errors.New("unexpected binding read")
}
func (*probeHandlerStore) SetAgentModelBinding(context.Context, string, string, string, string, string, string, json.RawMessage) (postgres.AgentModelBinding, error) {
	return postgres.AgentModelBinding{}, errors.New("unexpected binding write")
}
func (*probeHandlerStore) DeleteAgentModelBinding(context.Context, string, string, string) error {
	return errors.New("unexpected binding delete")
}

type probeHandlerVault struct{ resolveCalls int }

func (*probeHandlerVault) Put(context.Context, string, []byte) (string, error) {
	return "", errors.New("unexpected vault write")
}
func (*probeHandlerVault) Resolve(context.Context, string) ([]byte, error) {
	return nil, errors.New("unexpected unscoped resolve")
}
func (*probeHandlerVault) Revoke(context.Context, string) error { return nil }
func (v *probeHandlerVault) ResolveScoped(context.Context, string, string) ([]byte, error) {
	v.resolveCalls++
	return []byte("secret"), nil
}

type probeHandlerProbe struct{ calls int }

func (p *probeHandlerProbe) Probe(context.Context, ProviderProbeInput, []byte, *http.Client) (ProviderProbeResult, error) {
	p.calls++
	return ProviderProbeResult{}, nil
}

func testModelSecurityHandler(store *probeHandlerStore, vault *probeHandlerVault, authorizer *probeHandlerAuthorizer, probe ProviderProbe) ModelSecurityHandler {
	return ModelSecurityHandler{
		Registry: store, Vault: vault, Auth: probeHandlerAuth{}, Authorizer: authorizer,
		EndpointPolicy: ProviderEndpointPolicy{}, Probe: probe,
	}
}

func TestCredentialStatusNeverReturnsVaultReference(t *testing.T) {
	store := &probeHandlerStore{provider: postgres.ProviderSecurityRecord{
		ID: "00000000-0000-4000-8000-000000000002", Status: "active", SecretRef: "filevault://private-reference",
	}}
	authorizer := &probeHandlerAuthorizer{}
	handler := testModelSecurityHandler(store, &probeHandlerVault{}, authorizer, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-providers/00000000-0000-4000-8000-000000000002/credentials", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("credential status returned %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private-reference") || strings.Contains(response.Body.String(), "secret_ref") {
		t.Fatalf("credential status disclosed vault reference: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"credential_configured":true`) || len(authorizer.actions) != 1 || authorizer.actions[0] != "model_provider.credential.read" {
		t.Fatalf("credential status response or permission mismatch: %s actions=%v", response.Body.String(), authorizer.actions)
	}
}

func TestCapabilityProbeRequiresCostAcknowledgementBeforeSecretResolution(t *testing.T) {
	store := &probeHandlerStore{}
	vault := &probeHandlerVault{}
	probe := &probeHandlerProbe{}
	authorizer := &probeHandlerAuthorizer{}
	handler := testModelSecurityHandler(store, vault, authorizer, probe)
	body := `{"mode":"capabilities","deployment_id":"00000000-0000-4000-8000-000000000003","capabilities":{"json":true}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/model-providers/00000000-0000-4000-8000-000000000002/probe", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), `"may_incur_token_cost":true`) {
		t.Fatalf("unacknowledged capability probe returned %d: %s", response.Code, response.Body.String())
	}
	if store.probeTargetGets != 0 || vault.resolveCalls != 0 || probe.calls != 0 {
		t.Fatalf("unacknowledged probe reached secret/network stage: target=%d secret=%d probe=%d", store.probeTargetGets, vault.resolveCalls, probe.calls)
	}
}
