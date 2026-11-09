import type { CaseDetail, DraftReview } from "../api/cases";

// Derived author-draft-review state shared by the case page and its review panel.

const liveStatuses = ["ready", "running", "waiting_external"];

/** Steps whose draft was sent back and whose agent is re-running, with the iteration in progress. */
export function revisingSteps(c: CaseDetail): { nodeId: string; name: string; iteration: number }[] {
  const latest = new Map<string, DraftReview>();
  for (const d of c.drafts ?? []) if (!latest.has(d.node_id)) latest.set(d.node_id, d);
  const out: { nodeId: string; name: string; iteration: number }[] = [];
  for (const d of latest.values()) {
    if (d.status !== "revision_requested") continue;
    const run = [...c.timeline].reverse().find((x) => x.node_id === d.node_id && liveStatuses.includes(x.status));
    if (run) out.push({ nodeId: d.node_id, name: d.node_name || d.node_id, iteration: run.iteration });
  }
  return out;
}

/** Whether the review panel belongs at the top of the case: a draft waits or is being revised. */
export function draftReviewActive(c: CaseDetail): boolean {
  return (c.drafts ?? []).some((d) => d.status === "pending") || revisingSteps(c).length > 0;
}
