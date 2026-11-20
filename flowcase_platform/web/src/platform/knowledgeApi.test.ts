import {afterEach,describe,expect,it,vi} from "vitest";
import {knowledgeRequest} from "./knowledgeApi";
afterEach(()=>vi.unstubAllGlobals());
describe("Knowledge service boundary",()=>{
  it("uses the fixed CP proxy without caller tokens or tenant/role headers",async()=>{
    const request=vi.fn().mockResolvedValue(new Response(JSON.stringify({collections:[]}),{status:200}));vi.stubGlobal("fetch",request);
    await knowledgeRequest("/collections",{headers:{Authorization:"Bearer flowcase-only","X-Tenant-ID":"forged","X-Roles":"platform_admin"}});
    const call=request.mock.calls[0];if(!call)throw new Error("Expected a Knowledge request");const [url,options]=call;
    expect(url).toBe("/apps/kms/api/v1/collections");expect(options.credentials).toBe("same-origin");expect([...options.headers.keys()]).toEqual(["accept"]);
  });
  it("lets the browser set the multipart boundary for PDF ingestion",async()=>{
    const request=vi.fn().mockResolvedValue(new Response("{}",{status:202}));vi.stubGlobal("fetch",request);
    const body=new FormData();body.set("file",new File(["%PDF-1.4"],"synthetic.pdf",{type:"application/pdf"}));
    await knowledgeRequest("/documents",{method:"POST",body});
    const call=request.mock.calls[0];if(!call)throw new Error("Expected an upload request");
    expect(call[1].headers.has("Content-Type")).toBe(false);expect(call[1].body).toBe(body);
  });
  it.each(["https://other.invalid/collections","//other.invalid/session","/documents/../../session","/collections?tenant=forged"])("rejects an unapproved route %s",async path=>{
    const request=vi.fn();vi.stubGlobal("fetch",request);await expect(knowledgeRequest(path)).rejects.toThrow("Unsupported Knowledge route");expect(request).not.toHaveBeenCalled();
  });
  it("preserves server authorization errors instead of replacing them with fixture data",async()=>{
    vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({error:{message:"Collection access denied."}}),{status:403})));
    await expect(knowledgeRequest("/collections")).rejects.toThrow("Collection access denied.");
  });
});
