package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/elenio/agent-studio/internal/repository/postgres"
)

func TestUsageLedgerRetainsRetriesWithoutDoubleCountingOrCrossTenantReads(t *testing.T) {
	db, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID, otherTenant := acceptanceUUID(t), acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, version.ID, "0.1.0", "usage contract fixture")
	approvedModel(t, registry, tenantID, version.ID)
	run, _, err := registry.CreateRun(ctx, tenantID, postgres.RunCreate{AgentKey: agent.Key, AgentVersionID: version.ID, Kind: "sandbox", CallerKind: "user", CallerSubject: "usage-fixture", IdempotencyKey: "usage-contract-retry-01", Input: json.RawMessage(`{}`), Evidence: json.RawMessage(`[]`)})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		exec, err := registry.BeginRunExecution(ctx, tenantID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		usage := postgres.RunUsage{Attempt: attempt, DeploymentID: exec.Model.DeploymentID, ConfigVersion: exec.Model.ConfigVersion, TokensIn: attempt * 100, TokensOut: attempt * 10, ToolCalls: attempt, UsageReports: 1}
		if err = registry.RecordRunUsage(ctx, tenantID, run.ID, usage); err != nil {
			t.Fatal(err)
		}
		if err = registry.RecordRunUsage(ctx, tenantID, run.ID, usage); err != nil {
			t.Fatal("identical replay", err)
		}
		changed := usage
		changed.TokensIn++
		if err = registry.RecordRunUsage(ctx, tenantID, run.ID, changed); !errors.Is(err, postgres.ErrInvalidState) {
			t.Fatal("conflicting observation replaced ledger", err)
		}
		if attempt == 1 {
			if err = registry.RequeueRun(ctx, tenantID, run.ID, "provider_unavailable", "provider_timeout", 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	settled, err := registry.FinishRun(ctx, tenantID, run.ID, postgres.RunResult{Status: "succeeded", Output: json.RawMessage(`{"summary":"fixture"}`), TokensIn: 200, TokensOut: 20, ToolCalls: 2})
	if err != nil {
		t.Fatal(err)
	}
	if settled.TokensIn != 300 || settled.TokensOut != 30 || settled.ToolCalls != 3 || len(settled.Usage) != 2 || settled.UsageCoverage != "complete" {
		t.Fatalf("retry-inclusive observation totals: %+v", settled)
	}
	if _, err := registry.GetRun(ctx, otherTenant, run.ID); !errors.Is(err, postgres.ErrRunNotFound) {
		t.Fatalf("cross-tenant usage read: %v", err)
	}
	if err := registry.RecordRunUsage(ctx, otherTenant, run.ID, postgres.RunUsage{Attempt: 1}); !errors.Is(err, postgres.ErrRunNotFound) {
		t.Fatalf("cross-tenant usage write: %v", err)
	}
	if err := registry.RecordRunUsage(ctx, tenantID, run.ID, postgres.RunUsage{Attempt: 3}); !errors.Is(err, postgres.ErrInvalidState) {
		t.Fatal("unstarted future attempt recorded", err)
	}
	for _, query := range []string{`UPDATE agent_run_usage SET tokens_in=0 WHERE run_id=$1::uuid`, `DELETE FROM agent_run_usage WHERE run_id=$1::uuid`} {
		if _, err := db.ExecContext(ctx, query, run.ID); err == nil {
			t.Fatal("usage mutation was allowed")
		}
	}
	role := "usage_rls_" + strings.ReplaceAll(acceptanceUUID(t), "-", "")
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s NOLOGIN", role)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP OWNED BY %s; DROP ROLE %s", role, role))
	})
	if _, err := db.ExecContext(ctx, fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s; GRANT SELECT,INSERT ON agent_run_usage TO %s", role, role)); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SET LOCAL ROLE "+role); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenantID); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_run_usage WHERE run_id=$1::uuid`, run.ID).Scan(&visible); err != nil || visible != 2 {
		t.Fatalf("RLS own rows=%d: %v", visible, err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, otherTenant); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_run_usage WHERE run_id=$1::uuid`, run.ID).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("RLS foreign rows=%d: %v", visible, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_run_usage(tenant_id,run_id,attempt,config_version,tokens_in,tokens_out,tool_calls,usage_reports,missing_usage) VALUES($1::uuid,$2::uuid,3,0,0,0,0,0,0)`, tenantID, run.ID)
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("RLS cross-tenant write: %v", err)
	}
	_ = tx.Rollback()
	metrics, err := registry.RunMetrics(ctx, tenantID, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.TokensIn != 300 || metrics.TokensOut != 30 || metrics.UsageByClass["test"].TokensIn != 300 || metrics.UsageByClass["test"].IncompleteRuns != 0 {
		t.Fatalf("retry usage rollup: %+v", metrics)
	}
	if _, exists := metrics.UsageByClass["actual"]; exists {
		t.Fatal("sandbox observations mislabeled actual business usage")
	}
}

func TestUsageLedgerShowsPartialCoverageAfterUnreportedAttempt(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, version.ID, "0.1.0", "partial usage fixture")
	approvedModel(t, registry, tenantID, version.ID)
	run, _, err := registry.CreateRun(ctx, tenantID, postgres.RunCreate{AgentKey: agent.Key, AgentVersionID: version.ID, Kind: "sandbox", CallerKind: "user", CallerSubject: "usage-fixture", IdempotencyKey: "usage-unknown-attempt-01", Input: json.RawMessage(`{}`), Evidence: json.RawMessage(`[]`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.BeginRunExecution(ctx, tenantID, run.ID); err != nil {
		t.Fatal(err)
	}
	if err = registry.RecordRunUsage(ctx, tenantID, run.ID, postgres.RunUsage{Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if err = registry.RequeueRun(ctx, tenantID, run.ID, "provider_unavailable", "provider_timeout", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.BeginRunExecution(ctx, tenantID, run.ID); err != nil {
		t.Fatal(err)
	}
	if err = registry.RecordRunUsage(ctx, tenantID, run.ID, postgres.RunUsage{Attempt: 2, TokensIn: 50, TokensOut: 10, UsageReports: 1}); err != nil {
		t.Fatal(err)
	}
	observed, err := registry.GetRun(ctx, tenantID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if observed.TokensIn != 50 || observed.UsageCoverage != "partial" {
		t.Fatalf("unknown attempt silently became zero: %+v", observed)
	}
	metrics, err := registry.RunMetrics(ctx, tenantID, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.UsageByClass["test"].IncompleteRuns != 1 || metrics.TokensIn != 50 {
		t.Fatalf("active partial usage was excluded: %+v", metrics)
	}
}

func TestUsageRollupSeparatesMockProductionAndEvaluationFixtures(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, version.ID, "0.1.0", "usage mode fixture")
	approvedModel(t, registry, tenantID, version.ID)
	suiteID := evalSuite(t, registry, tenantID, agent.Key)
	publishVersion(t, registry, tenantID, version.ID, suiteID)
	input := serviceRun("usage-production-fixture", "{}")
	input.AgentKey = agent.Key
	production, _, err := registry.CreateRun(ctx, tenantID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.BeginRunExecution(ctx, tenantID, production.ID); err != nil {
		t.Fatal(err)
	}
	if err = registry.RecordRunUsage(ctx, tenantID, production.ID, postgres.RunUsage{Attempt: 1, TokensIn: 40, TokensOut: 4, UsageReports: 1}); err != nil {
		t.Fatal(err)
	}
	mockAgent, mockVersion := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, mockVersion.ID, "0.1.0", "mock usage fixture")
	mocked, _, err := registry.CreateRun(ctx, tenantID, postgres.RunCreate{AgentKey: mockAgent.Key, AgentVersionID: mockVersion.ID, Kind: "sandbox", CallerKind: "user", CallerSubject: "usage-fixture", IdempotencyKey: "usage-mock-fixture", Input: json.RawMessage("{}"), Evidence: json.RawMessage("[]"), AllowDevMock: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.BeginRunExecution(ctx, tenantID, mocked.ID); err != nil {
		t.Fatal(err)
	}
	if err = registry.RecordRunUsage(ctx, tenantID, mocked.ID, postgres.RunUsage{Attempt: 1, TokensIn: 10, TokensOut: 1, UsageReports: 1}); err != nil {
		t.Fatal(err)
	}
	metrics, err := registry.RunMetrics(ctx, tenantID, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.UsageByClass["actual"].TokensIn != 40 || metrics.UsageByClass["mock"].TokensIn != 10 || metrics.UsageByClass["test"].Runs < 1 {
		t.Fatalf("run kinds were mixed: %+v", metrics.UsageByClass)
	}
}
