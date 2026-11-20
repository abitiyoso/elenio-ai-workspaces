package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/elenio/control-plane/internal/solutions"
	"github.com/elenio/control-plane/migrations"
)

func TestSolutionRevisionHistoryIsScopedImmutableAndPaginated(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	definition := solutionPlan("revision-history")
	first := expectStatus(t, b.do("POST", "/api/v1/solutions", definition, solutionHeaders("revision-history-01")), 201)
	path := "/api/v1/solutions/" + first["id"].(string)
	original := expectStatus(t, b.do("GET", path+"/revisions/1", nil, nil), 200)
	mustEqual(t, original["origin"].(string), "write", "authored revision origin")
	mustEqual(t, original["recorded_by"].(string), e.userID(subRafi), "verified actor")
	raw, _ := json.Marshal(original["plan"])
	hash, err := solutions.CanonicalSnapshotHash(raw)
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, original["content_hash"].(string), hash, "wire snapshot hash")
	for revision := 1; revision < 24; revision++ {
		definition["name"] = fmt.Sprintf("Revision %d", revision+1)
		expectStatus(t, b.do("PATCH", path, definition, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, revision)}), 200)
	}
	expectStatus(t, b.do("PATCH", path, definition, map[string]string{"If-Match": `"1"`}), 412)
	expectStatus(t, b.do("POST", "/api/v1/solutions", solutionPlan("revision-history"), solutionHeaders("revision-history-01")), 200)
	response := b.do("GET", path+"/revisions/1", nil, nil)
	mustEqual(t, response.Header.Get("ETag"), `"`+hash+`"`, "immutable checksum ETag")
	mustEqual(t, response.Header.Get("Cache-Control"), "no-store", "scoped data not cached publicly")
	after := expectStatus(t, response, 200)
	mustEqual(t, after["plan"].(map[string]any)["definition"].(map[string]any)["name"].(string), "Purchase review", "old name survives edits")
	mustEqual(t, after["content_hash"].(string), hash, "old hash survives edits")
	page := expectStatus(t, b.do("GET", path+"/revisions", nil, nil), 200)
	mustEqual(t, len(page["items"].([]any)), 20, "bounded revision page")
	mustEqual(t, page["items"].([]any)[0].(map[string]any)["plan"].(map[string]any)["revision"].(float64), 24, "newest first")
	mustEqual(t, page["next_before"].(float64), 5, "strict keyset cursor")
	older := expectStatus(t, b.do("GET", path+"/revisions?before=5", nil, nil), 200)
	mustEqual(t, len(older["items"].([]any)), 4, "remaining revisions with no duplicate writes")
	for _, suffix := range []string{"/revisions/0", "/revisions/01", "/revisions/nope", "/revisions?before=0", "/revisions?before=-1", "/revisions?before=01"} {
		expectStatus(t, b.do("GET", path+suffix, nil, nil), 400)
	}
	expectStatus(t, b.do("GET", path+"/revisions/25", nil, nil), 404)
	expectStatus(t, b.do("DELETE", path+"/revisions/1", nil, nil), 404)
	for _, query := range []string{`UPDATE solution_project_revisions SET snapshot='{}'::jsonb WHERE solution_id=$1::uuid`, `DELETE FROM solution_project_revisions WHERE solution_id=$1::uuid`} {
		if _, err := e.pool.Exec(context.Background(), query, first["id"]); err == nil {
			t.Fatal("revision mutation was allowed")
		}
	}
	expectStatus(t, e.as(subDimas).do("GET", path+"/revisions", nil, nil), 403)
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO role_bindings(tenant_id,user_id,workspace_id,role_id,granted_by,reason) SELECT $1::uuid,$2::uuid,$3::uuid,id,$2::uuid,'history scope test' FROM roles WHERE key='solution_builder'`, e.tenantID, e.userID(subRafi), e.workspaceID("engineering")); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("engineering")}, nil), 204)
	expectStatus(t, b.do("GET", path+"/revisions", nil, nil), 404)
	expectStatus(t, b.do("GET", path+"/revisions/1", nil, nil), 404)
}

func TestSolutionRevisionUpgradeRetainsOnlyObservedLegacyCurrent(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	definition := solutionPlan("legacy-revision")
	first := expectStatus(t, b.do("POST", "/api/v1/solutions", definition, solutionHeaders("legacy-revision-01")), 201)
	path := "/api/v1/solutions/" + first["id"].(string)
	// Simulate a pre-history database inside this test's disposable schema, then execute the actual additive migration.
	if _, err := e.pool.Exec(context.Background(), `DROP TABLE solution_project_revisions; DROP FUNCTION solution_project_revisions_immutable(); UPDATE solution_projects SET revision=7`); err != nil {
		t.Fatal(err)
	}
	sql, err := migrations.FS.ReadFile("0007_solution_revisions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.pool.Exec(context.Background(), string(sql)); err != nil {
		t.Fatal(err)
	}
	page := expectStatus(t, b.do("GET", path+"/revisions", nil, nil), 200)
	mustEqual(t, len(page["items"].([]any)), 1, "only observed current revision imported")
	imported := page["items"].([]any)[0].(map[string]any)
	mustEqual(t, imported["origin"].(string), "migration_current", "backfill provenance is explicit")
	mustEqual(t, imported["plan"].(map[string]any)["revision"].(float64), 7, "observed legacy revision")
	if imported["recorded_by"] != nil {
		t.Fatal("migration invented original editor identity")
	}
	expectStatus(t, b.do("GET", path+"/revisions/1", nil, nil), 404)
	definition["name"] = "New retained edit"
	expectStatus(t, b.do("PATCH", path, definition, map[string]string{"If-Match": `"7"`}), 200)
	next := expectStatus(t, b.do("GET", path+"/revisions/8", nil, nil), 200)
	mustEqual(t, next["origin"].(string), "write", "new revision is attributed normally")
	old := expectStatus(t, b.do("GET", path+"/revisions/7", nil, nil), 200)
	mustEqual(t, old["plan"].(map[string]any)["definition"].(map[string]any)["name"].(string), "Purchase review", "imported current remains immutable")
}
