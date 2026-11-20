package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func catalogWith(t *testing.T, cfg string) error {
	t.Helper()
	ps, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Catalog(ps)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "packs.json")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadBindings(path)
	if err != nil {
		return err
	}
	return b.Apply(c)
}

func TestShippedBindingsApply(t *testing.T) {
	ps, _ := Load()
	c, _ := Catalog(ps)
	b, err := LoadBindings("../../config/packs.json")
	if err != nil || b.Apply(c) != nil {
		t.Fatalf("config/packs.json must apply cleanly: %v", err)
	}
	// The SIM agents map to Agent Studio agent keys; "unpinned" means the active release.
	for ref, key := range map[string]string{"sim.intake": "sim.intake", "sim.solution-matching": "sim.solution_matching",
		"sim.standard-project": "sim.standard_project", "sim.design": "sim.design", "sim.cost-challenge": "sim.cost_challenge", "sim.proposal": "sim.proposal"} {
		a, _ := c.Agent(ref)
		if a.RuntimeAgentID != key || a.SkillVersion != "unpinned" {
			t.Fatalf("agent mapping not applied for %s: %+v", ref, a)
		}
	}
	for _, p := range c.Snapshot().Policies {
		if !p.Provisional || p.Ratification != nil {
			t.Fatalf("no policy may be presented as ratified without a cited decision: %+v", p)
		}
	}
}

func TestBindingsRatificationNeedsCitation(t *testing.T) {
	ps, _ := Load()
	c, _ := Catalog(ps)
	b := &Bindings{Policies: map[string]PolicyBinding{"poc.budget": {}}}
	if err := b.Apply(c); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Policy("poc.budget"); !p.Provisional {
		t.Fatal("bindings without ratification keep the policy provisional")
	}
	ok := `{"policies":{"poc.budget":{"ratification":{"reference":"DoA-2026-14","date":"2026-10-28","by":"Finance committee"}}}}`
	if err := catalogWith(t, ok); err != nil {
		t.Fatalf("cited ratification rejected: %v", err)
	}
}

func TestBindingsAcceptUnderscoreRuntimeIDs(t *testing.T) {
	if err := catalogWith(t, `{"agents":{"sim.cost-challenge":{"runtime_agent_id":"sim.cost_challenge","skill_version":"1.2.0"}}}`); err != nil {
		t.Fatalf("underscored runtime id rejected: %v", err)
	}
	if err := catalogWith(t, `{"agents":{"sim.cost-challenge":{"runtime_agent_id":"sim cost_challenge"}}}`); err == nil {
		t.Fatal("runtime ids with spaces must still be rejected")
	}
}

func TestBindingsRejectMistakes(t *testing.T) {
	for name, cfg := range map[string]string{
		"unknown agent":          `{"agents":{"sim.orchestrator":{"runtime_agent_id":"x"}}}`,
		"unknown input contract": `{"agents":{"sim.intake":{"input_contract":"browser-defined"}}}`,
		"unknown policy":         `{"policies":{"sim.anything":{}}}`,
		"unknown sod rule":       `{"policies":{"poc.budget":{"sod":["four_eyes_light"]}}}`,
		"bad runtime id":         `{"agents":{"sim.intake":{"runtime_agent_id":"https://evil.example/agent?x=1 2"}}}`,
		"vague ratification":     `{"policies":{"poc.budget":{"ratification":{"reference":"ok","date":"soon","by":"me"}}}}`,
		"unknown field":          `{"agents":{},"approvers":{"poc.budget":["alice"]}}`,
		"not json":               `agents: yes`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := catalogWith(t, cfg); err == nil || !strings.Contains(err.Error(), "PACKS_CONFIG") {
				t.Fatalf("expected PACKS_CONFIG error, got %v", err)
			}
		})
	}
}

func TestInputContractIsDeploymentOwnedAndValidated(t *testing.T) {
	for _, contract := range []string{"sim.v1", "flowcase.case.v1"} {
		ps, _ := Load()
		c, _ := Catalog(ps)
		b := &Bindings{Agents: map[string]AgentBinding{"sim.intake": {InputContract: contract}}}
		if err := b.Apply(c); err != nil {
			t.Fatal(err)
		}
		a, _ := c.Agent("sim.intake")
		if a.InputContract != contract {
			t.Fatalf("binding lost: %+v", a)
		}
	}
}
