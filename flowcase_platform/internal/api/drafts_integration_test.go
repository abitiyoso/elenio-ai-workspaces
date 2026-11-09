package api_test

import (
	"context"
	"io"
	"log/slog"
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

// TestDraftReviewOverHTTP drives the author draft review of SIM v3 through the API with the development
// agent stub: inbox, ETag/If-Match, revision with feedback, submit, and the owner-only rule.
func TestDraftReviewOverHTTP(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	ctx := context.Background()
	ps, _ := packs.Load()
	cat, _ := packs.Catalog(ps)
	tpls, _ := packs.Templates(ps)
	seed := postgres.DevSeed{TenantID: tenantID, UserID: designerID, DisplayName: "Ada", Roles: []string{"workspace_member", "case_owner", "workflow_designer", "workflow_publisher"},
		Personas: []postgres.DevSeedPersona{{UserID: approverID, DisplayName: "Dian", Roles: []string{"workspace_member", "domain_approver"}}}}
	for _, p := range cat.Snapshot().Policies {
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
	stub := agent.NewDevelopmentStub("test-callback-secret-0123456789abcdef")
	engine := &cases.Engine{DB: pool, Catalog: cat, Agents: stub, AgentPoll: time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c := client{t: t, h: api.NewServer(api.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Environment: "development",
		Auth: owner, Knowledge: kms.NewDisabled(), Agents: stub, Registry: &api.Registry{Service: reg, Catalog: cat, Templates: tpls}, Cases: engine})}
	asApprover := map[string]string{auth.DevPersonaHeader: "approver"}
	drain := func() {
		for range 50 {
			if n, err := engine.ProcessReady(ctx, "http-test", 50); err != nil || n == 0 {
				return
			}
			time.Sleep(2 * time.Millisecond) // let agent polls come due
		}
	}

	w, created := c.do("POST", "/api/v1/cases", map[string]any{"workflow_key": "sim-tender", "title": "Tender",
		"variables": map[string]any{"opportunity_id": "o-1", "customer_id": "c-1", "deadline": "2026-12-01T00:00:00Z"}}, map[string]string{"Idempotency-Key": "draft-case-1"})
	if w.Code != 201 || created["workflow_version"] != float64(4) {
		t.Fatalf("create SIM v4 case: %d %v", w.Code, created)
	}
	caseID := created["id"].(string)
	drain()
	_, inbox := c.do("GET", "/api/v1/approvals", nil, asApprover)
	g0 := inbox["items"].([]any)[0].(map[string]any)
	if w, b := c.do("POST", "/api/v1/approvals/"+g0["id"].(string)+"/decision", map[string]any{"decision": "approve", "rationale": "Bid qualified"},
		map[string]string{auth.DevPersonaHeader: "approver", "If-Match": `"1"`}); w.Code != 200 {
		t.Fatalf("approve G0: %d %v", w.Code, b)
	}
	drain()

	w, list := c.do("GET", "/api/v1/drafts", nil, nil)
	items, _ := list["items"].([]any)
	if w.Code != 200 || len(items) != 1 {
		t.Fatalf("owner draft inbox: %d %v", w.Code, list)
	}
	draftID := items[0].(map[string]any)["id"].(string)
	if _, b := c.do("GET", "/api/v1/drafts", nil, asApprover); len(b["items"].([]any)) != 0 {
		t.Fatalf("the draft inbox is the owner's: %v", b)
	}
	if w, _ := c.do("GET", "/api/v1/drafts?status=nope", nil, nil); w.Code != 400 {
		t.Fatalf("unknown status filter must be 400, got %d", w.Code)
	}
	w, draft := c.do("GET", "/api/v1/drafts/"+draftID, nil, nil)
	if w.Code != 200 || w.Header().Get("ETag") != `"1"` || draft["node_id"] != "intake" || draft["content"] == nil || draft["can_submit"] != true {
		t.Fatalf("get draft: %d %s %v", w.Code, w.Header().Get("ETag"), draft)
	}
	if w, _ := c.do("POST", "/api/v1/drafts/"+draftID+"/revision", map[string]any{"feedback": "Add the SLA section"}, nil); w.Code != 428 {
		t.Fatalf("revision without If-Match must be 428, got %d", w.Code)
	}
	if w, b := c.do("POST", "/api/v1/drafts/"+draftID+"/revision", map[string]any{"feedback": "no"}, map[string]string{"If-Match": `"1"`}); w.Code != 400 || errCode(b) != "feedback_required" {
		t.Fatalf("short feedback must be 400 feedback_required: %d %v", w.Code, b)
	}
	if w, b := c.do("POST", "/api/v1/drafts/"+draftID+"/submit", map[string]any{}, map[string]string{auth.DevPersonaHeader: "approver", "If-Match": `"1"`}); w.Code != 403 || errCode(b) != "not_case_owner" {
		t.Fatalf("non-owner submit must be 403 not_case_owner: %d %v", w.Code, b)
	}
	if w, b := c.do("POST", "/api/v1/drafts/"+draftID+"/revision", map[string]any{"feedback": "Add the SLA section"}, map[string]string{"If-Match": `"1"`}); w.Code != 200 || b["status"] != "revision_requested" {
		t.Fatalf("request revision: %d %v", w.Code, b)
	}
	drain()

	_, detail := c.do("GET", "/api/v1/cases/"+caseID, nil, nil)
	drafts := detail["drafts"].([]any)
	latest := drafts[0].(map[string]any)
	if len(drafts) != 2 || latest["status"] != "pending" || latest["artifact_version"] != float64(2) || latest["revisions"] != float64(1) {
		t.Fatalf("revision opens a new draft on v2: %v", drafts)
	}
	if w, b := c.do("POST", "/api/v1/drafts/"+latest["id"].(string)+"/submit", map[string]any{"note": "Good now"}, map[string]string{"If-Match": `"1"`}); w.Code != 200 || b["status"] != "submitted" {
		t.Fatalf("submit: %d %v", w.Code, b)
	}
	drain()
	_, inbox = c.do("GET", "/api/v1/approvals", nil, asApprover)
	if g1 := inbox["items"].([]any)[0].(map[string]any); g1["node_id"] != "g1" || g1["can_decide"] != true {
		t.Fatalf("submit opens G1 for the approver: %v", g1)
	}
}
