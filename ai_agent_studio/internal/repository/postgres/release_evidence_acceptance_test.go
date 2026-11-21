package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/elenio/agent-studio/internal/repository/postgres"
)

func TestReleaseEvidenceUsesIndependentDomainRecordsAndCurrentModelConfig(t *testing.T) {
	db, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenant := acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenant)
	configureVersion(t, registry, tenant, version.ID, "1.0.0", "private-release-canary")
	modelID := approvedModel(t, registry, tenant, version.ID)
	_, skill, err := registry.CreateSkill(ctx, tenant, agent.Key, postgres.SkillInput{Key: "evidence.skill", Domain: "acceptance", CreatedBy: "author"})
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.BindSkill(ctx, tenant, version.ID, skill.ID, "author"); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.ObserveRelease(ctx, tenant, agent.Key, "", "production"); !errors.Is(err, postgres.ErrNoActiveRelease) {
		t.Fatal("unreleased agent observed", err)
	}
	request := publishVersion(t, registry, tenant, version.ID, evalSuite(t, registry, tenant, agent.Key))
	count := func() int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM agent_runs WHERE tenant_id=$1::uuid)+(SELECT count(*) FROM audit_events WHERE tenant_id=$1::uuid)`, tenant).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	first, err := registry.ObserveRelease(ctx, tenant, agent.Key, "1.0.0", "production")
	if err != nil || first.State != "verified" || first.RequestID != request.ID || !first.IndependentApproval || !first.EvaluationPrimaryConfigMatches || first.PrimaryModel == nil || !first.PrimaryModel.Approved || len(first.Skills) != 1 {
		t.Fatalf("independent release observation: %+v %v", first, err)
	}
	if first.Skills[0].State != "published" || first.Skills[0].Checksum == "" {
		t.Fatal("skill pin missing")
	}
	again, err := registry.ObserveRelease(ctx, tenant, agent.Key, version.ID, "production")
	if err != nil || again.ContentHash != first.ContentHash || count() != before {
		t.Fatal("read changed source records or digest", err)
	}
	raw, _ := json.Marshal(first)
	for _, private := range []string{"private-release-canary", "vault://", "base_url", "secret_ref", "instructions", "input_schema", "https://"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("private config exported: %s", private)
		}
	}
	if _, err = registry.ObserveRelease(ctx, acceptanceUUID(t), agent.Key, "", "production"); !errors.Is(err, postgres.ErrNoActiveRelease) {
		t.Fatal("foreign tenant observation", err)
	}
	if _, err = registry.ObserveRelease(ctx, tenant, agent.Key, "9.9.9", "production"); !errors.Is(err, postgres.ErrVersionMismatch) {
		t.Fatal("wrong version pin observed", err)
	}
	if _, err = registry.SetModelApproval(ctx, tenant, modelID, "production", "revoked", "fixture revocation", "admin"); err != nil {
		t.Fatal(err)
	}
	revoked, err := registry.ObserveRelease(ctx, tenant, agent.Key, "", "production")
	if err != nil || revoked.State != "blocked" || revoked.PrimaryModel.Approved || revoked.ContentHash == first.ContentHash {
		t.Fatalf("revocation hidden: %+v %v", revoked, err)
	}
	if _, err = registry.SetModelApproval(ctx, tenant, modelID, "production", "approved", "fixture restore", "admin"); err != nil {
		t.Fatal(err)
	}
	var provider string
	if err = db.QueryRowContext(ctx, `SELECT provider_id::text FROM llm_model_deployments WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenant, modelID).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.SetProviderCredentialRef(ctx, tenant, provider, "vault://fixture/rotated", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.RecordProviderProbe(ctx, tenant, provider, modelID, "admin", "passed", json.RawMessage(`{"capabilities":{"json":true,"tools":true}}`), ""); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.SetModelApproval(ctx, tenant, modelID, "production", "approved", "fixture reprobe", "admin"); err != nil {
		t.Fatal(err)
	}
	rotated, err := registry.ObserveRelease(ctx, tenant, agent.Key, "", "production")
	if err != nil || rotated.State != "blocked" || !rotated.PrimaryModel.Approved || rotated.EvaluationPrimaryConfigMatches || !slices.Contains(rotated.Blockers, "evaluation_primary_config_unverified") {
		t.Fatalf("approved config drift substituted for evaluated config: %+v %v", rotated, err)
	}
}

func TestReleaseEvidencePreservesApprovedRollbackAndRejectsDevelopmentExemption(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenant := acceptanceUUID(t)
	agent, v1 := createAcceptanceAgent(t, registry, tenant)
	configureVersion(t, registry, tenant, v1.ID, "1.0.0", "first")
	model := approvedModel(t, registry, tenant, v1.ID)
	suite := evalSuite(t, registry, tenant, agent.Key)
	original := publishVersion(t, registry, tenant, v1.ID, suite)
	v2, err := registry.CreateAgentVersion(ctx, tenant, agent.Key, postgres.AgentVersionInput{Semver: "1.1.0", Manifest: json.RawMessage(`{"configured":true}`), InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(outputSchema), CreatedBy: "author"})
	if err != nil {
		t.Fatal(err)
	}
	bindModel(t, registry, tenant, v2.ID, model)
	publishVersion(t, registry, tenant, v2.ID, suite)
	releases, err := registry.ListReleases(ctx, tenant, agent.Key)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := registry.RollbackRelease(ctx, tenant, releases[0].ID, "ops", "fixture rollback")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := registry.ObserveRelease(ctx, tenant, agent.Key, "1.0.0", "production")
	if err != nil || evidence.State != "verified" || evidence.ReleaseID != restored.ID || evidence.RequestID != original.ID {
		t.Fatalf("rollback approval provenance lost: %+v %v", evidence, err)
	}
	registry.SetSelfApprovalExempt([]string{"demo"})
	other, version := createAcceptanceAgent(t, registry, tenant)
	configureVersion(t, registry, tenant, version.ID, "1.0.0", "demo")
	approvedModel(t, registry, tenant, version.ID)
	run, err := registry.StartEvalRun(ctx, tenant, evalSuite(t, registry, tenant, other.Key), version.ID, "trainer", false)
	if err != nil {
		t.Fatal(err)
	}
	settleEval(t, registry, tenant, run, "")
	req, err := registry.CreateReleaseRequest(ctx, tenant, version.ID, run.ID, "production", "fixture", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.ReviewRelease(ctx, tenant, req.ID, "demo", true, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.DecideRelease(ctx, tenant, req.ID, "demo", true, "fixture"); err != nil {
		t.Fatal(err)
	}
	exempt, err := registry.ObserveRelease(ctx, tenant, other.Key, "", "production")
	if err != nil || exempt.State != "blocked" || exempt.IndependentApproval || !exempt.SelfApprovalExempt || !slices.Contains(exempt.Blockers, "independent_release_approval_unverified") {
		t.Fatalf("development exemption treated as independent: %+v %v", exempt, err)
	}
}

func TestReleaseEvidenceDoesNotInventFallbackEvaluationConfig(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenant := acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenant)
	configureVersion(t, registry, tenant, version.ID, "1.0.0", "fallback fixture")
	primary := approvedModel(t, registry, tenant, version.ID)
	fallback := approvedModel(t, registry, tenant, version.ID)
	if _, err := registry.SetAgentModelBinding(ctx, tenant, version.ID, primary, fallback, "development", "author", nil); err != nil {
		t.Fatal(err)
	}
	publishVersion(t, registry, tenant, version.ID, evalSuite(t, registry, tenant, agent.Key))
	evidence, err := registry.ObserveRelease(ctx, tenant, agent.Key, "", "production")
	if err != nil || evidence.State != "blocked" || evidence.PrimaryModel == nil || !evidence.PrimaryModel.Approved || evidence.FallbackModel == nil || !evidence.FallbackModel.Approved ||
		!slices.Contains(evidence.Blockers, "evaluation_fallback_config_unverified") {
		t.Fatalf("missing fallback pin invented: %+v %v", evidence, err)
	}
	if _, err = registry.SetModelApproval(ctx, tenant, fallback, "production", "revoked", "fixture", "admin"); err != nil {
		t.Fatal(err)
	}
	revoked, err := registry.ObserveRelease(ctx, tenant, agent.Key, "", "production")
	if err != nil || revoked.FallbackModel.Approved || !slices.Contains(revoked.Blockers, "fallback_model_not_currently_approved") {
		t.Fatalf("fallback revocation hidden: %+v %v", revoked, err)
	}
}
