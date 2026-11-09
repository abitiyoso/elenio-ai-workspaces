// Mirrors api/openapi.yaml (Phase 0 surface).

export type Role =
  | "workspace_member"
  | "case_owner"
  | "workflow_designer"
  | "workflow_publisher"
  | "sme_contributor"
  | "domain_approver"
  | "ai_ops"
  | "tenant_admin";

export interface DevPersona {
  key: string;
  display_name: string;
  roles: Role[];
}

export interface Session {
  persona?: string;
  dev_personas?: DevPersona[];
  user_id: string;
  tenant_id: string;
  display_name: string;
  roles: Role[];
  auth_mode: "trusted_jwt" | "dev_mock";
  environment: "development" | "test" | "staging" | "production";
  expires_at: string | null;
}

export type IntegrationState = "ok" | "warning" | "disabled" | "stub" | "unavailable";

export interface IntegrationStatus {
  mode: string;
  status: IntegrationState;
  message: string;
}

export interface Integrations {
  host_auth: IntegrationStatus;
  knowledge: IntegrationStatus;
  agent_runtime: IntegrationStatus;
  case_runtime: IntegrationStatus;
}
