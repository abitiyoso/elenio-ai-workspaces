import { api } from "../api/endpoints";
import type { Me, Workspace } from "../api/types";
import { useApi } from "./useApi";

export type AdminWorkspace = Workspace & { member_count: number };

/**
 * Workspace names for admin screens: the admin list when the caller may read it, merged with the
 * caller's own memberships. Errors are tolerated (names fall back to ids).
 */
export function useWorkspaceDirectory(me: Me, enabled: boolean) {
  const res = useApi<{ items: AdminWorkspace[] }>(enabled ? api.admin.workspaces : null);
  const byId = new Map<string, Workspace>();
  for (const w of me.workspaces) byId.set(w.id, w);
  for (const w of res.data?.items ?? []) byId.set(w.id, w);
  const name = (id: string) => byId.get(id)?.name ?? `${id.slice(0, 8)}…`;
  const managed = me.capabilities.manage_memberships.map((id) => ({ id, name: name(id), key: byId.get(id)?.key ?? "" }));
  /** Every workspace name known to the caller (admin list merged with own memberships). */
  const all = Array.from(byId.values());
  return { byId, name, managed, all, list: res.data?.items, state: res };
}
