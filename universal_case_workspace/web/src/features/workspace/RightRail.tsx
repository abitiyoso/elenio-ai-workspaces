import { Link } from "react-router-dom";
import type { CaseContext, NavItem } from "../../api/types";
import { formatDate } from "../../components/format";
import { Icon } from "../../components/Icon";
import { StatusPill } from "../../components/StatusPill";
import { aggregateSources, navId, nodeName, revisingSteps, safeLink } from "./model";

export function RightRail({ ctx, navigation, caseBase, requested }: { ctx: CaseContext; navigation: NavItem[]; caseBase: string; requested: ReadonlySet<string> }) {
  const pending = ctx.drafts.filter((d) => d.status === "pending");
  const mine = pending.filter((d) => d.can_submit);
  const revising = revisingSteps(ctx, requested);
  const latestRun = [...ctx.agent_runs].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? ""))[0];
  const sources = aggregateSources(ctx).slice(0, 6);
  const overview = navigation.find((n) => n.view === "overview");
  const docs = navigation.find((n) => n.view === "documents");
  const flowcaseCase = safeLink(ctx.links?.flowcase_case);
  const flowcaseApprovals = safeLink(ctx.links?.flowcase_approvals);
  const pendingApprovals = ctx.approvals.filter((a) => a.status === "pending").length;

  return (
    <>
      <section className="card pad" aria-labelledby="rail-review">
        <div className="cardhead"><div><h2 id="rail-review">Review summary</h2><p>Agent drafts for the case owner</p></div></div>
        <div aria-live="polite">
          {mine.length > 0 ? (
            <p className="rail-strong"><strong>{mine.length}</strong> draft{mine.length === 1 ? "" : "s"} wait for you</p>
          ) : pending.length > 0 ? <p className="muted">{pending.length} draft(s) wait for the case owner.</p> : <p className="muted">Nothing waits on you.</p>}
          {revising.map((r) => <p key={r.nodeId} className="tiny"><span className="pill blue">revising</span> {r.name}</p>)}
        </div>
        {pending.length > 0 && overview && <Link className="btn sm soft" to={`${caseBase}/${encodeURIComponent(navId(overview))}#your-review`}>Open review</Link>}
      </section>

      <section className="card pad" aria-labelledby="rail-agent">
        <div className="cardhead"><div><h2 id="rail-agent">Latest agent run</h2><p>Agent Studio · drafts only</p></div></div>
        {latestRun ? (
          <div className="listline">
            <span className="square"><Icon name="bot" size="sm" /></span>
            <div><strong>{latestRun.node_name || nodeName(ctx.workflow?.nodes, latestRun.node_id)}</strong><p>{latestRun.provider ?? "agent"} · attempt {latestRun.attempt ?? 1} · {formatDate(latestRun.created_at)}</p></div>
            <StatusPill status={latestRun.status} />
          </div>
        ) : <p className="muted">No agent run yet.</p>}
        <p className="notice warning tight">Agent outputs are drafts; AI has no approval authority.</p>
      </section>

      <section className="card pad" aria-labelledby="rail-sources">
        <div className="cardhead">
          <div><h2 id="rail-sources">Cited sources</h2><p>KMS metadata</p></div>
          {docs && sources.length > 0 && <Link className="small-link" to={`${caseBase}/${encodeURIComponent(navId(docs))}`}>All</Link>}
        </div>
        {sources.length === 0 ? <p className="muted">No sources cited yet.</p> : (
          <ul className="listlines">
            {sources.map(({ source, locators }) => (
              <li className="listline" key={`${source.source_id}@${source.version_id ?? ""}`}>
                <span className="square"><Icon name="file" size="sm" /></span>
                <div><strong>{source.title}</strong>{locators[0] && <p>{locators[0]}</p>}</div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card pad" aria-labelledby="rail-flowcase">
        <div className="cardhead"><div><h2 id="rail-flowcase">Decisions in Flowcase</h2><p>Flowcase is authoritative</p></div></div>
        <p className="muted">{pendingApprovals} approval{pendingApprovals === 1 ? "" : "s"} pending.</p>
        <div className="actions start">
          {flowcaseApprovals && <a className="btn sm primary" href={flowcaseApprovals}>Approvals in Flowcase <Icon name="external" size="sm" /></a>}
          {flowcaseCase && <a className="btn sm" href={flowcaseCase}>Open case in Flowcase</a>}
        </div>
      </section>
    </>
  );
}
