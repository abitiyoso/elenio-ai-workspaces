import { createContext, useContext, type ReactNode } from "react";
import type { CitedSource } from "../api/types";
import { artifactSources, isObject, unwrapAgentEnvelope, type Json } from "./artifactContent";
import { humanize, shortRef } from "./format";
import { SourcePills } from "./SourcePills";
import { indexSources, type SourceIndex } from "./sources";

// Readable rendering of artifact content for reviewers (ported from Flowcase). Agent Studio results arrive as an
// envelope ({artifact_draft, review, sources, model_mode}); the draft is shown as sections with validation facts,
// and the raw JSON stays available as escaped text. Unknown shapes fall back to a generic structured view.

const SourceTitles = createContext<SourceIndex>(new Map());

// Generic structural hints only: no domain vocabulary lives in the shared renderer.
const headlineKeys = ["statement", "question", "title", "name", "topic", "summary", "description", "item"];
const idKeys = ["id", "key"];
const evidenceKeys = new Set(["evidence_refs", "source_refs", "source_ref"]);

const label = (key: string) => (key === "evidence_refs" ? "Evidence" : humanize(key));

function Scalar({ value }: { value: Json }) {
  if (typeof value === "boolean") return <span className={value ? "pill green" : "pill grey"}>{value ? "yes" : "no"}</span>;
  if (value === null || value === "") return <span className="tiny muted">—</span>;
  return <span className="artifact-text">{String(value)}</span>;
}

function Refs({ refs }: { refs: Json }) {
  const titles = useContext(SourceTitles);
  const ids = (Array.isArray(refs) ? refs : [refs]).filter((r) => r !== null && r !== "").map(String);
  return <SourcePills ids={ids} index={titles} />;
}

function Item({ value, depth }: { value: Json; depth: number }) {
  if (!isObject(value)) return <li><Value value={value} depth={depth + 1} /></li>;
  const idKey = idKeys.find((k) => typeof value[k] === "string") ?? Object.keys(value).find((k) => /_id$/.test(k) && typeof value[k] === "string");
  const headKey = headlineKeys.find((k) => typeof value[k] === "string" && value[k] !== "");
  const rest = Object.entries(value).filter(([k]) => k !== idKey && k !== headKey);
  return (
    <li className="artifact-item">
      <div className="artifact-item-head">
        {idKey && <code>{String(value[idKey])}</code>}
        {headKey ? <strong>{String(value[headKey])}</strong> : null}
      </div>
      {rest.map(([k, v]) => (
        <div className="keyval" key={k}>
          <span>{label(k)}</span>
          <strong>{evidenceKeys.has(k) ? <Refs refs={v} /> : <Value value={v} depth={depth + 1} />}</strong>
        </div>
      ))}
    </li>
  );
}

function Value({ value, depth }: { value: Json; depth: number }): ReactNode {
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="tiny muted">none</span>;
    if (value.every((v) => !isObject(v) && !Array.isArray(v))) {
      return <ul className="artifact-list">{value.map((v, i) => <li key={i}><Scalar value={v} /></li>)}</ul>;
    }
    return <ol className="artifact-items">{value.map((v, i) => <Item key={i} value={v} depth={depth} />)}</ol>;
  }
  if (isObject(value)) {
    if (depth > 3) return <code className="artifact-json">{JSON.stringify(value)}</code>;
    return (
      <div className="artifact-object">
        {Object.entries(value).map(([k, v]) => (
          <div className="keyval" key={k}><span>{label(k)}</span><strong>{evidenceKeys.has(k) ? <Refs refs={v} /> : <Value value={v} depth={depth + 1} />}</strong></div>
        ))}
      </div>
    );
  }
  return <Scalar value={value} />;
}

/** Renders one artifact version's content for a human reviewer. */
export function ArtifactView({ content, sources: extra = [] }: { content: Record<string, unknown>; sources?: CitedSource[] }) {
  const { draft, review, modelMode } = unwrapAgentEnvelope(content);
  const sources = artifactSources(content, extra);
  const sections = Object.entries(draft);
  const v = review?.validation;
  return (
    <SourceTitles.Provider value={indexSources(sources)}>
      <div className="artifact-view">
        {(review || modelMode) && (
          <div className="artifact-facts">
            {review?.status && <span className="pill amber">{humanize(review.status)}</span>}
            {v?.schema_valid !== undefined && <span className={v.schema_valid ? "pill green" : "pill red"}>{v.schema_valid ? "schema valid" : "schema invalid"}</span>}
            {v?.risk_flags?.map((f) => <span key={f} className="pill red">{humanize(f)}</span>)}
            {v?.cited_sources && <span className="pill grey">{v.cited_sources.length} KMS source(s) cited</span>}
            {modelMode && <span className="pill grey">{modelMode === "provider" ? "real model" : modelMode}</span>}
          </div>
        )}
        {sections.length === 0 && <p className="tiny muted">This artifact has no content.</p>}
        {sections.map(([k, val]) => (
          <section className="artifact-section" key={k}>
            <h4>{label(k)} {Array.isArray(val) && <span className="count">{val.length}</span>}</h4>
            {evidenceKeys.has(k) ? <Refs refs={val} /> : <Value value={val} depth={0} />}
          </section>
        ))}
        {sources.length > 0 && (
          <section className="artifact-section">
            <h4>Sources <span className="count">{sources.length}</span></h4>
            <ol className="artifact-list artifact-sources">
              {sources.map((s) => (
                <li key={`${s.source_id}-${s.version_id ?? ""}`}>
                  <strong>{s.title}</strong>
                  {s.citation && <span className="tiny muted"> · {s.citation}</span>}
                  <span className="tiny muted"> · <code title={s.version_id ? `version ${s.version_id}` : undefined}>{shortRef(s.source_id)}</code></span>
                </li>
              ))}
            </ol>
          </section>
        )}
        <details className="artifact-raw">
          <summary>Raw JSON</summary>
          <pre className="codebox">{JSON.stringify(content, null, 2)}</pre>
        </details>
      </div>
    </SourceTitles.Provider>
  );
}
