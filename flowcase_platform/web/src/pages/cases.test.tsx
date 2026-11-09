import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../api/types";
import type { WorkflowVersion } from "../api/workflows";
import { App } from "../app/App";

const session = (roles: Session["roles"]): Session => ({
  user_id: "u", tenant_id: "t", display_name: "Ada Owner", roles, auth_mode: "dev_mock", environment: "development", expires_at: null,
});

const summary = { id: "d", key: "poc", name: "PoC request", description: "Internal PoC", domain_pack: null,
  latest_published_version: 1, open_draft_version: null, version_count: 1, node_count: 3, updated_at: "2026-10-30T10:00:00Z" };

const version: WorkflowVersion = {
  id: "v-id", workflow_key: "poc", version: 1, status: "published", schema_version: "1.0",
  definition: {
    schema_version: "1.0", key: "poc", name: "PoC request", version: 1, status: "published", policy_refs: [],
    nodes: [
      { id: "start", type: "start", name: "New request", next: ["approve"] },
      { id: "approve", type: "human_approval", name: "Approval", next: ["end"] },
      { id: "end", type: "end", name: "Done", next: [] },
    ],
  },
  definition_hash: "a".repeat(64), derived_from_version: null, change_note: "", created_by_name: "Ada", created_at: "2026-10-30T10:00:00Z",
  updated_at: "2026-10-30T10:00:00Z", validated_at: null, published_by_name: "Ada", published_at: "2026-10-30T10:00:00Z", deprecated_at: null, version_num: 2,
};

const caseId = "11111111-1111-4111-8111-111111111111";

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

describe("Cases: new case", () => {
  it("starts a case from the plain form, pinned to the latest published version", async () => {
    const calls = mockApi({
      "/api/v1/session": [200, session(["workspace_member", "case_owner"])],
      "/api/v1/cases": [200, { items: [], next_cursor: null }],
      "/api/v1/workflows?status=published&limit=100": [200, { items: [summary], next_cursor: null }],
      "/api/v1/runtime-capabilities": [200, { supported_node_types: ["start", "human_approval", "end"] }],
      "/api/v1/workflows/poc/versions/1": [200, version],
      "POST /api/v1/cases": [201, { id: caseId, reference: "CASE-2026-0001", workflow_key: "poc", workflow_version: 1 }],
    });
    const user = userEvent.setup();
    renderAt("/cases");

    await user.click(await screen.findByRole("button", { name: "New case" }));
    const dialog = await screen.findByRole("dialog");
    await user.selectOptions(within(dialog).getByLabelText("Workflow"), "poc");
    await user.type(await within(dialog).findByLabelText("Case title *"), "Branch PoC");
    await user.click(within(dialog).getByRole("button", { name: "Start case" }));

    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST");
    expect(post?.url).toBe("/api/v1/cases");
    expect(JSON.parse(String(post?.init.body))).toEqual({ workflow_key: "poc", title: "Branch PoC", variables: {} });
  });

  it("offers the AI assistant as a secondary link to the Case Workspace intake", async () => {
    mockApi({
      "/api/v1/session": [200, session(["workspace_member", "case_owner"])],
      "/api/v1/cases": [200, { items: [], next_cursor: null }],
    });
    renderAt("/cases");
    const link = await screen.findByRole("link", { name: "Create with AI assistant" });
    expect(link).toHaveAttribute("href", "/apps/case-workspace/cases/new");
    expect(screen.getByRole("button", { name: "New case" })).toHaveClass("primary");
  });

  it("shows no case creation to members without the case_owner role", async () => {
    mockApi({
      "/api/v1/session": [200, session(["workspace_member"])],
      "/api/v1/cases": [200, { items: [], next_cursor: null }],
    });
    renderAt("/cases");
    await screen.findByRole("heading", { name: "Cases" });
    expect(screen.queryByRole("button", { name: "New case" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Create with AI assistant" })).not.toBeInTheDocument();
  });
});
