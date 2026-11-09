import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/client";
import { useResource } from "../api/useResource";
import {
  createDraftVersion, getVersion, publishVersion, saveDraft, validateDocument, validateVersion,
  type Catalog, type Diagnostic, type Page, type ValidationReport, type WorkflowDefinition, type WorkflowDetail,
  type WorkflowSummary, type WorkflowVersion,
} from "../api/workflows";
import { Icon } from "../components/Icon";
import { Modal } from "../components/Modal";
import { StatusPill } from "../components/StatusPill";
import { shortHash } from "../components/format";
import { EmptyState, ErrorState, Loading, PageHead } from "../components/States";
import { useToast } from "../components/Toast";
import { FlowGraph } from "../features/designer/FlowGraph";
import { diagnosticsByNode, insertStep } from "../features/designer/graph";
import { Inspector } from "../features/designer/Inspector";
import { canDesign, canPublish } from "../features/session/permissions";
import { useCurrentSession } from "../features/session/SessionContext";

/** /designer without a workflow: pick one. */
export function DesignerPickerPage() {
  const [resource, reload] = useResource<Page<WorkflowSummary>>("/api/v1/workflows");
  return (
    <>
      <PageHead title="Workflow Designer" subtitle="Compose processes from reusable nodes, validate, and publish immutable versions." />
      {resource.state === "loading" && <Loading />}
      {resource.state === "error" && <ErrorState error={resource.error} onRetry={reload} />}
      {resource.state === "ready" && (
        <div className="panel">
          <div className="task-list">
            {resource.data.items.length === 0 && <EmptyState icon="layers" title="No workflows yet">Create one in the <Link to="/workflows">Workflow Library</Link>.</EmptyState>}
            {resource.data.items.map((w) => (
              <div className="task-row" key={w.id}>
                <div>
                  <strong>{w.name}</strong>
                  <p><code>{w.key}</code> · {w.node_count} steps</p>
                </div>
                <div className="task-actions">
                  {w.open_draft_version !== null && <Link className="btn tinybtn primary" to={`/designer/${w.key}/${w.open_draft_version}`}>Edit draft v{w.open_draft_version}</Link>}
                  {w.latest_published_version !== null && <Link className="btn tinybtn" to={`/designer/${w.key}/${w.latest_published_version}`}>View v{w.latest_published_version}</Link>}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
}

const clone = (d: WorkflowDefinition): WorkflowDefinition => JSON.parse(JSON.stringify(d)) as WorkflowDefinition;
const asError = (e: unknown) => (e instanceof ApiError ? e : new ApiError(0, "unknown", "Unexpected error."));

/** 422 responses carry error details; show them in the same panel as validator output. */
function reportFromError(e: ApiError): ValidationReport | null {
  if (e.code !== "invalid_definition") return null;
  return { valid: false, diagnostics: e.details.map((d) => ({ severity: "error", code: d.code, message: d.message, path: d.path ?? "" })) };
}

export function DesignerPage() {
  const { key = "", version: versionParam = "1" } = useParams();
  const versionNo = Number(versionParam);
  const session = useCurrentSession();
  const navigate = useNavigate();
  const toast = useToast();

  const [detail, reloadDetail] = useResource<WorkflowDetail>(`/api/v1/workflows/${encodeURIComponent(key)}`);
  const [catalogRes] = useResource<Catalog>("/api/v1/workflow-catalog");
  const catalog = catalogRes.state === "ready" ? catalogRes.data : null;

  const [loaded, setLoaded] = useState<{ forKey: string; version: WorkflowVersion } | null>(null);
  const [loadError, setLoadError] = useState<ApiError | null>(null);
  const [doc, setDoc] = useState<WorkflowDefinition | null>(null);
  const [report, setReport] = useState<{ report: ValidationReport; unsaved: boolean } | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<ApiError | null>(null);
  const [conflict, setConflict] = useState(false);
  const [showJson, setShowJson] = useState(false);
  const [confirmPublish, setConfirmPublish] = useState(false);
  const [nonce, setNonce] = useState(0);
  const requestKey = `${key}/${versionNo}#${nonce}`;

  const mayValidate = canDesign(session) || canPublish(session);
  const accept = useCallback((v: WorkflowVersion, r: ValidationReport | null, forKey: string) => {
    setLoaded({ forKey, version: v });
    setDoc(clone(v.definition));
    if (r) setReport({ report: r, unsaved: false });
  }, []);

  useEffect(() => {
    const ctrl = new AbortController();
    getVersion(key, versionNo, ctrl.signal).then(
      async (v) => {
        accept(v, null, requestKey);
        setLoadError(null);
        setConflict(false);
        setReport(null);
        if (mayValidate) {
          const r = await validateDocument(v.definition).catch(() => null);
          if (!ctrl.signal.aborted && r) setReport({ report: r, unsaved: false });
        }
      },
      (e: unknown) => { if (!ctrl.signal.aborted) setLoadError(asError(e)); },
    );
    return () => ctrl.abort();
  }, [key, versionNo, requestKey, mayValidate, accept]);

  const version = loaded?.forKey === requestKey ? loaded.version : null;
  const dirty = useMemo(() => !!version && !!doc && JSON.stringify(doc) !== JSON.stringify(version.definition), [doc, version]);
  const isDraft = version?.status === "draft" || version?.status === "validated";
  const editable = isDraft && canDesign(session);

  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  const issues = useMemo(() => (doc && report ? diagnosticsByNode(doc, report.report.diagnostics) : new Map<string, Diagnostic[]>()), [doc, report]);

  if (loadError) return <ErrorState error={loadError} onRetry={() => setNonce((n) => n + 1)} />;
  if (!version || !doc) return <Loading label="Loading workflow version…" />;

  const run = async (label: string, fn: () => Promise<void>) => {
    setBusy(label);
    setActionError(null);
    try {
      await fn();
    } catch (e) {
      const err = asError(e);
      if (err.status === 412) setConflict(true);
      const r = reportFromError(err);
      if (r) setReport({ report: r, unsaved: false });
      else setActionError(err);
    } finally {
      setBusy(null);
      reloadDetail();
    }
  };

  const save = () => run("save", async () => {
    const res = await saveDraft(key, versionNo, version.version_num, doc);
    accept(res.version, res.validation, requestKey);
    toast(res.validation.valid ? "Draft saved · no validation errors" : `Draft saved · ${res.validation.diagnostics.filter((d) => d.severity === "error").length} validation errors`);
  });

  const validate = () => run("validate", async () => {
    if (dirty || !isDraft) {
      setReport({ report: await validateDocument(doc), unsaved: dirty });
      return;
    }
    const res = await validateVersion(key, versionNo);
    accept(res.version, res.validation, requestKey);
    toast(res.validation.valid ? `v${versionNo} validated` : "Validation found errors");
  });

  const publish = () => run("publish", async () => {
    setConfirmPublish(false);
    const res = await publishVersion(key, versionNo, version.version_num);
    accept(res.version, res.validation, requestKey);
    toast(`Published v${versionNo} · ${shortHash(res.version.definition_hash)}`);
  });

  const fork = () => run("fork", async () => {
    const res = await createDraftVersion(key, versionNo);
    navigate(`/designer/${key}/${res.version.version}`);
  });

  const addStep = () => {
    const res = insertStep(doc, selected);
    setDoc(res.def);
    setSelected(res.id);
  };

  const exportJson = () => {
    const blob = new Blob([JSON.stringify({ ...doc, status: version.status }, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${key}-v${versionNo}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const versions = detail.state === "ready" ? detail.data.versions : [];
  const openDraft = detail.state === "ready" ? detail.data.open_draft_version : null;
  const errors = report?.report.diagnostics.filter((d) => d.severity === "error") ?? [];
  const warnings = report?.report.diagnostics.filter((d) => d.severity === "warning") ?? [];
  const meta = isDraft
    ? `${version.status === "validated" ? "Validated" : "Draft"} v${versionNo}${version.derived_from_version ? ` • derived from v${version.derived_from_version}` : ""}`
    : `${version.status === "published" ? "Published" : "Deprecated"} v${versionNo} • immutable • ${shortHash(version.definition_hash)}`;

  return (
    <>
      <PageHead title="Workflow Designer" subtitle="Compose processes from reusable nodes, validate, and publish immutable versions."
        actions={<>
          <button className="btn" type="button" onClick={() => setShowJson((s) => !s)} aria-expanded={showJson}>{showJson ? "Hide JSON" : "Show JSON"}</button>
          <button className="btn" type="button" onClick={exportJson}><Icon name="download" size="sm" /> Export JSON</button>
          {mayValidate && (
            <button className="btn primary" type="button" disabled={!!busy} onClick={validate}><Icon name="check-circle" size="sm" /> {busy === "validate" ? "Validating…" : "Validate definition"}</button>
          )}
        </>} />

      <div className="designer-top">
        <div className="itemrow">
          <div className="workflow-icon"><Icon name="workflow" /></div>
          <div>
            <div className="designer-name">{doc.name} {dirty && <span className="pill draft">Unsaved changes</span>}</div>
            <div className="designer-meta">{meta}</div>
          </div>
        </div>
        <div className="actions">
          <select className="field version-select" aria-label="Select version" value={versionNo}
            onChange={(e) => navigate(`/designer/${key}/${e.target.value}`)}>
            {versions.length === 0 && <option value={versionNo}>v{versionNo}</option>}
            {versions.map((v) => <option key={v.id} value={v.version}>v{v.version} · {v.status}</option>)}
          </select>
          <StatusPill status={version.status} />
          {editable && (
            <>
              <button className="btn" type="button" disabled={!dirty || !!busy} onClick={() => { setDoc(clone(version.definition)); setSelected(null); }}>Discard edits</button>
              <button className="btn" type="button" disabled={!dirty || !!busy} onClick={save}><Icon name="save" size="sm" /> {busy === "save" ? "Saving…" : "Save draft"}</button>
            </>
          )}
          {isDraft && canPublish(session) && (
            <button className="btn primary" type="button" disabled={dirty || !!busy} title={dirty ? "Save the draft before publishing" : undefined}
              onClick={() => setConfirmPublish(true)}>Publish version <Icon name="arrow-up-right" size="sm" /></button>
          )}
          {!isDraft && canDesign(session) && openDraft === null && (
            <button className="btn primary" type="button" disabled={!!busy} onClick={fork}><Icon name="plus" size="sm" /> New draft from v{versionNo}</button>
          )}
          {!isDraft && openDraft !== null && <Link className="btn primary" to={`/designer/${key}/${openDraft}`}>Open draft v{openDraft}</Link>}
        </div>
      </div>

      {conflict && (
        <div className="validation-box error conflict" role="alert">
          <strong>This draft was changed elsewhere.</strong> Your edits were not saved.{" "}
          <button className="link" type="button" onClick={() => setNonce((n) => n + 1)}>Reload latest (discards your edits)</button>
        </div>
      )}
      {actionError && !conflict && <ErrorState error={actionError} />}

      <div className="designer-layout">
        <div className="panel canvas-panel">
          <div className="canvas-head">
            <div>
              <h2>Process graph</h2>
              <p>Select a step to inspect it. Rows follow execution order; parallel branches sit side by side.</p>
            </div>
            {editable && <button className="btn tinybtn soft" type="button" onClick={addStep}><Icon name="plus" size="sm" /> Add step</button>}
          </div>
          <div className="canvas-backdrop">
            <FlowGraph def={doc} selected={selected} issues={issues} onSelect={setSelected} />
          </div>
          <div className="flow-legend">
            <span><i className="legenddot" /> Agent / service</span>
            <span><i className="legenddot human" /> Human task / approval</span>
            <span><i className="legenddot branch" /> Routing / conditions</span>
          </div>
          {report && (
            <div className={report.report.valid ? "validation-box ok" : "validation-box error"} role="status" aria-live="polite">
              <strong>{report.report.valid ? "No validation errors" : `${errors.length} error${errors.length === 1 ? "" : "s"}`}</strong>
              {warnings.length > 0 && ` · ${warnings.length} warning${warnings.length === 1 ? "" : "s"}`}
              {report.unsaved && " · checked unsaved edits"}
              {(errors.length > 0 || warnings.length > 0) && (
                <ul>
                  {[...errors, ...warnings].map((d, i) => (
                    <li key={i}>
                      <span className={d.severity === "error" ? "sev error" : "sev"}>{d.severity}</span>{" "}
                      <code>{d.code}</code> {d.message}{" "}
                      {d.node_id || /^\/nodes\/\d+/.test(d.path)
                        ? <button className="link" type="button" onClick={() => setSelected(d.node_id ?? doc.nodes[Number(/^\/nodes\/(\d+)/.exec(d.path)?.[1])]?.id ?? null)}>{d.path}</button>
                        : <code className="muted">{d.path}</code>}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
          {showJson && <div className="json-toggle open"><pre>{JSON.stringify({ ...doc, status: version.status }, null, 2)}</pre></div>}
        </div>
        <Inspector def={doc} selected={selected} editable={editable} catalog={catalog}
          issues={selected ? (issues.get(selected) ?? []) : []} onChange={setDoc} onSelect={setSelected} />
      </div>
      <div className="notice notice-gap"><strong>Design principle:</strong> workflow definitions specify orchestration. Approvals, compliance and customer commitments remain separate, authorized domain policies. AI agents never approve.</div>

      {confirmPublish && (
        <Modal title={`Publish v${versionNo}?`} subtitle="Published versions are immutable. New cases may start on it; running cases stay on their version."
          onClose={() => setConfirmPublish(false)}
          footer={<>
            <button className="btn" type="button" onClick={() => setConfirmPublish(false)}>Cancel</button>
            <button className="btn primary" type="button" disabled={!!busy} onClick={publish}>Publish v{versionNo}</button>
          </>}>
          <p className="hint">The server re-validates the stored draft and records a SHA-256 content hash. This action is audited under your name.</p>
          {warnings.some((w) => w.code === "provisional_policy") && (
            <div className="validation-box"><strong>Provisional policies.</strong> Some approval gates use policy bindings that still await business sign-off.</div>
          )}
        </Modal>
      )}
    </>
  );
}
