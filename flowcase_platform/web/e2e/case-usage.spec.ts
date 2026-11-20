import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("case usage contract fixture distinguishes partial, unavailable and legacy source records", async ({ page }) => {
  let writes = 0;
  await page.route("**/api/v1/**", route => {
    if (route.request().method() !== "GET") writes++;
    const session = new URL(route.request().url()).pathname === "/api/v1/session";
    const snapshot = {
      schema: "elenio.agent-usage.v1", class: "test", coverage: "partial",
      tokens_in: 120, tokens_out: 24, attempts: 2, observed_attempts: 1, pricing_status: "unpriced",
    };
    return route.fulfill({contentType:"application/json",body:JSON.stringify(session ? {
      user_id:"viewer",tenant_id:"tenant",display_name:"Contract test viewer",
      roles:["workspace_member"],auth_mode:"trusted_jwt",environment:"test",
    } : {
      id:"usage-contract",title:"Usage contract fixture",reference:"CONTRACT",workflow_key:"contract",
      workflow_version:1,owner_name:"Fixture owner",status:"under_review",progress:50,
      tasks:[],approvals:[],drafts:[],actions:{can_pause:false,can_resume:false,can_note:false},
      agent_runs:[
        {node_id:"extract",agent_ref:"contract.extract",attempt:1,runtime_run_id:"runtime-partial",created_at:"2026-11-20T00:00:00Z",usage_observation:snapshot,usage_received_at:"2026-11-20T00:01:00Z"},
        {node_id:"check",agent_ref:"contract.check",attempt:1,runtime_run_id:"runtime-unknown",created_at:"2026-11-20T00:00:00Z",usage_observation:{...snapshot,coverage:"unavailable",tokens_in:0,tokens_out:0}},
        {node_id:"legacy",agent_ref:"contract.legacy",attempt:1,runtime_run_id:"runtime-legacy",created_at:"2026-11-20T00:00:00Z"},
      ],
    })});
  });
  await page.goto("/elenio.html#/connected/cases/usage-contract");
  await expect(page.getByRole("heading",{name:"Usage contract fixture",exact:true})).toBeVisible();
  await expect(page.getByRole("heading",{name:"Tasks",exact:true})).toBeVisible();
  await page.getByRole("button",{name:"AI usage",exact:true}).click();
  const usage=page.getByRole("region",{name:"Reported model usage"});
  await expect(usage).toContainText("test · partial");
  await expect(usage).toContainText("Reported tokens: 120 in / 24 out.");
  await expect(usage).toContainText("1 of 2 runtime attempts retained");
  await expect(usage).toContainText("Token usage has not been reported.");
  await expect(usage).toContainText("No verified usage snapshot is available.");
  await expect(usage).not.toContainText("Reported tokens: 0");
  await expect(page.getByRole("button",{name:"Confirm action",exact:true})).toHaveCount(0);
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:1000});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    const audit=await new AxeBuilder({page}).include('[aria-label="Reported model usage"]').analyze();
    expect(audit.violations).toEqual([]);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/case-usage-contract-${width}.png`,fullPage:true});
  }
  expect(writes).toBe(0);
});
