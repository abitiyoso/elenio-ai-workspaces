import {useEffect, useRef, useState} from "react";
import {Link, useBlocker, useLocation, useNavigate, useParams} from "react-router-dom";
import {Badge, Button, DialogPanel, Notice} from "../../../../packages/ui/primitives";
import {newIdempotencyKey} from "../api/client";
import {ApplicationComposer, ApplicationPreview} from "./ApplicationComposer";
import {ArchitectBrief} from "./Architect";
import {PublisherCandidates} from "./PublisherCandidates";
import {getSolution, parseWorkflowReferences, saveSolution, solutionSession, type Solution, type SolutionDefinition, type SolutionSession} from "./solutionApi";

const stages = [["describe","Describe"],["blueprint","Blueprint"],["build","Configure"],["preview","Preview & Test"],["publish","Publish"]] as const;
const empty: SolutionDefinition = {key:"",name:"",description:"",delivery_mode:"solution",output:"both",workflows:[]};

export function SolutionJourney() {
  const {id="new", stage="describe"}=useParams();
  return <JourneyDocument key={id} id={id} stage={stage}/>;
}
function JourneyDocument({id,stage}:{id:string;stage:string}) {
  const navigate=useNavigate(),location=useLocation();
  const seed=(location.state as {brief?:unknown}|null)?.brief;
  const [session,setSession]=useState<SolutionSession>();
  const [plan,setPlan]=useState<Solution>();
  const [definition,setDefinition]=useState<SolutionDefinition>(()=>({...empty,description:typeof seed==="string"?seed.slice(0,12000):""}));
  const [references,setReferences]=useState("");
  const [error,setError]=useState("");
  const [loading,setLoading]=useState(true),[saving,setSaving]=useState(false);
  const [publisher,setPublisher]=useState(false);
  const requestKey=useRef(newIdempotencyKey());
  const dirty=JSON.stringify(definition)!==JSON.stringify(plan?.definition??empty) || references!==(plan?.definition.workflows.map(w=>`${w.key}@${w.version}`).join("\n")??"");
  const blocker=useBlocker(dirty&&!saving);
  const active=stages.some(([key])=>key===stage)?stage:"describe";
  useEffect(()=>{
    const controller=new AbortController();
    void solutionSession(controller.signal).then(async current=>{
      const item=id==="new"?undefined:await getSolution(current,id,controller.signal);
      if(controller.signal.aborted)return;
      setSession(current);
      if(item){setPlan(item);setDefinition(item.definition);setReferences(item.definition.workflows.map(w=>`${w.key}@${w.version}`).join("\n"));}
    }).catch((e:unknown)=>{if(!controller.signal.aborted)setError(e instanceof Error?e.message:"Solution could not be loaded.");})
      .finally(()=>{if(!controller.signal.aborted)setLoading(false);});
    return ()=>controller.abort();
  },[id]);
  useEffect(()=>{
    if(!dirty)return;
    const warn=(event:BeforeUnloadEvent)=>event.preventDefault();
    window.addEventListener("beforeunload",warn);return ()=>window.removeEventListener("beforeunload",warn);
  },[dirty]);
  async function persist(next?:SolutionDefinition) {
    if(!session)throw new Error("Connect an authorized Solution workspace first.");
    setSaving(true);setError("");
    try{
      const value=next??{...definition,workflows:parseWorkflowReferences(references)};
      const saved=await saveSolution(session,value,{...(plan?{id:plan.id,revision:plan.revision}:{}),requestKey:requestKey.current});
      setPlan(saved);setDefinition(saved.definition);setReferences(saved.definition.workflows.map(w=>`${w.key}@${w.version}`).join("\n"));
      return saved;
    }finally{setSaving(false);}
  }
  async function saveForm() {
    try{const saved=await persist();if(!plan)navigate(`/solutions/${saved.id}/blueprint`,{replace:true});}
    catch(e){setError(e instanceof Error?e.message:"Save failed; your inputs were retained.");}
  }
  if(loading)return <section className="page"><p role="status">Loading Solution context…</p></section>;
  if(!session)return <section className="page"><h1>Connect your Solution workspace</h1><Notice error>{error}</Notice><Link to="/solutions">Back to Solutions</Link></section>;
  const author=session.permissions.includes("solution:author");
  return <section className="page solution-journey">
    <header className="page-heading"><div><Link to="/solutions">All Solutions</Link><p className="eyebrow">SOLUTION JOURNEY</p>
      <h1>{plan?.definition.name??"Create a solution"}</h1><p>{session.workspace.name} · {plan?`Saved revision ${plan.revision}`:"Save your requirements to begin"}</p></div>
      <Badge>{plan?"Draft Solution":"New Solution"}</Badge></header>
    <nav className="solution-stages" aria-label="Solution stages">{stages.map(([key,label],index)=>plan?
      <Link key={key} to={`/solutions/${plan.id}/${key}`} aria-current={active===key?"step":undefined}><span>{index+1}</span>{label}</Link>:
      <span key={key} aria-current={key==="describe"?"step":undefined}><span>{index+1}</span>{label}</span>)}</nav>
    {error&&<Notice error>{error}</Notice>}
    <p className="solution-stage-note">Configure and test this draft before requesting a release review.</p>
    {(!plan||active==="describe"||active==="build")&&<form className="surface solution-build-form" onSubmit={event=>{event.preventDefault();void saveForm();}}>
      <h2>{active==="build"&&plan?"Configure your Solution":"Describe the business outcome"}</h2>
      <fieldset disabled={!author||saving}>
        <label className="e-field">Solution name<input required minLength={2} maxLength={120} value={definition.name} onChange={event=>setDefinition({...definition,name:event.target.value})}/></label>
        <label className="e-field">Solution key<input required pattern="[a-z][a-z0-9\-]{1,62}" disabled={!!plan} value={definition.key} onChange={event=>setDefinition({...definition,key:event.target.value})}/></label>
        <label className="e-field">Business requirement<textarea className="solution-requirement" required rows={6} maxLength={12000} value={definition.description} onChange={event=>setDefinition({...definition,description:event.target.value})}/></label>
        <div className="application-page-row"><label className="e-field">Delivery mode<select value={definition.delivery_mode} onChange={event=>setDefinition({...definition,delivery_mode:event.target.value as SolutionDefinition["delivery_mode"]})}>
          <option value="internal">Internal engineering</option><option value="solution">Customer solution</option><option value="enterprise">Enterprise AI OS</option></select></label>
          <label className="e-field">Output<select value={definition.output} onChange={event=>{const next={...definition,output:event.target.value as SolutionDefinition["output"]};if(next.output==="api")delete next.application;setDefinition(next);}}>
            <option value="both">Business application + API</option><option value="business_ui">Business application</option><option value="api">API service</option></select></label></div>
        {active==="build"&&<><label className="e-field">Workflow versions<textarea rows={4} placeholder="purchase-review@1" value={references} onChange={event=>setReferences(event.target.value)}/></label>
          <ApplicationComposer definition={{...definition,workflows:(()=>{try{return parseWorkflowReferences(references);}catch{return definition.workflows;}})()}} onChange={application=>{const next={...definition};if(application)next.application=application;else delete next.application;setDefinition(next);}}/>
          <h3>Contextual workbenches</h3><p>Use the existing versioned services. Save workflow pins and application pages here before running Preview & Test.</p>
          <div className="dialog-actions">{plan?.definition.workflows.map(pin=><Link className="e-button" key={pin.key} to={`/connected/projects/${pin.key}/${pin.version}`}>{pin.key}@{pin.version}</Link>)}
            <Link className="e-button" to="/capabilities/agents">Agent Studio</Link><Link className="e-button" to="/capabilities/models">Model Hub</Link><Link className="e-button" to="/knowledge">Knowledge Hub</Link><Link className="e-button" to="/connected/integrations">Integration Hub</Link></div></>}
      </fieldset>
      <Button type="submit" variant="primary" disabled={!author||saving}>{saving?"Saving…":plan?"Save Solution":"Save & continue to Blueprint"}</Button>
    </form>}
    {plan&&active==="blueprint"&&<><Notice>Requirements are saved in this Solution. Creating a workflow still requires reviewing the AI proposal, server validation and your explicit confirmation. Publishing remains separate.</Notice>
      <ArchitectBrief brief={definition.description} onBrief={description=>setDefinition({...definition,description})}
        solutionContext={{id:plan.id,revision:plan.revision}} onBuild={()=>navigate(`/solutions/${plan.id}/build`)}
        onBeforeGenerate={async()=>{const current=await solutionSession();if(!current.permissions.includes("solution:author"))throw new Error("Solution authoring permission is required.");if(dirty)await persist();else if((await getSolution(session,plan.id)).revision!==plan.revision)throw new Error("Solution changed. Reload its saved revision before starting a model run.");}}
        onBeforeApply={async()=>{const currentSession=await solutionSession();if(!currentSession.permissions.includes("solution:author"))throw new Error("Solution authoring permission is required.");const current=await getSolution(session,plan.id);if(current.revision!==plan.revision||dirty)throw new Error("Solution changed. Save matching requirements and review the current revision before applying.");}}
        onCreated={async workflow=>{
          try{const saved=await persist({...plan.definition,workflows:[...plan.definition.workflows.filter(w=>w.key!==workflow.key),workflow]});navigate(`/solutions/${saved.id}/build`);}
          catch(e){throw new Error(`Workflow draft ${workflow.key}@${workflow.version} was created but could not be linked. Reuse that draft in Configure. ${e instanceof Error?e.message:""}`,{cause:e});}
        }}/></>}
    {plan&&active==="preview"&&<ApplicationPreview key={`${plan.id}:${plan.revision}`} definition={plan.definition} runtime={{plan,session}}/>}
    {plan&&active==="publish"&&<section className="surface solution-build-form"><h2>Review delivery readiness</h2><p>Capture immutable review candidates from this saved Solution. Candidates do not grant approval or install a customer environment.</p>
      <Button variant="primary" onClick={()=>setPublisher(true)}>Open Publisher candidates</Button></section>}
    {publisher&&plan&&<PublisherCandidates plan={plan} session={session} onClose={()=>setPublisher(false)}/>}
    {blocker.state==="blocked"&&<DialogPanel title="Unsaved Solution changes" description="Save your requirements or configuration before switching stages, or discard the unsaved edits." onClose={()=>blocker.reset()}>
      <Button onClick={()=>blocker.reset()}>Keep editing</Button><Button variant="danger" onClick={()=>{if(plan){setDefinition(plan.definition);setReferences(plan.definition.workflows.map(w=>`${w.key}@${w.version}`).join("\n"));}blocker.proceed();}}>Discard and continue</Button></DialogPanel>}
  </section>;
}
