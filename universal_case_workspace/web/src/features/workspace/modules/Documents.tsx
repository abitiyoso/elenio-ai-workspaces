import { shortRef } from "../../../components/format";
import { EmptyState, PanelError } from "../../../components/States";
import { aggregateSources, errorsFor } from "../model";
import type { ModuleProps } from "./types";

export function DocumentsModule({ ctx, nav }: ModuleProps) {
  const errors = errorsFor(ctx, ["kms"]);
  const sources = aggregateSources(ctx);
  return (
    <div className="stack">
      <div className="module-head"><div><h2>{nav.label}</h2><p>Knowledge sources cited across this case's artifacts. Metadata only; open documents in KMS.</p></div></div>
      {errors.map((e, i) => <PanelError key={i} error={e} />)}
      {sources.length === 0 ? <div className="card pad"><EmptyState icon="files" title="No cited sources yet">Sources appear when agents or people cite KMS documents.</EmptyState></div> : (
        <div className="card">
          <div className="table-scroll">
            <table className="table">
              <caption className="sr-only">Cited sources</caption>
              <thead><tr><th scope="col">Source</th><th scope="col">Citation / anchor</th><th scope="col">Cited by</th></tr></thead>
              <tbody>
                {sources.map(({ source, locators, citedBy }) => (
                  <tr key={`${source.source_id}@${source.version_id ?? ""}`}>
                    <td><strong>{source.title}</strong><small><code>{shortRef(source.source_id)}</code>{source.version_id && <> · version <code>{shortRef(source.version_id)}</code></>}</small></td>
                    <td>{locators.length ? locators.join("; ") : "—"}</td>
                    <td><span className="pill-row">{citedBy.map((c) => <span key={c} className="pill grey">{c}</span>)}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
