import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ApiError } from "../api/client";
import { blockedReasonText, decideApproval, type Approval, type CaseDetail } from "../api/cases";
import { useResource } from "../api/useResource";
import { ArtifactView } from "../components/ArtifactView";
import { formatDate } from "../components/format";
import { Icon } from "../components/Icon";
import { EmptyState, ErrorState, Loading, PageHead } from "../components/States";
import { useToast } from "../components/Toast";

const tabs = [
  { status: "pending", label: "Pending" },
  { status: "decided", label: "Decided" },
  { status: "all", label: "All" },
];

const statusPill: Record<Approval["status"], string> = {
  pending: "pill review", approved: "pill approved", rejected: "pill rejected", returned: "pill draft", cancelled: "pill", invalidated: "pill failed",
};

const briefFields = ["case_summary", "customer_name", "deadline", "rfp_excerpt", "constraints"];

function show(v: unknown): string {
  if (typeof v === "string") return v;
  return JSON.stringify(v);
}

/** What the approver is deciding on: the case brief and data, loaded from the case the gate belongs to. */
function CaseBrief({ approval }: { approval: Approval }) {
  const [res] = useResource<CaseDetail>(`/api/v1/cases/${encodeURIComponent(approval.case.id)}`);
  const noOutputs = approval.covered_artifacts.length === 0;
  const scope = noOutputs
    ? "No agent output exists yet for this gate. You are deciding, from the case brief below, whether this case may proceed to the next step."
    : "Review the outputs below — they are what this gate approves — together with the case brief.";
  if (res.state === "loading") return <p className="tiny">Loading case brief…</p>;
  if (res.state === "error") return <div className="notice notice-gap">{scope} The case brief could not be loaded ({res.error.code}); open the case to review it.</div>;
  const vars = res.data.variables;
  const summary = typeof vars.case_summary === "string" ? vars.case_summary : "";
  const constraints = Array.isArray(vars.constraints) ? vars.constraints.filter((c): c is string => typeof c === "string") : [];
  const rest = Object.entries(vars).filter(([k]) => !briefFields.includes(k));
  const covered = approval.covered_artifacts.map((ca) => {
    const artifact = res.data.artifacts.find((a) => a.artifact_type === ca.type);
    const version = artifact?.versions.find((v) => v.version === ca.version);
    return { ...ca, content: version?.content };
  });
  return (
    <div className="case-brief" aria-label="Case brief">
      <div className="notice notice-gap">{scope}</div>
      {covered.map((ca) => (
        <details className="gate-artifact" key={`${ca.type}-${ca.version}`} open>
          <summary><strong>{ca.type}</strong> v{ca.version}{ca.status === "stale" && <span className="pill failed">stale</span>}</summary>
          {ca.content ? <ArtifactView content={ca.content} /> : <p className="tiny">This version is not available; open the case.</p>}
        </details>
      ))}
      <h3 className="case-brief-title">{noOutputs ? "Case brief" : "Case brief (context)"}</h3>
      {summary ? <p className="case-brief-text">{summary}</p> : <p className="tiny">No free-text brief was provided for this case.</p>}
      {typeof vars.customer_name === "string" && <div className="keyval"><span>Customer</span><strong>{vars.customer_name}</strong></div>}
      {typeof vars.deadline === "string" && <div className="keyval"><span>Deadline</span><strong>{formatDate(vars.deadline)}</strong></div>}
      {constraints.length > 0 && (
        <div className="keyval"><span>Constraints</span><strong><ul className="case-brief-list">{constraints.map((c) => <li key={c}>{c}</li>)}</ul></strong></div>
      )}
      {typeof vars.rfp_excerpt === "string" && vars.rfp_excerpt && (
        <details className="case-brief-rfp"><summary>RFP excerpt</summary><p className="case-brief-text">{vars.rfp_excerpt}</p></details>
      )}
      {rest.map(([k, v]) => <div className="keyval" key={k}><span>{k}</span><strong>{show(v)}</strong></div>)}
    </div>
  );
}

function DecisionCard({ approval, onDecided }: { approval: Approval; onDecided: () => void }) {
  const [rationale, setRationale] = useState("");
  const [confirmReject, setConfirmReject] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const toast = useToast();
  const decide = async (decision: "approve" | "reject" | "return") => {
    setBusy(true);
    setError(null);
    try {
      await decideApproval(approval.id, approval.version_num, decision, rationale.trim());
      toast(`${approval.title}: ${decision === "approve" ? "approved" : decision === "reject" ? "rejected" : "returned for rework"}`);
      onDecided();
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(0, "unknown", "Unexpected error."));
      setBusy(false);
    }
  };
  const ok = rationale.trim().length >= 5;
  return (
    <section className="panel panel-pad decision-card" aria-label={`Decision for ${approval.title}`}>
      <div className="panelhead">
        <div><h2>{approval.title}</h2><p><Link to={`/cases/${approval.case.id}`}>{approval.case.reference}</Link> · {approval.case.title}</p></div>
        <span className={statusPill[approval.status]}>{approval.status}</span>
      </div>
      <div className="keyval"><span>Policy</span><strong>{approval.policy_name || approval.policy_ref}{approval.policy_provisional && <span className="pill draft provisional">provisional</span>}</strong></div>
      <div className="keyval"><span>Gate</span><strong>{approval.node_id}{approval.iteration > 1 && ` · iteration ${approval.iteration}`}</strong></div>
      {approval.kind === "rereview" && (
        <div className="notice notice-gap"><strong>Re-review.</strong> A change invalidated the earlier decision for this gate. Approving restores it; the flow does not move.</div>
      )}
      {approval.invalidated_reason && <div className="validation-box notice-gap"><strong>Invalidated:</strong> {approval.invalidated_reason}</div>}
      {approval.covered_artifacts.length > 0 && (
        <div className="keyval"><span>Covers</span><strong className="covered">
          {approval.covered_artifacts.map((a) => <span key={a.type} className={a.status === "stale" ? "pill failed" : "pill"}>{a.type} v{a.version}</span>)}
        </strong></div>
      )}
      <CaseBrief approval={approval} />
      <div className="keyval"><span>Bound to state</span><strong><code title={approval.subject_hash}>{approval.subject_hash.slice(0, 16)}…</code></strong></div>
      <div className="keyval"><span>Requested</span><strong>{formatDate(approval.created_at)}</strong></div>
      {approval.policy_provisional && (
        <div className="notice notice-gap"><strong>Provisional policy.</strong> This gate's binding awaits business sign-off; decisions are still recorded and audited.</div>
      )}
      {approval.decision && (
        <div className="notice notice-gap">
          <strong>{approval.decision.decided_by_name}</strong> decided <strong>{approval.decision.decision}</strong> · {formatDate(approval.decision.decided_at)}
          <br />“{approval.decision.rationale}”<br /><span className="tiny">Decision hash <code>{approval.decision.decision_hash.slice(0, 16)}…</code></span>
        </div>
      )}
      {approval.status === "pending" && !approval.can_decide && (
        <div className="validation-box notice-gap" role="status">
          <strong>You cannot decide this approval.</strong> {blockedReasonText[approval.blocked_reason ?? ""] ?? approval.blocked_reason}
        </div>
      )}
      {approval.can_decide && (
        <>
          <label className="label" htmlFor="rationale">Rationale * <span className="muted">(recorded permanently)</span></label>
          <textarea className="field" id="rationale" value={rationale} maxLength={4000} onChange={(e) => setRationale(e.target.value)}
            placeholder="Why this decision? Reference the evidence you reviewed." />
          <div className="decision-actions">
            <button className="btn primary" type="button" disabled={!ok || busy} onClick={() => decide("approve")}><Icon name="check-circle" size="sm" /> Approve</button>
            {approval.return_to && <button className="btn" type="button" disabled={!ok || busy} onClick={() => decide("return")}>Return to {approval.return_to}</button>}
            {!confirmReject
              ? <button className="btn danger" type="button" disabled={!ok || busy} onClick={() => setConfirmReject(true)}>Reject…</button>
              : <button className="btn danger" type="button" disabled={busy} onClick={() => decide("reject")}>Confirm reject (closes the case)</button>}
          </div>
        </>
      )}
      {error && <div className="validation-box error notice-gap" role="alert">{error.message}</div>}
    </section>
  );
}

export function ApprovalsPage() {
  const [params, setParams] = useSearchParams();
  const [status, setStatus] = useState("pending");
  const [resource, reload] = useResource<{ items: Approval[] }>(`/api/v1/approvals?status=${status}`);
  const focus = params.get("focus");
  const items = resource.state === "ready" ? resource.data.items : [];
  const selected = items.find((a) => a.id === focus) ?? items[0] ?? null;

  return (
    <>
      <PageHead title="Approvals" subtitle="Human authorization with accountable, version-bound decisions."
        actions={<Link className="btn" to="/audit">Decision history <Icon name="arrow-up-right" size="sm" /></Link>} />
      <div className="filterbar">
        <div className="tabs" role="tablist" aria-label="Filter approvals">
          {tabs.map((t) => (
            <button key={t.status} type="button" role="tab" aria-selected={status === t.status} className={status === t.status ? "tab active" : "tab"}
              onClick={() => setStatus(t.status)}>{t.label}</button>
          ))}
        </div>
      </div>
      {resource.state === "loading" && <Loading label="Loading approvals…" />}
      {resource.state === "error" && <ErrorState error={resource.error} onRetry={reload} />}
      {resource.state === "ready" && (
        <div className="approvals-layout">
          <div className="panel">
            <div className="task-list">
              {items.length === 0 && <EmptyState icon="shield-check" title="No approvals">Approvals appear here for policies you hold a delegation for.</EmptyState>}
              {items.map((a) => (
                <button type="button" key={a.id} className={a.id === selected?.id ? "approval-row selected" : "approval-row"} aria-pressed={a.id === selected?.id}
                  onClick={() => setParams({ focus: a.id })}>
                  <div>
                    <strong>{a.title}{a.kind === "rereview" && <span className="pill draft rework">re-review</span>}</strong>
                    <p>{a.case.reference} · {a.policy_ref}{a.iteration > 1 && ` · iteration ${a.iteration}`}</p>
                  </div>
                  <span className={statusPill[a.status]}>{a.can_decide ? "your decision" : a.status}</span>
                </button>
              ))}
            </div>
          </div>
          <div className="dashboard-col">
            {selected && <DecisionCard key={selected.id + selected.version_num} approval={selected} onDecided={reload} />}
            <div className="panel panel-pad">
              <div className="panelhead"><div><h2>Approval safeguards</h2><p>Enforced by the server</p></div><div className="docmark"><Icon name="shield-check" /></div></div>
              <div className="keyval"><span>Decision owner</span><strong>Policy delegation</strong></div>
              <div className="keyval"><span>Version binding</span><strong>Case-state hash</strong></div>
              <div className="keyval"><span>Segregation of duties</span><strong>No own case or work</strong></div>
              <div className="keyval"><span>AI privileges</span><strong>No approval authority</strong></div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
