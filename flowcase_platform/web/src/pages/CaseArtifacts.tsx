import { useState } from "react";
import { ApiError } from "../api/client";
import { reviseArtifact, type Artifact, type CaseDetail } from "../api/cases";
import { ArtifactView } from "../components/ArtifactView";
import { formatDate } from "../components/format";
import { Modal } from "../components/Modal";
import { useToast } from "../components/Toast";

const versionPill: Record<string, string> = {
  ready_for_review: "pill review", approved: "pill approved", stale: "pill failed", superseded: "pill", draft: "pill draft",
};

function ReviseModal({ c, artifact, onClose, onDone }: { c: CaseDetail; artifact: Artifact; onClose: () => void; onDone: () => void }) {
  const current = artifact.versions[0];
  const [text, setText] = useState(JSON.stringify(current?.content ?? {}, null, 2));
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    let content: Record<string, unknown>;
    try {
      content = JSON.parse(text) as Record<string, unknown>;
    } catch {
      setError("Content must be valid JSON.");
      return;
    }
    setBusy(true);
    try {
      await reviseArtifact(c.id, artifact.id, artifact.version_num, content, note.trim());
      toast(`${artifact.artifact_type} v${artifact.current_version + 1} recorded; dependent reviews re-opened`);
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Unexpected error.");
      setBusy(false);
    }
  };
  return (
    <Modal title={`Revise ${artifact.artifact_type}`} subtitle="Creates a new immutable version. Downstream artifacts go stale and affected approvals re-open." onClose={onClose}
      footer={<>
        <button className="btn" type="button" onClick={onClose}>Cancel</button>
        <button className="btn primary" type="submit" form="revise-form" disabled={busy || note.trim().length < 5}>Record v{artifact.current_version + 1}</button>
      </>}>
      <form id="revise-form" onSubmit={submit}>
        <label className="label" htmlFor="rv-note">What changed? *</label>
        <input className="field" id="rv-note" value={note} maxLength={2000} onChange={(e) => setNote(e.target.value)} placeholder="e.g. REQ-014 changed by customer" />
        <label className="label" htmlFor="rv-content">Content (JSON)</label>
        <textarea className="field code" id="rv-content" value={text} spellCheck={false} onChange={(e) => setText(e.target.value)} />
        {error && <div className="validation-box error" role="alert">{error}</div>}
      </form>
    </Modal>
  );
}

export function CaseArtifacts({ c, canRevise, onChanged }: { c: CaseDetail; canRevise: boolean; onChanged: () => void }) {
  const [revising, setRevising] = useState<Artifact | null>(null);
  if (c.artifacts.length === 0 && c.agent_runs.length === 0) return null;
  return (
    <div className="panel panel-pad">
      <div className="panelhead"><div><h2>Artifacts</h2><p>Immutable versions, dependencies and the evidence they cite</p></div></div>
      {c.artifacts.map((a) => {
        const v = a.versions[0];
        if (!v) return null;
        return (
          <div className="artifact" key={a.id}>
            <div className="artifact-head">
              <strong>{a.artifact_type} v{v.version}</strong>
              <span className={versionPill[v.status] ?? "pill"}>{v.status.replace(/_/g, " ")}</span>
              {canRevise && <button className="link" type="button" onClick={() => setRevising(a)}>Revise</button>}
            </div>
            <p className="sub">
              {v.produced_by_kind === "agent" ? `agent output · ${a.producer_node_id}` : v.produced_by_kind === "revision" ? `revised by ${v.produced_by_name ?? "owner"}` : `submitted by ${v.produced_by_name ?? "a person"}`}
              {" · "}{formatDate(v.created_at)} · <code>{v.checksum.slice(0, 10)}</code>
            </p>
            {v.stale_reason && v.status === "stale" && <p className="field-error">Stale: {v.stale_reason}</p>}
            {v.change_note && <p className="sub">“{v.change_note}”</p>}
            {v.depends_on.length > 0 && <p className="sub">Depends on {v.depends_on.map((d) => `${d.type} v${d.version}`).join(", ")}</p>}
            <details className="artifact-content">
              <summary>View content</summary>
              <ArtifactView content={v.content} />
            </details>
            {v.evidence.length > 0 && (
              <p className="sub">Evidence {v.evidence.map((e) => <span key={e.source_id} className={e.status === "valid" ? "pill" : "pill failed"}>{e.source_id} {e.status}</span>)}</p>
            )}
          </div>
        );
      })}
      {c.agent_runs.length > 0 && (
        <>
          <h2 className="subhead notice-gap">Agent runs</h2>
          {c.agent_runs.map((r, i) => (
            <p className="sub" key={i}>
              <strong>{r.agent_ref}</strong> ({r.provider}{r.provider === "stub" && ", fictional output"}) · {r.status} · attempt {r.attempt}
              {r.evidence && r.evidence.some((e) => e.status !== "valid") && ` · ${r.evidence.filter((e) => e.status !== "valid").length} source(s) withheld`}
            </p>
          ))}
        </>
      )}
      {revising && <ReviseModal c={c} artifact={revising} onClose={() => setRevising(null)} onDone={() => { setRevising(null); onChanged(); }} />}
    </div>
  );
}
