package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/golang-jwt/jwt/v5"
)

func solutionPlan(key string) map[string]any {
	return map[string]any{"key": key, "name": "Purchase review", "description": "Synthetic solution plan", "delivery_mode": "solution", "output": "both", "workflows": []map[string]any{{"key": "purchase-review", "version": 2}, {"key": "exception-review", "version": 1}}}
}

func TestSolutionReadinessUsesAuthorizedPinnedUpstreamAndNeverClaimsRelease(t *testing.T) {
	var e *env
	var requests, redirects atomic.Int64
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1); w.WriteHeader(200) }))
	defer trap.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("upstream received mutation or browser identity")
		}
		tok, err := jwt.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), func(*jwt.Token) (any, error) { return e.tokens.PublicKey(), nil }, jwt.WithAudience("flowcase"), jwt.WithIssuer(e.cfg.PublicBaseURL.String()), jwt.WithValidMethods([]string{"ES256"}))
		if err != nil || !tok.Valid {
			t.Error("invalid delegated token")
			w.WriteHeader(401)
			return
		}
		claims := tok.Claims.(jwt.MapClaims)
		if claims["sub"] != e.userID(subRafi) || claims["workspace_id"] != "sim-b2b" {
			t.Error("wrong delegated identity")
		}
		key := strings.Split(r.URL.Path, "/")[4]
		if key == "missing" {
			w.WriteHeader(404)
			return
		}
		if key == "redirect" {
			http.Redirect(w, r, trap.URL, 302)
			return
		}
		status := "published"
		hash := strings.Repeat("a", 64)
		if key == "draft" {
			status = "draft"
		}
		if key == "no-hash" {
			hash = ""
		}
		if key == "mismatch" {
			key = "another-workflow"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_key": key, "version": 2, "status": status, "definition_hash": hash})
	}))
	defer upstream.Close()
	e = newEnv(t, envOptions{upstreams: map[string]config.Upstream{"flowcase": {URL: upstream.URL, Audience: "flowcase", Mode: "proxy", StripPrefix: true}}})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	body := solutionPlan("readiness-plan")
	refs := []map[string]any{}
	for _, key := range []string{"published", "draft", "missing", "redirect", "no-hash", "mismatch"} {
		refs = append(refs, map[string]any{"key": key, "version": 2})
	}
	body["workflows"] = refs
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("readiness-create-1")), 201)
	path := "/api/v1/solutions/" + plan["id"].(string) + "/readiness"
	result := expectStatus(t, b.do("GET", path, nil, map[string]string{"X-Tenant-ID": "forged"}), 200)
	mustEqual(t, result["release_ready"].(bool), false, "partial readiness is not a release")
	dependencies := result["workflows"].([]any)
	for i, value := range dependencies {
		d := value.(map[string]any)
		want := "blocked"
		if i == 0 {
			want = "verified"
		}
		mustEqual(t, d["state"].(string), want, "dependency state")
	}
	mustEqual(t, requests.Load(), int64(6), "one read per pin")
	mustEqual(t, redirects.Load(), int64(0), "redirect not followed")
	if len(result["blockers"].([]any)) == 0 {
		t.Fatal("transitive/deployment blockers absent")
	}
	// Disabling the authoritative app takes effect on the next check, without an upstream request.
	if _, err := e.pool.Exec(context.Background(), `UPDATE mini_apps SET status='disabled' WHERE app_key='flowcase'`); err != nil {
		t.Fatal(err)
	}
	result = expectStatus(t, b.do("GET", path, nil, nil), 200)
	for _, value := range result["workflows"].([]any) {
		mustEqual(t, value.(map[string]any)["state"].(string), "blocked", "revoked app")
	}
	mustEqual(t, requests.Load(), int64(6), "no forwarding after disable")
}
func solutionHeaders(key string) map[string]string { return map[string]string{"Idempotency-Key": key} }

func TestSolutionPersistenceIdempotencyAndRevision(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	body := solutionPlan("purchase-review")
	expectStatus(t, b.do("POST", "/api/v1/solutions", body, nil), 400)
	first := expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("create-purchase-1")), 201)
	id := first["id"].(string)
	path := "/api/v1/solutions/" + id
	mustEqual(t, first["revision"].(float64), 1, "initial revision")
	mustEqual(t, first["workspace_id"].(string), e.workspaceID("sim-b2b"), "server workspace")
	retry := expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("create-purchase-1")), 200)
	mustEqual(t, retry["id"].(string), id, "retry identity")
	changed := solutionPlan("different-plan")
	expectStatus(t, b.do("POST", "/api/v1/solutions", changed, solutionHeaders("create-purchase-1")), 409)
	expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("another-key-01")), 409)
	// A separate authenticated session reads the same persisted plan.
	reader := e.as(subRafi)
	expectStatus(t, reader.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	read := reader.do("GET", path, nil, nil)
	mustEqual(t, read.Header.Get("ETag"), `"1"`, "strong ETag")
	expectStatus(t, read, 200)
	body["name"] = "Updated purchase review"
	for _, etag := range []string{"", `W/"1"`, `"01"`, `*`, `"1","2"`} {
		expectStatus(t, b.do("PATCH", path, body, map[string]string{"If-Match": etag}), 428)
	}
	updated := expectStatus(t, b.do("PATCH", path, body, map[string]string{"If-Match": `"1"`}), 200)
	mustEqual(t, updated["revision"].(float64), 2, "updated revision")
	expectStatus(t, b.do("PATCH", path, body, map[string]string{"If-Match": `"1"`}), 412)
	body["key"] = "changed-key"
	expectStatus(t, b.do("PATCH", path, body, map[string]string{"If-Match": `"2"`}), 400)
	list := expectStatus(t, b.do("GET", "/api/v1/solutions", nil, nil), 200)
	mustEqual(t, len(list["items"].([]any)), 1, "one authoritative solution")
	if e.auditCount("solution.created") != 1 || e.auditCount("solution.updated") != 1 {
		t.Fatal("retry must not duplicate transactional audit")
	}
	var workflows int
	if err := e.pool.QueryRow(context.Background(), `SELECT jsonb_array_length(definition->'workflows') FROM solution_projects WHERE id=$1::uuid`, id).Scan(&workflows); err != nil || workflows != 2 {
		t.Fatalf("pinned refs: %d, %v", workflows, err)
	}
}

func TestSolutionIsolationPermissionsAndRevocation(t *testing.T) {
	e := newEnv(t, envOptions{})
	author := e.as(subRafi)
	expectStatus(t, author.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	first := expectStatus(t, author.do("POST", "/api/v1/solutions", solutionPlan("isolated-plan"), solutionHeaders("isolated-plan-1")), 201)
	path := "/api/v1/solutions/" + first["id"].(string)
	worker := e.as(subDimas)
	expectStatus(t, worker.do("GET", path, nil, nil), 403)
	expectStatus(t, worker.do("POST", "/api/v1/solutions", solutionPlan("forbidden-plan"), solutionHeaders("forbidden-plan-1")), 403)
	// Forged scope/actor fields are rejected, not used as authority.
	body := solutionPlan("tampered-plan")
	body["tenant_id"] = e.tenantID
	expectStatus(t, author.do("POST", "/api/v1/solutions", body, solutionHeaders("tampered-plan-1")), 400)
	body = solutionPlan("tampered-plan")
	body["workspace_id"] = e.workspaceID("engineering")
	expectStatus(t, author.do("POST", "/api/v1/solutions", body, solutionHeaders("tampered-plan-2")), 400)
	// Provision the nonsensitive planning role in the second workspace using the test fixture only.
	_, err := e.pool.Exec(context.Background(), `INSERT INTO role_bindings(tenant_id,user_id,workspace_id,role_id,granted_by,reason) SELECT $1::uuid,$2::uuid,$3::uuid,id,$2::uuid,'test planning access' FROM roles WHERE key='solution_builder'`, e.tenantID, e.userID(subRafi), e.workspaceID("engineering"))
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, author.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("engineering")}, nil), 204)
	expectStatus(t, author.do("GET", path, nil, nil), 404)
	list := expectStatus(t, author.do("GET", "/api/v1/solutions", nil, nil), 200)
	mustEqual(t, len(list["items"].([]any)), 0, "workspace isolation")
	expectStatus(t, author.do("PATCH", path, solutionPlan("isolated-plan"), map[string]string{"If-Match": `"1"`}), 404)
	// Database constraint prevents cross-tenant workspace placement even by a faulty caller.
	var foreignTenant string
	if err = e.pool.QueryRow(context.Background(), `INSERT INTO tenants(key,name) VALUES('foreign-tenant','Foreign tenant') RETURNING id::text`).Scan(&foreignTenant); err != nil {
		t.Fatal(err)
	}
	_, err = e.pool.Exec(context.Background(), `UPDATE solution_projects SET tenant_id=$1::uuid WHERE id=$2::uuid`, foreignTenant, first["id"])
	if err == nil {
		t.Fatal("cross-tenant workspace FK did not protect solution")
	}
	_, err = e.pool.Exec(context.Background(), `UPDATE workspace_memberships SET state='suspended' WHERE workspace_id=$1::uuid AND user_id=$2::uuid`, e.workspaceID("engineering"), e.userID(subRafi))
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, author.do("GET", "/api/v1/solutions", nil, nil), 403)
	expectStatus(t, author.do("POST", "/api/v1/solutions", solutionPlan("revoked-plan"), solutionHeaders("revoked-plan-1")), 403)
}

func TestSolutionConcurrentCreateAndUpdate(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	var wg sync.WaitGroup
	responses := make(chan *http.Response, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- b.do("POST", "/api/v1/solutions", solutionPlan("concurrent-plan"), solutionHeaders("concurrent-create-1"))
		}()
	}
	wg.Wait()
	close(responses)
	var id string
	created, replayed := 0, 0
	for resp := range responses {
		status := resp.StatusCode
		if status == 201 {
			created++
		} else if status == 200 {
			replayed++
		} else {
			t.Fatalf("create status %d", status)
		}
		got := expectStatus(t, resp, status)
		if id != "" && id != got["id"].(string) {
			t.Fatal("duplicate solutions")
		}
		id = got["id"].(string)
	}
	mustEqual(t, created, 1, "one creation")
	mustEqual(t, replayed, 1, "one replay")
	responses = make(chan *http.Response, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := solutionPlan("concurrent-plan")
			body["name"] = fmt.Sprintf("Edit %d", i)
			responses <- b.do("PATCH", "/api/v1/solutions/"+id, body, map[string]string{"If-Match": `"1"`})
		}(i)
	}
	wg.Wait()
	close(responses)
	saved, conflicted := 0, 0
	for resp := range responses {
		status := resp.StatusCode
		resp.Body.Close()
		if status == 200 {
			saved++
		} else if status == 412 {
			conflicted++
		} else {
			t.Fatalf("edit status %d", status)
		}
	}
	mustEqual(t, saved, 1, "one save")
	mustEqual(t, conflicted, 1, "one conflict")
}

func TestSolutionStrictValidationAndCSRF(t *testing.T) {
	e := newEnv(t, envOptions{})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	mutations := []func(map[string]any){func(d map[string]any) { d["delivery_mode"] = "production" }, func(d map[string]any) { d["output"] = "script" }, func(d map[string]any) { d["name"] = " " }, func(d map[string]any) { d["key"] = "../escape" }, func(d map[string]any) { d["workflows"] = []map[string]any{{"key": "purchase-review", "version": 0}} }, func(d map[string]any) {
		d["workflows"] = []map[string]any{{"key": "same", "version": 1}, {"key": "same", "version": 2}}
	}, func(d map[string]any) { d["published"] = true }}
	for _, mutate := range mutations {
		d := solutionPlan("invalid-plan")
		mutate(d)
		expectStatus(t, b.do("POST", "/api/v1/solutions", d, solutionHeaders("invalid-create-1")), 400)
	}
	expectStatus(t, b.do("GET", "/api/v1/solutions?cursor=not-a-uuid", nil, nil), 400)
	expectStatus(t, b.do("POST", "/api/v1/solutions", solutionPlan("csrf-plan"), map[string]string{"Idempotency-Key": "csrf-create-1", "X-CSRF-Token": "wrong"}), 403)
	expectStatus(t, b.do("POST", "/api/v1/solutions", solutionPlan("origin-plan"), map[string]string{"Idempotency-Key": "origin-create-1", "Origin": "https://foreign.invalid"}), 403)
	expectStatus(t, e.browser().do("GET", "/api/v1/solutions", nil, nil), 401)
}
