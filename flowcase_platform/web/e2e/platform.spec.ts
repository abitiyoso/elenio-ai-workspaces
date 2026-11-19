import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("home → project → graph; edit, persist, role boundary and knowledge evidence", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/elenio.html#/");
  await expect(
    page.getByRole("heading", { name: /Build AI that works/ }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: /Vendor payment assurance Draft/ })
    .click();
  await expect(
    page.getByRole("heading", { name: "Vendor payment assurance" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Open Workflow Studio" }).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(11);
  await page
    .locator(".react-flow__node")
    .filter({ hasText: "Vendor matched?" })
    .click();
  await expect(page.getByLabel("true branch target")).toHaveValue("review");
  await expect(page.getByLabel("false branch target")).toHaveValue("exception");
  await page
    .getByLabel("Node name", { exact: true })
    .fill("Vendor evidence matched?");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Draft saved on this device" }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page
      .locator(".react-flow__node")
      .filter({ hasText: "Vendor evidence matched?" }),
  ).toBeVisible();
  await page.getByLabel("Preview role").selectOption("Reviewer");
  await expect(
    page.getByRole("button", { name: "Add node", exact: true }),
  ).toBeDisabled();
  await page
    .locator(".react-flow__node")
    .filter({ hasText: "Finance approval" })
    .click();
  await expect(page.getByLabel("Node name", { exact: true })).toHaveAttribute(
    "readonly",
    "",
  );
  await page.getByRole("link", { name: "Knowledge", exact: true }).click();
  await page.getByLabel("Search documents").fill("vendor");
  await expect(
    page.getByRole("button", { name: /^Vendor verification handbook.pdf/ }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: /^Vendor verification handbook.pdf/ })
    .click();
  await expect(page.getByRole("dialog")).toContainText(
    "Synthetic design-review content",
  );
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(errors).toEqual([]);
});

test("node palette, invalid diagnostic, undo/redo and canonical import preview", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/elenio.html#/projects/vendor-payment/workflow");
  await expect(page.locator(".react-flow__node")).toHaveCount(11);
  await page.getByRole("button", { name: "Add node", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /Unavailable.*runtime 1.0/ }),
  ).toBeDisabled();
  await page.getByLabel("Search node types").fill("timer");
  await page.getByRole("button", { name: /Timer timer/ }).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(12);
  await page.getByRole("button", { name: /issues/ }).click();
  await expect(page.locator(".graph-issues")).toContainText("unreachable");
  await page.getByRole("button", { name: "Close diagnostics" }).click();
  await page.getByRole("button", { name: "Close inspector" }).click();
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(11);
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(12);
  await page.getByRole("button", { name: "Import Flowcase JSON" }).click();
  await page
    .getByLabel("Definition JSON")
    .fill('{"connections":{},"nodes":[]}');
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByRole("alert")).toContainText("schema 1.0");
});

test("responsive pages have no horizontal overflow and mobile graph remains readable", async ({
  page,
}) => {
  for (const size of [
    { width: 1440, height: 900 },
    { width: 1280, height: 800 },
    { width: 1024, height: 768 },
    { width: 768, height: 1024 },
    { width: 390, height: 844 },
    { width: 360, height: 800 },
  ]) {
    await page.setViewportSize(size);
    for (const route of [
      "",
      "projects/vendor-payment",
      "projects/vendor-payment/workflow",
      "knowledge",
    ]) {
      await page.goto(`/elenio.html#/${route}`);
      await page.waitForTimeout(500);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 1,
      );
      expect(overflow, `${size.width}px ${route} overflows`).toBe(false);
      if (size.width < 768 && route.endsWith("workflow")) {
        await expect(page.locator(".mobile-node-list")).toBeVisible();
        await page
          .locator(".mobile-node-list button")
          .filter({ hasText: "Vendor matched?" })
          .click();
        await expect(page.getByLabel("true branch target")).toBeVisible();
        await page.getByRole("button", { name: "Close inspector" }).click();
      }
      await page.screenshot({
        path: `../../docs/elenio-handoff/evidence/${route.replaceAll("/", "-") || "home"}-${size.width}.png`,
        fullPage: true,
      });
    }
  }
});

test("keyboard command navigation and accessible dialog focus", async ({
  page,
}) => {
  await page.goto("/elenio.html#/");
  await page.keyboard.press("Control+k");
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByLabel("Search workspace").fill("design");
  await page
    .getByRole("button", { name: "Design system · component gallery" })
    .click();
  await expect(
    page.getByRole("heading", { name: "Design system", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Primary action" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Primary action" }),
  ).toBeFocused();
});

test("core pages pass serious/critical accessibility checks", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  for (const route of [
    "",
    "projects/vendor-payment",
    "projects/vendor-payment/workflow",
    "knowledge",
    "design-system",
  ]) {
    await page.goto(`/elenio.html#/${route}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.waitForTimeout(300);
    const result = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
      .analyze();
    expect(
      result.violations.filter((v) =>
        ["serious", "critical"].includes(v.impact ?? ""),
      ),
      `${route}: ${JSON.stringify(result.violations.map((v) => ({ id: v.id, nodes: v.nodes.map((n) => n.target) })))}`,
    ).toEqual([]);
  }
});

test("real drag, connector edits, resizable inspector and position undo preserve semantics", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/elenio.html#/projects/vendor-payment/workflow");
  await page.getByRole("button", { name: "Find a node" }).click();
  await page.getByLabel("Find graph node").fill("Collect evidence");
  await page.getByRole("button", { name: /Collect evidence Join/ }).click();
  const resize = page.getByRole("separator", { name: "Resize inspector" });
  await resize.focus();
  await page.keyboard.press("ArrowLeft");
  await expect(resize).toHaveAttribute("aria-valuenow", "330");
  await page.screenshot({
    path: "../../docs/elenio-handoff/evidence/inspector-1440.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "Close inspector" }).click();
  const node = page.locator('.react-flow__node[data-id="join"]');
  const before = await node.getAttribute("style");
  const box = await node.boundingBox();
  if (!box) throw new Error("Node not visible");
  await page.mouse.move(box.x + 70, box.y + 45);
  await page.mouse.down();
  await page.mouse.move(box.x + 140, box.y + 100, { steps: 10 });
  await page.mouse.up();
  await expect(node).not.toHaveAttribute("style", before ?? "");
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(node).toHaveAttribute("style", before ?? "");
  await page.locator(".react-flow__controls-fitview").click();
  await page.waitForTimeout(400);
  const source = page.locator(
    '.react-flow__node[data-id="document-check"] .react-flow__handle.source',
  );
  const target = page.locator(
    '.react-flow__node[data-id="match"] .react-flow__handle.target',
  );
  const from = await source.boundingBox(),
    to = await target.boundingBox();
  if (!from || !to) throw new Error("Ports not visible");
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, {
    steps: 15,
  });
  await page.mouse.up();
  await page.getByRole("button", { name: "View canonical JSON" }).click();
  const doc = JSON.parse(
    await page.getByLabel("Definition JSON").inputValue(),
  ) as { nodes: { id: string; next: string[] }[] };
  expect(doc.nodes.find((n) => n.id === "document-check")?.next).toEqual([
    "match",
  ]);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await page.getByRole("button", { name: "Enter focus mode" }).click();
  await expect(page.locator(".platform-sidebar")).toBeHidden();
});

test("kit launchpad uses bundled typography, scenario brief and contextual project sections", async ({
  page,
}) => {
  await page.goto("/elenio.html#/");
  await expect(
    page.getByRole("heading", { name: "Build AI that works." }),
  ).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  const typography = await page.evaluate(() => ({
    family: getComputedStyle(document.body).fontFamily,
    loaded: [...document.fonts].some(
      (f) => f.family.includes("Inter Variable") && f.status === "loaded",
    ),
  }));
  expect(typography.family).toContain("Inter Variable");
  expect(typography.loaded).toBe(true);
  await page.getByRole("button", { name: "Recruitment", exact: true }).click();
  await expect(page.getByLabel("Solution brief")).toHaveValue(/HR/);
  await page
    .getByRole("button", { name: "Create with AI", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "AI Solution Architect", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Business requirement")).toHaveValue(/HR/);
  await expect(
    page.getByRole("button", { name: "Generate blueprint" }),
  ).toBeDisabled();
  await page.goto("/elenio.html#/projects/vendor-payment");
  await page.getByRole("button", { name: "Dependencies", exact: true }).click();
  await expect(page.getByRole("table")).toContainText(
    "fixture.document-intelligence@1",
  );
  await page.getByRole("button", { name: "Versions", exact: true }).click();
  await expect(page.getByRole("table")).toContainText("Local design preview");
  await page.getByRole("button", { name: "Economics", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Open scenario planner" }),
  ).toBeVisible();
});
