import type { ReactNode } from "react";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import type { AdminCapabilities, Me } from "../api/types";
import { SessionContext } from "../session/context";
import { ToastProvider } from "../components/ui";

export const NO_CAPS: AdminCapabilities = {
  view_people: false,
  manage_workspaces: false,
  manage_memberships: [],
  manage_registry: false,
  view_audit: false,
  view_integrations: false,
  approve_grants: false,
  suspend_users: false,
};

export const ALL_CAPS: AdminCapabilities = {
  view_people: true,
  manage_workspaces: true,
  manage_memberships: ["ws-1"],
  manage_registry: true,
  view_audit: true,
  view_integrations: true,
  approve_grants: true,
  suspend_users: true,
};

export function makeMe(caps: Partial<AdminCapabilities> = {}): Me {
  const ws = { id: "ws-1", key: "research", name: "Research", description: "", status: "active" as const };
  return {
    user: { id: "u-me", display_name: "Ada Lovelace", email: "ada@example.test", principal_type: "human" },
    tenant: { id: "t-1", key: "acme", name: "Acme" },
    workspace: ws,
    workspaces: [ws],
    apps: [],
    permissions: ["catalog:view"],
    capabilities: { ...NO_CAPS, ...caps },
    csrf_token: "csrf-abc",
    session: { expires_at: "2026-10-31T00:00:00Z", idle_expires_at: "2026-10-30T12:00:00Z" },
    environment: "development",
    policy_version: "p-1",
  };
}

export function renderWithSession(ui: ReactNode, me: Me, path = "/") {
  const value = {
    me,
    epoch: 0,
    refresh: vi.fn(async () => {}),
    switchWorkspace: vi.fn(async () => {}),
    signOut: vi.fn(async () => {}),
  };
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ToastProvider>
        <SessionContext.Provider value={value}>{ui}</SessionContext.Provider>
      </ToastProvider>
    </MemoryRouter>,
  );
}

export function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}
