import type { PackSummary } from "../../api/types";

/** Packs a user can start a case from: they declare an intake and their bound workflow/agents are live. */
export function startablePacks(items: PackSummary[]): PackSummary[] {
  return items.filter((p) => p.has_intake && p.live?.startable);
}
