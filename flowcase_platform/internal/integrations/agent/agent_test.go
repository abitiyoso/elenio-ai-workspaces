package agent

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func body(t *testing.T, mutate func(m map[string]any)) []byte {
	t.Helper()
	out := json.RawMessage(`{"summary":"ok","n":1}`)
	sum, _ := OutputChecksum(out)
	m := map[string]any{"message_id": "m-1", "audience": Audience, "tenant_id": "t-1",
		"run": map[string]any{"run_id": "r-1", "status": "succeeded", "agent_id": "a", "output": out, "output_checksum": sum}}
	if mutate != nil {
		mutate(m)
	}
	b, _ := json.Marshal(m)
	return b
}

func TestCallbackContract(t *testing.T) {
	now := time.Now()
	good := body(t, nil)
	res, err := VerifyHMACCallback(key, CallbackEnvelope{Headers: SignCallback(key, now, good), Body: good}, now)
	if err != nil || res.MessageID != "m-1" || res.Run.Status != RunSucceeded {
		t.Fatalf("valid callback rejected: %v %+v", err, res)
	}
	cases := map[string]CallbackEnvelope{
		"tampered body":  {Headers: SignCallback(key, now, good), Body: body(t, func(m map[string]any) { m["tenant_id"] = "t-2" })},
		"wrong key":      {Headers: SignCallback([]byte("ffffffffffffffffffffffffffffffff"), now, good), Body: good},
		"replayed later": {Headers: SignCallback(key, now.Add(-10*time.Minute), good), Body: good},
		"no headers":     {Body: good},
		"wrong audience": func() CallbackEnvelope {
			b := body(t, func(m map[string]any) { m["audience"] = "kms" })
			return CallbackEnvelope{Headers: SignCallback(key, now, b), Body: b}
		}(),
		"checksum mismatch": func() CallbackEnvelope {
			b := body(t, func(m map[string]any) { m["run"].(map[string]any)["output_checksum"] = "00" })
			return CallbackEnvelope{Headers: SignCallback(key, now, b), Body: b}
		}(),
	}
	for name, cb := range cases {
		if _, err := VerifyHMACCallback(key, cb, now); !errors.Is(err, ErrInvalidCallback) {
			t.Errorf("%s: expected rejection, got %v", name, err)
		}
	}
	if _, err := VerifyHMACCallback([]byte("short"), CallbackEnvelope{Headers: SignCallback([]byte("short"), now, good), Body: good}, now); err == nil {
		t.Error("a missing or short secret must disable callbacks")
	}
}

func TestOutputChecksumIsCanonical(t *testing.T) {
	a, _ := OutputChecksum(json.RawMessage(`{"b":1,"a":[1,2]}`))
	b, _ := OutputChecksum(json.RawMessage(`{ "a": [1,2], "b": 1 }`))
	if a != b || len(a) != 64 {
		t.Fatalf("checksums must ignore key order and whitespace: %s %s", a, b)
	}
}

func TestStubRunsCompleteWithLabelledOutput(t *testing.T) {
	s := NewDevelopmentStub("")
	run, _ := s.StartRun(t.Context(), StartRunRequest{IdempotencyKey: "x#1", AgentID: "sim.intake"})
	got, err := s.GetRun(t.Context(), Scope{}, run.RunID)
	sum, _ := OutputChecksum(got.Output)
	if err != nil || got.Status != RunSucceeded || sum != got.OutputChecksum || string(got.Output) == "" {
		t.Fatalf("stub run: %+v %v", got, err)
	}
	var out map[string]any
	_ = json.Unmarshal(got.Output, &out)
	if out["stub"] != true {
		t.Fatal("stub output must be labelled as fictional")
	}
}
