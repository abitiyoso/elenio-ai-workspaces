package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
)

type evidenceStore struct {
	RuntimeStore
	calls                         int
	tenant, key, pin, environment string
}

func (s *evidenceStore) ObserveRelease(_ context.Context, tenant, key, pin, environment string) (postgres.ReleaseEvidence, error) {
	s.calls++
	s.tenant, s.key, s.pin, s.environment = tenant, key, pin, environment
	return postgres.ReleaseEvidence{Schema: "elenio.agent-release-evidence.v1", AgentKey: key, Environment: environment, ContentHash: strings.Repeat("a", 64), State: "blocked", Blockers: []string{"fixture"}}, nil
}

type denyReleaseRead struct{ userAuth }

func (denyReleaseRead) Authorize(context.Context, ports.Principal, string, string) error {
	return errors.New("denied")
}

func TestReleaseEvidenceHTTPIsHumanAuthorizedScopedAndReadOnly(t *testing.T) {
	store := &evidenceStore{}
	mux := http.NewServeMux()
	RegisterRuntimeRoutes(mux, RuntimeHandler{Store: store, Auth: userAuth{}, Authorizer: userAuth{}, ReleaseEnvironment: "staging"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases/evidence?agent_key=finance.extract&skill_version=1.0.0", nil)
	req.Header.Set("X-Tenant-ID", "forged-tenant")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("ETag") != `"`+strings.Repeat("a", 64)+`"` ||
		store.calls != 1 || store.tenant != "482cbf9a-1707-4be1-8e22-6b71c3c94071" || store.key != "finance.extract" || store.pin != "1.0.0" || store.environment != "staging" {
		t.Fatalf("scope or envelope: %d %s %+v", rec.Code, rec.Body, store)
	}
	for _, path := range []string{"/api/v1/releases/evidence", "/api/v1/releases/evidence?agent_key=x&environment=production", "/api/v1/releases/evidence?agent_key=x&agent_key=y", "/api/v1/releases/evidence?agent_key=bad%20key", "/api/v1/releases/evidence?agent_key=x&skill_version=bad%20pin"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 400 || store.calls != 1 {
			t.Fatalf("untrusted query reached source: %s %d", path, rec.Code)
		}
	}
	for _, tc := range []struct {
		name      string
		auth      ports.AuthAdapter
		authorize ports.Authorizer
		want      int
	}{
		{"service", serviceAuth{}, serviceAuth{}, 403}, {"anonymous", denyAll{}, denyAll{}, 401}, {"no release grant", userAuth{}, denyReleaseRead{}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.NewServeMux()
			RegisterRuntimeRoutes(handler, RuntimeHandler{Store: store, Auth: tc.auth, Authorizer: tc.authorize})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/releases/evidence?agent_key=finance.extract", nil))
			if rec.Code != tc.want || store.calls != 1 {
				t.Fatalf("unauthorized read: %d", rec.Code)
			}
		})
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/releases/evidence?agent_key=finance.extract", nil))
	if rec.Code != 405 || store.calls != 1 {
		t.Fatal("mutation accepted", rec.Code)
	}
}
