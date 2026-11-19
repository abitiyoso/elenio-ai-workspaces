package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/elenio/control-plane/internal/solutions"
)

func candidateHeaders(key string) map[string]string {
	return map[string]string{"If-Match": `"1"`, "Idempotency-Key": key}
}
func TestConcurrentCandidateCaptureCreatesOneImmutableSnapshot(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", solutionPlan("concurrent-candidate"), solutionHeaders("concurrent-plan-1")), 201)
	path := "/api/v1/solutions/" + plan["id"].(string) + "/candidates"
	results := make(chan *http.Response, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- b.do("POST", path, map[string]any{"version": "1.0.0"}, candidateHeaders("concurrent-candidate-1"))
		}()
	}
	wg.Wait()
	close(results)
	created, replayed := 0, 0
	id := ""
	hash := ""
	for response := range results {
		status := response.StatusCode
		if status == 201 {
			created++
		} else if status == 200 {
			replayed++
		} else {
			t.Fatalf("unexpected status %d", status)
		}
		item := expectStatus(t, response, status)
		if id != "" {
			mustEqual(t, item["id"].(string), id, "same candidate")
			mustEqual(t, item["content_hash"].(string), hash, "same winning snapshot")
		}
		id = item["id"].(string)
		hash = item["content_hash"].(string)
	}
	mustEqual(t, created, 1, "one creation")
	mustEqual(t, replayed, 1, "one replay")
	mustEqual(t, e.auditCount("solution.candidate.created"), 1, "one candidate audit")
}
func TestReadinessReferenceInventoryIsBoundedWithoutExposingConfigPayloads(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		key := parts[4]
		nodes := []map[string]any{{"id": "start", "type": "start"}}
		for i := 0; i < 205; i++ {
			nodes = append(nodes, map[string]any{"id": fmt.Sprintf("review-%d", i), "type": "human_task", "config": map[string]any{"assignee_role": "finance-reviewer", "params": map[string]any{"secret": "not-for-export"}}})
		}
		nodes = append(nodes, map[string]any{"id": "end", "type": "end"})
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_key": key, "version": 1, "status": "published", "definition_hash": strings.Repeat("a", 64), "definition": map[string]any{"schema_version": "1.0", "key": key, "version": 1, "nodes": nodes}})
	}))
	defer upstream.Close()
	e := newEnv(t, envOptions{upstreams: map[string]config.Upstream{"flowcase": {URL: upstream.URL, Audience: "flowcase", Mode: "proxy", StripPrefix: true}}})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	body := solutionPlan("bounded-inventory")
	body["workflows"] = []map[string]any{{"key": "purchase-review", "version": 1}, {"key": "exception-review", "version": 1}}
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("bounded-plan-1")), 201)
	report := expectStatus(t, b.do("GET", "/api/v1/solutions/"+plan["id"].(string)+"/readiness", nil, nil), 200)
	total := 0
	for _, value := range report["workflows"].([]any) {
		row := value.(map[string]any)
		total += len(row["resources"].([]any))
		mustEqual(t, row["inventory_complete"].(bool), false, "bounded inventory remains incomplete")
	}
	mustEqual(t, total, 200, "aggregate reference limit")
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "not-for-export") {
		t.Fatal("config payload exported")
	}
	if !strings.Contains(string(raw), "200-resource") {
		t.Fatal("truncation blocker missing")
	}
}
func TestSolutionCandidatesAreImmutableVersionedAndReplayAfterPlanEdit(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	plan := solutionPlan("publisher-app")
	plan["application"] = map[string]any{"title": "Finance review", "pages": []map[string]any{{"key": "overview", "title": "Overview", "kind": "overview"}, {"key": "requests", "title": "Requests", "kind": "intake", "workflow_key": "purchase-review"}}}
	created := expectStatus(t, b.do("POST", "/api/v1/solutions", plan, solutionHeaders("publisher-plan-1")), 201)
	id := created["id"].(string)
	path := "/api/v1/solutions/" + id + "/candidates"
	expectStatus(t, b.do("POST", path, map[string]any{"version": "1.0.0"}, nil), 428)
	expectStatus(t, b.do("POST", path, map[string]any{"version": "latest"}, candidateHeaders("publisher-create-1")), 400)
	expectStatus(t, b.do("POST", path, map[string]any{"version": "1.0.0", "readiness": map[string]any{"release_ready": true}}, candidateHeaders("publisher-create-1")), 400)
	first := expectStatus(t, b.do("POST", path, map[string]any{"version": "1.0.0"}, candidateHeaders("publisher-create-1")), 201)
	candidateID := first["id"].(string)
	mustEqual(t, first["status"].(string), "candidate", "not an approved release")
	snapshot := first["snapshot"].(map[string]any)
	mustEqual(t, snapshot["readiness"].(map[string]any)["release_ready"].(bool), false, "blocked candidate is not executable")
	raw, _ := json.Marshal(snapshot)
	hash, err := solutions.CanonicalSnapshotHash(raw)
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, hash, first["content_hash"].(string), "snapshot checksum")
	// The exact JSONB document retains integrity across a fresh GET.
	fetched := expectStatus(t, b.do("GET", path+"/"+candidateID, nil, nil), 200)
	raw, _ = json.Marshal(fetched["snapshot"])
	hash, err = solutions.CanonicalSnapshotHash(raw)
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, hash, first["content_hash"].(string), "stored checksum")
	plan["name"] = "Changed solution plan"
	expectStatus(t, b.do("PATCH", "/api/v1/solutions/"+id, plan, map[string]string{"If-Match": `"1"`}), 200)
	retry := expectStatus(t, b.do("POST", path, map[string]any{"version": "1.0.0"}, candidateHeaders("publisher-create-1")), 200)
	mustEqual(t, retry["id"].(string), candidateID, "same original candidate after edit")
	expectStatus(t, b.do("POST", path, map[string]any{"version": "2.0.0"}, candidateHeaders("publisher-create-1")), 409)
	expectStatus(t, b.do("POST", path, map[string]any{"version": "2.0.0"}, candidateHeaders("publisher-create-2")), 412)
	headers := candidateHeaders("publisher-create-3")
	headers["If-Match"] = `"2"`
	expectStatus(t, b.do("POST", path, map[string]any{"version": "1.0.0"}, headers), 409)
	headers["Idempotency-Key"] = "publisher-create-4"
	next := expectStatus(t, b.do("POST", path, map[string]any{"version": "1.1.0"}, headers), 201)
	mustEqual(t, next["plan_revision"].(float64), 2, "new candidate pinned to current plan")
	old := expectStatus(t, b.do("GET", path+"/"+candidateID, nil, nil), 200)
	mustEqual(t, old["snapshot"].(map[string]any)["plan"].(map[string]any)["definition"].(map[string]any)["name"].(string), "Purchase review", "old plan retained")
	if _, err = e.pool.Exec(context.Background(), `UPDATE solution_candidates SET snapshot='{}' WHERE id=$1::uuid`, candidateID); err == nil {
		t.Fatal("candidate changed")
	}
	if _, err = e.pool.Exec(context.Background(), `DELETE FROM solution_candidates WHERE id=$1::uuid`, candidateID); err == nil {
		t.Fatal("candidate deleted")
	}
	mustEqual(t, e.auditCount("solution.candidate.created"), 2, "one audit per actual candidate")
	list := expectStatus(t, b.do("GET", path, nil, nil), 200)
	mustEqual(t, len(list["items"].([]any)), 2, "version history")
}
func TestCandidateIsolationAndApplicationConfigurationCannotGrantRights(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	body := solutionPlan("scope-candidate")
	body["application"] = map[string]any{"title": "App", "pages": []map[string]any{{"key": "overview", "title": "Overview", "kind": "overview", "script": "invalid"}}}
	expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("scope-plan-0")), 400)
	delete(body, "application")
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("scope-plan-1")), 201)
	path := "/api/v1/solutions/" + plan["id"].(string) + "/candidates"
	item := expectStatus(t, b.do("POST", path, map[string]any{"version": "0.1.0"}, candidateHeaders("scope-candidate-1")), 201)
	worker := e.as(subDimas)
	expectStatus(t, worker.do("GET", path, nil, nil), 403)
	expectStatus(t, worker.do("GET", path+"/"+item["id"].(string), nil, nil), 403)
	expectStatus(t, worker.do("POST", path, map[string]any{"version": "0.2.0"}, candidateHeaders("scope-candidate-2")), 403)
	// A second workspace author cannot read the first workspace's snapshot by ID.
	_, err := e.pool.Exec(context.Background(), `INSERT INTO role_bindings(tenant_id,user_id,workspace_id,role_id,granted_by,reason) SELECT $1::uuid,$2::uuid,$3::uuid,id,$2::uuid,'test access' FROM roles WHERE key='solution_builder'`, e.tenantID, e.userID(subRafi), e.workspaceID("engineering"))
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("engineering")}, nil), 204)
	expectStatus(t, b.do("GET", path, nil, nil), 404)
	expectStatus(t, b.do("GET", path+"/"+item["id"].(string), nil, nil), 404)
	expectStatus(t, b.do("POST", path, map[string]any{"version": "0.2.0"}, candidateHeaders("scope-candidate-2")), 404)
}
