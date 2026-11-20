import {test,expect} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("every HTML menu has a named entry and protected pages explain standalone access",async({page})=>{
  await page.goto("/elenio.html#/");
  const nav=page.getByRole("navigation",{name:"Main workspace"});
  for(const name of ["Home","AI Architect","Projects","Business Apps","Workflow Studio","Agent Studio","Template Repository","Knowledge Hub","Integration Hub","Skills & Tools","Model Hub","Testing Studio","Evaluation & Releases","AI Economics","Deployments","Operations","Control Center","Solutions"]){
    await expect(nav.getByRole("link",{name,exact:true})).toHaveCount(1);
  }
  await nav.getByRole("link",{name:"Business Apps",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Business Apps",level:1})).toBeVisible();
  await expect(page.getByText("Launch is unavailable in standalone mode.",{exact:false})).toBeVisible();
  await nav.getByRole("link",{name:"Deployments",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Deployments",level:1})).toBeVisible();
  await expect(page.getByRole("link",{name:"Review solution candidates"})).toHaveAttribute("href","#/solutions");
  await nav.getByRole("link",{name:"Control Center",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Control Center",level:1})).toBeVisible();
  const axe=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21aa","wcag22aa"]).analyze();
  expect(axe.violations.filter(v=>v.impact==="serious"||v.impact==="critical")).toEqual([]);
});
