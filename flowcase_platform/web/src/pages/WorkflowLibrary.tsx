import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ApiError } from "../api/client";
import { useResource } from "../api/useResource";
import { createWorkflow, workflowsPath, type Page, type Template, type WorkflowSummary } from "../api/workflows";
import { Icon } from "../components/Icon";
import { Modal } from "../components/Modal";
import { EmptyState, ErrorState, Loading, PageHead } from "../components/States";
import { useToast } from "../components/Toast";
import { canDesign } from "../features/session/permissions";
import { useCurrentSession } from "../features/session/SessionContext";

const tabs = [
  { status: "all", label: "All workflows" },
  { status: "published", label: "Published" },
  { status: "draft", label: "Drafts" },
];

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = window.setTimeout(() => setV(value), ms);
    return () => window.clearTimeout(t);
  }, [value, ms]);
  return v;
}

const keyPattern = /^[a-z][a-z0-9-]{0,62}$/;

function slugify(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").replace(/^[^a-z]+/, "").slice(0, 63);
}

export function NewWorkflowModal({ onClose }: { onClose: () => void }) {
  const [templates] = useResource<{ items: Template[] }>("/api/v1/workflow-templates");
  const [source, setSource] = useState("");
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const navigate = useNavigate();
  const toast = useToast();

  const templateName = templates.state === "ready" ? (templates.data.items.find((t) => t.key === source)?.name ?? "") : "";
  const effectiveKey = keyTouched ? key : slugify(name || templateName);
  const keyValid = keyPattern.test(effectiveKey);
  const nameValid = name.trim().length >= 3 || (source !== "" && name.trim() === "");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!keyValid || !nameValid) return;
    setBusy(true);
    setError(null);
    try {
      const res = await createWorkflow({ key: effectiveKey, ...(name.trim() ? { name: name.trim() } : {}), ...(source ? { template: source } : {}) });
      toast(`Created ${res.version.definition.name} (draft v1)`);
      navigate(`/designer/${res.version.workflow_key}/1`);
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, "unknown", "Unexpected error."));
      setBusy(false);
    }
  };

  return (
    <Modal title="New workflow" subtitle="Starts as draft v1. Publishing is a separate, authorized step." onClose={onClose}
      footer={<>
        <button className="btn" type="button" onClick={onClose}>Cancel</button>
        <button className="btn primary" type="submit" form="new-workflow" disabled={busy || !keyValid || !nameValid}>{busy ? "Creating…" : "Create draft"}</button>
      </>}>
      <form id="new-workflow" onSubmit={submit}>
        <label className="label" htmlFor="nw-source">Start from</label>
        <select className="field" id="nw-source" value={source} onChange={(e) => setSource(e.target.value)}>
          <option value="">Blank (start → end)</option>
          {templates.state === "ready" && templates.data.items.map((t) => (
            <option key={t.key} value={t.key}>{t.name} — {t.pack} template</option>
          ))}
        </select>
        <label className="label" htmlFor="nw-name">Name</label>
        <input className="field" id="nw-name" value={name} onChange={(e) => setName(e.target.value)}
          placeholder={source ? "Defaults to the template name" : "e.g. Vendor onboarding"} maxLength={200} />
        <label className="label" htmlFor="nw-key">Key</label>
        <input className="field" id="nw-key" value={effectiveKey} aria-invalid={!keyValid && effectiveKey !== ""}
          onChange={(e) => { setKeyTouched(true); setKey(e.target.value); }} placeholder="vendor-onboarding" maxLength={63} />
        <p className="hint">Lowercase letters, digits and hyphens. Permanent once created; cases record it.</p>
        {error && <div className="validation-box error" role="alert">{error.message}{error.traceId && <> · trace {error.traceId}</>}</div>}
      </form>
    </Modal>
  );
}

function WorkflowCard({ w }: { w: WorkflowSummary }) {
  const icon = w.domain_pack === "internal-poc-request" ? "workflow-icon poc" : "workflow-icon";
  return (
    <Link className="workflow-card" to={`/workflows/${w.key}`} aria-label={`${w.name} (${w.key})`}>
      <div className="flow-head">
        <div className={icon}><Icon name="workflow" /></div>
        <div className="pill-stack">
          {w.latest_published_version !== null && <span className="pill published">Published v{w.latest_published_version}</span>}
          {w.open_draft_version !== null && <span className="pill draft">Draft v{w.open_draft_version}</span>}
        </div>
      </div>
      <h3>{w.name}</h3>
      <p>{w.description || "No description."}</p>
      <div className="workflow-footer">
        <span><code>{w.key}</code> · {w.node_count} steps · {w.version_count} version{w.version_count === 1 ? "" : "s"}</span>
        <span className="link-text">Open <Icon name="arrow-up-right" size="sm" /></span>
      </div>
    </Link>
  );
}

export function WorkflowLibraryPage() {
  const session = useCurrentSession();
  const [status, setStatus] = useState("all");
  const [query, setQuery] = useState("");
  const q = useDebounced(query, 250);
  const [resource, reload] = useResource<Page<WorkflowSummary>>(workflowsPath(status, q));
  const [creating, setCreating] = useState(false);

  return (
    <>
      <PageHead title="Workflow Library" subtitle="Reusable process definitions with controlled versions and publication."
        actions={canDesign(session) && (
          <button className="btn primary" type="button" onClick={() => setCreating(true)}><Icon name="plus" size="sm" /> New workflow</button>
        )} />
      <div className="filterbar">
        <div className="tabs" role="tablist" aria-label="Filter workflows">
          {tabs.map((t) => (
            <button key={t.status} type="button" role="tab" aria-selected={status === t.status}
              className={status === t.status ? "tab active" : "tab"} onClick={() => setStatus(t.status)}>{t.label}</button>
          ))}
        </div>
        <div className="searchinput">
          <Icon name="search" size="sm" />
          <input aria-label="Search workflows" placeholder="Search workflows..." value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
      </div>
      {resource.state === "loading" && <Loading label="Loading workflows…" />}
      {resource.state === "error" && <ErrorState error={resource.error} onRetry={reload} />}
      {resource.state === "ready" && resource.data.items.length === 0 && (
        <div className="panel panel-pad">
          <EmptyState icon="workflow" title={q || status !== "all" ? "No matching workflows" : "No workflows yet"}>
            {canDesign(session) ? "Create one from the SIM tender or PoC template, or start blank." : "Workflow designers can create workflows here."}
          </EmptyState>
        </div>
      )}
      {resource.state === "ready" && resource.data.items.length > 0 && (
        <div className="workflow-grid">
          {resource.data.items.map((w) => <WorkflowCard key={w.id} w={w} />)}
          <div className="template-promo">
            <div>
              <strong>Same engine, different processes</strong>
              <p>Domain packs register their own policies, agents and roles. No workflow-specific engine code.</p>
            </div>
            <Link className="btn soft tinybtn" to="/integrations">Boundaries</Link>
          </div>
        </div>
      )}
      {creating && <NewWorkflowModal onClose={() => setCreating(false)} />}
    </>
  );
}
