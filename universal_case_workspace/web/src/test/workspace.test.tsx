import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { CaseContext, DraftReview } from "../api/types";
import { caseTiming } from "../features/workspace/model";
import { bodyOf, caseFor, headersOf, loadManifest, me, mockApi, renderAt } from "./fixtures";

const manifest = loadManifest("contract-review.v1.json");
const draft: DraftReview = {
  id: "d-1", node_id: "draft", node_name: "Draft generation", iteration: 1, artifact_type: "analysis", artifact_version: 2,
  content: { artifact_draft: { summary: "Draft summary for review" }, review: { status: "needs_review" } },
  status: "pending", feedback: null, decided_by_name: null, decided_at: null, created_at: "2026-10-31T08:00:00Z",
  version_num: 5, can_submit: true, can_revise: true, revisions: 0,
};

beforeEach(() => { caseTiming.pollMs = 20; });
afterEach(() => { caseTiming.pollMs = 4000; });

describe("Case Workspace", () => {
  it("builds the left navigation from the pinned manifest and renders the header and workflow steps", async () => {
    const ctx = caseFor(manifest);
    mockApi({ "/me/context": [200, me], "/cases/case-1": [200, ctx] });
    renderAt("/cases/case-1");
    const nav = await screen.findByRole("navigation", { name: "Case views" });
    const labels = within(nav).getAllByRole("link").map((a) => a.textContent);
    expect(labels).toEqual((manifest.navigation ?? []).map((n) => n.label));
    expect(within(nav).getAllByRole("link")[0]).toHaveAttribute("aria-current", "page");

    expect(screen.getByRole("heading", { level: 1, name: ctx.case.title })).toBeInTheDocument();
    expect(screen.getByText("PT Contoh", { exact: false })).toBeInTheDocument();
    expect(screen.getByText(`v${manifest.version}`)).toBeInTheDocument();
    expect(screen.getByText(/pinned at case creation/)).toBeInTheDocument();
    // Approval step names come from Flowcase data.
    const steps = screen.getByRole("list", { name: "Workflow steps" });
    expect(within(steps).getByText("Readiness approval")).toBeInTheDocument();
    expect(within(steps).getByText(/Approval · Waiting approval/)).toBeInTheDocument();
  });

  it("navigates to an artifact view from the manifest and opens the read-only row inspector", async () => {
    mockApi({ "/me/context": [200, me], "/cases/case-1": [200, caseFor(manifest)] });
    const user = userEvent.setup();
    renderAt("/cases/case-1");
    const second = manifest.navigation?.[1];
    await user.click(await within(await screen.findByRole("navigation", { name: "Case views" })).findByRole("link", { name: second?.label ?? "" }));
    const table = await screen.findByRole("table", { name: manifest.artifact_types?.[0]?.title ?? "" });
    expect(within(table).getAllByText("Master Services Agreement.pdf").length).toBeGreaterThan(0);
    expect(screen.getAllByText("AI draft — needs human review").length).toBeGreaterThan(0);
    await user.click(within(table).getAllByRole("button", { name: /Inspect/ })[0] as HTMLElement);
    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByRole("button", { name: "Edit (coming in Phase 3)" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("renders unknown view kinds as an unsupported card", async () => {
    const ctx = caseFor(manifest, { navigation: [{ id: "overview", view: "overview", label: "Overview" }, { id: "kanban", view: "kanban_board", label: "Board" }] });
    mockApi({ "/me/context": [200, me], "/cases/case-1": [200, ctx] });
    renderAt("/cases/case-1/kanban");
    expect(await screen.findByText(/does not support/)).toBeInTheDocument();
    expect(screen.getByText("kanban_board")).toBeInTheDocument();
  });

  it("submits a draft to the approver with If-Match", async () => {
    const ctx = caseFor(manifest, { drafts: [draft] });
    const after: CaseContext = { ...ctx, drafts: [{ ...draft, status: "submitted", decided_by_name: "Maya Putri", decided_at: "2026-10-31T09:00:00Z" }] };
    const calls = mockApi({ "/me/context": [200, me], "/cases/case-1": [200, ctx], "POST /cases/case-1/drafts/d-1/submit": [200, after] });
    const user = userEvent.setup();
    renderAt("/cases/case-1");
    const review = await screen.findByRole("region", { name: "Your review" });
    expect(within(review).getByText("Draft summary for review")).toBeInTheDocument();
    expect(within(review).getByText("v2")).toBeInTheDocument();
    await user.click(within(review).getByRole("button", { name: "Submit to approver" }));
    await waitFor(() => expect(calls.some((c) => c.url.endsWith("/submit"))).toBe(true));
    const submit = calls.find((c) => c.url.endsWith("/submit"));
    expect(headersOf(submit)["If-Match"]).toBe('"5"');
    expect(headersOf(submit)["Idempotency-Key"]).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole("region", { name: "Your review" })).not.toBeInTheDocument());
    expect(screen.getByText(/Review history \(1\)/)).toBeInTheDocument();
  });

  it("validates revision feedback, sends it with If-Match, then shows the revising state and polls", async () => {
    const ctx = caseFor(manifest, { drafts: [draft] });
    const revising: CaseContext = { ...ctx, drafts: [{ ...draft, status: "revision_requested", feedback: "Add the liability cap" }] };
    let requested = false;
    const calls = mockApi({
      "/me/context": [200, me],
      "/cases/case-1": () => [200, requested ? revising : ctx],
      "POST /cases/case-1/drafts/d-1/revision": () => { requested = true; return [200, revising]; },
    });
    const user = userEvent.setup();
    renderAt("/cases/case-1");
    const review = await screen.findByRole("region", { name: "Your review" });
    const box = within(review).getByLabelText("Request revision");
    expect(within(review).getByText("The agent will regenerate this draft using your feedback")).toBeInTheDocument();
    await user.type(box, "abc");
    expect(within(review).getByText("3/1000")).toBeInTheDocument();
    await user.click(within(review).getByRole("button", { name: "Request revision" }));
    expect(await within(review).findByText(/at least 5 characters/)).toBeInTheDocument();
    expect(box).toHaveAttribute("aria-invalid", "true");
    expect(calls.some((c) => c.url.endsWith("/revision"))).toBe(false);

    await user.type(box, " Add the liability cap");
    await user.click(within(review).getByRole("button", { name: "Request revision" }));
    await waitFor(() => expect(calls.some((c) => c.url.endsWith("/revision"))).toBe(true));
    const rev = calls.find((c) => c.url.endsWith("/revision"));
    expect(headersOf(rev)["If-Match"]).toBe('"5"');
    expect(bodyOf(rev)).toEqual({ feedback: "abc Add the liability cap" });
    expect(await screen.findByText(/Agent is revising Draft generation/)).toBeInTheDocument();
    // The case is polled while the agent re-runs.
    const before = calls.filter((c) => c.url.endsWith("/cases/case-1")).length;
    await waitFor(() => expect(calls.filter((c) => c.url.endsWith("/cases/case-1")).length).toBeGreaterThan(before + 1));
    expect(screen.getByText(/Agent is revising Draft generation/)).toBeInTheDocument();
  });

  it("explains a stale draft (412) and offers a reload", async () => {
    const ctx = caseFor(manifest, { drafts: [draft] });
    mockApi({
      "/me/context": [200, me], "/cases/case-1": [200, ctx],
      "POST /cases/case-1/drafts/d-1/submit": [412, { error: { code: "version_mismatch", message: "stale", trace_id: "t" } }],
    });
    const user = userEvent.setup();
    renderAt("/cases/case-1");
    await user.click(await screen.findByRole("button", { name: "Submit to approver" }));
    expect(await screen.findByText(/Someone changed this since you loaded it/)).toBeInTheDocument();
  });

  it("shows approvals read-only with a Decide in Flowcase link", async () => {
    const ctx = caseFor(manifest, {
      approvals: [{
        id: "ap-1", node_id: "gate-a", title: "Readiness approval", kind: "gate", status: "pending", policy_name: "Two-person rule", policy_ref: "pol.two",
        covered_artifacts: [{ type: "analysis", version: 2, status: "ready_for_review" }], decision: null, created_at: "2026-10-31T08:00:00Z",
      }, {
        id: "ap-0", node_id: "gate-0", title: "Earlier gate", status: "approved", policy_name: "Owner sign-off",
        decision: { decision: "approve", rationale: "Looks complete", decided_by_name: "Raka S.", decided_at: "2026-10-30T10:00:00Z" },
      }],
    });
    mockApi({ "/me/context": [200, me], "/cases/case-1": [200, ctx] });
    const approvalsNav = manifest.navigation?.find((n) => n.view === "approvals");
    renderAt(`/cases/case-1/${approvalsNav?.key}`);
    expect(await screen.findByText(/never decides gates/)).toBeInTheDocument();
    const card = screen.getByRole("article", { name: /Readiness approval/ });
    expect(within(card).getByText("Two-person rule")).toBeInTheDocument();
    expect(within(card).getByText(/analysis v2/)).toBeInTheDocument();
    expect(within(card).getByRole("link", { name: /Decide in Flowcase/ })).toHaveAttribute("href", "/apps/flowcase/approvals");
    expect(within(card).queryByRole("button", { name: /approve/i })).not.toBeInTheDocument();
    const earlier = screen.getByRole("article", { name: /Earlier gate/ });
    expect(within(earlier).getByText(/Looks complete/)).toBeInTheDocument();
    expect(within(earlier).queryByRole("link", { name: /Decide in Flowcase/ })).not.toBeInTheDocument();
  });

  it("falls back to a generic read-only shell when the pinned pack is unavailable", async () => {
    const base = caseFor(manifest);
    const ctx = caseFor(manifest, { navigation: [], artifact_types: [], pack: base.pack && { ...base.pack, status: "unavailable" } });
    mockApi({ "/me/context": [200, me], "/cases/case-1": [200, ctx] });
    renderAt("/cases/case-1");
    expect(await screen.findByText(/is unavailable\./)).toBeInTheDocument();
    expect(screen.getByText(/generic read-only shell/)).toBeInTheDocument();
    const nav = await screen.findByRole("navigation", { name: "Case views" });
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual(["Overview", "Tasks", "Approvals", "Documents", "AI Activity", "Timeline"]);
  });

  it("keeps rendering other panels when one upstream failed", async () => {
    const ctx = caseFor(manifest, { agent_runs: [], partial_errors: [{ source: "agent_studio", code: "agent_studio_unavailable", message: "Agent Studio timed out." }] });
    mockApi({ "/me/context": [200, me], "/cases/case-1": [200, ctx] });
    const aiNav = manifest.navigation?.find((n) => n.view === "ai_activity");
    renderAt(`/cases/case-1/${aiNav?.key}`);
    expect(await screen.findByText(/This panel could not load/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: ctx.case.title })).toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Workflow steps" })).toBeInTheDocument();
  });

  it("aggregates cited sources on the documents view", async () => {
    mockApi({ "/me/context": [200, me], "/cases/case-1": [200, caseFor(manifest)] });
    const docNav = manifest.navigation?.find((n) => n.view === "documents");
    renderAt(`/cases/case-1/${docNav?.key}`);
    const table = await screen.findByRole("table", { name: "Cited sources" });
    expect(within(table).getByText("Master Services Agreement.pdf")).toBeInTheDocument();
    expect(within(table).getByText("p. 4 §2.1 · p4")).toBeInTheDocument();
    for (const t of manifest.artifact_types ?? []) expect(within(table).getByText(`${t.title} v2`)).toBeInTheDocument();
  });
});
