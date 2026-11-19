package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elenio/agent-studio/internal/auth"
	"github.com/elenio/agent-studio/internal/httpapi"
	"github.com/elenio/agent-studio/internal/ports"
	agentruntime "github.com/elenio/agent-studio/internal/runtime"
)

type denyIdentity struct{}

func TestRuntimeConfigCanDisableMocksWithoutEnablingThemInProduction(t *testing.T) {
	t.Setenv("AGENT_STUDIO_DEV_MOCK_MODEL", "false")
	cfg, err := loadRuntimeConfig(auth.EnvironmentDevelopment)
	if err != nil || cfg.allowDevMock {
		t.Fatalf("explicit real-model-only development failed: %v", err)
	}
	t.Setenv("AGENT_STUDIO_DEV_MOCK_MODEL", "true")
	if _, err := loadRuntimeConfig(auth.EnvironmentProduction); err == nil {
		t.Fatal("production accepted mock models")
	}
	t.Setenv("AGENT_STUDIO_DEV_MOCK_MODEL", "invalid")
	if _, err := loadRuntimeConfig(auth.EnvironmentDevelopment); err == nil {
		t.Fatal("invalid mock flag accepted")
	}
}

func (denyIdentity) VerifyRequest(context.Context, *http.Request) (ports.Principal, error) {
	return ports.Principal{}, errors.New("denied")
}

func (denyIdentity) Authorize(context.Context, ports.Principal, string, string) error {
	return errors.New("denied")
}

func TestRouterServesCollectionRoutesWithoutRedirect(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	router := newRouter(nil, denyIdentity{}, nil, httpapi.ProviderEndpointPolicy{}, func(context.Context) error { return nil }, webDir)
	for _, target := range []string{
		"/api/v1/model-providers?limit=5",
		"/api/v1/model-deployments?limit=5",
		"/api/v1/agents",
		"/api/v1/catalog/tools",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s = %d (Location %q), want 401 from an API handler", method, target, rec.Code, rec.Header().Get("Location"))
			}
		}
	}
}

func TestBindAddressRequiresLoopbackUnlessDevIdentity(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8190", ":8190", "100.126.146.52:8190"} {
		if err := requireBindAddress(addr, false); err == nil {
			t.Errorf("%s was accepted without development identity", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:5190", ":5190", "[::]:5190", "kibi-vm:5190"} {
		if err := requireBindAddress(addr, true); err == nil {
			t.Errorf("%s was accepted as a specific development interface", addr)
		}
	}
	if err := requireBindAddress("127.0.0.1:8190", false); err != nil {
		t.Fatal(err)
	}
	if err := requireBindAddress("100.126.146.52:5190", true); err != nil {
		t.Fatal(err)
	}
}

func TestSessionReportsDevelopmentIdentity(t *testing.T) {
	access, err := auth.NewTailscaleServe([]auth.AccessGrant{{
		Login: "operator@example.com", TenantID: "482cbf9a-1707-4be1-8e22-6b71c3c94071", Permissions: []string{"agent.read"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := auth.NewDevIdentity(auth.EnvironmentDevelopment, access, "operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for identity, want := range map[identityAdapter]string{access: "", dev: `"identity_mode":"development"`} {
		rec := httptest.NewRecorder()
		sessionHandler(identity, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
		if want == "" {
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("session without Serve header = %d, want 401", rec.Code)
			}
			continue
		}
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("dev session = %d %s", rec.Code, rec.Body.String())
		}
	}
}

func writeAccessMap(t *testing.T, login string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "access-map.json")
	body := `[{"login":"` + login + `","tenant_id":"482cbf9a-1707-4be1-8e22-6b71c3c94071","permissions":["agent.read"]}]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildHumanIdentityModes(t *testing.T) {
	const cpUser = "0b7e2a4c-91d3-4f5e-8a6b-1c2d3e4f5a6b"
	env := func(values map[string]string) func(string) string {
		return func(k string) string { return values[k] }
	}
	tsMap := writeAccessMap(t, "operator@example.com")
	cpMap := writeAccessMap(t, cpUser)
	cpEnv := func(extra map[string]string) map[string]string {
		values := map[string]string{
			"AGENT_STUDIO_HUMAN_AUTH":      "control_plane_jwt",
			"AGENT_STUDIO_ACCESS_MAP_FILE": cpMap,
			"AGENT_STUDIO_CP_ISSUER":       "http://100.126.146.52:5200",
			"AGENT_STUDIO_CP_JWKS_URL":     "https://cp.example/.well-known/jwks.json",
			"AGENT_STUDIO_CP_TENANT_KEY":   "acme-demo",
		}
		for k, v := range extra {
			values[k] = v
		}
		return values
	}

	// Default mode is unchanged: Tailscale Serve, or the dev login in development.
	human, err := buildHumanIdentity(env(map[string]string{"AGENT_STUDIO_ACCESS_MAP_FILE": tsMap}), auth.EnvironmentProduction)
	if _, ok := human.(*auth.TailscaleServe); err != nil || !ok {
		t.Fatalf("default mode = %T err=%v", human, err)
	}
	human, err = buildHumanIdentity(env(map[string]string{"AGENT_STUDIO_HUMAN_AUTH": "tailscale", "AGENT_STUDIO_ACCESS_MAP_FILE": tsMap,
		"AGENT_STUDIO_DEV_LOGIN": "operator@example.com"}), auth.EnvironmentDevelopment)
	if _, ok := human.(*auth.DevIdentity); err != nil || !ok {
		t.Fatalf("dev mode = %T err=%v", human, err)
	}
	if _, err := buildHumanIdentity(env(map[string]string{"AGENT_STUDIO_ACCESS_MAP_FILE": tsMap,
		"AGENT_STUDIO_DEV_LOGIN": "operator@example.com"}), auth.EnvironmentProduction); err == nil {
		t.Fatal("dev login accepted in production")
	}

	human, err = buildHumanIdentity(env(cpEnv(nil)), auth.EnvironmentProduction)
	if _, ok := human.(*auth.ControlPlaneJWT); err != nil || !ok {
		t.Fatalf("control plane mode = %T err=%v", human, err)
	}
	for name, values := range map[string]map[string]string{
		"dev login with control_plane_jwt": cpEnv(map[string]string{"AGENT_STUDIO_DEV_LOGIN": cpUser}),
		"http JWKS outside development":    cpEnv(map[string]string{"AGENT_STUDIO_CP_JWKS_URL": "http://100.126.146.52:5200/.well-known/jwks.json"}),
		"missing issuer":                   cpEnv(map[string]string{"AGENT_STUDIO_CP_ISSUER": ""}),
		"tailscale login in access map":    cpEnv(map[string]string{"AGENT_STUDIO_ACCESS_MAP_FILE": tsMap}),
		"unknown mode":                     {"AGENT_STUDIO_HUMAN_AUTH": "header", "AGENT_STUDIO_ACCESS_MAP_FILE": tsMap},
	} {
		if _, err := buildHumanIdentity(env(values), auth.EnvironmentProduction); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := buildHumanIdentity(env(cpEnv(map[string]string{"AGENT_STUDIO_DEV_LOGIN": cpUser})), auth.EnvironmentDevelopment); err == nil {
		t.Error("dev login with control_plane_jwt accepted in development")
	}
	if _, err := buildHumanIdentity(env(cpEnv(map[string]string{"AGENT_STUDIO_CP_JWKS_URL": "http://127.0.0.1:5200/.well-known/jwks.json"})), auth.EnvironmentDevelopment); err != nil {
		t.Errorf("http JWKS rejected in development: %v", err)
	}
}

func TestSessionReportsControlPlaneIdentityMode(t *testing.T) {
	cp, err := auth.NewControlPlaneJWT(auth.ControlPlaneConfig{
		Issuer: "http://100.126.146.52:5200", JWKSURL: "https://cp.example/.well-known/jwks.json", TenantKey: "acme-demo",
	}, []auth.AccessGrant{{Login: "0b7e2a4c-91d3-4f5e-8a6b-1c2d3e4f5a6b", TenantID: "482cbf9a-1707-4be1-8e22-6b71c3c94071", Permissions: []string{"agent.read"}}})
	if err != nil {
		t.Fatal(err)
	}
	identity := auth.Composite{Human: cp}
	if identity.IdentityMode() != "control-plane" {
		t.Fatalf("mode = %q", identity.IdentityMode())
	}
	// A forged Serve header is not identity evidence in this mode.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	r.Header.Set("Tailscale-User-Login", "operator@example.com")
	rec := httptest.NewRecorder()
	sessionHandler(identity, nil).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session with forged Serve header = %d, want 401", rec.Code)
	}
}

func TestRuntimeConfigKMSDelegationSettings(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "kms-token")
	if err := os.WriteFile(tokenFile, []byte("kms-service-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_STUDIO_KMS_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("AGENT_STUDIO_KMS_SERVICE_TOKEN_FILE", tokenFile)
	t.Setenv("AGENT_STUDIO_FLOWCASE_CALLBACK_URL", "")

	for _, header := range []string{"Authorization", "authorization", " AUTHORIZATION ", "Proxy-Authorization", "X Bad", "X-Delegated:User"} {
		t.Setenv("AGENT_STUDIO_KMS_USER_HEADER", header)
		if _, err := loadRuntimeConfig(auth.EnvironmentProduction); err == nil {
			t.Errorf("KMS user header %q was accepted", header)
		}
	}

	t.Setenv("AGENT_STUDIO_KMS_USER_HEADER", "X-KMS-Delegated-User")
	t.Setenv("AGENT_STUDIO_KMS_FORWARD_CASE_ID", "")
	cfg, err := loadRuntimeConfig(auth.EnvironmentProduction)
	if err != nil {
		t.Fatal(err)
	}
	kms, ok := cfg.kms.(agentruntime.KMSRetrieval)
	if !ok || kms.UserHeader != "X-KMS-Delegated-User" || kms.ServiceToken != "kms-service-token" || kms.ForwardCaseID {
		t.Fatalf("unexpected default KMS config: %+v", cfg.kms)
	}

	t.Setenv("AGENT_STUDIO_KMS_FORWARD_CASE_ID", "true")
	if cfg, err = loadRuntimeConfig(auth.EnvironmentProduction); err != nil || !cfg.kms.(agentruntime.KMSRetrieval).ForwardCaseID {
		t.Fatalf("AGENT_STUDIO_KMS_FORWARD_CASE_ID=true was not applied: %+v %v", cfg.kms, err)
	}
	t.Setenv("AGENT_STUDIO_KMS_FORWARD_CASE_ID", "sometimes")
	if _, err := loadRuntimeConfig(auth.EnvironmentProduction); err == nil {
		t.Fatal("invalid AGENT_STUDIO_KMS_FORWARD_CASE_ID was accepted")
	}
}

func TestSelfApprovalExemptSubjectsIsDevelopmentOnly(t *testing.T) {
	const demo = "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c06"
	if got, err := selfApprovalExemptSubjects("", auth.EnvironmentProduction); err != nil || got != nil {
		t.Fatalf("unset must be a no-op: %v %v", got, err)
	}
	if _, err := selfApprovalExemptSubjects(demo, auth.EnvironmentProduction); err == nil {
		t.Fatal("exemption accepted outside development")
	}
	if got, err := selfApprovalExemptSubjects(" "+demo+" ,", auth.EnvironmentDevelopment); err == nil {
		t.Fatalf("empty entry accepted: %v", got)
	}
	for _, bad := range []string{"demo", strings.ToUpper(demo), demo + "0"} {
		if _, err := selfApprovalExemptSubjects(bad, auth.EnvironmentDevelopment); err == nil {
			t.Fatalf("invalid subject %q accepted", bad)
		}
	}
	got, err := selfApprovalExemptSubjects(demo+", 00000000-0000-4000-8000-000000000001", auth.EnvironmentDevelopment)
	if err != nil || len(got) != 2 || got[0] != demo {
		t.Fatalf("valid list rejected: %v %v", got, err)
	}
}

func TestSessionReportsSelfApprovalExemption(t *testing.T) {
	access, err := auth.NewTailscaleServe([]auth.AccessGrant{{
		Login: "operator@example.com", TenantID: "482cbf9a-1707-4be1-8e22-6b71c3c94071", Permissions: []string{"release.review"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := auth.NewDevIdentity(auth.EnvironmentDevelopment, access, "operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for subject, want := range map[string]string{"operator@example.com": `"self_approval_exempt":true`, "someone-else": `"self_approval_exempt":false`} {
		rec := httptest.NewRecorder()
		sessionHandler(dev, func(s string) bool { return s == subject }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("session for exempt=%s: %d %s", subject, rec.Code, rec.Body.String())
		}
	}
}
