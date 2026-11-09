import type { IconName } from "../components/Icon";

export interface NavItem {
  path: string;
  label: string;
  icon: IconName;
  group: "workspace" | "platform";
}

export const navItems: NavItem[] = [
  { path: "/", label: "Overview", icon: "grid", group: "workspace" },
  { path: "/workflows", label: "Workflow Library", icon: "workflow", group: "workspace" },
  { path: "/designer", label: "Workflow Designer", icon: "layers", group: "workspace" },
  { path: "/cases", label: "Cases", icon: "briefcase", group: "workspace" },
  { path: "/tasks", label: "My Tasks", icon: "check-square", group: "workspace" },
  { path: "/approvals", label: "Approvals", icon: "shield-check", group: "workspace" },
  { path: "/integrations", label: "Integrations", icon: "plug", group: "platform" },
  { path: "/audit", label: "Activity & Audit", icon: "clock", group: "platform" },
];

export function labelFor(pathname: string): string {
  return navItems.find((n) => (n.path === "/" ? pathname === "/" : pathname.startsWith(n.path)))?.label ?? "Not found";
}
