package agent

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestUsageObservationRejectsUnknownOrContradictoryCounters(t *testing.T) {
	good := UsageObservation{Schema: "elenio.agent-usage.v1", Class: "actual", Coverage: "complete", TokensIn: 120, TokensOut: 24, Attempts: 2, ObservedAttempts: 2, PricingStatus: "unpriced"}
	for _, tc := range []struct {
		name   string
		mutate func(*UsageObservation)
		valid  bool
	}{
		{"complete", func(o *UsageObservation) {}, true},
		{"partial", func(o *UsageObservation) { o.Coverage = "partial"; o.ObservedAttempts = 1 }, true},
		{"missing", func(o *UsageObservation) { o.Coverage = "unavailable"; o.TokensIn = 0; o.TokensOut = 0 }, true},
		{"legacy", func(o *UsageObservation) { o.Coverage = "legacy_final_attempt"; o.ObservedAttempts = 0 }, true},
		{"unknown schema", func(o *UsageObservation) { o.Schema = "other" }, false},
		{"unknown class", func(o *UsageObservation) { o.Class = "billable" }, false},
		{"invented price", func(o *UsageObservation) { o.PricingStatus = "priced" }, false},
		{"negative", func(o *UsageObservation) { o.TokensIn = -1 }, false},
		{"unsafe integer", func(o *UsageObservation) { o.TokensOut = 9007199254740992 }, false},
		{"future attempt", func(o *UsageObservation) { o.ObservedAttempts = 3 }, false},
		{"incomplete", func(o *UsageObservation) { o.ObservedAttempts = 1 }, false},
		{"unknown coverage", func(o *UsageObservation) { o.Coverage = "unknown" }, false},
		{"missing positive", func(o *UsageObservation) { o.Coverage = "unavailable" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := good
			tc.mutate(&o)
			if o.Valid() != tc.valid {
				t.Fatalf("validation=%v for %+v", o.Valid(), o)
			}
		})
	}
	if (*UsageObservation)(nil).Valid() {
		t.Fatal("nil observation is not reported usage")
	}
}

func TestCallbackSignatureCoversUsageMetadata(t *testing.T) {
	now := time.Now()
	good := body(t, func(m map[string]any) {
		m["run"].(map[string]any)["usage_observation"] = &UsageObservation{
			Schema: "elenio.agent-usage.v1", Class: "test", Coverage: "complete", Attempts: 1, ObservedAttempts: 1, PricingStatus: "unpriced",
		}
	})
	headers := SignCallback(key, now, good)
	got, err := VerifyHMACCallback(key, CallbackEnvelope{Headers: headers, Body: good}, now)
	if err != nil || !got.Run.UsageObservation.Valid() {
		t.Fatalf("signed snapshot: %+v %v", got, err)
	}
	var tampered map[string]any
	_ = json.Unmarshal(good, &tampered)
	tampered["run"].(map[string]any)["usage_observation"].(map[string]any)["tokens_in"] = 999
	changed, _ := json.Marshal(tampered)
	if _, err = VerifyHMACCallback(key, CallbackEnvelope{Headers: headers, Body: changed}, now); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("tampered usage accepted: %v", err)
	}
}
