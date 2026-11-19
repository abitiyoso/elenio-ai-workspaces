import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { randomUUID } from "node:crypto";
async function start(page: Page, title: string) {
  await page.goto("/elenio.html#/connected/operations");
  await page.getByRole("button", { name: "Start a case", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Published workflow" })
    .selectOption("internal-poc-request");
  await page.getByLabel("Case title", { exact: true }).fill(title);
  await page.getByLabel(/Requester id/).fill("local-ui-requester");
  await page.getByLabel(/Purpose/).fill("Verify local manual operations");
  await page.getByLabel(/Estimated cost/).fill("10");
  await page.getByRole("button", { name: "Start case", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", {
      name: "Scope & success criteria",
      exact: true,
    }),
  ).toBeVisible({ timeout: 12000 });
}
async function complete(page: Page, name: string) {
  const card = page
    .locator("article")
    .filter({ has: page.getByRole("heading", { name, exact: true }) });
  await card
    .getByRole("button", { name: "Complete task", exact: true })
    .click();
  await page
    .getByLabel("Comment", { exact: true })
    .fill("Completed by authorized local operator");
  await page.getByRole("button", { name: "Confirm action" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
}
test.beforeEach(async ({ request }) => {
  expect(process.env.ELENIO_LOCAL_E2E).toBe("1");
  expect(await (await request.get("/api/v1/session")).json()).toMatchObject({
    auth_mode: "dev_mock",
    environment: "development",
    display_name: "Local E2E developer",
  });
});

test("UI completes intake, tasks and distinct reviewer approval with real persisted state", async ({
  page,
  request,
}) => {
  const title = `UI operations ${randomUUID().slice(0, 8)}`;
  await page.setViewportSize({ width: 1440, height: 900 });
  await start(page, title);
  const id = page.url().split("/").at(-1) ?? "";
  await complete(page, "Scope & success criteria");
  await expect(
    page.getByRole("heading", { name: "Budget approval", exact: true }),
  ).toBeVisible({ timeout: 12000 });
  await expect(
    page.getByRole("button", { name: "Approve", exact: true }),
  ).toHaveCount(0);
  await page
    .getByLabel("Server development identity", { exact: true })
    .selectOption("approver");
  await expect(
    page.getByRole("button", { name: "Approve", exact: true }),
  ).toBeVisible({ timeout: 10000 });
  await page.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Confirm action" }),
  ).toBeDisabled();
  await page
    .getByLabel("Rationale", { exact: true })
    .fill("Independent reviewer checked the local test budget");
  await page.getByRole("button", { name: "Confirm action" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page
    .getByLabel("Server development identity", { exact: true })
    .selectOption("default");
  await expect(
    page.getByRole("heading", { name: "Assign resources", exact: true }),
  ).toBeVisible({ timeout: 12000 });
  await complete(page, "Assign resources");
  await expect(page.locator(".page-heading .e-badge")).toContainText(
    "completed",
    { timeout: 12000 },
  );
  const server = await (await request.get(`/api/v1/cases/${id}`)).json();
  expect(server.status).toBe("completed");
  expect(server.approvals[0].decision.decided_by_name).toBe(
    "Local independent approver",
  );
  expect(server.approvals[0].decision.decision_hash).toHaveLength(64);
  await page.getByRole("button", { name: "Activity", exact: true }).click();
  await expect(page.locator(".operation-case")).toContainText("approval");
  await page.getByRole("button", { name: "Evidence", exact: true }).click();
  await expect(page.locator(".operation-case")).toContainText(
    "No artifacts have been produced",
  );
  await page.getByRole("button", { name: "Work", exact: true }).click();
  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    axe.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBe(false);
    await page.screenshot({
      path: `../../docs/elenio-handoff/evidence/operations-case-${width}.png`,
      fullPage: true,
    });
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/elenio.html#/connected/operations");
  await page.getByLabel("Search cases", { exact: true }).fill(title);
  await page.getByRole("combobox", { name: "Case status", exact: true }).selectOption("closed");
  const match = page
    .getByRole("link")
    .filter({ has: page.getByRole("heading", { name: title, exact: true }) });
  await expect(match).toBeVisible();
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/operations-list-1440.png",
    fullPage: true,
  });
  await match.click();
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
});

test("stale task form retains inputs and cannot resubmit after a server conflict", async ({
  page,
  request,
}) => {
  await start(page, `UI conflict ${randomUUID().slice(0, 8)}`);
  const id = page.url().split("/").at(-1) ?? "";
  const current = await (await request.get(`/api/v1/cases/${id}`)).json();
  const task = current.tasks.find(
    (t: { node_id: string; status: string }) =>
      t.node_id === "scope" && t.status === "assigned",
  );
  await page
    .getByRole("button", { name: "Complete task", exact: true })
    .click();
  await page
    .getByLabel("Comment", { exact: true })
    .fill("Keep this comment after conflict");
  const elsewhere = await request.post(`/api/v1/tasks/${task.id}/complete`, {
    headers: { "If-Match": `"${task.version_num}"` },
    data: { output: {}, comment: "Other writer completed task" },
  });
  expect(elsewhere.ok()).toBe(true);
  await page.getByRole("button", { name: "Confirm action" }).click();
  await expect(page.getByLabel("Comment", { exact: true })).toHaveValue(
    "Keep this comment after conflict",
  );
  await expect(
    page.getByRole("button", { name: "Confirm action" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Discard form and reload" }).click();
  await expect(
    page.getByText("Form discarded. Latest server state loaded."),
  ).toBeVisible();
});

test("case owner can pause/resume and add notes; viewer cannot start a case", async ({
  page,
  request,
}) => {
  await start(page, `UI pause ${randomUUID().slice(0, 8)}`);
  const id = page.url().split("/").at(-1) ?? "";
  for (const name of ["Pause case", "Resume case"]) {
    await page.getByRole("button", { name, exact: true }).click();
    await page
      .getByLabel("Rationale", { exact: true })
      .fill("Authorized local lifecycle check");
    await page.getByRole("button", { name: "Confirm action" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.locator(".page-heading .e-badge")).toContainText(
      name === "Pause case" ? "paused" : "active",
    );
  }
  await page.getByRole("button", { name: "Add note", exact: true }).click();
  await page
    .getByLabel("Rationale", { exact: true })
    .fill("Persistent local test note");
  await page.getByRole("button", { name: "Confirm action" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(
    JSON.stringify(
      (await (await request.get(`/api/v1/cases/${id}`)).json()).history,
    ),
  ).toContain("Persistent local test note");
  await page.goto("/elenio.html#/connected/operations");
  await page
    .getByLabel("Server development identity", { exact: true })
    .selectOption("viewer");
  await expect(
    page.getByRole("button", { name: "Start a case", exact: true }),
  ).toBeDisabled();
});

