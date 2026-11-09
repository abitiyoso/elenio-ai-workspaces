import { formatDate } from "../../../components/format";
import { EmptyState, PanelError } from "../../../components/States";
import { StatusPill } from "../../../components/StatusPill";
import { errorsFor, nodeName } from "../model";
import type { ModuleProps } from "./types";

export function AiActivityModule({ ctx, nav }: ModuleProps) {
  const errors = errorsFor(ctx, ["agent_studio"]);
  const runs = [...ctx.agent_runs].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? ""));
  return (
    <div className="stack">
      <div className="module-head"><div><h2>{nav.label}</h2><p>Agent Studio runs on this case. Agent results are drafts and never approvals.</p></div></div>
      {errors.map((e, i) => <PanelError key={i} error={e} />)}
      {runs.length === 0 ? <div className="card pad"><EmptyState icon="bot" title="No agent runs yet">Runs appear when the workflow reaches an agent step.</EmptyState></div> : (
        <div className="card">
          <div className="table-scroll">
            <table className="table">
              <caption className="sr-only">Agent runs</caption>
              <thead><tr><th scope="col">Step</th><th scope="col">Provider</th><th scope="col">Status</th><th scope="col">Attempt</th><th scope="col">Started</th><th scope="col">Last update</th></tr></thead>
              <tbody>
                {runs.map((r, i) => (
                  <tr key={r.id ?? `${r.node_id}-${r.attempt}-${i}`}>
                    <td><strong>{r.node_name || nodeName(ctx.workflow?.nodes, r.node_id)}</strong>{r.agent_ref && <small>{r.agent_ref}</small>}</td>
                    <td>{r.provider ?? "—"}</td>
                    <td><StatusPill status={r.status} /></td>
                    <td>{r.attempt ?? "—"}</td>
                    <td className="nowrap">{formatDate(r.created_at)}</td>
                    <td className="nowrap">{formatDate(r.completed_at ?? r.updated_at)}</td>
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
