package cases_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elenio/flowcase/internal/app"
	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/workflows/catalog"
)

const studioToken = "as-service-token-0123456789abcdefghijklmnop"

// studioServer is a minimal Agent Studio: strict request decoding (unknown fields rejected), runs
// report "running" on the first poll and "succeeded" on the next, with Agent Studio's output envelope.
type studioServer struct {
	mu       sync.Mutex
	requests []map[string]json.RawMessage
	runs     map[string]string // run id -> agent key
	polls    map[string]int
}

func (s *studioServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+studioToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	reply := func(status int, run agent.Run) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(run)
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/runs":
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(r.Body)
		var body struct {
			IdempotencyKey  string          `json:"idempotency_key"`
			AgentID         string          `json:"agent_id"`
			SkillVersion    string          `json:"skill_version"`
			Input           json.RawMessage `json:"input"`
			Evidence        json.RawMessage `json:"evidence"`
			CaseID          string          `json:"case_id"`
			CorrelationID   string          `json:"correlation_id"`
			DeadlineSeconds int             `json:"deadline_seconds"`
			DelegatedUser   string          `json:"delegated_user"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || !regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`).MatchString(body.IdempotencyKey) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"request body must be one valid JSON object"}`))
			return
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw.Bytes(), &m)
		s.requests = append(s.requests, m)
		id := fmt.Sprintf("as-run-%03d", len(s.requests))
		s.runs[id] = body.AgentID
		reply(http.StatusAccepted, agent.Run{RunID: id, Status: agent.RunQueued, AgentID: body.AgentID, SkillVersion: "1.0.0"})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/runs/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
		key, ok := s.runs[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		s.polls[id]++
		if s.polls[id] == 1 {
			reply(http.StatusOK, agent.Run{RunID: id, Status: agent.RunRunning, AgentID: key, SkillVersion: "1.0.0"})
			return
		}
		out, _ := json.Marshal(map[string]any{"artifact_draft": map[string]any{"agent": key, "items": []any{map[string]any{"id": key + "-1"}}},
			"review": map[string]any{"status": "ready_for_review"}})
		sum, _ := agent.OutputChecksum(out)
		reply(http.StatusOK, agent.Run{RunID: id, Status: agent.RunSucceeded, AgentID: key, SkillVersion: "1.0.0", Output: out, OutputChecksum: sum, TraceIDs: []string{id},
			UsageObservation: &agent.UsageObservation{Schema: "elenio.agent-usage.v1", Class: "test", Coverage: "partial", TokensIn: 120, TokensOut: 24, Attempts: 2, ObservedAttempts: 1, PricingStatus: "unpriced"}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestSIMCaseRunsThroughAgentStudioClient(t *testing.T) {
	e := newEnv(t)
	bindings, err := packs.LoadBindings("../../config/packs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := bindings.Apply(e.eng.Catalog.(*catalog.Static)); err != nil {
		t.Fatal(err)
	}
	studio := &studioServer{runs: map[string]string{}, polls: map[string]int{}}
	srv := httptest.NewServer(studio)
	defer srv.Close()
	client, err := agent.NewAgentStudio(agent.AgentStudioConfig{BaseURL: srv.URL, ServiceToken: studioToken,
		CallbackSecret: string(secret), Timeout: 5 * time.Second, ResolveSubject: app.DirectorySubject(e.pool)})
	if err != nil {
		t.Fatal(err)
	}
	e.phase3(client)
	cpUser := "3f2504e0-4f89-41d3-9a0c-0305e82c3301" // Control Plane user id held as the directory subject
	e.exec(`UPDATE users SET subject = $2 WHERE id = $1`, e.owner.UserID, cpUser)

	vars := map[string]any{"opportunity_id": "opp-1", "customer_id": "cust-1", "deadline": "2026-12-01T00:00:00Z", "kms_source_ids": []any{"s-ok"},
		"case_summary": "Managed SD-WAN for 40 branches, dual uplinks, 99.95% availability.", "customer_name": "Acme Logistics",
		"rfp_excerpt": "4.2 Each branch needs two independent uplinks.", "constraints": []any{"Budget cap EUR 1.2M"}}
	id := e.createCase("sim-tender", vars)
	d := e.advance(id, "")
	if d.Status != cases.CaseCompleted || len(d.Artifacts) != 6 {
		t.Fatalf("SIM case must complete through Agent Studio with 6 artifacts: %s, %d", d.Status, len(d.Artifacts))
	}
	if d.WorkflowVersion != 4 {
		t.Fatalf("new cases start on the published v4 (author draft review, optional hidden IDs), got v%d", d.WorkflowVersion)
	}
	if len(d.Drafts) != 6 {
		t.Fatalf("each of the six agent drafts is reviewed by the owner once, got %d reviews", len(d.Drafts))
	}

	studio.mu.Lock()
	defer studio.mu.Unlock()
	if len(studio.requests) != 6 {
		t.Fatalf("expected 6 runs, got %d", len(studio.requests))
	}
	allowed := map[string]bool{"case_summary": true, "rfp_excerpt": true, "customer": true, "upstream_artifacts": true, "constraints": true}
	byAgent := map[string]map[string]any{}
	for _, req := range studio.requests {
		str := func(k string) string { var s string; _ = json.Unmarshal(req[k], &s); return s }
		if _, sent := req["case_id"]; sent || str("correlation_id") != id || str("skill_version") != "" || str("delegated_user") != cpUser {
			t.Fatalf("request mapping: case_id sent=%v corr=%q skill=%q user=%q", sent, str("correlation_id"), str("skill_version"), str("delegated_user"))
		}
		if strings.Contains(str("idempotency_key"), "#") || !strings.HasSuffix(str("idempotency_key"), ":1") {
			t.Fatalf("idempotency key %q", str("idempotency_key"))
		}
		var input map[string]any
		_ = json.Unmarshal(req["input"], &input)
		for k := range input {
			if !allowed[k] {
				t.Fatalf("undeclared input field %q sent to %s", k, str("agent_id"))
			}
		}
		summary, _ := input["case_summary"].(string)
		if !strings.Contains(summary, "Test case") || !strings.Contains(summary, "Managed SD-WAN for 40 branches") || input["customer"] != "Acme Logistics" {
			t.Fatalf("case text must reach %s: %v", str("agent_id"), input)
		}
		byAgent[str("agent_id")] = input
	}
	for _, key := range []string{"sim.intake", "sim.solution_matching", "sim.design", "sim.standard_project", "sim.cost_challenge", "sim.proposal"} {
		if byAgent[key] == nil {
			t.Fatalf("agent %s was not called with its Agent Studio key (got %v)", key, len(byAgent))
		}
	}
	if _, ok := byAgent["sim.intake"]["upstream_artifacts"]; ok {
		t.Fatal("the first agent has no upstream artifacts")
	}
	ups := func(key string) map[string]any { m, _ := byAgent[key]["upstream_artifacts"].(map[string]any); return m }
	if m := ups("sim.solution_matching"); len(m) != 1 || m["requirement-register"] == nil {
		t.Fatalf("matching must see the requirement register only (design is its parallel sibling): %v", m)
	}
	if m := ups("sim.proposal"); len(m) != 5 {
		t.Fatalf("proposal must see all five prior artifacts, got %d", len(m))
	}
	reg := ups("sim.design")["requirement-register"].(map[string]any)
	if content, _ := reg["content"].(map[string]any); content["agent"] != "sim.intake" {
		t.Fatalf("upstream content must be the prior agent's draft: %v", reg)
	}
	for _, r := range d.AgentRuns {
		if r.Status != "succeeded" {
			t.Fatalf("agent run %+v did not reconcile", r)
		}
		if r.RuntimeRunID == "" || r.UsageReceivedAt == nil || !r.UsageObservation.Valid() ||
			r.UsageObservation.TokensIn != 120 || r.UsageObservation.TokensOut != 24 ||
			r.UsageObservation.Coverage != "partial" || r.UsageObservation.Class != "test" {
			t.Fatalf("case lost the authenticated source snapshot: %+v", r)
		}
	}
}

// A request Agent Studio rejects (4xx) is terminal: the step fails without burning retries on the same
// request, and the case is closed as failed for a human to look at.
func TestAgentStudioRejectionIsTerminal(t *testing.T) {
	e := newEnv(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"evaluation gate did not pass for this version"}`))
	}))
	defer srv.Close()
	client, err := agent.NewAgentStudio(agent.AgentStudioConfig{BaseURL: srv.URL, ServiceToken: studioToken, CallbackSecret: string(secret)})
	if err != nil {
		t.Fatal(err)
	}
	e.phase3(client)
	id := e.createCase("sim-tender", simVars)
	e.drain()
	g0 := pendingApproval(t, e.detail(id))
	if err := e.eng.Decide(e.ctx, e.approver, g0.ID, g0.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "Bid qualified"}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d := e.detail(id)
	if d.Status != cases.CaseFailed || calls != 1 {
		t.Fatalf("a 409 must fail the step once, got status %s after %d calls", d.Status, calls)
	}
}
