import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { asApiError, type ApiError } from "../../api/client";
import type { CaseCard, CaseList, PackSummary } from "../../api/types";
import { useResource } from "../../api/useResource";
import { listCases, listPacks } from "../../api/workspace";
import { readPref, writePref } from "../../app/prefs";
import { useSession } from "../../app/sessionContext";
import { Icon } from "../../components/Icon";
import { relativeTime } from "../../components/format";
import { EmptyState, ErrorState, Loading, PageHead, PartialErrorBanner } from "../../components/States";
import { StatusPill } from "../../components/StatusPill";
import { PackPickerDialog } from "./PackPicker";
import { startablePacks } from "./startable";

const statuses = ["active", "under_review", "paused", "completed", "cancelled", "failed"];

function PackBadge({ c }: { c: CaseCard }) {
  if (!c.pack) return <span className="pack-badge muted-badge">No pack</span>;
  const accent = c.pack.brand?.accent;
  return (
    <span className="pack-badge" style={accent && /^#[0-9a-f]{3,8}$/i.test(accent) ? { borderColor: accent, color: accent } : undefined}>
      <Icon name={c.pack.brand?.icon ?? "blocks"} size="sm" /> {c.pack.title} <small>v{c.pack.version}</small>
    </span>
  );
}

function Progress({ value }: { value?: number | undefined }) {
  const v = Math.max(0, Math.min(100, Math.round(value ?? 0)));
  return (
    <div className="progress" role="progressbar" aria-valuenow={v} aria-valuemin={0} aria-valuemax={100} aria-label="Workflow progress">
      <span style={{ width: `${v}%` }} />
    </div>
  );
}

function Counts({ c }: { c: CaseCard }) {
  return (
    <div className="counts">
      <span title="Agent drafts waiting for review"><b>{c.pending_drafts ?? 0}</b> drafts</span>
      <span title="Flowcase approvals pending"><b>{c.pending_approvals ?? 0}</b> approvals</span>
      <span title="Open Flowcase tasks"><b>{c.open_tasks ?? 0}</b> tasks</span>
    </div>
  );
}

function CaseTile({ c }: { c: CaseCard }) {
  return (
    <article className="card case-tile" aria-labelledby={`case-${c.id}`}>
      <div className="case-tile-top">
        <span className="ref">{c.reference}</span>
        <StatusPill status={c.status} />
      </div>
      <h3 id={`case-${c.id}`}><Link className="stretched" to={`/cases/${encodeURIComponent(c.id)}`}>{c.title}</Link></h3>
      <PackBadge c={c} />
      <dl className="tile-meta">
        <div><dt>Owner</dt><dd>{c.owner_name ?? "—"}</dd></div>
        <div><dt>Current step</dt><dd>{c.current_steps?.length ? c.current_steps.join(", ") : "—"}</dd></div>
      </dl>
      <Progress value={c.progress} />
      <div className="case-tile-foot">
        <Counts c={c} />
        <span className="tiny muted">Updated {relativeTime(c.updated_at)}</span>
      </div>
    </article>
  );
}

function CaseTable({ items }: { items: CaseCard[] }) {
  return (
    <div className="card">
      <div className="table-scroll">
        <table className="table">
          <caption className="sr-only">My cases</caption>
          <thead><tr><th scope="col">Case</th><th scope="col">Domain pack</th><th scope="col">Current step</th><th scope="col">Owner</th><th scope="col">Progress</th><th scope="col">Pending</th><th scope="col">Status</th><th scope="col">Updated</th></tr></thead>
          <tbody>
            {items.map((c) => (
              <tr key={c.id}>
                <td><strong><Link to={`/cases/${encodeURIComponent(c.id)}`}>{c.title}</Link></strong><small>{c.reference}</small></td>
                <td><PackBadge c={c} /></td>
                <td>{c.current_steps?.join(", ") || "—"}</td>
                <td>{c.owner_name ?? "—"}</td>
                <td className="progress-cell"><Progress value={c.progress} /><small>{Math.round(c.progress ?? 0)}%</small></td>
                <td><Counts c={c} /></td>
                <td><StatusPill status={c.status} /></td>
                <td className="nowrap">{relativeTime(c.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export function MyCasesPage() {
  const navigate = useNavigate();
  const { me } = useSession();
  const [q, setQ] = useState("");
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [pack, setPack] = useState("");
  const [view, setView] = useState(() => readPref("cases.view", "cards"));
  const [more, setMore] = useState<{ key: string; page: CaseList } | null>(null);
  const [moreError, setMoreError] = useState<ApiError | null>(null);
  const [picker, setPicker] = useState<{ packs: PackSummary[] } | null>(null);
  const [pickerError, setPickerError] = useState<ApiError | null>(null);
  const [opening, setOpening] = useState(false);

  useEffect(() => {
    const t = window.setTimeout(() => setQuery(q.trim()), 300);
    return () => window.clearTimeout(t);
  }, [q]);

  const listKey = `cases?q=${query}&status=${status}`;
  const { res, reload } = useResource(listKey, (signal) => listCases({ q: query, status }, signal));
  // Extra pages belong to one query; a new search starts from its first page again.
  const extra = more?.key === listKey ? more.page : null;

  const all = useMemo(() => (res.state === "ready" ? [...res.data.items, ...(extra?.items ?? [])] : []), [res, extra]);
  const packOptions = useMemo(() => {
    const m = new Map<string, string>();
    for (const c of all) if (c.pack) m.set(c.pack.pack_key, c.pack.title);
    return [...m.entries()];
  }, [all]);
  const items = pack ? all.filter((c) => c.pack?.pack_key === pack) : all;
  const nextCursor = extra ? extra.next_cursor : res.state === "ready" ? res.data.next_cursor : null;
  const canCreate = me.state !== "ready" || me.data.flowcase.can_create_cases;

  const setViewPref = (v: string) => { setView(v); writePref("cases.view", v); };

  const loadMore = async () => {
    if (!nextCursor) return;
    setMoreError(null);
    try {
      const page = await listCases({ q: query, status, cursor: nextCursor });
      setMore({ key: listKey, page: { ...page, items: [...(extra?.items ?? []), ...page.items] } });
    } catch (err) {
      setMoreError(asApiError(err));
    }
  };

  const newCase = async () => {
    setOpening(true);
    setPickerError(null);
    try {
      const packs = startablePacks((await listPacks()).items);
      const only = packs.length === 1 ? packs[0] : undefined;
      if (only) navigate(`/cases/new?pack=${encodeURIComponent(only.pack_key)}`);
      else setPicker({ packs });
    } catch (err) {
      setPickerError(asApiError(err));
    } finally {
      setOpening(false);
    }
  };

  const partial = res.state === "ready" ? res.data.partial_errors : undefined;
  const flowcaseDown = partial?.some((e) => e.source === "flowcase");

  return (
    <>
      <PageHead eyebrow="CASE WORKSPACE" title="My Cases" subtitle="Every case you can see in Flowcase, rendered by its domain pack."
        actions={<>
          <button className="btn primary" type="button" onClick={() => void newCase()} disabled={!canCreate || opening}
            title={canCreate ? undefined : "Your Flowcase role cannot create cases"}>
            <Icon name="plus" size="sm" /> {opening ? "Opening…" : "New case"}
          </button>
        </>} />
      {!canCreate && <p className="notice tight">Your Flowcase role cannot create cases. Ask a workspace admin for the case owner role.</p>}
      {pickerError && <ErrorState error={pickerError} compact onRetry={() => void newCase()} />}
      <PartialErrorBanner errors={partial} what={flowcaseDown && all.length === 0 ? "showing nothing" : "showing partial data"} />

      <div className="filters" role="search">
        <div className="filters-left">
          <label className="sr-only" htmlFor="case-search">Search cases</label>
          <input id="case-search" className="input search" type="search" placeholder="Search by title or reference…" value={q} onChange={(e) => setQ(e.target.value)} />
          <label className="sr-only" htmlFor="case-status">Status</label>
          <select id="case-status" className="select" value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">All statuses</option>
            {statuses.map((s) => <option key={s} value={s}>{s.replace(/_/g, " ")}</option>)}
          </select>
          <label className="sr-only" htmlFor="case-pack">Domain pack</label>
          <select id="case-pack" className="select" value={pack} onChange={(e) => setPack(e.target.value)}>
            <option value="">All packs</option>
            {packOptions.map(([k, t]) => <option key={k} value={k}>{t}</option>)}
          </select>
        </div>
        <div className="segmented" role="group" aria-label="View">
          <button type="button" aria-pressed={view === "cards"} onClick={() => setViewPref("cards")}><Icon name="cards" size="sm" /> Cards</button>
          <button type="button" aria-pressed={view === "table"} onClick={() => setViewPref("table")}><Icon name="table" size="sm" /> Table</button>
        </div>
      </div>

      {res.state === "loading" && <Loading label="Loading your cases…" />}
      {res.state === "error" && <ErrorState error={res.error} onRetry={reload} />}
      {res.state === "ready" && (items.length === 0 ? (
        <div className="card pad">
          <EmptyState icon="briefcase" title={query || status || pack ? "No cases match these filters" : "No cases yet"}>
            {query || status || pack ? "Clear the search or filters to see more." : "Start one with New case: a short chat prepares the brief."}
          </EmptyState>
        </div>
      ) : view === "table" ? <CaseTable items={items} /> : (
        <div className="case-grid">{items.map((c) => <CaseTile key={c.id} c={c} />)}</div>
      ))}
      {nextCursor && <div className="load-more"><button className="btn" type="button" onClick={() => void loadMore()}>Load more</button></div>}
      {moreError && <ErrorState error={moreError} compact />}
      {picker && <PackPickerDialog packs={picker.packs} onClose={() => setPicker(null)} />}
    </>
  );
}
