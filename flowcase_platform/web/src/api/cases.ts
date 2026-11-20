// Typed client for the case runtime (mirrors api/openapi.yaml).
import { apiRequest, newIdempotencyKey } from "./client";

export type CaseStatus = "active" | "paused" | "under_review" | "completed" | "cancelled" | "failed";

export interface CaseRef {
  id: string;
  reference: string;
  title: string;
  status: CaseStatus;
}

export interface CaseSummary extends CaseRef {
  outcome: string | null;
  workflow_key: string;
  workflow_name: string;
  workflow_version: number;
  definition_hash: string;
  owner_name: string;
  current_steps: string[];
  open_tasks: number;
  pending_approvals: number;
  progress: number;
  created_at: string;
  updated_at: string;
  version_num: number;
}

export interface Execution {
  id: string;
  node_id: string;
  node_name: string;
  node_type: string;
  iteration: number;
  status: string;
  attempt: number;
  last_error: string | null;
  created_at: string;
  completed_at: string | null;
}

export interface Task {
  id: string;
  case: CaseRef;
  node_id: string;
  iteration: number;
  title: string;
  assignee_role: string;
  instructions: string;
  form_schema: DataSchema | null;
  revision_note: string | null;
  status: "assigned" | "submitted" | "revision_requested" | "cancelled";
  output: Record<string, unknown> | null;
  completed_by_name: string | null;
  due_at: string | null;
  created_at: string;
  completed_at: string | null;
  version_num: number;
  can_complete: boolean;
  can_request_revision: boolean;
  comments?: { author_name: string; body: string; created_at: string }[];
}

export interface ArtifactVersion {
  id: string;
  version: number;
  status: "draft" | "ready_for_review" | "approved" | "stale" | "superseded";
  checksum: string;
  produced_by_kind: "agent" | "human" | "revision";
  produced_by_name: string | null;
  change_note: string;
  stale_reason: string | null;
  content: Record<string, unknown>;
  created_at: string;
  depends_on: { type: string; version: number }[];
  evidence: { source_id: string; version_id: string; title: string; status: "valid" | "stale" | "forbidden"; classification: string }[];
}

export interface Artifact {
  id: string;
  artifact_type: string;
  producer_node_id: string;
  current_version: number;
  version_num: number;
  versions: ArtifactVersion[];
}

export interface AgentRun {
  node_id: string;
  attempt: number;
  provider: string;
  agent_ref: string;
  skill_version: string;
  status: string;
  evidence: { source_id: string; status: string }[] | null;
  polls: number;
  created_at: string;
}

export interface Approval {
  kind: "gate" | "rereview";
  invalidated_reason: string | null;
  covered_artifacts: { type: string; version: number; status: string; checksum: string }[];
  id: string;
  case: CaseRef;
  node_id: string;
  iteration: number;
  title: string;
  policy_ref: string;
  policy_name: string;
  policy_provisional: boolean;
  return_to: string | null;
  subject_hash: string;
  subject: Record<string, unknown>;
  status: "pending" | "approved" | "rejected" | "returned" | "cancelled" | "invalidated";
  decision: { decision: string; rationale: string; decided_by_name: string; decision_hash: string; decided_at: string } | null;
  created_at: string;
  version_num: number;
  can_decide: boolean;
  blocked_reason: string | null;
}

export interface CaseDetail extends CaseSummary {
  variables: Record<string, unknown>;
  business_facets: Record<string, unknown>;
  workflow_nodes: { id: string; name: string; type: string; next: string[] }[];
  timeline: Execution[];
  tasks: Task[];
  approvals: Approval[];
  condition_evaluations: { node_id: string; expression: unknown; inputs: Record<string, unknown>; result: boolean; target: string; evaluated_at: string }[];
  history: { action: string; actor_name: string | null; actor_type: string; occurred_at: string; data: Record<string, unknown> }[];
  actions: { can_pause: boolean; can_resume: boolean; can_note: boolean };
  artifacts: Artifact[];
  agent_runs: AgentRun[];
  drafts: DraftReview[];
}

/** The case owner's review of an agent draft (author_review steps); newest first in CaseDetail.drafts. */
export interface DraftReview {
  id: string;
  case: CaseRef;
  node_id: string;
  node_name: string;
  iteration: number;
  artifact_type: string;
  artifact_version: number;
  artifact_version_id: string;
  content: Record<string, unknown> | null;
  status: "pending" | "submitted" | "revision_requested" | "cancelled";
  feedback: string | null;
  decided_by_name: string | null;
  decided_at: string | null;
  created_at: string;
  version_num: number;
  can_submit: boolean;
  can_revise: boolean;
  revisions: number;
}

export interface DataProperty {
  type: "string" | "number" | "integer" | "boolean" | "array" | "object";
  title?: string;
  description?: string;
  format?: string;
  enum?: unknown[];
  items?: DataProperty;
  "x-hidden"?: boolean;
}

export interface DataSchema {
  type: "object";
  properties?: Record<string, DataProperty>;
  required?: string[];
}

const ifMatch = (n: number) => ({ "If-Match": `"${n}"` });

export async function createCase(input: { workflow_key: string; version?: number; title: string; variables: Record<string, unknown> }) {
  return (await apiRequest<CaseDetail>("POST", "/api/v1/cases", { body: input, headers: { "Idempotency-Key": newIdempotencyKey() } })).data;
}

export async function setCasePaused(id: string, versionNum: number, pause: boolean, reason: string) {
  return (await apiRequest<CaseDetail>("POST", `/api/v1/cases/${id}/${pause ? "pause" : "resume"}`, { body: { reason }, headers: ifMatch(versionNum) })).data;
}

export async function addCaseNote(id: string, body: string) {
  return (await apiRequest<CaseDetail>("POST", `/api/v1/cases/${id}/events`, { body: { type: "note", body } })).data;
}

export async function completeTask(id: string, versionNum: number, output: Record<string, unknown>, comment: string) {
  return (await apiRequest<Task>("POST", `/api/v1/tasks/${id}/complete`, { body: { output, comment }, headers: ifMatch(versionNum) })).data;
}

export async function requestRevision(id: string, versionNum: number, rationale: string) {
  return (await apiRequest<Task>("POST", `/api/v1/tasks/${id}/revision`, { body: { rationale }, headers: ifMatch(versionNum) })).data;
}

export async function decideApproval(id: string, versionNum: number, decision: "approve" | "reject" | "return", rationale: string) {
  return (await apiRequest<Approval>("POST", `/api/v1/approvals/${id}/decision`, { body: { decision, rationale }, headers: ifMatch(versionNum) })).data;
}

export async function submitDraft(id: string, versionNum: number, note = "") {
  return (await apiRequest<DraftReview>("POST", `/api/v1/drafts/${id}/submit`, { body: note ? { note } : {}, headers: ifMatch(versionNum) })).data;
}

export async function reviseDraft(id: string, versionNum: number, feedback: string) {
  return (await apiRequest<DraftReview>("POST", `/api/v1/drafts/${id}/revision`, { body: { feedback }, headers: ifMatch(versionNum) })).data;
}

export async function reviseArtifact(caseId: string, artifactId: string, versionNum: number, content: Record<string, unknown>, changeNote: string) {
  return (await apiRequest<CaseDetail>("POST", `/api/v1/cases/${caseId}/artifacts/${artifactId}/versions`, {
    body: { content, change_note: changeNote }, headers: ifMatch(versionNum),
  })).data;
}

export const blockedReasonText: Record<string, string> = {
  stale_artifacts: "A covered artifact is stale and must be revised before this decision.",
  rereview_pending: "Affected reviews were re-opened by a change and must be re-approved first.",
  service_principal: "AI agents and service identities never hold approval authority.",
  missing_role: "Deciding requires the domain_approver role.",
  no_delegation: "You hold no delegation for this policy.",
  sod_case_owner: "Segregation of duties: you own or created this case.",
  sod_task_submitter: "Segregation of duties: you submitted work in this case.",
  case_paused: "The case is paused.",
};
