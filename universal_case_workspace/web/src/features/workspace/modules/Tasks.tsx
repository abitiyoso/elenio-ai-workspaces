import { formatDate, humanize } from "../../../components/format";
import { Icon } from "../../../components/Icon";
import { EmptyState, PanelError } from "../../../components/States";
import { StatusPill } from "../../../components/StatusPill";
import { DraftReviewPanel } from "../DraftReview";
import { errorsFor, nodeName, safeLink } from "../model";
import type { ModuleProps } from "./types";

export function TasksModule({ ctx, nav, actions }: ModuleProps) {
  const errors = errorsFor(ctx, ["flowcase"]);
  const flowcase = safeLink(ctx.links?.flowcase_case);
  return (
    <div className="stack">
      <div className="module-head"><div><h2>{nav.label}</h2><p>Drafts waiting for you and the Flowcase tasks of this case.</p></div></div>
      {errors.map((e, i) => <PanelError key={i} error={e} />)}
      <DraftReviewPanel ctx={ctx} actions={actions} showEmpty />
      <section className="card">
        <div className="card-top cardhead">
          <div><h2>Flowcase tasks</h2><p>Read-only here; complete tasks in Flowcase.</p></div>
          {flowcase && <a className="btn sm soft" href={flowcase}>Open in Flowcase <Icon name="external" size="sm" /></a>}
        </div>
        {ctx.tasks.length === 0 ? <div className="pad"><EmptyState icon="check-square" title="No Flowcase tasks">Nothing is assigned on this case right now.</EmptyState></div> : (
          <div className="table-scroll">
            <table className="table">
              <caption className="sr-only">Flowcase tasks</caption>
              <thead><tr><th scope="col">Task</th><th scope="col">Step</th><th scope="col">Assignee role</th><th scope="col">Due</th><th scope="col">Status</th></tr></thead>
              <tbody>
                {ctx.tasks.map((t) => (
                  <tr key={t.id}>
                    <td><strong>{t.title ?? "Task"}</strong><small>Created {formatDate(t.created_at)}</small></td>
                    <td>{nodeName(ctx.workflow?.nodes, t.node_id)}</td>
                    <td>{humanize(t.assignee_role)}</td>
                    <td>{formatDate(t.due_at)}</td>
                    <td><StatusPill status={t.status} />{t.completed_by_name && <small>by {t.completed_by_name}</small>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
