import {useCallback,useEffect,useState,type FormEvent} from "react";
import {Button,DialogPanel,Notice,Badge} from "../../../../packages/ui/primitives";
import {Knowledge,type KnowledgeRecord} from "./Knowledge";
import {controlPlaneGet,safeAppRoute} from "./controlPlaneApps";
import {knowledgeRequest} from "./knowledgeApi";

interface Identity {user_id:string;tenant_id:string;roles:string[]}
interface Collection {id:string;name:string;description:string;classification:string}
interface DocumentRecord {id:string;title:string;collection_id:string;lifecycle_status:string;version_number?:number;created_at:string;job_id?:string;job_status?:string;job_stage?:string;last_error?:string}
interface Operations {dependencies:Record<string,string>;embedding:{enabled:boolean};ingestion:{queued:number;processing:number;failed:number;completed:number}}
interface Version {id:string;version_number:number;review_status:string;trust_status:string;publication_status:string;parser_status:string}
interface SearchResult {chunk_id:string;document_id:string;version_id:string;title?:string;snippet?:string;citation?:string;source_anchor?:string;page_start?:number;page_end?:number;trust_status?:string;matched_reason?:string}
interface Snapshot {identity:Identity;collections:Collection[];documents:DocumentRecord[];ops:Operations|null;opsError:string}
const kmsBase="/apps/kms";
const errorText=(error:unknown)=>error instanceof Error?error.message:"Knowledge request failed.";
const label=(value:string|undefined)=>value?.replaceAll("_"," ")||"Not provided";

export function ConnectedKnowledge(){
  const [snapshot,setSnapshot]=useState<Snapshot|null>(null),[error,setError]=useState(""),[loading,setLoading]=useState(true),[revision,setRevision]=useState(0);
  const [form,setForm]=useState<"collection"|"upload"|null>(null),[busy,setBusy]=useState(false),[formError,setFormError]=useState(""),[notice,setNotice]=useState("");
  useEffect(()=>{
    const controller=new AbortController(),signal=controller.signal;
    (async()=>{
      const launch=await controlPlaneGet<{app_key:string;route_base:string;integration:string}>("/api/v1/apps/kms/launch",signal);
      if(launch.app_key!=="kms"||!safeAppRoute("kms",launch.route_base)||launch.integration!=="proxy")throw new Error("No verified Knowledge workspace is available.");
      const identity=await knowledgeRequest<Identity>("/session",{signal});
      if(!identity.user_id||!identity.tenant_id||!Array.isArray(identity.roles))throw new Error("Invalid Knowledge session.");
      const [collections,documents,operations]=await Promise.all([
        knowledgeRequest<{collections:Collection[]}>("/collections",{signal}),
        knowledgeRequest<{documents:DocumentRecord[]}>("/documents",{signal}),
        knowledgeRequest<Operations>("/ops/status",{signal}).then(ops=>({ops,opsError:""}),e=>({ops:null,opsError:errorText(e)})),
      ]);
      if(!Array.isArray(collections.collections)||!Array.isArray(documents.documents))throw new Error("Invalid Knowledge library response.");
      if(signal.aborted)return;
      setSnapshot({identity,collections:collections.collections,documents:documents.documents,...operations});setError("");
    })().catch(e=>{if(!signal.aborted){setSnapshot(null);setError(errorText(e));}}).finally(()=>{if(!signal.aborted)setLoading(false);});
    return()=>controller.abort();
  },[revision]);
  const reload=useCallback(()=>{setLoading(true);setRevision(v=>v+1);},[]);
  if(!snapshot)return <section className="page knowledge-page"><header className="page-heading"><div><span className="eyebrow">CAPABILITY LIBRARY</span><h1>Knowledge Hub</h1><p>Trusted sources. Clear provenance. Better decisions.</p></div></header>{loading?<p role="status">Checking Knowledge access and loading your library…</p>:<Notice error>{error}<Button onClick={reload}>Retry Knowledge connection</Button></Notice>}</section>;
  const {identity,collections,documents,ops}=snapshot;
  const canCreate=identity.roles.includes("platform_admin")||identity.roles.includes("knowledge_steward");
  const ingestReason=!ops?"Dependency status is unavailable.":ops.dependencies.object_storage!=="ok"?"Object storage is unavailable.":ops.dependencies.docling!=="ok"?"PDF processing is not configured or unavailable.":"";
  const records:KnowledgeRecord[]=documents.map(d=>({id:d.id,name:d.title,collection:d.collection_id,status:label(d.lifecycle_status),...(d.version_number===undefined?{}:{version:String(d.version_number)}),updated:new Date(d.created_at).toLocaleDateString("en-GB")}));
  async function submit(event:FormEvent<HTMLFormElement>){
    event.preventDefault();if(busy)return;setBusy(true);setFormError("");
    const fields=new FormData(event.currentTarget);
    try{
      if(form==="collection")await knowledgeRequest("/collections",{method:"POST",body:JSON.stringify({name:String(fields.get("name")??"").trim(),description:String(fields.get("description")??"").trim(),classification:fields.get("classification")})});
      else await knowledgeRequest("/documents",{method:"POST",body:fields});
      setNotice(form==="collection"?"Collection created by KMS.":"PDF accepted by KMS. Track its processing in Ingestion jobs.");setForm(null);reload();
    }catch(e){setFormError(errorText(e));}finally{setBusy(false);}
  }
  function openForm(value:"collection"|"upload"){setFormError("");setForm(value);}
  return <>
    <Knowledge connection={{documents:records,collections:collections.map(c=>({id:c.id,name:c.name,count:documents.filter(d=>d.collection_id===c.id).length})),
      actions:<div className="knowledge-actions"><Button disabled={loading} onClick={reload}>{loading?"Refreshing…":"Refresh library"}</Button><Button disabled={!canCreate} title={!canCreate?"Requires a KMS knowledge steward or platform administrator.":undefined} onClick={()=>openForm("collection")}>New collection</Button><div><Button variant="primary" disabled={!!ingestReason||!collections.length} title={ingestReason||(!collections.length?"Create an authorized collection first.":undefined)} onClick={()=>openForm("upload")}>Ingest PDF</Button>{ingestReason&&<small className="disabled-reason">{ingestReason}</small>}</div></div>,
      notice:<>{notice&&<p role="status">{notice}</p>}{error&&<Notice error>{error}</Notice>}{snapshot.opsError&&<Notice>Library metadata is available. Processing status could not be checked: {snapshot.opsError}</Notice>}<div className="knowledge-live-summary"><Badge tone="green">Verified KMS session</Badge><span>{documents.length} documents · {collections.length} collections</span><a href="/apps/kms/">Open governance workspace</a></div></>,
      renderTab:tab=>tab==="Ingestion jobs"?<KnowledgeJobs documents={documents} ops={ops} refresh={reload}/>:<KnowledgeSearch ops={ops}/>,
      renderDocument:document=><KnowledgeVersions key={document.id} document={document}/>,
    }}/>
    {form&&<DialogPanel title={form==="collection"?"New collection":"Ingest PDF"} description={form==="collection"?"Create an access-controlled collection in KMS.":"Upload a PDF into an authorized collection. KMS enforces document access."} onClose={()=>{if(!busy)setForm(null);}}>
      <form className="knowledge-form" onSubmit={event=>void submit(event)}>
        {form==="collection"?<><label>Collection name<input name="name" required maxLength={200}/></label><label>Description<textarea name="description" maxLength={2000}/></label><label>Classification<select name="classification"><option value="internal">Internal</option><option value="confidential">Confidential</option><option value="restricted">Restricted</option></select></label></>:<><label>Document title<input name="title" required maxLength={200}/></label><label>Collection<select name="collection_id" required>{collections.map(c=><option value={c.id} key={c.id}>{c.name}</option>)}</select></label><label>Case ID (optional)<input name="case_id"/></label><label>PDF file<input name="file" type="file" accept="application/pdf,.pdf" required/></label></>}
        {formError&&<Notice error>{formError}</Notice>}<Button variant="primary" type="submit" disabled={busy}>{busy?"Saving…":form==="collection"?"Create collection":"Upload PDF"}</Button>
      </form>
    </DialogPanel>}
  </>;
}

function KnowledgeJobs({documents,ops,refresh}:{documents:DocumentRecord[];ops:Operations|null;refresh:()=>void}){
  const [busy,setBusy]=useState(""),[error,setError]=useState("");
  const jobs=documents.filter(d=>d.job_id);
  async function retry(document:DocumentRecord){setBusy(document.id);setError("");try{await knowledgeRequest(`/ingestion/jobs/${document.job_id}/retry`,{method:"POST"});refresh();}catch(e){setError(errorText(e));}finally{setBusy("");}}
  return <section className="surface catalog-section"><header className="section-title"><div><h2>Ingestion jobs</h2><p>Latest job for each document you can access. Retry permissions are checked by KMS.</p></div><Button onClick={refresh}>Refresh jobs</Button></header>{ops&&<p>Tenant queue · {ops.ingestion.queued} queued · {ops.ingestion.processing} processing · {ops.ingestion.failed} failed · {ops.ingestion.completed} completed</p>}{!jobs.length?<div className="e-empty"><h3>No ingestion jobs</h3><p>Accepted PDFs appear here with their actual processing status.</p></div>:<div className="knowledge-job-list">{jobs.map(d=><article key={d.id}><div><h3>{d.title}</h3><p>{label(d.job_status)} · {label(d.job_stage)}</p>{d.last_error&&<p>{d.last_error}</p>}</div>{d.job_status==="failed"&&<Button disabled={!!busy} onClick={()=>void retry(d)}>{busy===d.id?"Retrying…":"Retry ingestion"}</Button>}</article>)}</div>}{error&&<Notice error>{error}</Notice>}</section>;
}

function KnowledgeSearch({ops}:{ops:Operations|null}){
  const [query,setQuery]=useState(""),[mode,setMode]=useState("keyword"),[results,setResults]=useState<SearchResult[]|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState("");
  const unavailable=!ops?"Search dependency status is unavailable.":ops.dependencies.elasticsearch!=="ok"?"The search index is unavailable.":mode!=="keyword"&&!ops.embedding.enabled?"Semantic and hybrid search require a configured embedding provider.":"";
  async function search(event:FormEvent){event.preventDefault();setBusy(true);setResults(null);setError("");try{const data=await knowledgeRequest<{results:SearchResult[]}>("/search",{method:"POST",body:JSON.stringify({query:query.trim(),mode})});if(!Array.isArray(data.results))throw new Error("Invalid search response.");setResults(data.results);}catch(e){setError(errorText(e));}finally{setBusy(false);}}
  return <section className="surface catalog-section"><h2>Retrieval playground</h2><p>Search published evidence with server-enforced tenant, collection, case and trust checks. Search mode is never substituted automatically.</p><form className="knowledge-search-form" onSubmit={event=>void search(event)}><label>Search evidence<input value={query} onChange={e=>setQuery(e.target.value)} required/></label><label>Search mode<select value={mode} onChange={e=>{setMode(e.target.value);setResults(null);setError("");}}><option value="keyword">Keyword</option><option value="semantic">Semantic</option><option value="hybrid">Hybrid</option></select></label><Button variant="primary" type="submit" disabled={busy||!query.trim()||!!unavailable}>{busy?"Searching…":"Search sources"}</Button></form>{unavailable&&<Notice>{unavailable}</Notice>}{error&&<Notice error>{error}</Notice>}{results?.length===0&&<p role="status">No authorized evidence matched your query.</p>}<div className="knowledge-results">{results?.map((r,i)=><article key={r.chunk_id||i}><h3>{r.title??"Source document"}</h3>{r.trust_status&&<Badge>{label(r.trust_status)}</Badge>}<p>{r.snippet}</p>{r.citation&&<p className="knowledge-citation">{r.citation}</p>}{r.page_start!==undefined&&<small>Page {r.page_start}{r.page_end&&r.page_end!==r.page_start?`–${r.page_end}`:""}</small>}{r.source_anchor&&<p>Source anchor: {r.source_anchor}</p>}<small>{r.matched_reason}</small></article>)}</div></section>;
}

function KnowledgeVersions({document}:{document:KnowledgeRecord}){
  const [versions,setVersions]=useState<Version[]|null>(null),[error,setError]=useState("");
  useEffect(()=>{const controller=new AbortController();knowledgeRequest<{versions:Version[]}>(`/documents/${document.id}/versions`,{signal:controller.signal}).then(data=>{if(!controller.signal.aborted){if(!Array.isArray(data.versions))setError("Invalid version history.");else setVersions(data.versions);}},e=>{if(!controller.signal.aborted)setError(errorText(e));});return()=>controller.abort();},[document.id]);
  return <>{error?<Notice error>{error}</Notice>:versions===null?<p role="status">Loading authorized versions…</p>:!versions.length?<p>No versions are visible to this session.</p>:versions.map(v=><article className="knowledge-version" key={v.id}><h3>Version {v.version_number}</h3><p>Review: {label(v.review_status)} · Trust: {label(v.trust_status)} · Publication: {label(v.publication_status)}</p>{v.parser_status==="success"?<a className="e-button" href={`${kmsBase}/api/v1/documents/${encodeURIComponent(document.id)}/versions/${encodeURIComponent(v.id)}/preview`} target="_blank" rel="noopener noreferrer">Open source PDF</a>:<p>Source preview is not ready for this version.</p>}</article>)}<Notice>Review, publication, revocation, expiry and ACL changes remain in the existing independently governed KMS workspace.</Notice><a className="e-button" href="/apps/kms/">Open governance workspace</a></>;
}
