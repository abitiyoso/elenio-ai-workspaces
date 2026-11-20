package postgres

import (
	"testing"
	"time"
)

func TestRunUsageCoverageDoesNotTreatUnknownAttemptsAsZero(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name string
		run  Run
		want string
	}{
		{"new queued", Run{}, "unavailable"},
		{"legacy terminal", Run{Attempts: 2, TokensIn: 10, CompletedAt: &now}, "legacy_final_attempt"},
		{"blocked before inference", Run{Attempts: 1, CompletedAt: &now}, "unavailable"},
		{"reported zero is known", Run{Attempts: 1, Usage: []RunUsage{{Attempt: 1, UsageReports: 1}}}, "complete"},
		{"all absent", Run{Attempts: 1, Usage: []RunUsage{{Attempt: 1}}}, "unavailable"},
		{"missing retry", Run{Attempts: 2, Usage: []RunUsage{{Attempt: 1, UsageReports: 1}}}, "partial"},
		{"timeout then reported", Run{Attempts: 2, Usage: []RunUsage{{Attempt: 1}, {Attempt: 2, UsageReports: 1}}}, "partial"},
		{"missing one model reply", Run{Attempts: 1, Usage: []RunUsage{{Attempt: 1, UsageReports: 1, MissingUsage: 1}}}, "partial"},
		{"all retries known", Run{Attempts: 2, Usage: []RunUsage{{Attempt: 1, UsageReports: 1}, {Attempt: 2, UsageReports: 2}}}, "complete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := runUsageCoverage(test.run); got != test.want {
				t.Fatalf("coverage=%s want %s", got, test.want)
			}
		})
	}
}

func TestUsageSnapshotKeepsSourceClassAndMissingCoverage(t *testing.T) {
	for _, tc := range []struct{ kind, mode, class string }{
		{"production", "provider", "actual"}, {"sandbox", "provider", "test"},
		{"evaluation", "provider", "test"}, {"sandbox", "dev_mock", "mock"},
	} {
		got := ObserveRunUsage(Run{Kind: tc.kind, ModelMode: tc.mode, Attempts: 2,
			TokensIn: 30, TokensOut: 4, Usage: []RunUsage{{Attempt: 1, UsageReports: 1}}})
		if got == nil || got.Class != tc.class || got.Coverage != "partial" ||
			got.TokensIn != 30 || got.TokensOut != 4 || got.Attempts != 2 ||
			got.ObservedAttempts != 1 || got.PricingStatus != "unpriced" || got.Schema != "elenio.agent-usage.v1" {
			t.Fatalf("%s/%s snapshot: %+v", tc.kind, tc.mode, got)
		}
	}
	if ObserveRunUsage(Run{}) != nil {
		t.Fatal("unknown source mode must not invent usage")
	}
}
