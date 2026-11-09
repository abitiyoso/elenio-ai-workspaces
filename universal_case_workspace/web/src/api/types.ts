// Shapes of the UCW BFF contract (contracts/BFF_API.md). Flowcase passthrough objects are typed loosely:
// every field the UI reads is optional so a missing upstream field degrades to "—" instead of a crash.

export interface PartialError {
  source: "flowcase" | "agent_studio" | "kms" | "registry" | string;
  code: string;
  message: string;
}

export interface MeContext {
  user_id: string;
  display_name: string;
  tenant_key: string;
  workspace_key: string;
  platform_roles: string[];
  flowcase: { available: boolean; roles: string[]; can_create_cases: boolean };
  partial_errors?: PartialError[];
}

export interface Brand {
  accent?: string;
  icon?: string;
}

export interface ValidationIssue {
  path: string;
  code: string;
  message: string;
}

export interface PackSummary {
  pack_key: string;
  version: string;
  title: string;
  description?: string;
  status: string;
  checksum?: string;
  brand?: Brand;
  flowcase_workflow_key?: string;
  validation?: { schema_ok: boolean; semantic_ok: boolean; errors?: ValidationIssue[] };
  live?: { startable: boolean; reasons?: string[] };
  has_intake?: boolean;
}

export type FieldWidget = "text" | "textarea" | "number" | "select" | "date" | "boolean" | "evidence_ref" | string;

export interface PackField {
  key: string;
  label: string;
  widget: FieldWidget;
  required?: boolean;
  read_only?: boolean;
  options?: string[];
  hint?: string;
  min?: number;
  max?: number;
}

export interface ArtifactSource {
  provider?: string;
  flowcase_artifact_type?: string;
  /** Where the rows live inside the artifact content: a JSON pointer ("/artifact_draft/requirements") or a dotted path. */
  rows_path?: string;
}

export interface PackArtifactType {
  key: string;
  title: string;
  description?: string;
  fields: PackField[];
  source?: ArtifactSource;
}

export interface NavItem {
  /** The case context uses `id`; raw manifests use `key`. */
  id?: string;
  key?: string;
  view: string;
  label: string;
  icon?: string;
  artifact_type?: string;
  required_permission?: string;
}

export interface PackManifest {
  schema_version?: string;
  pack_key: string;
  version: string;
  title: string;
  description?: string;
  status?: string;
  brand?: Brand;
  bindings?: Record<string, unknown>;
  navigation?: NavItem[];
  artifact_types?: PackArtifactType[];
  actions?: { key: string; label: string; executor?: string; agent_key?: string; placement?: string; output_artifact_type?: string }[];
  intake?: unknown;
  [key: string]: unknown;
}

export interface PackDetail {
  pack: PackSummary;
  manifest: PackManifest;
}

export type CaseStatus = "active" | "under_review" | "paused" | "completed" | "cancelled" | "failed" | string;

export interface CasePackRef {
  pack_key: string;
  version: string;
  title: string;
  brand?: Brand;
}

export interface CaseCard {
  id: string;
  reference: string;
  title: string;
  status: CaseStatus;
  outcome?: string | null;
  workflow_key?: string;
  workflow_version?: number | string;
  owner_name?: string;
  current_steps?: string[];
  open_tasks?: number;
  pending_approvals?: number;
  pending_drafts?: number;
  progress?: number;
  created_at?: string;
  updated_at?: string;
  pack?: CasePackRef | null;
}

export interface CaseList {
  items: CaseCard[];
  next_cursor?: string | null;
  partial_errors?: PartialError[];
}

export interface CitedSource {
  source_id: string;
  version_id?: string;
  title: string;
  citation?: string;
  anchor?: string;
}

export interface WorkflowNode {
  id: string;
  name?: string;
  type?: string;
  status?: string;
}

export interface CaseArtifact {
  id: string;
  type: string;
  pack_artifact_type?: string | null;
  version?: number;
  status?: string;
  producer_node_id?: string;
  content?: Record<string, unknown> | null;
  sources?: CitedSource[];
  updated_at?: string;
}

export interface DraftReview {
  id: string;
  node_id: string;
  node_name?: string;
  iteration?: number;
  artifact_type?: string;
  artifact_version?: number;
  content?: Record<string, unknown> | null;
  status: "pending" | "submitted" | "revision_requested" | "cancelled" | string;
  feedback?: string | null;
  decided_by_name?: string | null;
  decided_at?: string | null;
  created_at?: string;
  version_num: number;
  can_submit?: boolean;
  can_revise?: boolean;
  revisions?: number;
}

export interface FlowcaseTask {
  id: string;
  node_id?: string;
  title?: string;
  assignee_role?: string;
  status?: string;
  due_at?: string | null;
  created_at?: string;
  completed_at?: string | null;
  completed_by_name?: string | null;
}

export interface FlowcaseApproval {
  id: string;
  node_id?: string;
  title?: string;
  kind?: string;
  status?: string;
  policy_ref?: string;
  policy_name?: string;
  covered_artifacts?: { type: string; version: number; status?: string }[];
  decision?: { decision: string; rationale?: string; decided_by_name?: string; decided_at?: string } | null;
  blocked_reason?: string | null;
  invalidated_reason?: string | null;
  created_at?: string;
}

export interface AgentRun {
  id?: string;
  node_id?: string;
  node_name?: string;
  provider?: string;
  agent_ref?: string;
  status?: string;
  attempt?: number;
  created_at?: string;
  updated_at?: string;
  completed_at?: string | null;
}

export interface HistoryEntry {
  action?: string;
  actor_name?: string | null;
  actor_type?: string;
  occurred_at?: string;
  data?: Record<string, unknown>;
}

export interface CaseContext {
  case: CaseCard & { variables?: Record<string, unknown>; business_facets?: Record<string, unknown> };
  pack: (CasePackRef & { binding?: string; bound_at?: string; status: "ok" | "unavailable" | string }) | null;
  navigation: NavItem[];
  artifact_types: PackArtifactType[];
  workflow: { version?: number | string; nodes: WorkflowNode[] } | null;
  artifacts: CaseArtifact[];
  drafts: DraftReview[];
  tasks: FlowcaseTask[];
  approvals: FlowcaseApproval[];
  agent_runs: AgentRun[];
  history: HistoryEntry[];
  permissions?: { can_review_drafts?: boolean; can_note?: boolean };
  links?: { flowcase_case?: string; flowcase_approvals?: string };
  partial_errors?: PartialError[];
}
