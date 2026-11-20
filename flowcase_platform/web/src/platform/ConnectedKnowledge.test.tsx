import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,render,screen,waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {ConnectedKnowledge} from "./ConnectedKnowledge";
const json=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status});
let requests:{url:string;init:RequestInit|undefined}[]=[];
beforeEach(()=>{
  requests=[];
  vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{
    requests.push({url,init});
    if(url==="/api/v1/apps/kms/launch")return json({app_key:"kms",route_base:"/apps/kms/",integration:"proxy"});
    if(url.endsWith("/session"))return json({user_id:"steward",tenant_id:"test-tenant",roles:["knowledge_steward"]});
    if(url.endsWith("/collections"))return json({collections:[{id:"collection-1",name:"Engineering policies"}]});
    if(url.endsWith("/documents"))return json({documents:[{id:"document-1",title:"Actual source.pdf",collection_id:"collection-1",lifecycle_status:"pending_review",created_at:"2026-11-20T00:00:00Z",version_number:2}]});
    if(url.endsWith("/ops/status"))return json({dependencies:{object_storage:"ok",docling:"ok",elasticsearch:"ok"},embedding:{enabled:false},ingestion:{queued:0,processing:0,failed:0,completed:1}});
    if(url.endsWith("/versions"))return json({versions:[{id:"version-2",version_number:2,parser_status:"success",review_status:"pending",trust_status:"unverified",publication_status:"draft"}]});
    if(url.endsWith("/search"))return json({results:[{chunk_id:"chunk-1",document_id:"document-1",version_id:"version-2",title:"Approved evidence",snippet:"A source excerpt returned by KMS.",citation:"Policy · page 4",page_start:4,trust_status:"approved"}]});
    throw new Error(`Unexpected route ${url}`);
  }));
});
afterEach(()=>{cleanup();vi.unstubAllGlobals();});
it("keeps actual lifecycle and version metadata and delegates PDF access to KMS",async()=>{
  const user=userEvent.setup();render(<ConnectedKnowledge/>);
  await screen.findByText("Verified KMS session");
  expect(screen.getByRole("cell",{name:"pending review"})).toBeVisible();
  expect(screen.queryByText("Finance approval policy.pdf")).toBeNull();
  expect(screen.getByText("Not provided",{exact:true})).toBeVisible();
  await user.click(screen.getByRole("button",{name:"Preview Actual source.pdf"}));
  expect(await screen.findByRole("link",{name:"Open source PDF"})).toHaveAttribute("href","/apps/kms/api/v1/documents/document-1/versions/version-2/preview");
  expect(screen.getByText(/Trust: unverified/)).toBeVisible();
  expect(screen.queryByText(/Illustrative citation/)).toBeNull();
});
it("renders server excerpts and citations without silently substituting a search mode",async()=>{
  const user=userEvent.setup();render(<ConnectedKnowledge/>);await screen.findByText("Verified KMS session");
  await user.click(screen.getByRole("button",{name:"Retrieval playground"}));
  await user.type(screen.getByLabelText("Search evidence"),"approval");
  await user.click(screen.getByRole("button",{name:"Search sources"}));
  expect(await screen.findByText("Policy · page 4")).toBeVisible();
  const request=requests.find(r=>r.url.endsWith("/search"));expect(JSON.parse(String(request?.init?.body))).toEqual({query:"approval",mode:"keyword"});
  await user.selectOptions(screen.getByLabelText("Search mode"),"semantic");
  expect(screen.getByRole("button",{name:"Search sources"})).toBeDisabled();
  expect(screen.getByText("Semantic and hybrid search require a configured embedding provider.")).toBeVisible();
  expect(screen.queryByText("Policy · page 4")).toBeNull();
});
it("clears live content on denied access and never presents fixture records",async()=>{
  vi.stubGlobal("fetch",vi.fn().mockResolvedValue(json({error:{message:"Workspace access denied."}},403)));
  render(<ConnectedKnowledge/>);
  await waitFor(()=>expect(screen.getByText("Workspace access denied.")).toBeVisible());
  expect(screen.queryByText("Verified KMS session")).toBeNull();expect(screen.queryByRole("table")).toBeNull();
});
