import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("real provider: requirement → durable proposal → review → validate → native server draft", async ({
  page,
  request,
}) => {
  expect(process.env.ELENIO_LOCAL_E2E).toBe("1");
  expect(process.env.ELENIO_AI_E2E).toBe("1");
  expect(await (await request.get("/api/v1/session")).json()).toMatchObject({
    auth_mode: "dev_mock",
    environment: "development",
    display_name: "Local E2E developer",
  });
  expect(
    await (await request.get("/agent-api/v1/session")).json(),
  ).toMatchObject({
    identity_mode: "development",
    self_approval_exempt: false,
  });
  const writes: string[] = [];
  page.on("request", (r) => {
    if (r.method() !== "GET") writes.push(r.url());
  });
  await page.goto("/elenio.html#/architect");
  await page
    .getByLabel("Business requirement")
    .fill(
      "Design a human-operated purchase request review draft. Trigger: an employee submits a purchase request with purpose, amount and a boolean needs_exception. The POC owner records a scope check. Include an explicit condition: if needs_exception is true, add an exception review by the POC owner; if false, bypass that step. Both paths join at independent approval using an existing registered approval policy from the catalog, then end. Use catalog roles/policies only and 5 to 8 nodes. No external posting or automated payment. AI extraction and ERP automation are missing dependencies, not executable agents or connectors. Success: a recorded human decision, with no invented rates or savings.",
    );
  await expect(
    page.getByRole("button", { name: "Generate blueprint", exact: true }),
  ).toBeEnabled({ timeout: 15000 });
  await page
    .getByRole("button", { name: "Generate blueprint", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Preview changes", exact: true }),
  ).toBeVisible({ timeout: 150000 });
  expect(writes.filter((url) => url.endsWith("/workflows"))).toHaveLength(0);
  await expect(
    page.getByRole("log", { name: "Requirement history" }),
  ).toContainText("Saved in the sandbox run");
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/architect-real-proposal-1440.png",
    fullPage: true,
  });
  await page.reload();
  const history = page.getByRole("region", {
    name: "Workspace proposal history",
  });
  await expect(history).toBeVisible({ timeout: 15000 });
  await history.getByRole("button").first().click();
  await expect(
    page.getByRole("button", { name: "Preview changes", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Preview changes", exact: true })
    .click();
  const json = page.getByLabel("Proposed workflow JSON");
  const definition = JSON.parse(await json.inputValue());
  expect(
    definition.nodes.some((n: { type: string }) => n.type === "condition"),
  ).toBe(true);
  expect(definition.version).toBe(1);
  expect(definition.status).toBe("draft");
  await expect(
    page.getByRole("button", { name: "Confirm create draft", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Validate proposal", exact: true })
    .click();
  await expect(
    page.getByText("Server validation passed.", { exact: true }),
  ).toBeVisible({ timeout: 10000 });
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/architect-real-validation-1440.png",
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Confirm create draft", exact: true })
    .click();
  await expect(page).toHaveURL(
    new RegExp(`/connected/projects/${definition.key}/1$`),
  );
  const saved = await (
    await request.get(`/api/v1/workflows/${definition.key}/versions/1`)
  ).json();
  expect(saved.status).toBe("draft");
  // Go's canonical serialization omits empty config maps; execution fields stay exact.
  const normalize = (nodes: { config?: Record<string, unknown> }[]) =>
    nodes.map((node) => ({ ...node, config: node.config ?? {} }));
  expect(normalize(saved.definition.nodes)).toEqual(
    normalize(definition.nodes),
  );
  expect(saved.definition.metadata.architect.model_mode).toBe("provider");
  await expect(page.locator(".react-flow__node")).toHaveCount(
    definition.nodes.length,
  );
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/architect-real-server-draft-1440.png",
    fullPage: true,
  });
  expect(
    writes.some(
      (url) =>
        url.includes("/publish") ||
        url.includes("/releases") ||
        url.includes("/cases"),
    ),
  ).toBe(false);
});

test("stored real-model proposal stays readable and accessible on desktop, tablet and mobile without another model call", async ({ page, request }) => {
  expect(process.env.ELENIO_LOCAL_E2E).toBe("1");
  expect(process.env.ELENIO_AI_E2E).toBe("1");
  expect(await (await request.get("/agent-api/v1/session")).json()).toMatchObject({ identity_mode: "development", self_approval_exempt: false });
  const externalWrites: string[] = [];
  page.on("request", r => { if (r.method() !== "GET" && r.url().includes("/agent-api/")) externalWrites.push(r.url()); });
  await page.goto("/elenio.html#/architect");
  const history = page.getByRole("region", { name: "Workspace proposal history" });
  await expect(history).toBeVisible();
  await history.getByRole("button").filter({ hasText: "Design a human-operated purchase request review draft." }).first().click();
  await expect(page.getByRole("button", { name: "Preview changes", exact: true })).toBeVisible();
  for (const width of [1440, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: width < 768 ? 844 : 900 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
    await page.screenshot({ path: `../../docs/elenio-handoff/evidence/architect-stored-proposal-${width}.png`, fullPage: true });
    if (width === 1440 || width === 390) {
      const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"]).analyze();
      expect(result.violations.filter(v => ["serious", "critical"].includes(v.impact ?? ""))).toEqual([]);
    }
  }
  expect(externalWrites).toEqual([]);
});
