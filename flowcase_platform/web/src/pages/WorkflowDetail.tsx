import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/client";
import { useResource } from "../api/useResource";
import { createDraftVersion, deprecateVersion, duplicateWorkflow, type VersionSummary, type WorkflowDetail } from "../api/workflows";
import { Icon } from "../components/Icon";
import { Modal } from "../components/Modal";
import { StatusPill } from "../components/StatusPill";
import { formatDate, shortHash } from "../components/format";
import { ErrorState, Loading, PageHead } from "../components/States";
import { useToast } from "../components/Toast";
import { canDesign, canPublish } from "../features/session/permissions";
import { useCurrentSession } from "../features/session/SessionContext";

function DuplicateModal({ wf, onClose }: { wf: WorkflowDetail; onClose: () => void }) {
  const [key, setKey] = useState(`${wf.key}-copy`.slice(0, 63));
  const [name, setName] = useState(`${wf.name} (copy)`);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const navigate = useNavigate();
  const toast = useToast();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await duplicateWorkflow(wf.key, { key, name });
      toast(`Duplicated into ${res.version.workflow_key}`);
      navigate(`/designer/${res.version.workflow_key}/1`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Unexpected error.");
      setBusy(false);
    }
  };
  return (
    <Modal title="Duplicate workflow" subtitle={`Copies the latest published version of ${wf.key} into a new draft v1.`} onClose={onClose}
      footer={<>
        <button className="btn" type="button" onClick={onClose}>Cancel</button>
        <button className="btn primary" type="submit" form="dup-form" disabled={busy}>{busy ? "Duplicating…" : "Duplicate"}</button>
      </>}>
      <form id="dup-form" onSubmit={submit}>
        <label className="label" htmlFor="dup-name">Name</label>
        <input className="field" id="dup-name" value={name} onChange={(e) => setName(e.target.value)} maxLength={200} />
        <label className="label" htmlFor="dup-key">Key</label>
        <input className="field" id="dup-key" value={key} onChange={(e) => setKey(e.target.value)} maxLength={63} />
        {error && <div className="validation-box error" role="alert">{error}</div>}
      </form>
    </Modal>
  );
}

export function WorkflowDetailPage() {
  const { key = "" } = useParams();
  const session = useCurrentSession();
  const [resource, reload] = useResource<WorkflowDetail>(`/api/v1/workflows/${encodeURIComponent(key)}`);
  const [duplicating, setDuplicating] = useState(false);
  const [confirmDeprecate, setConfirmDeprecate] = useState<VersionSummary | null>(null);
  const [actionError, setActionError] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);
  const navigate = useNavigate();
  const toast = useToast();

  if (resource.state === "loading") return <Loading label="Loading workflow…" />;
  if (resource.state === "error") return <ErrorState error={resource.error} onRetry={reload} />;
  const wf = resource.data;

  const fork = async () => {
    setBusy(true);
    setActionError(null);
    try {
      const res = await createDraftVersion(wf.key);
      toast(`Draft v${res.version.version} created from v${res.version.derived_from_version}`);
      navigate(`/designer/${wf.key}/${res.version.version}`);
    } catch (err) {
      setActionError(err instanceof ApiError ? err : new ApiError(0, "unknown", "Unexpected error."));
      setBusy(false);
    }
  };

  const deprecate = async (v: VersionSummary) => {
    setBusy(true);
    setActionError(null);
    try {
      await deprecateVersion(wf.key, v.version, v.version_num);
      toast(`v${v.version} deprecated for new cases`);
      setConfirmDeprecate(null);
      reload();
    } catch (err) {
      setActionError(err instanceof ApiError ? err : new ApiError(0, "unknown", "Unexpected error."));
      setConfirmDeprecate(null);
    } finally {
      setBusy(false);
    }
  };

  const latest = wf.versions[0];
  return (
    <>
      <PageHead title={wf.name} subtitle={wf.description || wf.key}
        actions={<>
          <Link className="btn" to="/workflows">All workflows</Link>
          {canDesign(session) && wf.latest_published_version !== null && (
            <button className="btn" type="button" onClick={() => setDuplicating(true)}>Duplicate</button>
          )}
          {canDesign(session) && wf.open_draft_version === null && wf.latest_published_version !== null && (
            <button className="btn" type="button" disabled={busy} onClick={fork}><Icon name="plus" size="sm" /> New draft version</button>
          )}
          {latest && (
            <Link className="btn primary" to={`/designer/${wf.key}/${wf.open_draft_version ?? latest.version}`}>
              Open designer <Icon name="arrow-up-right" size="sm" />
            </Link>
          )}
        </>} />
      {actionError && <ErrorState error={actionError} />}
      <div className="main-table">
        <div className="table-actions">
          <div>
            <h2>Version history</h2>
            <small>Published versions are immutable. Cases stay pinned to the version they started on.</small>
          </div>
          <span className="pill info"><code>{wf.key}</code>{wf.domain_pack && ` · ${wf.domain_pack}`}</span>
        </div>
        <div className="table-scroll">
          <table className="tbl">
            <thead><tr><th>Version</th><th>Status</th><th>Content hash</th><th>Steps</th><th>Created</th><th>Published</th><th><span className="sr-only">Actions</span></th></tr></thead>
            <tbody>
              {wf.versions.map((v) => (
                <tr key={v.id}>
                  <td className="tdmain">v{v.version}{v.change_note && <div className="sub">{v.change_note}</div>}</td>
                  <td><StatusPill status={v.status} /></td>
                  <td><code title={v.definition_hash ?? undefined}>{shortHash(v.definition_hash)}</code></td>
                  <td>{v.node_count}</td>
                  <td>{v.created_by_name}<div className="sub">{formatDate(v.created_at)}</div></td>
                  <td>{v.published_by_name ?? "—"}<div className="sub">{formatDate(v.published_at)}</div></td>
                  <td className="text-right nowrap">
                    <Link className="link" to={`/designer/${wf.key}/${v.version}`}>{v.status === "draft" || v.status === "validated" ? "Edit" : "View"}</Link>
                    {canPublish(session) && v.status === "published" && (
                      <button className="link danger-link" type="button" onClick={() => setConfirmDeprecate(v)}>Deprecate</button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {duplicating && <DuplicateModal wf={wf} onClose={() => setDuplicating(false)} />}
      {confirmDeprecate && (
        <Modal title={`Deprecate v${confirmDeprecate.version}?`} subtitle="New cases can no longer start on this version. Running cases are unaffected."
          onClose={() => setConfirmDeprecate(null)}
          footer={<>
            <button className="btn" type="button" onClick={() => setConfirmDeprecate(null)}>Cancel</button>
            <button className="btn danger" type="button" disabled={busy} onClick={() => deprecate(confirmDeprecate)}>Deprecate</button>
          </>}>
          <p className="hint">Content hash <code>{confirmDeprecate.definition_hash}</code> stays recorded and immutable.</p>
        </Modal>
      )}
    </>
  );
}
