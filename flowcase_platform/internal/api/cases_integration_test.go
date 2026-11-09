package api_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elenio/flowcase/internal/api"
	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/testutil/pgtest"
	"github.com/elenio/flowcase/internal/workflows/registry"
)

const approverID = "00000000-0000-4000-8000-000000000102"

func TestCaseRuntimeOverHTTP(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	ctx := context.Background()
	ps, _ := packs.Load()
	cat, _ := packs.Catalog(ps)
	tpls, _ := packs.Templates(ps)
	snap := cat.Snapshot()
	roles := []string{"workspace_member", "case_owner", "workflow_designer", "workflow_publisher"}
	seed := postgres.DevSeed{TenantID: tenantID, UserID: designerID, DisplayName: "Ada", Roles: roles, DomainRoles: snap.AssigneeRoles,
		Personas: []postgres.DevSeedPersona{{UserID: approverID, DisplayName: "Dian", Roles: []string{"workspace_member", "domain_approver"}}}}
	for _, p := range snap.Policies {
		seed.Policies = append(seed.Policies, p.Ref)
	}
	if err := postgres.SeedDevelopment(ctx, pool, seed); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Service{DB: pool, Catalog: cat, Templates: tpls}
	owner := auth.NewDevMockAdapter(designerID, tenantID, "Ada", []auth.Role{auth.RoleWorkspaceMember, auth.RoleCaseOwner, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher},
		auth.DevPersona{Key: "approver", UserID: approverID, DisplayName: "Dian", Roles: []auth.Role{auth.RoleWorkspaceMember, auth.RoleDomainApprover}})
	ownerID, _ := owner.Authenticate(ctx, httptestRequest())
	if _, err := reg.SeedPacks(ctx, ownerID, ps); err != nil {
		t.Fatal(err)
	}
	engine := &cases.Engine{DB: pool, Catalog: cat, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c := client{t: t, h: api.NewServer(api.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Environment: "development",
		Auth: owner, Knowledge: kms.NewDisabled(), Agents: agent.NewDisabled(),
		Registry: &api.Registry{Service: reg, Catalog: cat, Templates: tpls}, Cases: engine})}
	asApprover := map[string]string{auth.DevPersonaHeader: "approver"}
	drain := func() {
		for range 20 {
			if n, err := engine.ProcessReady(ctx, "http-test", 50); err != nil || n == 0 {
				return
			}
		}
	}
	with := func(base map[string]string, kv ...string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}

	if w, b := c.do("GET", "/api/v1/session", nil, map[string]string{auth.DevPersonaHeader: "root"}); w.Code != 401 {
		t.Fatalf("unknown persona must be 401: %d %v", w.Code, b)
	}
	if _, b := c.do("GET", "/api/v1/session", nil, asApprover); b["display_name"] != "Dian" || len(b["dev_personas"].([]any)) != 2 {
		t.Fatalf("persona session: %v", b)
	}

	if w, b := c.do("POST", "/api/v1/cases", map[string]any{"workflow_key": "sim-tender", "title": "Tender"}, map[string]string{"Idempotency-Key": "case-sim-1"}); w.Code != 422 ||
		!strings.Contains(fmt.Sprint(b), "agent_runtime_unavailable") {
		t.Fatalf("SIM must be refused without an agent runtime: %d %v", w.Code, b)
	}
	body := map[string]any{"workflow_key": "internal-poc-request", "title": "Evaluate vector DB",
		"variables": map[string]any{"requester_id": "u-1", "purpose": "Pilot", "estimated_cost": 1200}}
	w, created := c.do("POST", "/api/v1/cases", body, map[string]string{"Idempotency-Key": "case-poc-1"})
	if w.Code != 201 || !strings.HasPrefix(w.Header().Get("Location"), "/api/v1/cases/") || created["workflow_version"] != float64(1) {
		t.Fatalf("create case: %d %v", w.Code, created)
	}
	caseID := created["id"].(string)
	drain()

	_, tasks := c.do("GET", "/api/v1/tasks", nil, nil)
	items := tasks["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["can_complete"] != true {
		t.Fatalf("owner should have the scope task: %v", tasks)
	}
	taskID := items[0].(map[string]any)["id"].(string)
	if w, _ := c.do("POST", "/api/v1/tasks/"+taskID+"/complete", map[string]any{}, nil); w.Code != 428 {
		t.Fatalf("complete without If-Match must be 428, got %d", w.Code)
	}
	if w, b := c.do("POST", "/api/v1/tasks/"+taskID+"/complete", map[string]any{}, with(asApprover, "If-Match", `"1"`)); w.Code != 403 || errCode(b) != "not_assignee_role" {
		t.Fatalf("approver cannot do the owner's task: %d %v", w.Code, b)
	}
	if w, b := c.do("POST", "/api/v1/tasks/"+taskID+"/complete", map[string]any{"comment": "Scoped with the team"}, map[string]string{"If-Match": `"1"`}); w.Code != 200 || b["status"] != "submitted" {
		t.Fatalf("complete: %d %v", w.Code, b)
	}
	drain()

	if _, b := c.do("GET", "/api/v1/approvals", nil, nil); len(b["items"].([]any)) != 0 {
		t.Fatalf("approval inbox must be filtered by delegation: %v", b)
	}
	_, inbox := c.do("GET", "/api/v1/approvals", nil, asApprover)
	appr := inbox["items"].([]any)[0].(map[string]any)
	if appr["can_decide"] != true || appr["policy_provisional"] != true {
		t.Fatalf("approver inbox: %v", appr)
	}
	apprID := appr["id"].(string)
	if w, b := c.do("POST", "/api/v1/approvals/"+apprID+"/decision", map[string]any{"decision": "approve", "rationale": "self approval"}, map[string]string{"If-Match": `"1"`}); w.Code != 403 || errCode(b) != "missing_role" {
		t.Fatalf("owner decision must be 403 missing_role: %d %v", w.Code, b)
	}
	if w, b := c.do("POST", "/api/v1/approvals/"+apprID+"/decision", map[string]any{"decision": "approve", "rationale": "Within budget"}, with(asApprover, "If-Match", `"1"`)); w.Code != 200 || b["status"] != "approved" {
		t.Fatalf("approve: %d %v", w.Code, b)
	}
	drain()

	w, detail := c.do("GET", "/api/v1/cases/"+caseID, nil, nil)
	if w.Code != 200 || detail["status"] != "active" || len(detail["condition_evaluations"].([]any)) != 1 {
		t.Fatalf("detail after approval: %d %v", w.Code, detail["status"])
	}
	etag := w.Header().Get("ETag")
	if w, b := c.do("POST", "/api/v1/cases/"+caseID+"/pause", map[string]any{"reason": "budget freeze"}, with(asApprover, "If-Match", etag)); w.Code != 403 {
		t.Fatalf("approver cannot pause: %d %v", w.Code, b)
	}
	if w, b := c.do("POST", "/api/v1/cases/"+caseID+"/pause", map[string]any{"reason": "budget freeze"}, map[string]string{"If-Match": etag}); w.Code != 200 || b["status"] != "paused" {
		t.Fatalf("pause: %d %v", w.Code, b)
	}
	if w, b := c.do("POST", "/api/v1/cases/"+caseID+"/events", map[string]any{"type": "note", "body": "Waiting on finance"}, nil); w.Code != 201 ||
		!strings.Contains(fmt.Sprint(b["history"]), "case.note") {
		t.Fatalf("note: %d", w.Code)
	}
	if w, b := c.do("POST", "/api/v1/cases/"+caseID+"/events", map[string]any{"type": "set_variables", "body": "x"}, nil); w.Code != 400 {
		t.Fatalf("unsupported events must be 400: %d %v", w.Code, b)
	}
	if w, b := c.do("GET", "/api/v1/cases?status=paused", nil, nil); w.Code != 200 || len(b["items"].([]any)) != 1 {
		t.Fatalf("filter paused: %d %v", w.Code, b)
	}
	if w, _ := c.do("GET", "/api/v1/cases/not-a-uuid", nil, nil); w.Code != 404 {
		t.Fatalf("malformed ids are 404, got %d", w.Code)
	}
}

func httptestRequest() *http.Request { return httptest.NewRequest("GET", "/", nil) }

func TestAgentCallbackEndpoint(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	key := "http-callback-secret-0123456789abcdef"
	engine := &cases.Engine{DB: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Agents: agent.NewDevelopmentStub(key)}
	h := api.NewServer(api.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Environment: "test",
		Auth: auth.NewDevMockAdapter(designerID, tenantID, "Ada", nil), Knowledge: kms.NewDisabled(), Agents: agent.NewDisabled(), Cases: engine})
	send := func(provider string, body []byte, headers map[string][]string) (int, string) {
		r := httptest.NewRequest("POST", "/api/v1/agent-callbacks/"+provider, strings.NewReader(string(body)))
		for k, v := range headers {
			r.Header[k] = v
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	body := []byte(`{"message_id":"m-1","audience":"flowcase","tenant_id":"t","run":{"run_id":"stub:x:y","status":"running","agent_id":"x"}}`)
	if code, out := send("stub", body, agent.SignCallback([]byte(key), time.Now(), body)); code != 202 || !strings.Contains(out, "unknown_run") {
		t.Fatalf("signed callback for an unknown run must be acknowledged without effect: %d %s", code, out)
	}
	if code, _ := send("stub", body, agent.SignCallback([]byte("another-secret-0123456789abcdefghij"), time.Now(), body)); code != 401 {
		t.Fatalf("bad signature must be 401, got %d", code)
	}
	if code, _ := send("stub", body, nil); code != 401 {
		t.Fatalf("unsigned callback must be 401, got %d", code)
	}
	if code, _ := send("other", body, agent.SignCallback([]byte(key), time.Now(), body)); code != 404 {
		t.Fatalf("unknown provider must be 404, got %d", code)
	}
}
