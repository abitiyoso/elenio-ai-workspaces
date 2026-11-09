package api

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/workflows/registry"
)

var (
	specPath   = regexp.MustCompile(`^  (/[^:]*):\s*$`)
	specMethod = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	pathParam  = regexp.MustCompile(`\{[a-z_]+\}`)
)

type operation struct{ method, path string }

func documentedOperations(t *testing.T) []operation {
	t.Helper()
	f, err := os.Open("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ops []operation
	inPaths, current := false, ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && len(line) > 0 && line[0] != ' ':
			inPaths = false
		case inPaths:
			if m := specPath.FindStringSubmatch(line); m != nil {
				current = m[1]
			} else if m := specMethod.FindStringSubmatch(line); m != nil && current != "" {
				ops = append(ops, operation{strings.ToUpper(m[1]), current})
			}
		}
	}
	return ops
}

// TestOpenAPIOperationsAreRouted keeps api/openapi.yaml and the router in sync: every documented
// operation reaches a handler (never the route-not-found or method-not-allowed fallbacks), and
// every routed path is documented. The caller has no roles, so registry handlers stop at 403
// before touching the database.
func TestOpenAPIOperationsAreRouted(t *testing.T) {
	ops := documentedOperations(t)
	if len(ops) < 30 {
		t.Fatalf("parsed only %d operations from openapi.yaml", len(ops))
	}
	ok := Check{Name: "postgres", Run: func(context.Context) error { return nil }}
	noRoles := auth.Identity{UserID: "u", TenantID: "t", AuthMode: "dev_mock"}
	var logs strings.Builder
	h := NewServer(Dependencies{Logger: discardLogger(&logs), Environment: "test", Auth: stubAuth{mode: "dev_mock", id: noRoles},
		Readiness: []Check{ok}, Knowledge: kmsDisabled(), Agents: agentDisabled(),
		Registry: &Registry{Service: &registry.Service{}, Catalog: nil}, Cases: &cases.Engine{}})

	documented := map[string]bool{}
	for _, op := range ops {
		documented[op.path] = true
		path := pathParam.ReplaceAllString(op.path, "1")
		r := httptest.NewRequest(op.method, path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"1"`)
		r.Header.Set("Idempotency-Key", "contract-test-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var body struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code == http.StatusMethodNotAllowed || body.Error.Code == "route_not_found" || w.Code == http.StatusInternalServerError {
			t.Errorf("%s %s is documented but not routed correctly (%d %s)", op.method, op.path, w.Code, body.Error.Code)
		}
	}
	for _, p := range []string{"/healthz", "/readyz", "/api/v1/session", "/api/v1/integrations", "/api/v1/workflows",
		"/api/v1/cases", "/api/v1/cases/{id}", "/api/v1/tasks/{id}/complete", "/api/v1/approvals/{id}/decision",
		"/api/v1/drafts", "/api/v1/drafts/{id}", "/api/v1/drafts/{id}/submit", "/api/v1/drafts/{id}/revision",
		"/api/v1/workflows/{key}", "/api/v1/workflows/{key}/versions/{version}/publish", "/api/v1/audit-events"} {
		if !documented[p] {
			t.Errorf("routed path %s is missing from openapi.yaml", p)
		}
	}
	w := do(h, http.MethodGet, "/api/v1/undocumented", nil)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "route_not_found") {
		t.Errorf("unknown API paths must 404 route_not_found, got %d", w.Code)
	}
	if !slices.ContainsFunc(ops, func(o operation) bool { return o.method == "PATCH" }) {
		t.Error("PATCH draft operation must be documented")
	}
}

func discardLogger(w *strings.Builder) *slog.Logger { return slog.New(slog.NewJSONHandler(w, nil)) }
func kmsDisabled() kms.KnowledgeProvider            { return kms.NewDisabled() }
func agentDisabled() agent.AgentExecutor            { return agent.NewDisabled() }
