import type { HistoryEntry } from "../../../api/types";
import { formatDate, humanize } from "../../../components/format";
import { EmptyState, PanelError } from "../../../components/States";
import { errorsFor } from "../model";
import type { ModuleProps } from "./types";

function summary(h: HistoryEntry): string | null {
  const d = h.data ?? {};
  for (const k of ["body", "note", "reason", "rationale", "message", "title"]) {
    const v = d[k];
    if (typeof v === "string" && v.trim()) return v;
  }
  const scalars = Object.entries(d).filter(([, v]) => ["string", "number", "boolean"].includes(typeof v)).slice(0, 3);
  return scalars.length ? scalars.map(([k, v]) => `${humanize(k)}: ${String(v)}`).join(" · ") : null;
}

export function TimelineModule({ ctx, nav }: ModuleProps) {
  const errors = errorsFor(ctx, ["flowcase"]);
  const items = [...ctx.history].sort((a, b) => (b.occurred_at ?? "").localeCompare(a.occurred_at ?? ""));
  return (
    <div className="stack">
      <div className="module-head"><div><h2>{nav.label}</h2><p>Source-stamped case activity from Flowcase.</p></div></div>
      {errors.map((e, i) => <PanelError key={i} error={e} />)}
      {items.length === 0 ? <div className="card pad"><EmptyState icon="clock-3" title="No activity yet" /></div> : (
        <ol className="card pad timeline">
          {items.map((h, i) => {
            const s = summary(h);
            return (
              <li key={i}>
                <span className="timeline-dot" aria-hidden="true" />
                <div>
                  <strong>{humanize(h.action)}</strong>
                  <p>{h.actor_name ?? humanize(h.actor_type ?? "system")} · <time dateTime={h.occurred_at}>{formatDate(h.occurred_at)}</time></p>
                  {s && <p className="timeline-note">{s}</p>}
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
