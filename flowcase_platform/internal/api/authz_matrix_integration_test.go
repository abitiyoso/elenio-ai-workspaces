package api_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

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

const (
	viewerID      = "00000000-0000-4000-8000-000000000103"
	otherTenantID = "00000000-0000-4000-8000-0000000000ff"
	otherUserID   = "00000000-0000-4000-8000-0000000001ff"
)

// TestPermissionMatrix drives every mutating endpoint as callers who must be refused: a plain member,
// an approver acting outside their authority, and a fully privileged user of another tenant.
// None may get a 2xx (or a 5xx), and the case must be unchanged afterwards.
func TestPermissionMatrix(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	ctx := context.Background()
	ps, _ := packs.Load()
	cat, _ := packs.Catalog(ps)
	tpls, _ := packs.Templates(ps)
	snap := cat.Snapshot()
	all := []string{"workspace_member", "case_owner", "workflow_designer", "workflow_publisher", "tenant_admin"}
	seed := postgres.DevSeed{TenantID: tenantID, UserID: designerID, DisplayName: "Ada", Roles: all, DomainRoles: snap.AssigneeRoles,
		Personas: []postgres.DevSeedPersona{
			{UserID: approverID, DisplayName: "Dian", Roles: []string{"workspace_member", "domain_approver"}},
			{UserID: viewerID, DisplayName: "Vera", Roles: []string{"workspace_member"}},
		}}
	for _, p := range snap.Policies {
		seed.Policies = append(seed.Policies, p.Ref)
	}
	if err := postgres.SeedDevelopment(ctx, pool, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tenants SET external_ref = 'acme' WHERE id = $1`, tenantID); err != nil {
		t.Fatal(err)
	}
	if err := postgres.SeedDevelopment(ctx, pool, postgres.DevSeed{TenantID: otherTenantID, UserID: otherUserID, DisplayName: "Olaf", Roles: append(all, "domain_approver"),
		DomainRoles: snap.AssigneeRoles, Policies: seed.Policies}); err != nil {
		t.Fatal(err)
	}
	roles := func(names ...string) []auth.Role {
		var out []auth.Role
		for _, n := range names {
			r, _ := auth.ParseRole(n)
			out = append(out, r)
		}
		return out
	}
	reg := &registry.Service{DB: pool, Catalog: cat, Templates: tpls}
	engine := &cases.Engine{DB: pool, Catalog: cat, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	build := func(a auth.AuthAdapter) client {
		return client{t: t, h: api.NewServer(api.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Environment: "development",
			Auth: a, Knowledge: kms.NewDisabled(), Agents: agent.NewDisabled(), Registry: &api.Registry{Service: reg, Catalog: cat, Templates: tpls}, Cases: engine})}
	}
	home := build(auth.NewDevMockAdapter(designerID, tenantID, "Ada", roles(all...),
		auth.DevPersona{Key: "approver", UserID: approverID, DisplayName: "Dian", Roles: roles("workspace_member", "domain_approver")},
		auth.DevPersona{Key: "viewer", UserID: viewerID, DisplayName: "Vera", Roles: roles("workspace_member")}))
	foreign := build(auth.NewDevMockAdapter(otherUserID, otherTenantID, "Olaf", roles(append(all, "domain_approver")...)))
	owner, _ := auth.NewDevMockAdapter(designerID, tenantID, "Ada", roles(all...)).Authenticate(ctx, httptestRequest())
	if _, err := reg.SeedPacks(ctx, owner, ps); err != nil {
		t.Fatal(err)
	}

	// Arrange: a PoC case with a submitted scope task and a pending budget approval, plus an open task in a second case.
	_, created := home.do("POST", "/api/v1/cases", map[string]any{"workflow_key": "internal-poc-request", "title": "Matrix case",
		"variables": map[string]any{"requester_id": "r", "purpose": "p", "estimated_cost": 10}}, map[string]string{"Idempotency-Key": "matrix-case-1"})
	caseID := created["id"].(string)
	drain := func() {
		for range 10 {
			if n, _ := engine.ProcessReady(ctx, "matrix", 50); n == 0 {
				return
			}
		}
	}
	drain()
	_, tasks := home.do("GET", "/api/v1/tasks", nil, nil)
	scopeID := tasks["items"].([]any)[0].(map[string]any)["id"].(string)
	if w, _ := home.do("POST", "/api/v1/tasks/"+scopeID+"/complete", map[string]any{}, map[string]string{"If-Match": `"1"`}); w.Code != 200 {
		t.Fatalf("arrange: complete scope: %d", w.Code)
	}
	drain()
	_, appr := home.do("GET", "/api/v1/approvals?status=all", nil, map[string]string{auth.DevPersonaHeader: "approver"})
	approvalID := appr["items"].([]any)[0].(map[string]any)["id"].(string)
	_, created2 := home.do("POST", "/api/v1/cases", map[string]any{"workflow_key": "internal-poc-request", "title": "Second",
		"variables": map[string]any{"requester_id": "r", "purpose": "p"}}, map[string]string{"Idempotency-Key": "matrix-case-2"})
	drain()
	_, tasks = home.do("GET", "/api/v1/tasks", nil, nil)
	var openTaskID string
	for _, it := range tasks["items"].([]any) {
		if m := it.(map[string]any); m["case"].(map[string]any)["id"] == created2["id"] {
			openTaskID = m["id"].(string)
		}
	}
	_, detail := home.do("GET", "/api/v1/cases/"+caseID, nil, nil)
	caseETag := `"` + fmtInt(detail["version_num"]) + `"`
	missing := "99999999-9999-4999-8999-999999999999"

	type call struct {
		name, method, path string
		body               any
		headers            map[string]string
	}
	mutations := []call{
		{"create workflow", "POST", "/api/v1/workflows", map[string]any{"key": "x-flow", "name": "X flow"}, map[string]string{"Idempotency-Key": "deny-wf-1"}},
		{"edit draft", "PATCH", "/api/v1/workflows/sim-tender/versions/2", map[string]any{"definition": map[string]any{}}, map[string]string{"If-Match": `"1"`}},
		{"validate", "POST", "/api/v1/workflows/sim-tender/versions/2/validate", nil, nil},
		{"publish", "POST", "/api/v1/workflows/sim-tender/versions/2/publish", nil, map[string]string{"If-Match": `"1"`}},
		{"deprecate", "POST", "/api/v1/workflows/sim-tender/versions/1/deprecate", nil, map[string]string{"If-Match": `"1"`}},
		{"duplicate", "POST", "/api/v1/workflows/sim-tender/duplicate", map[string]any{"key": "copy-x"}, map[string]string{"Idempotency-Key": "deny-dup-1"}},
		{"fork version", "POST", "/api/v1/workflows/internal-poc-request/versions", map[string]any{}, map[string]string{"Idempotency-Key": "deny-fork-1"}},
		{"start case", "POST", "/api/v1/cases", map[string]any{"workflow_key": "internal-poc-request", "title": "t"}, map[string]string{"Idempotency-Key": "deny-case-1"}},
		{"pause case", "POST", "/api/v1/cases/" + caseID + "/pause", map[string]any{}, map[string]string{"If-Match": caseETag}},
		{"note on case", "POST", "/api/v1/cases/" + caseID + "/events", map[string]any{"type": "note", "body": "x"}, nil},
		{"revise artifact", "POST", "/api/v1/cases/" + caseID + "/artifacts/" + missing + "/versions", map[string]any{"content": map[string]any{}, "change_note": "nope nope"}, map[string]string{"If-Match": `"1"`}},
		{"complete task", "POST", "/api/v1/tasks/" + openTaskID + "/complete", map[string]any{}, map[string]string{"If-Match": `"1"`}},
		{"request revision", "POST", "/api/v1/tasks/" + scopeID + "/revision", map[string]any{"rationale": "please redo"}, map[string]string{"If-Match": `"2"`}},
		{"comment on task", "POST", "/api/v1/tasks/" + openTaskID + "/comments", map[string]any{"body": "x"}, nil},
		{"decide approval", "POST", "/api/v1/approvals/" + approvalID + "/decision", map[string]any{"decision": "approve", "rationale": "looks fine"}, map[string]string{"If-Match": `"1"`}},
		{"read audit", "GET", "/api/v1/audit-events", nil, nil},
		{"read case", "GET", "/api/v1/cases/" + caseID, nil, nil},
	}
	callers := map[string]struct {
		c       client
		headers map[string]string
		skip    map[string]bool // calls this caller is legitimately allowed to make
	}{
		"member":           {home, map[string]string{auth.DevPersonaHeader: "viewer"}, nil},
		"approver":         {home, map[string]string{auth.DevPersonaHeader: "approver"}, map[string]bool{"decide approval": true, "read case": true, "note on case": true, "comment on task": true}},
		"other tenant all": {foreign, nil, map[string]bool{"create workflow": true, "start case": true, "read audit": true}},
	}
	for who, caller := range callers {
		for _, m := range mutations {
			if caller.skip[m.name] {
				continue
			}
			t.Run(who+"/"+m.name, func(t *testing.T) {
				h := map[string]string{}
				for k, v := range m.headers {
					h[k] = v
				}
				for k, v := range caller.headers {
					h[k] = v
				}
				w, b := caller.c.do(m.method, m.path, m.body, h)
				if w.Code < 400 || w.Code >= 500 || (w.Code != 403 && w.Code != 404 && w.Code != 400) {
					t.Fatalf("expected a refusal, got %d %v", w.Code, b)
				}
			})
		}
	}
	// Other tenant: lists are empty, not errors.
	for _, path := range []string{"/api/v1/cases", "/api/v1/tasks", "/api/v1/approvals?status=all", "/api/v1/workflows"} {
		_, b := foreign.do("GET", path, nil, nil)
		if items, _ := b["items"].([]any); len(items) != 0 {
			t.Fatalf("%s leaked %d items across tenants", path, len(items))
		}
	}
	// Nothing changed.
	_, after := home.do("GET", "/api/v1/cases/"+caseID, nil, nil)
	if after["status"] != "under_review" || fmtInt(after["version_num"]) != fmtInt(detail["version_num"]) {
		t.Fatalf("refused calls must not change the case: %v v%v", after["status"], after["version_num"])
	}
}

func fmtInt(v any) string {
	f, _ := v.(float64)
	return itoa(int(f))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
