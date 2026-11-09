import { useCallback, useState } from "react";
import type { CaseArtifact, CaseContext, CitedSource, NavItem, PackArtifactType, PackField } from "../../../api/types";
import { ArtifactView } from "../../../components/ArtifactView";
import { artifactSources, evidenceIds, resolveRows } from "../../../components/artifactContent";
import { Drawer } from "../../../components/Drawer";
import { formatDate, formatDay, humanize } from "../../../components/format";
import { Icon } from "../../../components/Icon";
import { SourcePills } from "../../../components/SourcePills";
import { indexSources, type SourceIndex } from "../../../components/sources";
import { EmptyState } from "../../../components/States";
import { StatusPill } from "../../../components/StatusPill";
import { artifactsForType, findArtifactType, isUnreviewedAgentOutput, nodeName, producerFor } from "../model";

// Schema-driven artifact view. The pack manifest decides everything: which artifact type, its fields and widgets,
// and whether the content is a row table (source.rows_path) or a whole document. No pack-specific code.

const maxColumns = 4;

/** Value of a field in a row; evidence fields fall back to the agent envelope's generic `evidence_refs`. */
function fieldValue(row: Record<string, unknown>, f: PackField): unknown {
  const v = row[f.key];
  if (f.widget === "evidence_ref" && (v === undefined || v === null || v === "")) return row.evidence_refs;
  return v;
}

function FieldValue({ field, value, index, full }: { field: PackField; value: unknown; index: SourceIndex; full?: boolean }) {
  if (field.widget === "evidence_ref") return <SourcePills ids={evidenceIds(value)} index={index} />;
  if (value === undefined || value === null || value === "") return <span className="muted">—</span>;
  if (field.widget === "boolean" || typeof value === "boolean") return <span className={value ? "pill green" : "pill grey"}>{value ? "Yes" : "No"}</span>;
  if (field.widget === "select") return <span className="pill grey value-pill">{String(value)}</span>;
  if (field.widget === "date" && typeof value === "string") return <span>{formatDay(value)}</span>;
  if (Array.isArray(value)) return <span>{value.map((x) => (typeof x === "object" ? JSON.stringify(x) : String(x))).join(", ")}</span>;
  if (typeof value === "object") return <code className="inline-json">{JSON.stringify(value)}</code>;
  const text = String(value);
  if (!full && field.widget === "textarea" && text.length > 160) return <span title={text}>{text.slice(0, 157)}…</span>;
  return <span className={field.widget === "textarea" ? "prewrap" : undefined}>{text}</span>;
}

function RowInspector({ type, row, rowLabel, index, onClose }: {
  type: PackArtifactType; row: Record<string, unknown>; rowLabel: string; index: SourceIndex; onClose: () => void;
}) {
  const known = new Set(type.fields.map((f) => f.key));
  const extra = Object.entries(row).filter(([k]) => !known.has(k) && k !== "evidence_refs");
  return (
    <Drawer title={rowLabel} subtitle={`${type.title} · read-only`} onClose={onClose} labelId="row-inspector-title"
      footer={<>
        <button className="btn" type="button" onClick={onClose}>Close</button>
        <button className="btn primary" type="button" disabled title="Editing artifact rows arrives in Phase 3">Edit (coming in Phase 3)</button>
      </>}>
      <dl className="inspector">
        {type.fields.map((f) => (
          <div className="inspector-field" key={f.key}>
            <dt>{f.label}{f.required && <span className="req" aria-label="required"> *</span>}{f.read_only && <span className="pill grey tiny-pill">read-only</span>}</dt>
            <dd><FieldValue field={f} value={fieldValue(row, f)} index={index} full /></dd>
            {f.hint && <dd className="hint">{f.hint}</dd>}
          </div>
        ))}
      </dl>
      {extra.length > 0 && (
        <details className="artifact-raw">
          <summary>Other data in this row ({extra.length})</summary>
          <pre className="codebox">{JSON.stringify(Object.fromEntries(extra), null, 2)}</pre>
        </details>
      )}
      <p className="notice">Editing creates a new draft revision through Flowcase with version checks; it never approves the artifact.</p>
    </Drawer>
  );
}

function RowTable({ type, rows, index }: { type: PackArtifactType; rows: Record<string, unknown>[]; index: SourceIndex }) {
  const [open, setOpen] = useState<number | null>(null);
  const close = useCallback(() => setOpen(null), []);
  const columns = type.fields.filter((f) => f.widget !== "evidence_ref").slice(0, maxColumns);
  const evidence = type.fields.find((f) => f.widget === "evidence_ref");
  const first = columns[0];
  const label = (row: Record<string, unknown>, i: number) => {
    const v = first ? row[first.key] : undefined;
    return typeof v === "string" || typeof v === "number" ? String(v) : `Row ${i + 1}`;
  };
  if (rows.length === 0) return <EmptyState icon="table" title="No rows yet">The artifact exists but has no rows.</EmptyState>;
  const openRow = open !== null ? rows[open] : undefined;
  return (
    <>
      <div className="table-scroll">
        <table className="table">
          <caption className="sr-only">{type.title}</caption>
          <thead>
            <tr>
              {columns.map((c) => <th scope="col" key={c.key} className={c.widget === "textarea" ? "wide" : undefined}>{c.label}</th>)}
              {evidence && <th scope="col">{evidence.label}</th>}
              <th scope="col"><span className="sr-only">Inspect</span></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr key={i} className="clickable" onClick={() => setOpen(i)}>
                {columns.map((c) => <td key={c.key}><FieldValue field={c} value={fieldValue(row, c)} index={index} /></td>)}
                {evidence && <td><FieldValue field={evidence} value={fieldValue(row, evidence)} index={index} /></td>}
                <td className="nowrap">
                  <button className="btn sm soft" type="button" onClick={(e) => { e.stopPropagation(); setOpen(i); }} aria-label={`Inspect ${label(row, i)}`}>
                    Inspect
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {openRow && open !== null && <RowInspector type={type} row={openRow} rowLabel={label(openRow, open)} index={index} onClose={close} />}
    </>
  );
}

function ArtifactBlock({ artifact, type, ctx }: { artifact: CaseArtifact; type: PackArtifactType; ctx: CaseContext }) {
  const sources: CitedSource[] = artifactSources(artifact.content, artifact.sources ?? []);
  const index = indexSources(sources);
  const rowsPath = type.source?.rows_path;
  const rows = rowsPath ? resolveRows(artifact.content, rowsPath) : null;
  const aiDraft = isUnreviewedAgentOutput(artifact, ctx);
  return (
    <section className="card artifact-block" aria-label={`${type.title} v${artifact.version ?? "?"}`}>
      <div className="artifact-meta">
        <div className="meta-pills">
          <span className="pill blue">v{artifact.version ?? "?"}</span>
          <StatusPill status={artifact.status} />
          {aiDraft && <span className="pill amber ai-marker">AI draft — needs human review</span>}
        </div>
        <p className="tiny muted">
          Produced by <strong>{nodeName(ctx.workflow?.nodes, artifact.producer_node_id)}</strong>
          {artifact.updated_at && <> · updated {formatDate(artifact.updated_at)}</>}
          {rowsPath && rows && <> · {rows.length} rows</>}
        </p>
      </div>
      {rowsPath && rows ? (
        <RowTable type={type} rows={rows} index={index} />
      ) : (
        <div className="pad">
          {rowsPath && <p className="notice warning">The manifest expects rows at <code>{rowsPath}</code>, but this version has none there. Showing the whole document.</p>}
          {artifact.content ? <ArtifactView content={artifact.content} sources={artifact.sources ?? []} /> : <p className="muted">This artifact version has no readable content.</p>}
        </div>
      )}
    </section>
  );
}

export function ArtifactListModule({ ctx, nav }: { ctx: CaseContext; nav: NavItem }) {
  const type = findArtifactType(ctx, nav.artifact_type);
  if (!type) {
    return (
      <div className="card pad">
        <EmptyState icon="alert-circle" title="Artifact type not declared">
          The pack's navigation points to <code>{nav.artifact_type ?? "(none)"}</code>, which its artifact types do not define.
        </EmptyState>
      </div>
    );
  }
  const artifacts = artifactsForType(ctx, type.key);
  return (
    <div className="stack">
      <div className="module-head">
        <div>
          <h2>{nav.label}</h2>
          <p>{type.description ?? type.title} · {type.fields.length} fields{type.source?.rows_path ? " · row table" : " · document"}</p>
        </div>
      </div>
      {artifacts.length === 0 ? (
        <div className="card pad">
          <EmptyState icon="file" title={`${type.title} not available yet`}>
            Produced by {producerFor(ctx, type) ?? "a later workflow step"} — not yet available.
          </EmptyState>
        </div>
      ) : artifacts.map((a) => <ArtifactBlock key={a.id} artifact={a} type={type} ctx={ctx} />)}
      {artifacts.length > 0 && <p className="tiny muted"><Icon name="lock" size="sm" /> Read-only in this release. {humanize(type.source?.provider ?? "flowcase")} keeps the versions.</p>}
    </div>
  );
}
