import {
  Activity, BookOpen, Boxes, FlaskConical, FolderOpen, GitBranch,
  Grid2X2, Home, Layers, Plug, Rocket, Settings, Sparkles, Cpu,
  type LucideIcon,
} from "lucide-react";

export interface WorkspaceMenuItem {
  id: string;
  name: string;
  path: string;
  icon: LucideIcon;
}
export interface WorkspaceMenuGroup { name: string; items: WorkspaceMenuItem[] }

/** V10 NAV, including its additive V9 entries. Solutions is the owner's Publisher addendum. */
export function workspaceNavigation(connected: boolean): WorkspaceMenuGroup[] {
  return [
    {name:"WORKSPACE",items:[
      {id:"home",name:"Home",path:"/",icon:Home},
      {id:"architect",name:"AI Architect",path:"/architect",icon:Sparkles},
      {id:"projects",name:"Projects",path:connected?"/connected":"/projects",icon:FolderOpen},
      {id:"apps",name:"Business Apps",path:"/business-apps",icon:Grid2X2},
    ]},
    {name:"BUILD",items:[
      {id:"studio",name:"Workflow Studio",path:connected?"/connected?view=studio":"/projects/vendor-payment/workflow",icon:GitBranch},
      {id:"agents",name:"Agent Studio",path:"/capabilities/agents",icon:Boxes},
    ]},
    {name:"CAPABILITIES",items:[
      {id:"templates",name:"Template Repository",path:"/connected?create=template",icon:Layers},
      {id:"knowledge",name:"Knowledge Hub",path:"/knowledge",icon:BookOpen},
      {id:"integrations",name:"Integration Hub",path:"/connected/integrations",icon:Plug},
      {id:"skills",name:"Skills & Tools",path:"/capabilities/skills",icon:Layers},
      {id:"models",name:"Model Hub",path:"/capabilities/models",icon:Cpu},
    ]},
    {name:"DELIVER & OPERATE",items:[
      {id:"testing",name:"Testing Studio",path:"/capabilities/testing",icon:FlaskConical},
      {id:"evaluation",name:"Evaluation & Releases",path:"/capabilities/evaluation",icon:FlaskConical},
      {id:"economics",name:"AI Economics",path:"/economics",icon:Activity},
      {id:"deploy",name:"Deployments",path:"/deployments",icon:Rocket},
      {id:"operations",name:"Operations",path:"/connected/operations",icon:Activity},
      {id:"solutions",name:"Solutions",path:"/solutions",icon:FolderOpen},
    ]},
    {name:"PLATFORM",items:[
      {id:"settings",name:"Control Center",path:"/control-center",icon:Settings},
    ]},
  ];
}

export function isWorkspaceMenuActive(item: WorkspaceMenuItem, pathname: string, search: string) {
  if(item.id==="templates")return pathname==="/connected"&&new URLSearchParams(search).get("create")==="template";
  if(item.id==="studio")return pathname.startsWith("/connected/projects/")||pathname.endsWith("/workflow")||(pathname==="/connected"&&new URLSearchParams(search).get("view")==="studio");
  if(item.id==="projects")return (pathname==="/connected"&&new URLSearchParams(search).get("create")!=="template"&&new URLSearchParams(search).get("view")!=="studio")||((pathname==="/projects"||pathname.startsWith("/projects/"))&&!pathname.endsWith("/workflow"));
  if(item.id==="operations")return pathname==="/connected/operations"||pathname.startsWith("/connected/cases/");
  if(item.id==="evaluation")return ["/capabilities/evaluation","/capabilities/evaluations","/capabilities/releases"].includes(pathname);
  return pathname===item.path;
}
