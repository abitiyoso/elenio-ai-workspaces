import type { CitedSource } from "../api/types";

export type SourceIndex = Map<string, CitedSource>;

export function indexSources(sources: CitedSource[]): SourceIndex {
  const m: SourceIndex = new Map();
  for (const s of sources) {
    if (!m.has(s.source_id)) m.set(s.source_id, s);
    if (!m.has(s.source_id.toLowerCase())) m.set(s.source_id.toLowerCase(), s);
  }
  return m;
}

