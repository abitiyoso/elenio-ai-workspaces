import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AccessPreviewPanel } from "../pages/Access";
import { resetAuthRedirect, setCsrfToken } from "../api/client";
import type { Decision } from "../api/types";
import { jsonResponse, makeMe, renderWithSession } from "./fixtures";

const deny: Decision = {
  decision_id: "dec-1",
  effect: "deny",
  reason_code: "MISSING_PERMISSION",
  subject_id: "u-2",
  tenant_id: "t-1",
  workspace_id: "ws-1",
  resource_type: "mini_app",
  resource_id: "kms",
  action: "app:open",
  policy_version: "p-1",
  effective_roles: ["workspace_viewer"],
  missing_permissions: ["kms:open"],
  evaluated_at: "2026-10-30T10:00:00Z",
  correlation_id: "corr-1",
  trace: [
    { step: "membership", outcome: "pass", detail: "active member of research" },
    { step: "permission", outcome: "fail", detail: "kms:open not granted by any role" },
  ],
};

describe("access preview", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    resetAuthRedirect();
    setCsrfToken("csrf-abc");
    fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.startsWith("/api/v1/admin/users")) {
        return jsonResponse(200, {
          items: [
            {
              id: "u-2",
              display_name: "Grace Hopper",
              email: "grace@example.test",
              idp_issuer: "https://idp",
              idp_subject: "s2",
              principal_type: "human",
              status: "active",
              last_login_at: null,
              tenant_roles: [],
              memberships: [],
            },
          ],
        });
      }
      if (url === "/api/v1/admin/workspaces") {
        return jsonResponse(200, {
          items: [{ id: "ws-1", key: "research", name: "Research", description: "", status: "active", member_count: 3 }],
        });
      }
      if (url === "/api/v1/admin/apps") {
        return jsonResponse(200, { items: [{ app_key: "kms", display_name: "Knowledge Base", versions: [] }] });
      }
      if (url === "/api/v1/admin/access-preview" && init?.method === "POST") {
        return jsonResponse(200, deny);
      }
      return jsonResponse(404, { code: "NOT_FOUND", message: url, trace_id: "" });
    });
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("renders the deny reason, missing permissions and trace from the server", async () => {
    const user = userEvent.setup();
    renderWithSession(<AccessPreviewPanel />, makeMe({ view_people: true, manage_registry: true, manage_memberships: ["ws-1"] }));

    await screen.findByRole("option", { name: /Grace Hopper/ });
    await screen.findByRole("option", { name: "Knowledge Base" });
    await user.selectOptions(screen.getByLabelText("User"), "u-2");
    await user.selectOptions(screen.getByLabelText("Workspace"), "ws-1");
    await user.selectOptions(screen.getByLabelText("App"), "kms");
    await user.click(screen.getByRole("button", { name: /Evaluate access/ }));

    await waitFor(() => expect(screen.getByRole("heading", { name: /Denied/ })).toBeInTheDocument());
    expect(screen.getByText("MISSING_PERMISSION")).toBeInTheDocument();
    expect(screen.getByText(/none of the user's roles grant the required permission/)).toBeInTheDocument();
    expect(screen.getByText("kms:open")).toBeInTheDocument();
    expect(screen.getByText("workspace_viewer")).toBeInTheDocument();
    expect(screen.getByText("kms:open not granted by any role")).toBeInTheDocument();

    const call = fetchMock.mock.calls.find(([u]) => u === "/api/v1/admin/access-preview")!;
    const init = call[1] as RequestInit & { headers: Record<string, string> };
    expect(JSON.parse(init.body as string)).toEqual({ user_id: "u-2", workspace_id: "ws-1", app_key: "kms" });
    expect(init.headers["X-CSRF-Token"]).toBe("csrf-abc");
  });

  it("shows code, message and trace id when the server forbids the preview", async () => {
    fetchMock.mockImplementation(async (url: string) =>
      url === "/api/v1/admin/access-preview"
        ? jsonResponse(403, { code: "FORBIDDEN", message: "not allowed", trace_id: "trace-77" })
        : url.startsWith("/api/v1/admin/users")
          ? jsonResponse(200, { items: [{ id: "u-2", display_name: "Grace", email: null, memberships: [], tenant_roles: [] }] })
          : url === "/api/v1/admin/apps"
            ? jsonResponse(200, { items: [{ app_key: "kms", display_name: "KB", versions: [] }] })
            : jsonResponse(200, { items: [] }),
    );
    const user = userEvent.setup();
    renderWithSession(<AccessPreviewPanel />, makeMe({ view_people: true, manage_registry: true }));
    await screen.findByRole("option", { name: "Grace" });
    await screen.findByRole("option", { name: "KB" });
    await user.selectOptions(screen.getByLabelText("User"), "u-2");
    await user.selectOptions(screen.getByLabelText("Workspace"), "ws-1");
    await user.selectOptions(screen.getByLabelText("App"), "kms");
    await user.click(screen.getByRole("button", { name: /Evaluate access/ }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("FORBIDDEN");
    expect(alert).toHaveTextContent("not allowed");
    expect(alert).toHaveTextContent("trace-77");
  });
});
