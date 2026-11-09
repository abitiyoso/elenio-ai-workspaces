import { createContext, useContext, type ReactNode } from "react";
import { isObject, unwrapAgentEnvelope, type CitedSource, type Json } from "./artifactContent";

// Titles of the sources the result cites, so evidence refs read as document names instead of ids.
const SourceTitles = createContext<Map<string, CitedSource>>(new Map());

// Readable rendering of artifact content for reviewers. Agent Studio results arrive as an envelope
// ({artifact_draft, review, pinned, model_mode}); the draft is shown as sections, with validation facts
// and the raw JSON kept available. Unknown shapes fall back to a generic structured view.


const labels: Record<string, string> = {
  requirements: "Requirements",
  ambiguities: "Clarification questions",
  unresolved: "Unresolved",
  compliance_skeleton: "Compliance skeleton",
  evidence_refs: "Sources",
  options: "Options",
  components: "Components",
  flows: "Flows",
  assumptions: "Assumptions",
  risks: "Risks",
  wbs: "Work breakdown",
  estimate_lines: "Estimate lines",
  pricing_note: "Pricing note",
};

// Fields used as the headline of a list item, in order of preference.
const headlineKeys = ["statement", "question", "title", "name", "topic", "summary", "description", "item"];
const idKeys = ["id", "requirement_id", "key"];

function label(key: string): string {
  return labels[key] ?? key.replace(/_/g, " ").replace(/^\w/, (c) => c.toUpperCase());
}

function shortRef(ref: string): string {
  return /^[0-9a-f-]{36}$/i.test(ref) ? ref.slice(0, 8) : ref;
}

function Scalar({ value }: { value: Json }) {
  if (typeof value === "boolean") return <span className={value ? "pill approved" : "pill"}>{value ? "yes" : "no"}</span>;
  if (value === null || value === "") return <span className="tiny">—</span>;
  return <span className="artifact-text">{String(value)}</span>;
}

function Refs({ refs }: { refs: Json[] }) {
  const titles = useContext(SourceTitles);
  if (refs.length === 0) return <span className="pill draft" title="No knowledge-base source cited">no source</span>;
  return (
    <>
      {refs.map((r) => {
        const src = titles.get(String(r));
        const tip = src ? `${src.title}${src.citation ? ` · ${src.citation}` : ""} (KMS ${String(r)})` : `Knowledge source ${String(r)}`;
        return (
          <span key={String(r)} className="pill source-pill" title={tip}>
            {src ? src.title : `KMS ${shortRef(String(r))}`}
          </span>
        );
      })}
    </>
  );
}

function Item({ value, depth }: { value: Json; depth: number }) {
  if (!isObject(value)) return <li><Value value={value} depth={depth + 1} /></li>;
  const idKey = idKeys.find((k) => typeof value[k] === "string");
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
          <strong>{k === "evidence_refs" && Array.isArray(v) ? <Refs refs={v} /> : <Value value={v} depth={depth + 1} />}</strong>
        </div>
      ))}
    </li>
  );
}

function Value({ value, depth }: { value: Json; depth: number }): ReactNode {
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="tiny">none</span>;
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
          <div className="keyval" key={k}><span>{label(k)}</span><strong><Value value={v} depth={depth + 1} /></strong></div>
        ))}
      </div>
    );
  }
  return <Scalar value={value} />;
}

/** Renders one artifact version's content for a human reviewer. */
export function ArtifactView({ content }: { content: Record<string, unknown> }) {
  const { draft, review, modelMode, sources } = unwrapAgentEnvelope(content);
  const titles = new Map(sources.map((s) => [s.source_id, s]));
  const sections = Object.entries(draft);
  const v = review?.validation;
  return (
    <SourceTitles.Provider value={titles}>
    <div className="artifact-view">
      {(review || modelMode) && (
        <div className="artifact-facts">
          {review?.status && <span className="pill review">{review.status.replace(/_/g, " ")}</span>}
          {v?.schema_valid !== undefined && <span className={v.schema_valid ? "pill approved" : "pill failed"}>{v.schema_valid ? "schema valid" : "schema invalid"}</span>}
          {v?.risk_flags && v.risk_flags.length > 0 && v.risk_flags.map((f) => <span key={f} className="pill failed">{f.replace(/_/g, " ")}</span>)}
          {v?.cited_sources && <span className="pill">{v.cited_sources.length} KMS source(s) cited</span>}
          {modelMode && <span className="pill">{modelMode === "provider" ? "real model" : modelMode}</span>}
        </div>
      )}
      {sections.length === 0 && <p className="tiny">This artifact has no content.</p>}
      {sections.map(([k, val]) => (
        <section className="artifact-section" key={k}>
          <h4>{label(k)} {Array.isArray(val) && <span className="count">{val.length}</span>}</h4>
          <Value value={val} depth={0} />
        </section>
      ))}
      {sources.length > 0 && (
        <section className="artifact-section">
          <h4>Sources <span className="count">{sources.length}</span></h4>
          <ol className="artifact-list artifact-sources">
            {sources.map((s) => (
              <li key={`${s.source_id}-${s.version_id}`}>
                <strong>{s.title}</strong>
                {s.citation && <span className="tiny"> · {s.citation}</span>}
                <span className="tiny"> · <code title={`version ${s.version_id}`}>{shortRef(s.source_id)}</code></span>
              </li>
            ))}
          </ol>
        </section>
      )}
      <details className="artifact-raw">
        <summary>Raw JSON</summary>
        <pre className="artifact-json">{JSON.stringify(content, null, 2)}</pre>
      </details>
    </div>
    </SourceTitles.Provider>
  );
}
