// Helpers for artifact content produced by Agent Studio ({artifact_draft, review, sources, model_mode}) or by people.
import type { CitedSource } from "../api/types";

export type Json = string | number | boolean | null | Json[] | { [key: string]: Json };

export function isObject(v: unknown): v is Record<string, Json> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function citedSources(raw: unknown): CitedSource[] {
  if (!Array.isArray(raw)) return [];
  return raw.filter((s): s is CitedSource => isObject(s) && typeof s.source_id === "string" && typeof s.title === "string");
}

export interface Unwrapped {
  draft: Record<string, Json>;
  review: { status?: string; validation?: { schema_valid?: boolean; risk_flags?: string[]; cited_sources?: string[] } } | null;
  modelMode: string | null;
  sources: CitedSource[];
  isAgentEnvelope: boolean;
}

export function unwrapAgentEnvelope(content: Record<string, unknown>): Unwrapped {
  if (isObject(content.artifact_draft)) {
    return {
      draft: content.artifact_draft,
      review: isObject(content.review) ? (content.review as Unwrapped["review"]) : null,
      modelMode: typeof content.model_mode === "string" ? content.model_mode : null,
      sources: citedSources(content.sources),
      isAgentEnvelope: true,
    };
  }
  return { draft: content as Record<string, Json>, review: null, modelMode: null, sources: [], isAgentEnvelope: false };
}

/**
 * Resolves `rows_path` inside artifact content. Accepts a JSON pointer ("/artifact_draft/requirements") or a dotted
 * path ("artifact_draft.requirements"); when the path is not found on the raw content it is also tried on the
 * unwrapped agent draft, so manifests can name the path relative to either.
 */
export function resolveRows(content: Record<string, unknown> | null | undefined, rowsPath: string): Record<string, unknown>[] | null {
  if (!content) return null;
  const segments = rowsPath.startsWith("/")
    ? rowsPath.slice(1).split("/").map((s) => s.replace(/~1/g, "/").replace(/~0/g, "~"))
    : rowsPath.split(".").filter(Boolean);
  const walk = (root: unknown): unknown => segments.reduce<unknown>((cur, seg) => (isObject(cur) || Array.isArray(cur) ? (cur as Record<string, unknown>)[seg] : undefined), root);
  let found = walk(content);
  if (!Array.isArray(found)) found = walk(unwrapAgentEnvelope(content).draft);
  if (!Array.isArray(found)) return null;
  return found.map((r) => (isObject(r) ? r : { value: r as Json }));
}

/** Source ids referenced by a field value: a string id, a list of ids, or {source_id} objects. */
export function evidenceIds(v: unknown): string[] {
  if (v === null || v === undefined || v === "") return [];
  if (typeof v === "string") return [v];
  if (Array.isArray(v)) return v.flatMap(evidenceIds);
  if (isObject(v) && typeof v.source_id === "string") return [v.source_id];
  return [];
}

/** Every source the artifact cites: the BFF's `sources` plus the agent envelope's own list, deduplicated. */
export function artifactSources(content: Record<string, unknown> | null | undefined, extra: CitedSource[] = []): CitedSource[] {
  const all = [...extra, ...(content ? unwrapAgentEnvelope(content).sources : [])];
  const seen = new Set<string>();
  return all.filter((s) => {
    const k = `${s.source_id}@${s.version_id ?? ""}`;
    if (seen.has(k)) return false;
    seen.add(k);
    return true;
  });
}
