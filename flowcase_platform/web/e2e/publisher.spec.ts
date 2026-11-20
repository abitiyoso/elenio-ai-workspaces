import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import type {
  Solution,
  SolutionCandidate,
  SolutionReadiness,
} from "../src/platform/solutionApi";
const tenant = "00000000-0000-4000-8000-000000000001",
  workspace = "00000000-0000-4000-8000-000000000002",
  id = "00000000-0000-4000-8000-000000000201";
async function fixture(
  page: Page,
  options: { lostResponse?: boolean; viewer?: boolean; seed?: boolean } = {},
) {
  let plan: Solution | undefined = options.seed
    ? {
        id,
        tenant_id: tenant,
        workspace_id: workspace,
        revision: 1,
        definition: {
          key: "finance-app",
          name: "Finance application",
          description: "Synthetic composition",
          delivery_mode: "solution",
          output: "both",
          workflows: [{ key: "purchase-review", version: 1 }],
          application: {
            title: "Finance workspace",
            pages: [{ key: "overview", title: "Overview", kind: "overview" }],
          },
        },
        created_at: "2026-11-19T00:00:00Z",
        updated_at: "2026-11-19T00:00:00Z",
      }
    : undefined;
  const candidates: SolutionCandidate[] = [],
    posts: { url: string; key: string; body: unknown; revision: string }[] = [];
  const readiness = (): SolutionReadiness => ({
    solution_id: id,
    revision: plan?.revision ?? 1,
    checked_at: "2026-11-19T00:00:00Z",
    release_ready: false,
    blockers: ["Solution approval and target installation are unverified."],
    application_issues: [
      "Runtime page/schema mapping and business permissions require verification.",
    ],
    workflows: [
      {
        key: "purchase-review",
        version: 1,
        state: "verified",
        definition_hash: "b".repeat(64),
        reason: "Published version observed.",
        inventory_complete: true,
        resources: [
          {
            kind: "approval_policy",
            ref: "finance.approval",
            state: "blocked",
            reason: "Business ratification not resolved.",
          },
        ],
      },
    ],
  });
  await page.route("**/control-api/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url());
    if (url.pathname.endsWith("/me"))
      return route.fulfill({
        json: {
          user: {
            id: "00000000-0000-4000-8000-000000000101",
            display_name: "Contract publisher",
          },
          tenant: { id: tenant, name: "Contract tenant" },
          workspace: { id: workspace, name: "Contract test workspace" },
          permissions: options.viewer
            ? ["solution:view"]
            : ["solution:view", "solution:author"],
          csrf_token: "fixture-csrf",
          environment: "development",
        },
      });
    if (url.pathname.endsWith("/readiness"))
      return route.fulfill({ json: readiness() });
    if (url.pathname.endsWith("/executions"))
      return route.fulfill({json:{items:[],scope:"current_user_solution_executions"}});
    if (url.pathname.includes("/candidates")) {
      if (req.method() === "GET")
        return route.fulfill({ json: { items: candidates } });
      posts.push({
        url: url.pathname,
        key: req.headers()["idempotency-key"] ?? "",
        body: req.postDataJSON(),
        revision: req.headers()["if-match"] ?? "",
      });
      if (!plan) throw new Error("Missing fixture plan");
      const prior = candidates[0];
      if (prior) return route.fulfill({ status: 200, json: prior });
      const snapshot = {
        schema_version: "elenio.solution-candidate.v1" as const,
        plan: structuredClone(plan),
        readiness: readiness(),
        limitations: ["Candidate only; not an installation package."],
      };
      const item: SolutionCandidate = {
        id: "00000000-0000-4000-8000-000000000301",
        solution_id: id,
        version: "1.0.0",
        plan_revision: 1,
        status: "candidate",
        content_hash: createHash("sha256")
          .update(JSON.stringify(snapshot))
          .digest("hex"),
        created_at: "2026-11-19T00:00:00Z",
        snapshot,
      };
      candidates.push(item);
      if (options.lostResponse) return route.abort("failed");
      return route.fulfill({ status: 201, json: item });
    }
    if (req.method() === "GET")
      return route.fulfill({ json: { items: plan ? [plan] : [] } });
    posts.push({
      url: url.pathname,
      key: req.headers()["idempotency-key"] ?? "",
      body: req.postDataJSON(),
      revision: req.headers()["if-match"] ?? "",
    });
    plan = {
      id,
      tenant_id: tenant,
      workspace_id: workspace,
      revision: 1,
      definition: req.postDataJSON(),
      created_at: "2026-11-19T00:00:00Z",
      updated_at: "2026-11-19T00:00:00Z",
    };
    return route.fulfill({ status: 201, json: plan });
  });
  return { posts, candidates };
}
test("compose business pages, preview responsive views, capture an immutable candidate and export its blockers", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.route("**/api/v1/workflows/purchase-review/versions/1",route=>route.fulfill({status:403,json:{error:{code:"forbidden",message:"Contract fixture denies workflow access"}}}));
  await page.goto("/elenio.html#/solutions");
  await page.getByRole("button", { name: "New solution" }).click();
  await page.getByLabel("Solution name").fill("Finance application");
  await page.getByLabel("Solution key").fill("finance-app");
  await page.getByLabel("Workflow versions").fill("purchase-review@1");
  await page
    .getByRole("button", { name: "Configure application pages" })
    .click();
  await page.getByLabel("Application title").fill("Finance workspace");
  for (const [index, title, kind] of [
    [2, "New request", "intake"],
    [3, "Reviews", "tasks"],
    [4, "Results", "report"],
  ] as const) {
    await page.getByRole("button", { name: "Add application page" }).click();
    await page.getByLabel(`Page ${index} title`).fill(title);
    await page.getByLabel(`Page ${index} component`).selectOption(kind);
    await page
      .getByLabel(`Page ${index} workflow`)
      .selectOption("purchase-review");
  }
  await page
    .getByRole("button", { name: "Save solution plan", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Preview application", exact: true })
    .click();
  await page.getByRole("button", { name: "New request", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Start case",exact:true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Reviews", exact: true }).click();
  await expect(
    page.getByText("No executions for this page in the loaded history.", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Results", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Results", exact: true }),
  ).toHaveCSS("background-color", "rgb(30, 82, 199)");
  await page.mouse.move(0, 0);
  await expect(
    page.getByRole("button", { name: "Results", exact: true }),
  ).toHaveCSS("background-color", "rgb(40, 100, 236)");
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: `../../docs/elenio-handoff/evidence/publisher-application-${width}.png`,
      fullPage: false,
    });
  }
  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    axe.violations.filter(
      (v) => v.impact === "serious" || v.impact === "critical",
    ),
  ).toEqual([]);
  await page.getByRole("button", { name: "Close dialog" }).click();
  await page
    .getByRole("button", { name: "Publisher candidates", exact: true })
    .click();
  await page.getByRole("button", { name: "Create review candidate" }).click();
  await expect(
    page.getByText("Candidate 1.0.0", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Not approved for deployment.", { exact: false }),
  ).toBeVisible();
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    page.getByRole("button", { name: "Download candidate JSON" }).click(),
  ]);
  const path = await download.path();
  if (!path) throw new Error("Missing candidate download");
  const output = JSON.parse(await readFile(path, "utf8"));
  expect(output.status).toBe("candidate");
  expect(output.snapshot.plan.definition.application.pages).toHaveLength(4);
  expect(output.snapshot.readiness.release_ready).toBe(false);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/publisher-candidate-1440.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "Close dialog" }).click();
  await page.reload();
  await page
    .getByRole("button", { name: "Publisher candidates", exact: true })
    .click();
  await expect(
    page.getByText("Candidate 1.0.0", { exact: true }),
  ).toBeVisible();
  expect(state.posts).toHaveLength(2);
  expect(state.posts[1]?.body).toEqual({ version: "1.0.0" });
  expect(state.posts[1]?.revision).toBe('"1"');
});
test("candidate retry after a lost response keeps the same key and does not create another snapshot", async ({
  page,
}) => {
  const state = await fixture(page, { seed: true, lostResponse: true });
  await page.goto("/elenio.html#/solutions");
  await page
    .getByRole("button", { name: "Publisher candidates", exact: true })
    .click();
  await page.getByRole("button", { name: "Create review candidate" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await page.getByRole("button", { name: "Create review candidate" }).click();
  await expect(
    page.getByText("Candidate 1.0.0", { exact: true }),
  ).toBeVisible();
  expect(state.posts).toHaveLength(2);
  expect(state.posts[0]?.key).toBe(state.posts[1]?.key);
  expect(state.candidates).toHaveLength(1);
});
test("business-only viewer can preview but cannot capture a Publisher candidate", async ({
  page,
}) => {
  const state = await fixture(page, { seed: true, viewer: true });
  await page.goto("/elenio.html#/solutions");
  await page
    .getByRole("button", { name: "Preview application", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Finance workspace" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Close dialog" }).click();
  await page
    .getByRole("button", { name: "Publisher candidates", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Create review candidate" }),
  ).toBeDisabled();
  expect(state.posts).toHaveLength(0);
});
