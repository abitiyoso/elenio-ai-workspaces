// Shared test helpers: fetch mock, app renderer and fixtures built from the real domain-pack manifests.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { StrictMode } from "react";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import type { CaseCard, CaseContext, MeContext, PackArtifactType, PackManifest, PackSummary } from "../api/types";
import { App } from "../app/App";

export const API = "/api/v1/workspace";

type Reply = [number, unknown, Record<string, string>?];
type Route = Reply | ((init: RequestInit) => Reply);
export interface Call { method: string; url: string; init: RequestInit }

/** Mocks fetch with `"METHOD /path"` or `"/path"` routes (paths relative to the workspace API root). */
export function mockApi(routes: Record<string, Route>) {
  const calls: Call[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init = {}) => {
    const url = String(input);
    const method = init.method ?? "GET";
    calls.push({ method, url, init });
    const rel = url.startsWith(API) ? url.slice(API.length) : url;
    const route = routes[`${method} ${rel}`] ?? routes[rel];
    const [status, body, headers] = typeof route === "function" ? route(init) : route ?? [404, { error: { code: "not_found", message: "nope", trace_id: "t" } }];
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ETag: '"1"', ...headers } });
  });
  return calls;
}

export const headersOf = (c?: Call) => (c?.init.headers ?? {}) as Record<string, string>;
export const bodyOf = (c?: Call) => (c?.init.body ? JSON.parse(String(c.init.body)) as Record<string, unknown> : null);

export function renderAt(path: string) {
  return render(<StrictMode><MemoryRouter initialEntries={[path]}><App /></MemoryRouter></StrictMode>);
}

export const me: MeContext = {
  user_id: "u-1", display_name: "Maya Putri", tenant_key: "acme-demo", workspace_key: "demo-workspace",
  platform_roles: ["case_worker"], flowcase: { available: true, roles: ["case_owner"], can_create_cases: true }, partial_errors: [],
};

/** Loads a manifest from the repository's domain-packs folder (the same files the BFF serves). */
export function loadManifest(file: string): PackManifest {
  return JSON.parse(readFileSync(resolve(process.cwd(), "../domain-packs", file), "utf8")) as PackManifest;
}

export const manifestFiles = ["sim-tender.v1.json", "contract-review.v1.json", "poc-assessment.v1.json"];

/** The artifact type with a row source: keeps the manifest's own rows_path, or adds a fixture one. */
export function withRows(t: PackArtifactType): PackArtifactType {
  return { ...t, source: { provider: "flowcase", flowcase_artifact_type: `${t.key}-doc`, ...t.source, rows_path: t.source?.rows_path ?? "/artifact_draft/rows" } };
}

export function rowFor(t: PackArtifactType, i: number, sourceId: string): Record<string, unknown> {
  const row: Record<string, unknown> = {};
  for (const f of t.fields) {
    if (f.widget === "evidence_ref") row[f.key] = [sourceId];
    else if (f.widget === "select") row[f.key] = f.options?.[i % (f.options.length || 1)] ?? "Option";
    else if (f.widget === "number") row[f.key] = 40 + i;
    else if (f.widget === "boolean") row[f.key] = i % 2 === 0;
    else row[f.key] = `${f.label} value ${i + 1}`;
  }
  return row;
}

/** Content with `rows` placed at the type's rows_path (JSON pointer or dotted path). */
export function contentWithRows(t: PackArtifactType, rows: Record<string, unknown>[]): Record<string, unknown> {
  const path = t.source?.rows_path ?? "/artifact_draft/rows";
  const segs = path.startsWith("/") ? path.slice(1).split("/") : path.split(".");
  const root: Record<string, unknown> = {};
  let cur = root;
  segs.forEach((s, i) => {
    if (i === segs.length - 1) cur[s] = rows;
    else cur = (cur[s] = {}) as Record<string, unknown>;
  });
  return root;
}

export function packSummary(m: PackManifest, over: Partial<PackSummary> = {}): PackSummary {
  return {
    pack_key: m.pack_key, version: m.version, title: m.title, description: m.description ?? "", status: m.status ?? "published",
    checksum: "sha256:0123456789abcdef0123", brand: m.brand ?? {}, flowcase_workflow_key: "wf-fixture",
    validation: { schema_ok: true, semantic_ok: true, errors: [] }, live: { startable: true, reasons: [] }, has_intake: true, ...over,
  };
}

export const sourceId = "9cea8321-b363-4d27-8b0d-fa967570c011";
export const source = { source_id: sourceId, version_id: "v-1", title: "Master Services Agreement.pdf", citation: "p. 4 §2.1", anchor: "p4" };

export function caseCard(over: Partial<CaseCard> = {}): CaseCard {
  return {
    id: "case-1", reference: "CASE-2026-0002", title: "Regional network upgrade", status: "active", outcome: null,
    workflow_key: "wf-fixture", workflow_version: 3, owner_name: "Maya Putri", current_steps: ["Draft review"],
    open_tasks: 1, pending_approvals: 1, pending_drafts: 1, progress: 40, created_at: "2026-10-30T08:00:00Z", updated_at: "2026-10-31T08:00:00Z",
    pack: null, ...over,
  };
}

/** A full case context for a manifest: navigation and artifact types come straight from it. */
export function caseFor(m: PackManifest, over: Partial<CaseContext> = {}): CaseContext {
  const types = (m.artifact_types ?? []).map(withRows);
  return {
    case: { ...caseCard({ pack: { pack_key: m.pack_key, version: m.version, title: m.title, brand: m.brand ?? {} } }), variables: { customer_name: "PT Contoh", deadline: "2026-12-15" }, business_facets: {} },
    pack: { pack_key: m.pack_key, version: m.version, title: m.title, brand: m.brand ?? {}, binding: "pinned_at_creation", bound_at: "2026-10-30T08:00:00Z", status: "ok" },
    navigation: (m.navigation ?? []).map((n) => ({ ...n, id: n.key ?? n.view })),
    artifact_types: types,
    workflow: {
      version: 3,
      nodes: [
        { id: "draft", name: "Draft generation", type: "agent_task", status: "completed" },
        { id: "gate-a", name: "Readiness approval", type: "human_approval", status: "waiting_approval" },
        { id: "final", name: "Final release", type: "human_task", status: "pending" },
      ],
    },
    artifacts: types.map((t, i) => ({
      id: `art-${t.key}`, type: t.source?.flowcase_artifact_type ?? t.key, pack_artifact_type: t.key, version: 2, status: "ready_for_review",
      producer_node_id: "draft", content: contentWithRows(t, [rowFor(t, 0, sourceId), rowFor(t, 1, sourceId)]), sources: [source],
      updated_at: `2026-10-31T0${i}:00:00Z`,
    })),
    drafts: [], tasks: [], approvals: [], agent_runs: [{ node_id: "draft", provider: "agent_studio", status: "succeeded", attempt: 1, created_at: "2026-10-31T07:00:00Z" }],
    history: [{ action: "case_created", actor_name: "Maya Putri", actor_type: "user", occurred_at: "2026-10-30T08:00:00Z", data: { note: "Created from the Case Workspace intake chat (4 messages)" } }],
    permissions: { can_review_drafts: true, can_note: true },
    links: { flowcase_case: "/apps/flowcase/cases/case-1", flowcase_approvals: "/apps/flowcase/approvals" },
    partial_errors: [],
    ...over,
  };
}
