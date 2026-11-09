import type { ReactNode } from "react";
import type { SourceIndex } from "./sources";
import { shortRef } from "./format";

/** Evidence references as document-title pills; metadata only (no guessed links to KMS). */
export function SourcePills({ ids, index, emptyLabel = "no source" }: { ids: string[]; index: SourceIndex; emptyLabel?: string }) {
  if (ids.length === 0) return <span className="pill amber" title="No knowledge-base source cited">{emptyLabel}</span>;
  return (
    <span className="pill-row">
      {ids.map((id) => {
        const src = index.get(id) ?? index.get(id.toLowerCase());
        const tip = src ? `${src.title}${src.citation ? ` · ${src.citation}` : ""} (KMS ${id})` : `Knowledge source ${id}`;
        return <span key={id} className="pill source-pill" title={tip}>{src ? src.title : `KMS ${shortRef(id)}`}</span>;
      })}
    </span>
  );
}

// The assistant cites KMS sources inline as [source-id] (several may share one bracket).
const citation = /\[((?:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}[\s,;]*)+)\]/gi;

/** Text with inline [uuid] citations replaced by source-title pills. */
export function CitedText({ text, index }: { text: string; index: SourceIndex }) {
  const parts: ReactNode[] = [];
  let last = 0;
  for (const match of text.matchAll(citation)) {
    parts.push(text.slice(last, match.index));
    for (const id of (match[1] ?? "").split(/[\s,;]+/).filter(Boolean)) {
      const title = index.get(id.toLowerCase())?.title;
      parts.push(<span key={`${match.index}-${id}`} className="pill source-pill" title={`Knowledge source ${id}`}>{title ?? "KMS source"}</span>);
    }
    last = (match.index ?? 0) + match[0].length;
  }
  parts.push(text.slice(last));
  return <>{parts}</>;
}
