import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { CaseContext, NavItem, PackArtifactType } from "../api/types";
import { ArtifactListModule } from "../features/workspace/modules/ArtifactList";
import { caseFor, loadManifest, manifestFiles, rowFor, sourceId } from "./fixtures";

function renderModule(ctx: CaseContext, nav: NavItem) {
  return render(<MemoryRouter><ArtifactListModule ctx={ctx} nav={nav} /></MemoryRouter>);
}

describe("generic artifact_list module", () => {
  it("renders each pack's own columns and row fields with the same component", () => {
    const headerSets: string[][] = [];
    for (const file of manifestFiles) {
      const manifest = loadManifest(file);
      const ctx = caseFor(manifest);
      const nav = (manifest.navigation ?? []).find((n) => n.view === "artifact_list") as NavItem;
      const type = ctx.artifact_types.find((t) => t.key === nav.artifact_type) as PackArtifactType;
      const { unmount } = renderModule(ctx, nav);
      const table = screen.getByRole("table", { name: type.title });
      const headers = within(table).getAllByRole("columnheader").map((h) => h.textContent ?? "");
      const expected = [
        ...type.fields.filter((f) => f.widget !== "evidence_ref").slice(0, 4).map((f) => f.label),
        ...type.fields.filter((f) => f.widget === "evidence_ref").slice(0, 1).map((f) => f.label),
        "Inspect",
      ];
      expect(headers).toEqual(expected);
      // Row cells hold the fixture values of the manifest's own fields, and evidence refs read as source titles.
      const first = rowFor(type, 0, sourceId);
      const textField = type.fields.find((f) => f.widget === "text" || f.widget === "textarea");
      if (textField) expect(within(table).getAllByText(String(first[textField.key])).length).toBeGreaterThan(0);
      expect(within(table).getAllByText("Master Services Agreement.pdf").length).toBe(2);
      expect(table.textContent).not.toContain(sourceId);
      headerSets.push(headers);
      unmount();
    }
    expect(new Set(headerSets.map((h) => h.join("|"))).size).toBe(manifestFiles.length);
  });

  it("renders a whole-document artifact type with the artifact renderer when the manifest has no rows_path", () => {
    const manifest = loadManifest("poc-assessment.v1.json");
    const ctx = caseFor(manifest);
    const t = ctx.artifact_types[0] as PackArtifactType;
    ctx.artifact_types = [{ ...t, source: { provider: "flowcase", flowcase_artifact_type: "plan" } }];
    ctx.artifacts = [{ id: "a", type: "plan", pack_artifact_type: t.key, version: 3, status: "ready_for_review", producer_node_id: "draft",
      content: { artifact_draft: { summary: "Whole plan text", evidence_refs: [sourceId] }, sources: [{ source_id: sourceId, title: "Lab handbook" }] } }];
    const nav = (manifest.navigation ?? []).find((n) => n.artifact_type === t.key) as NavItem;
    renderModule(ctx, nav);
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText("Whole plan text")).toBeInTheDocument();
    expect(screen.getAllByText("Lab handbook").length).toBeGreaterThan(0);
    expect(screen.getByText("v3")).toBeInTheDocument();
    expect(screen.getByText("AI draft — needs human review")).toBeInTheDocument();
  });

  it("shows which step produces an artifact that does not exist yet", () => {
    const manifest = loadManifest("contract-review.v1.json");
    const ctx = caseFor(manifest);
    const t = ctx.artifact_types[1] as PackArtifactType;
    ctx.artifacts = [];
    ctx.drafts = [{ id: "d", node_id: "n", node_name: "Exception analysis", artifact_type: t.source?.flowcase_artifact_type ?? "", status: "cancelled", version_num: 1 }];
    const nav = (manifest.navigation ?? []).find((n) => n.artifact_type === t.key) as NavItem;
    renderModule(ctx, nav);
    expect(screen.getByText("Produced by Exception analysis — not yet available.")).toBeInTheDocument();
  });
});
