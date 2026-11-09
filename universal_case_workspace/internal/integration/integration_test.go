package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/internal/store"
)

const api = "/api/v1/workspace"

func TestMeContext(t *testing.T) {
	h := newHarness(t, nil)
	r := h.expect(h.do("GET", api+"/me/context", maya, nil, nil), 200)
	fc := r.body["flowcase"].(map[string]any)
	if r.body["user_id"] != maya || r.body["tenant_key"] != "acme-demo" || fc["available"] != true || fc["can_create_cases"] != true {
		t.Fatalf("me = %s", r.raw)
	}
	if r.header.Get("X-Request-ID") == "" || r.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("request id missing or CORS header present")
	}
	// exchange denied (CP session no longer active): still 200, flowcase unavailable + partial error
	r = h.expect(h.do("GET", api+"/me/context", outside, nil, nil), 200)
	if r.body["flowcase"].(map[string]any)["available"] != false || !strings.Contains(r.raw, `"session_inactive"`) {
		t.Fatalf("denied me = %s", r.raw)
	}
	// non-case-owner
	r = h.expect(h.do("GET", api+"/me/context", rafi, nil, nil), 200)
	if r.body["flowcase"].(map[string]any)["can_create_cases"] != false {
		t.Fatal("rafi cannot create cases")
	}
	// no token / forged headers
	h.expect(h.do("GET", api+"/me/context", "", nil, map[string]string{"X-User-ID": maya, "X-Tenant-ID": "acme-demo"}), 401)
	h.expect(h.do("DELETE", api+"/cases", "", nil, nil), 401) // unknown method/route still authenticated first
	h.expect(h.do("GET", api+"/nope", maya, nil, nil), 404)
	// exchanged tokens are cached
	calls := h.cp.calls
	h.do("GET", api+"/me/context", maya, nil, nil)
	if h.cp.calls != calls {
		t.Fatal("exchange not cached")
	}
	h.expect(h.do("GET", "/readyz", "", nil, nil), 200)
}

func TestCasesComposition(t *testing.T) {
	h := newHarness(t, nil)
	simCase, _ := h.fc.addCase(maya, "sim-tender", 3)
	legacy, _ := h.fc.addCase(maya, "sim-tender", 2)
	other, _ := h.fc.addCase(maya, "internal-poc-request", 1)
	hidden, _ := h.fc.addCase(rafi, "sim-tender", 3)

	r := h.expect(h.do("GET", api+"/cases", maya, nil, nil), 200)
	items := r.body["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %s", r.raw)
	}
	first := items[0].(map[string]any)
	if first["pending_drafts"].(float64) != 1 || first["pack"].(map[string]any)["version"] != "1.3.0" {
		t.Fatalf("card = %v", first)
	}
	if items[2].(map[string]any)["pack"] != nil {
		t.Fatal("case without matching pack must have pack null")
	}

	r = h.expect(h.do("GET", api+"/cases/"+simCase, maya, nil, nil), 200)
	if r.header.Get("ETag") != `"4"` {
		t.Fatal("etag")
	}
	pack := r.body["pack"].(map[string]any)
	if pack["status"] != "ok" || pack["binding"] != "pinned_on_first_view" || pack["version"] != "1.3.0" {
		t.Fatalf("pack = %v", pack)
	}
	nav := r.body["navigation"].([]any)
	if nav[1].(map[string]any)["id"] != "requirements" {
		t.Fatalf("nav = %v", nav)
	}
	art := r.body["artifacts"].([]any)[0].(map[string]any)
	if art["pack_artifact_type"] != "requirement" || art["produced_by_kind"] != "agent" || len(art["sources"].([]any)) != 1 {
		t.Fatalf("artifact = %v", art)
	}
	drafts := r.body["drafts"].([]any)
	if drafts[0].(map[string]any)["status"] != "pending" {
		t.Fatalf("drafts must be newest first: %v", drafts)
	}
	nodes := map[string]string{}
	for _, n := range r.body["workflow"].(map[string]any)["nodes"].([]any) {
		nodes[n.(map[string]any)["id"].(string)] = n.(map[string]any)["status"].(string)
	}
	if nodes["g0"] != "completed" || nodes["intake"] != "waiting_review" {
		t.Fatalf("nodes = %v", nodes)
	}
	b, _ := h.st.GetBinding(context.Background(), "acme-demo", simCase)
	if b == nil || b.Binding != store.PinnedOnFirstView || b.BoundBy != maya {
		t.Fatalf("binding = %+v", b)
	}
	// the legacy (workflow v2) case binds to the latest pack for its workflow key
	r = h.expect(h.do("GET", api+"/cases/"+legacy, maya, nil, nil), 200)
	if r.body["pack"].(map[string]any)["pack_key"] != "sim.tender" {
		t.Fatal("legacy case binding")
	}
	// no pack for the workflow: generic read-only shell + partial error
	r = h.expect(h.do("GET", api+"/cases/"+other, maya, nil, nil), 200)
	if r.body["pack"].(map[string]any)["status"] != "unavailable" || len(r.body["navigation"].([]any)) != 4 || !strings.Contains(r.raw, "pack_unavailable") {
		t.Fatalf("shell = %s", r.raw)
	}
	// a case the user cannot see in Flowcase: 404 and nothing bound
	h.expect(h.do("GET", api+"/cases/"+hidden, maya, nil, nil), 404)
	if b, _ := h.st.GetBinding(context.Background(), "acme-demo", hidden); b != nil {
		t.Fatal("binding created for an invisible case")
	}
	h.expect(h.do("GET", api+"/cases/not-a-uuid", maya, nil, nil), 404)
	// a later view keeps the pin even if the registry offered something else
	h.expect(h.do("GET", api+"/cases/"+simCase, maya, nil, nil), 200)
	if h.auditCount("case.pack_bound") != 2 {
		t.Fatalf("bound audit = %d", h.auditCount("case.pack_bound"))
	}

	// session inactive -> 401; Flowcase down -> list partial, detail 503
	h.expect(h.do("GET", api+"/cases", outside, nil, nil), 401)
	h.fc.mu.Lock()
	h.fc.down = true
	h.fc.mu.Unlock()
	r = h.expect(h.do("GET", api+"/cases", maya, nil, nil), 200)
	if len(r.body["items"].([]any)) != 0 || !strings.Contains(r.raw, "flowcase_unavailable") {
		t.Fatalf("down list = %s", r.raw)
	}
	r = h.expect(h.do("GET", api+"/cases/"+simCase, maya, nil, nil), 503)
	if r.errCode() != "flowcase_unavailable" {
		t.Fatal(r.raw)
	}
	r = h.expect(h.do("GET", api+"/me/context", maya, nil, nil), 200) // me/context still answers
	h.cp.mu.Lock()
	h.cp.down = true
	h.cp.mu.Unlock()
	r = h.expect(h.do("GET", api+"/cases", rafi, nil, nil), 200)
	if !strings.Contains(r.raw, "flowcase_unavailable") {
		t.Fatal("exchange outage must be a partial error")
	}
}

func TestDraftForwarding(t *testing.T) {
	h := newHarness(t, nil)
	caseA, draftA := h.fc.addCase(maya, "sim-tender", 3)
	_, draftB := h.fc.addCase(maya, "sim-tender", 3)
	p := api + "/cases/" + caseA + "/drafts/"

	h.expect(h.do("POST", p+draftA+"/submit", maya, map[string]any{}, nil), 428)
	r := h.expect(h.do("POST", p+draftA+"/submit", maya, map[string]any{}, map[string]string{"If-Match": `"9"`}), 412)
	if r.errCode() != "version_mismatch" {
		t.Fatal(r.raw)
	}
	r = h.expect(h.do("POST", p+draftA+"/revision", maya, map[string]any{"feedback": "no"}, map[string]string{"If-Match": `"1"`}), 400)
	if r.errCode() != "feedback_required" || !strings.Contains(r.raw, `"path":"feedback"`) {
		t.Fatalf("passthrough = %s", r.raw)
	}
	// draft of another case -> 404 without forwarding
	h.expect(h.do("POST", p+draftB+"/submit", maya, map[string]any{}, map[string]string{"If-Match": `"1"`}), 404)
	if h.fc.drafts[draftB]["status"] != "pending" {
		t.Fatal("foreign draft was forwarded")
	}
	r = h.expect(h.do("POST", p+draftA+"/submit", maya, map[string]any{"note": "ok"}, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "client-key-123"}), 200)
	if r.body["case"].(map[string]any)["id"] != caseA || h.fc.drafts[draftA]["status"] != "submitted" {
		t.Fatalf("submit = %s", r.raw)
	}
	if h.auditCount("draft.submit_forwarded") != 2 || h.auditCount("draft.revision_forwarded") != 1 {
		t.Fatal("draft audit events missing")
	}
	// another user cannot act on the case
	h.expect(h.do("POST", p+draftA+"/submit", rafi, map[string]any{}, map[string]string{"If-Match": `"2"`}), 404)
}

func TestPacksLibrary(t *testing.T) {
	h := newHarness(t, nil)
	r := h.expect(h.do("GET", api+"/packs", maya, nil, nil), 200)
	got := map[string]map[string]any{}
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		got[m["pack_key"].(string)+"@"+m["version"].(string)] = m
	}
	sim := got["sim.tender@1.3.0"]
	if sim["live"].(map[string]any)["startable"] != true || sim["has_intake"] != true || !strings.HasPrefix(sim["checksum"].(string), "sha256:") {
		t.Fatalf("sim = %v", sim)
	}
	for _, k := range []string{"legal.contract-review@1.0.0", "lab.poc-assessment@1.0.0", "sim.tender@1.0.0", "sim.tender@1.1.0", "sim.tender@1.2.0"} {
		l := got[k]["live"].(map[string]any)
		if l["startable"] != false || len(l["reasons"].([]any)) == 0 || got[k]["validation"].(map[string]any)["schema_ok"] != true {
			t.Fatalf("%s live = %v", k, l)
		}
	}
	r = h.expect(h.do("GET", api+"/packs/sim.tender/versions/1.1.0", maya, nil, nil), 200)
	if r.body["manifest"].(map[string]any)["intake"] == nil {
		t.Fatal("manifest missing")
	}
	h.expect(h.do("GET", api+"/packs/sim.tender/versions/7.0.0", maya, nil, nil), 404)
	// intake requires every Flowcase-required variable to be collected
	h.fc.mu.Lock()
	h.fc.required = append(h.fc.required, "budget_code")
	h.fc.mu.Unlock()
	r = h.expect(h.do("GET", api+"/packs", rafi, nil, nil), 200) // other user: not cached
	if !strings.Contains(r.raw, "budget_code") {
		t.Fatal("missing required variable not reported")
	}
}

func TestIntakeFlow(t *testing.T) {
	h := newHarness(t, nil)
	h.expect(h.do("POST", api+"/intake-sessions", rafi, map[string]any{"pack_key": "sim.tender"}, nil), 403)
	h.expect(h.do("POST", api+"/intake-sessions", maya, map[string]any{"pack_key": "legal.contract-review"}, nil), 409)
	h.expect(h.do("POST", api+"/intake-sessions", maya, map[string]any{"pack_key": "nope.pack"}, nil), 404)

	r := h.expect(h.do("POST", api+"/intake-sessions", maya, map[string]any{"pack_key": "sim.tender"}, nil), 201)
	id := r.body["id"].(string)
	s := api + "/intake-sessions/" + id
	if r.body["pack_version"] != "1.3.0" || len(r.body["fields"].([]any)) != 6 || len(r.body["starters"].([]any)) != 3 ||
		r.body["messages"].([]any)[0].(map[string]any)["content"] != r.body["greeting"] {
		t.Fatalf("session = %s", r.raw)
	}
	if strings.Join(toStrings(r.body["missing"]), ",") != "Case title,Customer name,Target date" {
		t.Fatalf("missing must be field labels: %v", r.body["missing"])
	}
	// owner only
	h.expect(h.do("GET", s, rafi, nil, nil), 404)

	r = h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "Ada KAK asisten AI untuk agen contact center"}, nil), 202)
	if r.body["pending"] != true {
		t.Fatal("not pending")
	}
	h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "again"}, nil), 409) // turn_in_progress
	req := h.as.requests[0]
	if req.IdempotencyKey != "ucw-intake-"+id+":1" || req.AgentID != "sim.case_intake" || req.DelegatedUser != maya || req.CorrelationID != "intake:"+id || req.SkillVersion != "" {
		t.Fatalf("run request = %+v", req)
	}
	var in map[string]any
	_ = json.Unmarshal(req.Input, &in)
	if in["case_summary"] == nil || len(in["conversation"].([]any)) != 2 {
		t.Fatalf("agent input = %s", req.Input)
	}

	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	if r.body["pending"] != false || r.body["draft"].(map[string]any)["title"] != "Asisten AI contact center" || r.body["kb_status"] != "partial" {
		t.Fatalf("after turn 1 = %s", r.raw)
	}
	last := r.body["messages"].([]any)[2].(map[string]any)
	if len(last["sources"].([]any)) != 1 || len(last["updated_fields"].([]any)) != 3 {
		t.Fatalf("assistant message = %v", last)
	}
	h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "CUST-1, OPP-1, akhir Januari"}, nil), 202)
	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	if r.body["ready"] != true || len(r.body["missing"].([]any)) != 0 {
		t.Fatalf("after turn 2 = %s", r.raw)
	}
	v := r.header.Get("ETag")

	// manual edit with If-Match
	r = h.expect(h.do("PUT", s+"/draft", maya, map[string]any{"draft": map[string]any{"title": "Asisten AI CC", "customer_name": "PT Contoh",
		"deadline": "2027-01-29", "constraints": []string{"Data di Indonesia"}}}, map[string]string{"If-Match": v}), 200)
	h.expect(h.do("PUT", s+"/draft", maya, map[string]any{"draft": map[string]any{"budget": "x"}}, map[string]string{"If-Match": r.header.Get("ETag")}), 422)
	h.expect(h.do("PUT", s+"/draft", maya, map[string]any{"draft": map[string]any{}}, map[string]string{"If-Match": v}), 412)
	v = r.header.Get("ETag")

	h.expect(h.do("POST", s+"/case", maya, map[string]any{}, map[string]string{"If-Match": v}), 400) // Idempotency-Key required
	r = h.expect(h.do("POST", s+"/case", maya, map[string]any{}, map[string]string{"If-Match": v, "Idempotency-Key": "create-0001"}), 201)
	caseID := r.body["case_id"].(string)
	if !strings.HasPrefix(r.body["reference"].(string), "CASE-") {
		t.Fatal(r.raw)
	}
	created := h.fc.created[0]
	if created.WorkflowKey != "sim-tender" || created.Version != 0 || created.Title != "Asisten AI CC" || created.Variables["deadline"] != "2027-01-29T00:00:00Z" {
		t.Fatalf("create input = %+v", created)
	}
	b, _ := h.st.GetBinding(context.Background(), "acme-demo", caseID)
	if b == nil || b.Binding != store.PinnedAtCreation || b.PackVersion != "1.3.0" {
		t.Fatalf("binding = %+v", b)
	}
	if len(h.fc.notes[caseID]) != 1 || !strings.Contains(h.fc.notes[caseID][0], "intake chat (2 messages)") {
		t.Fatalf("notes = %v", h.fc.notes[caseID])
	}
	// replay returns the same case; the session is closed for new messages
	r = h.expect(h.do("POST", s+"/case", maya, map[string]any{}, map[string]string{"If-Match": v, "Idempotency-Key": "create-0001"}), 201)
	if r.body["case_id"] != caseID || len(h.fc.created) != 1 {
		t.Fatal("replay created a second case")
	}
	h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "more"}, nil), 409)
	r = h.expect(h.do("GET", api+"/cases/"+caseID, maya, nil, nil), 200)
	if r.body["pack"].(map[string]any)["binding"] != "pinned_at_creation" {
		t.Fatal("case not pinned at creation")
	}
	for _, a := range []string{"intake.turn_started", "intake.turn_applied", "case.created_via_intake"} {
		if h.auditCount(a) == 0 {
			t.Errorf("audit %s missing", a)
		}
	}
	var meta string
	_ = h.st.DB.QueryRow(context.Background(), `SELECT metadata::text FROM workspace_audit_events WHERE action = 'intake.turn_started' LIMIT 1`).Scan(&meta)
	if strings.Contains(meta, "contact center") {
		t.Fatal("audit metadata must not contain message text")
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestIntakeFailuresAndLimits(t *testing.T) {
	h := newHarness(t, nil)
	open := func() string {
		r := h.expect(h.do("POST", api+"/intake-sessions", maya, map[string]any{"pack_key": "sim.tender"}, nil), 201)
		return api + "/intake-sessions/" + r.body["id"].(string)
	}
	// Agent Studio down: 503, message kept, last_error set
	s := open()
	h.as.mu.Lock()
	h.as.down = true
	h.as.mu.Unlock()
	r := h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "halo"}, nil), 503)
	if r.errCode() != "agent_studio_unavailable" {
		t.Fatal(r.raw)
	}
	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	msgs := r.body["messages"].([]any)
	if r.body["last_error"] == nil || r.body["pending"] != false || msgs[len(msgs)-1].(map[string]any)["content"] != "halo" {
		t.Fatalf("after outage = %s", r.raw)
	}
	h.as.mu.Lock()
	h.as.down = false
	h.as.mu.Unlock()
	h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "halo"}, nil), 202) // retry reuses the saved message
	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	if n := len(r.body["messages"].([]any)); n != 3 {
		t.Fatalf("retry duplicated the message: %d messages", n)
	}

	// checksum mismatch and agent mismatch are rejected
	for _, mode := range []string{"tamper", "agent"} {
		s := open()
		h.as.mu.Lock()
		h.as.tamper, h.as.wrongAgnt = mode == "tamper", mode == "agent"
		h.as.mu.Unlock()
		h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "halo"}, nil), 202)
		r := h.expect(h.do("GET", s, maya, nil, nil), 200)
		if r.body["last_error"] == nil || len(r.body["draft"].(map[string]any)) != 0 {
			t.Fatalf("%s accepted: %s", mode, r.raw)
		}
	}
	h.as.mu.Lock()
	h.as.tamper, h.as.wrongAgnt = false, false
	h.as.mu.Unlock()
	if h.auditCount("intake.turn_rejected") != 2 {
		t.Fatal("rejections not audited")
	}

	// 30 user turns
	s = open()
	id := s[strings.LastIndexByte(s, '/')+1:]
	var msgs30 []map[string]string
	for range 30 {
		msgs30 = append(msgs30, map[string]string{"role": "user", "content": "x", "at": "2026-10-31T00:00:00Z"}, map[string]string{"role": "assistant", "content": "y", "at": "2026-10-31T00:00:00Z"})
	}
	raw, _ := json.Marshal(msgs30)
	if _, err := h.st.DB.Exec(context.Background(), `UPDATE workspace_intake_sessions SET messages = $1 WHERE id = $2`, raw, id); err != nil {
		t.Fatal(err)
	}
	r = h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": "one more"}, nil), 409)
	if r.errCode() != "too_many_turns" {
		t.Fatal(r.raw)
	}
	h.expect(h.do("POST", s+"/messages", maya, map[string]any{"content": strings.Repeat("x", 4001)}, nil), 422)

	// Flowcase validation errors pass through as 422 with details
	s = open()
	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	r = h.expect(h.do("PUT", s+"/draft", maya, map[string]any{"draft": map[string]any{"title": "Only a title"}}, map[string]string{"If-Match": r.header.Get("ETag")}), 200)
	r = h.expect(h.do("POST", s+"/case", maya, map[string]any{}, map[string]string{"If-Match": r.header.Get("ETag"), "Idempotency-Key": "create-0002"}), 422)
	if r.errCode() != "invalid_input" || !strings.Contains(r.raw, "/variables/") {
		t.Fatalf("validation passthrough = %s", r.raw)
	}
}

func TestPackChecksumDrift(t *testing.T) {
	h := newHarness(t, func(st *store.Store) {
		// a stored sim.tender 1.3.0 whose content differs from the file
		if err := st.InsertPackVersion(context.Background(), packs.StoredVersion{PackKey: "sim.tender", Version: "1.3.0",
			Checksum: "sha256:" + strings.Repeat("ab", 32), Manifest: json.RawMessage(`{"pack_key":"sim.tender"}`), Status: "published", SourceFile: "old.json"}); err != nil {
			t.Fatal(err)
		}
	})
	p := h.reg.Get("sim.tender", "1.3.0")
	if p == nil || p.Valid() {
		t.Fatal("drifted pack must be invalid")
	}
	var checksum string
	_ = h.st.DB.QueryRow(context.Background(), `SELECT checksum FROM workspace_pack_versions WHERE pack_key = 'sim.tender' AND version = '1.3.0'`).Scan(&checksum)
	if checksum != "sha256:"+strings.Repeat("ab", 32) {
		t.Fatal("stored version was overwritten")
	}
	if _, err := h.st.DB.Exec(context.Background(), `UPDATE workspace_pack_versions SET status = 'retired'`); err == nil {
		t.Fatal("pack versions must be immutable")
	}
	r := h.expect(h.do("GET", api+"/packs", maya, nil, nil), 200)
	if !strings.Contains(r.raw, "checksum_drift") {
		t.Fatal("drift not reported")
	}
	// the other packs still load
	if !h.reg.Get("legal.contract-review", "1.0.0").Valid() {
		t.Fatal("other packs must stay valid")
	}
	// the drifted 1.3.0 never starts an intake: new sessions fall back to the newest valid version (1.2.0)
	if r := h.expect(h.do("POST", api+"/intake-sessions", maya, map[string]any{"pack_key": "sim.tender"}, nil), 201); r.body["pack_version"] != "1.2.0" {
		t.Fatalf("intake pack = %v", r.body["pack_version"])
	}
	// cases of the workflow bind to the newest *valid* version (1.2.0), never the drifted one
	caseID, _ := h.fc.addCase(maya, "sim-tender", 3)
	r = h.expect(h.do("GET", api+"/cases/"+caseID, maya, nil, nil), 200)
	if r.body["pack"].(map[string]any)["version"] != "1.2.0" {
		t.Fatalf("pack = %v", r.body["pack"])
	}
	if h.auditCount("pack.invalid") == 0 {
		t.Fatal("pack.invalid not audited")
	}
}

func TestIntakeDocuments(t *testing.T) {
	h := newHarness(t, nil)
	h.dl.pendingN = 1
	h.as.rfpUpdate = true
	r := h.expect(h.do("POST", api+"/intake-sessions", maya, map[string]any{"pack_key": "sim.tender"}, nil), 201)
	id := r.body["id"].(string)
	s := api + "/intake-sessions/" + id
	up := r.body["uploads"].(map[string]any)
	if up["max_files"] != 3.0 || len(r.body["documents"].([]any)) != 0 || !strings.Contains(strings.Join(toStrings(up["accept"]), ","), ".pdf") {
		t.Fatalf("uploads = %s", r.raw)
	}

	// content decides the type; hostile names are reduced to a base name
	if r = h.upload(s+"/documents", maya, "setup.exe", []byte("MZ\x90\x00")); r.status != 415 || r.errCode() != "unsupported_document" {
		t.Fatalf("exe upload = %d %s", r.status, r.raw)
	}
	h.expect(h.upload(s+"/documents", rafi, "kak.pdf", []byte("%PDF-1.7 x")), 404) // owner only
	kak := []byte("%PDF-1.7 KAK pengadaan server CCTV")
	r = h.expect(h.upload(s+"/documents", maya, "../../RKST Server CCTV", kak), 202)
	docs := r.body["documents"].([]any)
	doc := docs[0].(map[string]any)
	if len(docs) != 1 || doc["status"] != "processing" || doc["filename"] != "RKST Server CCTV" || doc["media_type"] != "application/pdf" || doc["content"] != nil {
		t.Fatalf("after upload = %s", r.raw)
	}
	if h.dl.tasks["task-00000001"].filename != "RKST Server CCTV.pdf" || !strings.Contains(strings.Join(h.dl.submitted[0]["ocr_lang"], ","), "id") ||
		h.dl.submitted[0]["do_ocr"][0] != "true" || h.dl.submitted[0]["image_export_mode"][0] != "placeholder" {
		t.Fatalf("docling submit = %+v %+v", h.dl.tasks["task-00000001"], h.dl.submitted[0])
	}
	// while the document is read: no second upload, no message
	h.expect(h.upload(s+"/documents", maya, "lain.txt", []byte("lain")), 409)
	if r = h.do("POST", s+"/messages", maya, map[string]any{"content": "halo"}, nil); r.errCode() != "document_processing" {
		t.Fatalf("send while processing = %s", r.raw)
	}
	r = h.expect(h.do("GET", s, maya, nil, nil), 200) // poll 1: still converting
	if r.body["documents"].([]any)[0].(map[string]any)["status"] != "processing" || len(h.as.requests) != 0 {
		t.Fatalf("poll 1 = %s", r.raw)
	}

	// poll 2: converted -> the text fills rfp_excerpt and the pack prompt starts a turn
	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	doc = r.body["documents"].([]any)[0].(map[string]any)
	rfp, _ := r.body["draft"].(map[string]any)["rfp_excerpt"].(string)
	if doc["status"] != "ready" || doc["chars"].(float64) < 50 || !strings.HasPrefix(rfp, "# RKST Server CCTV\n\n## RENCANA KERJA") ||
		strings.Contains(rfp, "<!-- image -->") || !strings.Contains(rfp, "server & storage CCTV") {
		t.Fatalf("after conversion = %s", r.raw)
	}
	// the same poll also applied the agent's answer (the fake run finishes at once)
	msgs := r.body["messages"].([]any)
	sent := msgs[len(msgs)-2].(map[string]any)
	if sent["role"] != "user" || sent["document"].(map[string]any)["filename"] != "RKST Server CCTV" ||
		!strings.Contains(sent["content"].(string), `"RKST Server CCTV"`) || msgs[len(msgs)-1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("document turn = %s", r.raw)
	}
	var in map[string]any
	_ = json.Unmarshal(h.as.requests[0].Input, &in)
	conv := in["conversation"].([]any)
	if len(h.as.requests) != 1 || !strings.Contains(in["draft"].(map[string]any)["rfp_excerpt"].(string), "310 kamera") ||
		!strings.Contains(conv[len(conv)-1].(map[string]any)["content"].(string), "rfp_excerpt") {
		t.Fatalf("agent input = %s", h.as.requests[0].Input)
	}
	// the agent's answer applied, but it cannot overwrite the uploaded text
	if r.body["pending"] != false || r.body["draft"].(map[string]any)["title"] != "Asisten AI contact center" {
		t.Fatalf("after turn = %s", r.raw)
	}
	if r = h.upload(s+"/documents", maya, "copy.pdf", kak); r.errCode() != "duplicate_document" {
		t.Fatalf("duplicate = %s", r.raw)
	}

	// a text file is ready at once and appended under its own heading
	r = h.expect(h.upload(s+"/documents", maya, "catatan.md", []byte("Garansi pemeliharaan 1 tahun.")), 202)
	rfp, _ = r.body["draft"].(map[string]any)["rfp_excerpt"].(string)
	if !strings.Contains(rfp, "# RKST Server CCTV") || !strings.HasSuffix(rfp, "# catatan.md\n\nGaransi pemeliharaan 1 tahun.") || r.body["pending"] != true {
		t.Fatalf("text upload = %s", r.raw)
	}
	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	first := r.body["documents"].([]any)[0].(map[string]any)["id"].(string)

	// a document Docling cannot read fails without a turn
	turns := len(h.as.requests)
	h.expect(h.upload(s+"/documents", maya, "rusak.pdf", []byte("%PDF-FAIL")), 202)
	h.expect(h.do("GET", s, maya, nil, nil), 200)
	r = h.expect(h.do("GET", s, maya, nil, nil), 200)
	failed := r.body["documents"].([]any)[2].(map[string]any)
	if failed["status"] != "failed" || failed["error"] == nil || len(h.as.requests) != turns {
		t.Fatalf("failed doc = %s", r.raw)
	}
	// failed documents do not count against max_files; removing the first leaves only the note
	r = h.expect(h.do("DELETE", s+"/documents/"+first, maya, nil, nil), 200)
	rfp, _ = r.body["draft"].(map[string]any)["rfp_excerpt"].(string)
	if rfp != "# catatan.md\n\nGaransi pemeliharaan 1 tahun." || len(r.body["documents"].([]any)) != 2 {
		t.Fatalf("after remove = %s", r.raw)
	}
	h.expect(h.do("DELETE", s+"/documents/"+first, maya, nil, nil), 404)

	// the case carries the document text as the rfp_excerpt variable
	r = h.expect(h.do("PUT", s+"/draft", maya, map[string]any{"draft": map[string]any{"title": "Server CCTV", "customer_name": "Bandara Contoh",
		"deadline": "2026-12-01", "rfp_excerpt": rfp}}, map[string]string{"If-Match": r.header.Get("ETag")}), 200)
	h.expect(h.do("POST", s+"/case", maya, map[string]any{}, map[string]string{"If-Match": r.header.Get("ETag"), "Idempotency-Key": "create-doc-0001"}), 201)
	if v, _ := h.fc.created[0].Variables["rfp_excerpt"].(string); !strings.Contains(v, "Garansi pemeliharaan") {
		t.Fatalf("case variables = %+v", h.fc.created[0].Variables)
	}
	h.expect(h.upload(s+"/documents", maya, "late.txt", []byte("late")), 409) // session closed

	for _, a := range []string{"intake.document_uploaded", "intake.document_ready", "intake.document_failed", "intake.document_removed"} {
		if h.auditCount(a) == 0 {
			t.Errorf("audit %s missing", a)
		}
	}
	var meta string
	_ = h.st.DB.QueryRow(context.Background(), `SELECT string_agg(metadata::text, ' ') FROM workspace_audit_events WHERE action LIKE 'intake.document_%'`).Scan(&meta)
	if strings.Contains(meta, "310 kamera") || strings.Contains(meta, "Garansi") || strings.Contains(meta, "RKST") {
		t.Fatalf("audit metadata must not contain document text or names: %s", meta)
	}
}
