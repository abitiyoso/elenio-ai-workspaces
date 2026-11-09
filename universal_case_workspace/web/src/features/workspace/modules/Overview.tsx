import { Link } from "react-router-dom";
import { humanize } from "../../../components/format";
import { Icon } from "../../../components/Icon";
import { EmptyState, PanelError } from "../../../components/States";
import { StatusPill } from "../../../components/StatusPill";
import { DraftHistoryPanel, DraftReviewPanel } from "../DraftReview";
import { artifactsForType, errorsFor, isUnreviewedAgentOutput, navId, nodeName, producerFor } from "../model";
import type { ModuleProps } from "./types";

export function OverviewModule({ ctx, navigation, actions, caseBase }: ModuleProps) {
  const errors = errorsFor(ctx, ["flowcase"]);
  const openTasks = ctx.tasks.filter((t) => t.status === "assigned" || t.status === "open" || t.status === "revision_requested");
  const pendingApprovals = ctx.approvals.filter((a) => a.status === "pending");
  const blockers = [
    ...ctx.approvals.filter((a) => a.blocked_reason).map((a) => ({ key: `ap-${a.id}`, text: `${a.title ?? nodeName(ctx.workflow?.nodes, a.node_id)}: ${a.blocked_reason}` })),
    ...ctx.approvals.filter((a) => a.status === "invalidated").map((a) => ({ key: `inv-${a.id}`, text: `${a.title ?? "Approval"} was invalidated${a.invalidated_reason ? `: ${a.invalidated_reason}` : ""}` })),
    ...(ctx.workflow?.nodes ?? []).filter((n) => n.status === "failed").map((n) => ({ key: `node-${n.id}`, text: `${n.name || n.id} failed` })),
    ...ctx.agent_runs.filter((r) => r.status === "failed").map((r, i) => ({ key: `run-${i}`, text: `Agent run for ${r.node_name || nodeName(ctx.workflow?.nodes, r.node_id)} failed (attempt ${r.attempt ?? "?"})` })),
  ];
  const navFor = (typeKey: string) => navigation.find((n) => n.view === "artifact_list" && n.artifact_type === typeKey);
  const tasksNav = navigation.find((n) => n.view === "tasks");

  return (
    <div className="stack">
      <DraftReviewPanel ctx={ctx} actions={actions} />
      {errors.map((e, i) => <PanelError key={i} error={e} />)}

      <section className="card pad">
        <div className="cardhead">
          <div><h2>Case overview</h2><p>Workflow, artifacts and reviews — the same shell for every domain pack.</p></div>
          <StatusPill status={ctx.case.status} />
        </div>
        <div className="detail-columns">
          <div className="ministat"><strong>{ctx.artifacts.length}</strong><small>Versioned artifacts</small></div>
          <div className="ministat"><strong>{openTasks.length}</strong><small>Open tasks</small></div>
          <div className="ministat"><strong>{pendingApprovals.length}</strong><small>Pending approvals</small></div>
          <div className="ministat"><strong>{Math.round(ctx.case.progress ?? 0)}%</strong><small>Workflow progress</small></div>
        </div>
        {ctx.pack && ctx.pack.status !== "unavailable" && (
          <p className="notice">Views are generated from <strong>{ctx.pack.pack_key}@{ctx.pack.version}</strong>; business decisions remain in Flowcase.</p>
        )}
      </section>

      {ctx.artifact_types.length > 0 && (
        <section className="card pad">
          <div className="cardhead"><div><h2>Artifacts</h2><p>One tile per artifact type declared by the pack.</p></div></div>
          <div className="artifact-tiles">
            {ctx.artifact_types.map((t) => {
              const latest = artifactsForType(ctx, t.key)[0];
              const nav = navFor(t.key);
              const body = (
                <>
                  <span className="square"><Icon name={nav?.icon ?? "file"} /></span>
                  <span className="tile-text">
                    <strong>{t.title}</strong>
                    {latest ? (
                      <span className="pill-row">
                        <span className="pill blue">v{latest.version ?? "?"}</span>
                        <span className="pill-status"><StatusPill status={latest.status} /></span>
                        {isUnreviewedAgentOutput(latest, ctx) && <span className="pill amber">AI draft</span>}
                      </span>
                    ) : <small>Produced by {producerFor(ctx, t) ?? "a later step"} — not yet available</small>}
                  </span>
                </>
              );
              return nav
                ? <Link key={t.key} className="artifact-tile" to={`${caseBase}/${encodeURIComponent(navId(nav))}`}>{body}</Link>
                : <div key={t.key} className="artifact-tile">{body}</div>;
            })}
          </div>
        </section>
      )}

      <div className="grid2">
        <section className="card pad">
          <div className="cardhead">
            <div><h2>Open work</h2><p>Flowcase tasks and approvals</p></div>
            {tasksNav && <Link className="small-link" to={`${caseBase}/${encodeURIComponent(navId(tasksNav))}`}>All tasks</Link>}
          </div>
          {openTasks.length === 0 && pendingApprovals.length === 0 ? <EmptyState icon="check-square" title="Nothing open" /> : (
            <ul className="listlines">
              {openTasks.slice(0, 5).map((t) => (
                <li className="listline" key={t.id}><span className="square amber"><Icon name="check-square" size="sm" /></span><div><strong>{t.title ?? "Task"}</strong><p>{humanize(t.assignee_role)} · {nodeName(ctx.workflow?.nodes, t.node_id)}</p></div><StatusPill status={t.status} /></li>
              ))}
              {pendingApprovals.slice(0, 5).map((a) => (
                <li className="listline" key={a.id}><span className="square"><Icon name="shield-check" size="sm" /></span><div><strong>{a.title ?? nodeName(ctx.workflow?.nodes, a.node_id)}</strong><p>{a.policy_name ?? "Approval"}</p></div><StatusPill status={a.status} /></li>
              ))}
            </ul>
          )}
        </section>
        <section className="card pad">
          <div className="cardhead"><div><h2>Blockers</h2><p>What stops the case from moving</p></div></div>
          {blockers.length === 0 ? <p className="notice good">No blockers reported.</p> : (
            <ul className="listlines">{blockers.map((b) => <li className="listline" key={b.key}><span className="square red"><Icon name="alert-circle" size="sm" /></span><div><p className="blocker">{b.text}</p></div></li>)}</ul>
          )}
        </section>
      </div>
      <DraftHistoryPanel ctx={ctx} requested={actions.requested} />
    </div>
  );
}
