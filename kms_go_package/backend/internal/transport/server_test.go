package transport

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/observability"
)

func TestSessionRequiresAuthentication(t *testing.T) {
	server := NewServer(Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: auth.NewTrustedSessionAdapter(nil)})
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHealthzIsUnauthenticated(t *testing.T) {
	server := NewServer(Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: auth.NewTrustedSessionAdapter(nil)})
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "ok") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRetrievalRouteDoesNotAcceptDevelopmentHumanAuth(t *testing.T) {
	server := NewServer(Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: auth.NewDevelopmentMock("00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002")})
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/v1/retrieval/query", strings.NewReader(`{"query":"test"}`)))
	if resp.Code != http.StatusUnauthorized || !strings.Contains(resp.Body.String(), "invalid_service_credential") {
		t.Fatalf("status=%d body=%s; retrieval must require a service bearer credential", resp.Code, resp.Body.String())
	}
}

type failingAuditDB struct{ err error }

func (db failingAuditDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, db.err
}

func TestRetrievalAuditPersistenceErrorIsReturned(t *testing.T) {
	writeErr := errors.New("audit database unavailable")
	if err := persistRetrievalAudit(context.Background(), failingAuditDB{err: writeErr}, retrievalAudit{TenantID: "tenant", ClientID: "client", UserID: "user", Outcome: "allowed", ResultCount: 1}); !errors.Is(err, writeErr) {
		t.Fatalf("persistRetrievalAudit error=%v, want %v", err, writeErr)
	}
}

func TestRetrievalAuditFailureReturnsUnavailableWithoutResults(t *testing.T) {
	resp := httptest.NewRecorder()
	if retrievalAuditOrFail(resp, context.Background(), failingAuditDB{err: errors.New("audit unavailable")}, retrievalAudit{TenantID: "tenant", ClientID: "client", UserID: "user", Outcome: "allowed", ResultCount: 3}, "results were not returned") {
		t.Fatal("failed audit write must block the response")
	}
	if resp.Code != http.StatusServiceUnavailable || !strings.Contains(resp.Body.String(), "audit_unavailable") || strings.Contains(resp.Body.String(), "results") && strings.Contains(resp.Body.String(), "result_count") {
		t.Fatalf("status=%d body=%s, want fail-closed audit error without search results", resp.Code, resp.Body.String())
	}
}

func TestResponsesCarryRequestIDAndHonorWellFormedCallerID(t *testing.T) {
	server := NewServer(Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: auth.NewTrustedSessionAdapter(nil)})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "host-request-0001")
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)
	if got := resp.Header().Get("X-Request-ID"); got != "host-request-0001" {
		t.Fatalf("X-Request-ID=%q", got)
	}
	resp = httptest.NewRecorder()
	server.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if resp.Header().Get("X-Request-ID") == "" {
		t.Fatal("generated request ID missing")
	}
}

func TestMetricsAreClosedOutsideDevelopmentWithoutToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	production := NewServer(Dependencies{Logger: logger, Auth: auth.NewTrustedSessionAdapter(nil), Operations: OperationsConfig{Environment: "production"}})
	resp := httptest.NewRecorder()
	production.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if resp.Code != http.StatusNotFound {
		t.Fatalf("production metrics without token status=%d", resp.Code)
	}
	token := strings.Repeat("m", 32)
	protected := NewServer(Dependencies{Logger: logger, Auth: auth.NewTrustedSessionAdapter(nil), Operations: OperationsConfig{Environment: "production", MetricsToken: token}})
	resp = httptest.NewRecorder()
	protected.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if resp.Code != http.StatusNotFound {
		t.Fatalf("metrics without bearer status=%d", resp.Code)
	}
	protected.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = httptest.NewRecorder()
	protected.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `route="GET /healthz"`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestInternalErrorsAreLoggedWithRequestID(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := instrument(logger, observability.NewMetrics(), authMiddleware(auth.NewDevelopmentMock("00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeServiceError(w, r, errors.New("database connection reset"))
		})))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/collections", nil)
	req.Header.Set("X-Request-ID", "trace-internal-01")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusInternalServerError || strings.Contains(resp.Body.String(), "connection reset") {
		t.Fatalf("status=%d body=%s; cause must stay server-side", resp.Code, resp.Body.String())
	}
	for _, want := range []string{`"msg":"workspace request failed"`, `"error":"database connection reset"`, `"request_id":"trace-internal-01"`, `"tenant_id":"00000000-0000-4000-8000-000000000002"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log missing %s: %s", want, logs.String())
		}
	}
}

func TestSessionStatusDistinguishesUnavailableVerifierFromRejectedToken(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"verifier unavailable": {auth.ErrVerifierUnavailable, http.StatusServiceUnavailable},
		"token rejected":       {&auth.TokenRejection{Reason: "expired"}, http.StatusUnauthorized},
		"not configured stub":  {auth.ErrVerifierNotConfigured, http.StatusUnauthorized},
	} {
		t.Run(name, func(t *testing.T) {
			verifier := auth.TrustedSessionVerifierFunc(func(context.Context, *http.Request) (auth.Identity, error) { return auth.Identity{}, tc.err })
			server := NewServer(Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: auth.NewTrustedSessionAdapter(verifier)})
			resp := httptest.NewRecorder()
			server.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
			if resp.Code != tc.status {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
		})
	}
}
