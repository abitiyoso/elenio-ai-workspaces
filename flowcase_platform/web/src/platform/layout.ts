import type { WorkflowDefinition } from "../api/workflows";
import { processEdges, type Positions } from "./graphModel";
export async function layoutDefinition(
  def: WorkflowDefinition,
): Promise<Positions> {
  const { default: ELK } = await import("elkjs/lib/elk.bundled.js");
  const layout = await new ELK().layout({
    id: "root",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "RIGHT",
      "elk.spacing.nodeNode": "150",
      "elk.layered.spacing.nodeNodeBetweenLayers": "85",
      "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
    },
    children: def.nodes.map((n) => ({ id: n.id, width: 238, height: 125 })),
    edges: processEdges(def).map((e) => ({
      id: e.id,
      sources: [e.source],
      targets: [e.target],
    })),
  });
  return Object.fromEntries(
    (layout.children ?? []).map((n) => [n.id, { x: n.x ?? 0, y: n.y ?? 0 }]),
  );
}
