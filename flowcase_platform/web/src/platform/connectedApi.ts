import { apiGet, apiRequest, newIdempotencyKey } from "../api/client";
import type {
  NodeType,
  SaveResult,
  WorkflowDefinition,
  WorkflowVersion,
  ValidationReport,
} from "../api/workflows";
import type { Session } from "../api/types";
import { parseDefinition } from "./graphModel";

export interface RuntimeCapabilities {
  supported_node_types: NodeType[];
}
export interface CreateDefinitionInput {
  key: string;
  name: string;
  description?: string;
  template?: string;
  definition?: WorkflowDefinition;
}
export function isWorkflowVersion(value: unknown): value is WorkflowVersion {
  if (!value || typeof value !== "object") return false;
  const v = value as Partial<WorkflowVersion>;
  if (
    typeof v.id !== "string" ||
    typeof v.workflow_key !== "string" ||
    !Number.isInteger(v.version) ||
    !Number.isInteger(v.version_num) ||
    (v.version_num ?? 0) < 1 ||
    !["draft", "validated", "published", "deprecated"].includes(v.status ?? "")
  )
    return false;
  try {
    const d = parseDefinition(JSON.stringify(v.definition));
    return d.key === v.workflow_key && d.version === v.version;
  } catch {
    return false;
  }
}
export function assertSaveResult(value: unknown): asserts value is SaveResult {
  const result = value as Partial<SaveResult> | null;
  if (
    !result ||
    !isWorkflowVersion(result.version) ||
    !isValidationReport(result.validation)
  )
    throw new Error(
      "The service returned an invalid draft response. The editor document was retained.",
    );
}
export function isValidationReport(value: unknown): value is ValidationReport {
  const report = value as Partial<ValidationReport> | null;
  return (
    !!report &&
    typeof report.valid === "boolean" &&
    Array.isArray(report.diagnostics) &&
    report.diagnostics.every(
      (d) =>
        d &&
        typeof d.message === "string" &&
        ["error", "warning"].includes(d.severity),
    )
  );
}
export function isSession(value: unknown): value is Session {
  if (!value || typeof value !== "object") return false;
  const s = value as Partial<Session>;
  return (
    typeof s.user_id === "string" &&
    !!s.user_id &&
    typeof s.tenant_id === "string" &&
    !!s.tenant_id &&
    typeof s.display_name === "string" &&
    Array.isArray(s.roles) &&
    s.roles.every((r) => typeof r === "string") &&
    ["trusted_jwt", "dev_mock"].includes(s.auth_mode ?? "") &&
    ["development", "test", "staging", "production"].includes(
      s.environment ?? "",
    )
  );
}
export function isRuntimeCapabilities(
  value: unknown,
): value is RuntimeCapabilities {
  return (
    !!value &&
    typeof value === "object" &&
    Array.isArray((value as RuntimeCapabilities).supported_node_types) &&
    (value as RuntimeCapabilities).supported_node_types.every(
      (t) => typeof t === "string",
    )
  );
}

/** The server already accepts definition imports. Existing clients keep their narrower signature. */
export async function createDefinition(
  input: CreateDefinitionInput,
  idempotencyKey: string = newIdempotencyKey(),
): Promise<SaveResult> {
  const result = (
    await apiRequest<SaveResult>("POST", "/api/v1/workflows", {
      body: input,
      headers: { "Idempotency-Key": idempotencyKey },
    })
  ).data;
  assertSaveResult(result);
  return result;
}
export function fetchRuntimeCapabilities(signal?: AbortSignal) {
  return apiGet<RuntimeCapabilities>("/api/v1/runtime-capabilities", signal);
}
export function canEditVersion(status: string, designer: boolean) {
  return designer && (status === "draft" || status === "validated");
}

/** Unknown runtime node types never become executable because their schema name exists. */
export function unsupportedNodes(
  definition: WorkflowDefinition,
  capabilities: RuntimeCapabilities,
): string[] {
  return definition.nodes
    .filter((n) => !capabilities.supported_node_types.includes(n.type))
    .map((n) => n.id);
}
