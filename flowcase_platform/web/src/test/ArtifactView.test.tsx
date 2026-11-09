import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ArtifactView } from "../components/ArtifactView";
import { unwrapAgentEnvelope } from "../components/artifactContent";

const envelope = {
  model_mode: "provider",
  sources: [{ source_id: "9cea8321-b363-4d27-8b0d-fa967570c011", version_id: "v1", title: "Telkom Digital DPI — Katalog Use Case", citation: "p.2 §11" }],
  review: { status: "ready_for_review", validation: { schema_valid: true, risk_flags: [], cited_sources: ["9cea8321-b363-4d27-8b0d-fa967570c011"] } },
  artifact_draft: {
    requirements: [
      { id: "R-001", category: "functional", mandatory: false, statement: "Asisten AI menjawab dari SOP.", evidence_refs: ["9cea8321-b363-4d27-8b0d-fa967570c011"] },
      { id: "R-004", category: "compliance", mandatory: true, statement: "Data tetap on-premise.", evidence_refs: [] },
    ],
    ambiguities: [{ question: "Berapa target ketersediaan?", reason: "KAK belum diterima.", evidence_refs: [] }],
  },
};

describe("ArtifactView", () => {
  it("unwraps the Agent Studio envelope", () => {
    const { draft, review, modelMode } = unwrapAgentEnvelope(envelope);
    expect(Object.keys(draft)).toEqual(["requirements", "ambiguities"]);
    expect(review?.status).toBe("ready_for_review");
    expect(modelMode).toBe("provider");
  });

  it("renders requirements and questions as readable items, not JSON", () => {
    render(<ArtifactView content={envelope} />);
    expect(screen.getByText("Requirements")).toBeInTheDocument();
    expect(screen.getByText("Asisten AI menjawab dari SOP.")).toBeInTheDocument();
    expect(screen.getByText("R-004")).toBeInTheDocument();
    expect(screen.getByText("Clarification questions")).toBeInTheDocument();
    expect(screen.getByText("Berapa target ketersediaan?")).toBeInTheDocument();
    expect(screen.getAllByText("Telkom Digital DPI — Katalog Use Case").length).toBe(2); // pill + Sources list
    expect(screen.queryByText("KMS 9cea8321")).not.toBeInTheDocument();
    expect(screen.getAllByText("no source").length).toBeGreaterThan(0);
    expect(screen.getByText("schema valid")).toBeInTheDocument();
    expect(screen.getByText("real model")).toBeInTheDocument();
  });

  it("falls back to short ids when the result carries no titles", () => {
    const { sources: _ignored, ...older } = envelope;
    void _ignored;
    render(<ArtifactView content={older} />);
    expect(screen.getByText("KMS 9cea8321")).toBeInTheDocument();
  });

  it("renders plain human content without an envelope", () => {
    render(<ArtifactView content={{ summary: "Ringkasan manual" }} />);
    expect(screen.getByText("Ringkasan manual")).toBeInTheDocument();
  });
});
