import {describe,expect,it} from "vitest";
import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {workspaceNavigation,isWorkspaceMenuActive} from "./workspaceNavigation";

describe("development-kit navigation contract",()=>{
  it("contains every exact V10 menu, including its additive V9 entries",()=>{
    const html=readFileSync(resolve(process.cwd(),"../../docs/elenio-handoff/references/Elenio_AI_Workspaces_V10_Precision.html"),"utf8");
    const start=html.indexOf("const NAV="),end=html.indexOf(";",start);
    expect(start).toBeGreaterThan(0);
    const literals=html.slice(start,end)+[...html.matchAll(/NAV\[[0-9]+\]\[1\]\.(?:push|splice)\([^;]+;/g)].map(match=>match[0]).join("");
    const reference=[...literals.matchAll(/\['([^']+)','([^']+)','[^']+'\]/g)].map(match=>match[2]);
    expect(reference).toHaveLength(17);
    const menus=workspaceNavigation(true).flatMap(group=>group.items).filter(item=>item.id!=="solutions");
    expect(menus.map(item=>item.name).sort()).toEqual(reference.sort());
    expect(workspaceNavigation(true).map(group=>group.name)).toEqual(["WORKSPACE","BUILD","CAPABILITIES","DELIVER & OPERATE","PLATFORM"]);
  });
  it("keeps template creation, workflow editing and operation context distinct",()=>{
    const items=workspaceNavigation(true).flatMap(group=>group.items);
    const active=(path:string,search="")=>items.filter(item=>isWorkspaceMenuActive(item,path,search)).map(item=>item.id);
    expect(active("/connected")).toEqual(["projects"]);
    expect(active("/connected","?view=studio")).toEqual(["studio"]);
    expect(active("/connected","?create=template")).toEqual(["templates"]);
    expect(active("/connected/projects/review/1")).toEqual(["studio"]);
    expect(active("/connected/cases/case-1")).toEqual(["operations"]);
    expect(active("/capabilities/releases")).toEqual(["evaluation"]);
    expect(active("/projectshack")).toEqual([]);
  });
});
