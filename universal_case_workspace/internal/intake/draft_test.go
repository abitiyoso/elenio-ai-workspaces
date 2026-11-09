package intake

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elenio/case-workspace/internal/agentstudio"
	"github.com/elenio/case-workspace/internal/apierr"
	"github.com/elenio/case-workspace/internal/packs"
)

func simFields(t *testing.T) []packs.IntakeField {
	t.Helper()
	reg, err := packs.Load(context.Background(), "../../domain-packs", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.All() {
		if p.Valid() && p.Manifest.Intake != nil {
			return p.Manifest.Intake.Fields
		}
	}
	t.Fatal("no pack with intake")
	return nil
}

func TestCleanValidatesAgainstPackFields(t *testing.T) {
	f := simFields(t)
	d, err := Clean(f, map[string]any{"title": "  Contact center AI  ", "deadline": "2027-01-29T10:00:00Z", "constraints": []any{"- data in ID", "data in ID", " "}})
	if err != nil {
		t.Fatal(err)
	}
	if d["title"] != "Contact center AI" || d["deadline"] != "2027-01-29" {
		t.Fatalf("draft = %v", d)
	}
	if l, _ := d["constraints"].([]string); len(l) != 1 || l[0] != "data in ID" {
		t.Fatalf("constraints = %v", d["constraints"])
	}
	for name, in := range map[string]map[string]any{
		"unknown key":      {"budget": "1"},
		"bad date":         {"deadline": "next friday"},
		"too long":         {"customer_id": strings.Repeat("x", 201)},
		"wrong type":       {"title": 5},
		"list of numbers":  {"constraints": []any{1, 2}},
		"text given array": {"title": []any{"a"}},
	} {
		_, err := Clean(f, in)
		var ae *apierr.Error
		if !errors.As(err, &ae) || ae.Status != 422 || ae.Code != "invalid_draft" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestMergeMissingReady(t *testing.T) {
	f := simFields(t)
	d := Draft{"title": "Old"}
	changed := d.Merge(f, map[string]any{"title": "New title", "customer_name": "", "deadline": "garbage", "opportunity_id": 42,
		"unknown": "x", "constraints": []any{"a", "a", "b"}, "case_summary": strings.Repeat("y", 5000)})
	if strings.Join(changed, ",") != "title,case_summary,constraints" {
		t.Fatalf("changed = %v", changed)
	}
	if utf8.RuneCountInString(d["case_summary"].(string)) != 4000 {
		t.Fatal("long text not truncated to max_length")
	}
	if d.Merge(f, map[string]any{"constraints": []any{}}) != nil {
		t.Fatal("empty list must not erase")
	}
	missing := Missing(f, d)
	if strings.Join(missing, ",") != "Customer name,Target date" {
		t.Fatalf("missing = %v", missing)
	}
	if Ready(true, missing, "covered") {
		t.Fatal("ready with missing fields")
	}
	d.Merge(f, map[string]any{"customer_name": "PT X", "deadline": "2027-01-29"})
	missing = Missing(f, d)
	if len(missing) != 0 || !Ready(true, missing, "partial") || Ready(true, missing, "not_covered") || Ready(false, missing, "covered") {
		t.Fatalf("ready logic wrong, missing = %v", missing)
	}
}

func TestBuildInputBounds(t *testing.T) {
	f := simFields(t)
	var msgs []Message
	for i := range 30 {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		msgs = append(msgs, Message{Role: role, Content: strings.Repeat("m", 5000)})
	}
	msgs = append(msgs, Message{Role: RoleUser, Content: "latest"})
	d := Draft{"case_summary": strings.Repeat("s", 4000), "rfp_excerpt": strings.Repeat("r", 20000)}
	raw, err := BuildInput("Pack", "wf", f, d, Missing(f, d), msgs)
	if err != nil {
		t.Fatal(err)
	}
	var in struct {
		CaseSummary  string `json:"case_summary"`
		Conversation []struct{ Role, Content string }
		Draft        map[string]any
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	if len(in.Conversation) != 20 || in.Conversation[19].Content != "latest" {
		t.Fatalf("conversation len %d", len(in.Conversation))
	}
	for _, c := range in.Conversation {
		if utf8.RuneCountInString(c.Content) > 4000 {
			t.Fatal("message over 4000")
		}
	}
	if n := utf8.RuneCountInString(in.CaseSummary); n > 4000 || !strings.Contains(in.CaseSummary, "Still missing") {
		t.Fatalf("summary %d runes", n)
	}
	if _, err := BuildInput("Pack", "wf", f, d, nil, []Message{{Role: RoleAssistant, Content: "hi"}}); err == nil {
		t.Fatal("conversation must end with the user")
	}
	if len(raw) > agentstudio.MaxRequest {
		t.Fatal("input exceeds run body limit")
	}
}

func output(t *testing.T, draft map[string]any) (json.RawMessage, string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"artifact_draft": draft, "review": map[string]any{"validation": map[string]any{"cited_sources": []string{"s1"}}},
		"model_mode": "live", "sources": []map[string]any{{"source_id": "s1", "title": "Catalog"}, {"source_id": "s2", "title": "Uncited"}}})
	sum, _ := agentstudio.OutputChecksum(raw)
	return raw, sum
}

func TestReconcile(t *testing.T) {
	out, sum := output(t, map[string]any{"reply": "Halo", "case_update": map[string]any{"title": "T"}, "kb_status": "partial", "ready": false,
		"kb_items": []any{map[string]any{"name": "AI CC", "kind": "solution", "evidence_refs": []any{"s1"}}}})
	ok := agentstudio.Run{Status: "succeeded", AgentID: "agent.x", Output: out, OutputChecksum: sum}
	turn, err := Reconcile(ok, "agent.x")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Reply != "Halo" || len(turn.Sources) != 1 || turn.Sources[0].SourceID != "s1" || turn.KBStatus != "partial" {
		t.Fatalf("turn = %+v", turn)
	}
	bad := ok
	bad.OutputChecksum = strings.Repeat("0", 64)
	if _, err := Reconcile(bad, "agent.x"); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if _, err := Reconcile(ok, "agent.other"); err == nil {
		t.Fatal("agent mismatch accepted")
	}
	empty, esum := output(t, map[string]any{"reply": "  "})
	if _, err := Reconcile(agentstudio.Run{Status: "succeeded", AgentID: "agent.x", Output: empty, OutputChecksum: esum}, "agent.x"); err == nil {
		t.Fatal("empty reply accepted")
	}
}

func TestToCreateInput(t *testing.T) {
	f := simFields(t)
	d := Draft{"title": "Contact center AI", "customer_id": "C-1", "opportunity_id": "O-1", "deadline": "2027-01-29", "constraints": []string{"a"}, "customer_name": "PT X"}
	in, err := ToCreateInput(f, d, "wf-key")
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Contact center AI" || in.WorkflowKey != "wf-key" || in.Version != 0 {
		t.Fatalf("in = %+v", in)
	}
	if in.Variables["deadline"] != "2027-01-29T00:00:00Z" || in.Variables["customer_name"] != "PT X" {
		t.Fatalf("vars = %v", in.Variables)
	}
	if _, has := in.Variables["customer_id"]; has {
		t.Fatal("fields the pack does not declare (customer_id) must not reach Flowcase")
	}
	if _, has := in.Variables["title"]; has {
		t.Fatal("title must not be a variable")
	}
	if l, _ := in.Variables["constraints"].([]string); len(l) != 1 {
		t.Fatalf("constraints = %v", in.Variables["constraints"])
	}
	if _, err := ToCreateInput(f, Draft{"customer_id": "x"}, "wf"); err == nil {
		t.Fatal("missing title accepted")
	}
}

func TestIdempotencyKeysAcceptable(t *testing.T) {
	k := IdempotencyKey("0b9d6f5e-6a59-4b8e-9d1c-2f4e8a7b6c5d", 30)
	if !agentstudio.IdempotencyPattern.MatchString(k) {
		t.Fatalf("%s rejected by Agent Studio pattern", k)
	}
	if !agentstudio.RefPattern.MatchString("intake:0b9d6f5e-6a59-4b8e-9d1c-2f4e8a7b6c5d") {
		t.Fatal("correlation id rejected")
	}
	a, b := FlowcaseIdempotencyKey("client-key-1", "S"), FlowcaseIdempotencyKey("client-key-1", "T")
	if a == b || len(a) < 8 || len(a) > 128 {
		t.Fatal("flowcase key derivation")
	}
}
