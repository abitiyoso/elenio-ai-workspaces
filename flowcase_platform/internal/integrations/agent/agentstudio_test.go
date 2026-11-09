package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	testToken    = "as-service-token-0123456789abcdefghijklmnop"
	testSecret   = "callback-secret-0123456789abcdefghij"
	testOwner    = "11111111-2222-4333-8444-555555555555"
	testSubject  = "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	testCase     = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	testExecKey  = "0b6e1f9a-1111-4222-8333-944444444444#2"
	testAgentKey = "sim.solution_matching"
)

// simInputKeys is the Agent Studio SIM input_schema (additionalProperties: false).
var simInputKeys = map[string]bool{"case_summary": true, "rfp_excerpt": true, "customer": true, "upstream_artifacts": true, "constraints": true}

// fakeStudio mimics Agent Studio's /api/v1/runs contract, rejecting unknown fields like the real one.
type fakeStudio struct {
	t        *testing.T
	mu       sync.Mutex
	bodies   [][]byte
	auth     []string
	status   int // forced status for every call when non-zero
	errBody  string
	output   json.RawMessage
	runState RunStatus
	cancels  []string
}

type studioRunBody struct {
	IdempotencyKey  string          `json:"idempotency_key"`
	AgentID         string          `json:"agent_id"`
	SkillVersion    string          `json:"skill_version"`
	Input           json.RawMessage `json:"input"`
	Evidence        []Evidence      `json:"evidence"`
	CaseID          string          `json:"case_id"`
	CorrelationID   string          `json:"correlation_id"`
	DeadlineSeconds int             `json:"deadline_seconds"`
	DelegatedUser   string          `json:"delegated_user"`
}

func (f *fakeStudio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	if r.URL.Path == "/health/ready" {
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing or invalid bearer token"}`))
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.errBody))
		return
	}
	write := func(status int, run Run) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(run)
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/runs":
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(r.Body)
		f.bodies = append(f.bodies, raw.Bytes())
		dec := json.NewDecoder(bytes.NewReader(raw.Bytes()))
		dec.DisallowUnknownFields()
		var b studioRunBody
		if err := dec.Decode(&b); err != nil || !asIdempotencyPattern.MatchString(b.IdempotencyKey) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"request body must be one valid JSON object"}`))
			return
		}
		write(http.StatusAccepted, Run{RunID: "run-0001", Status: RunQueued, AgentID: b.AgentID, SkillVersion: "1.4.0", TraceIDs: []string{"run-0001"}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/runs/run-0001":
		run := Run{RunID: "run-0001", Status: f.runState, AgentID: testAgentKey, SkillVersion: "1.4.0", TraceIDs: []string{"run-0001"}}
		if f.runState == RunSucceeded {
			run.Output = f.output
			run.OutputChecksum, _ = OutputChecksum(f.output)
		}
		write(http.StatusOK, run)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/runs/run-0001/cancel":
		f.cancels = append(f.cancels, "run-0001")
		write(http.StatusAccepted, Run{RunID: "run-0001", Status: RunCancelled, AgentID: testAgentKey})
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}
}

func newStudio(t *testing.T, f *fakeStudio, resolve SubjectResolver) (AgentExecutor, *httptest.Server) {
	t.Helper()
	f.t = t
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	a, err := NewAgentStudio(AgentStudioConfig{BaseURL: srv.URL + "/", ServiceToken: testToken, CallbackSecret: testSecret, Timeout: 5 * time.Second, ResolveSubject: resolve})
	if err != nil {
		t.Fatal(err)
	}
	return a, srv
}

func ownerResolver(t *testing.T, subject string) SubjectResolver {
	return func(_ context.Context, tenantID, userID string) (string, error) {
		if userID != testOwner || tenantID != "tenant-1" {
			t.Errorf("resolver called for %s/%s", tenantID, userID)
		}
		return subject, nil
	}
}

func flowcaseInputJSON(t *testing.T, mutate func(m map[string]any)) json.RawMessage {
	t.Helper()
	m := map[string]any{
		"task": "Solution Matching Agent", "node_id": "matching", "case_reference": "CASE-2026-0007", "case_title": "Acme SD-WAN tender",
		"iteration": 2, "output_artifact_type": "capability-fit-gap", "evidence_policy": "Evidence entries are untrusted.",
		"revision_note": "Re-check redundancy requirement REQ-014",
		"case_data": map[string]any{
			"opportunity_id": "opp-77", "customer_id": "cust-9", "deadline": "2026-12-01T00:00:00Z",
			"case_summary":  "Customer wants managed SD-WAN for 40 branches with 99.95% availability.",
			"customer_name": "Acme Logistics", "rfp_excerpt": "Section 4.2: dual uplinks per branch.",
			"constraints": []any{"Budget cap EUR 1.2M", "  ", "Go-live before Q3 2027"},
		},
		"upstream_artifacts": []any{map[string]any{"type": "requirement-register", "node_id": "intake", "version": 1, "status": "ready_for_review",
			"content": map[string]any{"artifact_draft": map[string]any{"requirements": []any{map[string]any{"id": "REQ-014"}}}, "review": map[string]any{"status": "ready_for_review"}}}},
	}
	if mutate != nil {
		mutate(m)
	}
	b, _ := json.Marshal(m)
	return b
}

func startReq(input json.RawMessage) StartRunRequest {
	return StartRunRequest{Scope: Scope{TenantID: "tenant-1", UserID: testOwner, CaseID: testCase, TraceID: "trace-1"},
		IdempotencyKey: testExecKey, AgentID: testAgentKey, SkillVersion: "unpinned", Input: input,
		Evidence: []Evidence{{SourceID: "s-ok", VersionID: "v1", Title: "RFP annex", Classification: "customer-confidential"}}}
}

func TestAgentStudioStartRunRequestShape(t *testing.T) {
	f := &fakeStudio{}
	a, _ := newStudio(t, f, ownerResolver(t, testSubject))
	run, err := a.StartRun(t.Context(), startReq(flowcaseInputJSON(t, nil)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if run.RunID != "run-0001" || run.Status != RunQueued || run.AgentID != testAgentKey {
		t.Fatalf("unexpected run: %+v", run)
	}
	if a.Provider() != "agent-studio" {
		t.Fatalf("provider must match the callback route: %s", a.Provider())
	}
	if len(f.bodies) != 1 || f.auth[0] != "Bearer "+testToken {
		t.Fatalf("expected one authenticated call, got %d (%v)", len(f.bodies), len(f.auth))
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(f.bodies[0], &body); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range body {
		keys = append(keys, k)
	}
	want := map[string]bool{"idempotency_key": true, "agent_id": true, "skill_version": true, "input": true, "evidence": true, "correlation_id": true, "delegated_user": true}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected top-level field %q (case_id must never be sent)", k)
		}
	}
	if len(keys) != len(want) {
		t.Errorf("fields sent: %v", keys)
	}
	str := func(k string) string { var s string; _ = json.Unmarshal(body[k], &s); return s }
	if got := str("idempotency_key"); got != strings.ReplaceAll(testExecKey, "#", ":") {
		t.Errorf("idempotency key %q must use ':'", got)
	}
	if str("skill_version") != "" || str("agent_id") != testAgentKey || str("correlation_id") != testCase || str("delegated_user") != testSubject {
		t.Errorf("mapping wrong: skill=%q agent=%q corr=%q user=%q", str("skill_version"), str("agent_id"), str("correlation_id"), str("delegated_user"))
	}
	var ev []map[string]string
	_ = json.Unmarshal(body["evidence"], &ev)
	if len(ev) != 1 || ev[0]["source_id"] != "s-ok" || len(ev[0]) != 4 {
		t.Errorf("evidence must be the reference list only: %v", ev)
	}

	var input map[string]any
	_ = json.Unmarshal(body["input"], &input)
	for k := range input {
		if !simInputKeys[k] {
			t.Errorf("input field %q is not declared by the SIM input_schema", k)
		}
	}
	summary, _ := input["case_summary"].(string)
	for _, part := range []string{"Acme SD-WAN tender", "CASE-2026-0007", "Solution Matching Agent", "iteration 2", "REQ-014",
		"managed SD-WAN for 40 branches", "opportunity_id: opp-77", "deadline: 2026-12-01T00:00:00Z", "requirement-register v1"} {
		if !strings.Contains(summary, part) {
			t.Errorf("case_summary lacks %q:\n%s", part, summary)
		}
	}
	if strings.Contains(summary, "untrusted") || utf8.RuneCountInString(summary) > 4000 {
		t.Errorf("summary must not carry Flowcase-internal fields and stay <= 4000 chars")
	}
	if input["customer"] != "Acme Logistics" || input["rfp_excerpt"] != "Section 4.2: dual uplinks per branch." {
		t.Errorf("customer/rfp mapping: %v %v", input["customer"], input["rfp_excerpt"])
	}
	if c, _ := input["constraints"].([]any); len(c) != 2 || c[0] != "Budget cap EUR 1.2M" {
		t.Errorf("constraints: %v", input["constraints"])
	}
	ups, _ := input["upstream_artifacts"].(map[string]any)
	reg, _ := ups["requirement-register"].(map[string]any)
	content, _ := reg["content"].(map[string]any)
	if reg["producer_node"] != "intake" || content["requirements"] == nil || content["review"] != nil {
		t.Errorf("upstream artifacts must carry the prior draft without the runtime envelope: %v", ups)
	}
}

func TestAgentStudioOptionalFieldsAndDelegation(t *testing.T) {
	minimal := flowcaseInputJSON(t, func(m map[string]any) {
		m["case_data"] = map[string]any{"customer_id": "cust-9"}
		delete(m, "upstream_artifacts")
		delete(m, "revision_note")
		m["case_title"] = ""
	})
	for name, subject := range map[string]string{"non-uuid subject": "sub-ada", "unknown user": ""} {
		f := &fakeStudio{}
		a, _ := newStudio(t, f, ownerResolver(t, subject))
		req := startReq(minimal)
		req.SkillVersion = "1.4.0"
		req.IdempotencyKey = "0b6e1f9a-1111-4222-8333-944444444444:1"
		if _, err := a.StartRun(t.Context(), req); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var body map[string]any
		_ = json.Unmarshal(f.bodies[0], &body)
		if _, ok := body["delegated_user"]; ok {
			t.Errorf("%s: delegated_user must only be sent for a UUID subject: %v", name, body["delegated_user"])
		}
		if body["skill_version"] != "1.4.0" || body["idempotency_key"] != req.IdempotencyKey {
			t.Errorf("%s: pinned skill and new-format keys pass through: %v", name, body)
		}
		input, _ := body["input"].(map[string]any)
		if len(input) != 2 || input["customer"] != "cust-9" || input["case_summary"] == "" {
			t.Errorf("%s: only declared, non-empty fields are sent: %v", name, input)
		}
	}
}

func TestMapSIMInputBounds(t *testing.T) {
	long := strings.Repeat("x", 30000)
	var many []any
	for range 80 {
		many = append(many, strings.Repeat("c", 900))
	}
	raw := flowcaseInputJSON(t, func(m map[string]any) {
		m["case_data"] = map[string]any{"case_summary": strings.Repeat("b", 4000), "rfp_excerpt": long, "customer_name": long, "constraints": many,
			"notes": map[string]any{"deep": long}}
		m["revision_note"] = long
	})
	out, err := MapSIMInput(raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	var in map[string]any
	_ = json.Unmarshal(out, &in)
	if n := utf8.RuneCountInString(in["case_summary"].(string)); n > 4000 || n < 1 {
		t.Errorf("case_summary length %d", n)
	}
	if n := utf8.RuneCountInString(in["rfp_excerpt"].(string)); n > 20000 {
		t.Errorf("rfp_excerpt length %d", n)
	}
	if n := utf8.RuneCountInString(in["customer"].(string)); n > 200 {
		t.Errorf("customer length %d", n)
	}
	cs := in["constraints"].([]any)
	if len(cs) != 50 || utf8.RuneCountInString(cs[0].(string)) > 500 {
		t.Errorf("constraints bounds: %d items", len(cs))
	}
	shrunk, _ := MapSIMInput(raw, 2)
	if strings.Contains(string(shrunk), "REQ-014") || !strings.Contains(string(shrunk), "content_omitted") {
		t.Errorf("shrink level 2 must drop upstream content: %s", shrunk)
	}
}

func TestAgentStudioOversizedRequestShrinksUpstream(t *testing.T) {
	big := strings.Repeat("z", 250<<10)
	raw := flowcaseInputJSON(t, func(m map[string]any) {
		m["upstream_artifacts"] = []any{map[string]any{"type": "hld", "node_id": "design", "version": 1, "status": "ready_for_review", "content": map[string]any{"text": big}}}
	})
	f := &fakeStudio{}
	a, _ := newStudio(t, f, nil)
	if _, err := a.StartRun(t.Context(), startReq(raw)); err != nil {
		t.Fatal(err)
	}
	if n := len(f.bodies[0]); n > MaxAgentStudioRequest {
		t.Fatalf("request is %d bytes", n)
	}
	if !strings.Contains(string(f.bodies[0]), `"truncated":true`) {
		t.Fatal("oversized upstream content must be reduced to an excerpt")
	}
}

func TestAgentStudioGetAndCancel(t *testing.T) {
	f := &fakeStudio{runState: RunRunning}
	a, _ := newStudio(t, f, nil)
	run, err := a.GetRun(t.Context(), Scope{}, "run-0001")
	if err != nil || run.Status != RunRunning || run.Status.Terminal() {
		t.Fatalf("running: %+v %v", run, err)
	}
	f.runState, f.output = RunSucceeded, json.RawMessage(`{"artifact_draft":{"fit":[1,2]},"review":{"status":"ready_for_review"}}`)
	run, err = a.GetRun(t.Context(), Scope{}, "run-0001")
	sum, _ := OutputChecksum(run.Output)
	if err != nil || run.Status != RunSucceeded || run.AgentID != testAgentKey || sum != run.OutputChecksum {
		t.Fatalf("succeeded run must reconcile: %+v %v", run, err)
	}
	if err := a.CancelRun(t.Context(), Scope{}, "run-0001"); err != nil || len(f.cancels) != 1 {
		t.Fatalf("cancel: %v %v", err, f.cancels)
	}
	if _, err := a.GetRun(t.Context(), Scope{}, "run-missing"); !errors.Is(err, ErrUnknownRun) || !errors.Is(err, ErrRejected) {
		t.Fatalf("404 must map to ErrUnknownRun: %v", err)
	}
	if _, err := a.GetRun(t.Context(), Scope{}, "../admin"); !errors.Is(err, ErrUnknownRun) {
		t.Fatalf("invalid run ids must not reach the network: %v", err)
	}
	for _, auth := range f.auth {
		if auth != "Bearer "+testToken {
			t.Fatalf("every call must carry the service token, got %q", auth)
		}
	}
}

func TestAgentStudioErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		status    int
		body      string
		retryable bool
		msg       string
	}{
		{400, `{"error":"idempotency_key, agent_id and an input object are required"}`, false, "input object are required"},
		{401, `{"error":"missing or invalid bearer token"}`, false, "bearer token"},
		{404, `{"error":"agent not found"}`, false, "agent not found"},
		{409, `{"error":"evaluation gate did not pass for this version"}`, false, "evaluation gate"},
		{422, `{"message":"input does not match the agent's input_schema"}`, false, "input_schema"},
		{429, `{"error":"rate limited"}`, true, "rate limited"},
		{500, `{"error":"request could not be completed"}`, true, "could not be completed"},
		{503, `upstream down`, true, "Service Unavailable"},
	} {
		f := &fakeStudio{status: tc.status, errBody: tc.body}
		a, _ := newStudio(t, f, nil)
		_, err := a.StartRun(t.Context(), startReq(flowcaseInputJSON(t, nil)))
		var rt *RuntimeError
		if !errors.As(err, &rt) || rt.Status != tc.status || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%d: unexpected error %v", tc.status, err)
			continue
		}
		if errors.Is(err, ErrUnavailable) != tc.retryable || errors.Is(err, ErrRejected) == tc.retryable {
			t.Errorf("%d: retryable=%v, got %v", tc.status, tc.retryable, err)
		}
		if errors.Is(err, ErrUnknownRun) {
			t.Errorf("%d: StartRun errors are never 'unknown run'", tc.status)
		}
		if strings.Contains(err.Error(), testToken) {
			t.Errorf("%d: error leaks the token", tc.status)
		}
	}
	// Network failure is retryable.
	a, srv := newStudio(t, &fakeStudio{}, nil)
	srv.Close()
	if _, err := a.StartRun(t.Context(), startReq(flowcaseInputJSON(t, nil))); !errors.Is(err, ErrUnavailable) {
		t.Errorf("network failure must be retryable: %v", err)
	}
	// A resolver outage is retryable, never silently dropped.
	b, _ := newStudio(t, &fakeStudio{}, func(context.Context, string, string) (string, error) { return "", errors.New("db down") })
	if _, err := b.StartRun(t.Context(), startReq(flowcaseInputJSON(t, nil))); !errors.Is(err, ErrUnavailable) {
		t.Errorf("resolver failure must be retryable: %v", err)
	}
	// A key Agent Studio would refuse is a terminal, local rejection.
	c, _ := newStudio(t, &fakeStudio{}, nil)
	bad := startReq(flowcaseInputJSON(t, nil))
	bad.IdempotencyKey = "short"
	if _, err := c.StartRun(t.Context(), bad); !errors.Is(err, ErrRejected) {
		t.Errorf("invalid key must be rejected: %v", err)
	}
}

func TestAgentStudioHealthAndCallbacks(t *testing.T) {
	f := &fakeStudio{}
	a, _ := newStudio(t, f, nil)
	if h := a.Health(t.Context()); h.Mode != "agent_studio" || h.Status != "ok" {
		t.Fatalf("health: %+v", h)
	}
	f.status = http.StatusServiceUnavailable
	if h := a.Health(t.Context()); h.Status != "unavailable" || strings.Contains(h.Message, testToken) {
		t.Fatalf("health when not ready: %+v", h)
	}
	now := time.Now()
	out := json.RawMessage(`{"artifact_draft":{"ok":true}}`)
	sum, _ := OutputChecksum(out)
	body, _ := json.Marshal(map[string]any{"message_id": "m-1", "audience": Audience, "tenant_id": "t-1",
		"run": map[string]any{"run_id": "run-0001", "status": "succeeded", "agent_id": testAgentKey, "output": out, "output_checksum": sum}})
	res, err := a.VerifyResult(t.Context(), CallbackEnvelope{Provider: ProviderAgentStudio, Headers: SignCallback([]byte(testSecret), now, body), Body: body})
	if err != nil || res.Run.RunID != "run-0001" {
		t.Fatalf("valid Agent Studio callback rejected: %v", err)
	}
	if _, err := a.VerifyResult(t.Context(), CallbackEnvelope{Headers: SignCallback([]byte("another-secret-0123456789abcdefghij"), now, body), Body: body}); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("wrongly signed callback accepted: %v", err)
	}
}

func TestNewAgentStudioRejectsBadConfig(t *testing.T) {
	for name, cfg := range map[string]AgentStudioConfig{
		"no url":        {ServiceToken: testToken, CallbackSecret: testSecret},
		"ftp url":       {BaseURL: "ftp://studio", ServiceToken: testToken, CallbackSecret: testSecret},
		"short token":   {BaseURL: "https://studio", ServiceToken: "short", CallbackSecret: testSecret},
		"no secret":     {BaseURL: "https://studio", ServiceToken: testToken},
		"token with \n": {BaseURL: "https://studio", ServiceToken: testToken + "\nX-Evil: 1", CallbackSecret: testSecret},
	} {
		if _, err := NewAgentStudio(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
