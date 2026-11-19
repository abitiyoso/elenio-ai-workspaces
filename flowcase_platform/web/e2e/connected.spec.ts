import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { vendorPayment } from "../src/platform/fixtures";

const clone = <T>(x: T): T => JSON.parse(JSON.stringify(x)) as T;
function version(key = "vendor-payment", v = 1, status = "draft") {
  return {
    id: `${key}-v${v}`,
    workflow_key: key,
    version: v,
    status,
    schema_version: "1.0",
    definition: { ...clone(vendorPayment), key, version: v, status },
    definition_hash: null,
    derived_from_version: null,
    change_note: "",
    created_by_name: "Contract fixture",
    created_at: "2026-11-19T01:00:00Z",
    updated_at: "2026-11-19T01:00:00Z",
    validated_at: null,
    published_by_name: null,
    published_at: null,
    deprecated_at: null,
    version_num: 3,
  };
}
async function contract(
  page: Page,
  options: {
    roles?: string[];
    sessionStatus?: number;
    capsStatus?: number;
    conflict?: boolean;
    createRetry?: boolean;
    malformedSession?: boolean;
  } = {},
) {
  let current = version();
  let creates = 0;
  const writes: {
    method: string;
    path: string;
    body: unknown;
    headers: Record<string, string>;
  }[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname,
      method = request.method();
    const body = request.postData()
      ? (request.postDataJSON() as Record<string, unknown>)
      : null;
    const json = (value: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(value),
      });
    if (method !== "GET")
      writes.push({ method, path, body, headers: request.headers() });
    if (path === "/api/v1/session")
      return json(
        options.malformedSession
          ? {}
          : options.sessionStatus
            ? {
                error: {
                  code: "unauthenticated",
                  message: "Sign in through the workspace.",
                },
              }
            : {
                user_id: "fixture-user",
                tenant_id: "fixture-tenant",
                display_name: "Contract test fixture",
                roles: options.roles ?? ["workflow_designer"],
                auth_mode: "dev_mock",
                environment: "test",
                expires_at: null,
              },
        options.sessionStatus ?? 200,
      );
    if (path === "/api/v1/runtime-capabilities")
      return json(
        options.capsStatus
          ? {
              error: {
                code: "unavailable",
                message: "Capability endpoint unavailable.",
              },
            }
          : {
              supported_node_types: [
                "start",
                "end",
                "agent_task",
                "service_task",
                "human_approval",
                "human_task",
                "condition",
                "parallel_split",
                "parallel_join",
                "timer",
              ],
            },
        options.capsStatus ?? 200,
      );
    if (path === "/api/v1/workflow-catalog")
      return json({
        agents: [
          {
            ref: "approved.extract@2",
            name: "Registered extractor",
            pack: "test",
            skill_version: "2.0.0",
            runtime_agent_id: "registered-extractor",
          },
        ],
        connectors: [
          {
            ref: "approved.erp@1",
            name: "ERP sandbox",
            pack: "test",
            operations: ["match", "lookup"],
          },
        ],
        policies: [
          {
            ref: "finance@3",
            name: "Finance policy",
            pack: "test",
            provisional: true,
            sod: ["requester != reviewer"],
          },
        ],
        assignee_roles: ["finance"],
        artifact_types: ["invoice"],
      });
    if (path === "/api/v1/workflow-templates")
      return json({
        items: [
          {
            key: "sim-tender",
            pack: "sim",
            name: "SIM Tender",
            description: "Source-backed template; runtime bindings required.",
          },
        ],
      });
    if (path === "/api/v1/workflows" && method === "GET")
      return json({
        items: [
          {
            id: "w1",
            key: current.workflow_key,
            name: current.definition.name,
            description: "API contract fixture, not a live deployment.",
            domain_pack: null,
            latest_published_version: 2,
            open_draft_version: 1,
            version_count: 2,
            node_count: current.definition.nodes.length,
            updated_at: current.updated_at,
          },
        ],
        next_cursor: null,
      });
    if (path === "/api/v1/workflows" && method === "POST") {
      creates++;
      if (options.createRetry && creates === 1)
        return json(
          {
            error: { code: "unavailable", message: "Temporary create failure" },
          },
          503,
        );
      const key = String(body?.key),
        name = String(body?.name);
      current = version(key);
      current.version_num = 1;
      current.definition = {
        ...current.definition,
        name,
        ...(!body?.definition && !body?.template
          ? {
              nodes: [
                {
                  id: "start",
                  type: "start" as const,
                  name: "Start",
                  next: ["end"],
                },
                { id: "end", type: "end" as const, name: "End", next: [] },
              ],
            }
          : {}),
        ...((body?.definition as object) ?? {}),
      };
      current.definition.key = key;
      current.definition.version = 1;
      return json(
        { version: current, validation: { valid: true, diagnostics: [] } },
        201,
      );
    }
    if (path === "/api/v1/workflow-validations")
      return json({
        valid: false,
        diagnostics: [
          {
            severity: "error",
            code: "missing_binding",
            node_id: "extract",
            path: "/nodes/1/config/agent_ref",
            message: "Bind an approved agent version.",
          },
        ],
      });
    if (/\/versions\/\d+$/.test(path)) {
      if (method === "PATCH") {
        if (options.conflict)
          return json(
            {
              error: {
                code: "version_mismatch",
                message: "Draft changed elsewhere.",
              },
            },
            412,
          );
        if (request.headers()["if-match"] !== `"${current.version_num}"`)
          return json(
            {
              error: {
                code: "precondition_failed",
                message: "Wrong version token",
              },
            },
            412,
          );
        current = {
          ...current,
          definition: body?.definition as typeof current.definition,
          version_num: current.version_num + 1,
        };
        return json({
          version: current,
          validation: { valid: true, diagnostics: [] },
        });
      }
      return json(
        path.endsWith("/2")
          ? version(current.workflow_key, 2, "published")
          : current,
      );
    }
    if (path.endsWith("/versions") && method === "POST")
      return json({
        version: current,
        validation: { valid: true, diagnostics: [] },
      });
    if (/^\/api\/v1\/workflows\/[^/]+$/.test(path))
      return json({
        key: current.workflow_key,
        open_draft_version: 1,
        versions: [
          { id: "draft", version: 1, status: "draft" },
          { id: "published", version: 2, status: "published" },
        ],
      });
    return json(
      {
        error: {
          code: "not_found",
          message: `Unexpected contract route ${method} ${path}`,
        },
      },
      404,
    );
  });
  return writes;
}
async function openNode(page: Page, name: string) {
  await page.getByRole("button", { name: "Find a node" }).click();
  await page.getByLabel("Find graph node").fill(name);
  await page.getByRole("button", { name: new RegExp(`^${name}`) }).click();
}

test("unavailable/invalid identity never falls back to editable fixture data", async ({
  page,
}) => {
  await contract(page, { sessionStatus: 401 });
  await page.goto("/elenio.html#/connected");
  await expect(
    page.getByRole("heading", { name: "Workspace sign-in required" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "New workflow" })).toHaveCount(
    0,
  );
  await expect(page.locator(".react-flow__node")).toHaveCount(0);
});
test("connected create blank uses native registry and keeps a stable retry idempotency key", async ({
  page,
}) => {
  const writes = await contract(page, { createRetry: true });
  await page.goto("/elenio.html#/connected");
  await page.getByRole("button", { name: "New workflow" }).click();
  await page.getByLabel("Workflow name").fill("Manual assurance");
  await page.getByLabel("Workflow key").fill("manual-assurance");
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Temporary create failure",
  );
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Manual assurance" }),
  ).toBeVisible();
  await expect(page.locator(".react-flow__node")).toHaveCount(2);
  const create = writes.filter((w) => w.path === "/api/v1/workflows");
  expect(create).toHaveLength(2);
  expect(create[0]?.headers["idempotency-key"]).toBe(
    create[1]?.headers["idempotency-key"],
  );
  expect(create[0]?.body).toEqual({
    key: "manual-assurance",
    name: "Manual assurance",
  });
  expect(
    Object.keys(create[0]?.headers ?? {}).some((k) => /tenant|role/.test(k)),
  ).toBe(false);
});
test("server save uses version token; validation displays actual server diagnostics and native import requires a diff", async ({
  page,
}) => {
  const writes = await contract(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/elenio.html#/connected/projects/vendor-payment/1");
  await openNode(page, "Document intelligence");
  await page
    .getByLabel("Node name", { exact: true })
    .fill("Evidence extraction");
  await page.getByLabel("Agent version reference").fill("approved.extract@2");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(
    page.getByText("Draft saved by Flowcase. No version was published."),
  ).toBeVisible();
  const patch = writes.find((w) => w.method === "PATCH");
  expect(patch?.headers["if-match"]).toBe('"3"');
  expect(
    (
      patch?.body as {
        definition: { nodes: { id: string; config?: { agent_ref: string } }[] };
      }
    ).definition.nodes.find((n) => n.id === "extract")?.config?.agent_ref,
  ).toBe("approved.extract@2");
  await page.getByRole("button", { name: "Validate with server" }).click();
  await expect(page.locator(".server-diagnostics")).toContainText(
    "Bind an approved agent version",
  );
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/phase2-connected-editor-1440.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "Import Flowcase JSON" }).click();
  const imported = clone(vendorPayment);
  imported.name = "Reviewed import";
  await page.getByLabel("Definition JSON").fill(JSON.stringify(imported));
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(
    page.getByRole("heading", { name: "Review definition changes" }),
  ).toBeVisible();
  expect(writes.filter((w) => w.method === "PATCH")).toHaveLength(1);
  await page.getByRole("button", { name: "Confirm replacement" }).click();
  await expect(
    page.getByRole("heading", { name: "Reviewed import" }),
  ).toBeVisible();
  expect(
    writes.filter((w) => /publish|runs|approvals/.test(w.path)),
  ).toHaveLength(0);
});
test("412 conflict preserves edits; navigation requires an explicit discard and reload never auto-overwrites", async ({
  page,
}) => {
  await contract(page, { conflict: true });
  await page.goto("/elenio.html#/connected/projects/vendor-payment/1");
  await openNode(page, "Document intelligence");
  await page
    .getByLabel("Node name", { exact: true })
    .fill("Unsaved evidence work");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "This draft changed elsewhere" }),
  ).toBeVisible();
  await expect(page.getByLabel("Node name", { exact: true })).toHaveValue(
    "Unsaved evidence work",
  );
  await expect(
    page.getByRole("button", { name: "Add node", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("link", { name: "Workflow projects", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Leave unsaved changes?" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Stay in editor" }).click();
  await expect(page.getByLabel("Node name", { exact: true })).toHaveValue(
    "Unsaved evidence work",
  );
  await page.getByRole("button", { name: "Review reload" }).click();
  await page.getByRole("button", { name: "Keep my edits" }).click();
  await expect(page.getByLabel("Node name", { exact: true })).toHaveValue(
    "Unsaved evidence work",
  );
});
test("runtime capabilities and server roles gate edits; published version stays immutable", async ({
  page,
}) => {
  await contract(page, { capsStatus: 503 });
  await page.goto("/elenio.html#/connected/projects/vendor-payment/1");
  await expect(
    page.getByRole("button", { name: "Add node", exact: true }),
  ).toBeDisabled();
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: "Runtime capabilities unavailable" }),
  ).toBeVisible();
});
test("a server viewer cannot create or edit, including when a draft route is opened directly", async ({
  page,
}) => {
  await contract(page, { roles: ["workspace_member"] });
  await page.goto("/elenio.html#/connected");
  await expect(
    page.getByRole("button", { name: "New workflow" }),
  ).toBeDisabled();
  await page.goto("/elenio.html#/connected/projects/vendor-payment/1");
  await expect(
    page.getByRole("button", { name: "Add node", exact: true }),
  ).toBeDisabled();
  await openNode(page, "Document intelligence");
  await expect(page.getByLabel("Node name", { exact: true })).toHaveAttribute(
    "readonly",
    "",
  );
});
test("published graph is immutable and template creation names only a registered source", async ({
  page,
}) => {
  const writes = await contract(page);
  await page.goto("/elenio.html#/connected/projects/vendor-payment/2");
  await expect(
    page.getByRole("button", { name: "Add node", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Open existing draft" }),
  ).toBeVisible();
  await page.goto("/elenio.html#/connected");
  await page.getByRole("button", { name: "New workflow" }).click();
  await page
    .getByRole("button", { name: "Server template", exact: true })
    .click();
  await page.getByLabel("Workflow name").fill("Tender engineering");
  await page.getByLabel("Workflow key").fill("tender-engineering");
  await page.getByLabel("Registered template").selectOption("sim-tender");
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Tender engineering" }),
  ).toBeVisible();
  expect(writes.find((w) => w.path === "/api/v1/workflows")?.body).toEqual({
    key: "tender-engineering",
    name: "Tender engineering",
    template: "sim-tender",
  });
});
test("registered dependency catalog is real response data and standalone CP launch is unavailable", async ({
  page,
}) => {
  await contract(page);
  await page.goto("/elenio.html#/connected/catalog");
  await expect(page.getByRole("table")).toContainText("approved.extract@2");
  await expect(page.getByRole("table")).toContainText("2.0.0");
  await page.getByRole("button", { name: "Integrations", exact: true }).click();
  await expect(page.getByRole("table")).toContainText("match, lookup");
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: "Launch is unavailable in standalone mode" }),
  ).toBeVisible();
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/phase2-connected-catalog.png",
    fullPage: true,
  });
});

test("malformed session payload locks the service route and makes no write requests", async ({
  page,
}) => {
  const writes = await contract(page, { malformedSession: true });
  await page.goto("/elenio.html#/connected/projects/vendor-payment/1");
  await expect(page.getByRole("alert")).toContainText(
    "invalid session contract",
  );
  await expect(
    page.getByRole("button", { name: "Save draft", exact: true }),
  ).toHaveCount(0);
  expect(writes).toHaveLength(0);
});

test("native create import previews first and retains canonical branch configuration", async ({
  page,
}) => {
  const writes = await contract(page);
  await page.goto("/elenio.html#/connected?create=import");
  await expect(
    page.getByRole("dialog", { name: "Create a workflow" }),
  ).toBeVisible();
  await page.getByLabel("Workflow name").fill("Imported payments");
  await page.getByLabel("Workflow key").fill("imported-payments");
  await page
    .getByLabel("New workflow JSON")
    .fill(JSON.stringify(vendorPayment));
  await expect(
    page.getByRole("button", { name: "Create draft", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Preview import", exact: true })
    .click();
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(11);
  const input = writes.find((w) => w.path === "/api/v1/workflows")?.body as {
    definition: typeof vendorPayment;
  };
  expect(
    input.definition.nodes.find((n) => n.id === "match")?.config?.branches,
  ).toEqual({ true: "review", false: "exception" });
  expect(input.definition.input_schema).toEqual(vendorPayment.input_schema);
  expect(input.definition.policy_refs).toEqual(vendorPayment.policy_refs);
});

test("connected pages remain responsive on desktop, tablet and mobile", async ({
  page,
}) => {
  await contract(page);
  for (const size of [
    { width: 1440, height: 900 },
    { width: 768, height: 1024 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(size);
    await page.goto("/elenio.html#/connected");
    await expect(
      page.getByRole("heading", { name: "Workflow projects" }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 1,
      ),
    ).toBe(false);
    await page.screenshot({
      path: `../../docs/elenio-handoff/evidence/phase2-registry-${size.width}.png`,
      fullPage: true,
    });
    await page.goto("/elenio.html#/connected/projects/vendor-payment/1");
    await expect(page.locator(".react-flow__node")).toHaveCount(11);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 1,
      ),
    ).toBe(false);
    if (size.width === 390)
      await expect(page.locator(".mobile-node-list")).toBeVisible();
    await page.screenshot({
      path: `../../docs/elenio-handoff/evidence/phase2-editor-${size.width}.png`,
      fullPage: true,
    });
  }
});

test("visual positions survive reload in a scoped sidecar that contains no service definition", async ({
  page,
}) => {
  await contract(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/elenio.html#/connected/projects/vendor-payment/1");
  await openNode(page, "Collect evidence");
  await page.getByRole("button", { name: "Close inspector" }).click();
  const node = page.locator('.react-flow__node[data-id="join"]');
  const box = await node.boundingBox();
  if (!box) throw new Error("Join node not visible");
  await page.mouse.move(box.x + 75, box.y + 45);
  await page.mouse.down();
  await page.mouse.move(box.x + 130, box.y + 90, { steps: 10 });
  await page.mouse.up();
  const moved = await node.getAttribute("style");
  async function read() {
    return page.evaluate(
      () =>
        new Promise<unknown>((resolve) => {
          const req = indexedDB.open("elenio-visual-layout-v1", 1);
          req.onsuccess = () => {
            const db = req.result;
            const tx = db.transaction("layouts");
            const get = tx.objectStore("layouts").getAll();
            get.onsuccess = () => resolve(get.result);
            tx.oncomplete = () => db.close();
          };
          req.onerror = () => resolve(null);
        }),
    );
  }
  await expect.poll(async () => JSON.stringify(await read())).toContain("join");
  const stored = JSON.stringify(await read());
  expect(stored).not.toContain("definition");
  expect(stored).not.toContain("agent_ref");
  expect(stored).not.toContain("roles");
  await page.reload();
  await expect(node).toHaveAttribute("style", moved ?? "");
});

test("browser Back also protects an unsaved connected draft", async ({
  page,
}) => {
  const writes = await contract(page);
  await page.goto("/elenio.html#/connected");
  await page.getByRole("button", { name: "Open draft v1" }).click();
  await openNode(page, "Document intelligence");
  await page
    .getByLabel("Node name", { exact: true })
    .fill("Keep this unsaved edit");
  await page.goBack();
  await expect(
    page.getByRole("dialog", { name: "Leave unsaved changes?" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Stay in editor" }).click();
  await expect(page.getByLabel("Node name", { exact: true })).toHaveValue(
    "Keep this unsaved edit",
  );
  await page
    .getByRole("link", { name: "Workflow projects", exact: true })
    .click();
  await page.getByRole("button", { name: "Discard edits and leave" }).click();
  await expect(
    page.getByRole("heading", { name: "Workflow projects" }),
  ).toBeVisible();
  expect(writes).toHaveLength(0);
});

test("service pages pass serious/critical accessibility rules and focus expands the canvas", async ({
  page,
}) => {
  await contract(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  for (const route of [
    "connected",
    "connected/catalog",
    "connected/projects/vendor-payment/1",
  ]) {
    await page.goto(`/elenio.html#/${route}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    if (route.includes("/projects/"))
      await expect(page.locator(".react-flow__node")).toHaveCount(11);
    const result = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
      .analyze();
    expect(
      result.violations.filter((v) =>
        ["serious", "critical"].includes(v.impact ?? ""),
      ),
      route,
    ).toEqual([]);
  }
  const initial = await page.locator(".workflow-canvas").boundingBox();
  await page.getByRole("button", { name: "Enter focus mode" }).click();
  await expect(page.locator(".platform-sidebar")).toBeHidden();
  const focused = await page.locator(".workflow-canvas").boundingBox();
  expect(focused?.width).toBeGreaterThan(initial?.width ?? 0);
});

test("Templates can open creation from an already loaded registry", async ({
  page,
}) => {
  await contract(page);
  await page.goto("/elenio.html#/connected");
  await expect(
    page.getByRole("heading", { name: "Workflow projects" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Templates", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByLabel("Registered template")).toBeVisible();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
});
