import {useEffect, useState} from "react";
import {Badge, Button, DialogPanel, Notice} from "../../../../packages/ui/primitives";
import {listSolutionRevisions, type Solution, type SolutionRevision, type SolutionSession} from "./solutionApi";

export function SolutionHistory({plan,session,onClose}:{plan:Solution;session:SolutionSession;onClose:()=>void}) {
  const [items,setItems]=useState<SolutionRevision[]>([]);
  const [before,setBefore]=useState(0);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");
  useEffect(()=>{
    const controller=new AbortController();
    void listSolutionRevisions(session,plan,0,controller.signal).then(page=>{
      if(!controller.signal.aborted){setItems(page.items);setBefore(page.next_before);}
    }).catch((e:unknown)=>{if(!controller.signal.aborted)setError(e instanceof Error?e.message:"Version history could not be loaded.");})
      .finally(()=>{if(!controller.signal.aborted)setLoading(false);});
    return ()=>controller.abort();
  },[plan,session]);
  async function load(cursor:number) {
    setLoading(true);setError("");
    try{const page=await listSolutionRevisions(session,plan,cursor);setItems(previous=>cursor?[...previous,...page.items]:page.items);setBefore(page.next_before);}
    catch(e){setError(e instanceof Error?e.message:"History could not be loaded.");}
    finally{setLoading(false);}
  }
  return <DialogPanel title="Solution version history" description="Saved requirements, workflow pins and application configuration. These records are read-only; release approval remains separate." onClose={onClose} wide>
    {loading&&<p role="status">Loading saved revisions…</p>}
    {error&&<Notice error>{error}<Button disabled={loading} onClick={()=>void load(items.length?before:0)}>Retry history</Button></Notice>}
    {!loading&&!error&&!items.length&&<p>No retained revisions are available.</p>}
    {items.some(item=>item.origin==="migration_current")&&<Notice>History begins with the current revision observed during upgrade. Earlier overwritten revisions and their editors cannot be reconstructed.</Notice>}
    {items.map(item=><details className="surface solution-revision" key={item.plan.revision}>
      <summary>Revision {item.plan.revision}{item.plan.revision===plan.revision?" · current":""} · {item.plan.definition.name}</summary>
      <p>{new Date(item.recorded_at).toLocaleString()} · {item.origin==="write"?"Saved edit":"Imported current revision"}</p>
      <Badge>{item.plan.definition.delivery_mode} · {item.plan.definition.output}</Badge>
      <h3>Business requirement</h3><p className="solution-revision-requirement">{item.plan.definition.description||"No requirement recorded."}</p>
      <h3>Pinned workflows</h3>{item.plan.definition.workflows.length?<ul>{item.plan.definition.workflows.map(pin=><li key={pin.key}>{pin.key}@{pin.version}</li>)}</ul>:<p>No workflows linked.</p>}
      {item.plan.definition.application&&<><h3>{item.plan.definition.application.title}</h3><ul>{item.plan.definition.application.pages.map(page=><li key={page.key}>{page.title} · {page.kind}{page.workflow_key?` · ${page.workflow_key}`:""}</li>)}</ul></>}
      <p className="solution-revision-hash">Snapshot SHA-256: {item.content_hash}</p>
    </details>)}
    {before>0&&!error&&<Button disabled={loading} onClick={()=>void load(before)}>Load older revisions</Button>}
  </DialogPanel>;
}
