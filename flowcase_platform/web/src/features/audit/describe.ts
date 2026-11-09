import type { AuditEvent } from "../../api/workflows";

/** Human-readable summary of an audit event, e.g. "published sim-tender v2". */
export function describe(e: AuditEvent): string {
  const subject = `${e.subject_id}${e.subject_version ? ` v${e.subject_version}` : ""}`;
  const verb: Record<string, string> = {
    "workflow.created": "created workflow",
    "workflow.version.created": "created draft",
    "workflow.version.updated": "edited draft",
    "workflow.version.validated": "validated",
    "workflow.version.published": "published",
    "workflow.version.deprecated": "deprecated",
    "case.created": "started case",
    "task.submitted": "submitted task",
    "approval.decided": "decided approval",
    "approval.invalidated": "invalidated approval",
    "approval.rereview_requested": "re-opened review",
    "artifact.revised": "revised artifact",
    "artifact.version_created": "recorded artifact version",
    "agent.completed": "received agent output",
  };
  return `${verb[e.action] ?? e.action} ${subject}`;
}
