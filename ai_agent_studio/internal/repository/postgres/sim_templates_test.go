package postgres

import (
	"testing"

	"github.com/elenio/agent-studio/internal/simpack"
)

// Every pack agent needs a bootstrap identity, or ApplySIMPack reports it as
// agent_missing_or_archived; the order keeps sim.intake first.
func TestSIMTemplatesMatchPackAgents(t *testing.T) {
	pack, err := simpack.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(simAgentTemplates) != 7 || len(pack.Agents) != len(simAgentTemplates) {
		t.Fatalf("templates=%d pack agents=%d, want 7", len(simAgentTemplates), len(pack.Agents))
	}
	for i, template := range simAgentTemplates {
		if pack.Agents[i].Key != template.key {
			t.Fatalf("template %d is %s, pack agent is %s", i, template.key, pack.Agents[i].Key)
		}
	}
	last := simAgentTemplates[len(simAgentTemplates)-1]
	if last.key != "sim.case_intake" || last.name != "Case Intake Assistant" || last.domain != "case-intake" {
		t.Fatalf("case intake template: %+v", last)
	}
}
