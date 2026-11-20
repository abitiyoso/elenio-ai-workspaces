import {useEffect,useState} from "react";
import {Link} from "react-router-dom";
import {Badge,Notice} from "../../../../packages/ui/primitives";
import {basePath} from "../api/client";
import {AppLauncher} from "./ConnectedCatalog";
import {controlPlaneGet} from "./controlPlaneApps";

export function BusinessApps(){
  return <section className="solutions-page">
    <header className="page-heading"><div><p className="eyebrow">BUSINESS WORKSPACE</p><h1>Business Apps</h1><p>Open the business applications available to your current workspace.</p></div></header>
    <AppLauncher categories={["business","experience"]} title="Available applications"/>
    <div className="surface catalog-section"><h2>Cases & reviews</h2><p>Continue cases, human tasks and independent approvals in the existing operations workspace.</p><Link className="e-button" to="/connected/operations">Open operations</Link></div>
  </section>;
}

export function Deployments(){
  return <section className="solutions-page">
    <header className="page-heading"><div><p className="eyebrow">DELIVER & OPERATE</p><h1>Deployments</h1><p>Inspect registered runtimes and prepare your solution for a governed release.</p></div><Link className="e-button primary" to="/solutions">Review solution candidates</Link></header>
    <Notice>Installation requires an approved release and a configured target. A review candidate is not an installed application.</Notice>
    <AppLauncher title="Registered capability runtimes"/>
  </section>;
}

interface ControlContext {
  user:{display_name:string};workspace:{name:string}|null;
  capabilities:{view_people:boolean;manage_workspaces:boolean;manage_registry:boolean;view_integrations:boolean;view_audit:boolean;approve_grants:boolean};
}
export function ControlCenter(){
  const [context,setContext]=useState<ControlContext|null>(null),[error,setError]=useState("");
  const hosted=basePath==="/apps/flowcase";
  useEffect(()=>{
    if(!hosted)return;
    const controller=new AbortController();
    controlPlaneGet<ControlContext>("/api/v1/me",controller.signal).then(data=>{
      if(controller.signal.aborted)return;
      if(typeof data?.user?.display_name!=="string"||!data.capabilities){setError("Invalid workspace settings context.");return;}
      setContext(data);
    },e=>{if(!controller.signal.aborted)setError(e instanceof Error?e.message:"Settings unavailable.");});
    return()=>controller.abort();
  },[hosted]);
  return <section className="solutions-page">
    <header className="page-heading"><div><p className="eyebrow">PLATFORM</p><h1>Control Center</h1><p>Manage workspace access, approved capabilities, environments and audit in the existing Control Plane.</p></div></header>
    {!hosted?<Notice>Open this application through your Control Plane workspace to manage settings.</Notice>:error?<Notice error>{error}</Notice>:!context?<p role="status">Checking workspace settings…</p>:<>
      <div className="solutions-context"><Badge tone="blue">{context.workspace?.name??"Select workspace"}</Badge><span>{context.user.display_name}</span></div>
      <div className="solutions-list">
        <article className="surface control-setting"><div><h2>Workspace & Members</h2><p>People, memberships and workspace boundaries.</p></div><div>{context.capabilities.view_people&&<a className="e-button" href="/people">Manage people</a>}<a className="e-button" href="/workspaces">Workspace settings</a></div></article>
        <article className="surface control-setting"><div><h2>Models & Providers</h2><p>Approved endpoints, model limits and routing.</p></div><Link className="e-button" to="/capabilities/models">Open Model Hub</Link></article>
        <article className="surface control-setting"><div><h2>Security Policies</h2><p>Access requests and independent grant decisions.</p></div><a className="e-button" href="/access">Review access</a></article>
        {context.capabilities.view_integrations&&<article className="surface control-setting"><div><h2>Environments</h2><p>Registered workspace services and environment readiness.</p></div><a className="e-button" href="/integrations">Inspect environments</a></article>}
        {context.capabilities.view_audit&&<article className="surface control-setting"><div><h2>Audit & Retention</h2><p>Inspect recorded platform actions and access decisions.</p></div><a className="e-button" href="/audit">Open audit</a></article>}
      </div>
      <Notice>Control Plane and each capability service check their own permissions. Opening settings does not grant release or approval authority.</Notice>
    </>}
  </section>;
}
