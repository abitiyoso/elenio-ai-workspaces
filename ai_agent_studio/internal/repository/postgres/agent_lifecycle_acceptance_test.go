package postgres_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/elenio/agent-studio/internal/repository/postgres"
)

func TestAgentCloneStartsUnconfiguredWithoutBindingsAndPaginationCounts(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	tenantID := acceptanceUUID(t)
	source, sourceVersion := createAcceptanceAgent(t, registry, tenantID)

	if _, err := registry.UpdateAgentMetadata(context.Background(), tenantID, source.Key, "Updated source", "acceptance", "test-user"); err != nil {
		t.Fatalf("update draft agent metadata: %v", err)
	}
	if _, err := registry.UpdateAgentVersion(context.Background(), tenantID, sourceVersion.ID, postgres.AgentVersionInput{
		Semver: "0.1.0", Manifest: json.RawMessage(`{"configured":true,"prompt":"preserve content"}`),
		InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`),
		ModelPolicy: json.RawMessage(`{"deployment":"must-not-copy"}`), ToolPolicy: json.RawMessage(`{"tools":["must-not-copy"]}`),
	}, "test-user"); err != nil {
		t.Fatalf("prepare source draft version: %v", err)
	}

	skill, skillVersion, err := registry.CreateSkill(context.Background(), tenantID, source.Key, postgres.SkillInput{
		Key: "acceptance.skill", Domain: "acceptance", CreatedBy: "test-user",
	})
	if err != nil {
		t.Fatalf("create source skill: %v", err)
	}
	if err := registry.BindSkill(context.Background(), tenantID, sourceVersion.ID, skillVersion.ID, "test-user"); err != nil {
		t.Fatalf("bind source skill: %v", err)
	}
	if skill.ID == "" {
		t.Fatal("created skill has no identifier")
	}

	clone, err := registry.CloneAgent(context.Background(), tenantID, source.Key, postgres.AgentInput{
		Key: "clone." + acceptanceUUID(t)[:8], DisplayName: "Cloned draft", Domain: "acceptance", CreatedBy: "test-user",
	})
	if err != nil {
		t.Fatalf("clone agent: %v", err)
	}
	versions, err := registry.ListAgentVersions(context.Background(), tenantID, clone.Key)
	if err != nil || len(versions) != 1 {
		t.Fatalf("list cloned versions: versions=%d err=%v", len(versions), err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(versions[0].Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["configured"] != false || manifest["prompt"] != "preserve content" {
		t.Fatalf("clone must preserve definition content while marking it unconfigured: %v", manifest)
	}
	if string(versions[0].ModelPolicy) != "{}" || string(versions[0].ToolPolicy) != "{}" {
		t.Fatalf("clone copied model/tool authority: model=%s tool=%s", versions[0].ModelPolicy, versions[0].ToolPolicy)
	}
	clonedSkills, err := registry.ListAgentSkillBindings(context.Background(), tenantID, versions[0].ID)
	if err != nil || len(clonedSkills) != 0 {
		t.Fatalf("clone copied skill bindings: bindings=%+v err=%v", clonedSkills, err)
	}
	clonedTools, err := registry.ListAgentToolBindings(context.Background(), tenantID, versions[0].ID)
	if err != nil || len(clonedTools) != 0 {
		t.Fatalf("clone copied tool bindings: bindings=%+v err=%v", clonedTools, err)
	}
	if _, err := registry.GetAgentModelBinding(context.Background(), tenantID, versions[0].ID); err == nil {
		t.Fatal("clone copied a model binding")
	}

	for i := 0; i < 2; i++ {
		_, err := registry.CreateAgent(context.Background(), tenantID, postgres.AgentInput{
			Key: "page." + acceptanceUUID(t)[:8], DisplayName: "Page test", Domain: "acceptance", CreatedBy: "test-user",
		})
		if err != nil {
			t.Fatalf("create paginated agent %d: %v", i, err)
		}
	}
	page, total, err := registry.ListAgents(context.Background(), tenantID, 2, 1)
	if err != nil || total != 4 || len(page) != 2 {
		t.Fatalf("agent pagination mismatch: total=%d page=%d err=%v", total, len(page), err)
	}
}

func TestAcceptanceSIMBootstrapIsIdempotentAndTenantLocal(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	tenantA, tenantB := acceptanceUUID(t), acceptanceUUID(t)
	first, err := registry.BootstrapSIMAgents(context.Background(), tenantA, "test-user")
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if len(first.Agents) != 7 {
		t.Fatalf("bootstrap created %d agents, want 7", len(first.Agents))
	}
	keys := make(map[string]string, 7)
	for _, agent := range first.Agents {
		if _, dup := keys[agent.Key]; dup {
			t.Fatalf("bootstrap returned duplicate key %s", agent.Key)
		}
		keys[agent.Key] = agent.ID
	}
	second, err := registry.BootstrapSIMAgents(context.Background(), tenantA, "test-user")
	if err != nil {
		t.Fatalf("repeated bootstrap: %v", err)
	}
	for _, agent := range second.Agents {
		if keys[agent.Key] != agent.ID {
			t.Fatalf("repeated bootstrap replaced %s", agent.Key)
		}
		versions, err := registry.ListAgentVersions(context.Background(), tenantA, agent.Key)
		if err != nil || len(versions) != 1 {
			t.Fatalf("%s has %d draft versions after repeat bootstrap: %v", agent.Key, len(versions), err)
		}
	}
	listed, total, err := registry.ListAgents(context.Background(), tenantB, 100, 0)
	if err != nil || total != 0 || len(listed) != 0 {
		t.Fatalf("tenant B saw tenant A's bootstrap: total=%d err=%v", total, err)
	}
}
