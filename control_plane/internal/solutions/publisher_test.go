package solutions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApplicationCompositionRejectsUnsupportedComponentsAndReferences(t *testing.T) {
	base := Definition{Key: "purchase-app", Name: "Purchase app", DeliveryMode: "solution", Output: "both", Workflows: []WorkflowRef{{Key: "purchase-review", Version: 1}}}
	valid := Application{Title: "Finance workspace", Pages: []ApplicationPage{{Key: "overview", Title: "Overview", Kind: "overview"}, {Key: "requests", Title: "New request", Kind: "intake", WorkflowKey: "purchase-review"}, {Key: "reviews", Title: "Reviews", Kind: "tasks", WorkflowKey: "purchase-review"}, {Key: "results", Title: "Results", Kind: "report", WorkflowKey: "purchase-review"}}}
	base.Application = &valid
	if err := Validate(base); err != nil {
		t.Fatal(err)
	}
	bad := []Application{
		{Title: "A", Pages: valid.Pages}, {Title: "App", Pages: nil},
		{Title: "App", Pages: []ApplicationPage{{Key: "page", Title: "Page", Kind: "script", WorkflowKey: "purchase-review"}}},
		{Title: "App", Pages: []ApplicationPage{{Key: "page", Title: "Page", Kind: "intake", WorkflowKey: "unlinked-workflow"}}},
		{Title: "App", Pages: []ApplicationPage{{Key: "page", Title: "Page", Kind: "tasks"}}},
		{Title: "App", Pages: []ApplicationPage{{Key: "../escape", Title: "Page", Kind: "overview"}}},
		{Title: "App", Pages: []ApplicationPage{{Key: "same", Title: "One", Kind: "overview"}, {Key: "same", Title: "Two", Kind: "overview"}}},
	}
	for i, app := range bad {
		base.Application = &app
		if Validate(base) == nil {
			t.Errorf("accepted invalid composition %d", i)
		}
	}
	base.Application = &valid
	base.Output = "api"
	if Validate(base) == nil {
		t.Fatal("API-only plan accepted business UI")
	}
	base.Application = nil
	if err := Validate(base); err != nil {
		t.Fatal("existing API plans must stay compatible", err)
	}
}
func TestInventoryExtractsOnlyReferencesAndNeverTreatsDiscoveryAsApproval(t *testing.T) {
	raw := json.RawMessage(`{"schema_version":"1.0","key":"purchase-review","version":1,"metadata":{"password":"not-for-export"},"policy_refs":["finance.approval"],"nodes":[{"id":"start","type":"start"},{"id":"agent","type":"agent_task","config":{"agent_ref":"finance.extract","skill_version":"1.0.0","context_sources":["kms-approved","case-files"],"instructions":"not-for-export","endpoint":"https://private.invalid"}},{"id":"service","type":"service_task","config":{"connector_ref":"finance.erp","operation":"read","params":{"secret":"not-for-export"}}},{"id":"review","type":"human_task","config":{"assignee_role":"finance-reviewer"}},{"id":"approval","type":"human_approval","config":{"policy_ref":"finance.approval"}},{"id":"end","type":"end"}]}`)
	refs, complete := Inventory(raw, "purchase-review", 1)
	if !complete || len(refs) != 8 {
		t.Fatalf("inventory %d, complete=%v", len(refs), complete)
	}
	for _, ref := range refs {
		if ref.State != "blocked" {
			t.Fatal("discovery claimed approval")
		}
	}
	encoded, _ := json.Marshal(refs)
	if strings.Contains(string(encoded), "not-for-export") || strings.Contains(string(encoded), "private.invalid") {
		t.Fatal("config data leaked into inventory")
	}
	if refs[0].Kind != "approval_policy" || refs[1].Ref != "finance.extract" {
		t.Fatal("references lost")
	}
}
func TestInventoryFailsClosedOnWrongVersionsUnknownNodesAndMissingDefinition(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`{"schema_version":"1.0","key":"different","version":1,"nodes":[{"type":"start"},{"type":"end"}]}`), json.RawMessage(`{"schema_version":"1.0","key":"purchase-review","version":1,"nodes":[{"id":"start","type":"start"},{"id":"nested","type":"subworkflow"}]}`)} {
		refs, complete := Inventory(raw, "purchase-review", 1)
		if complete || len(refs) == 0 {
			t.Fatal("unsupported inventory accepted as complete")
		}
	}
}
func TestCandidateChecksumSurvivesJSONBOrderingWithoutLosingLargeNumbers(t *testing.T) {
	first := []byte(`{"schema":"candidate","plan":{"value":9007199254740993,"name":"A"}}`)
	second := []byte(`{ "plan": { "name":"A", "value":9007199254740993 }, "schema":"candidate" }`)
	h1, err := CanonicalSnapshotHash(first)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := CanonicalSnapshotHash(second)
	if err != nil || h1 != h2 {
		t.Fatal("JSONB reordering changed checksum")
	}
	h3, _ := CanonicalSnapshotHash([]byte(strings.ReplaceAll(string(first), "9007199254740993", "9007199254740992")))
	if h1 == h3 {
		t.Fatal("large integer precision lost")
	}
	if _, err = CanonicalSnapshotHash([]byte(`{} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
}
