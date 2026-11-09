import type { AdminCapabilities } from "../api/types";

export interface NavItem {
  to: string;
  label: string;
  icon: string;
  group: "workspace" | "admin";
  /** UI-only visibility; the server re-checks every admin endpoint. */
  visible: (caps: AdminCapabilities) => boolean;
}

const always = () => true;

export const canSeePeople = (c: AdminCapabilities) => c.view_people || c.approve_grants;
export const canSeeAccess = (c: AdminCapabilities) => c.view_people || c.manage_memberships.length > 0;
export const canSeeWorkspaceAdmin = (c: AdminCapabilities) => c.manage_workspaces || c.manage_memberships.length > 0;

export const NAV_ITEMS: NavItem[] = [
  { to: "/", label: "Home", icon: "home", group: "workspace", visible: always },
  { to: "/catalog", label: "App Catalog", icon: "grid", group: "workspace", visible: always },
  { to: "/workspaces", label: "Workspaces", icon: "layers", group: "workspace", visible: always },
  { to: "/people", label: "People & Teams", icon: "users", group: "admin", visible: canSeePeople },
  { to: "/access", label: "Access Control", icon: "shield", group: "admin", visible: canSeeAccess },
  { to: "/registry", label: "Mini-App Registry", icon: "box", group: "admin", visible: (c) => c.manage_registry },
  { to: "/integrations", label: "Identity & Integrations", icon: "plug", group: "admin", visible: (c) => c.view_integrations },
  { to: "/audit", label: "Audit Log", icon: "scroll", group: "admin", visible: (c) => c.view_audit },
];

export function visibleNavItems(caps: AdminCapabilities): NavItem[] {
  return NAV_ITEMS.filter((i) => i.visible(caps));
}

export function titleForPath(pathname: string): { section: string; title: string } {
  const item = NAV_ITEMS.find((i) => (i.to === "/" ? pathname === "/" : pathname.startsWith(i.to)));
  if (!item) return { section: "Workspace", title: "Not found" };
  return { section: item.group === "admin" ? "Administration" : "Workspace", title: item.label };
}
