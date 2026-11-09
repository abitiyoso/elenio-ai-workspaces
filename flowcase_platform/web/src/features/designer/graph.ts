import type { Diagnostic, NodeType, WorkflowDefinition, WorkflowNode } from "../../api/workflows";

/**
 * Layered layout: each node sits on the row of its longest path from start, so parallel branches
 * share a row and joins sit below every branch. Nodes outside the acyclic reachable part (orphans,
 * cycles in a broken draft) go to a final row so nothing disappears while editing.
 */
export function layoutRows(def: WorkflowDefinition): string[][] {
  const ids = new Set(def.nodes.map((n) => n.id));
  const succ = new Map<string, string[]>();
  for (const n of def.nodes) {
    if (!succ.has(n.id)) succ.set(n.id, n.next.filter((t) => ids.has(t) && t !== n.id));
  }
  const hasPred = new Set([...succ.values()].flat());
  const starts = def.nodes.filter((n) => n.type === "start").map((n) => n.id);
  const roots = starts.length > 0 ? starts : [...succ.keys()].filter((id) => !hasPred.has(id));

  // Only edges between nodes reachable from the roots count, so orphans cannot hold nodes back.
  const reach = new Set<string>();
  const stack = [...roots];
  while (stack.length > 0) {
    const id = stack.pop() as string;
    if (reach.has(id)) continue;
    reach.add(id);
    stack.push(...(succ.get(id) ?? []));
  }
  const indeg = new Map<string, number>([...reach].map((id) => [id, 0]));
  for (const id of reach) for (const t of succ.get(id) ?? []) indeg.set(t, (indeg.get(t) ?? 0) + 1);

  const level = new Map<string, number>();
  const queue = roots.filter((id) => indeg.get(id) === 0);
  for (const id of queue) level.set(id, 0);
  while (queue.length > 0) {
    const id = queue.shift() as string;
    for (const t of succ.get(id) ?? []) {
      level.set(t, Math.max(level.get(t) ?? 0, (level.get(id) ?? 0) + 1));
      indeg.set(t, (indeg.get(t) ?? 0) - 1);
      if (indeg.get(t) === 0) queue.push(t);
    }
  }
  const rows: string[][] = [];
  const leftovers: string[] = [];
  const seen = new Set<string>();
  for (const n of def.nodes) {
    if (seen.has(n.id)) continue;
    seen.add(n.id);
    const l = level.get(n.id);
    if (l === undefined || !reach.has(n.id) || (indeg.get(n.id) ?? 0) > 0) {
      leftovers.push(n.id);
      continue;
    }
    (rows[l] ??= []).push(n.id);
  }
  const compact = rows.filter((r) => r && r.length > 0);
  if (leftovers.length > 0) compact.push(leftovers);
  return compact;
}

/** Groups diagnostics by node, using node_id or the /nodes/{i} prefix of the JSON pointer. */
export function diagnosticsByNode(def: WorkflowDefinition, diags: Diagnostic[]): Map<string, Diagnostic[]> {
  const out = new Map<string, Diagnostic[]>();
  for (const d of diags) {
    let id = d.node_id;
    const m = /^\/nodes\/(\d+)/.exec(d.path);
    if (!id && m) id = def.nodes[Number(m[1])]?.id;
    if (!id) continue;
    out.set(id, [...(out.get(id) ?? []), d]);
  }
  return out;
}

export type NodeFamily = "human" | "condition" | "agent";

export function nodeFamily(t: NodeType): NodeFamily {
  if (t === "human_task" || t === "human_approval") return "human";
  if (t === "condition" || t === "parallel_split" || t === "parallel_join" || t === "timer") return "condition";
  return "agent";
}

export const nodeTypeLabel: Record<NodeType, string> = {
  start: "Start",
  end: "End",
  human_task: "Human task",
  agent_task: "Agent task",
  service_task: "Service task",
  human_approval: "Approval gate",
  condition: "Condition",
  parallel_split: "Parallel split",
  parallel_join: "Parallel join",
  timer: "Timer",
  subworkflow: "Subworkflow",
};

/** Removes a node and every reference to it (next lists, split_ref, branches, return_to). */
export function removeNode(def: WorkflowDefinition, id: string): WorkflowDefinition {
  const scrub = (n: WorkflowNode): WorkflowNode => {
    const config = n.config
      ? Object.fromEntries(Object.entries(n.config).filter(([k, v]) => !((k === "split_ref" || k === "return_to") && v === id)))
      : undefined;
    if (config) {
      const branches = config.branches;
      if (branches && typeof branches === "object") {
        config.branches = Object.fromEntries(Object.entries(branches as Record<string, unknown>).filter(([, t]) => t !== id));
      }
    }
    return { ...n, next: n.next.filter((t) => t !== id), ...(config ? { config } : {}) };
  };
  return { ...def, nodes: def.nodes.filter((n) => n.id !== id).map(scrub) };
}

/** Inserts a new human task after `afterId` (taking over its single transition), or at the end. */
export function insertStep(def: WorkflowDefinition, afterId: string | null): { def: WorkflowDefinition; id: string } {
  const taken = new Set(def.nodes.map((n) => n.id));
  let i = def.nodes.length;
  while (taken.has(`step_${i}`)) i++;
  const id = `step_${i}`;
  const after = def.nodes.find((n) => n.id === afterId);
  const inherit = after && after.type !== "end" && after.next.length === 1 ? after.next : [];
  const step: WorkflowNode = { id, type: "human_task", name: "New step", config: { assignee_role: "" }, next: [...inherit] };
  const nodes = def.nodes.map((n) => (after && n.id === after.id && inherit.length === 1 ? { ...n, next: [id] } : n));
  const at = after ? nodes.findIndex((n) => n.id === after.id) + 1 : nodes.length;
  nodes.splice(at, 0, step);
  return { def: { ...def, nodes }, id };
}
