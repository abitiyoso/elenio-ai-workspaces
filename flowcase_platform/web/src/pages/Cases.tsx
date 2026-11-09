import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ApiError } from "../api/client";
import { createCase, type CaseStatus, type CaseSummary, type DataSchema } from "../api/cases";
import { useResource } from "../api/useResource";
import { getVersion, type Page, type WorkflowSummary, type WorkflowVersion } from "../api/workflows";
import { DataForm } from "../components/DataForm";
import { toData, type FormValues } from "../components/formData";
import { Icon } from "../components/Icon";
import { Modal } from "../components/Modal";
import { EmptyState, ErrorState, Loading, PageHead } from "../components/States";
import { useToast } from "../components/Toast";
import { useCurrentSession } from "../features/session/SessionContext";

/** Chat-first case intake in the Universal Case Workspace app, served on the same origin. */
const CASE_WORKSPACE_INTAKE_URL = "/apps/case-workspace/cases/new";

const tabs = [
  { status: "all", label: "All cases" },
  { status: "active", label: "Active" },
  { status: "under_review", label: "Under review" },
  { status: "paused", label: "Paused" },
  { status: "closed", label: "Closed" },
];

const statusPill: Record<CaseStatus, string> = {
  active: "pill active",
  under_review: "pill review",
  paused: "pill draft",
  completed: "pill completed",
  cancelled: "pill",
  failed: "pill failed",
};

export function CaseStatusPill({ status, outcome }: { status: CaseStatus; outcome?: string | null }) {
  const label = status === "cancelled" && outcome?.startsWith("rejected") ? "Rejected" : status.replace("_", " ").replace(/^./, (c) => c.toUpperCase());
  return <span className={statusPill[status]}>{label}</span>;
}

function NewCaseModal({ onClose }: { onClose: () => void }) {
  const [workflows] = useResource<Page<WorkflowSummary>>("/api/v1/workflows?status=published&limit=100");
  const [caps] = useResource<{ supported_node_types: string[] }>("/api/v1/runtime-capabilities");
  const [key, setKey] = useState("");
  const [version, setVersion] = useState<WorkflowVersion | null>(null);
  const [title, setTitle] = useState("");
  const [values, setValues] = useState<FormValues>({});
  const [error, setError] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);
  const navigate = useNavigate();
  const toast = useToast();

  useEffect(() => {
    if (!key || workflows.state !== "ready") return;
    const w = workflows.data.items.find((x) => x.key === key);
    if (!w?.latest_published_version) return;
    const ctrl = new AbortController();
    getVersion(key, w.latest_published_version, ctrl.signal).then(setVersion, () => undefined);
    return () => ctrl.abort();
  }, [key, workflows]);

  const shown = version && version.workflow_key === key ? version : null;
  const supported = caps.state === "ready" ? caps.data.supported_node_types : [];
  const unsupported = shown ? [...new Set(shown.definition.nodes.map((n) => n.type).filter((t) => !supported.includes(t)))] : [];
  const schema = (shown?.definition.input_schema ?? { type: "object" }) as unknown as DataSchema;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const c = await createCase({ workflow_key: key, title: title.trim(), variables: toData(schema, values) });
      toast(`${c.reference} started on ${c.workflow_key} v${c.workflow_version}`);
      navigate(`/cases/${c.id}`);
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, "unknown", "Unexpected error."));
      setBusy(false);
    }
  };

  return (
    <Modal title="New case" subtitle="The case is pinned to the workflow's latest published version." onClose={onClose}
      footer={<>
        <button className="btn" type="button" onClick={onClose}>Cancel</button>
        <button className="btn primary" type="submit" form="new-case" disabled={busy || !shown || unsupported.length > 0 || !title.trim()}>
          {busy ? "Starting…" : "Start case"}
        </button>
      </>}>
      <form id="new-case" onSubmit={submit}>
        <label className="label" htmlFor="nc-workflow">Workflow</label>
        <select className="field" id="nc-workflow" value={key} onChange={(e) => { setKey(e.target.value); setValues({}); }}>
          <option value="">Choose a published workflow…</option>
          {workflows.state === "ready" && workflows.data.items.map((w) => (
            <option key={w.key} value={w.key}>{w.name} (v{w.latest_published_version})</option>
          ))}
        </select>
        {shown && unsupported.length > 0 && (
          <div className="validation-box" role="status">
            <strong>Not runnable yet.</strong> This workflow uses {unsupported.join(", ")} steps, which the runtime executes from Phase 3.
          </div>
        )}
        {shown && unsupported.length === 0 && (
          <>
            <label className="label" htmlFor="nc-title">Case title *</label>
            <input className="field" id="nc-title" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={300} />
            <DataForm schema={schema} values={values} onChange={setValues} idPrefix="nc" />
            <p className="hint">Pinned to v{shown.version} · hash <code>{shown.definition_hash?.slice(0, 12)}…</code></p>
          </>
        )}
        {error && (
          <div className="validation-box error" role="alert">
            {error.message}
            {error.details.length > 0 && <ul>{error.details.map((d, i) => <li key={i}><code>{d.path}</code> {d.message}</li>)}</ul>}
          </div>
        )}
      </form>
    </Modal>
  );
}

export function CasesPage() {
  const session = useCurrentSession();
  const [status, setStatus] = useState("all");
  const [query, setQuery] = useState("");
  const params = new URLSearchParams();
  if (status !== "all") params.set("status", status);
  if (query.trim()) params.set("q", query.trim());
  const [resource, reload] = useResource<Page<CaseSummary>>(`/api/v1/cases${params.size ? `?${params}` : ""}`);
  const [creating, setCreating] = useState(false);

  return (
    <>
      <PageHead title="Cases" subtitle="Every case runs on a pinned, published workflow version."
        actions={session?.roles.includes("case_owner") && (
          <>
            {/* Conversational intake lives in the Universal Case Workspace mini-app: same origin, not a router route. */}
            <a className="btn" href={CASE_WORKSPACE_INTAKE_URL}>Create with AI assistant</a>
            <button className="btn primary" type="button" onClick={() => setCreating(true)}><Icon name="plus" size="sm" /> New case</button>
          </>
        )} />
      <div className="filterbar">
        <div className="tabs" role="tablist" aria-label="Filter cases">
          {tabs.map((t) => (
            <button key={t.status} type="button" role="tab" aria-selected={status === t.status} className={status === t.status ? "tab active" : "tab"}
              onClick={() => setStatus(t.status)}>{t.label}</button>
          ))}
        </div>
        <div className="searchinput">
          <Icon name="search" size="sm" />
          <input aria-label="Search cases" placeholder="Search title or case ID..." value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
      </div>
      {resource.state === "loading" && <Loading label="Loading cases…" />}
      {resource.state === "error" && <ErrorState error={resource.error} onRetry={reload} />}
      {resource.state === "ready" && (
        <div className="main-table">
          <div className="table-actions">
            <div><h2>Case register</h2><small>Cases you own, work on, or may approve.</small></div>
            <span className="pill active">{resource.data.items.length}{resource.data.next_cursor ? "+" : ""} shown</span>
          </div>
          {resource.data.items.length === 0 ? (
            <div className="panel-pad"><EmptyState icon="briefcase" title="No cases">Start one from a published workflow.</EmptyState></div>
          ) : (
            <div className="table-scroll">
              <table className="tbl">
                <thead><tr><th>Case</th><th>Workflow</th><th>Owner</th><th>Progress</th><th>Status</th><th><span className="sr-only">Open</span></th></tr></thead>
                <tbody>
                  {resource.data.items.map((c) => (
                    <tr key={c.id}>
                      <td><div className="tdmain">{c.title}</div><div className="sub">{c.reference}{c.current_steps.length > 0 && ` · at ${c.current_steps.join(", ")}`}</div></td>
                      <td>{c.workflow_name}<div className="sub">v{c.workflow_version} · {c.definition_hash.slice(0, 8)}</div></td>
                      <td>{c.owner_name}</td>
                      <td><div className="progress" role="progressbar" aria-valuenow={c.progress} aria-valuemin={0} aria-valuemax={100} aria-label={`${c.progress}% complete`}><span style={{ width: `${c.progress}%` }} /></div></td>
                      <td><CaseStatusPill status={c.status} outcome={c.outcome} /></td>
                      <td className="text-right"><Link className="link" to={`/cases/${c.id}`}>Open</Link></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
      {creating && <NewCaseModal onClose={() => setCreating(false)} />}
    </>
  );
}
