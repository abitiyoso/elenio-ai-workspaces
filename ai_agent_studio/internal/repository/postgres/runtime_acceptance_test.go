package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elenio/agent-studio/internal/repository/postgres"
	agentruntime "github.com/elenio/agent-studio/internal/runtime"
	"github.com/elenio/agent-studio/internal/simpack"
)

const outputSchema = `{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"},"evidence_refs":{"type":"array","items":{"type":"string"}}}}`

// approvedModel registers a provider whose capability probe passed and
// approves it for development and production, then binds it to the version.
func approvedModel(t *testing.T, registry *postgres.ModelRegistry, tenantID, versionID string) string {
	t.Helper()
	ctx := context.Background()
	suffix := strings.ReplaceAll(acceptanceUUID(t), "-", "")[:10]
	provider, err := registry.CreateProvider(ctx, tenantID, postgres.ProviderInput{Key: "p" + suffix, DisplayName: "Test provider",
		AdapterType: "openai_compatible", BaseURL: "https://9router.kibicode.my.id/v1", CreatedBy: "platform-admin"})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := registry.CreateDeployment(ctx, tenantID, postgres.DeploymentInput{ProviderID: provider.ID, Key: "d" + suffix, ProviderModelID: "test-model",
		DisplayName: "Test model", Capabilities: json.RawMessage(`{"json":true,"tools":true}`), MaxOutputTokens: 1024, TimeoutMillis: 30000, CreatedBy: "platform-admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetProviderCredentialRef(ctx, tenantID, provider.ID, "vault://acceptance/"+suffix, "platform-admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RecordProviderProbe(ctx, tenantID, provider.ID, deployment.ID, "platform-admin", "passed",
		json.RawMessage(`{"capabilities":{"json":true,"tools":true}}`), ""); err != nil {
		t.Fatal(err)
	}
	for _, env := range []string{"development", "production"} {
		if _, err := registry.SetModelApproval(ctx, tenantID, deployment.ID, env, "approved", "acceptance", "platform-admin"); err != nil {
			t.Fatalf("approve %s: %v", env, err)
		}
	}
	bindModel(t, registry, tenantID, versionID, deployment.ID)
	return deployment.ID
}

func bindModel(t *testing.T, registry *postgres.ModelRegistry, tenantID, versionID, deploymentID string) {
	t.Helper()
	if _, err := registry.SetAgentModelBinding(context.Background(), tenantID, versionID, deploymentID, "", "development", "author", nil); err != nil {
		t.Fatalf("bind model: %v", err)
	}
}

func configureVersion(t *testing.T, registry *postgres.ModelRegistry, tenantID, versionID, semver, note string) {
	t.Helper()
	if _, err := registry.UpdateAgentVersion(context.Background(), tenantID, versionID, postgres.AgentVersionInput{
		Semver: semver, Manifest: json.RawMessage(`{"configured":true,"note":"` + note + `"}`), InputSchema: json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(outputSchema), ModelPolicy: json.RawMessage(`{}`), ToolPolicy: json.RawMessage(`{}`),
	}, "author"); err != nil {
		t.Fatalf("configure version: %v", err)
	}
}

func evalSuite(t *testing.T, registry *postgres.ModelRegistry, tenantID, agentKey string) string {
	t.Helper()
	suite, err := registry.CreateEvalSuite(context.Background(), tenantID, agentKey, "core", "acceptance", "trainer")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"positive", "critical-negative"} {
		if _, err := registry.CreateEvalCase(context.Background(), tenantID, suite.ID, postgres.EvalCaseInput{Key: key, Classification: "positive",
			Critical: true, Input: json.RawMessage(`{"q":"` + key + `"}`), CreatedBy: "trainer"}); err != nil {
			t.Fatal(err)
		}
	}
	return suite.ID
}

// settleEval stands in for the worker: it settles every case run and records
// a pass or fail outcome.
func settleEval(t *testing.T, registry *postgres.ModelRegistry, tenantID string, evalRun postgres.EvalRun, failCase string) postgres.EvalRun {
	t.Helper()
	ctx := context.Background()
	for _, res := range evalRun.Results {
		if _, err := registry.BeginRunExecution(ctx, tenantID, res.RunID); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.FinishRun(ctx, tenantID, res.RunID, postgres.RunResult{Status: "succeeded", Output: json.RawMessage(`{"summary":"draft"}`),
			Validation: json.RawMessage(`{"risk_flags":[]}`)}); err != nil {
			t.Fatal(err)
		}
		if err := registry.RecordEvalOutcome(ctx, tenantID, res.RunID, res.CaseKey != failCase, json.RawMessage(`[]`)); err != nil {
			t.Fatal(err)
		}
	}
	done, err := registry.GetEvalRun(ctx, tenantID, evalRun.ID)
	if err != nil || done.Status != "completed" {
		t.Fatalf("evaluation did not complete: %+v %v", done, err)
	}
	return done
}

func publishVersion(t *testing.T, registry *postgres.ModelRegistry, tenantID, versionID, suiteID string) postgres.ReleaseRequest {
	t.Helper()
	ctx := context.Background()
	evalRun, err := registry.StartEvalRun(ctx, tenantID, suiteID, versionID, "trainer", false)
	if err != nil {
		t.Fatal(err)
	}
	settleEval(t, registry, tenantID, evalRun, "")
	req, err := registry.CreateReleaseRequest(ctx, tenantID, versionID, evalRun.ID, "production", "release", "author")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReviewRelease(ctx, tenantID, req.ID, "domain-sme", true, "semantically correct"); err != nil {
		t.Fatal(err)
	}
	req, err = registry.DecideRelease(ctx, tenantID, req.ID, "ai-ops", true, "gates passed")
	if err != nil || req.Status != "published" {
		t.Fatalf("release was not published: %+v %v", req, err)
	}
	return req
}

func serviceRun(key string, input string) postgres.RunCreate {
	return postgres.RunCreate{Kind: "production", CallerKind: "service", CallerSubject: "service:flowcase", IdempotencyKey: key,
		CaseRef: "case-1", Input: json.RawMessage(input), Evidence: json.RawMessage(`[{"source_id":"kms-doc-1","version_id":"v1","title":"RFP"}]`),
		ReleaseEnvironment: "production"}
}

// UAT-05/06/07 and separation of duties: failing or stale evaluations block,
// nobody approves their own release, published versions pin existing runs,
// and rollback only changes routing for new runs.
func TestAcceptanceReleaseWorkflowGatesAndPinning(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	agent, v1 := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, v1.ID, "1.0.0", "first")
	deploymentID := approvedModel(t, registry, tenantID, v1.ID)
	suiteID := evalSuite(t, registry, tenantID, agent.Key)

	if _, _, err := registry.CreateRun(ctx, tenantID, func() postgres.RunCreate {
		in := serviceRun("before-any-release", `{"q":1}`)
		in.AgentKey = agent.Key
		return in
	}()); !errors.Is(err, postgres.ErrNoActiveRelease) {
		t.Fatalf("production run without a release: %v", err)
	}

	failing, err := registry.StartEvalRun(ctx, tenantID, suiteID, v1.ID, "trainer", false)
	if err != nil {
		t.Fatal(err)
	}
	failing = settleEval(t, registry, tenantID, failing, "critical-negative")
	if _, err := registry.CreateReleaseRequest(ctx, tenantID, v1.ID, failing.ID, "production", "", "author"); !errors.Is(err, postgres.ErrEvaluationBlocked) {
		t.Fatalf("critical failure did not block the release request: %v", err)
	}

	passing, err := registry.StartEvalRun(ctx, tenantID, suiteID, v1.ID, "trainer", false)
	if err != nil {
		t.Fatal(err)
	}
	passing = settleEval(t, registry, tenantID, passing, "")
	configureVersion(t, registry, tenantID, v1.ID, "1.0.0", "edited after evaluation")
	if _, err := registry.CreateReleaseRequest(ctx, tenantID, v1.ID, passing.ID, "production", "", "author"); !errors.Is(err, postgres.ErrEvaluationStale) {
		t.Fatalf("draft edited after evaluation was releasable: %v", err)
	}

	passing, err = registry.StartEvalRun(ctx, tenantID, suiteID, v1.ID, "trainer", false)
	if err != nil {
		t.Fatal(err)
	}
	passing = settleEval(t, registry, tenantID, passing, "")
	req, err := registry.CreateReleaseRequest(ctx, tenantID, v1.ID, passing.ID, "production", "", "author")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReviewRelease(ctx, tenantID, req.ID, "author", true, "self review"); !errors.Is(err, postgres.ErrSelfApproval) {
		t.Fatalf("requester reviewed own release: %v", err)
	}
	if _, err := registry.DecideRelease(ctx, tenantID, req.ID, "ai-ops", true, "too early"); !errors.Is(err, postgres.ErrInvalidState) {
		t.Fatalf("ops decided before domain review: %v", err)
	}
	if _, err := registry.ReviewRelease(ctx, tenantID, req.ID, "domain-sme", true, "ok"); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"author", "domain-sme"} {
		if _, err := registry.DecideRelease(ctx, tenantID, req.ID, actor, true, "self"); !errors.Is(err, postgres.ErrSelfApproval) {
			t.Fatalf("%s approved a release they requested or reviewed: %v", actor, err)
		}
	}
	if req, err = registry.DecideRelease(ctx, tenantID, req.ID, "ai-ops", true, "gates passed"); err != nil || req.Status != "published" {
		t.Fatalf("release not published: %+v %v", req, err)
	}

	runV1 := serviceRun("node-1:attempt-1", `{"q":"pinned"}`)
	runV1.AgentKey = agent.Key
	pinned, created, err := registry.CreateRun(ctx, tenantID, runV1)
	if err != nil || !created || pinned.AgentVersionID != v1.ID || pinned.ModelMode != "provider" {
		t.Fatalf("production run after release: %+v created=%v %v", pinned, created, err)
	}

	v2, err := registry.CreateAgentVersion(ctx, tenantID, agent.Key, postgres.AgentVersionInput{Semver: "1.1.0", Manifest: json.RawMessage(`{"configured":true}`),
		InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(outputSchema), CreatedBy: "author"})
	if err != nil {
		t.Fatal(err)
	}
	bindModel(t, registry, tenantID, v2.ID, deploymentID)
	publishVersion(t, registry, tenantID, v2.ID, suiteID)

	again, err := registry.GetRun(ctx, tenantID, pinned.ID)
	if err != nil || again.AgentVersionID != v1.ID {
		t.Fatalf("existing run moved to the new release: %+v %v", again, err)
	}
	runV2 := serviceRun("node-2:attempt-1", `{"q":"new"}`)
	runV2.AgentKey = agent.Key
	newer, _, err := registry.CreateRun(ctx, tenantID, runV2)
	if err != nil || newer.AgentVersionID != v2.ID {
		t.Fatalf("new run did not use the new release: %+v %v", newer, err)
	}
	pinV1 := serviceRun("node-3:attempt-1", `{"q":"pin"}`)
	pinV1.AgentKey, pinV1.PinVersion = agent.Key, "1.0.0"
	if _, _, err := registry.CreateRun(ctx, tenantID, pinV1); !errors.Is(err, postgres.ErrVersionMismatch) {
		t.Fatalf("pin to a superseded version was accepted: %v", err)
	}

	releases, err := registry.ListReleases(ctx, tenantID, agent.Key)
	if err != nil || len(releases) != 2 || releases[0].Status != "active" || releases[0].AgentVersionID != v2.ID {
		t.Fatalf("unexpected releases: %+v %v", releases, err)
	}
	rolled, err := registry.RollbackRelease(ctx, tenantID, releases[0].ID, "ai-ops", "regression in production")
	if err != nil || rolled.AgentVersionID != v1.ID || rolled.RolledBackFrom == nil {
		t.Fatalf("rollback failed: %+v %v", rolled, err)
	}
	runAfter := serviceRun("node-4:attempt-1", `{"q":"after rollback"}`)
	runAfter.AgentKey = agent.Key
	afterRollback, _, err := registry.CreateRun(ctx, tenantID, runAfter)
	if err != nil || afterRollback.AgentVersionID != v1.ID {
		t.Fatalf("new run after rollback did not use the previous release: %+v %v", afterRollback, err)
	}
	if direct, err := registry.GetRun(ctx, tenantID, newer.ID); err != nil || direct.AgentVersionID != v2.ID {
		t.Fatalf("rollback rewrote an existing run: %+v %v", direct, err)
	}
}

// UAT-02 and UAT-12: idempotent invocation, single settlement, deduplicated
// Flowcase result, and lease expiry handing a crashed run to another worker.
func TestAcceptanceProductionRunIdempotencyOutboxAndLeases(t *testing.T) {
	db, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	agent, v1 := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, v1.ID, "1.0.0", "x")
	approvedModel(t, registry, tenantID, v1.ID)
	publishVersion(t, registry, tenantID, v1.ID, evalSuite(t, registry, tenantID, agent.Key))
	if _, err := db.ExecContext(ctx, `DELETE FROM work_queue WHERE tenant_id=$1::uuid`, tenantID); err != nil {
		t.Fatal(err)
	}

	in := serviceRun("node-9:attempt-1", `{"q":"same"}`)
	in.AgentKey = agent.Key
	first, created, err := registry.CreateRun(ctx, tenantID, in)
	if err != nil || !created {
		t.Fatal(err)
	}
	second, created, err := registry.CreateRun(ctx, tenantID, in)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate invocation created another run: %s vs %s created=%v err=%v", first.ID, second.ID, created, err)
	}
	changed := in
	changed.Input = json.RawMessage(`{"q":"different"}`)
	if _, _, err := registry.CreateRun(ctx, tenantID, changed); !errors.Is(err, postgres.ErrIdempotencyConflict) {
		t.Fatalf("reused key with a different body: %v", err)
	}
	other := in
	other.CallerSubject = "service:other-client"
	if run, created, err := registry.CreateRun(ctx, tenantID, other); err != nil || !created || run.ID == first.ID {
		t.Fatalf("idempotency key collided across callers: %v", err)
	}
	evidence, err := registry.ListRunEvidence(ctx, tenantID, first.ID)
	if err != nil || len(evidence) != 1 || evidence[0].TrustStatus != "unverified" || evidence[0].Origin != "flowcase" {
		t.Fatalf("flowcase evidence refs must stay unverified: %+v %v", evidence, err)
	}

	item, ok, err := registry.ClaimWork(ctx, "run", "worker-a", time.Second)
	if err != nil || !ok {
		t.Fatalf("claim: %v", err)
	}
	if _, ok, _ := registry.ClaimWork(ctx, "run", "worker-b", time.Second); ok {
		// A second queued run exists (other caller); claim the specific one below.
	}
	time.Sleep(1100 * time.Millisecond)
	reclaimed, ok, err := registry.ClaimWork(ctx, "run", "worker-c", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("expired lease was not reclaimable: %v", err)
	}
	if held, _ := registry.ExtendLease(ctx, "run", item.RefID, "worker-a", time.Second); held && reclaimed.RefID == item.RefID {
		t.Fatal("crashed worker still holds a reclaimed lease")
	}

	if _, err := registry.BeginRunExecution(ctx, tenantID, first.ID); err != nil {
		t.Fatal(err)
	}
	output := json.RawMessage(`{"summary":"draft for review","evidence_refs":[]}`)
	checksum, _ := postgres.OutputChecksum(output)
	settled, err := registry.FinishRun(ctx, tenantID, first.ID, postgres.RunResult{Status: "needs_review", Output: output, OutputChecksum: checksum,
		Validation: json.RawMessage(`{"risk_flags":["unsupported_claim"]}`)})
	if err != nil || settled.Status != "needs_review" {
		t.Fatal(err)
	}
	if _, err := registry.FinishRun(ctx, tenantID, first.ID, postgres.RunResult{Status: "succeeded"}); !errors.Is(err, postgres.ErrInvalidState) {
		t.Fatalf("run settled twice: %v", err)
	}

	var outboxID, payload string
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM work_queue WHERE tenant_id=$1::uuid AND kind='outbox'`, tenantID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one outbox delivery, got %d (%v)", count, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT ref_id::text FROM work_queue WHERE tenant_id=$1::uuid AND kind='outbox'`, tenantID).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	event, err := registry.GetOutboxEvent(ctx, tenantID, outboxID)
	if err != nil {
		t.Fatal(err)
	}
	payload = string(event.Payload)
	var body struct {
		MessageID string                     `json:"message_id"`
		Audience  string                     `json:"audience"`
		Run       postgres.FlowcaseRunResult `json:"run"`
	}
	if err := json.Unmarshal(event.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.MessageID != first.ID+":final" || body.Audience != "flowcase" || body.Run.Status != "succeeded" || body.Run.AgentID != agent.Key {
		t.Fatalf("unexpected callback payload: %s", payload)
	}
	if strings.Contains(payload, `"approved"`) {
		t.Fatalf("callback carries an approval field: %s", payload)
	}
	if sum, _ := postgres.OutputChecksum(body.Run.Output); sum != body.Run.OutputChecksum {
		t.Fatal("callback output checksum does not match the canonical output")
	}
	var review struct {
		Review struct {
			Status string `json:"status"`
		} `json:"review"`
	}
	_ = json.Unmarshal(body.Run.Output, &review)
	if review.Review.Status != "needs_review" {
		t.Fatalf("review state not carried to Flowcase: %s", body.Run.Output)
	}
}

func TestAcceptanceRunsAndTraceAreTenantIsolatedAndAppendOnly(t *testing.T) {
	db, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantA, tenantB := acceptanceUUID(t), acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantA)
	configureVersion(t, registry, tenantA, version.ID, "0.1.0", "x")

	sandbox := postgres.RunCreate{AgentKey: agent.Key, AgentVersionID: version.ID, Kind: "sandbox", CallerKind: "user", CallerSubject: "author",
		IdempotencyKey: "sandbox-isolation-1", Input: json.RawMessage(`{}`), Evidence: json.RawMessage(`[]`)}
	if _, _, err := registry.CreateRun(ctx, tenantA, sandbox); !errors.Is(err, postgres.ErrModelNotApproved) {
		t.Fatalf("sandbox without an approved model or dev mock: %v", err)
	}
	sandbox.AllowDevMock = true
	run, _, err := registry.CreateRun(ctx, tenantA, sandbox)
	if err != nil || run.ModelMode != "dev_mock" {
		t.Fatalf("dev mock sandbox: %+v %v", run, err)
	}
	prod := serviceRun("prod-without-model", `{}`)
	prod.AgentKey, prod.AllowDevMock = agent.Key, true
	if _, _, err := registry.CreateRun(ctx, tenantA, prod); !errors.Is(err, postgres.ErrNoActiveRelease) {
		t.Fatalf("production run without release: %v", err)
	}

	if _, err := registry.GetRun(ctx, tenantB, run.ID); !errors.Is(err, postgres.ErrRunNotFound) {
		t.Fatalf("tenant B read tenant A's run: %v", err)
	}
	if items, total, err := registry.ListRuns(ctx, tenantB, postgres.RunFilter{}); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("tenant B listed tenant A's runs: %d %v", total, err)
	}
	if events, err := registry.ListRunEvents(ctx, tenantB, run.ID); err != nil || len(events) != 0 {
		t.Fatalf("tenant B read tenant A's trace: %d %v", len(events), err)
	}
	if _, err := registry.RequestCancel(ctx, tenantB, run.ID, "intruder"); !errors.Is(err, postgres.ErrRunNotFound) {
		t.Fatalf("tenant B cancelled tenant A's run: %v", err)
	}
	if _, err := registry.CreateEvalSuite(ctx, tenantB, agent.Key, "x", "", "intruder"); !errors.Is(err, postgres.ErrAgentNotFound) {
		t.Fatalf("tenant B created a suite on tenant A's agent: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantA); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_run_events SET event_type='tampered' WHERE run_id=$1::uuid`, run.ID); err == nil {
		t.Fatal("run trace was modified")
	}
	_ = tx.Rollback()

	cancelled, err := registry.RequestCancel(ctx, tenantA, run.ID, "author")
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("queued run was not cancelled: %+v %v", cancelled, err)
	}
	if _, err := registry.BeginRunExecution(ctx, tenantA, run.ID); !errors.Is(err, postgres.ErrInvalidState) {
		t.Fatalf("cancelled run started: %v", err)
	}
}

// UAT-01: seven distinct, configured SIM identities (six workflow specialists
// plus the case intake assistant) with skill, tools and an evaluation suite
// whose schemas compile; nothing is published.
func TestAcceptanceSIMPackConfiguresDraftsOnly(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	pack, err := simpack.Load()
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.ApplySIMPack(ctx, tenantID, "pack-admin", pack)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Agents) != 7 {
		t.Fatalf("pack configured %d agents", len(first.Agents))
	}
	if last := first.Agents[6]; last.AgentKey != "sim.case_intake" || last.Outcome != "configured" {
		t.Fatalf("case intake was not configured: %+v", last)
	}
	for _, outcome := range first.Agents {
		if outcome.Outcome != "configured" {
			t.Fatalf("%s: %s", outcome.AgentKey, outcome.Outcome)
		}
		versions, err := registry.ListAgentVersions(ctx, tenantID, outcome.AgentKey)
		if err != nil || len(versions) != 1 || versions[0].State != "draft" {
			t.Fatalf("%s versions changed or were published: %+v %v", outcome.AgentKey, versions, err)
		}
		if _, err := agentruntime.CompileSchema(versions[0].OutputSchema); err != nil {
			t.Fatalf("%s output schema does not compile: %v", outcome.AgentKey, err)
		}
		skills, _ := registry.ListAgentSkillBindings(ctx, tenantID, versions[0].ID)
		tools, _ := registry.ListAgentToolBindings(ctx, tenantID, versions[0].ID)
		if len(skills) != 1 || len(tools) == 0 {
			t.Fatalf("%s bindings: skills=%d tools=%d", outcome.AgentKey, len(skills), len(tools))
		}
		cases, err := registry.ListEvalCases(ctx, tenantID, outcome.EvalSuiteID)
		if err != nil || len(cases) != 4 {
			t.Fatalf("%s eval cases: %d %v", outcome.AgentKey, len(cases), err)
		}
		if outcome.AgentKey == "sim.case_intake" {
			var manifest struct {
				Configured    bool   `json:"configured"`
				ReviewGate    string `json:"review_gate"`
				MaxIterations int    `json:"max_iterations"`
			}
			_ = json.Unmarshal(versions[0].Manifest, &manifest)
			var input struct {
				Required []string `json:"required"`
			}
			_ = json.Unmarshal(versions[0].InputSchema, &input)
			if !manifest.Configured || manifest.ReviewGate != "G0" || manifest.MaxIterations != 4 || strings.Join(input.Required, ",") != "case_summary,conversation" {
				t.Fatalf("case intake draft: manifest=%+v input=%v", manifest, input.Required)
			}
			critical := 0
			for _, c := range cases {
				if c.Critical {
					critical++
				}
			}
			if critical != 3 {
				t.Fatalf("case intake critical cases = %d", critical)
			}
		}
	}
	second, err := registry.ApplySIMPack(ctx, tenantID, "pack-admin", pack)
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range second.Agents {
		if outcome.Outcome != "already_configured" {
			t.Fatalf("second apply touched %s: %s", outcome.AgentKey, outcome.Outcome)
		}
	}
}

// The worker drives the real Eino executor end to end against the database.
func TestAcceptanceWorkerExecutesSandboxRunWithEino(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	pack, _ := simpack.Load()
	result, err := registry.ApplySIMPack(ctx, tenantID, "pack-admin", pack)
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	worker := &agentruntime.Worker{Store: registry, Executor: &agentruntime.Executor{AllowDevMock: true}, ID: "acceptance-worker", Concurrency: 2, Lease: 10 * time.Second}
	go worker.Run(workerCtx)
	// sim.intake (first) and sim.case_intake (last) exercise both input shapes.
	for _, agent := range []postgres.SIMPackOutcome{result.Agents[0], result.Agents[len(result.Agents)-1]} {
		evalRun, err := registry.StartEvalRun(ctx, tenantID, agent.EvalSuiteID, agent.AgentVersionID, "trainer", true)
		if err != nil {
			t.Fatal(err)
		}
		for {
			current, err := registry.GetEvalRun(ctx, tenantID, evalRun.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status == "completed" {
				var summary postgres.EvalSummary
				_ = json.Unmarshal(current.Summary, &summary)
				if summary.Total != 4 || summary.Gate != "blocked" || !strings.Contains(strings.Join(summary.Reasons, ","), "dev_mock") {
					t.Fatalf("%s: dev mock evaluation must not open the release gate: %+v", agent.AgentKey, summary)
				}
				for _, res := range current.Results {
					if res.Passed == nil || !*res.Passed {
						t.Errorf("%s case %s failed: %s", agent.AgentKey, res.CaseKey, res.Checks)
					}
				}
				break
			}
			if workerCtx.Err() != nil {
				t.Fatalf("%s evaluation did not complete: %+v", agent.AgentKey, current)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// Concurrent duplicate invocations collapse to one run, and two AI Ops
// approvers racing on the same request publish it exactly once.
func TestAcceptanceConcurrentInvocationAndReleaseDecision(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	agent, v1 := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, v1.ID, "1.0.0", "x")
	approvedModel(t, registry, tenantID, v1.ID)
	suiteID := evalSuite(t, registry, tenantID, agent.Key)
	evalRun, err := registry.StartEvalRun(ctx, tenantID, suiteID, v1.ID, "trainer", false)
	if err != nil {
		t.Fatal(err)
	}
	evalRun = settleEval(t, registry, tenantID, evalRun, "")
	req, err := registry.CreateReleaseRequest(ctx, tenantID, v1.ID, evalRun.ID, "production", "", "author")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReviewRelease(ctx, tenantID, req.ID, "domain-sme", true, "ok"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, approver := range []string{"ai-ops-1", "ai-ops-2"} {
		go func(actor string) {
			_, err := registry.DecideRelease(ctx, tenantID, req.ID, actor, true, "race")
			results <- err
		}(approver)
	}
	wins := 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			wins++
		} else if !errors.Is(err, postgres.ErrInvalidState) {
			t.Fatalf("unexpected decision error: %v", err)
		}
	}
	releases, _ := registry.ListReleases(ctx, tenantID, agent.Key)
	if wins != 1 || len(releases) != 1 {
		t.Fatalf("release decided %d times, %d releases", wins, len(releases))
	}

	ids := make(chan string, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			in := serviceRun("node-race:attempt-1", `{"q":"race"}`)
			in.AgentKey = agent.Key
			run, _, err := registry.CreateRun(ctx, tenantID, in)
			errs <- err
			ids <- run.ID
		}()
	}
	unique := map[string]bool{}
	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent invocation failed: %v", err)
		}
		unique[<-ids] = true
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent duplicate invocations created %d runs", len(unique))
	}
}

// Thirty sandbox runs across four workers: every run settles exactly once.
func TestAcceptanceWorkerPoolLoad(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	pack, _ := simpack.Load()
	result, err := registry.ApplySIMPack(ctx, tenantID, "pack-admin", pack)
	if err != nil {
		t.Fatal(err)
	}
	intake := result.Agents[0]
	const total = 30
	for i := 0; i < total; i++ {
		if _, _, err := registry.CreateRun(ctx, tenantID, postgres.RunCreate{AgentKey: intake.AgentKey, AgentVersionID: intake.AgentVersionID, Kind: "sandbox",
			CallerKind: "user", CallerSubject: "load", IdempotencyKey: "load-run-" + strings.Repeat("0", 3) + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Input: json.RawMessage(`{"case_summary":"load test"}`), Evidence: json.RawMessage(`[{"source_id":"s1","title":"t","snippet":"requirements","trust_status":"approved"}]`),
			AllowDevMock: true}); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	workerCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		w := &agentruntime.Worker{Store: registry, Executor: &agentruntime.Executor{AllowDevMock: true}, ID: "load-" + string(rune('a'+i)), Concurrency: 2, Lease: 10 * time.Second}
		go w.Run(workerCtx)
	}
	for {
		runs, n, err := registry.ListRuns(ctx, tenantID, postgres.RunFilter{Kind: "sandbox", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		settled := 0
		for _, r := range runs {
			if r.Status == "succeeded" {
				settled++
			} else if r.Status == "failed" || r.Status == "needs_review" {
				t.Fatalf("run %s settled as %s", r.ID, r.Status)
			}
		}
		if n == total && settled == total {
			for _, r := range runs {
				events, _ := registry.ListRunEvents(ctx, tenantID, r.ID)
				finals := 0
				for _, e := range events {
					if e.Type == "run.succeeded" {
						finals++
					}
				}
				if finals != 1 {
					t.Fatalf("run %s settled %d times", r.ID, finals)
				}
			}
			t.Logf("%d runs settled by 4 workers in %s", total, time.Since(started).Round(time.Millisecond))
			return
		}
		if workerCtx.Err() != nil {
			t.Fatalf("only %d/%d runs settled", settled, total)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// A service run's delegated user is persisted, returned to the worker and part
// of the idempotency fingerprint; the database refuses it on other runs.
func TestAcceptanceDelegatedUserIsPersistedAndFingerprinted(t *testing.T) {
	db, registry := acceptanceRegistry(t)
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	agent, v1 := createAcceptanceAgent(t, registry, tenantID)
	configureVersion(t, registry, tenantID, v1.ID, "1.0.0", "x")
	approvedModel(t, registry, tenantID, v1.ID)
	publishVersion(t, registry, tenantID, v1.ID, evalSuite(t, registry, tenantID, agent.Key))

	owner, other := acceptanceUUID(t), acceptanceUUID(t)
	in := serviceRun("node-7:attempt-1", `{"q":"delegated"}`)
	in.AgentKey, in.DelegatedUser = agent.Key, owner
	first, created, err := registry.CreateRun(ctx, tenantID, in)
	if err != nil || !created || first.DelegatedUser == nil || *first.DelegatedUser != owner || first.CaseRef == nil || *first.CaseRef != "case-1" {
		t.Fatalf("delegated run was not stored: %+v created=%v %v", first, created, err)
	}
	replay, created, err := registry.CreateRun(ctx, tenantID, in)
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("identical replay was not idempotent: %+v created=%v %v", replay, created, err)
	}
	for name, mutate := range map[string]func(*postgres.RunCreate){
		"changed delegated user": func(r *postgres.RunCreate) { r.DelegatedUser = other },
		"dropped delegated user": func(r *postgres.RunCreate) { r.DelegatedUser = "" },
	} {
		changed := in
		mutate(&changed)
		if _, _, err := registry.CreateRun(ctx, tenantID, changed); !errors.Is(err, postgres.ErrIdempotencyConflict) {
			t.Fatalf("%s under the same key: %v", name, err)
		}
	}
	plain := serviceRun("node-8:attempt-1", `{"q":"plain"}`)
	plain.AgentKey = agent.Key
	if run, _, err := registry.CreateRun(ctx, tenantID, plain); err != nil || run.DelegatedUser != nil {
		t.Fatalf("run without delegated user: %+v %v", run, err)
	}
	added := plain
	added.DelegatedUser = owner
	if _, _, err := registry.CreateRun(ctx, tenantID, added); !errors.Is(err, postgres.ErrIdempotencyConflict) {
		t.Fatalf("added delegated user under the same key: %v", err)
	}

	exec, err := registry.BeginRunExecution(ctx, tenantID, first.ID)
	if err != nil || exec.Run.DelegatedUser == nil || *exec.Run.DelegatedUser != owner || exec.Run.CallerKind != "service" {
		t.Fatalf("worker execution lost the delegated user: %+v %v", exec.Run, err)
	}

	human := in
	human.IdempotencyKey, human.CallerKind, human.CallerSubject = "node-7:attempt-2", "user", "someone"
	if _, _, err := registry.CreateRun(ctx, tenantID, human); err == nil {
		t.Fatal("repository stored a delegated user on a user-started run")
	}
	if _, err := db.ExecContext(ctx, `UPDATE agent_runs SET delegated_user=$3 WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, first.ID, "NOT-A-UUID"); err == nil {
		t.Fatal("database accepted a malformed delegated user")
	}
}

// A development-exempt subject may take every release step alone; everyone
// else keeps independent review, and each waived step is flagged and audited.
func TestAcceptanceSelfApprovalExemptionIsExplicitAndAudited(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	registry.SetSelfApprovalExempt([]string{"demo"})
	ctx := context.Background()
	tenantID := acceptanceUUID(t)
	requestAs := func(requester string) postgres.ReleaseRequest {
		t.Helper()
		agent, v1 := createAcceptanceAgent(t, registry, tenantID)
		configureVersion(t, registry, tenantID, v1.ID, "1.0.0", "x")
		approvedModel(t, registry, tenantID, v1.ID)
		suiteID := evalSuite(t, registry, tenantID, agent.Key)
		evalRun, err := registry.StartEvalRun(ctx, tenantID, suiteID, v1.ID, "trainer", false)
		if err != nil {
			t.Fatal(err)
		}
		evalRun = settleEval(t, registry, tenantID, evalRun, "")
		req, err := registry.CreateReleaseRequest(ctx, tenantID, v1.ID, evalRun.ID, "production", "", requester)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}

	other := requestAs("author")
	if _, err := registry.ReviewRelease(ctx, tenantID, other.ID, "author", true, "self review"); !errors.Is(err, postgres.ErrSelfApproval) {
		t.Fatalf("non-exempt requester reviewed own release: %v", err)
	}
	if _, err := registry.ReviewRelease(ctx, tenantID, other.ID, "demo", true, "independent review"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.DecideRelease(ctx, tenantID, other.ID, "author", true, "self"); !errors.Is(err, postgres.ErrSelfApproval) {
		t.Fatalf("non-exempt requester decided own release: %v", err)
	}
	if req, err := registry.DecideRelease(ctx, tenantID, other.ID, "ai-ops", true, "ok"); err != nil || req.SelfApprovalExempt {
		t.Fatalf("independent release must not carry the exemption flag: %+v %v", req, err)
	}

	own := requestAs("demo")
	if req, err := registry.ReviewRelease(ctx, tenantID, own.ID, "demo", true, "walkthrough"); err != nil || req.Status != "pending_ops" || !req.SelfApprovalExempt {
		t.Fatalf("exempt requester could not review own release: %+v %v", req, err)
	}
	if req, err := registry.DecideRelease(ctx, tenantID, own.ID, "demo", true, "walkthrough"); err != nil || req.Status != "published" || !req.SelfApprovalExempt {
		t.Fatalf("exempt requester could not publish own release: %+v %v", req, err)
	}
	events, _, err := registry.ListAuditEvents(ctx, tenantID, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	waived := 0
	for _, e := range events {
		if e.Action == "release.self_approval_exempt" {
			waived++
			if e.Actor != "demo" || e.Resource != "release_request:"+own.ID {
				t.Fatalf("exemption audited for the wrong actor or request: %+v", e)
			}
		}
	}
	if waived != 2 {
		t.Fatalf("waived steps audited %d times, want 2 (review and decision)", waived)
	}
}
