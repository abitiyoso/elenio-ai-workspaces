// Typed client for the UCW-owned intake chat (contracts/BFF_API.md, "Intake chat").
import { apiRequest, ifMatch, newIdempotencyKey } from "./client";
import type { CitedSource } from "./types";

export type KBStatus = "covered" | "partial" | "not_covered" | "unknown";

export type IntakeWidget = "text" | "textarea" | "date" | "list";

export interface IntakeField {
  key: string;
  label: string;
  widget: IntakeWidget | string;
  required?: boolean;
  help?: string;
}

export type IntakeDraft = Record<string, string | string[] | undefined>;

export interface IntakeKBItem {
  name: string;
  kind?: string;
  evidence_refs?: string[];
}

/** An uploaded document (its extracted text lives in the brief's document field, never in this view). */
export interface IntakeDocument {
  id: string;
  filename: string;
  media_type: string;
  size_bytes: number;
  status: "processing" | "ready" | "failed";
  chars: number | null;
  error: string | null;
  created_at?: string;
}

/** Present when the pack accepts documents in the intake chat. */
export interface IntakeUploads {
  max_files: number;
  max_bytes: number;
  accept: string[];
  truncated: boolean;
}

export interface IntakeMessage {
  role: "user" | "assistant";
  content: string;
  /** Set on the user message sent for an uploaded document. */
  document?: { id: string; filename: string };
  at?: string;
  updated_fields?: string[];
  kb_items?: IntakeKBItem[];
  quick_replies?: string[];
  sources?: CitedSource[];
  unresolved?: { topic: string; reason?: string }[];
}

export interface IntakeSession {
  id: string;
  pack_key: string;
  pack_version?: string;
  workflow_key?: string;
  status: "open" | "created" | "abandoned";
  fields: IntakeField[];
  greeting?: string;
  starters?: string[];
  draft: IntakeDraft;
  messages: IntakeMessage[];
  kb_status: KBStatus;
  kb_items: IntakeKBItem[];
  sources: CitedSource[];
  documents?: IntakeDocument[];
  uploads?: IntakeUploads | null;
  ready: boolean;
  missing: string[];
  pending: boolean;
  last_error: string | null;
  case_id: string | null;
  case_reference: string | null;
  turn: number;
  version_num: number;
}

const base = "/intake-sessions";

/** Poll interval while the assistant answers (tests shorten it). */
export const intakeTiming = { pollMs: 2000 };

export async function createIntakeSession(packKey: string): Promise<IntakeSession> {
  return (await apiRequest<IntakeSession>("POST", base, { body: { pack_key: packKey } })).data;
}

export async function getIntakeSession(id: string, signal?: AbortSignal): Promise<IntakeSession> {
  return (await apiRequest<IntakeSession>("GET", `${base}/${encodeURIComponent(id)}`, signal ? { signal } : {})).data;
}

export async function sendIntakeMessage(id: string, content: string): Promise<IntakeSession> {
  return (await apiRequest<IntakeSession>("POST", `${base}/${encodeURIComponent(id)}/messages`, { body: { content } })).data;
}

export async function uploadIntakeDocument(id: string, file: File): Promise<IntakeSession> {
  const form = new FormData();
  form.append("file", file, file.name);
  return (await apiRequest<IntakeSession>("POST", `${base}/${encodeURIComponent(id)}/documents`, { form })).data;
}

export async function removeIntakeDocument(id: string, documentId: string): Promise<IntakeSession> {
  return (await apiRequest<IntakeSession>("DELETE", `${base}/${encodeURIComponent(id)}/documents/${encodeURIComponent(documentId)}`)).data;
}

export async function saveIntakeDraft(id: string, versionNum: number, draft: IntakeDraft): Promise<IntakeSession> {
  return (await apiRequest<IntakeSession>("PUT", `${base}/${encodeURIComponent(id)}/draft`, { body: { draft }, headers: ifMatch(versionNum) })).data;
}

export async function createCaseFromIntake(id: string, versionNum: number, idempotencyKey = newIdempotencyKey()) {
  return (await apiRequest<{ case_id: string; reference: string }>("POST", `${base}/${encodeURIComponent(id)}/case`, {
    body: {},
    headers: { ...ifMatch(versionNum), "Idempotency-Key": idempotencyKey },
  })).data;
}
