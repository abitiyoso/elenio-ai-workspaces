import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ApiError } from "../api/client";
import { addCaseNote, setCasePaused, type CaseDetail } from "../api/cases";
import { useResource } from "../api/useResource";
import type { NodeType, WorkflowDefinition } from "../api/workflows";
import { formatDate } from "../components/format";
import { Icon } from "../components/Icon";
import { ErrorState, Loading, PageHead } from "../components/States";
import { useToast } from "../components/Toast";
import { FlowGraph } from "../features/designer/FlowGraph";
import { CaseArtifacts } from "./CaseArtifacts";
import { CaseStatusPill } from "./Cases";
import { DraftHistoryPanel, DraftReviewPanel } from "./DraftReview";
import { revisingSteps } from "./draftState";

const execPill: Record<string, string> = {
  completed: "pill completed", waiting_human: "pill review", ready: "pill active", running: "pill running",
  failed: "pill failed", cancelled: "pill", superseded: "pill",
};

/** Latest execution per node decides how the graph shows it. */
function nodeStates(c: CaseDetail): Map<string, string> {
  const out = new Map<string, string>();
  for (const x of c.timeline) {
    if (x.status === "superseded" || x.status === "cancelled") continue;
    out.set(x.node_id, x.status === "completed" ? "done" : x.status === "failed" ? "failed" : "active");
  }
  return out;
}

function historyText(h: CaseDetail["history"][number]): string {
  const d = h.data;
  switch (h.action) {
    case "case.created": return `started the case on ${String(d.workflow_key)} v${String(d.workflow_version)}`;
    case "task.assigned": return `assigned ${String(d.node_id)} to ${String(d.assignee_role)}`;
    case "task.submitted": return `submitted ${String(d.node_id)}`;
    case "task.revision_requested": return `sent ${String(d.node_id)} back: “${String(d.rationale)}”`;
    case "approval.requested": return `requested approval at ${String(d.node_id)} (${String(d.policy_ref)})`;
    case "approval.decided": return `decided ${String(d.node_id)}: ${String(d.decision)}`;
    case "condition.evaluated": return `evaluated condition → ${String(d.target)} (${String(d.result)})`;
    case "case.note": return `noted: “${String(d.note)}”`;
    case "case.paused": return `paused the case${d.reason ? `: ${String(d.reason)}` : ""}`;
    case "case.resumed": return `resumed the case${d.reason ? `: ${String(d.reason)}` : ""}`;
    case "case.completed": return "completed the case";
    case "case.failed": return `case failed at ${String(d.node_id)}: ${String(d.error)}`;
    case "agent.started": return `started ${String(d.agent_ref)} (${String(d.evidence)} evidence, ${String(d.evidence_excluded)} withheld)`;
    case "agent.completed": return `received ${String(d.agent_ref)} output`;
    case "parallel.split": return "split into parallel branches";
    case "parallel.branch_arrived": return `branch arrived (${String(d.arrived)}/${String(d.expected)})`;
    case "parallel.joined": return `joined ${String(d.branches)} branches`;
    case "artifact.version_created": return `recorded ${String(d.type)} v${String(d.version)} (${String(d.kind)})`;
    case "artifact.revised": return `revised ${String(d.type)}: “${String(d.note)}”`;
    case "artifact.invalidated": return `invalidated downstream: ${String(d.reason)}`;
    case "approval.invalidated": return `invalidated approval at ${String(d.node_id)}`;
    case "approval.rereview_requested": return `re-opened review at ${String(d.node_id)}`;
    case "approval.rebound": return "re-bound a pending approval to the current versions";
    case "evidence.invalidated": return `evidence ${String(d.source_id)} is ${String(d.status)}`;
    case "timer.scheduled": return `timer set until ${String(d.wake_at)}`;
    case "timer.fired": return "timer fired";
    case "service.invoked": return `called ${String(d.connector)}.${String(d.operation)}`;
    case "draft.ready": return `${String(d.node_id)} draft v${String(d.artifact_version)} is ready for the case owner's review`;
    case "draft.submitted": return `submitted the ${String(d.node_id)} draft v${String(d.artifact_version)} to the approver`;
    case "draft.revision_requested": return `sent the ${String(d.node_id)} draft back to the agent: “${String(d.feedback)}”`;
    default: return h.action;
  }
}

export function CaseDetailPage() {
  const { id = "" } = useParams();
  const [resource, reload] = useResource<CaseDetail>(`/api/v1/cases/${encodeURIComponent(id)}`);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const toast = useToast();
  const c = resource.state === "ready" ? resource.data : null;
  const def = useMemo<WorkflowDefinition | null>(() => c && ({
    schema_version: "1.0", key: c.workflow_key, name: c.workflow_name, version: c.workflow_version,
    nodes: c.workflow_nodes.map((n) => ({ id: n.id, name: n.name, type: n.type as NodeType, next: n.next })),
  }), [c]);
  const states = useMemo(() => (c ? nodeStates(c) : new Map<string, string>()), [c]);
  // The worker advances cases asynchronously; refresh while steps are queued or running, and (slower)
  // while an agent regenerates a draft the owner sent back.
  const engineBusy = !!c?.timeline.some((x) => x.status === "ready" || x.status === "running");
  const agentRevising = !!c && revisingSteps(c).length > 0;
  useEffect(() => {
    if (!engineBusy && !agentRevising) return;
    const t = window.setTimeout(reload, engineBusy ? 1200 : 4000);
    return () => window.clearTimeout(t);
  }, [engineBusy, agentRevising, c, reload]);

  if (resource.state === "loading") return <Loading label="Loading case…" />;
  if (resource.state === "error") return <ErrorState error={resource.error} onRetry={reload} />;
  if (!c || !def) return null;

  const act = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      toast(done);
      reload();
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(0, "unknown", "Unexpected error."));
      if (e instanceof ApiError && e.status === 412) reload();
    } finally {
      setBusy(false);
    }
  };

  const openTasks = c.tasks.filter((t) => t.status === "assigned");
  const pending = c.approvals.filter((a) => a.status === "pending");
  const pendingDrafts = (c.drafts ?? []).filter((d) => d.status === "pending");
  return (
    <>
      <PageHead title={c.title} subtitle={`${c.reference} · ${c.workflow_name} v${c.workflow_version} · owner ${c.owner_name}`}
        actions={<>
          <Link className="btn" to="/cases">All cases</Link>
          {c.actions.can_pause && <button className="btn" type="button" disabled={busy} onClick={() => act(() => setCasePaused(c.id, c.version_num, true, "Paused from case view"), `${c.reference} paused`)}>Pause</button>}
          {c.actions.can_resume && <button className="btn primary" type="button" disabled={busy} onClick={() => act(() => setCasePaused(c.id, c.version_num, false, "Resumed from case view"), `${c.reference} resumed`)}>Resume</button>}
          <button className="btn" type="button" onClick={reload}><Icon name="refresh" size="sm" /> Refresh</button>
        </>} />
      {error && <ErrorState error={error} />}
      <DraftReviewPanel c={c} onChanged={reload} />

      <div className="case-summary">
        <div className="panel panel-pad">
          <div className="keyval"><span>Status</span><strong><CaseStatusPill status={c.status} outcome={c.outcome} /></strong></div>
          <div className="keyval"><span>Progress</span><strong>{c.progress}%</strong></div>
          <div className="keyval"><span>Pinned version</span><strong>{c.workflow_key} v{c.workflow_version}</strong></div>
          <div className="keyval"><span>Definition hash</span><strong><code title={c.definition_hash}>{c.definition_hash.slice(0, 16)}…</code></strong></div>
          <div className="keyval"><span>Started</span><strong>{formatDate(c.created_at)}</strong></div>
        </div>
        <div className="panel panel-pad">
          <div className="panelhead"><div><h2>Current work</h2><p>Open tasks and decisions</p></div>{engineBusy && <span className="pill running">engine working…</span>}</div>
          {openTasks.length === 0 && pending.length === 0 && pendingDrafts.length === 0 && <p className="hint">Nothing is waiting on a person right now.</p>}
          <div className="queue">
            {pendingDrafts.map((d) => (
              <div className="queue-item" key={d.id}>
                <div><strong>{d.node_name || d.node_id} · draft v{d.artifact_version}</strong><p>Agent draft · waiting for the case owner's review</p></div>
                <a className="link" href="#your-review">{d.can_submit ? "Review draft" : "View draft"}</a>
              </div>
            ))}
            {openTasks.map((t) => (
              <div className="queue-item" key={t.id}>
                <div><strong>{t.title}{t.iteration > 1 && ` · rework #${t.iteration}`}</strong><p>Task for {t.assignee_role}</p></div>
                <Link className="link" to="/tasks">Open in My Tasks</Link>
              </div>
            ))}
            {pending.map((a) => (
              <div className="queue-item" key={a.id}>
                <div><strong>{a.title}</strong><p>Approval · {a.policy_ref}{a.policy_provisional && " (provisional)"}</p></div>
                <Link className="link" to={`/approvals?focus=${a.id}`}>Open approval</Link>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="case-detail">
        <div className="panel">
          <div className="canvas-head"><div><h2>Process</h2><p>Live state of each step in the pinned version.</p></div></div>
          <div className="canvas-backdrop">
            <FlowGraph def={def} selected={null} issues={new Map()} onSelect={() => undefined} states={states} />
          </div>
          <div className="panel-pad">
            <h2 className="subhead">Execution timeline</h2>
            <ol className="timeline">
              {c.timeline.map((x) => (
                <li className={x.status === "completed" ? "tline done" : "tline"} key={x.id}>
                  <div className="ticon">{x.status === "completed" ? "✓" : x.iteration}</div>
                  <div>
                    <strong>{x.node_name}</strong> <span className={execPill[x.status] ?? "pill"}>{x.status.replace("_", " ")}</span>
                    <p>{x.node_type}{x.iteration > 1 && ` · iteration ${x.iteration}`}{x.attempt > 1 && ` · ${x.attempt} attempts`} · {formatDate(x.completed_at ?? x.created_at)}</p>
                    {x.last_error && <p className="field-error">{x.last_error}</p>}
                  </div>
                </li>
              ))}
            </ol>
          </div>
        </div>
        <div className="dashboard-col">
          <DraftHistoryPanel c={c} />
          <CaseArtifacts c={c} canRevise={c.actions.can_pause} onChanged={reload} />
          <div className="panel panel-pad">
            <div className="panelhead"><div><h2>Case data</h2><p>Variables used by conditions and tasks</p></div></div>
            {Object.entries(c.variables).map(([k, v]) => (
              <div className="keyval" key={k}><span>{k}</span><strong>{typeof v === "object" ? JSON.stringify(v) : String(v)}</strong></div>
            ))}
            {c.condition_evaluations.map((e, i) => (
              <div className="notice notice-gap" key={i}>
                <strong>{e.node_id}</strong> evaluated <code>{JSON.stringify(e.inputs)}</code> → <strong>{String(e.result)}</strong>, routed to <code>{e.target}</code>
              </div>
            ))}
          </div>
          <div className="panel panel-pad">
            <div className="panelhead"><div><h2>History</h2><p>Append-only, newest first</p></div></div>
            <form className="note-form" onSubmit={(e) => { e.preventDefault(); if (note.trim()) void act(() => addCaseNote(c.id, note.trim()).then(() => setNote("")), "Note added"); }}>
              <label className="sr-only" htmlFor="case-note">Add a note</label>
              <input className="field" id="case-note" placeholder="Add a note to the case history…" value={note} maxLength={4000} onChange={(e) => setNote(e.target.value)} />
              <button className="btn tinybtn" type="submit" disabled={busy || !note.trim()}>Add</button>
            </form>
            <div className="activity-list">
              {c.history.map((h, i) => (
                <div className="activity" key={i}>
                  <div className="activity-icon"><Icon name={h.actor_type === "system" ? "server" : "user"} size="sm" /></div>
                  <div><strong>{h.actor_name ?? "Flowcase engine"}</strong><p>{historyText(h)}</p></div>
                  <time dateTime={h.occurred_at}>{formatDate(h.occurred_at)}</time>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
