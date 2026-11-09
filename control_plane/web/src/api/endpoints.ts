// Central list of API paths (relative, same-origin).
import { seg } from "./client";

export const api = {
  me: "/api/v1/me",
  sessionWorkspace: "/api/v1/session/workspace",
  apps: "/api/v1/apps",
  authzCheck: "/api/v1/authz/check",
  logout: "/auth/logout",
  admin: {
    users: (q = "") => `/api/v1/admin/users?q=${encodeURIComponent(q)}`,
    usersCreate: "/api/v1/admin/users",
    user: (id: string) => `/api/v1/admin/users/${seg(id)}`,
    workspaces: "/api/v1/admin/workspaces",
    members: (id: string) => `/api/v1/admin/workspaces/${seg(id)}/members`,
    membership: (id: string) => `/api/v1/admin/memberships/${seg(id)}`,
    roleBinding: (id: string) => `/api/v1/admin/role-bindings/${seg(id)}`,
    grantRequests: "/api/v1/admin/grant-requests?state=pending",
    grantRequest: (id: string) => `/api/v1/admin/grant-requests/${seg(id)}`,
    roles: "/api/v1/admin/roles",
    permissions: "/api/v1/admin/permissions",
    overrides: (wsId: string) => `/api/v1/admin/workspaces/${seg(wsId)}/overrides`,
    override: (id: string) => `/api/v1/admin/overrides/${seg(id)}`,
    accessPreview: "/api/v1/admin/access-preview",
    apps: "/api/v1/admin/apps",
    app: (key: string) => `/api/v1/admin/apps/${seg(key)}`,
    audit: "/api/v1/admin/audit",
    integrations: "/api/v1/admin/integrations",
  },
} as const;
