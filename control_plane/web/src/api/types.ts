// Wire contract between the React shell and the Go BFF (see docs/API.md and api/openapi.yaml).
// Everything here is display data computed server-side; nothing in the browser is authoritative.

export type Uuid = string;
export type IsoTime = string;

export interface ApiError {
  code: string; // stable machine code, e.g. "FORBIDDEN", "CSRF_INVALID", "NOT_MEMBER"
  message: string;
  trace_id: string;
}

export type ReasonCode =
  | "ROLE_GRANT"
  | "EXPLICIT_DENY"
  | "NOT_MEMBER"
  | "MEMBERSHIP_SUSPENDED"
  | "APP_DISABLED"
  | "APP_UNPUBLISHED"
  | "MISSING_PERMISSION"
  | "SUBJECT_DISABLED"
  | "WRONG_AUDIENCE"
  | "POLICY_ERROR"
  | "WORKSPACE_NOT_ALLOWED"
  | "OVERRIDE_ALLOW";

export interface Workspace {
  id: Uuid;
  key: string;
  name: string;
  description: string;
  status: "active" | "archived";
}

export interface AppHealth {
  status: "healthy" | "unhealthy" | "unknown" | "not_configured";
  checked_at: IsoTime | null;
  detail: string; // short, non-sensitive
}

export interface AppSummary {
  app_key: string;
  display_name: string;
  description: string;
  category: "platform" | "experience" | "business" | "admin";
  icon: string;
  owner_team: string;
  route_base: string; // "/apps/<key>/"
  version: string;
  status: "draft" | "published" | "disabled" | "retired";
  // "proxy": BFF forwards with an audience-bound token; "pending": route authorised but the
  // service has not adopted the Control Plane identity contract yet (no upstream forwarding).
  integration: "proxy" | "pending";
  health: AppHealth;
}

export interface Me {
  user: { id: Uuid; display_name: string; email: string | null; principal_type: "human" | "service" };
  tenant: { id: Uuid; key: string; name: string };
  workspace: Workspace | null; // active workspace (server-side session state)
  workspaces: Workspace[]; // only active memberships
  apps: AppSummary[]; // only published + entitled in the active workspace
  permissions: string[]; // effective permissions in the active workspace (display only)
  capabilities: AdminCapabilities;
  csrf_token: string; // send as X-CSRF-Token on every non-GET request
  session: { expires_at: IsoTime; idle_expires_at: IsoTime };
  environment: "development" | "production";
  policy_version: string;
}

// Which admin areas the UI may show. Each admin endpoint re-checks on the server.
export interface AdminCapabilities {
  view_people: boolean; // platform admin, or workspace admin of >=1 workspace
  manage_workspaces: boolean; // create workspaces (platform admin)
  manage_memberships: Uuid[]; // workspace ids the caller may manage memberships/roles in
  manage_registry: boolean;
  view_audit: boolean;
  view_integrations: boolean;
  approve_grants: boolean; // may decide pending sensitive grant requests (never their own)
  suspend_users: boolean;
}

export interface Decision {
  decision_id: string;
  effect: "allow" | "deny";
  reason_code: ReasonCode;
  subject_id: Uuid;
  tenant_id: Uuid;
  workspace_id: Uuid | "";
  resource_type: "mini_app" | "workspace" | "platform_admin";
  resource_id: string;
  action: string;
  policy_version: string;
  effective_roles: string[];
  missing_permissions: string[];
  evaluated_at: IsoTime;
  correlation_id: string;
  trace?: { step: string; outcome: "pass" | "fail" | "info"; detail: string }[]; // admin preview only
}

export interface LaunchResult {
  app_key: string;
  route_base: string;
  integration: "proxy" | "pending";
}

// ---- Admin: people / workspaces / roles ----

export interface Role {
  key: string;
  display_name: string;
  description: string;
  scope: "tenant" | "workspace";
  sensitive: boolean; // granting requires an independent approver
  permissions: string[];
}

export interface Permission {
  key: string;
  resource_type: string;
  action: string;
  description: string;
  sensitive: boolean;
}

export interface RoleBinding {
  id: Uuid;
  role_key: string;
  workspace_id: Uuid | null; // null = tenant scope
  state: "active" | "revoked";
  expires_at: IsoTime | null;
  granted_by: Uuid | null;
  reason: string;
  created_at: IsoTime;
}

export interface Membership {
  id: Uuid;
  workspace_id: Uuid;
  workspace_key: string;
  workspace_name: string;
  user_id: Uuid;
  state: "active" | "suspended" | "revoked";
  joined_at: IsoTime;
  roles: RoleBinding[];
}

export interface DirectoryUser {
  id: Uuid;
  display_name: string;
  email: string | null;
  idp_issuer: string;
  idp_subject: string;
  principal_type: "human" | "service";
  status: "active" | "suspended";
  last_login_at: IsoTime | null;
  tenant_roles: RoleBinding[];
  memberships: Membership[];
}

export interface GrantRequest {
  id: Uuid;
  user_id: Uuid;
  user_name: string;
  workspace_id: Uuid | null;
  workspace_name: string | null;
  role_key: string;
  requested_by: Uuid;
  requested_by_name: string;
  reason: string;
  state: "pending" | "approved" | "rejected" | "cancelled" | "expired";
  decided_by: Uuid | null;
  decision_reason: string | null;
  created_at: IsoTime;
  expires_at: IsoTime;
}

export interface EntitlementOverride {
  id: Uuid;
  workspace_id: Uuid;
  user_id: Uuid;
  user_name: string;
  permission_key: string;
  effect: "allow" | "deny";
  reason: string;
  expires_at: IsoTime | null;
  created_by: Uuid;
  created_at: IsoTime;
}

// Result of PUT /admin/memberships/{id}/roles and PUT /admin/users/{id}/tenant-roles
export type GrantResult =
  | { outcome: "granted"; binding: RoleBinding }
  | { outcome: "pending_approval"; request: GrantRequest };

// ---- Admin: registry ----

export interface AppManifest {
  schema_version: "workspace.app.v1";
  app_key: string;
  display_name: string;
  description: string;
  category: "platform" | "experience" | "business" | "admin";
  icon: string;
  owner_team: string;
  deployment_mode: "same_origin_route";
  route_base: string;
  environment: "dev" | "staging" | "production";
  version: string;
  status: "draft" | "published" | "disabled" | "retired";
  required_entitlements: string[];
  workspace_allowlist?: string[];
  health_route?: "/healthz" | "/readyz";
  requires_domain_authorization: true;
  notes?: string;
}

export interface AppVersion {
  id: Uuid;
  version: string;
  release_state: "draft" | "published" | "superseded";
  checksum: string; // sha256 of canonical manifest
  manifest: AppManifest;
  created_by: Uuid | null;
  created_at: IsoTime;
  published_by: Uuid | null;
  published_at: IsoTime | null;
}

export interface RegistryApp {
  app_key: string;
  display_name: string;
  owner_team: string;
  status: "draft" | "published" | "disabled" | "retired";
  revision: number; // send as If-Match: "<revision>" on publish/disable/enable
  route_base: string;
  published_version: string | null;
  latest_version: string;
  disabled_reason: string | null;
  integration: "proxy" | "pending";
  upstream_configured: boolean; // trusted deployment config has a target (target itself is never shown)
  audience: string | null;
  health: AppHealth;
  versions: AppVersion[];
  updated_at: IsoTime;
}

export interface ValidationIssue {
  field: string;
  code: string;
  message: string;
}
// 422 body for manifest validation: ApiError & { issues: ValidationIssue[] }

// ---- Admin: integrations / audit ----

export interface IntegrationsStatus {
  identity_provider: {
    issuer: string;
    client_id: string;
    status: "ok" | "unavailable" | "not_configured";
    last_checked_at: IsoTime | null;
    detail: string;
    scopes: string[];
  };
  session: {
    cookie_name: string;
    cookie_secure: boolean;
    same_site: "Lax" | "Strict";
    idle_ttl_seconds: number;
    absolute_ttl_seconds: number;
    active_sessions: number;
  };
  service_tokens: {
    issuer: string;
    algorithm: "ES256";
    key_ids: string[];
    ttl_seconds: number;
    jwks_path: "/.well-known/jwks.json";
  };
  adapters: {
    app_key: string;
    display_name: string;
    audience: string | null;
    integration: "proxy" | "pending";
    contract: string; // e.g. "control_plane_jwt (Flowcase trusted_jwt)", short human text
    pending_reason: string | null;
    health: AppHealth;
  }[];
  service_identities: {
    client_id: string;
    audience: string;
    scopes: string[];
    status: "active" | "disabled";
    description: string;
  }[];
}

export interface AuditEvent {
  id: string; // monotonically increasing sequence as string
  created_at: IsoTime;
  actor_user_id: Uuid | null;
  actor_name: string | null;
  actor_type: "human" | "service" | "system";
  workspace_id: Uuid | null;
  action: string; // e.g. "role_binding.granted", "authz.denied", "app.published"
  target_type: string;
  target_id: string;
  decision: "allow" | "deny" | null;
  reason_code: string | null;
  policy_version: string | null;
  correlation_id: string;
  rationale: string | null;
  metadata: Record<string, unknown>; // already redacted server-side
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}
