import { useEffect, useId, useState } from "react";
import { useSession } from "../session/context";
import { useWorkspaceDirectory } from "../lib/workspaces";
import { api } from "../api/endpoints";
import { apiGet, toApiError, type ApiRequestError } from "../api/client";
import type { AuditEvent, Page } from "../api/types";
import { fmtDateTime } from "../lib/format";
import { Icon } from "../components/Icons";
import { Card, EmptyState, ErrorState, PageHead, Skeleton, StatusPill } from "../components/ui";
import { NotPermitted } from "./NotPermitted";

const PAGE_SIZE = 50;

export function AuditPage() {
  const { me } = useSession();
  if (!me.capabilities.view_audit) return <NotPermitted area="Audit Log" />;
  return <Audit />;
}

function buildQuery(filters: { action: string; workspace: string }, cursor: string | null): string {
  const p = new URLSearchParams({ limit: String(PAGE_SIZE) });
  if (cursor) p.set("cursor", cursor);
  if (filters.action.trim()) p.set("action", filters.action.trim());
  if (filters.workspace) p.set("workspace_id", filters.workspace);
  return `${api.admin.audit}?${p.toString()}`;
}

interface Feed {
  key: string;
  items: AuditEvent[];
  next: string | null;
  error?: ApiRequestError;
  loadingMore?: boolean;
}

function Audit() {
  const { me, epoch } = useSession();
  const wsDir = useWorkspaceDirectory(me, me.capabilities.manage_workspaces);
  const [draft, setDraft] = useState({ action: "", workspace: "" });
  const [filters, setFilters] = useState({ action: "", workspace: "" });
  const [nonce, setNonce] = useState(0);
  const key = `${epoch}:${nonce}:${filters.action}:${filters.workspace}`;
  const [feed, setFeed] = useState<Feed>({ key: "", items: [], next: null });
  const ids = { action: useId(), ws: useId() };

  useEffect(() => {
    const ctrl = new AbortController();
    apiGet<Page<AuditEvent>>(buildQuery(filters, null), ctrl.signal).then(
      (p) => {
        if (!ctrl.signal.aborted) setFeed({ key, items: p.items, next: p.next_cursor });
      },
      (err: unknown) => {
        if (!ctrl.signal.aborted) setFeed({ key, items: [], next: null, error: toApiError(err) });
      },
    );
    return () => ctrl.abort();
  }, [key, filters]);

  const loadMore = async () => {
    if (!feed.next) return;
    setFeed((f) => ({ ...f, loadingMore: true, error: undefined }));
    try {
      const p = await apiGet<Page<AuditEvent>>(buildQuery(filters, feed.next));
      setFeed((f) => (f.key === key ? { key, items: [...f.items, ...p.items], next: p.next_cursor } : f));
    } catch (err) {
      setFeed((f) => ({ ...f, loadingMore: false, error: toApiError(err) }));
    }
  };

  const loading = feed.key !== key;
  const wsOptions = wsDir.all;

  return (
    <>
      <PageHead eyebrow="ADMINISTRATION" title="Audit Log">
        Append-only record of sign-ins, decisions and admin changes, newest first. Metadata is redacted by the server.
      </PageHead>
      <Card pad={false}>
        <form
          className="cardtoolbar"
          onSubmit={(e) => {
            e.preventDefault();
            setFilters(draft);
            setNonce((n) => n + 1);
          }}
        >
          <label className="search" htmlFor={ids.action}>
            <Icon name="search" size="sm" />
            <span className="sr-only">Action</span>
            <input
              id={ids.action}
              placeholder="Action, e.g. role_binding.granted"
              value={draft.action}
              onChange={(e) => setDraft({ ...draft, action: e.target.value })}
            />
          </label>
          <label className="sr-only" htmlFor={ids.ws}>
            Workspace
          </label>
          <select id={ids.ws} className="select narrow" value={draft.workspace} onChange={(e) => setDraft({ ...draft, workspace: e.target.value })}>
            <option value="">All workspaces</option>
            {wsOptions.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
          <button type="submit" className="btn sm primary">
            Apply
          </button>
          {(filters.action || filters.workspace) && (
            <button
              type="button"
              className="btn sm"
              onClick={() => {
                const cleared = { action: "", workspace: "" };
                setDraft(cleared);
                setFilters(cleared);
              }}
            >
              Clear
            </button>
          )}
        </form>
        {loading ? (
          <div className="cardbody">
            <Skeleton lines={6} />
          </div>
        ) : feed.error && feed.items.length === 0 ? (
          <div className="cardbody">
            <ErrorState error={feed.error} title="Could not load audit events" onRetry={() => setNonce((n) => n + 1)} />
          </div>
        ) : feed.items.length === 0 ? (
          <EmptyState title="No audit events match" icon="scroll" />
        ) : (
          <>
            <div className="tablewrap">
              <table className="tbl audit">
                <thead>
                  <tr>
                    <th scope="col">
                      <span className="sr-only">Expand</span>
                    </th>
                    <th scope="col">Time</th>
                    <th scope="col">Action</th>
                    <th scope="col">Actor</th>
                    <th scope="col">Target</th>
                    <th scope="col">Decision</th>
                  </tr>
                </thead>
                <tbody>
                  {feed.items.map((e) => (
                    <AuditRow key={e.id} e={e} wsName={wsDir.name} />
                  ))}
                </tbody>
              </table>
            </div>
            <div className="loadmore">
              {feed.error && <ErrorState error={feed.error} title="Could not load more" compact />}
              {feed.next ? (
                <button type="button" className="btn" onClick={() => void loadMore()} disabled={feed.loadingMore}>
                  {feed.loadingMore ? "Loading…" : "Load more"}
                </button>
              ) : (
                <span className="tiny muted">End of log · {feed.items.length} events</span>
              )}
            </div>
          </>
        )}
      </Card>
    </>
  );
}

function AuditRow({ e, wsName }: { e: AuditEvent; wsName: (id: string) => string }) {
  const [open, setOpen] = useState(false);
  const detailId = useId();
  return (
    <>
      <tr>
        <td>
          <button
            type="button"
            className="iconbtn sm"
            aria-expanded={open}
            aria-controls={detailId}
            aria-label={open ? `Collapse event ${e.id}` : `Expand event ${e.id}`}
            onClick={() => setOpen((o) => !o)}
          >
            <Icon name={open ? "chevron-down" : "chevron-right"} size="sm" />
          </button>
        </td>
        <td className="nowrap">{fmtDateTime(e.created_at)}</td>
        <td>
          <code>{e.action}</code>
        </td>
        <td>
          {e.actor_name ?? <span className="muted">{e.actor_type}</span>}
          <span className="cellsub">{e.actor_type}</span>
        </td>
        <td>
          {e.target_type}
          <span className="cellsub">
            <code>{e.target_id}</code>
          </span>
        </td>
        <td>
          {e.decision ? <StatusPill value={e.decision} /> : <span className="muted">—</span>}
          {e.reason_code && <span className="cellsub">{e.reason_code}</span>}
        </td>
      </tr>
      {open && (
        <tr className="expandrow">
          <td colSpan={6} id={detailId}>
            <dl className="auditmeta">
              <div>
                <dt>Event id</dt>
                <dd>{e.id}</dd>
              </div>
              <div>
                <dt>Correlation</dt>
                <dd>
                  <code>{e.correlation_id}</code>
                </dd>
              </div>
              <div>
                <dt>Workspace</dt>
                <dd>{e.workspace_id ? wsName(e.workspace_id) : "—"}</dd>
              </div>
              <div>
                <dt>Policy</dt>
                <dd>{e.policy_version ?? "—"}</dd>
              </div>
              {e.rationale && (
                <div className="full">
                  <dt>Rationale</dt>
                  <dd>{e.rationale}</dd>
                </div>
              )}
            </dl>
            <pre className="routebox">{JSON.stringify(e.metadata, null, 2)}</pre>
          </td>
        </tr>
      )}
    </>
  );
}
