package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/golang-jwt/jwt/v5"
)

func TestSolutionExecutionRejectsInvalidSourceAndRevokedEntitlement(t *testing.T) {
	var mode string
	var calls, redirects int
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; w.WriteHeader(200) }))
	defer trap.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch mode {
		case "denied":
			w.WriteHeader(403)
			return
		case "redirect":
			http.Redirect(w, r, trap.URL, 302)
			return
		case "oversized":
			_, _ = io.WriteString(w, strings.Repeat("x", (1<<20)+1))
			return
		case "invalid":
			_, _ = io.WriteString(w, `{"id":`)
			return
		}
		version, hash := 2, strings.Repeat("a", 64)
		if mode == "wrong-version" {
			version = 3
		}
		if mode == "wrong-hash" {
			hash = "unverified"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "46e8df65-e679-49b4-95b1-222222222222", "workflow_key": "purchase-review", "workflow_version": version, "definition_hash": hash})
	}))
	defer upstream.Close()
	e := newEnv(t, envOptions{upstreams: map[string]config.Upstream{"flowcase": {URL: upstream.URL, Audience: "flowcase", Mode: "proxy", StripPrefix: true}}})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", solutionPlan("source-negative"), solutionHeaders("source-negative-plan")), 201)
	path := "/api/v1/solutions/" + plan["id"].(string) + "/executions"
	input := map[string]any{"workflow_key": "purchase-review", "title": "Synthetic", "variables": map[string]any{}}
	for i, variables := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`"not an object"`)} {
		invalid := map[string]any{"workflow_key": "purchase-review", "title": "Synthetic", "variables": variables}
		expectStatus(t, b.do("POST", path, invalid, map[string]string{"If-Match": `"1"`, "Idempotency-Key": fmt.Sprintf("invalid-input-%02d", i)}), 400)
	}
	expectStatus(t, b.do("POST", path, map[string]any{"workflow_key": "not-linked", "title": "Synthetic"}, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "unlinked-input-01"}), 400)
	if calls != 0 {
		t.Fatal("forwarded invalid or unlinked input")
	}
	for i, test := range []struct {
		mode   string
		status int
	}{{"denied", 403}, {"redirect", 502}, {"oversized", 502}, {"invalid", 502}, {"wrong-version", 502}, {"wrong-hash", 502}} {
		mode = test.mode
		expectStatus(t, b.do("POST", path, input, map[string]string{"If-Match": `"1"`, "Idempotency-Key": fmt.Sprintf("negative-run-%02d", i)}), test.status)
	}
	if redirects != 0 {
		t.Fatal("followed untrusted source redirect")
	}
	var confirmed int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM solution_executions WHERE case_id IS NOT NULL`).Scan(&confirmed); err != nil || confirmed != 0 {
		t.Fatalf("invalid source confirmed execution: %d %v", confirmed, err)
	}
	before := calls
	if _, err := e.pool.Exec(context.Background(), `UPDATE mini_apps SET status='disabled' WHERE app_key='flowcase'`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, b.do("POST", path, input, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "revoked-source-run"}), 403)
	if calls != before {
		t.Fatal("forwarded after app entitlement revoked")
	}
}

// Flowcase is an explicit HTTP contract double; the session, policy, migration and intent store use real PostgreSQL.
func TestSolutionExecutionPinnedRetryScopeAndSourceAuthority(t *testing.T) {
	var e *env
	var mu sync.Mutex
	var bodies []string
	var keys []string
	status, failFirst := 200, true
	caseID := "46e8df65-e679-49b4-95b1-111111111111"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		token, err := jwt.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), func(*jwt.Token) (any, error) { return e.tokens.PublicKey(), nil }, jwt.WithAudience("flowcase"), jwt.WithIssuer(e.cfg.PublicBaseURL.String()), jwt.WithValidMethods([]string{"ES256"}))
		if err != nil || !token.Valid {
			t.Error("invalid delegated identity")
			w.WriteHeader(401)
			return
		}
		claims := token.Claims.(jwt.MapClaims)
		if claims["sub"] != e.userID(subRafi) || claims["workspace_id"] != "sim-b2b" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("browser identity crossed service boundary")
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"secret":"must not escape upstream"}`))
			return
		}
		if r.Method == "POST" {
			raw, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(raw))
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			var in struct {
				WorkflowKey string          `json:"workflow_key"`
				Version     int             `json:"version"`
				Title       string          `json:"title"`
				Variables   json.RawMessage `json:"variables"`
			}
			if json.Unmarshal(raw, &in) != nil || in.WorkflowKey != "purchase-review" || in.Version != 2 {
				t.Error("execution did not use saved pin")
			}
			if failFirst {
				failFirst = false
				w.WriteHeader(503)
				return
			}
		} else if r.URL.Path != "/api/v1/cases/"+caseID {
			t.Error("wrong case read")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": caseID, "reference": "CASE-1", "title": "Invoice", "status": "running", "workflow_key": "purchase-review", "workflow_version": 2, "definition_hash": strings.Repeat("a", 64), "variables": map[string]any{"private": "not returned by BFF"}})
	}))
	defer upstream.Close()
	e = newEnv(t, envOptions{upstreams: map[string]config.Upstream{"flowcase": {URL: upstream.URL, Audience: "flowcase", Mode: "proxy", StripPrefix: true}}})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", solutionPlan("execution-plan"), solutionHeaders("execution-plan-01")), 201)
	path := "/api/v1/solutions/" + plan["id"].(string)
	headers := map[string]string{"If-Match": `"1"`, "Idempotency-Key": "execution-retry-01", "X-Tenant-ID": "forged"}
	input := map[string]any{"workflow_key": "purchase-review", "title": "Invoice", "variables": json.RawMessage(`{"z":9007199254740993,"a":true}`)}
	expectStatus(t, b.do("POST", path+"/executions", input, nil), 428)
	invalid := map[string]any{"workflow_key": "purchase-review", "title": "Invoice", "variables": map[string]any{}, "version": 999}
	expectStatus(t, b.do("POST", path+"/executions", invalid, headers), 400)
	expectStatus(t, b.do("POST", path+"/executions", input, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "csrf-rejected-01", "X-CSRF-Token": ""}), 403)
	expectStatus(t, b.do("POST", path+"/executions", input, headers), 503)
	pending := expectStatus(t, b.do("GET", path+"/executions", nil, nil), 200)
	mustEqual(t, len(pending["items"].([]any)), 1, "pending durable intent")
	if pending["items"].([]any)[0].(map[string]any)["case"] != nil {
		t.Fatal("pending intent fabricated a case")
	}
	updated := solutionPlan("execution-plan")
	updated["workflows"] = []map[string]any{{"key": "purchase-review", "version": 3}}
	expectStatus(t, b.do("PATCH", path, updated, map[string]string{"If-Match": `"1"`}), 200)
	input["variables"] = json.RawMessage(`{"a":true,"z":9007199254740993}`)
	result := expectStatus(t, b.do("POST", path+"/executions", input, headers), 200)
	execution := result["execution"].(map[string]any)
	mustEqual(t, execution["workflow_version"].(float64), 2, "retry preserves original pin after plan edit")
	mustEqual(t, result["case"].(map[string]any)["status"].(string), "running", "source status")
	if result["case"].(map[string]any)["variables"] != nil {
		t.Fatal("raw business data copied to CP response")
	}
	repeat := expectStatus(t, b.do("POST", path+"/executions", input, headers), 200)
	mustEqual(t, repeat["execution"].(map[string]any)["id"].(string), execution["id"].(string), "stable intent")
	mu.Lock()
	if len(bodies) != 2 || bodies[0] != bodies[1] || keys[0] != keys[1] || !strings.Contains(bodies[0], "9007199254740993") {
		t.Error("retry body/key changed or number lost precision")
	}
	mu.Unlock()
	changed := map[string]any{"workflow_key": "purchase-review", "title": "Changed", "variables": map[string]any{}}
	expectStatus(t, b.do("POST", path+"/executions", changed, headers), 409)
	expectStatus(t, b.do("POST", path+"/executions", input, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "stale-new-run-01"}), 412)
	reader := e.as(subRafi)
	expectStatus(t, reader.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	listed := expectStatus(t, reader.do("GET", path+"/executions", nil, nil), 200)
	mustEqual(t, listed["scope"].(string), "current_user_solution_executions", "explicit list scope")
	mustEqual(t, len(listed["items"].([]any)), 1, "reloaded persisted association")
	mu.Lock()
	status = 403
	mu.Unlock()
	denied := expectStatus(t, reader.do("GET", path+"/executions", nil, nil), 403)
	serialized, _ := json.Marshal(denied)
	if strings.Contains(string(serialized), "must not escape") {
		t.Fatal("upstream body leaked")
	}
	worker := e.as(subDimas)
	expectStatus(t, worker.do("GET", path+"/executions", nil, nil), 403)
	expectStatus(t, worker.do("POST", path+"/executions", input, headers), 403)
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO workspace_memberships(tenant_id,workspace_id,user_id) SELECT $1::uuid,$2::uuid,$3::uuid WHERE NOT EXISTS (SELECT 1 FROM workspace_memberships WHERE workspace_id=$2::uuid AND user_id=$3::uuid AND state<>'revoked')`, e.tenantID, e.workspaceID("sim-b2b"), e.userID(subDimas)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO role_bindings(tenant_id,user_id,workspace_id,role_id,granted_by,reason) SELECT $1::uuid,$2::uuid,$3::uuid,id,$4::uuid,'test actor isolation' FROM roles WHERE key='solution_builder'`, e.tenantID, e.userID(subDimas), e.workspaceID("sim-b2b"), e.userID(subRafi)); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, worker.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	other := expectStatus(t, worker.do("GET", path+"/executions", nil, nil), 200)
	mustEqual(t, len(other["items"].([]any)), 0, "another authorized builder cannot list the original actor's cases")
	if e.auditCount("solution.execution_requested") != 1 || e.auditCount("solution.execution_started") != 1 {
		t.Fatal("retry duplicated audit")
	}
	var count int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM solution_executions WHERE case_id=$1::uuid`, caseID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable association: %d %v", count, err)
	}
	_, err := e.pool.Exec(context.Background(), `INSERT INTO role_bindings(tenant_id,user_id,workspace_id,role_id,granted_by,reason) SELECT $1::uuid,$2::uuid,$3::uuid,id,$2::uuid,'test scope' FROM roles WHERE key='solution_builder'`, e.tenantID, e.userID(subRafi), e.workspaceID("engineering"))
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("engineering")}, nil), 204)
	expectStatus(t, b.do("GET", path+"/executions", nil, nil), 404)
}
