// Typed client for the workflow registry (mirrors api/openapi.yaml).
import { apiRequest, newIdempotencyKey } from "./client";

export type NodeType =
  | "start"
  | "end"
  | "human_task"
  | "agent_task"
  | "service_task"
  | "human_approval"
  | "condition"
  | "parallel_split"
  | "parallel_join"
  | "timer"
  | "subworkflow";

export const NODE_TYPES: NodeType[] = [
  "start", "end", "human_task", "agent_task", "service_task", "human_approval",
  "condition", "parallel_split", "parallel_join", "timer", "subworkflow",
];

export interface WorkflowNode {
  id: string;
  type: NodeType;
  name: string;
  config?: Record<string, unknown>;
  next: string[];
}

export interface WorkflowDefinition {
  schema_version: string;
  key: string;
  name: string;
  version: number;
  status?: VersionStatus;
  description?: string;
  tenant_scope?: string;
  input_schema?: Record<string, unknown>;
  policy_refs?: string[];
  metadata?: Record<string, unknown>;
  nodes: WorkflowNode[];
}

export type VersionStatus = "draft" | "validated" | "published" | "deprecated";

export interface Diagnostic {
  severity: "error" | "warning";
  code: string;
  message: string;
  path: string;
  node_id?: string;
}

export interface ValidationReport {
  valid: boolean;
  diagnostics: Diagnostic[];
}

export interface WorkflowVersion {
  id: string;
  workflow_key: string;
  version: number;
  status: VersionStatus;
  schema_version: string;
  definition: WorkflowDefinition;
  definition_hash: string | null;
  derived_from_version: number | null;
  change_note: string;
  created_by_name: string;
  created_at: string;
  updated_at: string;
  validated_at: string | null;
  published_by_name: string | null;
  published_at: string | null;
  deprecated_at: string | null;
  version_num: number;
}

export interface SaveResult {
  version: WorkflowVersion;
  validation: ValidationReport;
}

export interface VersionSummary {
  id: string;
  version: number;
  status: VersionStatus;
  definition_hash: string | null;
  change_note: string;
  node_count: number;
  created_by_name: string;
  created_at: string;
  updated_at: string;
  published_by_name: string | null;
  published_at: string | null;
  deprecated_at: string | null;
  version_num: number;
}

export interface WorkflowSummary {
  id: string;
  key: string;
  name: string;
  description: string;
  domain_pack: string | null;
  latest_published_version: number | null;
  open_draft_version: number | null;
  version_count: number;
  node_count: number;
  updated_at: string;
}

export interface WorkflowDetail extends WorkflowSummary {
  versions: VersionSummary[];
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface Template {
  pack: string;
  key: string;
  name: string;
  description: string;
}

export interface Catalog {
  policies: { ref: string; name: string; pack: string; provisional: boolean; sod: string[]; ratification?: { reference: string; date: string; by: string } }[];
  agents: { ref: string; name: string; pack: string; skill_version: string; runtime_agent_id: string }[];
  connectors: { ref: string; name: string; pack: string; operations: string[] }[];
  assignee_roles: string[];
  artifact_types: string[];
}

export interface AuditEvent {
  id: string;
  seq: number;
  occurred_at: string;
  actor_type: string;
  actor_name: string | null;
  action: string;
  subject_type: string;
  subject_id: string;
  subject_version: string | null;
  trace_id: string | null;
  data: Record<string, unknown>;
}

const enc = encodeURIComponent;
const versionPath = (key: string, v: number) => `/api/v1/workflows/${enc(key)}/versions/${v}`;
const ifMatch = (versionNum: number) => ({ "If-Match": `"${versionNum}"` });

export async function createWorkflow(input: { key: string; name?: string; description?: string; template?: string }) {
  return (await apiRequest<SaveResult>("POST", "/api/v1/workflows", { body: input, headers: { "Idempotency-Key": newIdempotencyKey() } })).data;
}

export async function duplicateWorkflow(key: string, input: { key: string; name?: string; from_version?: number }) {
  return (await apiRequest<SaveResult>("POST", `/api/v1/workflows/${enc(key)}/duplicate`, { body: input, headers: { "Idempotency-Key": newIdempotencyKey() } })).data;
}

export async function createDraftVersion(key: string, fromVersion = 0) {
  return (await apiRequest<SaveResult>("POST", `/api/v1/workflows/${enc(key)}/versions`, {
    body: { from_version: fromVersion },
    headers: { "Idempotency-Key": newIdempotencyKey() },
  })).data;
}

export async function getVersion(key: string, v: number, signal?: AbortSignal) {
  return (await apiRequest<WorkflowVersion>("GET", versionPath(key, v), signal ? { signal } : {})).data;
}

export async function saveDraft(key: string, v: number, versionNum: number, definition: WorkflowDefinition, changeNote?: string) {
  const body = changeNote === undefined ? { definition } : { definition, change_note: changeNote };
  return (await apiRequest<SaveResult>("PATCH", versionPath(key, v), { body, headers: ifMatch(versionNum) })).data;
}

export async function validateVersion(key: string, v: number) {
  return (await apiRequest<SaveResult>("POST", `${versionPath(key, v)}/validate`)).data;
}

export async function publishVersion(key: string, v: number, versionNum: number) {
  return (await apiRequest<SaveResult>("POST", `${versionPath(key, v)}/publish`, { headers: ifMatch(versionNum) })).data;
}

export async function deprecateVersion(key: string, v: number, versionNum: number) {
  return (await apiRequest<SaveResult>("POST", `${versionPath(key, v)}/deprecate`, { headers: ifMatch(versionNum) })).data;
}

export async function validateDocument(definition: WorkflowDefinition) {
  return (await apiRequest<ValidationReport>("POST", "/api/v1/workflow-validations", { body: { definition } })).data;
}

export function workflowsPath(status: string, q: string): string {
  const p = new URLSearchParams();
  if (status !== "all") p.set("status", status);
  if (q.trim()) p.set("q", q.trim());
  const s = p.toString();
  return `/api/v1/workflows${s ? `?${s}` : ""}`;
}
