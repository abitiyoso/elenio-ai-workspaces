package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAgentStudioBusinessInputPreservesTypedValuesAndDelegation(t *testing.T) {
	f := &fakeStudio{}
	executor, _ := newStudio(t, f, ownerResolver(t, testSubject))
	input := flowcaseInputJSON(t, func(m map[string]any) {
		m["case_data"] = map[string]any{"invoice_number": "INV-001", "external_id": json.Number("9007199254740993"),
			"total": json.Number("124.50"), "verified": false, "note": "Biaya perjalanan 東京",
			"lines": []any{map[string]any{"quantity": json.Number("2"), "description": "Layanan"}}}
	})
	req := startReq(input)
	req.AgentID = "finance.invoice_extract"
	req.InputContract = "flowcase.case.v1"
	if _, err := executor.StartRun(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	var sent AgentStudioRunRequest
	if len(f.bodies) != 1 {
		t.Fatal("expected one source request")
	}
	if err := json.Unmarshal(f.bodies[0], &sent); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	_ = json.Compact(&compact, input)
	if !bytes.Equal(sent.Input, compact.Bytes()) || !bytes.Contains(sent.Input, []byte("9007199254740993")) || !bytes.Contains(sent.Input, []byte("124.50")) {
		t.Fatalf("typed input changed: %s", sent.Input)
	}
	if sent.DelegatedUser != testSubject || sent.AgentID != "finance.invoice_extract" || len(sent.Evidence) != 1 {
		t.Fatalf("original authorization/evidence mapping changed: %+v", sent)
	}
	if bytes.Contains(f.bodies[0], []byte("input_contract")) {
		t.Fatal("internal contract selector leaked into source API")
	}
}

func TestAgentStudioBusinessInputRejectsWithoutTruncationOrNetwork(t *testing.T) {
	for _, tc := range []struct {
		name, contract string
		input          json.RawMessage
	}{
		{"unknown contract", "anything", json.RawMessage(`{"case_data":{}}`)},
		{"null", "flowcase.case.v1", json.RawMessage("null")},
		{"array", "flowcase.case.v1", json.RawMessage("[]")},
		{"missing case data", "flowcase.case.v1", json.RawMessage(`{"task":"Check"}`)},
		{"trailing object", "flowcase.case.v1", json.RawMessage(`{"case_data":{}}{}`)},
		{"unknown envelope field", "flowcase.case.v1", json.RawMessage(`{"case_data":{},"secret":"must not forward"}`)},
		{"wrong case data type", "flowcase.case.v1", json.RawMessage(`{"case_data":[]}`)},
		{"oversized", "flowcase.case.v1", flowcaseInputJSON(t, func(m map[string]any) {
			m["case_data"] = map[string]any{"text": strings.Repeat("x", MaxAgentStudioRequest)}
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeStudio{}
			executor, _ := newStudio(t, f, nil)
			req := startReq(tc.input)
			req.InputContract = tc.contract
			if _, err := executor.StartRun(t.Context(), req); !errors.Is(err, ErrRejected) {
				t.Fatalf("invalid request not rejected: %v", err)
			}
			if len(f.bodies) != 0 {
				t.Fatal("invalid business data reached source")
			}
		})
	}
}
