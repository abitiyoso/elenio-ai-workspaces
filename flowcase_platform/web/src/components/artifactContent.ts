// Helpers for artifact content produced by Agent Studio ({artifact_draft, review, pinned, model_mode}) or by people.

export type Json = string | number | boolean | null | Json[] | { [key: string]: Json };

export function isObject(v: unknown): v is Record<string, Json> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** A cited knowledge source as reported by Agent Studio (title and locator for reviewers). */
export interface CitedSource {
  source_id: string;
  version_id: string;
  title: string;
  citation?: string;
  anchor?: string;
}

function citedSources(raw: unknown): CitedSource[] {
  if (!Array.isArray(raw)) return [];
  return raw.filter((s): s is CitedSource => isObject(s) && typeof s.source_id === "string" && typeof s.title === "string");
}

export function unwrapAgentEnvelope(content: Record<string, unknown>): {
  draft: Record<string, Json>;
  review: { status?: string; validation?: { schema_valid?: boolean; risk_flags?: string[]; cited_sources?: string[] } } | null;
  modelMode: string | null;
  sources: CitedSource[];
} {
  if (isObject(content.artifact_draft)) {
    return {
      draft: content.artifact_draft,
      review: isObject(content.review) ? (content.review as never) : null,
      modelMode: typeof content.model_mode === "string" ? content.model_mode : null,
      sources: citedSources(content.sources),
    };
  }
  return { draft: content as Record<string, Json>, review: null, modelMode: null, sources: [] };
}
