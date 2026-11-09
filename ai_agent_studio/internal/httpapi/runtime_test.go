package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
)

type serviceAuth struct{}

func (serviceAuth) VerifyRequest(context.Context, *http.Request) (ports.Principal, error) {
	return ports.Principal{Issuer: "service-token", Subject: "service:flowcase", TenantID: "482cbf9a-1707-4be1-8e22-6b71c3c94071", Kind: "service"}, nil
}
func (serviceAuth) Authorize(context.Context, ports.Principal, string, string) error { return nil }

type denyAll struct{}

func (denyAll) VerifyRequest(context.Context, *http.Request) (ports.Principal, error) {
	return ports.Principal{}, errors.New("no identity")
}
func (denyAll) Authorize(context.Context, ports.Principal, string, string) error {
	return errors.New("denied")
}

func TestServiceClientsReachRunEndpointsOnly(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Auth: serviceAuth{}, Authorizer: serviceAuth{}})
	for _, path := range []string{"/api/v1/evaluations/suites", "/api/v1/releases/requests", "/api/v1/metrics", "/api/v1/audit-events", "/api/v1/sandbox/runs"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("service GET %s = %d, want 403", path, rec.Code)
		}
	}
}

func TestRuntimeRoutesRequireIdentityAndValidRunBodies(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Auth: denyAll{}, Authorizer: denyAll{}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated run creation = %d", rec.Code)
	}

	mux = http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Auth: serviceAuth{}, Authorizer: serviceAuth{}})
	for _, body := range []string{`{"agent_id":"sim.intake","input":{}}`, `{"idempotency_key":"short","agent_id":"sim.intake","input":{}}`,
		`{"idempotency_key":"node-1:attempt-1","agent_id":"sim.intake","input":[]}`, `{"idempotency_key":"node-1:attempt-1","agent_id":"sim.intake","input":{},"callback_url":"https://evil"}`} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /runs %s = %d, want 400", body, rec.Code)
		}
	}
}

func TestRateLimiterIsPerTenantWindow(t *testing.T) {
	now := time.Now()
	l := NewRateLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	if !l.Allow("a") || !l.Allow("a") || l.Allow("a") {
		t.Fatal("limit was not enforced")
	}
	if !l.Allow("b") {
		t.Fatal("another tenant was limited")
	}
	now = now.Add(time.Minute)
	if !l.Allow("a") {
		t.Fatal("window did not reset")
	}
}

type userAuth struct{}

func (userAuth) VerifyRequest(context.Context, *http.Request) (ports.Principal, error) {
	return ports.Principal{Issuer: "control-plane", Subject: "0b7d1f5e-3c55-4c1e-9d1a-3f1f5b0b9a11", TenantID: "482cbf9a-1707-4be1-8e22-6b71c3c94071", Kind: "user"}, nil
}
func (userAuth) Authorize(context.Context, ports.Principal, string, string) error { return nil }

// captureStore records the run a handler asks the repository to create.
type captureStore struct {
	RuntimeStore
	calls int
	got   postgres.RunCreate
}

func (s *captureStore) CreateRun(_ context.Context, _ string, in postgres.RunCreate) (postgres.Run, bool, error) {
	s.calls++
	s.got = in
	return postgres.Run{ID: "11111111-1111-4111-8111-111111111111", AgentKey: in.AgentKey, Status: "queued"}, true, nil
}

func TestCreateRunDelegatedUserIsServiceOnlyAndValidated(t *testing.T) {
	const owner = "3F2B8C1D-7E4A-4B6C-9D0E-1A2B3C4D5E6F"
	body := func(delegated string) string {
		return `{"idempotency_key":"node-1:attempt-1","agent_id":"sim.intake","input":{},"case_id":"fc-case-9","delegated_user":"` + delegated + `"}`
	}

	store := &captureStore{}
	mux := http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Store: store, Auth: serviceAuth{}, Authorizer: serviceAuth{}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(body(owner))))
	if rec.Code != http.StatusAccepted || store.calls != 1 {
		t.Fatalf("service run with delegated_user = %d (%s), calls=%d", rec.Code, rec.Body.String(), store.calls)
	}
	if store.got.DelegatedUser != strings.ToLower(owner) || store.got.CaseRef != "fc-case-9" || store.got.CallerKind != "service" {
		t.Fatalf("delegated user was not passed lowercased with the case ref: %+v", store.got)
	}

	for _, bad := range []string{"not-a-uuid", "3f2b8c1d7e4a4b6c9d0e1a2b3c4d5e6f", "{3f2b8c1d-7e4a-4b6c-9d0e-1a2b3c4d5e6f}", " 3f2b8c1d-7e4a-4b6c-9d0e-1a2b3c4d5e6f"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(body(bad))))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("delegated_user %q = %d, want 400", bad, rec.Code)
		}
	}
	if store.calls != 1 {
		t.Fatalf("invalid delegated users reached the store: calls=%d", store.calls)
	}

	human := &captureStore{}
	mux = http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Store: human, Auth: userAuth{}, Authorizer: userAuth{}})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(body(strings.ToLower(owner)))))
	if rec.Code != http.StatusBadRequest || human.calls != 0 {
		t.Fatalf("human caller with delegated_user = %d, calls=%d; want 400 and no run", rec.Code, human.calls)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{"idempotency_key":"node-1:attempt-1","agent_id":"sim.intake","input":{}}`)))
	if rec.Code != http.StatusAccepted || human.got.DelegatedUser != "" {
		t.Fatalf("human run without delegated_user = %d, delegated=%q", rec.Code, human.got.DelegatedUser)
	}
}

// settledStore returns one settled service run and its evidence.
type settledStore struct {
	RuntimeStore
	evidenceErr error
}

func (settledStore) GetRun(context.Context, string, string) (postgres.Run, error) {
	return postgres.Run{ID: "22222222-2222-4222-8222-222222222222", AgentKey: "sim.intake", AgentSemver: "0.1.0", Status: "succeeded",
		CallerKind: "service", CallerSubject: "service:flowcase", Output: json.RawMessage(`{"requirements":[]}`),
		Validation: json.RawMessage(`{"schema_valid":true}`), Pinned: json.RawMessage(`{}`), ModelMode: "provider"}, nil
}

func (s settledStore) ListRunEvidence(context.Context, string, string) ([]postgres.EvidenceRef, error) {
	if s.evidenceErr != nil {
		return nil, s.evidenceErr
	}
	return []postgres.EvidenceRef{{SourceID: "doc-1", SourceVersion: "ver-1", Title: "Telkom Digital DPI — Katalog Use Case", Cited: true},
		{SourceID: "doc-2", SourceVersion: "ver-2", Title: "Not cited", Cited: false}}, nil
}

func TestServiceRunResultIncludesCitedSourceTitles(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Store: settledStore{}, Auth: serviceAuth{}, Authorizer: serviceAuth{}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/runs/22222222-2222-4222-8222-222222222222", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get run = %d %s", rec.Code, rec.Body.String())
	}
	var res postgres.FlowcaseRunResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Sources []postgres.FlowcaseSource `json:"sources"`
	}
	if err := json.Unmarshal(res.Output, &env); err != nil || len(env.Sources) != 1 || env.Sources[0].Title != "Telkom Digital DPI — Katalog Use Case" {
		t.Fatalf("sources = %+v (%v)", env.Sources, err)
	}
	if sum, _ := postgres.OutputChecksum(res.Output); sum != res.OutputChecksum {
		t.Fatalf("checksum does not cover the output with sources")
	}

	mux = http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Store: settledStore{evidenceErr: errors.New("db down")}, Auth: serviceAuth{}, Authorizer: serviceAuth{}})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/runs/22222222-2222-4222-8222-222222222222", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("evidence failure must be retryable (503), got %d", rec.Code)
	}
}
