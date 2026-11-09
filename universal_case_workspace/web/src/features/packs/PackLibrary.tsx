import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import type { NavItem, PackManifest, PackSummary } from "../../api/types";
import { useResource } from "../../api/useResource";
import { getPack, listPacks } from "../../api/workspace";
import { humanize } from "../../components/format";
import { Icon } from "../../components/Icon";
import { EmptyState, ErrorState, Loading, PageHead } from "../../components/States";
import { StatusPill } from "../../components/StatusPill";

const shortChecksum = (c?: string) => (c ? c.replace(/^sha256:/, "").slice(0, 12) : "—");
const packPath = (p: PackSummary) => `/packs/${encodeURIComponent(p.pack_key)}/${encodeURIComponent(p.version)}`;

function Validation({ p }: { p: PackSummary }) {
  const v = p.validation;
  if (!v) return <span className="pill grey">Not validated</span>;
  const ok = v.schema_ok && v.semantic_ok && !(v.errors?.length);
  return (
    <div className="validation">
      <span className={ok ? "pill green" : "pill red"}>{ok ? "Validation passed" : `${v.errors?.length ?? 0} validation error${v.errors?.length === 1 ? "" : "s"}`}</span>
      {!ok && (
        <ul className="validation-errors" aria-label={`Validation errors for ${p.title}`}>
          {!v.schema_ok && <li>JSON Schema check failed</li>}
          {!v.semantic_ok && <li>Semantic reference check failed</li>}
          {v.errors?.map((e, i) => <li key={i}><code>{e.path || "/"}</code> {e.message} <span className="muted-code">({e.code})</span></li>)}
        </ul>
      )}
    </div>
  );
}

function Live({ p }: { p: PackSummary }) {
  if (!p.live) return null;
  return p.live.startable ? <span className="pill green">Live · startable</span> : (
    <div>
      <span className="pill amber">Not startable</span>
      {p.live.reasons && p.live.reasons.length > 0 && <ul className="reasons">{p.live.reasons.map((r) => <li key={r}>{r}</li>)}</ul>}
    </div>
  );
}

function PackCard({ p }: { p: PackSummary }) {
  return (
    <article className="card pack-card" aria-labelledby={`pack-${p.pack_key}-${p.version}`}>
      <div className="pack-card-top">
        <span className="pack-icon"><Icon name={p.brand?.icon ?? "blocks"} /></span>
        <span className="pill blue">v{p.version}</span>
      </div>
      <h3 id={`pack-${p.pack_key}-${p.version}`}>{p.title}</h3>
      <p className="pack-key"><code>{p.pack_key}</code> · <StatusPill status={p.status} /></p>
      {p.description && <p className="pack-desc">{p.description}</p>}
      <dl className="datapairs compact">
        <div className="datapair"><dt>Checksum</dt><dd><code title={p.checksum}>{shortChecksum(p.checksum)}</code></dd></div>
        <div className="datapair"><dt>Flowcase workflow</dt><dd>{p.flowcase_workflow_key ?? "—"}</dd></div>
        <div className="datapair"><dt>Intake chat</dt><dd>{p.has_intake ? "Yes" : "No"}</dd></div>
      </dl>
      <Validation p={p} />
      <Live p={p} />
      <div className="pack-footer"><Link className="btn sm soft" to={packPath(p)}>Inspect manifest</Link></div>
    </article>
  );
}

export function PackLibraryPage() {
  const { res, reload } = useResource("packs", listPacks);
  return (
    <>
      <PageHead eyebrow="CONFIGURATION" title="Domain Pack Library" subtitle="Reusable workspace definitions. Different domain experiences, the same UI engine. Read-only; publishing is an admin workflow." />
      {res.state === "loading" && <Loading label="Loading domain packs…" />}
      {res.state === "error" && <ErrorState error={res.error} onRetry={reload} />}
      {res.state === "ready" && (res.data.items.length === 0
        ? <div className="card pad"><EmptyState icon="blocks" title="No domain packs published" /></div>
        : <div className="pack-grid">{res.data.items.map((p) => <PackCard key={`${p.pack_key}@${p.version}`} p={p} />)}</div>)}
      <p className="notice tight">Each case is pinned to the exact pack version it was created with. Publishing a new version never migrates ongoing cases.</p>
    </>
  );
}

/** Renders the navigation and artifact fields a manifest would produce, without a case. */
export function NavigationPreview({ manifest }: { manifest: PackManifest }) {
  const nav: NavItem[] = manifest.navigation ?? [];
  const types = manifest.artifact_types ?? [];
  const [selected, setSelected] = useState(0);
  const item = nav[selected];
  const type = item?.view === "artifact_list" ? types.find((t) => t.key === item.artifact_type) : undefined;
  return (
    <div className="nav-preview">
      <nav className="preview-nav" aria-label="Navigation preview">
        {nav.map((n, i) => (
          <button key={n.key ?? n.id ?? i} type="button" className={i === selected ? "active" : ""} aria-pressed={i === selected} onClick={() => setSelected(i)}>
            <Icon name={n.icon ?? "dot"} size="sm" /> <span>{n.label}</span> <small>{n.view}</small>
          </button>
        ))}
      </nav>
      <div className="preview-body">
        {!item ? <p className="muted">The manifest declares no navigation.</p> : type ? (
          <>
            <h4>{type.title} <span className="muted">· {type.fields.length} fields · {type.source?.rows_path ? `rows at ${type.source.rows_path}` : "whole document"}</span></h4>
            {type.description && <p className="muted">{type.description}</p>}
            <table className="table preview-table">
              <caption className="sr-only">Fields of {type.title}</caption>
              <thead><tr><th scope="col">Field</th><th scope="col">Key</th><th scope="col">Widget</th><th scope="col">Rules</th></tr></thead>
              <tbody>
                {type.fields.map((f) => (
                  <tr key={f.key}>
                    <td><strong>{f.label}</strong>{f.hint && <small>{f.hint}</small>}</td>
                    <td><code>{f.key}</code></td>
                    <td>{f.widget}</td>
                    <td>{[f.required && "required", f.read_only && "read-only", f.options && `options: ${f.options.join(", ")}`, f.min !== undefined && `min ${f.min}`, f.max !== undefined && `max ${f.max}`].filter(Boolean).join(" · ") || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {type.source && <p className="tiny muted">Source: {type.source.provider ?? "—"}{type.source.flowcase_artifact_type ? ` · Flowcase type ${type.source.flowcase_artifact_type}` : ""}</p>}
          </>
        ) : item.view === "artifact_list" ? (
          <p className="notice warning">This item points to artifact type <code>{item.artifact_type ?? "(none)"}</code>, which the manifest does not declare.</p>
        ) : (
          <p className="muted">{item.label} renders the generic <strong>{humanize(item.view)}</strong> module, filled from Flowcase, KMS and Agent Studio data for the case.</p>
        )}
      </div>
    </div>
  );
}

export function PackDetailPage() {
  const { key = "", version = "" } = useParams();
  const { res, reload } = useResource(`pack:${key}@${version}`, (signal) => getPack(key, version, signal));
  const [copied, setCopied] = useState(false);
  if (res.state === "loading") return <Loading label="Loading the manifest…" />;
  if (res.state === "error") return <><p><Link className="small-link" to="/packs">← Domain Pack Library</Link></p><ErrorState error={res.error} onRetry={reload} /></>;
  const { pack, manifest } = res.data;
  const json = JSON.stringify(manifest, null, 2);
  const copy = () => {
    navigator.clipboard?.writeText(json).then(() => { setCopied(true); window.setTimeout(() => setCopied(false), 2000); }, () => undefined);
  };
  return (
    <>
      <p><Link className="small-link" to="/packs"><Icon name="arrow-left" size="sm" /> Domain Pack Library</Link></p>
      <PageHead eyebrow="DOMAIN PACK" title={pack.title} subtitle={<><code>{pack.pack_key}@{pack.version}</code> · {pack.status}</>} />
      <div className="grid2 align-start">
        <section className="card pad">
          <div className="cardhead"><div><h2>Selected manifest</h2><p>Bindings and validation</p></div></div>
          <dl className="datapairs">
            <div className="datapair"><dt>Flowcase workflow</dt><dd>{pack.flowcase_workflow_key ?? "—"}</dd></div>
            <div className="datapair"><dt>Checksum</dt><dd><code title={pack.checksum}>{shortChecksum(pack.checksum)}</code></dd></div>
            <div className="datapair"><dt>Navigation items</dt><dd>{manifest.navigation?.length ?? 0}</dd></div>
            <div className="datapair"><dt>Artifact types</dt><dd>{manifest.artifact_types?.length ?? 0}</dd></div>
            <div className="datapair"><dt>Agent actions</dt><dd>{manifest.actions?.length ?? 0}</dd></div>
          </dl>
          <Validation p={pack} />
          <Live p={pack} />
          <p className="notice">This pack is version-pinned per case. Publishing a new version does not migrate existing cases.</p>
        </section>
        <section className="card pad">
          <div className="cardhead">
            <div><h2>Manifest JSON</h2><p>Read-only, rendered as text</p></div>
            <button className="btn sm soft" type="button" onClick={copy}>{copied ? "Copied" : "Copy JSON"}</button>
          </div>
          <pre className="codebox" tabIndex={0} aria-label="Manifest JSON">{json}</pre>
        </section>
      </div>
      <section className="card pad preview-card">
        <div className="cardhead"><div><h2>Preview navigation</h2><p>What a case pinned to this version shows: views and artifact fields from the manifest.</p></div></div>
        <NavigationPreview manifest={manifest} />
      </section>
    </>
  );
}
