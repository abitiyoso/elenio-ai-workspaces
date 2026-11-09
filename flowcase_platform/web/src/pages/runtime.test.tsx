import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { Approval, Task } from "../api/cases";
import { setPersona } from "../api/hostBridge";
import type { Session } from "../api/types";
import { App } from "../app/App";

const session: Session = {
  user_id: "u", tenant_id: "t", display_name: "Dian Approver", roles: ["workspace_member", "domain_approver"], auth_mode: "dev_mock",
  environment: "development", expires_at: null, persona: "approver",
  dev_personas: [{ key: "default", display_name: "Ada", roles: ["case_owner"] }, { key: "approver", display_name: "Dian Approver", roles: ["domain_approver"] }],
};

const caseRef = { id: "11111111-1111-4111-8111-111111111111", reference: "CASE-2026-0001", title: "Vector DB pilot", status: "under_review" as const };

const approval = (over: Partial<Approval> = {}): Approval => ({
  kind: "gate", invalidated_reason: null, covered_artifacts: [{ type: "hld", version: 2, status: "ready_for_review", checksum: "c".repeat(64) }],
  id: "22222222-2222-4222-8222-222222222222", case: caseRef, node_id: "budget_approval", iteration: 1, title: "Budget approval",
  policy_ref: "poc.budget", policy_name: "PoC budget approval", policy_provisional: true, return_to: null, subject_hash: "b".repeat(64), subject: {},
  status: "pending", decision: null, created_at: "2026-10-30T10:00:00Z", version_num: 1, can_decide: true, blocked_reason: null, ...over,
});

const task: Task = {
  id: "33333333-3333-4333-8333-333333333333", case: { ...caseRef, status: "active" }, node_id: "draft", iteration: 2, title: "Write draft",
  assignee_role: "poc-owner", instructions: "", revision_note: "Add the cost breakdown", status: "assigned", output: null, completed_by_name: null,
  form_schema: { type: "object", required: ["summary"], properties: { summary: { type: "string" }, cost: { type: "number" } } },
  due_at: null, created_at: "2026-10-30T10:00:00Z", completed_at: null, version_num: 3, can_complete: true, can_request_revision: false,
};

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

describe("Approvals", () => {
  it("requires a rationale, sends If-Match, and explains provisional policies", async () => {
    const calls = mockApi({
      "/api/v1/session": [200, session],
      "/api/v1/approvals?status=pending": [200, { items: [approval()] }],
      [`POST /api/v1/approvals/${approval().id}/decision`]: [200, approval({ status: "approved", can_decide: false })],
    });
    const user = userEvent.setup();
    renderAt("/approvals");
    const card = await screen.findByRole("region", { name: /Decision for Budget approval/ });
    expect(within(card).getByText("provisional")).toBeInTheDocument();
    const approve = within(card).getByRole("button", { name: /Approve/ });
    expect(approve).toBeDisabled();
    await user.type(within(card).getByLabelText(/Rationale/), "Within the quarterly budget");
    await user.click(approve);
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST");
    expect((post?.init.headers as Record<string, string>)["If-Match"]).toBe('"1"');
    expect(JSON.parse(String(post?.init.body))).toEqual({ decision: "approve", rationale: "Within the quarterly budget" });
  });

  it("explains segregation of duties instead of offering a decision", async () => {
    mockApi({
      "/api/v1/session": [200, session],
      "/api/v1/approvals?status=pending": [200, { items: [approval({ can_decide: false, blocked_reason: "sod_task_submitter" })] }],
    });
    renderAt("/approvals");
    expect(await screen.findByText(/you submitted work in this case/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Approve/ })).toBeNull();
  });
});

describe("Change invalidation", () => {
  it("marks re-reviews and explains why a gate is blocked", async () => {
    mockApi({
      "/api/v1/session": [200, session],
      "/api/v1/approvals?status=pending": [200, { items: [
        approval({ id: "44444444-4444-4444-8444-444444444444", title: "G5 — Release", can_decide: false, blocked_reason: "rereview_pending",
          covered_artifacts: [{ type: "wbs", version: 1, status: "stale", checksum: "d".repeat(64) }] }),
        approval({ kind: "rereview", title: "G3 — Delivery & cost" }),
      ] }],
    });
    renderAt("/approvals?focus=44444444-4444-4444-8444-444444444444");
    expect(await screen.findByText(/must be re-approved first/)).toBeInTheDocument();
    expect(screen.getAllByText("wbs v1").length).toBeGreaterThan(0);
    expect(screen.getByText("re-review")).toBeInTheDocument();
  });
});

describe("Tasks", () => {
  it("shows rework notes and submits typed form output with If-Match", async () => {
    const calls = mockApi({
      "/api/v1/session": [200, { ...session, roles: ["workspace_member"] }],
      "/api/v1/tasks?status=open": [200, { items: [task] }],
      [`POST /api/v1/tasks/${task.id}/complete`]: [200, { ...task, status: "submitted" }],
    });
    const user = userEvent.setup();
    renderAt("/tasks");
    expect(await screen.findByText("Rework #2")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Complete" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/Add the cost breakdown/)).toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Summary *"), "Costs added");
    await user.type(within(dialog).getByLabelText("Cost"), "1200");
    await user.click(within(dialog).getByRole("button", { name: "Submit" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST");
    expect((post?.init.headers as Record<string, string>)["If-Match"]).toBe('"3"');
    expect(JSON.parse(String(post?.init.body))).toEqual({ output: { summary: "Costs added", cost: 1200 }, comment: "" });
  });
});

describe("Dev personas", () => {
  it("sends the selected persona key and nothing else about identity", async () => {
    const calls = mockApi({ "/api/v1/session": [200, session], "/api/v1/cases": [200, { items: [], next_cursor: null }] });
    setPersona("approver");
    renderAt("/cases");
    await screen.findByText("No cases");
    const headers = calls.map((c) => c.init.headers as Record<string, string>);
    expect(headers.every((h) => h["X-Flowcase-Dev-Persona"] === "approver" && !("X-User-ID" in h))).toBe(true);
    expect(screen.getByLabelText("Development persona")).toHaveValue("approver");
    setPersona("default");
  });
});

describe("Integrations", () => {
  it("shows pack bindings and never presents an unratified policy as approved", async () => {
    mockApi({
      "/api/v1/session": [200, session],
      "/api/v1/integrations": [200, { host_auth: { mode: "dev_mock", status: "warning", message: "" }, knowledge: { mode: "stub", status: "stub", message: "" },
        agent_runtime: { mode: "stub", status: "stub", message: "" }, case_runtime: { mode: "postgres", status: "ok", message: "" } }],
      "/api/v1/workflow-catalog": [200, {
        agents: [{ ref: "sim.intake", name: "Intake Agent", pack: "sim-tender", skill_version: "2026.10", runtime_agent_id: "runtime/intake-v3" }],
        policies: [
          { ref: "sim.release", name: "G5 — Release authorization", pack: "sim-tender", provisional: true, sod: ["not_case_owner", "not_task_submitter"] },
          { ref: "poc.budget", name: "PoC budget", pack: "poc", provisional: false, sod: ["not_case_owner"], ratification: { reference: "DoA-2026-14", date: "2026-10-28", by: "Finance" } },
        ],
        connectors: [], assignee_roles: [], artifact_types: [],
      }],
    });
    renderAt("/integrations");
    const bindings = await screen.findByRole("region", { name: "Domain pack bindings" });
    expect(within(bindings).getByText("runtime/intake-v3")).toBeInTheDocument();
    expect(within(bindings).getByText("provisional")).toBeInTheDocument();
    expect(within(bindings).getByText(/ratified · DoA-2026-14/)).toBeInTheDocument();
  });
});
