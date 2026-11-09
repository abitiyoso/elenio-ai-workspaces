import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { Sidebar } from "../components/Shell";
import { visibleNavItems } from "../components/nav";
import { ALL_CAPS, NO_CAPS, makeMe } from "./fixtures";

const ADMIN_LABELS = ["People & Teams", "Access Control", "Mini-App Registry", "Identity & Integrations", "Audit Log"];

function renderSidebar(me: ReturnType<typeof makeMe>, path = "/") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Sidebar me={me} open={false} onNavigate={() => {}} />
    </MemoryRouter>,
  );
}

describe("navigation", () => {
  it("hides every admin item when the user has no capabilities", () => {
    renderSidebar(makeMe());
    const nav = screen.getByRole("navigation", { name: "Main navigation" });
    for (const label of ["Home", "App Catalog", "Workspaces"]) {
      expect(within(nav).getByRole("link", { name: label })).toBeInTheDocument();
    }
    for (const label of ADMIN_LABELS) {
      expect(within(nav).queryByRole("link", { name: label })).not.toBeInTheDocument();
    }
    expect(screen.queryByText("ADMINISTRATION")).not.toBeInTheDocument();
  });

  it("shows only the admin items the capabilities allow", () => {
    renderSidebar(makeMe({ view_audit: true, manage_registry: true }));
    expect(screen.getByRole("link", { name: "Audit Log" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Mini-App Registry" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "People & Teams" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Identity & Integrations" })).not.toBeInTheDocument();
  });

  it("shows all admin items for a full admin and marks the current page", () => {
    renderSidebar(makeMe(ALL_CAPS), "/audit");
    for (const label of ADMIN_LABELS) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByRole("link", { name: "Audit Log" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Home" })).not.toHaveAttribute("aria-current");
  });

  it("treats workspace-admin scope as People/Access visibility", () => {
    const labels = visibleNavItems({ ...NO_CAPS, manage_memberships: ["ws-1"] }).map((i) => i.label);
    expect(labels).toContain("Access Control");
    expect(labels).not.toContain("Mini-App Registry");
  });
});
