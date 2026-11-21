package http

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/elenio/control-plane/internal/solutions"
)

func rehashAgentEvidence(t *testing.T, e *sourceAgentEvidence) {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "observed_at")
	delete(fields, "content_hash")
	raw, _ = json.Marshal(fields)
	e.ContentHash, err = solutions.CanonicalSnapshotHash(raw)
	if err != nil {
		t.Fatal(err)
	}
}
func sourceAgentFixture(t *testing.T) (sourceAgentEvidence, solutionAgentObservation) {
	t.Helper()
	expected := solutionAgentObservation{RuntimeAgentID: "finance.extract", ExpectedVersion: "1.0.0"}
	e := sourceAgentEvidence{Schema: "elenio.agent-release-evidence.v1", AgentKey: expected.RuntimeAgentID, Environment: "production",
		ReleaseID: "00000000-0000-4000-8000-000000000601", AgentVersion: sourceDependencyPin{ID: "00000000-0000-4000-8000-000000000602", Version: "1.0.0", State: "published", Checksum: strings.Repeat("a", 64)},
		Fingerprint: strings.Repeat("b", 64), RequestID: "00000000-0000-4000-8000-000000000603", EvaluationID: "00000000-0000-4000-8000-000000000604",
		EvaluationGate: "pass", IndependentApproval: true, EvaluationPrimaryConfigMatches: true,
		PrimaryModel: &sourceModelPin{ID: "00000000-0000-4000-8000-000000000605", ConfigVersion: 3, Approved: true},
		Skills:       []sourceDependencyPin{}, Tools: []sourceDependencyPin{}, Blockers: []string{}, State: "verified", ObservedAt: time.Now().UTC()}
	rehashAgentEvidence(t, &e)
	return e, expected
}

func TestSourceAgentEvidenceRequiresExactPinsAndUnwaivedCurrentGovernance(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*sourceAgentEvidence)
		rehash bool
	}{
		{"different agent", func(e *sourceAgentEvidence) { e.AgentKey = "other.agent" }, true},
		{"wrong version", func(e *sourceAgentEvidence) { e.AgentVersion.Version = "2.0.0" }, true},
		{"old source", func(e *sourceAgentEvidence) { e.ObservedAt = time.Now().Add(-2 * time.Minute) }, false},
		{"future source", func(e *sourceAgentEvidence) { e.ObservedAt = time.Now().Add(time.Minute) }, false},
		{"changed model", func(e *sourceAgentEvidence) { e.PrimaryModel.ConfigVersion = 4 }, false},
		{"model revoked", func(e *sourceAgentEvidence) { e.PrimaryModel.Approved = false }, true},
		{"missing primary", func(e *sourceAgentEvidence) { e.PrimaryModel = nil }, true},
		{"model config not evaluated", func(e *sourceAgentEvidence) { e.EvaluationPrimaryConfigMatches = false }, true},
		{"waived approval", func(e *sourceAgentEvidence) { e.SelfApprovalExempt = true }, true},
		{"no independent review", func(e *sourceAgentEvidence) { e.IndependentApproval = false }, true},
		{"failed evaluation", func(e *sourceAgentEvidence) { e.EvaluationGate = "blocked" }, true},
		{"missing evaluation", func(e *sourceAgentEvidence) { e.EvaluationID = "" }, true},
		{"invalid fingerprint", func(e *sourceAgentEvidence) { e.Fingerprint = "" }, true},
		{"blocker ignored", func(e *sourceAgentEvidence) { e.Blockers = []string{"unresolved"} }, true},
		{"sensitive blocker", func(e *sourceAgentEvidence) { e.State = "blocked"; e.Blockers = []string{"https://private.invalid"} }, true},
		{"draft version", func(e *sourceAgentEvidence) { e.AgentVersion.State = "draft" }, true},
		{"invalid pins", func(e *sourceAgentEvidence) {
			e.Skills = []sourceDependencyPin{{ID: "wrong", Version: "1.0.0", State: "published"}}
		}, true},
		{"unknown fallback proof", func(e *sourceAgentEvidence) { e.FallbackModel = e.PrimaryModel }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, expected := sourceAgentFixture(t)
			if !validSourceAgentEvidence(e, expected) {
				t.Fatal("valid source evidence rejected")
			}
			tc.mutate(&e)
			if tc.rehash {
				rehashAgentEvidence(t, &e)
			}
			if validSourceAgentEvidence(e, expected) {
				t.Fatal("invalid source metadata accepted")
			}
		})
	}
	e, expected := sourceAgentFixture(t)
	expected.ExpectedVersion = e.AgentVersion.ID
	if !validSourceAgentEvidence(e, expected) {
		t.Fatal("exact UUID pin rejected")
	}
	e.State = "blocked"
	e.PrimaryModel.Approved = false
	e.Blockers = []string{"primary_model_not_currently_approved"}
	rehashAgentEvidence(t, &e)
	if !validSourceAgentEvidence(e, expected) {
		t.Fatal("negative source facts were hidden instead of retained")
	}
}

func TestAggregateEvidenceBudgetOmitsWholeSnapshotsWithoutInventingApproval(t *testing.T) {
	e, _ := sourceAgentFixture(t)
	out := solutionReadiness{Workflows: []solutionDependency{{Key: "large", Version: 1, BindingSnapshot: &solutionBindingSnapshot{Bindings: []solutionBinding{}}}},
		AgentReleases: []solutionAgentObservation{{RuntimeAgentID: "finance.extract", Evidence: &e}}, Blockers: []string{}}
	for i := 0; i < 200; i++ {
		out.Workflows[0].Resources = append(out.Workflows[0].Resources, solutions.ResourceDependency{Kind: "agent", Ref: strings.Repeat("a", 128), NodeID: strings.Repeat("b", 128), Version: strings.Repeat("c", 128), Reason: strings.Repeat("r", 128), State: "blocked"})
		out.Workflows[0].BindingSnapshot.Bindings = append(out.Workflows[0].BindingSnapshot.Bindings, solutionBinding{Kind: "agent", Ref: strings.Repeat("a", 128), NodeID: strings.Repeat("b", 128), RuntimeAgentID: strings.Repeat("c", 128), SkillVersion: strings.Repeat("d", 128)})
	}
	for i := 0; i < 99; i++ {
		out.Workflows = append(out.Workflows, solutionDependency{Key: strings.Repeat("w", 63), Version: i + 1, State: "blocked", Reason: strings.Repeat("r", 500), BindingReason: strings.Repeat("r", 500)})
	}
	boundAgentReport(&out)
	raw, _ := json.Marshal(out)
	if len(raw) > 180<<10 || out.ReleaseReady || out.AgentReleases[0].Evidence != nil || out.Workflows[0].BindingSnapshot != nil || len(out.Workflows[0].Resources) != 0 || out.Workflows[0].InventoryComplete {
		t.Fatal("evidence budget retained truncated or oversized authority", len(raw))
	}
}
