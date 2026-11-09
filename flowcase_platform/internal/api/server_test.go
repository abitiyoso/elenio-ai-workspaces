package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/platform/auth"
)

type stubAuth struct {
	id   auth.Identity
	err  error
	mode string
}

func (s stubAuth) Authenticate(context.Context, *http.Request) (auth.Identity, error) {
	return s.id, s.err
}
func (s stubAuth) Mode() string { return s.mode }

func newTestServer(a auth.AuthAdapter, checks ...Check) (http.Handler, *bytes.Buffer) {
	var logs bytes.Buffer
	return NewServer(Dependencies{
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Environment: "test", Auth: a,
		Readiness: checks, Knowledge: kms.NewDisabled(), Agents: agent.NewDisabled(),
	}), &logs
}

func do(h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, body io.Reader) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

var devID = auth.Identity{UserID: "u-1", TenantID: "t-1", DisplayName: "Dev", Roles: []auth.Role{auth.RoleWorkspaceMember}, AuthMode: "dev_mock"}

func TestHealthAndReadiness(t *testing.T) {
	ok := Check{Name: "postgres", Run: func(context.Context) error { return nil }}
	down := Check{Name: "schema", Run: func(context.Context) error { return errors.New("relation secret_table missing") }}

	h, _ := newTestServer(stubAuth{mode: "dev_mock", id: devID}, ok)
	if w := do(h, http.MethodGet, "/healthz", nil); w.Code != http.StatusOK {
		t.Fatalf("healthz: %d", w.Code)
	}
	if w := do(h, http.MethodGet, "/readyz", nil); w.Code != http.StatusOK {
		t.Fatalf("readyz should be ready: %d %s", w.Code, w.Body)
	}

	h, logs := newTestServer(stubAuth{mode: "dev_mock", id: devID}, ok, down)
	w := do(h, http.MethodGet, "/readyz", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz should fail: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret_table") {
		t.Fatal("readiness must not leak internal error text")
	}
	if !strings.Contains(logs.String(), "secret_table") {
		t.Fatal("readiness failure should be logged server-side")
	}
}

func TestUnauthenticatedRequestsGetStructuredError(t *testing.T) {
	h, _ := newTestServer(stubAuth{mode: "trusted_jwt", err: auth.ErrUnauthenticated})
	for _, path := range []string{"/api/v1/session", "/api/v1/integrations", "/api/v1/does-not-exist"} {
		w := do(h, http.MethodGet, path, map[string]string{"X-Request-ID": "trace-abc-12345", "X-User-ID": "admin"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d", path, w.Code)
		}
		body := decode[map[string]errorBody](t, w.Body)["error"]
		if body.Code != "unauthenticated" || body.TraceID != "trace-abc-12345" {
			t.Fatalf("%s: unexpected error body %+v", path, body)
		}
		if w.Header().Get("WWW-Authenticate") == "" || w.Header().Get("X-Request-ID") != "trace-abc-12345" {
			t.Fatalf("%s: missing auth challenge or trace header", path)
		}
	}
}

func TestMisconfiguredAuthFailsClosedWith503(t *testing.T) {
	h, _ := newTestServer(stubAuth{mode: "trusted_jwt", err: auth.ErrMisconfigured})
	if w := do(h, http.MethodGet, "/api/v1/session", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	h, _ = newTestServer(stubAuth{mode: "trusted_jwt", err: errors.New("unexpected")})
	if w := do(h, http.MethodGet, "/api/v1/session", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unknown auth errors must not authenticate; got %d", w.Code)
	}
}

func TestSessionReturnsServerResolvedIdentity(t *testing.T) {
	h, _ := newTestServer(stubAuth{mode: "dev_mock", id: devID})
	w := do(h, http.MethodGet, "/api/v1/session", map[string]string{"X-Tenant-ID": "evil"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	s := decode[sessionResponse](t, w.Body)
	if s.TenantID != "t-1" || s.AuthMode != "dev_mock" || len(s.Roles) != 1 || s.ExpiresAt != nil {
		t.Fatalf("unexpected session %+v", s)
	}
}

func TestIntegrationsExposeModesOnly(t *testing.T) {
	ok := Check{Name: "postgres", Run: func(context.Context) error { return nil }}
	h, _ := newTestServer(stubAuth{mode: "dev_mock", id: devID}, ok)
	w := do(h, http.MethodGet, "/api/v1/integrations", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	got := decode[map[string]integrationStatus](t, w.Body)
	if got["host_auth"].Status != "warning" || got["knowledge"].Status != "disabled" || got["agent_runtime"].Status != "disabled" || got["case_runtime"].Status != "ok" {
		t.Fatalf("unexpected integration statuses %+v", got)
	}
}

func TestSecurityHeadersAndPanicRecovery(t *testing.T) {
	h, logs := newTestServer(stubAuth{mode: "dev_mock", id: devID}, Check{Name: "boom", Run: func(context.Context) error { panic("kaboom") }})
	w := do(h, http.MethodGet, "/readyz", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after panic, got %d", w.Code)
	}
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer"} {
		if w.Header().Get(k) != v {
			t.Fatalf("header %s = %q, want %q", k, w.Header().Get(k), v)
		}
	}
	if !strings.Contains(logs.String(), "kaboom") {
		t.Fatal("panic should be logged")
	}
}

func TestRequestLogOmitsAuthorizationAndQuery(t *testing.T) {
	h, logs := newTestServer(stubAuth{mode: "dev_mock", id: devID})
	do(h, http.MethodGet, "/api/v1/session?access_token=leaky.token.value", map[string]string{"Authorization": "Bearer secret.jwt.value"})
	out := logs.String()
	if strings.Contains(out, "leaky.token.value") || strings.Contains(out, "secret.jwt.value") {
		t.Fatalf("request log leaked a token: %s", out)
	}
	if !strings.Contains(out, "GET /api/v1/session") {
		t.Fatalf("request log should contain the route pattern: %s", out)
	}
}
