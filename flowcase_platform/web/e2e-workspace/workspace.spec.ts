import {test,expect,type Page} from "@playwright/test";
import {randomUUID,createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import AxeBuilder from "@axe-core/playwright";
const mount="/apps/flowcase/elenio.html";
const api="/apps/flowcase/api/v1";
const unique=()=>`workspace-${randomUUID().slice(0,18)}`;
async function verifyWorkbench(page:Page,name:string,mobileMenu:string){
  const logo=page.getByRole("img",{name:"Elenio",exact:true});
  const response=await page.request.get(await logo.getAttribute("src")??"");
  const canonical=await readFile("public/brand/elenio-logo.png");
  expect(createHash("sha256").update(await response.body()).digest("hex")).toBe(createHash("sha256").update(canonical).digest("hex"));
  expect(await page.locator("body").evaluate(element=>getComputedStyle(element).fontFamily)).toContain("Inter Variable");
  expect(await page.locator(".sidebar").evaluate(element=>getComputedStyle(element).backgroundColor)).toBe("rgb(255, 255, 255)");
  expect(await page.evaluate(async()=> (await document.fonts.load('14px "Inter Variable"')).length)).toBeGreaterThan(0);
  for(const width of [1440,1280,1024,768,390]){
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/${name}-${width}.png`,fullPage:false});
    if(width===1440||width===390){const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);}
  }
  const trigger=page.getByRole("button",{name:mobileMenu,exact:true});
  await trigger.click();await expect(page.locator(".sidebar a[href]").first()).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  expect(await page.locator(".sidebar").evaluate(element=>element.contains(document.activeElement))).toBe(true);
  await page.keyboard.press("Escape");await expect(trigger).toBeFocused();
  await page.setViewportSize({width:1440,height:900});
}
// Real CP cookies, PKCE, trusted JWT, domain grants and PostgreSQL; no interception.
async function signIn(page:Page,persona="Demo All Access"){
  await page.goto(mount);
  await expect(page.getByRole("heading",{name:"Local workspace sign-in"})).toBeVisible();
  await page.getByLabel("Test identity").selectOption({label:persona});
  await page.getByRole("button",{name:"Continue with test identity"}).click();
  await expect(page).toHaveURL(new RegExp("^http://127\\.0\\.0\\.1:8200/apps/flowcase/"));
  await page.goto("/");
  await page.getByRole("button",{name:/Active workspace:.*Switch workspace/}).click();
  const workspace=["Maya Putri","Dimas Wijaya"].includes(persona)?"SIM B2B Solutions":"Platform Engineering";
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
  await expect(page.getByText("Governed workspace",{exact:true})).toBeVisible();
  await expect(page.getByText("Local workspace · 1 project",{exact:true})).toHaveCount(0);
  await expect(page.getByText("Abitiyoso",{exact:true})).toHaveCount(0);
  const registered=await (await page.request.get(`${api}/workflows`)).json();
  if(registered.items.length)await expect(page.locator(".recent-workflows")).toContainText(registered.items[0].name);
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
  const agentKeyInput=page.getByRole("textbox",{name:/^Agent key/});
  await agentKeyInput.fill("INVALID KEY");
  expect(await agentKeyInput.evaluate((element:HTMLInputElement)=>element.validity.patternMismatch)).toBe(true);
  await agentKeyInput.fill(agentKey);
  expect(await agentKeyInput.evaluate((element:HTMLInputElement)=>element.validity.patternMismatch)).toBe(false);
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
  expect(await page.locator("input[pattern]").evaluateAll(elements=>elements.every(element=>{try{new RegExp(element.getAttribute("pattern")??"","v");return true;}catch{return false;}}))).toBe(true);
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
  await page.getByRole("button",{name:"Add application page",exact:true}).click();
  await page.getByLabel("Page 2 title",{exact:true}).fill("Submit request");
  await page.getByLabel("Page 2 component").selectOption("intake");
  await page.getByLabel("Page 2 workflow").selectOption(key);
  await page.getByRole("button",{name:"Save solution plan",exact:true}).click();
  const card=page.locator("article.solution-card").filter({hasText:`${key}@1`});
  await expect(card.getByRole("button",{name:"Preview application",exact:true})).toBeVisible();
  await page.reload();
  await card.getByRole("button",{name:"Preview application",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Acceptance business workspace",exact:true})).toBeVisible();
  await page.getByRole("button",{name:"Submit request",exact:true}).click();
  await page.getByLabel("Case title",{exact:true}).fill("Real Solution development execution");
  const started=page.waitForResponse(response=>response.request().method()==="POST"&&/\/api\/v1\/solutions\/[^/]+\/executions$/.test(new URL(response.url()).pathname));
  await page.getByRole("button",{name:"Start case",exact:true}).click();
  const startResponse=await started;
  expect(startResponse.status()).toBe(200);
  const run=await startResponse.json();
  expect(run.execution).toMatchObject({workflow_key:key,workflow_version:1,plan_revision:1,case_id:run.case.id});
  await expect(page.getByRole("heading",{name:"Real Solution development execution",level:1})).toBeVisible();
  for(const width of [1440,768,390]){
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/solution-execution-${width}.png`,fullPage:false});
  }
  const executionAxe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();
  expect(executionAxe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
  await page.setViewportSize({width:1440,height:900});
  await page.getByRole("button",{name:"Close dialog",exact:true}).click();
  await page.reload();
  await card.getByRole("button",{name:"Preview application",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Real Solution development execution",level:4})).toBeVisible();
  const cp=await (await page.request.get("/api/v1/me")).json();
  const replay=await page.request.post(`/api/v1/solutions/${run.execution.solution_id}/executions`,{
    headers:{"Origin":"http://127.0.0.1:8200","X-CSRF-Token":cp.csrf_token,"If-Match":'"1"',"Idempotency-Key":startResponse.request().headers()["idempotency-key"]},
    data:startResponse.request().postDataJSON(),
  });
  expect(replay.status()).toBe(200);
  expect((await replay.json()).execution.id).toBe(run.execution.id);
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
  await page.getByRole("button",{name:"Close dialog",exact:true}).click();
  await page.setViewportSize({width:1440,height:900});
  await card.getByRole("link",{name:"Open Solution journey",exact:true}).click();
  await expect(page.getByRole("navigation",{name:"Solution stages"})).toBeVisible();
  await page.getByRole("link",{name:/3\s*Configure/}).click();
  await expect(page.getByLabel("Application title",{exact:true})).toHaveValue("Acceptance business workspace");
  await page.getByRole("link",{name:/4\s*Preview & Test/}).click();
  await expect(page.getByRole("heading",{name:"Real Solution development execution",level:4})).toBeVisible();
  await page.reload();
  await expect(page.getByRole("heading",{name:"Real Solution development execution",level:4})).toBeVisible();
  for(const width of [1440,768,390]){
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.evaluate(()=>window.scrollTo(0,0));
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/solution-journey-${width}.png`,fullPage:true});
  }
});
test("native Publisher observes exact source deployment bindings without invoking an agent",async({page})=>{
  await signIn(page);
  const direct=await page.request.get(`${api}/workflows/sim-tender/versions/4/dependencies`);
  expect(direct.ok()).toBe(true);
  const source=await direct.json();
  expect(source.schema).toBe("elenio.workflow-bindings.v1");
  expect(source.bindings.some((b:{kind:string})=>b.kind==="agent")).toBe(true);
  await page.goto(`${mount}#/solutions`);
  await page.getByRole("button",{name:"New solution",exact:true}).click();
  const key=unique();
  await page.getByLabel("Solution name").fill(`Native binding evidence ${key}`);
  await page.getByLabel("Solution key").fill(key);
  await page.getByLabel("Workflow versions").fill("sim-tender@4");
  await page.getByRole("button",{name:"Save solution plan",exact:true}).click();
  const card=page.locator("article.solution-card").filter({hasText:`Native binding evidence ${key}`});
  const checked=page.waitForResponse(response=>response.request().method()==="GET"&&/\/api\/v1\/solutions\/[^/]+\/readiness$/.test(new URL(response.url()).pathname));
  await card.getByRole("button",{name:"Check workflow dependencies",exact:true}).click();
  const result=await(await checked).json();
  expect(result.release_ready).toBe(false);
  expect(result.workflows[0].binding_snapshot.content_hash).toBe(source.content_hash);
  expect(result.workflows[0].binding_snapshot.definition_hash).toBe(source.definition_hash);
  const expectedAgents=[...new Set(source.bindings.filter((b:{kind:string})=>b.kind==="agent").map((b:{runtime_agent_id:string})=>b.runtime_agent_id))].sort();
  expect(result.agent_releases.map((o:{runtime_agent_id:string})=>o.runtime_agent_id).sort()).toEqual(expectedAgents);
  await card.getByText(/Observed bindings · sim-tender@4/).click();
  await expect(card.getByText(/runtime sim.intake/)).toBeVisible();
  await card.getByText(`Agent release evidence · ${expectedAgents.length} references`,{exact:true}).click();
  await expect(card.getByRole("heading",{name:/sim.intake · requested/})).toBeVisible();
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:1000});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    const audit=await new AxeBuilder({page}).include(".solution-card").analyze();
    expect(audit.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
    await card.getByText(`Agent release evidence · ${expectedAgents.length} references`,{exact:true}).focus();
    await card.screenshot({path:`../../docs/elenio-handoff/evidence/native-binding-evidence-${width}.png`});
    await page.setViewportSize({width,height:1800});
    const releaseEvidence=card.locator("details").filter({hasText:`Agent release evidence · ${expectedAgents.length} references`});
    await releaseEvidence.screenshot({path:`../../docs/elenio-handoff/evidence/native-publisher-agent-evidence-${width}.png`});
  }
});

test("native Agent release evidence enforces source routing and validation without inference",async({page})=>{
  await signIn(page);
  const path="/apps/agent-studio/api/v1/releases/evidence";
  const missing=await page.request.get(`${path}?agent_key=${unique()}`);
  expect(missing.status()).toBe(409);
  expect(await missing.json()).toMatchObject({error:"agent has no active release in this environment"});
  const invalid=await page.request.get(`${path}?agent_key=sim.intake&environment=production`);
  expect(invalid.status()).toBe(400);
});

test("guided Home saves one Solution across description, Composer and preview without starting a model call",async({page})=>{
  await signIn(page);
  let modelCalls=0;
  page.on("request",request=>{if(request.method()==="POST"&&request.url().includes("/sandbox/runs"))modelCalls++;});
  const key=unique(),requirement="Review employee equipment requests with independent department approval.";
  await page.getByLabel("Solution brief",{exact:true}).fill(requirement);
  await page.screenshot({path:"../../docs/elenio-handoff/evidence/solution-home-1440.png",fullPage:false});
  await page.getByRole("button",{name:"Create a solution",exact:true}).click();
  await expect(page.getByRole("textbox",{name:"Business requirement",exact:true})).toHaveValue(requirement);
  await page.getByLabel("Solution name",{exact:true}).fill("Department request workspace");
  await page.getByLabel("Solution key",{exact:true}).fill(key);
  await page.getByRole("button",{name:"Save & continue to Blueprint",exact:true}).click();
  await expect(page).toHaveURL(/#\/solutions\/[a-f0-9-]{36}\/blueprint$/);
  const solutionID=page.url().match(/solutions\/([a-f0-9-]{36})\//)?.[1];
  expect(solutionID).toBeTruthy();
  await page.getByRole("link",{name:/3\s*Configure/}).click();
  await page.getByRole("button",{name:"Configure application pages",exact:true}).click();
  await page.getByLabel("Application title",{exact:true}).fill("Department requests");
  await page.getByRole("button",{name:"Save Solution",exact:true}).click();
  await expect(page.getByText("Saved revision 2",{exact:false})).toBeVisible();
  await page.getByRole("link",{name:/4\s*Preview & Test/}).click();
  await expect(page.getByRole("heading",{name:"Department requests",exact:true})).toBeVisible();
  await page.reload();
  await expect(page.getByRole("heading",{name:"Department requests",exact:true})).toBeVisible();
  await page.getByRole("link",{name:/1\s*Describe/}).click();
  await page.getByRole("textbox",{name:"Business requirement",exact:true}).fill(requirement+" Track evidence.");
  await page.getByRole("link",{name:/3\s*Configure/}).click();
  await expect(page.getByRole("dialog",{name:"Unsaved Solution changes"})).toBeVisible();
  await page.getByRole("button",{name:"Keep editing",exact:true}).click();
  await page.getByRole("button",{name:"Save Solution",exact:true}).click();
  await expect(page.getByText("Saved revision 3",{exact:false})).toBeVisible();
  const saved=await (await page.request.get(`/api/v1/solutions/${solutionID}`)).json();
  expect(saved.definition).toMatchObject({key,description:requirement+" Track evidence.",application:{title:"Department requests"}});
  expect(modelCalls).toBe(0);
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:900});
    await page.evaluate(()=>window.scrollTo(0,0));
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/solution-describe-${width}.png`,fullPage:true});
    const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();
    expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
  }
  await page.setViewportSize({width:1440,height:900});
  await page.getByRole("button",{name:"Version history",exact:true}).click();
  const history=page.getByRole("dialog",{name:"Solution version history"});
  const firstRevision=history.locator("details").filter({hasText:"Revision 1 · Department request workspace"});
  await firstRevision.locator("summary").click();
  await expect(firstRevision.getByText(requirement,{exact:true})).toBeVisible();
  await expect(firstRevision.getByText("No workflows linked.",{exact:true})).toBeVisible();
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/solution-revisions-${width}.png`,fullPage:false});
    const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();
    expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
  }
  await page.getByRole("button",{name:"Close dialog",exact:true}).click();
  const older=await (await page.request.get(`/api/v1/solutions/${solutionID}/revisions/1`)).json();
  expect(older.plan.definition.description).toBe(requirement);
  expect(older.origin).toBe("write");
  expect(older.content_hash).toMatch(/^[a-f0-9]{64}$/);
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

test("HTML delivery menus reuse real connector catalog, Agent tests and Control Plane settings",async({page})=>{
  await signIn(page);
  await page.getByRole("navigation",{name:"Main workspace"}).getByRole("link",{name:"Workflow Studio",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Workflow Studio",level:1})).toBeVisible();
  await expect(page.getByRole("navigation",{name:"Main workspace"}).getByRole("link",{name:"Workflow Studio",exact:true})).toHaveAttribute("aria-current","page");
  await page.goto(`${mount}#/connected/integrations`);
  await expect(page.getByRole("heading",{name:"Integration Hub",level:1})).toBeVisible();
  const catalog=await (await page.request.get(`${api}/workflow-catalog`)).json();
  expect(catalog.connectors.length).toBeGreaterThan(0);
  await expect(page.getByRole("table")).toContainText(catalog.connectors[0].name);
  await page.goto(`${mount}#/capabilities/testing`);
  await page.getByRole("button",{name:"Open workspace",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Playground",level:1})).toBeVisible();
  await page.goto(`${mount}#/capabilities/evaluation`);
  await page.getByRole("button",{name:"Open workspace",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Evaluations",level:1})).toBeVisible();
  await page.goto(`${mount}#/control-center`);
  await expect(page.getByRole("heading",{name:"Control Center",level:1})).toBeVisible();
  await expect(page.getByRole("link",{name:"Manage people",exact:true})).toHaveAttribute("href","/people");
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/html-control-center-${width}.png`,fullPage:false});
  }
  await page.getByRole("link",{name:"Manage people",exact:true}).click();
  await expect(page).toHaveURL("http://127.0.0.1:8200/people");
});

test("existing KMS tenant authority persists collection metadata and rejects a CP admin without KMS membership",async({page,browser})=>{
  const unexpectedDialogs:string[]=[];
  page.on("dialog",async dialog=>{unexpectedDialogs.push(dialog.type());await dialog.dismiss();});
  await signIn(page);
  const cp=await (await page.request.get("/api/v1/me")).json();
  const identityResponse=await page.request.get("/apps/kms/api/v1/session");
  expect(identityResponse.ok()).toBe(true);
  expect(await identityResponse.json()).toMatchObject({user_id:cp.user.id,tenant_id:"00000000-0000-4000-8000-000000000001",roles:expect.arrayContaining(["knowledge_steward"])});
  await page.getByRole("navigation",{name:"Main workspace"}).getByRole("link",{name:"Knowledge Hub",exact:true}).click();
  await expect(page.getByText("Verified KMS session",{exact:true})).toBeVisible();
  const sharedName=unique();
  await page.getByRole("button",{name:"New collection",exact:true}).click();
  await page.getByLabel("Collection name",{exact:true}).fill(sharedName);
  await page.getByRole("button",{name:"Create collection",exact:true}).click();
  await expect(page.getByRole("button",{name:new RegExp(sharedName)})).toBeVisible();
  await page.reload();
  await expect(page.getByRole("button",{name:new RegExp(sharedName)})).toBeVisible();
  await expect(page.getByRole("button",{name:"Ingest PDF",exact:true})).toBeDisabled();
  for(const width of [1440,1280,1024,768,390]){
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/html-live-knowledge-${width}.png`,fullPage:false});
    if(width===1440||width===390){const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);}
  }
  await page.getByRole("button",{name:"Ingestion jobs",exact:true}).click();
  await expect(page.getByRole("heading",{name:"No ingestion jobs",exact:true})).toBeVisible();
  await page.getByRole("button",{name:"Retrieval playground",exact:true}).click();
  await expect(page.getByText("The search index is unavailable.",{exact:true})).toBeVisible();
  await page.setViewportSize({width:1440,height:900});
  await page.getByRole("link",{name:"Open governance workspace",exact:true}).click();
  await expect(page).toHaveURL("http://127.0.0.1:8200/apps/kms/");
  expect(unexpectedDialogs).toEqual([]);
  await page.getByRole("button",{name:"Collections",exact:true}).click();
  const name=unique();
  await page.getByLabel("Collection name",{exact:true}).fill(name);
  await page.getByRole("button",{name:"Create collection",exact:true}).click();
  await expect(page.getByText(name,{exact:true})).toBeVisible();
  const stored=await (await page.request.get("/apps/kms/api/v1/collections")).json();
  expect(stored.collections.some((item:{name:string})=>item.name===name)).toBe(true);
  await page.screenshot({path:"../../docs/elenio-handoff/evidence/html-existing-kms-1440.png",fullPage:false});
  await verifyWorkbench(page,"shared-kms-collections","Open navigation menu");
  const deniedContext=await browser.newContext(),deniedPage=await deniedContext.newPage();
  await signIn(deniedPage,"Nadia Kusuma");
  const rejected=await deniedPage.request.get("/apps/kms/api/v1/session",{headers:{"X-KMS-User-Id":cp.user.id,"X-Roles":"platform_admin,knowledge_steward"}});
  expect(rejected.status()).toBe(401);
  await deniedPage.goto(`${mount}#/knowledge`);
  await expect(deniedPage.getByRole("button",{name:"Retry Knowledge connection",exact:true})).toBeVisible();
  await expect(deniedPage.getByText("Verified KMS session",{exact:true})).toHaveCount(0);
  await expect(deniedPage.getByText("Finance approval policy.pdf",{exact:true})).toHaveCount(0);
  await deniedContext.close();
});

test("Business Apps opens original UCW and exchanges the real operator identity to Flowcase",async({page})=>{
  await signIn(page,"Maya Putri");
  const cp=await (await page.request.get("/api/v1/me")).json();
  await page.goto(`${mount}#/business-apps`);
  await expect(page.getByRole("heading",{name:"Business Apps",level:1})).toBeVisible();
  await page.getByRole("button",{name:"Open workspace",exact:true}).click();
  await expect(page).toHaveURL("http://127.0.0.1:8200/apps/case-workspace/");
  const response=await page.request.get("/apps/case-workspace/api/v1/workspace/me/context");
  expect(response.ok()).toBe(true);
  const context=await response.json();
  expect(context).toMatchObject({user_id:cp.user.id,tenant_key:"acme-demo",workspace_key:"sim-b2b",flowcase:{available:true,can_create_cases:true,roles:expect.arrayContaining(["case_owner"])}});
  expect(context.flowcase.roles).not.toContain("tenant_admin");
  const cases=await page.request.get("/apps/case-workspace/api/v1/workspace/cases");
  expect(cases.ok()).toBe(true);
  await expect(page.locator("main")).toBeVisible();
  await page.screenshot({path:"../../docs/elenio-handoff/evidence/html-existing-business-workspace-1440.png",fullPage:false});
  await verifyWorkbench(page,"shared-business-cases","Open navigation");
});

// Explicit UI contract fixture: real mounted Agent UI/session, intercepted usage payloads, no model execution or database metrics seeded.
test("Agent usage UI contract fixture separates test, partial and unpriced observations",async({page})=>{
  await signIn(page);
  const runID="00000000-0000-4000-8000-000000000711",stamp="2026-11-20T12:00:00Z";
  const fixture={id:runID,agent_id:"00000000-0000-4000-8000-000000000712",agent_key:"usage.contract-fixture",agent_version_id:"00000000-0000-4000-8000-000000000713",agent_semver:"1.0.0",kind:"sandbox",caller_kind:"user",caller_subject:"explicit-contract-fixture",idempotency_key:"explicit-usage-fixture",input:{},evidence_input:[],status:"succeeded",model_mode:"provider",pinned:{},output:{summary:"UI contract fixture, not a provider execution"},validation:{schema_valid:true,risk_flags:[]},attempts:2,max_attempts:3,deadline_at:stamp,cancel_requested:false,tokens_in:300,tokens_out:30,tool_calls:0,created_at:stamp,started_at:stamp,completed_at:stamp,usage_coverage:"complete",
    usage:[1,2].map(attempt=>({attempt,config_version:1,tokens_in:attempt*100,tokens_out:attempt*10,tool_calls:0,usage_reports:1,missing_usage:0,recorded_at:stamp}))};
  let writes=0;
  page.on("request",request=>{if(request.url().includes("/apps/agent-studio/api/v1/")&&request.method()!=="GET")writes++;});
  await page.route("**/apps/agent-studio/api/v1/runs**",async route=>{
    const path=new URL(route.request().url()).pathname;
    if(route.request().method()!=="GET")throw new Error("Usage UI attempted a mutation");
    if(path.endsWith("/events"))return route.fulfill({json:{items:[]}});
    return route.fulfill({json:path.endsWith(runID)?{run:fixture,evidence:[]}:{items:[fixture],total:1}});
  });
  await page.goto("/apps/agent-studio/#runs");
  await page.getByRole("button",{name:"Detail",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Usage test",exact:true})).toBeVisible();
  await expect(page.getByText("Token semua percobaan: 300 in / 30 out.",{exact:false})).toBeVisible();
  await expect(page.getByRole("table",{name:"Usage per percobaan",exact:true}).getByRole("row")).toHaveCount(3);
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:1000});
    await page.getByRole("heading",{name:"Usage test",exact:true}).scrollIntoViewIfNeeded();
    const layout=await page.evaluate(()=>({width:window.innerWidth,scrollWidth:document.documentElement.scrollWidth,overflow:[...document.querySelectorAll("main *")].filter(element=>element.getBoundingClientRect().right>window.innerWidth+1).slice(0,12).map(element=>({tag:element.tagName,class:element.className,right:element.getBoundingClientRect().right}))}));
    expect(layout.scrollWidth,JSON.stringify(layout)).toBeLessThanOrEqual(layout.width);
    await page.screenshot({path:`../../docs/elenio-handoff/evidence/agent-usage-contract-${width}.png`,fullPage:false});
    const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();
    expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
  }
  fixture.usage_coverage="partial";fixture.tokens_in=200;fixture.tokens_out=20;
  const unknown=fixture.usage[0];if(unknown){unknown.usage_reports=0;unknown.tokens_in=0;unknown.tokens_out=0;}
  await page.reload();await page.getByRole("button",{name:"Detail",exact:true}).click();
  await expect(page.getByText("Sebagian usage belum tersedia.",{exact:false})).toBeVisible();
  await expect(page.getByRole("table",{name:"Usage per percobaan",exact:true})).toContainText("Belum dilaporkan");
  await page.route("**/apps/agent-studio/api/v1/metrics**",route=>route.fulfill({json:{window_hours:24,by_status:{succeeded:1},by_kind:{sandbox:1},p50_duration_ms:null,p95_duration_ms:null,tokens_in:200,tokens_out:20,tool_denials:0,grounded_rate:null,eval_runs_completed:0,eval_gate_blocked:0,rollbacks:0,active_releases:0,pending_release_requests:0,outbox_pending:0,outbox_dead:0,usage_by_class:{test:{tokens_in:200,tokens_out:20,runs:1,incomplete_runs:1}}}}));
  await page.goto("/apps/agent-studio/#overview");
  await expect(page.getByRole("table",{name:"Usage menurut jenis",exact:true})).toContainText("test");
  await expect(page.getByRole("table",{name:"Usage menurut jenis",exact:true})).toContainText("200 / 20");
  expect(writes).toBe(0);
});
