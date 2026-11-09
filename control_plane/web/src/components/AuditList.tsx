import type { AuditEvent } from "../api/types";
import { fmtDateTime } from "../lib/format";
import { Icon } from "./Icons";
import { StatusPill } from "./ui";

export function AuditList({ events }: { events: AuditEvent[] }) {
  return (
    <ul className="itemlist">
      {events.map((e) => (
        <li key={e.id} className="item">
          <div className={`itemico${e.decision === "deny" ? " warn" : ""}`} aria-hidden="true">
            <Icon name={e.decision === "deny" ? "lock" : "scroll"} size="sm" />
          </div>
          <div className="itemtext">
            <strong>{e.action}</strong>
            <p>
              {e.actor_name ?? e.actor_type} → {e.target_type} <code>{e.target_id}</code>
            </p>
            <p className="tiny muted">{fmtDateTime(e.created_at)}</p>
          </div>
          {e.decision && <StatusPill value={e.decision} />}
        </li>
      ))}
    </ul>
  );
}
