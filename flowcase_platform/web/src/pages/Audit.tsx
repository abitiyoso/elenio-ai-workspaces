import { useEffect, useState } from "react";
import { apiGet, ApiError } from "../api/client";
import type { AuditEvent, Page } from "../api/workflows";
import { describe } from "../features/audit/describe";
import { Icon, type IconName } from "../components/Icon";
import { formatDate } from "../components/format";
import { EmptyState, ErrorState, Loading, PageHead } from "../components/States";

const actionIcon = (action: string): IconName =>
  action.endsWith("published") ? "shield-check" : action.endsWith("validated") ? "check-circle" : action.endsWith("deprecated") ? "clock" : "workflow";

function detail(e: AuditEvent): string {
  const parts: string[] = [];
  const d = e.data;
  if (typeof d.definition_hash === "string") parts.push(`hash ${d.definition_hash.slice(0, 12)}…`);
  if (typeof d.valid === "boolean") parts.push(d.valid ? "valid" : `${String(d.error_count)} errors`);
  if (typeof d.template === "string") parts.push(`from template ${d.template}`);
  if (typeof d.duplicated_from === "string") parts.push(`copied from ${d.duplicated_from}`);
  if (typeof d.derived_from_version === "number") parts.push(`from v${d.derived_from_version}`);
  if (e.trace_id) parts.push(`trace ${e.trace_id.slice(0, 12)}`);
  return parts.join(" · ");
}

export function AuditPage() {
  const [items, setItems] = useState<AuditEvent[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "more">("loading");
  const [error, setError] = useState<ApiError | null>(null);
  const [nonce, setNonce] = useState(0);
  const [subject, setSubject] = useState("");
  const base = `/api/v1/audit-events?limit=50${subject ? `&subject_type=${subject}` : ""}`;

  useEffect(() => {
    const ctrl = new AbortController();
    apiGet<Page<AuditEvent>>(base, ctrl.signal).then(
      (p) => { setItems(p.items); setCursor(p.next_cursor); setError(null); setState("ready"); },
      (e: unknown) => { if (!ctrl.signal.aborted) { setError(e instanceof ApiError ? e : new ApiError(0, "unknown", "Unexpected error.")); setState("ready"); } },
    );
    return () => ctrl.abort();
  }, [nonce, base]);

  const more = async () => {
    if (!cursor) return;
    setState("more");
    try {
      const p = await apiGet<Page<AuditEvent>>(`${base}&cursor=${encodeURIComponent(cursor)}`);
      setItems((prev) => [...prev, ...p.items]);
      setCursor(p.next_cursor);
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(0, "unknown", "Unexpected error."));
    }
    setState("ready");
  };

  return (
    <>
      <PageHead title="Activity & Audit" subtitle="Append-only server records: who did what, to which version, and when."
        actions={<>
          <select className="field version-select" aria-label="Filter by subject" value={subject} onChange={(e) => { setState("loading"); setSubject(e.target.value); }}>
            <option value="">All subjects</option>
            {["workflow", "case", "task", "approval", "artifact", "node", "agent_run"].map((s) => <option key={s} value={s}>{s.replace("_", " ")}</option>)}
          </select>
          <button className="btn" type="button" onClick={() => { setState("loading"); setNonce((n) => n + 1); }}><Icon name="refresh" size="sm" /> Refresh</button>
        </>} />
      {state === "loading" && <Loading label="Loading audit events…" />}
      {error?.status === 403 && (
        <div className="panel panel-pad"><EmptyState icon="lock" title="Audit log restricted">Workflow designers, publishers and tenant admins can view the audit trail.</EmptyState></div>
      )}
      {error && error.status !== 403 && <ErrorState error={error} onRetry={() => setNonce((n) => n + 1)} />}
      {state !== "loading" && !error && (
        <div className="panel">
          <div className="table-actions">
            <div>
              <h2>Activity stream</h2>
              <small>Records cannot be edited or deleted, by design and by database rule.</small>
            </div>
            <span className="pill active">{items.length}{cursor ? "+" : ""} events</span>
          </div>
          {items.length === 0 && <div className="panel-pad"><EmptyState icon="clock" title="No activity yet">Workflow changes will appear here.</EmptyState></div>}
          <ol className="audit-list">
            {items.map((e) => (
              <li className="audit-row" key={e.id}>
                <div className="audit-dot"><Icon name={actionIcon(e.action)} size="sm" /></div>
                <div>
                  <strong>{e.actor_name ?? e.actor_type} {describe(e)}</strong>
                  <p>{detail(e) || e.action}</p>
                </div>
                <time dateTime={e.occurred_at}>{formatDate(e.occurred_at)}</time>
              </li>
            ))}
          </ol>
          {cursor && (
            <div className="panel-pad load-more">
              <button className="btn" type="button" disabled={state === "more"} onClick={more}>{state === "more" ? "Loading…" : "Load older events"}</button>
            </div>
          )}
        </div>
      )}
    </>
  );
}
