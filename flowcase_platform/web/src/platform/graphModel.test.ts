import { describe, expect, it } from "vitest";
import { vendorPayment } from "./fixtures";
import {
  connect,
  disconnect,
  inspectGraph,
  parseDefinition,
  processEdges,
} from "./graphModel";

describe("Flowcase canonical adapter", () => {
  it("renders real next targets independently of array order and retains parallel join", () => {
    const reversed = {
      ...vendorPayment,
      nodes: [...vendorPayment.nodes].reverse(),
    };
    const edges = processEdges(reversed);
    expect(
      edges
        .filter((e) => e.source === "split")
        .map((e) => e.target)
        .sort(),
    ).toEqual(["document-check", "vendor-lookup"]);
    expect(edges.filter((e) => e.target === "join")).toHaveLength(2);
    expect(
      edges.find((e) => e.source === "match" && e.label === "true")?.target,
    ).toBe("review");
    expect(
      edges.find((e) => e.source === "match" && e.label === "false")?.target,
    ).toBe("exception");
    expect(inspectGraph(reversed)).toEqual([]);
  });
  it("matched path bypasses exception and unmatched path visits it before review", () => {
    const match = vendorPayment.nodes.find((n) => n.id === "match");
    const b = match?.config?.branches as Record<string, string>;
    expect(b.true).toBe("review");
    expect(b.false).toBe("exception");
    expect(vendorPayment.nodes.find((n) => n.id === b.false)?.next).toEqual([
      "review",
    ]);
    expect(
      vendorPayment.nodes.find((n) => n.id === "review")?.config?.return_to,
    ).toBe("extract");
  });
  it("reconnects one condition atomically without changing the other branch or config", () => {
    const d = connect(vendorPayment, "match", "end", "true");
    const match = d.nodes.find((n) => n.id === "match");
    expect(match?.next.sort()).toEqual(["end", "exception"]);
    expect(match?.config?.branches).toEqual({
      true: "end",
      false: "exception",
    });
    expect(match?.config?.expression).toEqual(
      vendorPayment.nodes.find((n) => n.id === "match")?.config?.expression,
    );
    expect(vendorPayment.nodes.find((n) => n.id === "match")?.next).toEqual([
      "review",
      "exception",
    ]);
  });
  it("deletion removes both semantic references and reports a nonexhaustive branch", () => {
    const d = disconnect(vendorPayment, "match", "exception");
    expect(d.nodes.find((n) => n.id === "match")?.next).toEqual(["review"]);
    expect(d.nodes.find((n) => n.id === "match")?.config?.branches).toEqual({
      true: "review",
    });
    expect(
      inspectGraph(d).some(
        (i) => i.node === "match" && i.message.includes("True and false"),
      ),
    ).toBe(true);
  });
  it("preserves all canonical fields and unknown metadata on semantic roundtrip", () => {
    const original = {
      ...vendorPayment,
      metadata: {
        ...vendorPayment.metadata,
        extension: { policy: "pinned@4" },
      },
      future_contract: { opaque: true },
    };
    expect(parseDefinition(JSON.stringify(original))).toEqual(original);
    const edited = connect(original, "post", "review");
    expect(edited.input_schema).toEqual(original.input_schema);
    expect(edited.policy_refs).toEqual(original.policy_refs);
    expect(edited.metadata).toEqual(original.metadata);
    expect("future_contract" in edited).toBe(true);
    expect(JSON.stringify(edited)).not.toContain("position");
  });
  it("diagnoses cycles, orphan nodes and unsupported runtime types without inventing execution", () => {
    const cycle = connect(vendorPayment, "post", "review");
    expect(inspectGraph(cycle).some((i) => i.message.includes("cycle"))).toBe(
      true,
    );
    const d = {
      ...vendorPayment,
      nodes: [
        ...vendorPayment.nodes,
        {
          id: "orphan",
          name: "Nested flow",
          type: "subworkflow" as const,
          next: [],
        },
      ],
    };
    expect(
      inspectGraph(d)
        .filter((i) => i.node === "orphan")
        .map((i) => i.message),
    ).toEqual(
      expect.arrayContaining([
        "Subworkflow is not supported by runtime 1.0.",
        "This node is unreachable from start.",
      ]),
    );
  });
  it("rejects non-native imports and malformed next targets", () => {
    expect(() => parseDefinition('{"nodes":[],"connections":{}}')).toThrow();
    expect(() =>
      parseDefinition(
        JSON.stringify({
          ...vendorPayment,
          nodes: [{ id: "x", type: "start", name: "Bad", next: [42] }],
        }),
      ),
    ).toThrow();
  });
  it("rejects self links, process links into start and links out of end", () => {
    expect(connect(vendorPayment, "start", "start")).toBe(vendorPayment);
    expect(connect(vendorPayment, "post", "start")).toBe(vendorPayment);
    expect(connect(vendorPayment, "end", "review")).toBe(vendorPayment);
  });
});
