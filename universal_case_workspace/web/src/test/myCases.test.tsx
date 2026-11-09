import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { caseCard, loadManifest, me, mockApi, packSummary, renderAt } from "./fixtures";

const packA = loadManifest("contract-review.v1.json");
const packB = loadManifest("poc-assessment.v1.json");
const cards = [
  caseCard({ id: "c1", reference: "CASE-2026-0001", title: "Data processing agreement", status: "under_review", pending_drafts: 2, pending_approvals: 1, open_tasks: 3, progress: 55,
    pack: { pack_key: packA.pack_key, version: packA.version, title: packA.title, brand: { accent: "#2864EC", icon: "file-check-2" } } }),
  caseCard({ id: "c2", reference: "CASE-2026-0002", title: "Search accuracy PoC", status: "active", current_steps: ["Experiment design"],
    pack: { pack_key: packB.pack_key, version: packB.version, title: packB.title, brand: {} } }),
];

describe("My Cases", () => {
  it("lists case cards with pack badge, counts and progress, and filters by pack", async () => {
    mockApi({ "/me/context": [200, me], "/cases": [200, { items: cards, next_cursor: null, partial_errors: [] }] });
    const user = userEvent.setup();
    renderAt("/");
    const card = (await screen.findByRole("link", { name: "Data processing agreement" })).closest("article");
    expect(card).not.toBeNull();
    const c = within(card as HTMLElement);
    expect(c.getByText("CASE-2026-0001")).toBeInTheDocument();
    expect(c.getByText(packA.title)).toBeInTheDocument();
    expect(c.getByText("Under review")).toBeInTheDocument();
    expect(c.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "55");
    expect(card?.textContent).toMatch(/2 drafts/);
    expect(card?.textContent).toMatch(/1 approvals/);
    expect(card?.textContent).toMatch(/3 tasks/);
    expect(c.getByRole("link")).toHaveAttribute("href", "/cases/c1");

    await user.selectOptions(screen.getByLabelText("Domain pack"), packB.pack_key);
    expect(screen.queryByText("Data processing agreement")).not.toBeInTheDocument();
    expect(screen.getByText("Search accuracy PoC")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Table/ }));
    expect(screen.getByRole("table", { name: "My cases" })).toBeInTheDocument();
  });

  it("shows a partial error banner when Flowcase is unavailable", async () => {
    mockApi({
      "/me/context": [200, me],
      "/cases": [200, { items: [], next_cursor: null, partial_errors: [{ source: "flowcase", code: "flowcase_unavailable", message: "Flowcase timed out." }] }],
    });
    renderAt("/");
    const banner = await screen.findByRole("alert");
    expect(banner).toHaveTextContent("Flowcase unavailable — showing nothing");
    expect(banner).toHaveTextContent("Flowcase timed out.");
  });

  it("New case goes straight to the intake chat when only one pack is startable", async () => {
    const calls = mockApi({
      "/me/context": [200, me],
      "/cases": [200, { items: cards, next_cursor: null }],
      "/packs": [200, { items: [packSummary(packA), packSummary(packB, { live: { startable: false, reasons: ["Workflow not live"] } })] }],
      "POST /intake-sessions": [403, { error: { code: "cannot_create_cases", message: "stop here", trace_id: "t" } }],
    });
    const user = userEvent.setup();
    renderAt("/");
    await user.click(await screen.findByRole("button", { name: /New case/ }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.url.endsWith("/intake-sessions"))).toBe(true));
    const post = calls.find((c) => c.method === "POST");
    expect(JSON.parse(String(post?.init.body))).toEqual({ pack_key: packA.pack_key });
    expect(await screen.findByText(/stop here/)).toBeInTheDocument();
  });

  it("offers a pack picker when several packs are startable", async () => {
    mockApi({
      "/me/context": [200, me],
      "/cases": [200, { items: [], next_cursor: null }],
      "/packs": [200, { items: [packSummary(packA), packSummary(packB), packSummary(packB, { pack_key: "x.no-intake", title: "No intake", has_intake: false })] }],
    });
    const user = userEvent.setup();
    renderAt("/");
    await user.click(await screen.findByRole("button", { name: /New case/ }));
    const dialog = await screen.findByRole("dialog", { name: "Start a new case" });
    expect(within(dialog).getAllByRole("link")).toHaveLength(2);
    expect(within(dialog).getByRole("link", { name: new RegExp(packA.title) })).toHaveAttribute("href", `/cases/new?pack=${encodeURIComponent(packA.pack_key)}`);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("asks to reload from the Control Plane when the session ended", async () => {
    mockApi({ "/me/context": [401, { error: { code: "unauthenticated", message: "no session", trace_id: "t" } }], "/cases": [401, { error: { code: "unauthenticated", message: "no", trace_id: "t" } }] });
    renderAt("/");
    expect(await screen.findByText(/Your session ended — reload from the Control Plane/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open the Control Plane" })).toHaveAttribute("href", "/");
  });
});
