import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../api/types";
import type { WorkflowDefinition, WorkflowVersion } from "../api/workflows";
import { App } from "../app/App";

const session = (roles: Session["roles"]): Session => ({
  user_id: "u", tenant_id: "t", display_name: "Ada Lovelace", roles, auth_mode: "trusted_jwt", environment: "test", expires_at: null,
});

const def: WorkflowDefinition = {
  schema_version: "1.0", key: "poc", name: "PoC request", version: 2, status: "draft", policy_refs: ["poc.budget"],
  nodes: [
    { id: "start", type: "start", name: "New request", next: ["approve"] },
    { id: "approve", type: "human_approval", name: "Budget approval", config: { policy_ref: "poc.budget" }, next: ["end"] },
    { id: "end", type: "end", name: "Done", next: [] },
  ],
};

const version = (status: WorkflowVersion["status"], versionNum = 4): WorkflowVersion => ({
  id: "v-id", workflow_key: "poc", version: 2, status, schema_version: "1.0", definition: { ...def, status },
  definition_hash: status === "published" ? "a".repeat(64) : null, derived_from_version: 1, change_note: "",
  created_by_name: "Ada", created_at: "2026-10-30T10:00:00Z", updated_at: "2026-10-30T10:00:00Z", validated_at: null,
  published_by_name: null, published_at: null, deprecated_at: null, version_num: versionNum,
});

const summary = { id: "d", key: "poc", name: "PoC request", description: "Internal PoC", domain_pack: "internal-poc-request",
  latest_published_version: 1, open_draft_version: 2, version_count: 2, node_count: 3, updated_at: "2026-10-30T10:00:00Z" };

type Reply = [number, unknown] | [number, unknown, Record<string, string>];

function mockApi(routes: Record<string, Reply>) {
  const calls: { method: string; url: string; init: RequestInit }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init = {}) => {
    const url = String(input);
    const method = init.method ?? "GET";
    calls.push({ method, url, init });
    const entry = routes[`${method} ${url}`] ?? routes[url];
    const [status, body, headers] = entry ?? [404, { error: { code: "not_found", message: "nope", trace_id: "t" } }];
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...(headers ?? {}) } });
  });
  return calls;
}

const renderAt = (path: string) => render(<MemoryRouter initialEntries={[path]}><App /></MemoryRouter>);

const common = (roles: Session["roles"]) => ({
  "/api/v1/session": [200, session(roles)] as [number, unknown],
  "/api/v1/workflow-catalog": [200, { policies: [{ ref: "poc.budget", name: "Budget", pack: "poc", provisional: true }], agents: [], connectors: [], assignee_roles: [], artifact_types: [] }] as [number, unknown],
  "/api/v1/workflows/poc": [200, { ...summary, versions: [{ id: "v-id", version: 2, status: "draft", definition_hash: null, change_note: "", node_count: 3, created_by_name: "Ada", created_at: "", updated_at: "", published_by_name: null, published_at: null, deprecated_at: null, version_num: 4 }] }] as [number, unknown],
  "POST /api/v1/workflow-validations": [200, { valid: true, diagnostics: [{ severity: "warning", code: "provisional_policy", message: "pending sign-off", path: "/nodes/1/config/policy_ref", node_id: "approve" }] }] as [number, unknown],
});

describe("Workflow Library", () => {
  it("lists workflows from the API and only offers creation to designers", async () => {
    mockApi({ ...common(["workspace_member"]), "/api/v1/workflows": [200, { items: [summary], next_cursor: null }] });
    renderAt("/workflows");
    const card = await screen.findByRole("link", { name: /PoC request/ });
    expect(within(card).getByText("Published v1")).toBeInTheDocument();
    expect(within(card).getByText("Draft v2")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /New workflow/ })).toBeNull();
  });

  it("creates from a template with an idempotency key and opens the designer", async () => {
    const calls = mockApi({
      ...common(["workspace_member", "workflow_designer"]),
      "/api/v1/workflows": [200, { items: [], next_cursor: null }],
      "/api/v1/workflow-templates": [200, { items: [{ pack: "internal-poc-request", key: "internal-poc-request", name: "Internal Proof-of-Concept Request", description: "" }] }],
      "POST /api/v1/workflows": [201, { version: { ...version("draft", 1), workflow_key: "poc", version: 1 }, validation: { valid: true, diagnostics: [] } }],
      "/api/v1/workflows/poc/versions/1": [200, { ...version("draft", 1), version: 1 }, { ETag: '"1"' }],
    });
    const user = userEvent.setup();
    renderAt("/workflows");
    await user.click(await screen.findByRole("button", { name: /New workflow/ }));
    const dialog = await screen.findByRole("dialog", { name: "New workflow" });
    await user.selectOptions(within(dialog).getByLabelText("Start from"), "internal-poc-request");
    expect(within(dialog).getByLabelText("Key")).toHaveValue("internal-proof-of-concept-request");
    await user.clear(within(dialog).getByLabelText("Key"));
    await user.type(within(dialog).getByLabelText("Key"), "poc");
    await user.click(within(dialog).getByRole("button", { name: "Create draft" }));
    await screen.findByRole("heading", { name: "Workflow Designer" });
    const post = calls.find((c) => c.method === "POST" && c.url === "/api/v1/workflows");
    expect((post?.init.headers as Record<string, string>)["Idempotency-Key"]).toMatch(/^ui-[0-9a-f]{32}$/);
    expect(JSON.parse(String(post?.init.body))).toEqual({ key: "poc", template: "internal-poc-request" });
  });
});

describe("Workflow Designer", () => {
  it("renders the graph, marks provisional policies, and saves drafts with If-Match", async () => {
    const calls = mockApi({
      ...common(["workspace_member", "workflow_designer", "workflow_publisher"]),
      "/api/v1/workflows/poc/versions/2": [200, version("draft"), { ETag: '"4"' }],
      "PATCH /api/v1/workflows/poc/versions/2": [200, { version: { ...version("draft", 5), definition: { ...def, name: "PoC request v2", status: "draft" } }, validation: { valid: true, diagnostics: [] } }, { ETag: '"5"' }],
    });
    const user = userEvent.setup();
    renderAt("/designer/poc/2");
    const node = await screen.findByRole("listitem", { name: /Approval gate Budget approval/ });
    expect(within(node).getByText("1 note")).toBeInTheDocument();

    const publish = screen.getByRole("button", { name: /Publish version/ });
    expect(publish).toBeEnabled();
    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), "PoC request v2");
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
    expect(publish).toBeDisabled();

    await user.click(screen.getByRole("button", { name: /Save draft/ }));
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true));
    const patch = calls.find((c) => c.method === "PATCH");
    expect((patch?.init.headers as Record<string, string>)["If-Match"]).toBe('"4"');
    expect(JSON.parse(String(patch?.init.body)).definition.name).toBe("PoC request v2");
    await waitFor(() => expect(screen.queryByText("Unsaved changes")).toBeNull());
  });

  it("shows a conflict banner instead of overwriting when the draft changed elsewhere", async () => {
    mockApi({
      ...common(["workspace_member", "workflow_designer"]),
      "/api/v1/workflows/poc/versions/2": [200, version("draft"), { ETag: '"4"' }],
      "PATCH /api/v1/workflows/poc/versions/2": [412, { error: { code: "version_mismatch", message: "This version changed since you loaded it.", trace_id: "t" } }],
    });
    const user = userEvent.setup();
    renderAt("/designer/poc/2");
    await screen.findByRole("listitem", { name: /Budget approval/ });
    await user.type(screen.getByLabelText("Name"), "!");
    await user.click(screen.getByRole("button", { name: /Save draft/ }));
    expect(await screen.findByText(/changed elsewhere/)).toBeInTheDocument();
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
  });

  it("is read-only for published versions and offers a new draft instead", async () => {
    mockApi({
      ...common(["workspace_member", "workflow_designer"]),
      "/api/v1/workflows/poc": [200, { ...summary, open_draft_version: null, versions: [] }],
      "/api/v1/workflows/poc/versions/2": [200, version("published"), { ETag: '"6"' }],
    });
    const user = userEvent.setup();
    renderAt("/designer/poc/2");
    await user.click(await screen.findByRole("listitem", { name: /Budget approval/ }));
    expect(screen.getByLabelText("Name")).toHaveAttribute("readonly");
    expect(screen.queryByRole("button", { name: /Save draft/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Publish version/ })).toBeNull();
    expect(screen.getByRole("button", { name: /New draft from v2/ })).toBeInTheDocument();
    expect(screen.getAllByText(/immutable/).length).toBeGreaterThan(0);
  });
});
