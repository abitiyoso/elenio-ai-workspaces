package http

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/elenio/control-plane/internal/solutions"
)

func bindingFixture(t *testing.T) (solutionBindingSnapshot, solutionDependency) {
	t.Helper()
	out := solutionDependency{Key: "invoice-review", Version: 1, DefinitionHash: strings.Repeat("a", 64),
		Resources: []solutions.ResourceDependency{{Kind: "agent", Ref: "finance.extract", NodeID: "extract"}}}
	s := solutionBindingSnapshot{Schema: "elenio.workflow-bindings.v1", WorkflowKey: out.Key, Version: out.Version, DefinitionHash: out.DefinitionHash, ObservedAt: time.Now().UTC(),
		Bindings: []solutionBinding{{Kind: "agent", Ref: "finance.extract", NodeID: "extract", Registered: true, RuntimeAgentID: "finance.extract", SkillVersion: "1.0.0", InputContract: "flowcase.case.v1"}}}
	raw, _ := json.Marshal(map[string]any{"workflow_key": s.WorkflowKey, "version": s.Version, "definition_hash": s.DefinitionHash, "bindings": s.Bindings})
	s.ContentHash, _ = solutions.CanonicalSnapshotHash(raw)
	return s, out
}

func TestBindingEvidenceIsExactBoundedAndNeverAcceptsUnknownAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*solutionBindingSnapshot)
		rehash bool
	}{
		{"wrong workflow", func(s *solutionBindingSnapshot) { s.WorkflowKey = "other" }, true},
		{"wrong version", func(s *solutionBindingSnapshot) { s.Version = 2 }, true},
		{"wrong definition", func(s *solutionBindingSnapshot) { s.DefinitionHash = strings.Repeat("b", 64) }, true},
		{"stale", func(s *solutionBindingSnapshot) { s.ObservedAt = time.Now().Add(-2 * time.Minute) }, false},
		{"future", func(s *solutionBindingSnapshot) { s.ObservedAt = time.Now().Add(time.Minute) }, false},
		{"unregistered", func(s *solutionBindingSnapshot) { s.Bindings[0].Registered = false }, true},
		{"changed binding", func(s *solutionBindingSnapshot) { s.Bindings[0].InputContract = "sim.v1" }, false},
		{"unknown contract", func(s *solutionBindingSnapshot) { s.Bindings[0].InputContract = "arbitrary" }, true},
		{"undeclared ref", func(s *solutionBindingSnapshot) { s.Bindings[0].Ref = "another.extract" }, true},
		{"extra binding", func(s *solutionBindingSnapshot) { s.Bindings = append(s.Bindings, s.Bindings[0]) }, true},
		{"missing binding", func(s *solutionBindingSnapshot) { s.Bindings = []solutionBinding{} }, true},
		{"endpoint", func(s *solutionBindingSnapshot) { s.Bindings[0].RuntimeAgentID = "https://private.example" }, true},
		{"cross-kind authority", func(s *solutionBindingSnapshot) { b := false; s.Bindings[0].Provisional = &b }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, out := bindingFixture(t)
			if !validSolutionBindings(s, out, 200) {
				t.Fatal("valid source fixture rejected")
			}
			tc.mutate(&s)
			if tc.rehash {
				raw, _ := json.Marshal(map[string]any{"workflow_key": s.WorkflowKey, "version": s.Version, "definition_hash": s.DefinitionHash, "bindings": s.Bindings})
				s.ContentHash, _ = solutions.CanonicalSnapshotHash(raw)
			}
			if validSolutionBindings(s, out, 200) {
				t.Fatal("invalid source evidence accepted")
			}
		})
	}
	s, out := bindingFixture(t)
	if validSolutionBindings(s, out, 0) {
		t.Fatal("aggregate bound ignored")
	}
}

func TestBindingEvidenceRecognizesOnlyDeclaredCatalogKinds(t *testing.T) {
	provisional := true
	for _, b := range []solutionBinding{
		{Kind: "approval_policy", Ref: "finance.review", NodeID: "review", Registered: true, Provisional: &provisional, SoD: []string{"not_case_owner", "not_task_submitter"}},
		{Kind: "connector", Ref: "erp.notify", NodeID: "notify", Registered: true, Operation: "send"},
		{Kind: "assignee_role", Ref: "finance-reviewer", NodeID: "correct", Registered: true},
	} {
		s, out := bindingFixture(t)
		s.Bindings = []solutionBinding{b}
		out.Resources = []solutions.ResourceDependency{{Kind: b.Kind, Ref: b.Ref, NodeID: b.NodeID}}
		raw, _ := json.Marshal(map[string]any{"workflow_key": s.WorkflowKey, "version": s.Version, "definition_hash": s.DefinitionHash, "bindings": s.Bindings})
		s.ContentHash, _ = solutions.CanonicalSnapshotHash(raw)
		if !validSolutionBindings(s, out, 200) {
			t.Fatalf("declared catalog metadata rejected: %+v", b)
		}
	}
}
