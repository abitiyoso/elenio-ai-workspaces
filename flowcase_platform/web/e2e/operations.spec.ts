import { test, expect } from "@playwright/test";
test("production session never exposes configured development identities or viewer writes", async ({
  page,
}) => {
  const writes: string[] = [];
  await page.route("**/api/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") writes.push(p);
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(
        p === "/api/v1/session"
          ? {
              user_id: "prod-user",
              tenant_id: "tenant",
              display_name: "Contract production viewer",
              roles: ["workspace_member"],
              auth_mode: "trusted_jwt",
              environment: "production",
              dev_personas: [
                {
                  key: "admin",
                  display_name: "Must not appear",
                  roles: ["tenant_admin"],
                },
              ],
            }
          : { items: [], next_cursor: null },
      ),
    });
  });
  await page.goto("/elenio.html#/connected/operations");
  await expect(
    page.getByRole("heading", { name: "Cases & reviews", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByLabel("Server development identity", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Start a case", exact: true }),
  ).toBeDisabled();
  expect(writes).toEqual([]);
});
test("denied case deep link has no fixture or mutation fallback", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/api/v1/**", (route) => {
    const session =
      new URL(route.request().url()).pathname === "/api/v1/session";
    if (route.request().method() !== "GET") writes++;
    return route.fulfill({
      status: session ? 200 : 403,
      contentType: "application/json",
      body: JSON.stringify(
        session
          ? {
              user_id: "viewer",
              tenant_id: "tenant",
              display_name: "Contract test viewer",
              roles: ["workspace_member"],
              auth_mode: "dev_mock",
              environment: "test",
            }
          : { error: { code: "forbidden", message: "Case access denied" } },
      ),
    });
  });
  await page.goto(
    "/elenio.html#/connected/cases/00000000-0000-4000-8000-000000000001",
  );
  await expect(page.getByRole("alert")).toContainText("Case access denied");
  await expect(
    page.getByRole("button", { name: "Complete task", exact: true }),
  ).toHaveCount(0);
  expect(writes).toBe(0);
});

test("draft review controls use existing versioned contracts without generating model output", async ({
  page,
}) => {
  const writes: {
    path: string;
    headers: Record<string, string>;
    body: unknown;
  }[] = [];
  await page.route("**/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") {
      writes.push({
        path,
        headers: route.request().headers(),
        body: route.request().postDataJSON(),
      });
      return route.fulfill({ contentType: "application/json", body: "{}" });
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(
        path === "/api/v1/session"
          ? {
              user_id: "owner",
              tenant_id: "tenant",
              display_name: "Contract draft reviewer",
              roles: ["case_owner"],
              auth_mode: "dev_mock",
              environment: "test",
            }
          : {
              id: "contract-case",
              title: "Contract draft case",
              reference: "CONTRACT",
              workflow_key: "contract",
              workflow_version: 1,
              owner_name: "Contract owner",
              status: "under_review",
              progress: 50,
              tasks: [],
              approvals: [],
              actions: { can_pause: false, can_resume: false, can_note: false },
              drafts: [
                {
                  id: "contract-draft",
                  node_name: "Contract agent",
                  artifact_type: "draft",
                  artifact_version: 1,
                  status: "pending",
                  revisions: 0,
                  content: { text: "Explicit contract fixture" },
                  can_submit: true,
                  can_revise: true,
                  version_num: 7,
                },
              ],
            },
      ),
    });
  });
  for (const kind of ["submit", "revision"]) {
    await page.goto("/elenio.html#/connected/cases/contract-case");
    await page
      .getByRole("button", {
        name: kind === "submit" ? "Submit draft" : "Request draft revision",
        exact: true,
      })
      .click();
    await page
      .getByLabel(kind === "submit" ? "Comment" : "Rationale", { exact: true })
      .fill("Human reviewed this contract fixture");
    await page.getByRole("button", { name: "Confirm action" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(writes.at(-1)).toMatchObject({
      path: `/api/v1/drafts/contract-draft/${kind}`,
      headers: { "if-match": '"7"' },
      body:
        kind === "submit"
          ? { note: "Human reviewed this contract fixture" }
          : { feedback: "Human reviewed this contract fixture" },
    });
  }
});
