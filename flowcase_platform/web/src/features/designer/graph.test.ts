import { describe, expect, it } from "vitest";
import type { WorkflowDefinition, WorkflowNode } from "../../api/workflows";
import { diagnosticsByNode, insertStep, layoutRows, removeNode } from "./graph";

const n = (id: string, type: WorkflowNode["type"], next: string[], config?: Record<string, unknown>): WorkflowNode =>
  ({ id, type, name: id, next, ...(config ? { config } : {}) });

const parallel: WorkflowDefinition = {
  schema_version: "1.0", key: "k", name: "Parallel", version: 1,
  nodes: [
    n("start", "start", ["split"]),
    n("split", "parallel_split", ["a", "b"]),
    n("a", "agent_task", ["a2"]),
    n("a2", "human_approval", ["join"]),
    n("b", "agent_task", ["join"]),
    n("join", "parallel_join", ["end"], { join: "all", split_ref: "split" }),
    n("end", "end", []),
  ],
};

describe("layoutRows", () => {
  it("puts parallel branches side by side and the join below the longest branch", () => {
    expect(layoutRows(parallel)).toEqual([["start"], ["split"], ["a", "b"], ["a2"], ["join"], ["end"]]);
  });

  it("keeps orphans and cycles visible on a trailing row without displacing real nodes", () => {
    const broken: WorkflowDefinition = {
      ...parallel,
      nodes: [...parallel.nodes, n("orphan", "human_task", ["b"]), n("loop1", "human_task", ["loop2"]), n("loop2", "human_task", ["loop1"])],
    };
    const rows = layoutRows(broken);
    expect(rows.slice(0, 6)).toEqual([["start"], ["split"], ["a", "b"], ["a2"], ["join"], ["end"]]);
    expect(rows[6]).toEqual(["orphan", "loop1", "loop2"]);
  });
});

describe("editing helpers", () => {
  it("removes a node with every reference to it", () => {
    const out = removeNode(parallel, "split");
    expect(out.nodes.find((x) => x.id === "start")?.next).toEqual([]);
    expect(out.nodes.find((x) => x.id === "join")?.config).toEqual({ join: "all" });
  });

  it("inserts a step after a single-transition node, taking over its edge", () => {
    const { def, id } = insertStep(parallel, "a");
    expect(def.nodes.find((x) => x.id === "a")?.next).toEqual([id]);
    expect(def.nodes.find((x) => x.id === id)?.next).toEqual(["a2"]);
    expect(def.nodes.map((x) => x.id).indexOf(id)).toBe(def.nodes.map((x) => x.id).indexOf("a") + 1);
  });

  it("maps diagnostics to nodes by node_id or JSON pointer", () => {
    const m = diagnosticsByNode(parallel, [
      { severity: "error", code: "x", message: "", path: "/nodes/2/config/agent_ref" },
      { severity: "warning", code: "y", message: "", path: "/policy_refs/0" },
      { severity: "error", code: "z", message: "", path: "", node_id: "join" },
    ]);
    expect(m.get("a")?.[0]?.code).toBe("x");
    expect(m.get("join")?.[0]?.code).toBe("z");
    expect(m.size).toBe(2);
  });
});
