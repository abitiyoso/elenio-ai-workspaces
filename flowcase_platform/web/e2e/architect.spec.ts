import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
const ARCHITECT_KEY = "elenio.solution-architect",
  ARCHITECT_VERSION = "0.3.0";

const tenant = "00000000-0000-4000-8000-000000000001",
  versionId = "00000000-0000-4000-8000-000000000201",
  deploymentId = "00000000-0000-4000-8000-000000000202",
  runId = "00000000-0000-4000-8000-000000000203";
async function fixture(
  page: Page,
  options: {
    mismatch?: boolean;
    mockModel?: boolean;
    retry?: boolean;
    clarification?: boolean;
  } = {},
) {
  const state = {
    runs: 0,
    creations: [] as Record<string, unknown>[],
    idempotency: [] as string[],
    cancelled: false,
  };
  const flowSession = {
    user_id: "00000000-0000-4000-8000-000000000101",
    tenant_id: tenant,
    display_name: "Contract builder",
    roles: ["workspace_member", "workflow_designer"],
    environment: "development",
    auth_mode: "dev_mock",
    expires_at: null,
  };
  const definition = {
    schema_version: "1.0",
    key: "request-review",
    name: "Request review",
    version: 1,
    status: "draft",
    input_schema: { type: "object" },
    nodes: [
      {
        id: "start",
        name: "Request received",
        type: "start",
        config: {},
        next: ["review"],
      },
      {
        id: "review",
        name: "Human review",
        type: "human_task",
        config: { assignee_role: "poc-owner" },
        next: ["end"],
      },
      { id: "end", name: "Recorded", type: "end", config: {}, next: [] },
    ],
  };
  const proposal = {
    stage: options.clarification ? "clarification" : "proposal",
    title: "Review the request",
    summary: "A human-operated draft; no external connector is executed.",
    questions: options.clarification ? ["Who owns the final decision?"] : [],
    assumptions: ["Human review remains mandatory."],
    agents: [],
    dependencies: [
      {
        name: "ERP posting",
        kind: "connector",
        status: "required",
        reason: "No approved connector is available.",
      },
    ],
    workflow: options.clarification ? null : definition,
  };
  const run = (status = "succeeded") => ({
    id: runId,
    agent_key: ARCHITECT_KEY,
    agent_version_id: versionId,
    agent_semver: ARCHITECT_VERSION,
    kind: "sandbox",
    model_mode: options.mockModel ? "dev_mock" : "provider",
    status: state.cancelled ? "cancelled" : status,
    input: { requirement: "Review employee requests with a human owner." },
    output: proposal,
    validation: { schema_valid: true },
    tool_calls: 0,
    tokens_in: 200,
    tokens_out: 100,
    created_at: "2026-11-19T00:00:00Z",
    pinned: { model: { target: { provider_model_id: "contract-model" } } },
  });
  await page.route("**/agent-api/v1/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname.replace("/agent-api/v1", "");
    let body: unknown;
    if (path === "/session")
      body = {
        subject: "local@elenio.test",
        tenant_id: options.mismatch ? "other-tenant" : tenant,
        identity_mode: "development",
        permissions: [
          "sandbox.run",
          "run.read",
          "run.cancel",
          "agent.version.read",
        ],
      };
    else if (path.includes("/versions"))
      body = {
        items: [
          {
            id: versionId,
            semver: ARCHITECT_VERSION,
            output_schema: { properties: { workflow: {} } },
          },
        ],
      };
    else if (path.endsWith("/model-binding"))
      body = { model_deployment_id: deploymentId, environment: "development" };
    else if (path.endsWith("/tools") || path.endsWith("/skills")) body = [];
    else if (path === "/model-deployments")
      body = {
        items: [
          {
            id: deploymentId,
            provider_model_id: "contract-model",
            status: "active",
          },
        ],
      };
    else if (path.includes("/approvals"))
      body = [{ state: "approved", capability_check_ref: "probe-1" }];
    else if (path === "/sandbox/runs") {
      state.runs++;
      state.idempotency.push(request.postDataJSON().idempotency_key);
      if (options.retry && state.runs === 1) {
        await route.abort("connectionreset");
        return;
      }
      body = run("queued");
    } else if (path.endsWith("/cancel")) {
      state.cancelled = true;
      body = run("cancelled");
    } else if (path === "/runs")
      body = { items: state.runs ? [run()] : [], total: state.runs ? 1 : 0 };
    else if (path === `/runs/${runId}`)
      body = { run: run(options.retry ? "queued" : "succeeded"), evidence: [] };
    else {
      await route.fulfill({ status: 404, json: {} });
      return;
    }
    await route.fulfill({ json: body });
  });
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    let body: unknown;
    if (path.endsWith("/session")) body = flowSession;
    else if (path.endsWith("/workflow-catalog"))
      body = {
        policies: [],
        agents: [],
        connectors: [],
        assignee_roles: ["poc-owner"],
        artifact_types: [],
      };
    else if (path.endsWith("/runtime-capabilities"))
      body = {
        supported_node_types: ["start", "end", "human_task", "condition"],
      };
    else if (path.endsWith("/workflow-validations"))
      body = { valid: true, diagnostics: [] };
    else if (path.endsWith("/workflows") && request.method() === "POST") {
      const input = request.postDataJSON();
      state.creations.push(input);
      body = {
        version: {
          id: versionId,
          workflow_key: input.key,
          version: 1,
          version_num: 1,
          status: "draft",
          schema_version: "1.0",
          definition: input.definition,
        },
        validation: { valid: true, diagnostics: [] },
      };
    } else {
      await route.fulfill({ status: 404, json: {} });
      return;
    }
    await route.fulfill({ json: body });
  });
  await page.goto("/elenio.html#/architect");
  await page
    .getByLabel("Business requirement")
    .fill("Review employee requests with a human owner.");
  return state;
}

test("model proposal waits for explicit generation, canonical review, fresh validation and create confirmation", async ({
  page,
}) => {
  const state = await fixture(page);
  await expect(
    page.getByRole("button", { name: "Generate blueprint", exact: true }),
  ).toBeEnabled();
  expect(state.runs).toBe(0);
  expect(state.creations).toHaveLength(0);
  await page
    .getByRole("button", { name: "Send requirement", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Preview changes", exact: true })
    .click();
  const confirm = page.getByRole("button", {
    name: "Confirm create draft",
    exact: true,
  });
  await expect(confirm).toBeDisabled();
  await page
    .getByRole("button", { name: "Validate proposal", exact: true })
    .click();
  await expect(confirm).toBeEnabled();
  const json = page.getByLabel("Proposed workflow JSON");
  const document = JSON.parse(await json.inputValue());
  document.name = "Refined request review";
  await json.fill(JSON.stringify(document));
  await expect(confirm).toBeDisabled();
  await expect(page.locator(".proposal-diff")).toContainText(
    /Workflow fields changed:.*name/,
  );
  await page
    .getByRole("button", { name: "Validate proposal", exact: true })
    .click();
  await expect(confirm).toBeEnabled();
  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    axe.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
  await confirm.click();
  await expect.poll(() => state.creations.length).toBe(1);
  expect(state.creations[0]?.definition).toMatchObject({
    name: "Refined request review",
    status: "draft",
    nodes: document.nodes,
  });
});

test("reviewed Architect draft returns to the same saved Solution with a pinned workflow",async({page})=>{
  const state=await fixture(page);
  const solutionID="00000000-0000-4000-8000-000000000601",workspaceID="00000000-0000-4000-8000-000000000602";
  let plan={id:solutionID,tenant_id:tenant,workspace_id:workspaceID,revision:1,
    definition:{key:"review-solution",name:"Review Solution",description:"Review employee requests with a human owner.",delivery_mode:"solution",output:"both",workflows:[] as {key:string;version:number}[]},
    created_at:"2026-11-20T00:00:00Z",updated_at:"2026-11-20T00:00:00Z"};
  const mutations:{revision:string|undefined;body:unknown}[]=[];
  await page.route("**/control-api/v1/**",async route=>{
    const request=route.request(),path=new URL(request.url()).pathname;
    if(path.endsWith("/me"))return route.fulfill({json:{user:{id:"00000000-0000-4000-8000-000000000101",display_name:"Contract builder"},tenant:{id:tenant,name:"Contract tenant"},workspace:{id:workspaceID,name:"Contract workspace"},permissions:["solution:view","solution:author"],csrf_token:"fixture-csrf",environment:"test"}});
    if(request.method()==="PATCH"){
      mutations.push({revision:request.headers()["if-match"],body:request.postDataJSON()});
      plan={...plan,revision:plan.revision+1,definition:request.postDataJSON()};
    }
    return route.fulfill({json:plan});
  });
  await page.goto(`/elenio.html#/solutions/${solutionID}/blueprint`);
  await page.getByRole("button",{name:"Send requirement",exact:true}).click();
  await page.getByRole("button",{name:"Preview changes",exact:true}).click();
  await page.getByRole("button",{name:"Validate proposal",exact:true}).click();
  await page.getByRole("button",{name:"Confirm create draft",exact:true}).click();
  await expect(page).toHaveURL(new RegExp(`/solutions/${solutionID}/build$`));
  expect(state.creations).toHaveLength(1);
  expect(state.creations[0]?.definition).toMatchObject({metadata:{architect:{solution_id:solutionID,solution_revision:1}}});
  expect(mutations).toHaveLength(1);
  expect(mutations[0]?.revision).toBe('"1"');
  expect(plan.definition.workflows).toEqual([{key:"request-review-00000000",version:1}]);
  await page.reload();
  await expect(page.getByRole("textbox",{name:"Workflow versions",exact:true})).toHaveValue("request-review-00000000@1");
});

test("tenant disagreement cannot generate or apply a proposal", async ({
  page,
}) => {
  const state = await fixture(page, { mismatch: true });
  await expect(
    page.getByText(/must use the same verified tenant/),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Generate blueprint", exact: true }),
  ).toBeDisabled();
  expect(state.runs).toBe(0);
  expect(state.creations).toHaveLength(0);
});

test("a mock-model result is rejected and never becomes an applicable AI proposal", async ({
  page,
}) => {
  const state = await fixture(page, { mockModel: true });
  await page
    .getByRole("button", { name: "Generate blueprint", exact: true })
    .click();
  await expect(
    page.getByText(/Only a schema-valid, tool-free real provider/),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Preview changes", exact: true }),
  ).toHaveCount(0);
  expect(state.creations).toHaveLength(0);
});

test("clarification presents the actual model question without a workflow to apply", async ({
  page,
}) => {
  const state = await fixture(page, { clarification: true });
  await page
    .getByRole("button", { name: "Generate blueprint", exact: true })
    .click();
  await expect(
    page.getByText("Who owns the final decision?", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Preview changes", exact: true }),
  ).toHaveCount(0);
  expect(state.creations).toHaveLength(0);
});

test("lost generation response reuses the idempotency key and cancellation uses the run authority", async ({
  page,
}) => {
  const state = await fixture(page, { retry: true });
  await page
    .getByRole("button", { name: "Generate blueprint", exact: true })
    .click();
  await expect(page.getByRole("alert")).toBeVisible();
  await page
    .getByRole("button", { name: "Generate blueprint", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Cancel generation", exact: true })
    .click();
  await expect(
    page.getByRole("status").filter({ hasText: "cancelled" }),
  ).toBeVisible();
  expect(state.idempotency).toHaveLength(2);
  expect(state.idempotency[0]).toBe(state.idempotency[1]);
  expect(state.creations).toHaveLength(0);
});
