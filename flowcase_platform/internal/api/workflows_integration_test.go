package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elenio/flowcase/internal/api"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/testutil/pgtest"
	"github.com/elenio/flowcase/internal/workflows/registry"
)

const (
	tenantID   = "00000000-0000-4000-8000-000000000001"
	designerID = "00000000-0000-4000-8000-000000000101"
	memberID   = "00000000-0000-4000-8000-000000000102"
)

type client struct {
	t *testing.T
	h http.Handler
}

func (c client) do(method, path string, body any, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rdr)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func newServer(t *testing.T) (client, client) {
	pool, _ := pgtest.Migrated(t)
	ctx := context.Background()
	roles := []string{"workspace_member", "workflow_designer", "workflow_publisher"}
	if err := postgres.SeedDevelopment(ctx, pool, postgres.DevSeed{TenantID: tenantID, UserID: designerID, DisplayName: "Ada", Roles: roles}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, issuer, subject, display_name) VALUES ($1, 'flowcase-dev-mock', $1::uuid::text, 'Mia')`, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO memberships (tenant_id, user_id) VALUES ($1, $2)`, tenantID, memberID); err != nil {
		t.Fatal(err)
	}
	ps, _ := packs.Load()
	cat, _ := packs.Catalog(ps)
	tpls, _ := packs.Templates(ps)
	reg := &api.Registry{Service: &registry.Service{DB: pool, Catalog: cat, Templates: tpls}, Catalog: cat, Templates: tpls}
	build := func(a auth.AuthAdapter) client {
		return client{t: t, h: api.NewServer(api.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Environment: "test",
			Auth: a, Knowledge: kms.NewDisabled(), Agents: agent.NewDisabled(), Registry: reg})}
	}
	designer := build(auth.NewDevMockAdapter(designerID, tenantID, "Ada", []auth.Role{auth.RoleWorkspaceMember, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher}))
	member := build(auth.NewDevMockAdapter(memberID, tenantID, "Mia", []auth.Role{auth.RoleWorkspaceMember}))
	return designer, member
}

func TestWorkflowLifecycleOverHTTP(t *testing.T) {
	c, member := newServer(t)
	create := map[string]any{"key": "poc-http", "template": "internal-poc-request", "name": "PoC over HTTP"}

	if w, b := c.do("POST", "/api/v1/workflows", create, nil); w.Code != 400 || errCode(b) != "idempotency_key_required" {
		t.Fatalf("missing Idempotency-Key: %d %v", w.Code, b)
	}
	r := httptest.NewRequest("POST", "/api/v1/workflows", strings.NewReader(`{"key":"x"}`))
	r.Header.Set("Content-Type", "text/plain")
	if w := httptest.NewRecorder(); func() int { c.h.ServeHTTP(w, r); return w.Code }() != 415 {
		t.Fatal("non-JSON bodies must be rejected with 415")
	}
	if w, b := c.do("POST", "/api/v1/workflows", map[string]any{"key": "x-y", "evil": true}, map[string]string{"Idempotency-Key": "unknown-field-1"}); w.Code != 400 || errCode(b) != "invalid_json" {
		t.Fatalf("unknown body fields must be rejected: %d %v", w.Code, b)
	}

	w, created := c.do("POST", "/api/v1/workflows", create, map[string]string{"Idempotency-Key": "create-poc-http-1"})
	if w.Code != 201 || w.Header().Get("ETag") != `"1"` || w.Header().Get("Location") != "/api/v1/workflows/poc-http/versions/1" {
		t.Fatalf("create: %d %v %v", w.Code, w.Header(), created)
	}
	id := created["version"].(map[string]any)["id"]
	if w, again := c.do("POST", "/api/v1/workflows", create, map[string]string{"Idempotency-Key": "create-poc-http-1"}); w.Code != 201 || again["version"].(map[string]any)["id"] != id {
		t.Fatalf("idempotent replay must return the same version: %d %v", w.Code, again)
	}

	if w, b := member.do("POST", "/api/v1/workflows", map[string]any{"key": "member-flow", "name": "Member flow"}, map[string]string{"Idempotency-Key": "member-create-1"}); w.Code != 403 || errCode(b) != "forbidden" {
		t.Fatalf("member create must be 403: %d %v", w.Code, b)
	}

	w, ver := c.do("GET", "/api/v1/workflows/poc-http/versions/1", nil, nil)
	if w.Code != 200 || w.Header().Get("ETag") != `"1"` {
		t.Fatalf("get version: %d %v", w.Code, w.Header())
	}
	def := ver["definition"].(map[string]any)
	for _, n := range def["nodes"].([]any) {
		if nm := n.(map[string]any); nm["id"] == "budget_approval" {
			nm["config"].(map[string]any)["policy_ref"] = "poc.unknown"
		}
	}
	def["policy_refs"] = []any{"poc.unknown"}

	if w, b := c.do("PATCH", "/api/v1/workflows/poc-http/versions/1", map[string]any{"definition": def}, nil); w.Code != 428 {
		t.Fatalf("PATCH without If-Match must be 428: %d %v", w.Code, b)
	}
	if w, b := c.do("PATCH", "/api/v1/workflows/poc-http/versions/1", map[string]any{"definition": def}, map[string]string{"If-Match": `"99"`}); w.Code != 412 || errCode(b) != "version_mismatch" {
		t.Fatalf("stale If-Match must be 412: %d %v", w.Code, b)
	}
	w, saved := c.do("PATCH", "/api/v1/workflows/poc-http/versions/1", map[string]any{"definition": def, "change_note": "try unknown policy"}, map[string]string{"If-Match": `"1"`})
	if w.Code != 200 || saved["validation"].(map[string]any)["valid"] != false || w.Header().Get("ETag") != `"2"` {
		t.Fatalf("draft save with semantic errors: %d %v", w.Code, saved)
	}

	w, b := c.do("POST", "/api/v1/workflows/poc-http/versions/1/publish", nil, map[string]string{"If-Match": `"2"`})
	if w.Code != 422 || errCode(b) != "invalid_definition" {
		t.Fatalf("publishing an invalid draft must be 422: %d %v", w.Code, b)
	}
	details := b["error"].(map[string]any)["details"].([]any)
	foundPath := false
	for _, d := range details {
		dm := d.(map[string]any)
		if dm["code"] == "unknown_policy_ref" && strings.HasPrefix(dm["path"].(string), "/nodes/") {
			foundPath = true
		}
	}
	if !foundPath {
		t.Fatalf("422 details must point at the offending node: %v", details)
	}

	original := created["version"].(map[string]any)["definition"]
	w, _ = c.do("PATCH", "/api/v1/workflows/poc-http/versions/1", map[string]any{"definition": original}, map[string]string{"If-Match": `"2"`})
	if w.Code != 200 {
		t.Fatalf("fix draft: %d", w.Code)
	}
	if w, b := member.do("POST", "/api/v1/workflows/poc-http/versions/1/publish", nil, map[string]string{"If-Match": `"3"`}); w.Code != 403 {
		t.Fatalf("member publish must be 403: %d %v", w.Code, b)
	}
	w, pub := c.do("POST", "/api/v1/workflows/poc-http/versions/1/publish", nil, map[string]string{"If-Match": `"3"`})
	if w.Code != 200 || pub["version"].(map[string]any)["status"] != "published" || pub["version"].(map[string]any)["definition_hash"] == nil {
		t.Fatalf("publish: %d %v", w.Code, pub)
	}
	if w, b := c.do("PATCH", "/api/v1/workflows/poc-http/versions/1", map[string]any{"definition": original}, map[string]string{"If-Match": `"4"`}); w.Code != 409 || errCode(b) != "immutable_version" {
		t.Fatalf("editing a published version must be 409 immutable_version: %d %v", w.Code, b)
	}

	w, fork := c.do("POST", "/api/v1/workflows/poc-http/versions", map[string]any{}, map[string]string{"Idempotency-Key": "fork-poc-http-2"})
	if w.Code != 201 || fork["version"].(map[string]any)["version"] != float64(2) {
		t.Fatalf("fork v2: %d %v", w.Code, fork)
	}
	if w, b := c.do("POST", "/api/v1/workflows/poc-http/versions", map[string]any{}, map[string]string{"Idempotency-Key": "fork-poc-http-3"}); w.Code != 409 || errCode(b) != "draft_exists" {
		t.Fatalf("second fork must be 409 draft_exists: %d %v", w.Code, b)
	}

	if w, list := member.do("GET", "/api/v1/workflows?status=published", nil, nil); w.Code != 200 || len(list["items"].([]any)) != 1 {
		t.Fatalf("member list published: %d %v", w.Code, list)
	}
	if w, b := c.do("GET", "/api/v1/workflows?status=bogus", nil, nil); w.Code != 400 {
		t.Fatalf("bad filter must be 400: %d %v", w.Code, b)
	}
	if w, detail := c.do("GET", "/api/v1/workflows/poc-http", nil, nil); w.Code != 200 || len(detail["versions"].([]any)) != 2 {
		t.Fatalf("detail: %d %v", w.Code, detail)
	}
	if w, _ := c.do("GET", "/api/v1/workflows/does-not-exist", nil, nil); w.Code != 404 {
		t.Fatalf("missing workflow must be 404, got %d", w.Code)
	}

	w, rep := c.do("POST", "/api/v1/workflow-validations", map[string]any{"definition": map[string]any{"schema_version": "1.0"}}, nil)
	if w.Code != 200 || rep["valid"] != false {
		t.Fatalf("stateless validation: %d %v", w.Code, rep)
	}
	if w, tpl := c.do("GET", "/api/v1/workflow-templates", nil, nil); w.Code != 200 || len(tpl["items"].([]any)) != 2 {
		t.Fatalf("templates: %d %v", w.Code, tpl)
	}
	if w, cat := c.do("GET", "/api/v1/workflow-catalog", nil, nil); w.Code != 200 || len(cat["agents"].([]any)) != 6 {
		t.Fatalf("catalog must register the six SIM agents: %d %v", w.Code, cat)
	}

	w, events := c.do("GET", "/api/v1/audit-events?limit=2", nil, nil)
	if w.Code != 200 || len(events["items"].([]any)) != 2 || events["next_cursor"] == nil {
		t.Fatalf("audit page: %d %v", w.Code, events)
	}
	if w, page2 := c.do("GET", "/api/v1/audit-events?limit=50&cursor="+events["next_cursor"].(string), nil, nil); w.Code != 200 || len(page2["items"].([]any)) == 0 {
		t.Fatalf("audit page 2: %d %v", w.Code, page2)
	}
	if w, _ := member.do("GET", "/api/v1/audit-events", nil, nil); w.Code != 403 {
		t.Fatalf("member audit must be 403, got %d", w.Code)
	}
}
