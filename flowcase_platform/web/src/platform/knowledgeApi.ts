// CP mints an audience-bound KMS token at its existing proxy. This client sends only its cookie.
export async function knowledgeRequest<T>(path:string,init:RequestInit={}):Promise<T>{
  if(!/^\/(?:session|collections|documents(?:\/[a-zA-Z0-9-]+\/versions)?|ops\/status|search|ingestion\/jobs\/[a-zA-Z0-9-]+(?:\/retry)?)$/.test(path))throw new Error("Unsupported Knowledge route.");
  const headers=new Headers();headers.set("Accept","application/json");
  if(typeof init.body==="string")headers.set("Content-Type","application/json");
  const response=await fetch(`/apps/kms/api/v1${path}`,{...init,headers,credentials:"same-origin",cache:"no-store"});
  const body=await response.json().catch(()=>null);
  if(!response.ok)throw new Error(typeof body?.error?.message==="string"?body.error.message:typeof body?.error==="string"?body.error:typeof body?.message==="string"?body.message:`Knowledge request failed (${response.status}).`);
  return body as T;
}
