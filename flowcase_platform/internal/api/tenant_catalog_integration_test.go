package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elenio/flowcase/internal/api"
	"github.com/elenio/flowcase/internal/app"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/testutil/pgtest"
	"github.com/elenio/flowcase/internal/workflows/catalog"
)

func TestTenantBusinessCatalogHTTPUsesSourceIdentityAndPublicationAuthority(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	ctx := context.Background()
	const otherTenant = "00000000-0000-4000-8000-000000000002"
	const otherUser = "00000000-0000-4000-8000-000000000202"
	if err := postgres.SeedDevelopment(ctx, pool, postgres.DevSeed{TenantID: tenantID, UserID: designerID, DisplayName: "Catalog HTTP fixture", Roles: []string{"workspace_member", "workflow_designer", "workflow_publisher"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id,external_ref,name) VALUES ($1,'other-catalog-fixture','Other catalog fixture')`, otherTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,issuer,subject,display_name) VALUES ($1,'flowcase-dev-mock',$1::uuid::text,'Other catalog fixture')`, otherUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO memberships (tenant_id,user_id) VALUES ($1,$2)`, otherTenant, otherUser); err != nil {
		t.Fatal(err)
	}
	config := packs.Bindings{Tenants: map[string]catalog.Snapshot{tenantID: {
		Agents:        []catalog.Agent{{Ref: "finance.extract", Name: "Invoice extraction fixture", RuntimeAgentID: "finance.extract", SkillVersion: "1.2.0", InputContract: "flowcase.case.v1"}},
		Policies:      []catalog.Policy{{Ref: "finance.approve", Name: "Independent Finance fixture"}},
		AssigneeRoles: []string{"finance-reviewer"}, ArtifactTypes: []string{"invoice.draft"},
	}, otherTenant: {
		Agents:        []catalog.Agent{{Ref: "access.analyze", Name: "Access analysis fixture", RuntimeAgentID: "access.analyze", SkillVersion: "2.0.0", InputContract: "flowcase.case.v1"}},
		ArtifactTypes: []string{"access.draft"},
	}}}
	path := filepath.Join(t.TempDir(), "packs.json")
	raw, _ := json.Marshal(config)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	reg, _, err := app.NewRegistry(pool, path)
	if err != nil {
		t.Fatal(err)
	}
	build := func(tenant, user string, roles ...auth.Role) client {
		return client{t: t, h: api.NewServer(api.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Environment: "test", Registry: reg,
			Auth: auth.NewDevMockAdapter(user, tenant, "Explicit identity fixture", roles)})}
	}
	a := build(tenantID, designerID, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)
	b := build(otherTenant, otherUser, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)
	for _, tc := range []struct {
		c                client
		expected, hidden string
	}{{a, "finance.extract", "access.analyze"}, {b, "access.analyze", "finance.extract"}} {
		w, _ := tc.c.do("GET", "/api/v1/workflow-catalog?tenant_id="+tenantID, nil, map[string]string{"X-Tenant-ID": tenantID, "X-Roles": "tenant_admin"})
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), tc.expected) || strings.Contains(w.Body.String(), tc.hidden) {
			t.Fatalf("catalog crossed tenant identity: %d %s", w.Code, w.Body.String())
		}
	}
	if w, _ := build(tenantID, designerID).do("GET", "/api/v1/workflow-catalog", nil, nil); w.Code != http.StatusForbidden {
		t.Fatal("roleless principal read catalog")
	}
	var grants int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM domain_role_assignments WHERE tenant_id=$1)+(SELECT count(*) FROM policy_delegations WHERE tenant_id=$1)`, tenantID).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("catalog invented domain grants: %d %v", grants, err)
	}
	doc := map[string]any{"schema_version": "1.0", "key": "finance-http", "name": "Finance transport fixture", "version": 1, "policy_refs": []any{"finance.approve"}, "nodes": []any{
		map[string]any{"id": "start", "type": "start", "name": "Start", "next": []any{"extract"}},
		map[string]any{"id": "extract", "type": "agent_task", "name": "Extract", "config": map[string]any{"agent_ref": "finance.extract", "output_artifact_type": "invoice.draft", "author_review": true}, "next": []any{"review"}},
		map[string]any{"id": "review", "type": "human_approval", "name": "Finance review", "config": map[string]any{"policy_ref": "finance.approve"}, "next": []any{"end"}},
		map[string]any{"id": "end", "type": "end", "name": "End", "next": []any{}},
	}}
	for _, tc := range []struct {
		c     client
		valid bool
	}{{a, true}, {b, false}} {
		w, created := tc.c.do("POST", "/api/v1/workflows", map[string]any{"key": "finance-http", "definition": doc}, map[string]string{"Idempotency-Key": "tenant-business-http-1"})
		if w.Code != http.StatusCreated {
			t.Fatalf("draft: %d %s", w.Code, w.Body.String())
		}
		if created["validation"].(map[string]any)["valid"] != tc.valid {
			t.Fatalf("wrong tenant validation report: %v", created["validation"])
		}
		// Idempotent replay validates against the same source tenant's catalog.
		_, replay := tc.c.do("POST", "/api/v1/workflows", map[string]any{"key": "finance-http", "definition": doc}, map[string]string{"Idempotency-Key": "tenant-business-http-1"})
		if replay["validation"].(map[string]any)["valid"] != tc.valid {
			t.Fatal("replay used unscoped catalog")
		}
		w, _ = tc.c.do("POST", "/api/v1/workflows/finance-http/versions/1/publish", nil, map[string]string{"If-Match": `"1"`})
		if tc.valid && w.Code != http.StatusOK || !tc.valid && w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("publish crossed tenant catalog: %d %s", w.Code, w.Body.String())
		}
	}
	w, observed := a.do("GET", "/api/v1/workflows/finance-http/versions/1/dependencies", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("source evidence: %d %s", w.Code, w.Body.String())
	}
	bindings := observed["bindings"].([]any)
	for _, item := range bindings {
		binding := item.(map[string]any)
		if binding["registered"] != true {
			t.Fatalf("tenant binding disappeared: %v", binding)
		}
		if binding["kind"] == "agent" && (binding["runtime_agent_id"] != "finance.extract" || binding["skill_version"] != "1.2.0" || binding["input_contract"] != "flowcase.case.v1") {
			t.Fatal("source binding differs from configured runtime")
		}
		if binding["kind"] == "approval_policy" && binding["provisional"] != true {
			t.Fatal("registration invented ratification")
		}
	}
	member := build(tenantID, designerID, auth.RoleWorkspaceMember)
	if w, _ := member.do("POST", "/api/v1/workflows/finance-http/versions/1/publish", nil, map[string]string{"If-Match": `"1"`}); w.Code != http.StatusForbidden {
		t.Fatal("catalog registration granted workflow publication")
	}
}
