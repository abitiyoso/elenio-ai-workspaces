package postgres

import (
	"encoding/json"
	"testing"
)

func TestFlowcaseOutputCarriesCitedSourcesInStableOrder(t *testing.T) {
	refs := []EvidenceRef{
		{SourceID: "b-doc", SourceVersion: "v1", Title: "Katalog Use Case", Citation: "p2", Anchor: "#11", Cited: true},
		{SourceID: "a-doc", SourceVersion: "v3", Title: "Data Dummy POC", Cited: true},
		{SourceID: "c-doc", SourceVersion: "v1", Title: "Seen but not cited", Cited: false},
		{SourceID: "b-doc", SourceVersion: "v1", Title: "Katalog Use Case", Cited: true}, // duplicate tool hit
	}
	run := Run{Status: "succeeded", Output: json.RawMessage(`{"requirements":[]}`), Validation: json.RawMessage(`{"schema_valid":true}`), Pinned: json.RawMessage(`{}`), ModelMode: "provider"}
	first, err := FlowcaseOutput(run, refs)
	if err != nil {
		t.Fatal(err)
	}
	// Reversed input order must not change the bytes (poll and callback must share one checksum).
	reversed := []EvidenceRef{refs[3], refs[2], refs[1], refs[0]}
	second, _ := FlowcaseOutput(run, reversed)
	if string(first) != string(second) {
		t.Fatalf("output depends on evidence order:\n%s\n%s", first, second)
	}
	var env struct {
		Sources []FlowcaseSource `json:"sources"`
	}
	if err := json.Unmarshal(first, &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Sources) != 2 || env.Sources[0].SourceID != "a-doc" || env.Sources[1].Title != "Katalog Use Case" || env.Sources[1].Anchor != "#11" {
		t.Fatalf("unexpected sources: %+v", env.Sources)
	}
	none, _ := FlowcaseOutput(run, nil)
	if err := json.Unmarshal(none, &env); err != nil || env.Sources == nil || len(env.Sources) != 0 {
		t.Fatalf("sources must be an empty array when nothing is cited: %s", none)
	}
}
