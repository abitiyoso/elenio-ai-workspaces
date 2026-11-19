import {test,expect,type Page} from "@playwright/test";
import {randomUUID,createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import AxeBuilder from "@axe-core/playwright";
const mount="/apps/flowcase/elenio.html";
const api="/apps/flowcase/api/v1";
const unique=()=>`workspace-${randomUUID().slice(0,18)}`;
// Real CP cookies, PKCE, trusted JWT, domain grants and PostgreSQL; no interception.
async function signIn(page:Page,persona="Demo All Access"){
  await page.goto(mount);
  await expect(page.getByRole("heading",{name:"Local workspace sign-in"})).toBeVisible();
  await page.getByLabel("Test identity").selectOption({label:persona});
  await page.getByRole("button",{name:"Continue with test identity"}).click();
  await expect(page).toHaveURL(new RegExp("^http://127\\.0\\.0\\.1:8200/apps/flowcase/"));
  await page.goto("/");
  await page.getByRole("button",{name:/Active workspace:.*Switch workspace/}).click();
  const workspace=persona==="Demo All Access"?"Platform Engineering":"SIM B2B Solutions";
  await page.getByRole("menuitemradio",{name:new RegExp(workspace,"i")}).click();
  await expect(page.getByRole("button",{name:new RegExp(`Active workspace: ${workspace}.*Switch workspace`,"i")})).toBeVisible();
  await page.goto(mount);
}
test.beforeEach(async({request})=>{
  expect(process.env.ELENIO_LOCAL_E2E).toBe("1");
  expect(process.env.ELENIO_WORKSPACE_E2E).toBe("1");
  const health=await request.get("http://127.0.0.1:8210/healthz");
  expect(await health.json()).toMatchObject({identity:"local_test_fixture"});
});
test("real workspace: persistent login, independent domain IDs and existing Agent Studio navigation",async({page})=>{
  await signIn(page);
  const cp=await (await page.request.get("/api/v1/me")).json();
  expect(cp.environment).toBe("development");
  const flow=await (await page.request.get(`${api}/session`)).json();
  const agent=await (await page.request.get("/apps/agent-studio/api/v1/session")).json();
  expect(flow).toMatchObject({auth_mode:"trusted_jwt",identity_issuer:"http://127.0.0.1:8200",identity_subject:cp.user.id});
  expect(flow.user_id).not.toBe(cp.user.id);
  expect(flow.dev_personas).toBeUndefined();
  expect(agent).toMatchObject({identity_mode:"control-plane",issuer:flow.identity_issuer,subject:flow.identity_subject,tenant_id:flow.tenant_id});
  expect(agent.self_approval_exempt).toBe(false);
  await page.goto(`${mount}#/capabilities/agents`);
  await expect(page.getByRole("heading",{name:"Agent Studio",exact:true,level:1})).toBeVisible();
  await page.getByRole("button",{name:"Open workspace",exact:true}).click();
  await expect(page).toHaveURL(/\/apps\/agent-studio\/#agents$/);
  await expect(page.getByRole("heading",{name:"Agent Registry",exact:true})).toBeVisible();
  await expect(page.getByLabel("Nama agent",{exact:true})).toBeEnabled();
  await page.screenshot({path:"../../docs/elenio-handoff/evidence/shared-agent-studio-agents-1440.png",fullPage:true});
  await page.reload();
  await expect(page.getByRole("heading",{name:"Agent Registry",exact:true})).toBeVisible();
  const agentKey=unique();
  await page.getByLabel("Nama agent",{exact:true}).fill("Workspace acceptance agent");
  await page.getByRole("textbox",{name:/^Agent key/}).fill(agentKey);
  await page.getByLabel("Domain",{exact:true}).fill("local-acceptance");
  const created=page.waitForResponse(r=>r.request().method()==="POST"&&new URL(r.url()).pathname==="/apps/agent-studio/api/v1/agents");
  await page.getByRole("button",{name:"Buat draft agent",exact:true}).click();
  expect((await created).status()).toBe(201);
  const stored=await page.request.get("/apps/agent-studio/api/v1/agents?limit=100&offset=0");
  expect(stored.ok()).toBe(true);
  const record=(await stored.json()).items.find((item:{key:string})=>item.key===agentKey);
  expect(record).toMatchObject({key:agentKey,lifecycle_status:"draft",domain:"local-acceptance",display_name:"Workspace acceptance agent"});
  expect(record.owner_subject??null).toBeNull();
  await page.goto(`${mount}#/capabilities/skills`);
  await page.getByRole("button",{name:"Open workspace",exact:true}).click();
  await expect(page).toHaveURL(/#skills$/);
  await expect(page.getByRole("heading",{name:"Skills Studio",exact:true})).toBeVisible();
  await page.goto(`${mount}#/capabilities/models`);
  await page.getByRole("button",{name:"Open workspace",exact:true}).click();
  await expect(page).toHaveURL(/#models$/);
  await expect(page.getByRole("heading",{name:"Models & Providers",exact:true})).toBeVisible();
  await expect(page.getByText("9Router Elenio",{exact:false}).first()).toBeVisible();
});
test("real workspace: create workflow, compose persisted solution, inspect and capture candidate",async({page})=>{
  await signIn(page);
  const key=unique(), solutionKey=unique();
  await page.goto(`${mount}#/connected`);
  await page.getByRole("button",{name:"New workflow",exact:true}).click();
  await page.getByLabel("Workflow name",{exact:true}).fill("Workspace acceptance workflow");
  await page.getByLabel("Workflow key").fill(key);
  await page.getByRole("button",{name:"Create draft",exact:true}).click();
  await expect(page.locator(".react-flow__node")).toHaveCount(2);
  await page.getByRole("button",{name:"Find a node"}).click();
  await page.getByLabel("Find graph node").fill("Start");
  await page.getByRole("button",{name:/Start Trigger/}).click();
  await page.getByLabel("Node name",{exact:true}).fill("Workspace reviewed start");
  await page.getByRole("button",{name:"Save draft",exact:true}).click();
  await expect(page.getByText("Draft saved by Flowcase. No version was published.")).toBeVisible();
  const path=`${api}/workflows/${key}/versions/1`;
  const validated=await page.request.post(`${path}/validate`,{headers:{"Origin":"http://127.0.0.1:8200","Idempotency-Key":unique()}});
  expect(validated.ok()).toBe(true);
  const validation=await validated.json();
  const published=await page.request.post(`${path}/publish`,{headers:{"Origin":"http://127.0.0.1:8200","If-Match":`"${validation.version.version_num}"`}});
  expect(published.ok()).toBe(true);
  await page.goto(`${mount}#/solutions`);
  await page.getByRole("button",{name:"New solution",exact:true}).click();
  await page.getByLabel("Solution name").fill("Workspace acceptance solution");
  await page.getByLabel("Solution key").fill(solutionKey);
  await page.getByLabel("Workflow versions").fill(`${key}@1`);
  await page.getByRole("button",{name:"Configure application pages"}).click();
  await page.getByLabel("Application title").fill("Acceptance business workspace");
  await page.getByRole("button",{name:"Save solution plan",exact:true}).click();
  const card=page.locator("article.solution-card").filter({hasText:`${key}@1`});
  await expect(card.getByRole("button",{name:"Preview application",exact:true})).toBeVisible();
  await page.reload();
  await card.getByRole("button",{name:"Preview application",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Acceptance business workspace",exact:true})).toBeVisible();
  await page.getByRole("button",{name:"Close dialog"}).click();
  await card.getByRole("button",{name:"Publisher candidates",exact:true}).click();
  await page.getByRole("button",{name:"Create review candidate"}).click();
  await expect(page.getByText("Candidate 1.0.0",{exact:true})).toBeVisible();
  await expect(page.getByText("Not approved for deployment.",{exact:false})).toBeVisible();
  for(const width of [1440,768,390]){
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/workspace-publisher-${width}.png`,fullPage:false});
  }
  const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();
  expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
  await page.getByRole("button",{name:"Close dialog"}).click();
  await page.reload();
  await card.getByRole("button",{name:"Publisher candidates",exact:true}).click();
  await expect(page.getByText("Candidate 1.0.0",{exact:true})).toBeVisible();
});
test("real workspace: operator cannot gain builder app or solution rights via browser headers",async({page})=>{
  await signIn(page,"Maya Putri");
  const flow=await (await page.request.get(`${api}/session`)).json();
  expect(flow.roles).toContain("case_owner");
  expect(flow.roles).not.toContain("workflow_designer");
  const spoof={"X-Flowcase-Dev-Persona":"developer","X-Tenant-ID":"acme-demo","X-Roles":"tenant_admin","Tailscale-User-Login":"demo-all@acme.local"};
  const solutions=await page.request.get("/api/v1/solutions",{headers:spoof});
  expect(solutions.status()).toBe(403);
  const studio=await page.request.get("/apps/agent-studio/api/v1/session",{headers:spoof});
  expect(studio.status()).toBe(403);
  const unchanged=await (await page.request.get(`${api}/session`,{headers:spoof})).json();
  expect(unchanged.identity_subject).toBe(flow.identity_subject);
  expect(unchanged.roles).toEqual(flow.roles);
});

test("existing Agent workbenches use the agreed shared light system, exact brand and bundled font",async({page})=>{
  await signIn(page);
  await page.goto("/apps/agent-studio/#agents");
  await expect(page.getByRole("heading",{name:"Agent Registry",level:1})).toBeVisible();
  await expect(page.getByLabel("Nama agent",{exact:true})).toBeEnabled();
  await expect(page.getByRole("link",{name:"Workspace: Platform Engineering. Open Control Plane to change workspace"})).toBeVisible();
  await expect(page.locator(".sidebar")).toHaveCSS("background-color","rgb(255, 255, 255)");
  await expect(page.locator(".domain-pack")).not.toHaveAttribute("open");
  await page.locator(".domain-pack summary").click();
  await expect(page.getByRole("button",{name:"Apply SIM pack v1",exact:true})).toBeVisible();
  await page.locator(".domain-pack summary").click();
  const logo=await page.request.get("/apps/agent-studio/brand/elenio-logo.png");
  expect(logo.ok()).toBe(true);
  const canonical=await readFile("public/brand/elenio-logo.png");
  expect(createHash("sha256").update(await logo.body()).digest("hex")).toBe(createHash("sha256").update(canonical).digest("hex"));
  await page.evaluate(()=>document.fonts.ready);
  expect(await page.evaluate(()=>document.fonts.check('14px "Inter Variable"'))).toBe(true);
  expect(await page.locator("body").evaluate(element=>getComputedStyle(element).fontFamily)).toContain("Inter Variable");
  for(const [view,heading,widths] of [
    ["agents","Agent Registry",[1440,1280,1024,768,390]],
    ["skills","Skills Studio",[1440,390]],
    ["models","Models & Providers",[1440,390]],
  ] as const){
    if(view!=="agents")await page.goto(`/apps/agent-studio/#${view}`);
    await expect(page.getByRole("heading",{name:heading,level:1})).toBeVisible();
    if(view==="skills")await expect(page.getByRole("combobox",{name:"Agent tenant",exact:true})).toBeVisible();
    if(view==="models")await expect(page.getByText("9Router Elenio",{exact:false}).first()).toBeVisible();
    for(const width of widths){
      await page.setViewportSize({width,height:900});
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
      await page.screenshot({path:`../../docs/elenio-handoff/evidence/shared-agent-studio-${view}-${width}.png`,fullPage:false});
      if(width===1440||width===390){
        const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();
        expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
      }
    }
  }
  await page.setViewportSize({width:390,height:900});
  await page.getByRole("button",{name:"Buka menu navigasi"}).click();
  await expect(page.getByRole("link",{name:"Solution engineering",exact:true})).toBeVisible();
  await page.getByRole("link",{name:"Solution engineering",exact:true}).click();
  await expect(page).toHaveURL(/\/apps\/flowcase\/elenio\.html#\/connected$/);
});
