import type { CaseContext } from "../../api/types";
import { formatDate, formatDay, humanize } from "../../components/format";
import { Icon } from "../../components/Icon";
import { StatusPill } from "../../components/StatusPill";
import { customerKeys, deadlineKeys, pickVariable } from "./model";

const bindingNote: Record<string, string> = {
  pinned_at_creation: "pinned at case creation",
  pinned_on_first_view: "pinned on first view",
};

export function CaseHeader({ ctx, onOpenRail, railOpen }: { ctx: CaseContext; onOpenRail: () => void; railOpen: boolean }) {
  const c = ctx.case;
  const customer = pickVariable(ctx, customerKeys);
  const deadline = pickVariable(ctx, deadlineKeys);
  const pack = ctx.pack;
  return (
    <header className="case-head">
      <div className="case-head-main">
        <div className="pill-row">
          <StatusPill status={c.status} />
          <span className="pill blue">{c.reference}</span>
          {c.outcome && <span className="pill grey">Outcome: {humanize(c.outcome)}</span>}
        </div>
        <h1 className="case-title">{c.title}</h1>
        <p className="case-sub">
          {customer && <><span className="sr-only">Customer: </span>{customer} · </>}
          <span><Icon name="user" size="sm" /> {c.owner_name ?? "—"}</span>
          {deadline && <> · <span><Icon name="calendar" size="sm" /> Due {formatDay(deadline)}</span></>}
        </p>
      </div>
      <dl className="case-binding">
        <div><dt>Workflow</dt><dd>{c.workflow_key ?? "—"} <span className="muted">v{ctx.workflow?.version ?? c.workflow_version ?? "?"}</span></dd></div>
        <div>
          <dt>Domain pack</dt>
          <dd>
            {pack ? <>{pack.pack_key} <span className="muted">v{pack.version}</span></> : "none"}
            {pack?.binding && <small className="binding-note">{bindingNote[pack.binding] ?? humanize(pack.binding)}{pack.bound_at ? ` · ${formatDate(pack.bound_at)}` : ""}</small>}
          </dd>
        </div>
      </dl>
      <button className="btn sm soft rail-toggle" type="button" onClick={onOpenRail} aria-expanded={railOpen} aria-controls="case-rail">
        <Icon name="panel" size="sm" /> Context
      </button>
    </header>
  );
}
