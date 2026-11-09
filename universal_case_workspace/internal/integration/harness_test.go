// Package integration runs the BFF end to end against a real PostgreSQL (UCW_TEST_DATABASE_URL) with
// fake Control Plane (JWKS + token exchange), Flowcase and Agent Studio servers. Skipped without the env.
package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elenio/case-workspace/internal/agentstudio"
	"github.com/elenio/case-workspace/internal/audit"
	"github.com/elenio/case-workspace/internal/auth"
	"github.com/elenio/case-workspace/internal/controlplane"
	"github.com/elenio/case-workspace/internal/docling"
	"github.com/elenio/case-workspace/internal/flowcase"
	"github.com/elenio/case-workspace/internal/httpapi"
	"github.com/elenio/case-workspace/internal/intake"
	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/internal/store"
	"github.com/elenio/case-workspace/internal/testkit"
	"github.com/elenio/case-workspace/internal/workspace"
)

const (
	maya    = "aaaaaaaa-0000-4000-8000-000000000003" // case owner
	rafi    = "aaaaaaaa-0000-4000-8000-000000000001" // member without case_owner
	outside = "aaaaaaaa-0000-4000-8000-000000000009" // exchange denied (session ended)
	secret  = "exchange-client-secret-0123456789"
	svcTok  = "agent-studio-service-token-0123456789abcdef"
	dlKey   = "docling-api-key-0123456789"
)

// --- fake Control Plane token exchange --------------------------------------------------------------

type fakeCP struct {
	mu     sync.Mutex
	denied map[string]bool
	down   bool
	calls  int
}

func (f *fakeCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if r.URL.Path != "/api/v1/token-exchange" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if f.down {
		w.WriteHeader(503)
		return
	}
	if u, p, ok := r.BasicAuth(); !ok || u != "case-workspace" || p != secret {
		w.WriteHeader(401)
		return
	}
	var in struct {
		SubjectToken string `json:"subject_token"`
		Audience     string `json:"audience"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	parts := strings.Split(in.SubjectToken, ".")
	if len(parts) != 3 || in.Audience != "flowcase" {
		w.WriteHeader(400)
		return
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(raw, &c)
	if f.denied[c.Sub] {
		w.WriteHeader(401)
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": "fc-" + c.Sub, "expires_in": 300, "token_type": "Bearer"})
}

// --- fake Flowcase ----------------------------------------------------------------------------------

type fcCase struct {
	detail map[string]any
	owner  string
}

type fakeFlowcase struct {
	mu       sync.Mutex
	down     bool
	roles    map[string][]string
	cases    map[string]*fcCase
	order    []string
	drafts   map[string]map[string]any // draft id -> draft (with case id)
	idem     map[string]string
	notes    map[string][]string
	created  []flowcase.CreateInput
	required []string
	seq      int
}

func newFlowcase() *fakeFlowcase {
	return &fakeFlowcase{roles: map[string][]string{maya: {"workspace_member", "case_owner"}, rafi: {"workspace_member"}},
		cases: map[string]*fcCase{}, drafts: map[string]map[string]any{}, idem: map[string]string{}, notes: map[string][]string{},
		required: []string{"deadline"}}
}

func fcErr(w http.ResponseWriter, status int, code, msg string, details ...map[string]string) {
	body := map[string]any{"code": code, "message": msg, "trace_id": "fc"}
	if len(details) > 0 {
		body["details"] = details
	}
	writeJSON(w, status, map[string]any{"error": body})
}

func uuid(n int) string { return fmt.Sprintf("cccccccc-0000-4000-8000-%012d", n) }

// addCase seeds a case of workflowKey owned by owner with one pending draft and one artifact.
func (f *fakeFlowcase) addCase(owner, workflowKey string, version int) (caseID, draftID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	caseID, draftID = uuid(f.seq), uuid(1000+f.seq)
	now := time.Now().UTC()
	draft := map[string]any{"id": draftID, "case": map[string]any{"id": caseID}, "node_id": "intake", "node_name": "Intake Agent", "iteration": 1,
		"artifact_type": "requirement-register", "artifact_version": 1, "content": map[string]any{}, "status": "pending", "version_num": 1,
		"can_submit": true, "can_revise": true, "revisions": 0, "created_at": now.Format(time.RFC3339)}
	older := map[string]any{"id": uuid(5000 + f.seq), "case": map[string]any{"id": caseID}, "node_id": "intake", "status": "revision_requested",
		"version_num": 2, "created_at": now.Add(-time.Hour).Format(time.RFC3339)}
	f.drafts[draftID] = draft
	f.cases[caseID] = &fcCase{owner: owner, detail: map[string]any{
		"id": caseID, "reference": fmt.Sprintf("CASE-2026-%04d", f.seq), "title": "Case " + strconv.Itoa(f.seq), "status": "active", "outcome": nil,
		"workflow_key": workflowKey, "workflow_name": workflowKey, "workflow_version": version, "owner_name": "Maya Putri",
		"current_steps": []string{"intake"}, "open_tasks": 0, "pending_approvals": 1, "progress": 40, "created_at": now, "updated_at": now, "version_num": 4,
		"variables": map[string]any{"customer_id": "C-1"}, "business_facets": map[string]any{},
		"workflow_nodes": []map[string]any{{"id": "g0", "name": "G0", "type": "human_approval"}, {"id": "intake", "name": "Intake Agent", "type": "agent_task"}},
		"timeline": []map[string]any{{"node_id": "g0", "status": "completed", "iteration": 1, "created_at": now.Add(-2 * time.Hour)},
			{"node_id": "intake", "status": "waiting_human", "iteration": 1, "created_at": now.Add(-time.Hour)}},
		"tasks": []any{}, "approvals": []map[string]any{{"id": uuid(9000 + f.seq), "node_id": "g1", "status": "pending"}},
		"history": []any{}, "agent_runs": []any{}, "actions": map[string]any{"can_note": true},
		"artifacts": []map[string]any{{"id": uuid(7000 + f.seq), "artifact_type": "requirement-register", "producer_node_id": "intake", "current_version": 1,
			"versions": []map[string]any{{"id": uuid(8000 + f.seq), "version": 1, "status": "ready_for_review", "produced_by_kind": "agent", "created_at": now,
				"content": map[string]any{"artifact_draft": map[string]any{"requirements": []any{map[string]any{"id": "R-001", "statement": "x"}}},
					"sources": []any{map[string]any{"source_id": "kms-1", "title": "RFP"}}}}}}},
		"_drafts": []string{older["id"].(string), draftID},
	}}
	f.drafts[older["id"].(string)] = older
	f.order = append(f.order, caseID)
	return caseID, draftID
}

func (f *fakeFlowcase) detail(id string) map[string]any {
	c := f.cases[id]
	out := map[string]any{}
	for k, v := range c.detail {
		if k != "_drafts" {
			out[k] = v
		}
	}
	var ds []any
	for _, did := range c.detail["_drafts"].([]string) {
		ds = append(ds, f.drafts[did])
	}
	out["drafts"] = ds
	return out
}

func (f *fakeFlowcase) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		w.WriteHeader(503)
		return
	}
	sub := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer fc-")
	roles, ok := f.roles[sub]
	if !ok {
		fcErr(w, 401, "unauthenticated", "no")
		return
	}
	path := r.URL.Path
	visible := func(id string) bool { c, ok := f.cases[id]; return ok && c.owner == sub }
	switch {
	case r.Method == "GET" && path == "/api/v1/session":
		writeJSON(w, 200, map[string]any{"user_id": "fc-" + sub, "display_name": "User " + sub[len(sub)-1:], "roles": roles})
	case r.Method == "GET" && path == "/api/v1/workflows/sim-tender":
		writeJSON(w, 200, map[string]any{"key": "sim-tender", "latest_published_version": 3})
	case r.Method == "GET" && path == "/api/v1/workflows/sim-tender/versions/3":
		writeJSON(w, 200, map[string]any{"version": 3, "status": "published", "definition": map[string]any{"input_schema": map[string]any{"required": f.required}}})
	case r.Method == "GET" && strings.HasPrefix(path, "/api/v1/workflows/"):
		fcErr(w, 404, "not_found", "Not found.")
	case r.Method == "GET" && path == "/api/v1/cases":
		var items []any
		for _, id := range f.order {
			if visible(id) {
				items = append(items, f.detail(id))
			}
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
	case r.Method == "GET" && path == "/api/v1/drafts":
		var items []any
		for _, d := range f.drafts {
			if d["status"] == "pending" && visible(d["case"].(map[string]any)["id"].(string)) {
				items = append(items, d)
			}
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case r.Method == "GET" && strings.HasPrefix(path, "/api/v1/cases/"):
		id := strings.TrimPrefix(path, "/api/v1/cases/")
		if !visible(id) {
			fcErr(w, 404, "not_found", "Not found.")
			return
		}
		w.Header().Set("ETag", `"4"`)
		writeJSON(w, 200, f.detail(id))
	case r.Method == "POST" && strings.HasPrefix(path, "/api/v1/drafts/"):
		parts := strings.Split(strings.TrimPrefix(path, "/api/v1/drafts/"), "/")
		d, ok := f.drafts[parts[0]]
		if !ok || !visible(d["case"].(map[string]any)["id"].(string)) {
			fcErr(w, 404, "not_found", "Not found.")
			return
		}
		if r.Header.Get("If-Match") != fmt.Sprintf(`"%v"`, d["version_num"]) {
			fcErr(w, 412, "version_mismatch", "stale")
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if parts[1] == "revision" && len(in["feedback"]) < 5 {
			fcErr(w, 400, "feedback_required", "Feedback needs 5 to 1000 characters.", map[string]string{"code": "feedback_required", "message": "short", "path": "feedback"})
			return
		}
		d["status"] = map[string]string{"submit": "submitted", "revision": "revision_requested"}[parts[1]]
		d["version_num"] = d["version_num"].(int) + 1
		writeJSON(w, 200, d)
	case r.Method == "POST" && path == "/api/v1/cases":
		key := r.Header.Get("Idempotency-Key")
		if len(key) < 8 {
			fcErr(w, 400, "idempotency_key_required", "key")
			return
		}
		if !contains(roles, "case_owner") {
			fcErr(w, 403, "forbidden", "role")
			return
		}
		var in flowcase.CreateInput
		_ = json.NewDecoder(r.Body).Decode(&in)
		if id, ok := f.idem[key]; ok {
			writeJSON(w, 201, f.detail(id))
			return
		}
		for _, req := range f.required {
			if _, ok := in.Variables[req]; !ok {
				fcErr(w, 422, "invalid_input", "schema", map[string]string{"code": "required", "message": req + " is required", "path": "/variables/" + req})
				return
			}
		}
		f.created = append(f.created, in)
		f.mu.Unlock()
		id, _ := f.addCase(sub, in.WorkflowKey, 3)
		f.mu.Lock()
		f.cases[id].detail["title"] = in.Title
		f.idem[key] = id
		writeJSON(w, 201, f.detail(id))
	case r.Method == "POST" && strings.HasSuffix(path, "/events"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/cases/"), "/events")
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.notes[id] = append(f.notes[id], in["body"])
		writeJSON(w, 201, f.detail(id))
	default:
		fcErr(w, 404, "route_not_found", path)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// --- fake Agent Studio ------------------------------------------------------------------------------

type fakeAS struct {
	mu        sync.Mutex
	down      bool
	tamper    bool
	wrongAgnt bool
	rfpUpdate bool // the agent also tries to rewrite rfp_excerpt
	runs      map[string]agentstudio.RunRequest
	requests  []agentstudio.RunRequest
}

func turnOf(key string) int {
	n, _ := strconv.Atoi(key[strings.LastIndexByte(key, ':')+1:])
	return n
}

func (f *fakeAS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+svcTok {
		writeJSON(w, 401, map[string]string{"error": "unauthenticated"})
		return
	}
	if f.down {
		writeJSON(w, 503, map[string]string{"error": "down"})
		return
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/v1/runs":
		var req agentstudio.RunRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || !agentstudio.IdempotencyPattern.MatchString(req.IdempotencyKey) {
			writeJSON(w, 400, map[string]string{"error": "bad request"})
			return
		}
		f.requests = append(f.requests, req)
		id := "run-" + strings.ReplaceAll(req.IdempotencyKey, ":", "-")
		f.runs[id] = req
		writeJSON(w, 202, map[string]any{"run_id": id, "status": "queued", "agent_id": req.AgentID})
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/runs/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
		req, ok := f.runs[id]
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		draft := map[string]any{"reply": "Terima kasih, saya catat.", "case_update": map[string]any{"title": "Asisten AI contact center",
			"customer_name": "PT Contoh", "case_summary": "Asisten AI berbasis SOP"}, "kb_status": "partial", "ready": false,
			"kb_items": []any{map[string]any{"name": "AI CC", "kind": "solution", "evidence_refs": []any{"kms-1"}}}, "missing": []any{}, "quick_replies": []any{}, "unresolved": []any{}}
		if f.rfpUpdate {
			draft["case_update"].(map[string]any)["rfp_excerpt"] = "ringkasan agent"
		}
		if turnOf(req.IdempotencyKey) > 1 {
			draft["case_update"] = map[string]any{"customer_id": "CUST-1", "opportunity_id": "OPP-1", "deadline": "2027-01-29"} // IDs are not pack fields: ignored
			draft["kb_status"], draft["ready"] = "covered", true
		}
		out, _ := json.Marshal(map[string]any{"artifact_draft": draft, "review": map[string]any{"status": "ready_for_review",
			"validation": map[string]any{"cited_sources": []any{"kms-1"}}}, "pinned": nil, "model_mode": "live",
			"sources": []any{map[string]any{"source_id": "kms-1", "version_id": "v1", "title": "Katalog AI CC"}}})
		sum, _ := agentstudio.OutputChecksum(out)
		if f.tamper {
			sum = strings.Repeat("0", 64)
		}
		agent := req.AgentID
		if f.wrongAgnt {
			agent = "sim.other"
		}
		writeJSON(w, 200, map[string]any{"run_id": id, "status": "succeeded", "agent_id": agent, "output": json.RawMessage(out), "output_checksum": sum})
	default:
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

// --- fake Docling -----------------------------------------------------------------------------------

type dlTask struct {
	filename string
	content  []byte
	polls    int
}

type fakeDocling struct {
	mu        sync.Mutex
	tasks     map[string]*dlTask
	pendingN  int // polls answered "started" before success
	submitted []url.Values
}

func (f *fakeDocling) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-Api-Key") != dlKey {
		writeJSON(w, 401, map[string]string{"detail": "bad key"})
		return
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/convert/file/async":
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeJSON(w, 400, map[string]string{"detail": err.Error()})
			return
		}
		file, hdr, err := r.FormFile("files")
		if err != nil {
			writeJSON(w, 400, map[string]string{"detail": "no file"})
			return
		}
		content, _ := io.ReadAll(file)
		id := fmt.Sprintf("task-%08d", len(f.tasks)+1)
		f.tasks[id] = &dlTask{filename: hdr.Filename, content: content}
		f.submitted = append(f.submitted, r.MultipartForm.Value)
		writeJSON(w, 200, map[string]any{"task_id": id, "task_status": "pending"})
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/status/poll/"):
		t, ok := f.tasks[strings.TrimPrefix(r.URL.Path, "/v1/status/poll/")]
		if !ok {
			writeJSON(w, 404, map[string]string{"detail": "Task not found."})
			return
		}
		t.polls++
		status := "success"
		switch {
		case bytes.HasPrefix(t.content, []byte("%PDF-FAIL")):
			status = "failure"
		case t.polls <= f.pendingN:
			status = "started"
		}
		writeJSON(w, 200, map[string]any{"task_status": status})
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/result/"):
		t, ok := f.tasks[strings.TrimPrefix(r.URL.Path, "/v1/result/")]
		if !ok {
			writeJSON(w, 404, map[string]string{"detail": "Task not found."})
			return
		}
		md := "## RENCANA KERJA DAN SYARAT TEKNIS\n\n<!-- image -->\n\nPengadaan management server &amp; storage CCTV untuk 310 kamera, rekaman 30 hari (" + t.filename + ")."
		writeJSON(w, 200, map[string]any{"status": "success", "errors": []any{}, "document": map[string]any{"filename": t.filename, "md_content": md}})
	default:
		writeJSON(w, 404, map[string]string{"detail": "Not Found"})
	}
}

// --- harness ----------------------------------------------------------------------------------------

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	signer *testkit.Signer
	cp     *fakeCP
	fc     *fakeFlowcase
	as     *fakeAS
	dl     *fakeDocling
	st     *store.Store
	reg    *packs.Registry
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func resetDB(t *testing.T, st *store.Store) {
	t.Helper()
	if _, err := st.DB.Exec(context.Background(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// newHarness wires the real BFF; before runs between migration and pack loading (drift tests).
func newHarness(t *testing.T, before func(st *store.Store)) *harness {
	t.Helper()
	url := os.Getenv("UCW_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("UCW_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	resetDB(t, st)
	if before != nil {
		before(st)
	}
	log := slog.New(slog.DiscardHandler)
	rec := &audit.Recorder{Sink: st, Tenant: testkit.Tenant, Log: log}
	reg, err := packs.Load(ctx, "../../domain-packs", st, func(ctx context.Context, a, res string, m map[string]any) { rec.Record(ctx, "system", a, res, m) }, log)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, signer: testkit.NewSigner(), cp: &fakeCP{denied: map[string]bool{outside: true}}, fc: newFlowcase(),
		as: &fakeAS{runs: map[string]agentstudio.RunRequest{}}, dl: &fakeDocling{tasks: map[string]*dlTask{}}, st: st, reg: reg}
	jwks := h.signer.JWKSServer()
	cpSrv, fcSrv, asSrv, dlSrv := httptest.NewServer(h.cp), httptest.NewServer(h.fc), httptest.NewServer(h.as), httptest.NewServer(h.dl)
	t.Cleanup(func() { jwks.Close(); cpSrv.Close(); fcSrv.Close(); asSrv.Close(); dlSrv.Close() })
	v, _ := auth.NewVerifier(auth.Config{Issuer: testkit.Issuer, JWKSURL: jwks.URL, Audience: testkit.Audience, TenantKey: testkit.Tenant})
	ex, _ := controlplane.New(controlplane.Config{BaseURL: cpSrv.URL, ClientID: "case-workspace", ClientSecret: secret})
	fc, _ := flowcase.New(fcSrv.URL, 5*time.Second, nil)
	as, _ := agentstudio.New(asSrv.URL, svcTok, 5*time.Second, nil)
	dl, _ := docling.New(dlSrv.URL, dlKey, 5*time.Second, nil)
	ws := &workspace.Service{Exchange: ex, Flowcase: fc, Packs: reg, Bindings: st, Audit: rec, FlowcaseUI: "/apps/flowcase", Log: log}
	in := &intake.Service{DB: st.DB, Runner: as, Packs: reg, Gate: ws.Gate, Flowcase: fc, Token: ws.FlowcaseToken, Binder: st, Audit: rec, Log: log, Docling: dl}
	h.srv = httptest.NewServer(httpapi.New(httpapi.Deps{Log: log, Verifier: v, Workspace: ws, Intake: in,
		Readiness: []httpapi.Check{{Name: "database", Run: st.Ping}}}))
	t.Cleanup(h.srv.Close)
	return h
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (h *harness) do(method, path, user string, body any, headers map[string]string) resp {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("Authorization", "Bearer "+h.signer.Token(user))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, header: res.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

// upload posts one multipart file field.
func (h *harness) upload(path, user, filename string, content []byte) resp {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", h.srv.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.signer.Token(user))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, header: res.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (r resp) errCode() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (h *harness) expect(r resp, status int) resp {
	h.t.Helper()
	if r.status != status {
		h.t.Fatalf("status %d, want %d: %s", r.status, status, r.raw)
	}
	return r
}

func (h *harness) auditCount(action string) int {
	var n int
	_ = h.st.DB.QueryRow(context.Background(), `SELECT count(*) FROM workspace_audit_events WHERE action = $1`, action).Scan(&n)
	return n
}
