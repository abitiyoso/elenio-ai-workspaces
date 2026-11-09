import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { loadManifest, me, mockApi, packSummary, renderAt } from "./fixtures";

const ok = loadManifest("poc-assessment.v1.json");
const broken = loadManifest("contract-review.v1.json");

describe("Domain Pack Library", () => {
  it("lists packs with validation errors (JSON pointer paths) and live reasons", async () => {
    mockApi({
      "/me/context": [200, me],
      "/packs": [200, { items: [
        packSummary(ok),
        packSummary(broken, {
          validation: { schema_ok: true, semantic_ok: false, errors: [{ path: "/navigation/2/artifact_type", code: "unknown_artifact_type", message: "Navigation references an undeclared artifact type." }] },
          live: { startable: false, reasons: ["Flowcase workflow legal-review is not published"] },
        }),
      ] }],
    });
    renderAt("/packs");
    const errors = await screen.findByRole("list", { name: `Validation errors for ${broken.title}` });
    expect(within(errors).getByText("/navigation/2/artifact_type")).toBeInTheDocument();
    expect(within(errors).getByText(/undeclared artifact type/)).toBeInTheDocument();
    expect(screen.getByText("Flowcase workflow legal-review is not published")).toBeInTheDocument();
    expect(screen.getByText("Validation passed")).toBeInTheDocument();
    expect(screen.getAllByText("0123456789ab").length).toBe(2);
  });

  it("shows the manifest as escaped text and previews navigation and fields", async () => {
    const hostile = { ...ok, description: "<img src=x onerror=alert(1)>" };
    mockApi({
      "/me/context": [200, me],
      [`/packs/${encodeURIComponent(ok.pack_key)}/versions/${ok.version}`]: [200, { pack: packSummary(ok), manifest: hostile }],
    });
    const user = userEvent.setup();
    const { container } = renderAt(`/packs/${encodeURIComponent(ok.pack_key)}/${ok.version}`);
    const code = await screen.findByLabelText("Manifest JSON");
    expect(code.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(container.querySelector("img")).toBeNull();
    const preview = screen.getByRole("navigation", { name: "Navigation preview" });
    const listNav = (ok.navigation ?? []).find((n) => n.view === "artifact_list");
    await user.click(within(preview).getByRole("button", { name: new RegExp(listNav?.label ?? "") }));
    const type = (ok.artifact_types ?? []).find((t) => t.key === listNav?.artifact_type);
    const table = screen.getByRole("table", { name: `Fields of ${type?.title}` });
    for (const f of type?.fields ?? []) expect(within(table).getByText(f.label)).toBeInTheDocument();
  });
});
