import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { CaseDetail, DraftReview, Execution } from "../api/cases";
import type { Session } from "../api/types";
import { App } from "../app/App";

const session: Session = {
  user_id: "u", tenant_id: "t", display_name: "Ada Owner", roles: ["workspace_member", "case_owner"], auth_mode: "dev_mock",
  environment: "development", expires_at: null,
};

const caseId = "11111111-1111-4111-8111-111111111111";
const caseRef = { id: caseId, reference: "CASE-2026-0007", title: "SD-WAN tender", status: "active" as const };

const draft = (over: Partial<DraftReview> = {}): DraftReview => ({
  id: "55555555-5555-4555-8555-555555555555", case: caseRef, node_id: "intake", node_name: "Intake Agent — RFP analysis", iteration: 1,
  artifact_type: "requirement-register", artifact_version: 1, artifact_version_id: "66666666-6666-4666-8666-666666666666",
  content: { requirements: [{ id: "R-001", statement: "Dual uplinks per branch" }] }, status: "pending", feedback: null, decided_by_name: null,
  decided_at: null, created_at: "2026-10-31T09:00:00Z", version_num: 2, can_submit: true, can_revise: true, revisions: 0, ...over,
});

const exec = (over: Partial<Execution>): Execution => ({
  id: "x", node_id: "intake", node_name: "Intake Agent — RFP analysis", node_type: "agent_task", iteration: 1, status: "waiting_human",
  attempt: 1, last_error: null, created_at: "2026-10-31T09:00:00Z", completed_at: null, ...over,
});

const detail = (drafts: DraftReview[], timeline: Execution[] = [exec({})]): CaseDetail => ({
  ...caseRef, outcome: null, workflow_key: "sim-tender", workflow_name: "SIM B2B Tender Lifecycle", workflow_version: 3, definition_hash: "a".repeat(64),
  owner_name: "Ada Owner", current_steps: ["intake"], open_tasks: 0, pending_approvals: 0, progress: 20, created_at: "2026-10-31T08:00:00Z",
  updated_at: "2026-10-31T09:00:00Z", version_num: 5, variables: {}, business_facets: {},
  workflow_nodes: [
    { id: "start", name: "Case created", type: "start", next: ["intake"] },
    { id: "intake", name: "Intake Agent — RFP analysis", type: "agent_task", next: ["g1"] },
    { id: "g1", name: "G1 — Requirement baseline", type: "human_approval", next: ["end"] },
    { id: "end", name: "Done", type: "end", next: [] },
  ],
  timeline, tasks: [], approvals: [], condition_evaluations: [], history: [], actions: { can_pause: true, can_resume: false, can_note: true },
  artifacts: [], agent_runs: [], drafts,
});

type Reply = [number, unknown];
function mockApi(routes: Record<string, Reply>) {
  const calls: { method: string; url: string; init: RequestInit }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init = {}) => {
    const url = String(input);
    const method = init.method ?? "GET";
    calls.push({ method, url, init });
    const [status, body] = routes[`${method} ${url}`] ?? routes[url] ?? [404, { error: { code: "not_found", message: "nope", trace_id: "t" } }];
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  });
  return calls;
}

const renderAt = (path: string) => render(<MemoryRouter initialEntries={[path]}><App /></MemoryRouter>);

afterEach(() => vi.restoreAllMocks());

describe("Author draft review", () => {
  it("lets the case owner submit the draft to the approver with If-Match", async () => {
    const d = draft();
    const calls = mockApi({
      "/api/v1/session": [200, session],
      [`/api/v1/cases/${caseId}`]: [200, detail([d])],
      [`POST /api/v1/drafts/${d.id}/submit`]: [200, { ...d, status: "submitted" }],
    });
    const user = userEvent.setup();
    renderAt(`/cases/${caseId}`);
    const panel = await screen.findByRole("region", { name: "Your review" });
    const card = within(panel).getByRole("article", { name: /Draft Intake Agent — RFP analysis v1/ });
    expect(within(card).getByText("Dual uplinks per branch")).toBeInTheDocument();
    await user.click(within(card).getByRole("button", { name: "Submit to approver" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST");
    expect(post?.url).toBe(`/api/v1/drafts/${d.id}/submit`);
    expect((post?.init.headers as Record<string, string>)["If-Match"]).toBe('"2"');
  });

  it("sends feedback (5 to 1000 characters) for a revision and counts characters", async () => {
    const d = draft();
    const calls = mockApi({
      "/api/v1/session": [200, session],
      [`/api/v1/cases/${caseId}`]: [200, detail([d])],
      [`POST /api/v1/drafts/${d.id}/revision`]: [200, { ...d, status: "revision_requested" }],
    });
    const user = userEvent.setup();
    renderAt(`/cases/${caseId}`);
    const card = await screen.findByRole("article", { name: /Draft Intake Agent/ });
    expect(within(card).getByText("The agent will regenerate this draft using your feedback")).toBeInTheDocument();
    const send = within(card).getByRole("button", { name: "Request revision" });
    const box = within(card).getByLabelText("Request revision");
    await user.type(box, "Add");
    expect(send).toBeDisabled();
    expect(within(card).getByText("3/1000")).toBeInTheDocument();
    await user.type(box, " the SLA section");
    expect(box).toHaveAttribute("maxLength", "1000");
    await user.click(send);
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST");
    expect(post?.url).toBe(`/api/v1/drafts/${d.id}/revision`);
    expect(JSON.parse(String(post?.init.body))).toEqual({ feedback: "Add the SLA section" });
    expect((post?.init.headers as Record<string, string>)["If-Match"]).toBe('"2"');
  });

  it("shows other people the draft read-only, waiting for the case owner", async () => {
    mockApi({
      "/api/v1/session": [200, session],
      [`/api/v1/cases/${caseId}`]: [200, detail([draft({ can_submit: false, can_revise: false })])],
    });
    renderAt(`/cases/${caseId}`);
    const panel = await screen.findByRole("region", { name: "Draft review" });
    expect(within(panel).getByText("Waiting for the case owner's review.")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: "Submit to approver" })).toBeNull();
    expect(within(panel).queryByLabelText("Request revision")).toBeNull();
  });

  it("shows the agent revising after feedback, with the review history", async () => {
    const sentBack = draft({ status: "revision_requested", feedback: "Add the SLA section", decided_by_name: "Ada Owner",
      decided_at: "2026-10-31T09:30:00Z", can_submit: false, can_revise: false, content: null });
    mockApi({
      "/api/v1/session": [200, session],
      [`/api/v1/cases/${caseId}`]: [200, detail([sentBack], [exec({ status: "superseded" }), exec({ id: "y", iteration: 2, status: "waiting_external" })])],
    });
    renderAt(`/cases/${caseId}`);
    expect(await screen.findByText(/Agent is revising Intake Agent — RFP analysis \(iteration 2\)/)).toBeInTheDocument();
    expect(screen.getByText(/revision requested by Ada Owner: “Add the SLA section”/)).toBeInTheDocument();
  });
});

describe("Tasks · drafts to review", () => {
  it("lists the caller's pending drafts with a link to the case", async () => {
    mockApi({
      "/api/v1/session": [200, session],
      "/api/v1/tasks?status=open": [200, { items: [] }],
      "/api/v1/drafts?status=pending": [200, { items: [draft({ artifact_version: 2, revisions: 1 })] }],
    });
    renderAt("/tasks");
    const section = await screen.findByRole("region", { name: "Drafts to review" });
    expect(within(section).getByText(/Intake Agent — RFP analysis · v2/)).toBeInTheDocument();
    expect(within(section).getByRole("link", { name: "CASE-2026-0007" })).toHaveAttribute("href", `/cases/${caseId}`);
    expect(within(section).getByText("revised 1×")).toBeInTheDocument();
  });
});
