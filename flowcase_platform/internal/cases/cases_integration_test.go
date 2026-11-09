package cases_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/idempotency"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/testutil/pgtest"
	"github.com/elenio/flowcase/internal/workflows/registry"
)

type env struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	reg      *registry.Service
	eng      *cases.Engine
	tenant   string
	owner    auth.Identity // case_owner, holds poc-owner + delivery-lead
	approver auth.Identity // domain_approver with poc.budget delegation
	outsider auth.Identity // domain_approver without delegation
	robot    auth.Identity // service principal with role + delegation
	n        int
}

func (e *env) user(subject, kind string, roles ...auth.Role) auth.Identity {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(e.ctx, `INSERT INTO users (issuer, subject, display_name, kind) VALUES ('https://host', $1, $1, $2) RETURNING id::text`, subject, kind).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO memberships (tenant_id, user_id) VALUES ($1, $2)`, e.tenant, id)
	for _, r := range roles {
		e.exec(`INSERT INTO role_assignments (tenant_id, user_id, role) VALUES ($1, $2, $3)`, e.tenant, id, r)
	}
	return auth.Identity{UserID: id, TenantID: e.tenant, DisplayName: subject, Kind: kind, Roles: roles}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(e.ctx, sql, args...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

func newEnv(t *testing.T) *env {
	pool, _ := pgtest.Migrated(t)
	ps, _ := packs.Load()
	cat, _ := packs.Catalog(ps)
	tpls, _ := packs.Templates(ps)
	e := &env{t: t, ctx: context.Background(), pool: pool, reg: &registry.Service{DB: pool, Catalog: cat, Templates: tpls},
		eng: &cases.Engine{DB: pool, Catalog: cat, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	if err := pool.QueryRow(e.ctx, `INSERT INTO tenants (external_ref, name) VALUES ('acme', 'Acme') RETURNING id::text`).Scan(&e.tenant); err != nil {
		t.Fatal(err)
	}
	designer := e.user("designer", auth.KindHuman, auth.RoleWorkspaceMember, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)
	if _, err := e.reg.SeedPacks(e.ctx, designer, ps); err != nil {
		t.Fatal(err)
	}
	e.owner = e.user("owner", auth.KindHuman, auth.RoleWorkspaceMember, auth.RoleCaseOwner)
	e.approver = e.user("approver", auth.KindHuman, auth.RoleWorkspaceMember, auth.RoleDomainApprover)
	e.outsider = e.user("outsider", auth.KindHuman, auth.RoleWorkspaceMember, auth.RoleDomainApprover)
	e.robot = e.user("agent-runner", auth.KindService, auth.RoleDomainApprover, auth.RoleCaseOwner)
	for _, role := range []string{"poc-owner", "delivery-lead"} {
		e.exec(`INSERT INTO domain_role_assignments (tenant_id, user_id, role) VALUES ($1, $2, $3)`, e.tenant, e.owner.UserID, role)
	}
	for _, u := range []auth.Identity{e.approver, e.robot} {
		e.exec(`INSERT INTO policy_delegations (tenant_id, policy_ref, user_id) VALUES ($1, 'poc.budget', $2)`, e.tenant, u.UserID)
	}
	return e
}

func (e *env) createCase(workflow string, vars map[string]any) string {
	e.t.Helper()
	e.n++
	id, err := e.eng.CreateCase(e.ctx, e.owner, cases.Idempotency{Key: fmt.Sprintf("case-key-%04d", e.n), Hash: h(fmt.Sprint(fmt.Sprint(e.n)))},
		cases.CreateInput{WorkflowKey: workflow, Title: "Test case", Variables: vars})
	if err != nil {
		e.t.Fatalf("create case: %v", err)
	}
	return id
}

func (e *env) drain() {
	e.t.Helper()
	for range 50 {
		n, err := e.eng.ProcessReady(e.ctx, "test-worker", 20)
		if err != nil {
			e.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
	e.t.Fatal("work did not drain")
}

func (e *env) detail(id string) cases.CaseDetail {
	e.t.Helper()
	d, err := e.eng.GetCase(e.ctx, e.owner, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func openTask(t *testing.T, d cases.CaseDetail, node string) cases.Task {
	t.Helper()
	for _, tk := range d.Tasks {
		if tk.NodeID == node && tk.Status == "assigned" {
			return tk
		}
	}
	t.Fatalf("no open task for %s in %+v", node, d.Tasks)
	return cases.Task{}
}

func pendingApproval(t *testing.T, d cases.CaseDetail) cases.Approval {
	t.Helper()
	for _, a := range d.Approvals {
		if a.Status == "pending" {
			return a
		}
	}
	t.Fatalf("no pending approval in %+v", d.Approvals)
	return cases.Approval{}
}

func denied(t *testing.T, err error, reason string) {
	t.Helper()
	var d *cases.DeniedError
	if !errors.As(err, &d) || d.Reason != reason {
		t.Fatalf("expected denial %s, got %v", reason, err)
	}
}

func conflict(t *testing.T, err error, code string) {
	t.Helper()
	var c *cases.ConflictError
	if !errors.As(err, &c) || c.Code != code {
		t.Fatalf("expected conflict %s, got %v", code, err)
	}
}

var pocVars = map[string]any{"requester_id": "r-1", "purpose": "Evaluate vendor", "estimated_cost": 5000}

func TestPoCRunsWithGatedApprovalAndSoD(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("internal-poc-request", pocVars)
	e.drain()
	d := e.detail(id)
	if d.WorkflowVersion != 1 || len(d.DefinitionHash) != 64 || d.Status != cases.CaseActive {
		t.Fatalf("case must be active and pinned to v1: %+v", d.CaseSummary)
	}
	scope := openTask(t, d, "scope")

	denied(t, e.eng.CompleteTask(e.ctx, e.approver, scope.ID, scope.VersionNum, cases.CompleteTaskInput{}), "not_assignee_role")
	if err := e.eng.CompleteTask(e.ctx, e.owner, scope.ID, scope.VersionNum, cases.CompleteTaskInput{Comment: "Scope agreed"}); err != nil {
		t.Fatal(err)
	}
	conflict(t, e.eng.CompleteTask(e.ctx, e.owner, scope.ID, scope.VersionNum+1, cases.CompleteTaskInput{}), "task_not_open")
	e.drain()

	d = e.detail(id)
	if d.Status != cases.CaseUnderReview || len(d.Conditions) != 1 || !d.Conditions[0].Result || d.Conditions[0].Target != "budget_approval" {
		t.Fatalf("condition should route to the budget gate: %s %+v", d.Status, d.Conditions)
	}
	gate := pendingApproval(t, d)

	// Premature gate release: nothing downstream exists while the approval is pending, however often the worker runs.
	e.drain()
	for _, x := range e.detail(id).Timeline {
		if x.NodeID == "provision" || x.NodeID == "end" {
			t.Fatalf("downstream step %s scheduled before approval", x.NodeID)
		}
	}

	denied(t, e.eng.Decide(e.ctx, e.owner, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "my own case"}), "missing_role")
	denied(t, e.eng.Decide(e.ctx, e.outsider, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "looks fine"}), "no_delegation")
	denied(t, e.eng.Decide(e.ctx, e.robot, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "auto approve"}), "service_principal")
	var input *cases.InputError
	if err := e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "ok"}); !errors.As(err, &input) {
		t.Fatalf("a rationale is mandatory, got %v", err)
	}
	conflict(t, e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "return", Rationale: "needs more detail"}), "return_not_allowed")
	if err := e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum+5, cases.DecisionInput{Decision: "approve", Rationale: "Budget is reasonable"}); !errors.Is(err, cases.ErrPreconditionFailed) {
		t.Fatalf("stale If-Match must fail, got %v", err)
	}
	if err := e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "Budget is reasonable"}); err != nil {
		t.Fatal(err)
	}
	conflict(t, e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum+1, cases.DecisionInput{Decision: "approve", Rationale: "again please"}), "approval_closed")
	e.drain()

	d = e.detail(id)
	provision := openTask(t, d, "provision")
	if err := e.eng.CompleteTask(e.ctx, e.owner, provision.ID, provision.VersionNum, cases.CompleteTaskInput{}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d = e.detail(id)
	if d.Status != cases.CaseCompleted || d.Progress != 100 {
		t.Fatalf("case should complete: %s %d%%", d.Status, d.Progress)
	}
	decided := d.Approvals[0].Decision
	if decided == nil || decided.DecidedByName != "approver" || len(decided.DecisionHash) != 64 {
		t.Fatalf("decision record missing: %+v", d.Approvals[0])
	}
	if _, err := e.pool.Exec(e.ctx, `UPDATE approval_decisions SET rationale = 'rewritten'`); !postgres.HasCode(err, postgres.CodeImmutable) {
		t.Fatalf("decisions must be append-only, got %v", err)
	}
}

func TestDatabaseRefusesServicePrincipalDecisions(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("internal-poc-request", pocVars)
	e.drain()
	scope := openTask(t, e.detail(id), "scope")
	_ = e.eng.CompleteTask(e.ctx, e.owner, scope.ID, scope.VersionNum, cases.CompleteTaskInput{})
	e.drain()
	gate := pendingApproval(t, e.detail(id))
	_, err := e.pool.Exec(e.ctx, `INSERT INTO approval_decisions (tenant_id, approval_id, decided_by, decision, rationale, subject_hash, authority, decision_hash)
		VALUES ($1, $2, $3, 'approve', 'bypass the service', $4, '{}', $4)`, e.tenant, gate.ID, e.robot.UserID, gate.SubjectHash)
	if !postgres.HasCode(err, cases.CodeHumanOnly) {
		t.Fatalf("database must refuse non-human deciders, got %v", err)
	}
}

func TestConditionFalseSkipsGateAndSubmitterCannotApprove(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("internal-poc-request", map[string]any{"requester_id": "r-1", "purpose": "Free trial", "estimated_cost": 0})
	e.drain()
	scope := openTask(t, e.detail(id), "scope")
	if err := e.eng.CompleteTask(e.ctx, e.owner, scope.ID, scope.VersionNum, cases.CompleteTaskInput{}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d := e.detail(id)
	if len(d.Approvals) != 0 || d.Conditions[0].Result {
		t.Fatalf("zero cost must skip the budget gate: %+v", d.Conditions)
	}
	openTask(t, d, "provision")

	// A delegate who submitted work in a case may not approve it.
	id2 := e.createCase("internal-poc-request", pocVars)
	e.exec(`INSERT INTO domain_role_assignments (tenant_id, user_id, role) VALUES ($1, $2, 'poc-owner')`, e.tenant, e.approver.UserID)
	e.drain()
	scope2 := openTask(t, e.detail(id2), "scope")
	if err := e.eng.CompleteTask(e.ctx, e.approver, scope2.ID, scope2.VersionNum, cases.CompleteTaskInput{}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	gate := pendingApproval(t, e.detail(id2))
	denied(t, e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "approving my own work"}), "sod_task_submitter")
	list, err := e.eng.ListApprovals(e.ctx, e.approver, "pending")
	if err != nil || len(list) != 1 || list[0].CanDecide || list[0].BlockedReason == nil || *list[0].BlockedReason != "sod_task_submitter" {
		t.Fatalf("inbox must explain why the approver cannot decide: %+v %v", list, err)
	}

	// The development exemption (SELF_APPROVAL_EXEMPT_SUBJECTS) lets the same person decide, and marks it.
	exempt := e.approver
	exempt.SoDExempt = true
	if list, err := e.eng.ListApprovals(e.ctx, exempt, "pending"); err != nil || len(list) != 1 || !list[0].CanDecide {
		t.Fatalf("exempt approver must be able to decide: %+v %v", list, err)
	}
	if err := e.eng.Decide(e.ctx, exempt, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "demo walkthrough"}); err != nil {
		t.Fatalf("exempt approver was refused: %v", err)
	}
	var authority, audited bool
	_ = e.pool.QueryRow(e.ctx, `SELECT (authority->>'self_approval_exempt')::bool FROM approval_decisions WHERE approval_id = $1`, gate.ID).Scan(&authority)
	_ = e.pool.QueryRow(e.ctx, `SELECT EXISTS (SELECT 1 FROM audit_events WHERE action = 'approval.decided' AND subject_id = $1
		AND (data->>'self_approval_exempt')::bool)`, gate.ID).Scan(&audited)
	if !authority || !audited {
		t.Fatalf("exempt decision must be marked: authority=%v audit=%v", authority, audited)
	}
}

// reviewWorkflow: draft (human task) -> review (approval with return_to draft) -> end.
func (e *env) publishReviewWorkflow() {
	e.t.Helper()
	doc := []byte(`{"schema_version":"1.0","key":"doc-review","name":"Document review","version":1,"policy_refs":["poc.budget"],
		"nodes":[{"id":"start","type":"start","name":"Start","next":["draft"]},
		{"id":"draft","type":"human_task","name":"Write draft","config":{"assignee_role":"poc-owner",
			"form_schema":{"type":"object","properties":{"summary":{"type":"string","maxLength":200}},"required":["summary"]}},"next":["review"]},
		{"id":"review","type":"human_approval","name":"Review","config":{"policy_ref":"poc.budget","return_to":"draft"},"next":["end"]},
		{"id":"end","type":"end","name":"Done","next":[]}]}`)
	designer := e.user(fmt.Sprintf("designer-%d", e.n+100), auth.KindHuman, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)
	res, err := e.reg.CreateDefinition(e.ctx, designer, registry.Idempotency{Key: "review-wf-1", Hash: h(fmt.Sprint("x"))}, registry.CreateInput{Key: "doc-review", Definition: doc})
	if err != nil || !res.Report.Valid {
		e.t.Fatalf("create review workflow: %v %+v", err, res.Report.Errors())
	}
	if _, err := e.reg.Publish(e.ctx, designer, "doc-review", 1, res.Version.VersionNum); err != nil {
		e.t.Fatal(err)
	}
}

func TestReturnAndRevisionCreateReworkIterations(t *testing.T) {
	e := newEnv(t)
	e.publishReviewWorkflow()
	id := e.createCase("doc-review", nil)
	e.drain()
	draft := openTask(t, e.detail(id), "draft")

	var verr *cases.ValidationError
	if err := e.eng.CompleteTask(e.ctx, e.owner, draft.ID, draft.VersionNum, cases.CompleteTaskInput{Output: map[string]any{}}); !errors.As(err, &verr) {
		t.Fatalf("form_schema must be enforced, got %v", err)
	}
	if err := e.eng.CompleteTask(e.ctx, e.owner, draft.ID, draft.VersionNum, cases.CompleteTaskInput{Output: map[string]any{"summary": "v1 summary"}}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d := e.detail(id)
	if !json.Valid(d.Variables) || !slices.Contains([]string{`{"summary": "v1 summary"}`}, string(d.Variables)) {
		t.Fatalf("declared form fields become case variables: %s", d.Variables)
	}
	gate := pendingApproval(t, d)

	// Approver returns the draft for rework.
	if err := e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "return", Rationale: "Add the cost breakdown"}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d = e.detail(id)
	rework := openTask(t, d, "draft")
	if rework.Iteration != 2 || rework.RevisionNote == nil || *rework.RevisionNote != "Add the cost breakdown" || d.Status != cases.CaseActive {
		t.Fatalf("return must reopen draft as iteration 2 with the rationale: %+v status=%s", rework, d.Status)
	}
	if err := e.eng.CompleteTask(e.ctx, e.owner, rework.ID, rework.VersionNum, cases.CompleteTaskInput{Output: map[string]any{"summary": "v2"}}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	gate2 := pendingApproval(t, e.detail(id))
	if gate2.Iteration != 2 || gate2.SubjectHash == gate.SubjectHash {
		t.Fatalf("the new review must bind to the new state: %+v", gate2)
	}

	// Case owner pulls the submitted draft back while the review is pending: the review is withdrawn.
	tasks := e.detail(id).Tasks
	var submitted cases.Task
	for _, tk := range tasks {
		if tk.Iteration == 2 && tk.Status == "submitted" {
			submitted = tk
		}
	}
	denied(t, e.eng.RequestRevision(e.ctx, e.approver, submitted.ID, submitted.VersionNum, "not my call"), "not_case_owner")
	if err := e.eng.RequestRevision(e.ctx, e.owner, submitted.ID, submitted.VersionNum, "Typo in the summary"); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d = e.detail(id)
	third := openTask(t, d, "draft")
	if third.Iteration != 3 {
		t.Fatalf("revision must open iteration 3, got %d", third.Iteration)
	}
	for _, a := range d.Approvals {
		if a.Iteration == 2 && a.Status != "cancelled" {
			t.Fatalf("the pending review must be withdrawn, got %s", a.Status)
		}
	}
	conflict(t, e.eng.Decide(e.ctx, e.approver, gate2.ID, gate2.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "late approval"}), "approval_closed")
}

func TestRejectionClosesCase(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("internal-poc-request", pocVars)
	e.drain()
	scope := openTask(t, e.detail(id), "scope")
	_ = e.eng.CompleteTask(e.ctx, e.owner, scope.ID, scope.VersionNum, cases.CompleteTaskInput{})
	e.drain()
	gate := pendingApproval(t, e.detail(id))
	if err := e.eng.Decide(e.ctx, e.approver, gate.ID, gate.VersionNum, cases.DecisionInput{Decision: "reject", Rationale: "No budget this quarter"}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d := e.detail(id)
	if d.Status != cases.CaseCancelled || d.Outcome == nil || *d.Outcome != "rejected_at_budget_approval" {
		t.Fatalf("rejection must close the case: %s %v", d.Status, d.Outcome)
	}
	for _, x := range d.Timeline {
		if x.NodeID == "provision" {
			t.Fatal("rejected gate must not release downstream steps")
		}
	}
}

func TestRetriesWithBackoffThenFailsCase(t *testing.T) {
	e := newEnv(t)
	var calls atomic.Int32
	e.eng.FaultInjector = func(x cases.ExecInfo) error {
		if x.NodeID == "start" && calls.Add(1) <= 2 {
			return errors.New("transient database hiccup")
		}
		return nil
	}
	id := e.createCase("internal-poc-request", pocVars)
	for range 3 {
		e.drain()
		e.exec(`UPDATE node_executions SET next_attempt_at = now() WHERE case_id = $1 AND status = 'ready'`, id) // skip the backoff wait
	}
	e.drain()
	d := e.detail(id)
	if d.Timeline[0].Attempt != 3 || d.Timeline[0].Status != "completed" {
		t.Fatalf("start should succeed on attempt 3: %+v", d.Timeline[0])
	}
	openTask(t, d, "scope")

	e.eng.FaultInjector = func(x cases.ExecInfo) error { return errors.New("always broken") }
	e.eng.MaxAttempts = 2
	id2 := e.createCase("internal-poc-request", pocVars)
	for range 3 {
		e.drain()
		e.exec(`UPDATE node_executions SET next_attempt_at = now() WHERE case_id = $1 AND status = 'ready'`, id2)
	}
	d = e.detail(id2)
	if d.Status != cases.CaseFailed || d.Timeline[0].Status != "failed" || d.Timeline[0].LastError == nil {
		t.Fatalf("exhausted retries must fail node and case: %s %+v", d.Status, d.Timeline[0])
	}
}

func TestCrashedWorkerLeaseIsReclaimedOnce(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("internal-poc-request", pocVars)
	// Simulate a worker that claimed the start step and died before committing its work.
	e.exec(`UPDATE node_executions SET status = 'running', lease_owner = 'dead-worker', lease_expires_at = now() + interval '1 hour', attempt = 1
		WHERE case_id = $1`, id)
	if n, _ := e.eng.ProcessReady(e.ctx, "worker-b", 10); n != 0 {
		t.Fatal("an unexpired lease must not be stolen")
	}
	e.exec(`UPDATE node_executions SET lease_expires_at = now() - interval '1 second' WHERE case_id = $1`, id)
	e.drain()
	d := e.detail(id)
	if d.Timeline[0].Status != "completed" || d.Timeline[0].Attempt != 2 {
		t.Fatalf("expired lease must be reclaimed: %+v", d.Timeline[0])
	}
	var scopes int
	_ = e.pool.QueryRow(e.ctx, `SELECT count(*) FROM node_executions WHERE case_id = $1 AND node_id = 'scope'`, id).Scan(&scopes)
	if scopes != 1 {
		t.Fatalf("successor must be scheduled exactly once, got %d", scopes)
	}
}

func TestConcurrentWorkersProcessEachStepOnce(t *testing.T) {
	e := newEnv(t)
	ids := make([]string, 15)
	for i := range ids {
		ids[i] = e.createCase("internal-poc-request", pocVars)
	}
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := e.eng.ProcessReady(e.ctx, fmt.Sprintf("worker-%d", w), 3); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	e.drain()
	var tasks, execs int
	_ = e.pool.QueryRow(e.ctx, `SELECT count(*) FROM tasks`).Scan(&tasks)
	_ = e.pool.QueryRow(e.ctx, `SELECT count(*) FROM node_executions`).Scan(&execs)
	if tasks != len(ids) || execs != 2*len(ids) {
		t.Fatalf("expected %d tasks and %d executions, got %d and %d", len(ids), 2*len(ids), tasks, execs)
	}
}

func TestPauseHoldsCaseAndResumeContinues(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("internal-poc-request", pocVars)
	d := e.detail(id)
	denied(t, e.eng.SetPaused(e.ctx, e.approver, id, d.VersionNum, true, "not mine"), "not_case_owner")
	if err := e.eng.SetPaused(e.ctx, e.owner, id, d.VersionNum, true, "waiting for legal"); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d = e.detail(id)
	if d.Status != cases.CasePaused || len(d.Tasks) != 0 {
		t.Fatalf("paused case must not advance: %s %d tasks", d.Status, len(d.Tasks))
	}
	if err := e.eng.SetPaused(e.ctx, e.owner, id, d.VersionNum, false, "legal cleared"); err != nil {
		t.Fatal(err)
	}
	e.drain()
	scope := openTask(t, e.detail(id), "scope")
	d = e.detail(id)
	_ = e.eng.SetPaused(e.ctx, e.owner, id, d.VersionNum, true, "again")
	conflict(t, e.eng.CompleteTask(e.ctx, e.owner, scope.ID, scope.VersionNum, cases.CompleteTaskInput{}), "case_not_active")
}

func TestRunningCaseStaysOnPinnedVersion(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("internal-poc-request", map[string]any{"requester_id": "r", "purpose": "p", "estimated_cost": 0})
	e.drain()
	// Publish v2 that hands provisioning to a different role.
	designer := e.user("designer-v2", auth.KindHuman, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)
	draft, err := e.reg.CreateDraftVersion(e.ctx, designer, registry.Idempotency{Key: "poc-v2-fork", Hash: h(fmt.Sprint("x"))}, "internal-poc-request", 0)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(draft.Version.Definition, &doc)
	for _, n := range doc["nodes"].([]any) {
		if nm := n.(map[string]any); nm["id"] == "provision" {
			nm["config"].(map[string]any)["assignee_role"] = "poc-owner"
		}
	}
	raw, _ := json.Marshal(doc)
	saved, err := e.reg.UpdateDraft(e.ctx, designer, "internal-poc-request", 2, draft.Version.VersionNum, registry.UpdateInput{Definition: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.reg.Publish(e.ctx, designer, "internal-poc-request", 2, saved.Version.VersionNum); err != nil {
		t.Fatal(err)
	}

	scope := openTask(t, e.detail(id), "scope")
	_ = e.eng.CompleteTask(e.ctx, e.owner, scope.ID, scope.VersionNum, cases.CompleteTaskInput{})
	e.drain()
	if p := openTask(t, e.detail(id), "provision"); p.AssigneeRole != "delivery-lead" {
		t.Fatalf("v1 case must follow v1 (delivery-lead), got %s", p.AssigneeRole)
	}
	newer := e.createCase("internal-poc-request", map[string]any{"requester_id": "r", "purpose": "p", "estimated_cost": 0})
	if d := e.detail(newer); d.WorkflowVersion != 2 {
		t.Fatalf("new cases take the latest published version, got v%d", d.WorkflowVersion)
	}
}

func TestCaseCreationGuards(t *testing.T) {
	e := newEnv(t)
	var verr *cases.ValidationError
	_, err := e.eng.CreateCase(e.ctx, e.owner, cases.Idempotency{Key: "sim-case-1", Hash: h(fmt.Sprint("x"))}, cases.CreateInput{WorkflowKey: "sim-tender", Title: "Tender",
		Variables: map[string]any{"opportunity_id": "o", "customer_id": "c", "deadline": "2026-12-01T00:00:00Z"}})
	if !errors.As(err, &verr) || len(verr.Diagnostics) == 0 {
		t.Fatalf("SIM needs an agent runtime: %v", err)
	}
	if verr.Diagnostics[0].Code != "agent_runtime_unavailable" {
		t.Fatalf("expected agent_runtime_unavailable without an agent runtime, got %+v", verr.Diagnostics[0])
	}
	if _, err := e.eng.CreateCase(e.ctx, e.owner, cases.Idempotency{Key: "poc-bad-vars", Hash: h(fmt.Sprint("x"))}, cases.CreateInput{WorkflowKey: "internal-poc-request", Title: "x",
		Variables: map[string]any{"estimated_cost": "a lot"}}); !errors.As(err, &verr) || len(verr.Diagnostics) != 3 {
		t.Fatalf("input_schema must be enforced (2 required + 1 type): %v", err)
	}
	if _, err := e.eng.CreateCase(e.ctx, e.approver, cases.Idempotency{Key: "not-owner-1", Hash: h(fmt.Sprint("x"))}, cases.CreateInput{WorkflowKey: "internal-poc-request", Title: "x"}); !errors.Is(err, cases.ErrForbidden) {
		t.Fatalf("case_owner role required, got %v", err)
	}
	a, err := e.eng.CreateCase(e.ctx, e.owner, cases.Idempotency{Key: "replay-1", Hash: h(fmt.Sprint("h1"))}, cases.CreateInput{WorkflowKey: "internal-poc-request", Title: "x", Variables: pocVars})
	b, err2 := e.eng.CreateCase(e.ctx, e.owner, cases.Idempotency{Key: "replay-1", Hash: h(fmt.Sprint("h1"))}, cases.CreateInput{WorkflowKey: "internal-poc-request", Title: "x", Variables: pocVars})
	if err != nil || err2 != nil || a != b {
		t.Fatalf("idempotent replay must return the same case: %s %s %v %v", a, b, err, err2)
	}
	if _, err := e.eng.CreateCase(e.ctx, e.owner, cases.Idempotency{Key: "replay-1", Hash: h(fmt.Sprint("other"))}, cases.CreateInput{WorkflowKey: "internal-poc-request", Title: "y", Variables: pocVars}); !errors.Is(err, cases.ErrIdempotencyReuse) {
		t.Fatalf("key reuse with another body must fail, got %v", err)
	}
	// No status filter (and "all") must list everything visible: a nil filter reaches SQL as NULL.
	for _, status := range []string{"", "all"} {
		list, _, err := e.eng.ListCases(e.ctx, e.owner, cases.ListFilter{Status: status})
		found := false
		for _, s := range list {
			found = found || s.ID == a
		}
		if err != nil || !found {
			t.Fatalf("owner must see own case with status filter %q: %d %v", status, len(list), err)
		}
	}
	if list, _, err := e.eng.ListCases(e.ctx, e.outsider, cases.ListFilter{}); err != nil || len(list) != 0 {
		t.Fatalf("users without involvement must not see cases: %d %v", len(list), err)
	}
	if _, err := e.eng.GetCase(e.ctx, e.outsider, a); !errors.Is(err, cases.ErrNotFound) {
		t.Fatalf("hidden case must be 404, got %v", err)
	}
}

type flakySink struct{ fails int }

func (s *flakySink) Deliver(context.Context, cases.OutboxEvent) error {
	if s.fails > 0 {
		s.fails--
		return errors.New("consumer down")
	}
	return nil
}

func TestOutboxRetriesAndDeadLetters(t *testing.T) {
	e := newEnv(t)
	e.createCase("internal-poc-request", pocVars)
	sink := &flakySink{fails: 1000}
	if _, err := e.eng.DispatchOutbox(e.ctx, "w", 100, sink); err != nil {
		t.Fatal(err)
	}
	var pending, done, dead int
	count := func() {
		_ = e.pool.QueryRow(e.ctx, `SELECT count(*) FILTER (WHERE status = 'pending'), count(*) FILTER (WHERE status = 'done'),
			count(*) FILTER (WHERE status = 'dead') FROM outbox_events`).Scan(&pending, &done, &dead)
	}
	count()
	if pending == 0 || done != 0 {
		t.Fatalf("failed deliveries must stay pending for retry: pending=%d done=%d", pending, done)
	}
	e.exec(`UPDATE outbox_events SET available_at = now(), attempts = 7`)
	_, _ = e.eng.DispatchOutbox(e.ctx, "w", 100, sink)
	count()
	if dead != pending+dead || dead == 0 {
		t.Fatalf("exhausted events must be dead-lettered: dead=%d", dead)
	}
	e.createCase("internal-poc-request", pocVars)
	sink.fails = 0
	_, _ = e.eng.DispatchOutbox(e.ctx, "w", 100, sink)
	count()
	if done == 0 {
		t.Fatal("healthy deliveries must be marked done")
	}
}

func h(s string) string { return idempotency.RequestHash([]byte(s)) }

func TestMain(m *testing.M) { os.Exit(m.Run()) }
