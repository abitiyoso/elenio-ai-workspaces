package validator_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/definition"
	"github.com/elenio/flowcase/internal/workflows/validator"
)

func testCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	ps, err := packs.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := packs.Catalog(ps)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sample(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../../samples/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func encode(t *testing.T, m any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func nodes(m map[string]any) []any { return m["nodes"].([]any) }

func node(m map[string]any, id string) map[string]any {
	for _, n := range nodes(m) {
		if nm := n.(map[string]any); nm["id"] == id {
			return nm
		}
	}
	panic("no node " + id)
}

func config(m map[string]any, id string) map[string]any {
	n := node(m, id)
	if n["config"] == nil {
		n["config"] = map[string]any{}
	}
	return n["config"].(map[string]any)
}

func codes(r definition.Report) []string {
	var out []string
	for _, d := range r.Errors() {
		out = append(out, d.Code)
	}
	return out
}

func TestSamplePacksAreValid(t *testing.T) {
	cat := testCatalog(t)
	for _, name := range []string{"sim_tender_v1.json", "sim_tender_v2.json", "sim_tender_v3.json", "sim_tender_v4.json", "sim_tender_v5_draft.json", "poc_request_v1.json"} {
		t.Run(name, func(t *testing.T) {
			res := validator.Validate(encode(t, sample(t, name)), cat)
			if !res.Report.Valid {
				t.Fatalf("expected valid, got %+v", res.Report.Errors())
			}
			if res.Graph == nil || res.Graph.Order == nil {
				t.Fatal("valid definitions must compile to an ordered graph")
			}
		})
	}
}

func TestSIMWarnsThatPoliciesAreProvisional(t *testing.T) {
	res := validator.Validate(encode(t, sample(t, "sim_tender_v1.json")), testCatalog(t))
	n := 0
	for _, d := range res.Report.Diagnostics {
		if d.Code == "provisional_policy" {
			n++
		}
	}
	if n != 6 {
		t.Fatalf("expected 6 provisional-policy warnings for G0-G5, got %d", n)
	}
}

func TestCompiledSIMGraph(t *testing.T) {
	res := validator.Validate(encode(t, sample(t, "sim_tender_v1.json")), testCatalog(t))
	g := res.Graph
	if g.SplitJoin["solution_parallel"] != "solution_join" || g.JoinExpects["solution_join"] != 2 {
		t.Fatalf("split/join pairing wrong: %v %v", g.SplitJoin, g.JoinExpects)
	}
	if g.Order[0] != "start" || g.Order[len(g.Order)-1] != "end" {
		t.Fatalf("unexpected topological order %v", g.Order)
	}
}

func TestDefectsAreRejected(t *testing.T) {
	type mutation func(m map[string]any)
	cases := []struct {
		name   string
		sample string
		mutate mutation
		want   string
	}{
		{"missing edge target", "poc_request_v1.json", func(m map[string]any) { node(m, "scope")["next"] = []any{"nowhere"} }, "unknown_target"},
		{"duplicate node id", "poc_request_v1.json", func(m map[string]any) { node(m, "provision")["id"] = "scope" }, "duplicate_node_id"},
		{"missing start", "poc_request_v1.json", func(m map[string]any) {
			node(m, "start")["type"] = "human_task"
			config(m, "start")["assignee_role"] = "poc-owner"
		}, "missing_start"},
		{"two starts", "poc_request_v1.json", func(m map[string]any) {
			m["nodes"] = append(nodes(m), map[string]any{"id": "start2", "type": "start", "name": "Second", "next": []any{"scope"}})
		}, "multiple_starts"},
		{"missing end", "poc_request_v1.json", func(m map[string]any) {
			node(m, "end")["type"] = "human_task"
			node(m, "end")["next"] = []any{"provision"}
			config(m, "end")["assignee_role"] = "poc-owner"
		}, "missing_end"},
		{"end with transitions", "poc_request_v1.json", func(m map[string]any) { node(m, "end")["next"] = []any{"scope"} }, "invalid_next"},
		{"unreachable node", "poc_request_v1.json", func(m map[string]any) {
			m["nodes"] = append(nodes(m), map[string]any{"id": "orphan", "type": "human_task", "name": "Orphan", "config": map[string]any{"assignee_role": "poc-owner"}, "next": []any{"end"}})
		}, "unreachable_node"},
		{"cycle", "poc_request_v1.json", func(m map[string]any) { node(m, "provision")["next"] = []any{"scope"} }, "cycle"},
		{"self loop", "poc_request_v1.json", func(m map[string]any) { node(m, "scope")["next"] = []any{"scope"} }, "self_loop"},
		{"edge into start", "poc_request_v1.json", func(m map[string]any) { node(m, "provision")["next"] = []any{"start"} }, "edge_into_start"},

		{"string expression", "poc_request_v1.json", func(m map[string]any) { config(m, "budget_rule")["expression"] = "estimated_cost > 0" }, "unsafe_expression"},
		{"javascript operator", "poc_request_v1.json", func(m map[string]any) {
			config(m, "budget_rule")["expression"] = map[string]any{"operator": "js", "field": "estimated_cost", "value": "process.exit()"}
		}, "unsafe_expression"},
		{"extra expression property", "poc_request_v1.json", func(m map[string]any) {
			config(m, "budget_rule")["expression"].(map[string]any)["script"] = "return true"
		}, "unsafe_expression"},
		{"numeric comparison with string", "poc_request_v1.json", func(m map[string]any) {
			config(m, "budget_rule")["expression"].(map[string]any)["value"] = "zero"
		}, "invalid_expression"},
		{"comparison on string field", "poc_request_v1.json", func(m map[string]any) {
			config(m, "budget_rule")["expression"].(map[string]any)["field"] = "purpose"
		}, "type_mismatch"},
		{"bad field path", "poc_request_v1.json", func(m map[string]any) {
			config(m, "budget_rule")["expression"].(map[string]any)["field"] = "constructor.prototype['x']"
		}, "invalid_expression"},
		{"too deep", "poc_request_v1.json", func(m map[string]any) {
			var e any = map[string]any{"operator": "exists", "field": "purpose"}
			for range 6 {
				e = map[string]any{"operator": "not", "condition": e}
			}
			config(m, "budget_rule")["expression"] = e
		}, "expression_too_deep"},
		{"non exhaustive condition", "poc_request_v1.json", func(m map[string]any) {
			delete(config(m, "budget_rule")["branches"].(map[string]any), "false")
		}, "non_exhaustive_condition"},
		{"branches disagree with next", "poc_request_v1.json", func(m map[string]any) {
			config(m, "budget_rule")["branches"].(map[string]any)["false"] = "end"
		}, "branches_next_mismatch"},
		{"unknown branch label", "poc_request_v1.json", func(m map[string]any) {
			config(m, "budget_rule")["branches"].(map[string]any)["maybe"] = "provision"
		}, "invalid_branch_label"},

		{"split without join", "sim_tender_v1.json", func(m map[string]any) { config(m, "solution_join")["split_ref"] = "feasibility_parallel" }, "unmatched_split"},
		{"join referencing non-split", "sim_tender_v1.json", func(m map[string]any) { config(m, "solution_join")["split_ref"] = "g1" }, "unmatched_join"},
		{"branch bypasses join", "sim_tender_v1.json", func(m map[string]any) { node(m, "design")["next"] = []any{"end"} }, "branch_bypasses_join"},
		{"jump into parallel region", "sim_tender_v1.json", func(m map[string]any) { node(m, "g0")["next"] = []any{"matching"} }, "enters_parallel_region"},
		{"split with one branch", "sim_tender_v1.json", func(m map[string]any) { node(m, "solution_parallel")["next"] = []any{"matching"} }, "invalid_next"},
		{"unsupported join mode", "sim_tender_v1.json", func(m map[string]any) { config(m, "solution_join")["join"] = "any" }, "unsupported_join"},

		{"unknown policy (UAT-08)", "sim_tender_v1.json", func(m map[string]any) {
			config(m, "g5")["policy_ref"] = "sim.unpublished-policy"
			m["policy_refs"] = append(m["policy_refs"].([]any), "sim.unpublished-policy")
		}, "unknown_policy_ref"},
		{"policy not declared", "sim_tender_v1.json", func(m map[string]any) { m["policy_refs"] = m["policy_refs"].([]any)[:5] }, "policy_ref_not_declared"},
		{"approval without policy", "sim_tender_v1.json", func(m map[string]any) { delete(config(m, "g2"), "policy_ref") }, "required"},
		{"unregistered agent", "sim_tender_v1.json", func(m map[string]any) { config(m, "intake")["agent_ref"] = "sim.orchestrator" }, "unknown_agent_ref"},
		{"unregistered artifact type", "sim_tender_v1.json", func(m map[string]any) { config(m, "design")["output_artifact_type"] = "anything" }, "unknown_artifact_type"},
		{"unknown context source", "sim_tender_v1.json", func(m map[string]any) { config(m, "intake")["context_sources"] = []any{"internet"} }, "unknown_context_source"},
		{"embedded endpoint", "sim_tender_v1.json", func(m map[string]any) { config(m, "intake")["url"] = "https://example.com/hook" }, "unsafe_config"},
		{"unknown config key", "sim_tender_v1.json", func(m map[string]any) { config(m, "g1")["auto_approve"] = true }, "unknown_config"},
		{"author_review not boolean", "sim_tender_v1.json", func(m map[string]any) { config(m, "intake")["author_review"] = "yes" }, "invalid_type"},
		{"author_review on a gate", "sim_tender_v1.json", func(m map[string]any) { config(m, "g1")["author_review"] = true }, "unknown_config"},
		{"unregistered connector", "poc_request_v1.json", func(m map[string]any) {
			n := node(m, "provision")
			n["type"] = "service_task"
			n["config"] = map[string]any{"connector_ref": "crm.update", "operation": "write"}
		}, "unknown_connector_ref"},
		{"unknown assignee role", "poc_request_v1.json", func(m map[string]any) { config(m, "scope")["assignee_role"] = "ceo" }, "unknown_assignee_role"},
		{"subworkflow deferred", "poc_request_v1.json", func(m map[string]any) { node(m, "provision")["type"] = "subworkflow" }, "unsupported_node_type"},
		{"bad duration", "poc_request_v1.json", func(m map[string]any) { config(m, "scope")["due_in"] = "3 weeks" }, "invalid_duration"},
		{"timer needs one trigger", "poc_request_v1.json", func(m map[string]any) {
			n := node(m, "provision")
			n["type"] = "timer"
			n["config"] = map[string]any{}
		}, "invalid_timer"},
		{"invalid return_to", "sim_tender_v1.json", func(m map[string]any) { config(m, "g1")["return_to"] = "g2" }, "invalid_return_to"},
		{"return_to across region", "sim_tender_v1.json", func(m map[string]any) { config(m, "g2")["return_to"] = "design" }, "invalid_return_to"},

		{"unknown node type", "poc_request_v1.json", func(m map[string]any) { node(m, "scope")["type"] = "python" }, "unsupported_node_type"},
		{"unknown top-level property", "poc_request_v1.json", func(m map[string]any) { m["hooks"] = []any{"x"} }, "unknown_property"},
		{"wrong schema version", "poc_request_v1.json", func(m map[string]any) { m["schema_version"] = "2.0" }, "unsupported_schema_version"},
		{"bad key", "poc_request_v1.json", func(m map[string]any) { m["key"] = "Bad Key" }, "invalid_key"},
		{"unsafe input schema", "poc_request_v1.json", func(m map[string]any) {
			m["input_schema"].(map[string]any)["$ref"] = "https://evil.example/schema.json"
		}, "invalid_data_schema"},
		{"regex in input schema", "poc_request_v1.json", func(m map[string]any) {
			m["input_schema"].(map[string]any)["properties"].(map[string]any)["purpose"].(map[string]any)["pattern"] = "(a+)+$"
		}, "invalid_data_schema"},
		{"hidden field required", "sim_tender_v4.json", func(m map[string]any) {
			s := m["input_schema"].(map[string]any)
			s["required"] = []any{"deadline", "customer_id"}
		}, "invalid_data_schema"},
		{"x-hidden not boolean", "sim_tender_v4.json", func(m map[string]any) {
			m["input_schema"].(map[string]any)["properties"].(map[string]any)["customer_id"].(map[string]any)["x-hidden"] = "yes"
		}, "invalid_data_schema"},
	}
	cat := testCatalog(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sample(t, tc.sample)
			tc.mutate(m)
			res := validator.Validate(encode(t, m), cat)
			if res.Report.Valid {
				t.Fatalf("expected %s, but definition was accepted", tc.want)
			}
			if !slices.Contains(codes(res.Report), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, res.Report.Errors())
			}
			for _, d := range res.Report.Errors() {
				if d.Message == "" || (d.Path != "" && !strings.HasPrefix(d.Path, "/")) {
					t.Fatalf("diagnostic must carry a message and JSON pointer path: %+v", d)
				}
			}
		})
	}
}

// author_review is an agent_task flag: the case owner reviews the draft before the next step.
func TestAuthorReviewIsAnAgentTaskFlag(t *testing.T) {
	cat := testCatalog(t)
	m := sample(t, "sim_tender_v1.json")
	config(m, "intake")["author_review"] = true
	config(m, "design")["author_review"] = false
	if res := validator.Validate(encode(t, m), cat); !res.Report.Valid {
		t.Fatalf("author_review should be accepted on agent tasks: %+v", res.Report.Errors())
	}
	v3 := sample(t, "sim_tender_v3.json")
	for _, n := range nodes(v3) {
		nm := n.(map[string]any)
		if nm["type"] == "agent_task" && config(v3, nm["id"].(string))["author_review"] != true {
			t.Fatalf("sim_tender_v3 agent step %s must set author_review", nm["id"])
		}
	}
}

func TestMalformedDocuments(t *testing.T) {
	cat := testCatalog(t)
	for name, raw := range map[string]string{
		"not json":       `{"nodes":`,
		"trailing data":  `{} {}`,
		"array root":     `[]`,
		"huge":           `{"x":"` + strings.Repeat("a", 300<<10) + `"}`,
		"nodes not list": `{"schema_version":"1.0","key":"k","name":"Name","version":1,"nodes":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if res := validator.Validate([]byte(raw), cat); res.Report.Valid || res.Definition != nil {
				t.Fatalf("malformed document accepted: %+v", res.Report)
			}
		})
	}
}

func TestEmbeddedPackFilesMatchSamples(t *testing.T) {
	for _, pair := range [][2]string{
		{"../../packs/sim/sim_tender_v1.json", "../../../samples/sim_tender_v1.json"},
		{"../../packs/sim/sim_tender_v2.json", "../../../samples/sim_tender_v2.json"},
		{"../../packs/sim/sim_tender_v3.json", "../../../samples/sim_tender_v3.json"},
		{"../../packs/sim/sim_tender_v4.json", "../../../samples/sim_tender_v4.json"},
		{"../../packs/sim/sim_tender_v5_draft.json", "../../../samples/sim_tender_v5_draft.json"},
		{"../../packs/poc/poc_request_v1.json", "../../../samples/poc_request_v1.json"},
	} {
		a, errA := os.ReadFile(pair[0])
		b, errB := os.ReadFile(pair[1])
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			t.Fatalf("%s drifted from %s", pair[0], pair[1])
		}
	}
}

func TestNodeTypesMatchBundledJSONSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/WORKFLOW_DEFINITION_SCHEMA.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			Node struct {
				Properties struct {
					Type struct {
						Enum []string `json:"enum"`
					} `json:"type"`
				} `json:"properties"`
			} `json:"node"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	var ours []string
	for _, nt := range definition.NodeTypes {
		ours = append(ours, string(nt))
	}
	if !slices.Equal(schema.Defs.Node.Properties.Type.Enum, ours) {
		t.Fatalf("node types drifted from contracts schema: %v vs %v", schema.Defs.Node.Properties.Type.Enum, ours)
	}
}
