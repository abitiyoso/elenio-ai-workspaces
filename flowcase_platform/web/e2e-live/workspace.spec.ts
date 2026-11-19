import {
  test,
  expect,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import { randomUUID } from "node:crypto";
import AxeBuilder from "@axe-core/playwright";

// No route interception. These tests mutate only the explicitly started local E2E sandbox.
const unique = () => `e2e-${randomUUID().slice(0, 18)}`;
const path = (key: string) => `/api/v1/workflows/${key}/versions/1`;
async function post(api: APIRequestContext, url: string, data: unknown = {}) {
  const r = await api.post(url, {
    data,
    headers: { "Idempotency-Key": unique() },
  });
  expect(r.ok(), `${url}: ${await r.text()}`).toBe(true);
  return r.json();
}
async function openStart(page: Page) {
  await page.getByRole("button", { name: "Find a node" }).click();
  await page.getByLabel("Find graph node").fill("Start");
  await page.getByRole("button", { name: /Start Trigger/ }).click();
}
test.beforeEach(async ({ request }) => {
  expect(process.env.ELENIO_LOCAL_E2E).toBe("1");
  const session = await (await request.get("/api/v1/session")).json();
  expect(session).toMatchObject({
    auth_mode: "dev_mock",
    environment: "development",
    display_name: "Local E2E developer",
  });
  expect((await request.get("/readyz")).ok()).toBe(true);
});

test("real database: UI blank creation, edit, optimistic save, validation and reload", async ({
  page,
  request,
}) => {
  const key = unique();
  await page.goto("/elenio.html#/connected");
  await page.getByRole("button", { name: "New workflow", exact: true }).click();
  await page
    .getByLabel("Workflow name", { exact: true })
    .fill("Local E2E workflow");
  await page.getByLabel("Workflow key").fill(key);
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(2);
  await openStart(page);
  await page.getByLabel("Node name", { exact: true }).fill("Reviewed start");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(
    page.getByText("Draft saved by Flowcase. No version was published."),
  ).toBeVisible();
  const saved = await (await request.get(path(key))).json();
  expect(
    saved.definition.nodes.find((n: { type: string }) => n.type === "start")
      .name,
  ).toBe("Reviewed start");
  await page.getByRole("button", { name: "Validate with server" }).click();
  await expect(page.locator(".server-diagnostics")).toContainText(
    "Server validation passed",
  );
  await page.reload();
  await expect(page.locator(".react-flow__node")).toHaveCount(2);
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/live-editor-1440.png",
    fullPage: true,
  });
});

test("real database: concurrent write returns 412 and preserves UI edit", async ({
  page,
  request,
}) => {
  const key = unique();
  await post(request, "/api/v1/workflows", { key, name: "Concurrent draft" });
  await page.goto(`/elenio.html#/connected/projects/${key}/1`);
  await expect(page.locator(".react-flow__node")).toHaveCount(2);
  const loaded = await (await request.get(path(key))).json();
  loaded.definition.name = "Other engineer saved";
  expect(
    (
      await request.patch(path(key), {
        data: { definition: loaded.definition },
        headers: { "If-Match": `"${loaded.version_num}"` },
      })
    ).ok(),
  ).toBe(true);
  await openStart(page);
  await page.getByLabel("Node name", { exact: true }).fill("Keep my edit");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText(/This draft changed elsewhere/)).toBeVisible();
  await expect(page.getByLabel("Node name", { exact: true })).toHaveValue(
    "Keep my edit",
  );
});

test("real database: published graph is immutable, forks a draft and executes a pinned case", async ({
  page,
  request,
}) => {
  const key = unique();
  await post(request, "/api/v1/workflows", {
    key,
    name: "Local runtime acceptance",
  });
  const validated = await post(request, `${path(key)}/validate`);
  const pub = await request.post(`${path(key)}/publish`, {
    headers: { "If-Match": `"${validated.version.version_num}"` },
  });
  expect(pub.ok(), await pub.text()).toBe(true);
  await page.goto(`/elenio.html#/connected/projects/${key}/1`);
  await expect(
    page.getByText("This version is immutable.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Save draft", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Create draft from v1" }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${key}/2$`));
  const started = await post(request, "/api/v1/cases", {
    workflow_key: key,
    title: "Local runtime acceptance",
    variables: {},
  });
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/v1/cases/${started.id}`)).json())
          .status,
    )
    .toBe("completed");
  const detail = await (
    await request.get(`/api/v1/cases/${started.id}`)
  ).json();
  expect(detail.workflow_version).toBe(1);
  expect(detail.progress).toBe(100);
  await page.goto(`/elenio.html#/connected/projects/${key}/1`);
  await page
    .getByRole("button", { name: "Execution trace", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "Case execution", exact: true })
    .selectOption(started.id);
  await expect(page.locator(".execution-detail")).toContainText(
    "100% complete",
  );
  await expect(page.locator(".execution-detail")).toContainText("completed");
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/live-execution-1440.png",
    fullPage: true,
  });
  expect(
    detail.timeline.some(
      (e: { node_type: string; status: string }) =>
        e.node_type === "end" && e.status === "completed",
    ),
  ).toBe(true);
});

test("real server permissions deny viewer writes without changing identity claims", async ({
  request,
}) => {
  const headers = {
    "X-Flowcase-Dev-Persona": "viewer",
    "Idempotency-Key": unique(),
  };
  const session = await (
    await request.get("/api/v1/session", { headers })
  ).json();
  expect(session.display_name).toBe("Local viewer");
  const denied = await request.post("/api/v1/workflows", {
    headers,
    data: { key: unique(), name: "Forbidden creation" },
  });
  expect(denied.status()).toBe(403);
});

test("native branching import preserves config and both branches execute on the real worker", async ({
  page,
  request,
}) => {
  const key = unique();
  const definition = {
    schema_version: "1.0",
    key,
    name: "Local branch acceptance",
    version: 1,
    input_schema: {
      type: "object",
      properties: { matched: { type: "boolean" } },
      required: ["matched"],
    },
    policy_refs: [],
    metadata: { purpose: "local-acceptance" },
    nodes: [
      { id: "start", type: "start", name: "Start", next: ["decision"] },
      {
        id: "decision",
        type: "condition",
        name: "Matched?",
        config: {
          expression: { operator: "eq", field: "matched", value: true },
          branches: { true: "accepted", false: "exception" },
        },
        next: ["accepted", "exception"],
      },
      { id: "accepted", type: "end", name: "Accepted", next: [] },
      { id: "exception", type: "end", name: "Exception", next: [] },
    ],
  };
  await page.goto("/elenio.html#/connected?create=import");
  await page
    .getByLabel("Workflow name", { exact: true })
    .fill("Local branch acceptance");
  await page.getByLabel("Workflow key").fill(key);
  await page.getByLabel("New workflow JSON").fill(JSON.stringify(definition));
  await page
    .getByRole("button", { name: "Preview import", exact: true })
    .click();
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(4);
  const stored = await (await request.get(path(key))).json();
  expect(
    stored.definition.nodes.find((n: { id: string }) => n.id === "decision")
      .config,
  ).toEqual(definition.nodes[1].config);
  expect(stored.definition.input_schema).toEqual(definition.input_schema);
  const validated = await post(request, `${path(key)}/validate`);
  expect(validated.validation.valid).toBe(true);
  expect(
    (
      await request.post(`${path(key)}/publish`, {
        headers: { "If-Match": `"${validated.version.version_num}"` },
      })
    ).ok(),
  ).toBe(true);
  for (const matched of [true, false]) {
    const c = await post(request, "/api/v1/cases", {
      workflow_key: key,
      title: `Local branch ${matched}`,
      variables: { matched },
    });
    await expect
      .poll(
        async () =>
          (await (await request.get(`/api/v1/cases/${c.id}`)).json()).status,
      )
      .toBe("completed");
    const result = await (await request.get(`/api/v1/cases/${c.id}`)).json();
    expect(result.condition_evaluations[0]).toMatchObject({
      result: matched,
      target: matched ? "accepted" : "exception",
    });
  }
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/live-branching-1440.png",
    fullPage: true,
  });
});

test("real human process: task, independent approval, audit and completion", async ({
  page,
  request,
}) => {
  const c = await post(request, "/api/v1/cases", {
    workflow_key: "internal-poc-request",
    title: "Local independent approval acceptance",
    variables: {
      requester_id: "local-requester",
      purpose: "Test governed manual workflow",
      estimated_cost: 10,
    },
  });
  const detail = async () =>
    await (await request.get(`/api/v1/cases/${c.id}`)).json();
  await expect
    .poll(async () =>
      (await detail()).tasks.some(
        (t: { node_id: string; status: string }) =>
          t.node_id === "scope" && t.status === "assigned",
      ),
    )
    .toBe(true);
  const scope = (await detail()).tasks.find(
    (t: { node_id: string; status: string }) =>
      t.node_id === "scope" && t.status === "assigned",
  );
  expect(
    (
      await request.post(`/api/v1/tasks/${scope.id}/complete`, {
        headers: { "If-Match": `"${scope.version_num}"` },
        data: { output: {}, comment: "Scope verified locally" },
      })
    ).ok(),
  ).toBe(true);
  await expect
    .poll(async () =>
      (await detail()).approvals.some(
        (a: { status: string }) => a.status === "pending",
      ),
    )
    .toBe(true);
  const approval = (await detail()).approvals.find(
    (a: { status: string }) => a.status === "pending",
  );
  const decision = {
    decision: "approve",
    rationale: "Budget independently checked for local acceptance",
  };
  expect(
    (
      await request.post(`/api/v1/approvals/${approval.id}/decision`, {
        headers: { "If-Match": `"${approval.version_num}"` },
        data: decision,
      })
    ).status(),
  ).toBe(403);
  const authorized = await request.post(
    `/api/v1/approvals/${approval.id}/decision`,
    {
      headers: {
        "If-Match": `"${approval.version_num}"`,
        "X-Flowcase-Dev-Persona": "approver",
      },
      data: decision,
    },
  );
  expect(authorized.ok(), await authorized.text()).toBe(true);
  await expect
    .poll(async () =>
      (await detail()).tasks.some(
        (t: { node_id: string; status: string }) =>
          t.node_id === "provision" && t.status === "assigned",
      ),
    )
    .toBe(true);
  const provision = (await detail()).tasks.find(
    (t: { node_id: string; status: string }) =>
      t.node_id === "provision" && t.status === "assigned",
  );
  expect(
    (
      await request.post(`/api/v1/tasks/${provision.id}/complete`, {
        headers: { "If-Match": `"${provision.version_num}"` },
        data: { output: {}, comment: "Resources assigned locally" },
      })
    ).ok(),
  ).toBe(true);
  await expect.poll(async () => (await detail()).status).toBe("completed");
  const finished = await detail();
  expect(finished.approvals[0].decision.decided_by_name).toBe(
    "Local independent approver",
  );
  expect(finished.approvals[0].decision.decision_hash).toHaveLength(64);
  await page.goto("/elenio.html#/connected/projects/internal-poc-request/1");
  await page
    .getByRole("button", { name: "Execution trace", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "Case execution", exact: true })
    .selectOption(c.id);
  await expect(page.locator(".execution-detail")).toContainText(
    "100% complete",
  );
  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    axe.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
  await page.getByRole("button", {name:"Execution trace",exact:true}).focus();
  await page.locator(".execution-detail table").scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/live-human-approval-1440.png",
    fullPage: true,
  });
});
