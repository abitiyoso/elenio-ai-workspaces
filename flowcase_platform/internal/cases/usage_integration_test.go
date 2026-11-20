package cases_test

import (
	"testing"

	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
)

func TestCaseUsageSnapshotNeverSubstitutesForResultReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name, agentID, schema string
		status                agent.RunStatus
		wantStatus            string
	}{
		{"wrong agent success", "unrelated.agent", "elenio.agent-usage.v1", agent.RunSucceeded, "failed"},
		{"wrong agent failure", "unrelated.agent", "elenio.agent-usage.v1", agent.RunFailed, "failed"},
		{"invalid usage", "sim.design", "unknown.schema", agent.RunSucceeded, "succeeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			rt := &testRuntime{}
			e.phase3(rt)
			e.publishParallelWorkflow()
			id := e.createCase("two-agents", nil)
			e.drain()
			runID := rt.runID("sim.design", 1)
			usage := &agent.UsageObservation{Schema: tc.schema, Class: "test", Coverage: "complete", TokensIn: 50, TokensOut: 5, Attempts: 1, ObservedAttempts: 1, PricingStatus: "unpriced"}
			outcome, err := e.eng.HandleAgentCallback(e.ctx, "test", signedCallback(t, e.tenant, "usage-"+tc.name, runID, tc.agentID, tc.status, `{"draft":"fixture"}`, usage))
			if err != nil || outcome != cases.CallbackApplied {
				t.Fatalf("callback: %s %v", outcome, err)
			}
			found := false
			for _, run := range e.detail(id).AgentRuns {
				if run.RuntimeRunID != runID {
					continue
				}
				found = true
				if run.Status != tc.wantStatus || run.UsageObservation != nil || run.UsageReceivedAt != nil {
					t.Fatalf("unverified usage retained or execution changed: %+v", run)
				}
			}
			if !found {
				t.Fatal("source run missing")
			}
			if e.detail(id).Status == cases.CaseCompleted {
				t.Fatal("usage bypassed independent business approval")
			}
		})
	}
}
