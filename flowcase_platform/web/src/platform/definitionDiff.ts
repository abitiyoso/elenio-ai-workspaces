import type { WorkflowDefinition } from "../api/workflows";
export function definitionDiff(
  before: WorkflowDefinition,
  after: WorkflowDefinition,
) {
  const old = new Map(before.nodes.map((n) => [n.id, n])),
    next = new Map(after.nodes.map((n) => [n.id, n]));
  return {
    added: after.nodes.filter((n) => !old.has(n.id)).map((n) => n.id),
    removed: before.nodes.filter((n) => !next.has(n.id)).map((n) => n.id),
    changed: after.nodes
      .filter(
        (n) =>
          old.has(n.id) && JSON.stringify(old.get(n.id)) !== JSON.stringify(n),
      )
      .map((n) => n.id),
    fields: [
      ...new Set([...Object.keys(before), ...Object.keys(after)]),
    ].filter(
      (key) =>
        key !== "nodes" &&
        JSON.stringify((before as unknown as Record<string, unknown>)[key]) !==
          JSON.stringify((after as unknown as Record<string, unknown>)[key]),
    ),
  };
}
