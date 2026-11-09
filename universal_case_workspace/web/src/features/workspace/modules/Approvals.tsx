import type { FlowcaseApproval } from "../../../api/types";
import { formatDate, humanize } from "../../../components/format";
import { Icon } from "../../../components/Icon";
import { EmptyState, PanelError } from "../../../components/States";
import { StatusPill } from "../../../components/StatusPill";
import { errorsFor, nodeName, safeLink } from "../model";
import type { ModuleProps } from "./types";

function ApprovalCard({ a, step, decideHref }: { a: FlowcaseApproval; step: string; decideHref: string | null }) {
  return (
    <article className="card pad approval" aria-labelledby={`approval-${a.id}`}>
      <div className="cardhead">
        <div>
          <h3 id={`approval-${a.id}`}><Icon name="shield-check" size="sm" /> {a.title ?? step}</h3>
          <p>{step}{a.kind ? ` · ${humanize(a.kind)}` : ""} · requested {formatDate(a.created_at)}</p>
        </div>
        <StatusPill status={a.status} />
      </div>
      <dl className="datapairs">
        <div className="datapair"><dt>Policy</dt><dd>{a.policy_name ?? "—"}{a.policy_ref && <code className="muted-code"> {a.policy_ref}</code>}</dd></div>
        <div className="datapair">
          <dt>Covered artifacts</dt>
          <dd>{a.covered_artifacts?.length ? a.covered_artifacts.map((c) => <span key={`${c.type}-${c.version}`} className="pill grey">{c.type} v{c.version}{c.status ? ` · ${humanize(c.status)}` : ""}</span>) : "—"}</dd>
        </div>
        {a.decision && (
          <div className="datapair">
            <dt>Decision</dt>
            <dd><strong>{humanize(a.decision.decision)}</strong> by {a.decision.decided_by_name ?? "—"} · {formatDate(a.decision.decided_at)}{a.decision.rationale && <q className="rationale">{a.decision.rationale}</q>}</dd>
          </div>
        )}
      </dl>
      {a.blocked_reason && <p className="notice warning">{a.blocked_reason}</p>}
      {a.invalidated_reason && <p className="notice warning">Invalidated: {a.invalidated_reason}</p>}
      {a.status === "pending" && decideHref && (
        <div className="actions start"><a className="btn primary" href={decideHref}>Decide in Flowcase <Icon name="external" size="sm" /></a></div>
      )}
    </article>
  );
}

export function ApprovalsModule({ ctx, nav }: ModuleProps) {
  const errors = errorsFor(ctx, ["flowcase"]);
  const decideHref = safeLink(ctx.links?.flowcase_approvals);
  return (
    <div className="stack">
      <div className="module-head">
        <div><h2>{nav.label}</h2><p>Human approvals recorded by Flowcase for this case.</p></div>
        {decideHref && <a className="btn soft" href={decideHref}>Decide in Flowcase <Icon name="external" size="sm" /></a>}
      </div>
      <p className="muted-note">The Case Workspace never decides gates. Approvals are made and audited in Flowcase; this view is read-only.</p>
      {errors.map((e, i) => <PanelError key={i} error={e} />)}
      {ctx.approvals.length === 0
        ? <div className="card pad"><EmptyState icon="shield-check" title="No approvals yet">Approval steps appear here once the workflow reaches them.</EmptyState></div>
        : ctx.approvals.map((a) => <ApprovalCard key={a.id} a={a} step={nodeName(ctx.workflow?.nodes, a.node_id)} decideHref={decideHref} />)}
    </div>
  );
}
