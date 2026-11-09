import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { Integrations, Session } from "../api/types";
import { App } from "./App";

const devSession: Session = {
  user_id: "00000000-0000-4000-8000-000000000101",
  tenant_id: "00000000-0000-4000-8000-000000000001",
  display_name: "Local Developer",
  roles: ["workspace_member"],
  auth_mode: "dev_mock",
  environment: "development",
  expires_at: null,
};

const integrations: Integrations = {
  host_auth: { mode: "dev_mock", status: "warning", message: "Development mock identity." },
  knowledge: { mode: "stub", status: "stub", message: "Development stub." },
  agent_runtime: { mode: "disabled", status: "disabled", message: "No AI runtime contract is configured yet." },
  case_runtime: { mode: "postgres", status: "ok", message: "System of record reachable." },
};

function mockApi(routes: Record<string, [number, unknown]>) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const [status, body] = routes[String(input)] ?? [404, { error: { code: "not_found", message: "nope", trace_id: "t" } }];
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  });
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe("App shell", () => {
  it("shows the development identity banner driven by the server session", async () => {
    mockApi({ "/api/v1/session": [200, devSession], "/api/v1/integrations": [200, integrations] });
    renderAt("/");
    expect(await screen.findByRole("note")).toHaveTextContent(/Development identity/);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Local");
    expect(screen.getAllByText("LD").length).toBeGreaterThan(0);
  });

  it("never renders a login form when the host session is missing", async () => {
    mockApi({ "/api/v1/session": [401, { error: { code: "unauthenticated", message: "A verified host session is required.", trace_id: "t" } }] });
    renderAt("/");
    expect(await screen.findByText(/Waiting for your workspace session/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/password/i)).toBeNull();
    expect(document.querySelector("input[type=password]")).toBeNull();
    expect(screen.queryByRole("note")).toBeNull();
  });

  it("surfaces API errors with the trace id", async () => {
    mockApi({ "/api/v1/session": [503, { error: { code: "auth_unavailable", message: "Authentication is temporarily unavailable.", trace_id: "trace-503" } }] });
    renderAt("/");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Authentication is temporarily unavailable.");
    expect(alert).toHaveTextContent("trace-503");
  });

  it("renders live integration status", async () => {
    mockApi({ "/api/v1/session": [200, devSession], "/api/v1/integrations": [200, integrations] });
    renderAt("/integrations");
    const kms = await screen.findByRole("region", { name: "Knowledge Management System" });
    expect(within(kms).getByText("Dev stub")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Case Runtime / PostgreSQL" })).getByText("Connected")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Shared AI Agent Runtime" })).getByText("Not configured")).toBeInTheDocument();
  });

  it("shows honest empty states instead of demo data", async () => {
    mockApi({ "/api/v1/session": [200, devSession], "/api/v1/approvals?status=pending": [200, { items: [] }] });
    renderAt("/approvals");
    expect(await screen.findByText("No approvals")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /approve/i })).toBeNull();
  });

  it("navigates with the sidebar and closes the mobile drawer with Escape", async () => {
    mockApi({ "/api/v1/session": [200, devSession], "/api/v1/integrations": [200, integrations], "/api/v1/cases": [200, { items: [], next_cursor: null }] });
    const user = userEvent.setup();
    renderAt("/");
    await screen.findByRole("note");

    const menu = screen.getByRole("button", { name: "Open menu" });
    await user.click(menu);
    expect(menu).toHaveAttribute("aria-expanded", "true");
    await waitFor(() => expect(document.activeElement?.closest("#sidebar")).not.toBeNull());
    await user.keyboard("{Escape}");
    expect(menu).toHaveAttribute("aria-expanded", "false");
    expect(menu).toHaveFocus();

    await user.click(screen.getByRole("link", { name: "Cases" }));
    expect(await screen.findByText("No cases")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Cases" })).toHaveClass("active");
  });
});
