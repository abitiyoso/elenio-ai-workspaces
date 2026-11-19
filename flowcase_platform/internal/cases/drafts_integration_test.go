package cases_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
)

func execOf(d cases.CaseDetail, node, status string) *cases.Execution {
	for i := len(d.Timeline) - 1; i >= 0; i-- {
		if x := d.Timeline[i]; x.NodeID == node && x.Status == status {
			return &x
		}
	}
	return nil
}

func (e *env) auditCount(caseID, action string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM audit_events WHERE case_id = $1 AND action = $2`, caseID, action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// startSIMv3 creates a SIM case on the latest published version (v4: author_review on every agent step), approves G0
// and lets the intake agent produce its first draft.
func (e *env) startSIMv3(rt *testRuntime) (string, cases.CaseDetail) {
	e.t.Helper()
	e.phase3(rt)
	id := e.createCase("sim-tender", simVars)
	e.drain()
	g0 := pendingApproval(e.t, e.detail(id))
	if err := e.eng.Decide(e.ctx, e.approver, g0.ID, g0.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "Bid qualified"}); err != nil {
		e.t.Fatal(err)
	}
	e.drain()
	return id, e.detail(id)
}

// TestSIMv4AcceptsCaseWithoutCRMIDs: opportunity and customer IDs are optional (and hidden in forms)
// until a CRM source of truth exists; only the deadline is required.
func TestSIMv4AcceptsCaseWithoutCRMIDs(t *testing.T) {
	e := newEnv(t)
	e.phase3(&testRuntime{})
	id := e.createCase("sim-tender", map[string]any{"deadline": "2026-12-15T00:00:00Z", "customer_name": "PT Contoh"})
	if d := e.detail(id); d.WorkflowVersion != 4 {
		t.Fatalf("expected a v4 case, got v%d", d.WorkflowVersion)
	}
}

func TestAuthorReviewHoldsAgentDraftForCaseOwner(t *testing.T) {
	e := newEnv(t)
	rt := &testRuntime{autoComplete: true}
	id, d := e.startSIMv3(rt)
	if d.WorkflowVersion != 4 {
		t.Fatalf("new SIM cases start on v4 (author review, optional hidden IDs), got v%d", d.WorkflowVersion)
	}
	draft := pendingDraft(d)
	if draft == nil || draft.NodeID != "intake" || draft.ArtifactType != "requirement-register" || draft.ArtifactVersion != 1 || draft.Iteration != 1 {
		t.Fatalf("the intake result must wait as a pending draft: %+v", d.Drafts)
	}
	if !draft.CanSubmit || !draft.CanRevise || draft.NodeName != "Intake Agent — RFP analysis" || len(draft.Content) == 0 {
		t.Fatalf("the owner may act on the draft and sees its content: %+v", draft)
	}
	if execOf(d, "intake", "waiting_human") == nil || len(d.Approvals) != 1 {
		t.Fatalf("the agent step waits for its author and G1 must not open yet: %+v / %d approvals", d.Timeline, len(d.Approvals))
	}
	if e.auditCount(id, "draft.ready") != 1 {
		t.Fatal("draft.ready must be audited")
	}

	// Only the case owner reviews drafts; approvers and service principals cannot push them on.
	denied(t, e.eng.SubmitDraft(e.ctx, e.approver, draft.ID, draft.VersionNum, ""), "not_case_owner")
	denied(t, e.eng.ReviseDraft(e.ctx, e.approver, draft.ID, draft.VersionNum, "Please add more detail"), "not_case_owner")
	denied(t, e.eng.SubmitDraft(e.ctx, e.robot, draft.ID, draft.VersionNum, ""), "not_case_owner")
	if other, err := e.eng.GetCase(e.ctx, e.approver, id); err != nil || other.Drafts[0].CanSubmit || other.Drafts[0].CanRevise {
		t.Fatalf("non-owners see the draft read-only: %+v %v", other.Drafts, err)
	}
	if list, err := e.eng.ListDrafts(e.ctx, e.approver, ""); err != nil || len(list) != 0 {
		t.Fatalf("the draft inbox lists only the caller's cases: %d %v", len(list), err)
	}
	if list, err := e.eng.ListDrafts(e.ctx, e.owner, ""); err != nil || len(list) != 1 || list[0].ID != draft.ID {
		t.Fatalf("owner inbox: %+v %v", list, err)
	}

	var input *cases.InputError
	if err := e.eng.ReviseDraft(e.ctx, e.owner, draft.ID, draft.VersionNum, "fix"); !errors.As(err, &input) || input.Code != "feedback_required" {
		t.Fatalf("feedback of 5 to 1000 characters is required, got %v", err)
	}
	if err := e.eng.ReviseDraft(e.ctx, e.owner, draft.ID, draft.VersionNum, strings.Repeat("x", 1001)); !errors.As(err, &input) {
		t.Fatalf("feedback over 1000 characters must be rejected, got %v", err)
	}

	// Paused cases accept no review actions.
	if err := e.eng.SetPaused(e.ctx, e.owner, id, d.VersionNum, true, "hold"); err != nil {
		t.Fatal(err)
	}
	conflict(t, e.eng.SubmitDraft(e.ctx, e.owner, draft.ID, draft.VersionNum, ""), "case_not_active")
	if err := e.eng.SetPaused(e.ctx, e.owner, id, e.detail(id).VersionNum, false, "go"); err != nil {
		t.Fatal(err)
	}

	// Revision: the agent re-runs with the feedback and its previous draft, producing v2 and a new review.
	feedback := "Please add the SLA requirements from section 4.2"
	starts := len(rt.starts)
	if err := e.eng.ReviseDraft(e.ctx, e.owner, draft.ID, draft.VersionNum, feedback); err != nil {
		t.Fatal(err)
	}
	conflict(t, e.eng.SubmitDraft(e.ctx, e.owner, draft.ID, draft.VersionNum+1, ""), "draft_not_pending")
	e.drain()
	if len(rt.starts) != starts+1 || rt.starts[starts].AgentID != "sim.intake" {
		t.Fatalf("revision must start exactly one new intake run: %+v", rt.starts[starts:])
	}
	var in struct {
		Iteration    int    `json:"iteration"`
		RevisionNote string `json:"revision_note"`
		Upstream     []struct {
			Type    string          `json:"type"`
			NodeID  string          `json:"node_id"`
			Version int             `json:"version"`
			Content json.RawMessage `json:"content"`
		} `json:"upstream_artifacts"`
	}
	if err := json.Unmarshal(rt.starts[starts].Input, &in); err != nil {
		t.Fatal(err)
	}
	if in.RevisionNote != feedback || in.Iteration != 2 {
		t.Fatalf("the re-run carries the owner's feedback: %+v", in)
	}
	if len(in.Upstream) != 1 || in.Upstream[0].Type != "requirement-register" || in.Upstream[0].NodeID != "intake" || in.Upstream[0].Version != 1 || len(in.Upstream[0].Content) == 0 {
		t.Fatalf("the re-run sees its previous draft first: %+v", in.Upstream)
	}
	d = e.detail(id)
	if reg := artifactByType(t, d, "requirement-register"); reg.CurrentVersion != 2 {
		t.Fatalf("the revised draft is artifact v2, got v%d", reg.CurrentVersion)
	}
	second := pendingDraft(d)
	if second == nil || second.ID == draft.ID || second.ArtifactVersion != 2 || second.Iteration != 2 || second.Revisions != 1 {
		t.Fatalf("a new pending review opens on v2: %+v", d.Drafts)
	}
	var old *cases.DraftReview
	for i := range d.Drafts {
		if d.Drafts[i].ID == draft.ID {
			old = &d.Drafts[i]
		}
	}
	if old == nil || old.Status != "revision_requested" || old.Feedback == nil || *old.Feedback != feedback || old.DecidedByName == nil || *old.DecidedByName != "owner" {
		t.Fatalf("the sent-back review keeps its feedback and reviewer: %+v", old)
	}
	if d.Drafts[0].ID != second.ID || len(d.Approvals) != 1 {
		t.Fatalf("drafts are newest first and G1 still waits: %+v", d.Drafts)
	}

	if !errors.Is(e.eng.SubmitDraft(e.ctx, e.owner, second.ID, second.VersionNum+1, ""), cases.ErrPreconditionFailed) {
		t.Fatal("a stale If-Match must fail")
	}
	// Submit: the step completes and G1 opens on the reviewed version. The gate keeps its own rules.
	if err := e.eng.SubmitDraft(e.ctx, e.owner, second.ID, second.VersionNum, "Looks complete"); err != nil {
		t.Fatal(err)
	}
	e.drain()
	d = e.detail(id)
	g1 := pendingApproval(t, d)
	if g1.NodeID != "g1" || len(g1.Covered) != 1 || g1.Covered[0].Version != 2 {
		t.Fatalf("submit opens G1 covering the reviewed v2: %+v", g1)
	}
	denied(t, e.eng.Decide(e.ctx, e.owner, g1.ID, g1.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "My own draft"}), "missing_role")
	done := execOf(d, "intake", "completed")
	if done == nil || !strings.Contains(string(done.Output), second.ID) {
		t.Fatalf("the completed step records the draft review: %+v", done)
	}
	conflict(t, e.eng.SubmitDraft(e.ctx, e.owner, second.ID, second.VersionNum+1, ""), "draft_not_pending")
	if e.auditCount(id, "draft.revision_requested") != 1 || e.auditCount(id, "draft.submitted") != 1 || e.auditCount(id, "draft.ready") != 2 {
		t.Fatal("review actions must be audited")
	}
	if list, err := e.eng.ListDrafts(e.ctx, e.owner, "all"); err != nil || len(list) != 2 {
		t.Fatalf("history lists both reviews: %d %v", len(list), err)
	}
	if got, err := e.eng.GetDraft(e.ctx, e.owner, draft.ID); err != nil || len(got.Content) == 0 || got.ArtifactVersion != 1 {
		t.Fatalf("a decided review keeps the version it was decided on: %+v %v", got, err)
	}
}

func TestDraftRevisionLimit(t *testing.T) {
	e := newEnv(t)
	rt := &testRuntime{autoComplete: true}
	id, _ := e.startSIMv3(rt)
	// A drain can finish before the asynchronous agent poll's next_attempt_at.
	// Wait for the next review instead of dereferencing a transient nil read model.
	waitDraft := func() *cases.DraftReview {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			e.drain()
			if draft := pendingDraft(e.detail(id)); draft != nil {
				return draft
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("agent did not produce the next pending draft")
		return nil
	}
	for i := range 10 {
		draft := waitDraft()
		if err := e.eng.ReviseDraft(e.ctx, e.owner, draft.ID, draft.VersionNum, "Another round of edits"); err != nil {
			t.Fatalf("revision %d: %v", i+1, err)
		}
		e.drain()
	}
	draft := waitDraft()
	if draft.CanRevise || !draft.CanSubmit || draft.Revisions != 10 || draft.ArtifactVersion != 11 {
		t.Fatalf("after 10 revisions only submit remains: %+v", draft)
	}
	conflict(t, e.eng.ReviseDraft(e.ctx, e.owner, draft.ID, draft.VersionNum, "One more please"), "revision_limit")
}

// The callback path settles into the same draft review; closing the case cancels pending drafts.
func TestAuthorReviewViaCallbackAndCancelledOnClose(t *testing.T) {
	e := newEnv(t)
	rt := &testRuntime{}
	e.phase3(rt)
	e.eng.MaxAttempts = 1
	e.publishDoc("reviewed-agents", `{"schema_version":"1.0","key":"reviewed-agents","name":"Reviewed agents","version":1,"policy_refs":["sim.solution-baseline"],
		"nodes":[{"id":"start","type":"start","name":"Start","next":["fork"]},
		{"id":"fork","type":"parallel_split","name":"Fork","next":["a","b"]},
		{"id":"a","type":"agent_task","name":"Design","config":{"agent_ref":"sim.design","output_artifact_type":"hld","author_review":true},"next":["merge"]},
		{"id":"b","type":"agent_task","name":"Matching","config":{"agent_ref":"sim.solution-matching","output_artifact_type":"capability-fit-gap"},"next":["merge"]},
		{"id":"merge","type":"parallel_join","name":"Merge","config":{"join":"all","split_ref":"fork"},"next":["review"]},
		{"id":"review","type":"human_approval","name":"Review","config":{"policy_ref":"sim.solution-baseline"},"next":["end"]},
		{"id":"end","type":"end","name":"End","next":[]}]}`)
	id := e.createCase("reviewed-agents", nil)
	e.drain()
	runA, runB := rt.runID("sim.design", 1), rt.runID("sim.solution-matching", 1)
	if o, err := e.eng.HandleAgentCallback(e.ctx, "test", signedCallback(t, e.tenant, "r-a-1", runA, "sim.design", agent.RunSucceeded, `{"hld":"v1"}`)); err != nil || o != cases.CallbackApplied {
		t.Fatalf("A result: %s %v", o, err)
	}
	if o, _ := e.eng.HandleAgentCallback(e.ctx, "test", signedCallback(t, e.tenant, "r-a-2", runA, "sim.design", agent.RunSucceeded, `{"hld":"v2"}`)); o != cases.CallbackLate {
		t.Fatalf("a second result while the draft waits must be ignored, got %s", o)
	}
	d := e.detail(id)
	if draft := pendingDraft(d); draft == nil || draft.NodeID != "a" || !strings.Contains(string(draft.Content), `"v1"`) {
		t.Fatalf("the callback result waits as a draft: %+v", d.Drafts)
	}
	// Branch B (no author_review) behaves as before: its result completes the step immediately.
	if o, _ := e.eng.HandleAgentCallback(e.ctx, "test", signedCallback(t, e.tenant, "r-b-1", runB, "sim.solution-matching", agent.RunSucceeded, `{"fit":"good"}`)); o != cases.CallbackApplied {
		t.Fatalf("B result: %s", o)
	}
	e.drain()
	d = e.detail(id)
	if execOf(d, "b", "completed") == nil || len(d.Approvals) != 0 || len(d.Drafts) != 1 {
		t.Fatalf("B completes without review; the join waits for A's draft: %+v", d.Timeline)
	}

	// A failure elsewhere closes the case; the pending draft is cancelled with it.
	id2 := e.createCase("reviewed-agents", nil)
	e.drain()
	if o, _ := e.eng.HandleAgentCallback(e.ctx, "test", signedCallback(t, e.tenant, "r2-a", rt.runID("sim.design", 1), "sim.design", agent.RunSucceeded, `{"hld":"x"}`)); o != cases.CallbackApplied {
		t.Fatalf("A result (case 2): %s", o)
	}
	if o, _ := e.eng.HandleAgentCallback(e.ctx, "test", signedCallback(t, e.tenant, "r2-b", rt.runID("sim.solution-matching", 1), "sim.solution-matching", agent.RunFailed, "")); o != cases.CallbackApplied {
		t.Fatalf("B failure (case 2): %s", o)
	}
	d = e.detail(id2)
	if d.Status != cases.CaseFailed || len(d.Drafts) != 1 || d.Drafts[0].Status != "cancelled" || d.Drafts[0].CanSubmit {
		t.Fatalf("closing the case cancels its pending draft: %s %+v", d.Status, d.Drafts)
	}
	conflict(t, e.eng.SubmitDraft(e.ctx, e.owner, d.Drafts[0].ID, d.Drafts[0].VersionNum, ""), "case_not_active")
}

// Pinned versions without author_review keep today's behavior: the result goes straight to the gate.
func TestAgentStepWithoutAuthorReviewSchedulesNextStep(t *testing.T) {
	e := newEnv(t)
	e.phase3(&testRuntime{autoComplete: true})
	id, err := e.eng.CreateCase(e.ctx, e.owner, cases.Idempotency{Key: "sim-v2-case", Hash: h("sim-v2")},
		cases.CreateInput{WorkflowKey: "sim-tender", Version: 2, Title: "Tender v2", Variables: simVars})
	if err != nil {
		t.Fatal(err)
	}
	d := e.advance(id, "g1")
	if d.WorkflowVersion != 2 || len(d.Drafts) != 0 || pendingApproval(t, d).NodeID != "g1" || execOf(d, "intake", "completed") == nil {
		t.Fatalf("v2 has no draft review: v%d drafts=%d", d.WorkflowVersion, len(d.Drafts))
	}
}
