// Typed calls for the UCW BFF (contracts/BFF_API.md).
import { apiGet, apiRequest, ifMatch, newIdempotencyKey } from "./client";
import type { CaseContext, CaseList, MeContext, PackDetail, PackSummary } from "./types";

export function getMe(signal?: AbortSignal) {
  return apiGet<MeContext>("/me/context", signal);
}

export function listPacks(signal?: AbortSignal) {
  return apiGet<{ items: PackSummary[] }>("/packs", signal);
}

export function getPack(key: string, version: string, signal?: AbortSignal) {
  return apiGet<PackDetail>(`/packs/${encodeURIComponent(key)}/versions/${encodeURIComponent(version)}`, signal);
}

export function listCases(q: { q?: string; status?: string; cursor?: string } = {}, signal?: AbortSignal) {
  const params = new URLSearchParams();
  if (q.q) params.set("q", q.q);
  if (q.status) params.set("status", q.status);
  if (q.cursor) params.set("cursor", q.cursor);
  const qs = params.toString();
  return apiGet<CaseList>(`/cases${qs ? `?${qs}` : ""}`, signal);
}

export function getCase(id: string, signal?: AbortSignal) {
  return apiGet<CaseContext>(`/cases/${encodeURIComponent(id)}`, signal);
}

export async function submitDraft(caseId: string, draftId: string, versionNum: number, note = "") {
  return (await apiRequest<CaseContext>("POST", `/cases/${encodeURIComponent(caseId)}/drafts/${encodeURIComponent(draftId)}/submit`, {
    body: note ? { note } : {},
    headers: { ...ifMatch(versionNum), "Idempotency-Key": newIdempotencyKey() },
  })).data;
}

export async function requestDraftRevision(caseId: string, draftId: string, versionNum: number, feedback: string) {
  return (await apiRequest<CaseContext>("POST", `/cases/${encodeURIComponent(caseId)}/drafts/${encodeURIComponent(draftId)}/revision`, {
    body: { feedback },
    headers: { ...ifMatch(versionNum), "Idempotency-Key": newIdempotencyKey() },
  })).data;
}
