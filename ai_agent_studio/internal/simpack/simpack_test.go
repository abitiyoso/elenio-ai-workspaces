package simpack_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elenio/agent-studio/internal/repository/postgres"
	agentruntime "github.com/elenio/agent-studio/internal/runtime"
	"github.com/elenio/agent-studio/internal/simpack"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func loadAgent(t *testing.T, key string) (simpack.Pack, simpack.Agent) {
	t.Helper()
	pack, err := simpack.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range pack.Agents {
		if agent.Key == key {
			return pack, agent
		}
	}
	t.Fatalf("pack has no agent %s", key)
	return pack, simpack.Agent{}
}

func validates(t *testing.T, schema, doc json.RawMessage) error {
	t.Helper()
	compiled, err := agentruntime.CompileSchema(schema)
	if err != nil {
		t.Fatalf("schema does not compile: %v", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		t.Fatalf("instance is not JSON: %v", err)
	}
	return compiled.Validate(instance)
}

func TestPackLoadsSevenAgentsWithCompilingSchemasAndSuites(t *testing.T) {
	pack, err := simpack.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sim.intake", "sim.solution_matching", "sim.standard_project", "sim.design", "sim.cost_challenge", "sim.proposal", "sim.case_intake"}
	got := make([]string, 0, len(pack.Agents))
	for _, agent := range pack.Agents {
		got = append(got, agent.Key)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pack agents = %v, want %v", got, want)
	}
	for _, agent := range pack.Agents {
		var manifest struct {
			Configured bool   `json:"configured"`
			Template   string `json:"template"`
			ReviewGate string `json:"review_gate"`
			MaxIter    int    `json:"max_iterations"`
		}
		if err := json.Unmarshal(agent.Manifest, &manifest); err != nil {
			t.Fatalf("%s manifest: %v", agent.Key, err)
		}
		if !manifest.Configured || manifest.Template != pack.PackKey || manifest.ReviewGate != agent.ReviewGate || manifest.MaxIter < 1 || manifest.MaxIter > 15 {
			t.Fatalf("%s manifest is inconsistent: %+v", agent.Key, manifest)
		}
		if agent.Skill.Key == "" || agent.Skill.Semver == "" || len(agent.Tools) == 0 {
			t.Fatalf("%s is missing skill or tools", agent.Key)
		}
		if _, err := agentruntime.CompileSchema(agent.OutputSchema); err != nil {
			t.Fatalf("%s output schema: %v", agent.Key, err)
		}
		critical := 0
		for _, c := range agent.EvalSuite.Cases {
			if c.Critical {
				critical++
			}
			if err := validates(t, agent.InputSchema, c.Input); err != nil {
				t.Fatalf("%s case %s input violates the input schema: %v", agent.Key, c.Key, err)
			}
			var exp agentruntime.Expectations
			dec := json.NewDecoder(bytes.NewReader(c.Expectations))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&exp); err != nil {
				t.Fatalf("%s case %s uses unsupported expectation keys: %v", agent.Key, c.Key, err)
			}
			if _, err := agentruntime.ParseFixtures(c.EvidenceFixture); err != nil {
				t.Fatalf("%s case %s fixture: %v", agent.Key, c.Key, err)
			}
		}
		if len(agent.EvalSuite.Cases) != 4 || critical != 3 {
			t.Fatalf("%s suite has %d cases (%d critical), want 4 (3 critical)", agent.Key, len(agent.EvalSuite.Cases), critical)
		}
	}
}

func TestCaseIntakeDefinitionMatchesFlowcaseContract(t *testing.T) {
	_, agent := loadAgent(t, "sim.case_intake")
	if agent.ReviewGate != "G0" || !reflect.DeepEqual(agent.Tools, []string{"kms.search"}) {
		t.Fatalf("gate/tools: %s %v", agent.ReviewGate, agent.Tools)
	}
	if agent.Skill.Key != "case_intake.core" || agent.Skill.Semver != "1.0.0" {
		t.Fatalf("skill: %s %s", agent.Skill.Key, agent.Skill.Semver)
	}
	var manifest struct {
		MaxIterations int      `json:"max_iterations"`
		Forbidden     []string `json:"forbidden_actions"`
		Instructions  string   `json:"instructions"`
	}
	_ = json.Unmarshal(agent.Manifest, &manifest)
	if manifest.MaxIterations != 4 {
		t.Fatalf("max_iterations = %d", manifest.MaxIterations)
	}
	for _, action := range []string{"create_case", "approve_bid", "quote_price"} {
		if !strings.Contains(strings.Join(manifest.Forbidden, ","), action) {
			t.Fatalf("forbidden_actions misses %s: %v", action, manifest.Forbidden)
		}
	}
	var output struct {
		Required []string `json:"required"`
	}
	_ = json.Unmarshal(agent.OutputSchema, &output)
	if !reflect.DeepEqual(output.Required, []string{"reply", "case_update", "kb_status", "kb_items", "missing", "ready", "quick_replies", "unresolved"}) {
		t.Fatalf("output required = %v", output.Required)
	}
	good := json.RawMessage(`{"case_summary":"Workflow: SIM B2B Tender","conversation":[{"role":"user","content":"Halo"},{"role":"assistant","content":"Halo, ada yang bisa dibantu?"},{"role":"user","content":"Butuh chatbot"}],"draft":{"title":"x"}}`)
	if err := validates(t, agent.InputSchema, good); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	for name, bad := range map[string]string{
		"no conversation":    `{"case_summary":"x"}`,
		"empty conversation": `{"case_summary":"x","conversation":[]}`,
		"system role":        `{"case_summary":"x","conversation":[{"role":"system","content":"x"}]}`,
		"extra field":        `{"case_summary":"x","conversation":[{"role":"user","content":"x"}],"other":1}`,
		"extra msg field":    `{"case_summary":"x","conversation":[{"role":"user","content":"x","name":"a"}]}`,
		"draft not object":   `{"case_summary":"x","conversation":[{"role":"user","content":"x"}],"draft":"x"}`,
	} {
		if validates(t, agent.InputSchema, json.RawMessage(bad)) == nil {
			t.Errorf("invalid input accepted: %s", name)
		}
	}
}

const goodIntakeOutput = `{
 "reply": "Baik, kebutuhan ini cocok dengan use case Knowledge Base Assistant di katalog. Saya usulkan judul di bawah (bisa diubah). Berapa jumlah agen dan di lokasi mana?",
 "case_update": {"title": "Knowledge Base Assistant untuk Contact Center PT Sinar", "customer_name": "PT Sinar Nusantara Telekomunikasi", "deadline": "2026-12-15", "constraints": ["Data SOP tetap on-premise"]},
 "kb_status": "covered",
 "kb_items": [{"name": "Knowledge Base Assistant", "kind": "use_case", "evidence_refs": ["fixture-catalog-kba-1"]}],
 "missing": ["opportunity_id"],
 "ready": false,
 "quick_replies": ["Sekitar 300 agen", "Jakarta dan Surabaya"],
 "unresolved": []
}`

func TestCaseIntakeOutputValidationAndCitationCounting(t *testing.T) {
	_, agent := loadAgent(t, "sim.case_intake")
	seen := map[string]agentruntime.Evidence{"fixture-catalog-kba-1": {SourceID: "fixture-catalog-kba-1", TrustStatus: "approved"}}
	v := agentruntime.ValidateOutput(agent.OutputSchema, json.RawMessage(goodIntakeOutput), seen, nil)
	if !v.SchemaValid || len(v.RiskFlags) != 0 || !reflect.DeepEqual(v.CitedSources, []string{"fixture-catalog-kba-1"}) {
		t.Fatalf("good output: %+v", v)
	}
	validation, _ := json.Marshal(v)
	run := postgres.Run{Status: "succeeded", Output: json.RawMessage(goodIntakeOutput), Validation: validation}
	if passed, checks := agentruntime.ScoreCase(agent.EvalSuite.Cases[0].Expectations, run); !passed {
		t.Fatalf("good output fails the positive case: %+v", checks)
	}

	var base map[string]any
	_ = json.Unmarshal([]byte(goodIntakeOutput), &base)
	mutate := func(change func(map[string]any)) json.RawMessage {
		copy := map[string]any{}
		raw, _ := json.Marshal(base)
		_ = json.Unmarshal(raw, &copy)
		change(copy)
		out, _ := json.Marshal(copy)
		return out
	}
	bad := map[string]json.RawMessage{
		"missing ready":      mutate(func(m map[string]any) { delete(m, "ready") }),
		"empty reply":        mutate(func(m map[string]any) { m["reply"] = "" }),
		"bad deadline":       mutate(func(m map[string]any) { m["case_update"].(map[string]any)["deadline"] = "15/12/2026" }),
		"unknown case field": mutate(func(m map[string]any) { m["case_update"].(map[string]any)["status"] = "approved" }),
		"bad kb_status":      mutate(func(m map[string]any) { m["kb_status"] = "yes" }),
		"bad kb kind":        mutate(func(m map[string]any) { m["kb_items"].([]any)[0].(map[string]any)["kind"] = "product" }),
		"four quick replies": mutate(func(m map[string]any) { m["quick_replies"] = []any{"a", "b", "c", "d"} }),
		"extra top field":    mutate(func(m map[string]any) { m["case_created"] = true }),
		"unresolved no reason": mutate(func(m map[string]any) {
			m["unresolved"] = []any{map[string]any{"topic": "x"}}
		}),
	}
	for name, doc := range bad {
		if v := agentruntime.ValidateOutput(agent.OutputSchema, doc, seen, nil); v.SchemaValid {
			t.Errorf("invalid output accepted: %s", name)
		}
	}
	fabricated := mutate(func(m map[string]any) {
		m["kb_items"] = []any{map[string]any{"name": "Invented Suite", "kind": "solution", "evidence_refs": []any{"made-up"}}}
	})
	if v := agentruntime.ValidateOutput(agent.OutputSchema, fabricated, seen, nil); !v.SchemaValid || !contains(v.RiskFlags, "unknown_citation") {
		t.Fatalf("fabricated catalog citation was not flagged: %+v", v)
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

type discardSink struct{}

func (discardSink) Emit(context.Context, postgres.RunEvent) {}

// The case-intake suite runs through the real Eino executor (dev mock model,
// fixture evidence) and the deterministic scorer, and its output travels in
// the unchanged Flowcase envelope.
func TestCaseIntakeEvalSuiteRunsThroughExecutor(t *testing.T) {
	_, agent := loadAgent(t, "sim.case_intake")
	executor := &agentruntime.Executor{AllowDevMock: true}
	for _, c := range agent.EvalSuite.Cases {
		exec := postgres.RunExecution{
			Run: postgres.Run{ID: "run-" + c.Key, Kind: "evaluation", ModelMode: "dev_mock", Input: c.Input, EvidenceInput: c.EvidenceFixture,
				DeadlineAt: time.Now().Add(time.Minute)},
			Agent:   postgres.Agent{Key: agent.Key, DisplayName: "Case Intake Assistant", Domain: "case-intake"},
			Version: postgres.AgentVersion{ID: "v1", Semver: "0.1.0", Manifest: agent.Manifest, InputSchema: agent.InputSchema, OutputSchema: agent.OutputSchema},
			Skills: []postgres.SkillVersion{{Semver: agent.Skill.Semver, Instructions: agent.Skill.Instructions, Procedures: agent.Skill.Procedures,
				ForbiddenActions: agent.Skill.ForbiddenActions}},
			Tools: []postgres.BoundToolVersion{{ToolVersion: postgres.ToolVersion{ID: "t1", Key: "kms.search", AdapterRef: "kms.retrieval.v1",
				Policy: json.RawMessage(`{"max_calls":3}`)}, Scope: json.RawMessage(`{}`)}},
		}
		out, err := executor.Execute(context.Background(), exec, discardSink{})
		if err != nil {
			t.Fatalf("%s: %v", c.Key, err)
		}
		validation, _ := json.Marshal(out.Validation)
		run := postgres.Run{ID: exec.Run.ID, Status: out.Status, Output: out.Output, Validation: validation, ModelMode: "dev_mock", Pinned: json.RawMessage(`{}`)}
		if passed, checks := agentruntime.ScoreCase(c.Expectations, run); !passed {
			t.Errorf("%s did not pass: status=%s error=%s checks=%+v", c.Key, out.Status, out.ErrorCode, checks)
			continue
		}
		for i := range out.Evidence {
			out.Evidence[i].Cited = contains(out.Validation.CitedSources, out.Evidence[i].SourceID)
		}
		envelope, err := postgres.FlowcaseOutput(run, out.Evidence)
		if err != nil {
			t.Fatalf("%s envelope: %v", c.Key, err)
		}
		var decoded struct {
			ArtifactDraft map[string]any `json:"artifact_draft"`
			Review        struct {
				Status string `json:"status"`
			} `json:"review"`
			Sources []postgres.FlowcaseSource `json:"sources"`
		}
		if err := json.Unmarshal(envelope, &decoded); err != nil || decoded.ArtifactDraft["reply"] == nil || decoded.Review.Status == "" {
			t.Fatalf("%s envelope is not usable: %s %v", c.Key, envelope, err)
		}
		if c.Key == "positive-grounded" && len(decoded.Sources) == 0 {
			t.Fatalf("positive case envelope has no cited sources: %s", envelope)
		}
	}
}

// A recommendation naming the option plus a short reason ran past 200
// characters in live runs (235 and 269) and failed the whole HLD draft.
func TestDesignOptionForReviewFitsNameAndShortReason(t *testing.T) {
	_, agent := loadAgent(t, "sim.design")
	if agent.Skill.Semver != "1.1.0" || !strings.Contains(strings.Join(rawStrings(t, agent.Skill.Procedures), " "), "option_for_review") {
		t.Fatalf("skill %s does not tell the model how to fill option_for_review", agent.Skill.Semver)
	}
	output := func(option string) json.RawMessage {
		doc, _ := json.Marshal(map[string]any{
			"hld_options":       []any{map[string]any{"name": "Opsi A", "summary": "x", "components": []string{}, "risks": []any{}, "evidence_refs": []string{}}},
			"option_for_review": option, "assumptions": []string{}, "unresolved": []any{},
		})
		return doc
	}
	if err := validates(t, agent.OutputSchema, output(strings.Repeat("a", 269))); err != nil {
		t.Fatalf("269-character recommendation rejected: %v", err)
	}
	if validates(t, agent.OutputSchema, output(strings.Repeat("a", 501))) == nil {
		t.Fatal("501-character recommendation accepted")
	}
}

func rawStrings(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	return items
}
