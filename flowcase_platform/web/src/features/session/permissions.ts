import type { Session } from "../../api/types";

// UI affordances only. The server enforces every rule independently; hiding a button is not security.
export const canDesign = (s: Session | null) => !!s?.roles.includes("workflow_designer");
export const canPublish = (s: Session | null) => !!s?.roles.includes("workflow_publisher");
export const canReadAudit = (s: Session | null) =>
  !!s && (s.roles.includes("workflow_designer") || s.roles.includes("workflow_publisher") || s.roles.includes("tenant_admin"));
