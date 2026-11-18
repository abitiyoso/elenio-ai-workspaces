import type { WorkflowDefinition, WorkflowNode } from "../api/workflows";
export type Positions = Record<string, { x: number; y: number }>;
export interface ProcessEdge {
  id: string;
  source: string;
  target: string;
  label: string;
}

export function processEdges(def: WorkflowDefinition): ProcessEdge[] {
  return def.nodes.flatMap((n) =>
    n.next.map((target) => ({
      id: `${n.id}::${target}`,
      source: n.id,
      target,
      label:
        n.type === "condition"
          ? Object.entries((n.config?.branches ?? {}) as Record<string, string>)
              .filter(([, v]) => v === target)
              .map(([k]) => k)
              .join(" / ")
          : n.type === "parallel_split"
            ? "parallel"
            : "",
    })),
  );
}
/** Preserve every canonical field. Changing a branch also changes next atomically. */
export function connect(
  def: WorkflowDefinition,
  source: string,
  target: string,
  branch?: string,
): WorkflowDefinition {
  const s = def.nodes.find((n) => n.id === source),
    t = def.nodes.find((n) => n.id === target);
  if (!s || !t || source === target || s.type === "end" || t.type === "start")
    return def;
  return {
    ...def,
    nodes: def.nodes.map((n) => {
      if (n.id !== source) return n;
      if (n.type === "condition") {
        if (branch !== "true" && branch !== "false") return n;
        const branches = {
          ...(n.config?.branches as Record<string, string>),
          [branch]: target,
        };
        return {
          ...n,
          config: { ...n.config, branches },
          next: [...new Set(Object.values(branches))],
        };
      }
      return {
        ...n,
        next:
          n.type === "parallel_split"
            ? [...new Set([...n.next, target])]
            : [target],
      };
    }),
  };
}
export function disconnect(
  def: WorkflowDefinition,
  source: string,
  target: string,
): WorkflowDefinition {
  return {
    ...def,
    nodes: def.nodes.map((n) =>
      n.id !== source
        ? n
        : {
            ...n,
            next: n.next.filter((x) => x !== target),
            ...(n.type === "condition"
              ? {
                  config: {
                    ...n.config,
                    branches: Object.fromEntries(
                      Object.entries(
                        (n.config?.branches as Record<string, string>) ?? {},
                      ).filter(([, v]) => v !== target),
                    ),
                  },
                }
              : {}),
          },
    ),
  };
}
export function patchNode(
  def: WorkflowDefinition,
  id: string,
  patch: Partial<WorkflowNode>,
): WorkflowDefinition {
  return {
    ...def,
    nodes: def.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)),
  };
}
export function inspectGraph(
  def: WorkflowDefinition,
): { node: string; message: string }[] {
  const issues: { node: string; message: string }[] = [],
    ids = new Set(def.nodes.map((n) => n.id));
  if (ids.size !== def.nodes.length)
    issues.push({ node: "", message: "Duplicate node IDs." });
  if (def.nodes.filter((n) => n.type === "start").length !== 1)
    issues.push({ node: "", message: "Exactly one start node is required." });
  if (!def.nodes.some((n) => n.type === "end"))
    issues.push({ node: "", message: "An end node is required." });
  for (const n of def.nodes) {
    for (const next of n.next)
      if (!ids.has(next))
        issues.push({ node: n.id, message: `Missing target: ${next}.` });
    if (n.type === "subworkflow")
      issues.push({
        node: n.id,
        message: "Subworkflow is not supported by runtime 1.0.",
      });
    if (n.type === "end" && n.next.length)
      issues.push({
        node: n.id,
        message: "End cannot have outgoing connections.",
      });
    if (n.type !== "end" && !n.next.length)
      issues.push({ node: n.id, message: "Connect this step to a next step." });
    if (
      !["condition", "parallel_split", "end"].includes(n.type) &&
      n.next.length > 1
    )
      issues.push({
        node: n.id,
        message: "This step supports one outgoing process connection.",
      });
    if (n.type === "condition") {
      const b = n.config?.branches as Record<string, string> | undefined;
      if (
        !b?.true ||
        !b.false ||
        Object.keys(b).some((k) => !["true", "false"].includes(k)) ||
        [b.true, b.false].some((v) => !n.next.includes(v)) ||
        n.next.some((v) => !Object.values(b).includes(v))
      )
        issues.push({
          node: n.id,
          message: "True and false branches must agree with next.",
        });
    }
    if (n.type === "parallel_split" && n.next.length < 2)
      issues.push({
        node: n.id,
        message: "Parallel split needs at least two branches.",
      });
    if (
      n.type === "parallel_join" &&
      (n.config?.join !== "all" ||
        !def.nodes.some(
          (s) => s.id === n.config?.split_ref && s.type === "parallel_split",
        ))
    )
      issues.push({
        node: n.id,
        message: "Join requires all and a valid split reference.",
      });
  }
  const visited = new Set<string>(),
    active = new Set<string>();
  function visit(id: string) {
    if (active.has(id)) {
      issues.push({
        node: id,
        message: "Process connections contain a cycle.",
      });
      return;
    }
    if (visited.has(id)) return;
    visited.add(id);
    active.add(id);
    def.nodes
      .find((n) => n.id === id)
      ?.next.filter((x) => ids.has(x))
      .forEach(visit);
    active.delete(id);
  }
  def.nodes.filter((n) => n.type === "start").forEach((n) => visit(n.id));
  for (const n of def.nodes)
    if (!visited.has(n.id))
      issues.push({
        node: n.id,
        message: "This node is unreachable from start.",
      });
  return issues;
}
export function parseDefinition(text: string): WorkflowDefinition {
  const value: unknown = JSON.parse(text);
  if (!value || typeof value !== "object")
    throw new Error("Expected a Flowcase definition object.");
  const d = value as WorkflowDefinition;
  if (
    d.schema_version !== "1.0" ||
    typeof d.key !== "string" ||
    typeof d.name !== "string" ||
    !Number.isInteger(d.version) ||
    !Array.isArray(d.nodes) ||
    d.nodes.length > 200
  )
    throw new Error(
      "Expected schema 1.0, key, name, integer version and up to 200 nodes.",
    );
  const types = [
    "start",
    "end",
    "human_task",
    "agent_task",
    "service_task",
    "human_approval",
    "condition",
    "parallel_split",
    "parallel_join",
    "timer",
    "subworkflow",
  ];
  if (
    d.nodes.some(
      (n) =>
        !n ||
        typeof n.id !== "string" ||
        typeof n.name !== "string" ||
        !types.includes(n.type) ||
        !Array.isArray(n.next) ||
        n.next.some((t) => typeof t !== "string"),
    )
  )
    throw new Error(
      "Every node needs id, name, supported schema type and string next targets.",
    );
  return d;
}
