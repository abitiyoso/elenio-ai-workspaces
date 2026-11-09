import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { useResource } from "../../api/useResource";
import { getCase } from "../../api/workspace";
import { useCaseNav } from "../../app/caseNavContext";
import { Icon } from "../../components/Icon";
import { ErrorState, Loading, PartialErrorBanner } from "../../components/States";
import { CaseHeader } from "./CaseHeader";
import type { DraftActions } from "./DraftReview";
import { caseTiming, effectiveNavigation, isLiveRun, navIcon, navId, revisingSteps } from "./model";
import { modules } from "./modules/registry";
import { UnsupportedView } from "./modules/Unsupported";
import { RightRail } from "./RightRail";
import { WorkflowStrip } from "./WorkflowStrip";

export function CaseWorkspacePage() {
  const { id = "", navId: navParam } = useParams();
  const location = useLocation();
  const { res, reload, set } = useResource(`case:${id}`, (signal) => getCase(id, signal));
  const { setNav } = useCaseNav();
  const [requested, setRequested] = useState<Map<string, number>>(() => new Map());
  const [railOpen, setRailOpen] = useState(false);
  const caseBase = `/cases/${encodeURIComponent(id)}`;
  const ctx = res.state === "ready" ? res.data : null;

  // Revisions requested from this view are watched for a while even before Flowcase reports the agent re-running.
  const [now, setNow] = useState(() => Date.now());
  const requestedSet = useMemo(() => new Set([...requested].filter(([, at]) => now - at < caseTiming.revisionWatchMs).map(([k]) => k)), [requested, now]);

  const navigation = useMemo(() => (ctx ? effectiveNavigation(ctx) : []), [ctx]);
  const active = navigation.find((n) => navId(n) === navParam) ?? navigation[0];
  const activeId = active ? navId(active) : "";

  // Poll every few seconds while an agent works on this case or a revision was just requested.
  const busy = !!ctx && (ctx.agent_runs.some(isLiveRun) || revisingSteps(ctx, requestedSet).length > 0 || requestedSet.size > 0);
  useEffect(() => {
    if (!busy) return;
    const t = window.setTimeout(() => { setNow(Date.now()); reload(); }, caseTiming.pollMs);
    return () => window.clearTimeout(t);
  }, [busy, reload, res]);

  // Publish the pack-generated navigation to the shell sidebar.
  useEffect(() => {
    if (!ctx) return;
    const pendingMine = ctx.drafts.filter((d) => d.status === "pending" && d.can_submit).length;
    setNav({
      caseId: id,
      reference: ctx.case.reference,
      ...(ctx.pack && ctx.pack.status !== "unavailable" ? { packTitle: `${ctx.pack.title} v${ctx.pack.version}` } : {}),
      activeId,
      items: navigation.map((n) => {
        const badge = n.view === "overview" || n.view === "tasks" ? pendingMine : n.view === "approvals" ? ctx.approvals.filter((a) => a.status === "pending").length : 0;
        return { id: navId(n), label: n.label, icon: navIcon(n), to: `${caseBase}/${encodeURIComponent(navId(n))}`, ...(badge ? { badge } : {}) };
      }),
    });
  }, [ctx, navigation, activeId, id, caseBase, setNav]);
  useEffect(() => () => setNav(null), [setNav]);

  useEffect(() => {
    if (location.hash !== "#your-review" || !ctx) return;
    document.getElementById("your-review")?.scrollIntoView({ block: "start" });
  }, [location.hash, ctx]);

  useEffect(() => {
    if (!railOpen) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setRailOpen(false); };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [railOpen]);

  const onRevisionRequested = useCallback((nodeId: string) => {
    const at = Date.now();
    setNow(at);
    setRequested((m) => new Map(m).set(nodeId, at));
  }, []);
  const actions: DraftActions = useMemo(() => ({ caseId: id, onUpdated: set, onReload: reload, onRevisionRequested, requested: requestedSet }),
    [id, set, reload, onRevisionRequested, requestedSet]);

  if (res.state === "loading") return <Loading label="Loading the case…" />;
  if (res.state === "error") return <><p><Link className="small-link" to="/">← My Cases</Link></p><ErrorState error={res.error} onRetry={reload} /></>;
  if (!ctx || !active) return null;

  const Module = modules[active.view];
  const unavailable = !ctx.pack || ctx.pack.status === "unavailable";
  const flowcaseDown = (ctx.partial_errors ?? []).some((e) => e.source === "flowcase");

  return (
    <div className="case-page">
      <div className="case-topline">
        <Link className="small-link" to="/"><Icon name="arrow-left" size="sm" /> My Cases</Link>
        {res.refreshing && <span className="tiny muted" role="status">Refreshing…</span>}
      </div>
      <CaseHeader ctx={ctx} onOpenRail={() => setRailOpen(true)} railOpen={railOpen} />
      {unavailable && (
        <div className="notice warning banner" role="alert">
          <Icon name="alert-circle" size="sm" />
          <span>
            <strong>{ctx.pack ? `Domain pack ${ctx.pack.pack_key} v${ctx.pack.version} is unavailable.` : "This case has no domain pack."}</strong>{" "}
            Showing a generic read-only shell: Flowcase steps, tasks, approvals and history only.
          </span>
        </div>
      )}
      <PartialErrorBanner errors={ctx.partial_errors} />
      {ctx.workflow && ctx.workflow.nodes.length > 0 ? <WorkflowStrip nodes={ctx.workflow.nodes} /> : !flowcaseDown && <p className="muted-note">Flowcase reported no workflow steps.</p>}
      <div className="case-layout">
        <div className="case-main" aria-busy={res.refreshing}>
          {Module ? <Module ctx={ctx} nav={active} navigation={navigation} actions={actions} caseBase={caseBase} /> : <UnsupportedView view={active.view} label={active.label} />}
        </div>
        <div className={railOpen ? "rail-shade open" : "rail-shade"} onClick={() => setRailOpen(false)} aria-hidden="true" />
        <aside id="case-rail" className={railOpen ? "case-rail open" : "case-rail"} aria-label="Case context">
          <div className="rail-head">
            <strong>Case context</strong>
            <button className="btn sm" type="button" onClick={() => setRailOpen(false)} aria-label="Close case context"><Icon name="x" size="sm" /></button>
          </div>
          <RightRail ctx={ctx} navigation={navigation} caseBase={caseBase} requested={requestedSet} />
        </aside>
      </div>
    </div>
  );
}
