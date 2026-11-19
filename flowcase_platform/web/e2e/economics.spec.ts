import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { readFile } from "node:fs/promises";
test("economics matches kit totals and reacts to branch, retry, missing rates and zero completion", async ({
  page,
}) => {
  await page.goto("/elenio.html#/economics");
  await expect(page.getByTestId("cost-completed")).toContainText("5.550");
  await expect(page.locator(".economics-facts")).toContainText("318 cases/mo");
  await expect(page.locator(".economics-facts")).toContainText("53.75%");
  await page.getByLabel("Unmatched branch (%)", { exact: true }).fill("0");
  await expect(page.getByTestId("cost-completed")).toContainText("5.400");
  await page.getByLabel("Acceptable completion (%)", { exact: true }).fill("0");
  await expect(page.getByTestId("cost-completed")).toHaveText("N/A");
  await page.getByRole("spinbutton", { name: /^Extraction/ }).fill("");
  await expect(page.getByRole("alert")).toContainText("Unpriced: Extraction");
  await expect(page.getByTestId("cost-completed")).toHaveCount(0);
  await page.getByRole("button", { name: "Load synthetic sample" }).click();
  await page
    .getByLabel("Expected extraction attempts", { exact: true })
    .fill("2");
  await expect(page.getByTestId("cost-completed")).toContainText("5.800");
});
test("economics keeps explicit browser revisions and imports only valid scenario documents", async ({
  page,
}) => {
  await page.goto("/elenio.html#/economics");
  await page.getByRole("button", { name: "Save browser revision" }).click();
  await page
    .getByLabel("Monthly attempted cases", { exact: true })
    .fill("2000");
  await page.getByRole("button", { name: "Save browser revision" }).click();
  await page.reload();
  await page
    .getByRole("button", { name: "Load revision 2", exact: true })
    .click();
  await expect(
    page.getByLabel("Monthly attempted cases", { exact: true }),
  ).toHaveValue("2000");
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export scenario JSON" }).click();
  const exported = await download;
  expect(exported.suggestedFilename()).toBe("elenio-economics-scenario.json");
  await page
    .getByRole("button", { name: "Import scenario", exact: true })
    .click();
  await page.getByLabel("Scenario JSON").fill('{"schema":2}');
  await page.getByRole("button", { name: "Replace planning inputs" }).click();
  await expect(page.getByRole("dialog")).toContainText("Unsupported scenario");
  await page.keyboard.press("Escape");
  await expect(
    page.getByLabel("Monthly attempted cases", { exact: true }),
  ).toHaveValue("2000");
  const filename = await exported.path();
  if (!filename) throw new Error("Export file missing");
  await page
    .getByRole("button", { name: "Import scenario", exact: true })
    .click();
  await page.getByLabel("Scenario JSON").fill(await readFile(filename, "utf8"));
  await page.getByRole("button", { name: "Replace planning inputs" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByLabel("Monthly attempted cases", { exact: true }),
  ).toHaveValue("2000");
});
test("economics is responsive and passes serious/critical accessibility rules", async ({
  page,
}) => {
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/elenio.html#/economics");
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBe(false);
    await page.screenshot({
      path: `../../docs/elenio-handoff/evidence/economics-${width}.png`,
      fullPage: true,
    });
  }
  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    axe.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
});
