import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
const tenant = "00000000-0000-4000-8000-000000000001",
  workspace = "00000000-0000-4000-8000-000000000002",
  id = "00000000-0000-4000-8000-000000000201";
async function fixture(
  page: Page,
  options: { viewer?: boolean; stale?: boolean; unavailable?: boolean } = {},
) {
  let items: Record<string, unknown>[] = [];
  const writes: {
    headers: Record<string, string>;
    body: Record<string, unknown>;
    method: string;
  }[] = [];
  await page.route("**/control-api/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url());
    if (options.unavailable)
      return route.fulfill({ status: 503, json: { code: "NOT_CONFIGURED" } });
    if (url.pathname.endsWith("/me"))
      return route.fulfill({
        json: {
          user: {
            id: "00000000-0000-4000-8000-000000000101",
            display_name: "Contract builder",
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
    if (url.pathname.endsWith("/readiness")) {
      const item = items[0] as {
        id: string;
        revision: number;
        definition: { workflows: { key: string; version: number }[] };
      };
      return route.fulfill({
        json: {
          solution_id: item.id,
          revision: item.revision,
          checked_at: "2026-11-19T00:00:00Z",
          release_ready: false,
          blockers: [
            "Application composition and target installation remain unverified.",
          ],
          workflows: item.definition.workflows.map((w, i) => ({
            ...w,
            state: i === 0 ? "verified" : "blocked",
            reason:
              i === 0
                ? "Published workflow version observed."
                : "Pinned version not found.",
            ...(i === 0 ? { definition_hash: "a".repeat(64) } : {}),
          })),
        },
      });
    }
    if (req.method() === "GET") return route.fulfill({ json: { items } });
    const body = req.postDataJSON();
    writes.push({ headers: req.headers(), body, method: req.method() });
    if (options.stale && req.method() === "PATCH")
      return route.fulfill({
        status: 412,
        json: { code: "REVISION_CONFLICT" },
      });
    const item = {
      id,
      tenant_id: tenant,
      workspace_id: workspace,
      revision: req.method() === "POST" ? 1 : 2,
      definition: body,
      created_at: "2026-11-19T00:00:00Z",
      updated_at: "2026-11-19T00:00:00Z",
    };
    items = [item];
    return route.fulfill({
      status: req.method() === "POST" ? 201 : 200,
      json: item,
    });
  });
  return {
    writes,
    seed: () => {
      items = [
        {
          id,
          tenant_id: tenant,
          workspace_id: workspace,
          revision: 1,
          definition: {
            key: "review-app",
            name: "Review application",
            description: "Synthetic plan",
            delivery_mode: "solution",
            output: "both",
            workflows: [{ key: "purchase-review", version: 1 }],
          },
          created_at: "2026-11-19T00:00:00Z",
          updated_at: "2026-11-19T00:00:00Z",
        },
      ];
    },
  };
}
test("solution plan persists multiple workflow pins and supports review/edit without deployment", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.goto("/elenio.html#/solutions");
  await page.getByRole("button", { name: "New solution" }).click();
  await page.getByLabel("Solution name").fill("Purchase review application");
  await page.getByLabel("Solution key").fill("purchase-review-app");
  await page
    .getByLabel("Description", { exact: true })
    .fill("Synthetic customer application plan");
  await page
    .getByLabel("Workflow versions")
    .fill("purchase-review@2\nexception-review@1");
  await page
    .getByRole("button", { name: "Save solution plan", exact: true })
    .click();
  await expect(page.getByText("Plan · revision 1")).toBeVisible();
  expect(state.writes[0].body.workflows).toHaveLength(2);
  expect(state.writes[0].headers["x-csrf-token"]).toBe("fixture-csrf");
  expect(state.writes[0].body).not.toHaveProperty("workspace_id");
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Purchase review application" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Edit solution plan" }).click();
  await expect(page.getByLabel("Solution key")).toBeDisabled();
  await page.getByLabel("Delivery mode").selectOption("enterprise");
  await page.getByLabel("Application output").selectOption("api");
  await page
    .getByRole("button", { name: "Save solution plan", exact: true })
    .click();
  await expect(page.getByText("Plan · revision 2")).toBeVisible();
  await page
    .getByRole("button", { name: "Check workflow dependencies" })
    .click();
  await expect(
    page.getByText("Published version verified", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("region", { name: "Dependency check" }),
  ).toContainText("Solution release is not verified");
  expect(state.writes[1].headers["if-match"]).toBe('"1"');
  expect(state.writes[1].body.delivery_mode).toBe("enterprise");
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: `../../docs/elenio-handoff/evidence/solutions-contract-${width}.png`,
      fullPage: true,
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
  expect(state.writes.map((w) => w.method)).toEqual(["POST", "PATCH"]);
});
test("solution conflict retains edits and unsaved navigation requires a decision", async ({
  page,
}) => {
  const state = await fixture(page, { stale: true });
  state.seed();
  await page.goto("/elenio.html#/economics");
  await page.getByRole("link", { name: "Solutions", exact: true }).click();
  await page.getByRole("button", { name: "Edit solution plan" }).click();
  await page.getByLabel("Solution name").fill("My retained change");
  await page
    .getByRole("button", { name: "Save solution plan", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("edits were retained");
  await expect(page.getByLabel("Solution name")).toHaveValue(
    "My retained change",
  );
  await page.evaluate(() => {
    history.back();
  });
  await expect(
    page.getByRole("heading", { name: "Unsaved solution changes" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Keep editing" }).click();
  await expect(page.getByLabel("Solution name")).toHaveValue(
    "My retained change",
  );
});
test("solution viewer cannot author and unavailable CP never substitutes fixture data", async ({
  page,
}) => {
  const state = await fixture(page, { viewer: true });
  state.seed();
  await page.goto("/elenio.html#/solutions");
  await expect(
    page.getByRole("button", { name: "New solution" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "View solution plan" }).click();
  await expect(page.getByLabel("Solution name")).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Save solution plan", exact: true }),
  ).toBeDisabled();
  expect(state.writes).toHaveLength(0);
  await page.unrouteAll({ behavior: "wait" });
  await fixture(page, { unavailable: true });
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Connect your solution workspace" }),
  ).toBeVisible();
  await expect(
    page.getByText("Review application", { exact: true }),
  ).toHaveCount(0);
});
