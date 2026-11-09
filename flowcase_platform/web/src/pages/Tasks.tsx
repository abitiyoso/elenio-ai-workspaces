import { useState } from "react";
import { Link } from "react-router-dom";
import { ApiError } from "../api/client";
import { completeTask, requestRevision, type DraftReview, type Task } from "../api/cases";
import { useResource } from "../api/useResource";
import { DataForm } from "../components/DataForm";
import { toData, type FormValues } from "../components/formData";
import { formatDate } from "../components/format";
import { Modal } from "../components/Modal";
import { EmptyState, ErrorState, Loading, PageHead } from "../components/States";
import { useToast } from "../components/Toast";

const tabs = [
  { status: "open", label: "Open" },
  { status: "completed", label: "Completed by me" },
  { status: "all", label: "All" },
];

function CompleteModal({ task, onClose, onDone }: { task: Task; onClose: () => void; onDone: () => void }) {
  const [values, setValues] = useState<FormValues>({});
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const toast = useToast();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await completeTask(task.id, task.version_num, task.form_schema ? toData(task.form_schema, values) : {}, comment.trim());
      toast(`Submitted “${task.title}”`);
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, "unknown", "Unexpected error."));
      setBusy(false);
    }
  };
  return (
    <Modal title={task.title} subtitle={`${task.case.reference} · ${task.case.title}`} onClose={onClose}
      footer={<>
        <button className="btn" type="button" onClick={onClose}>Cancel</button>
        <button className="btn primary" type="submit" form="complete-task" disabled={busy}>{busy ? "Submitting…" : "Submit"}</button>
      </>}>
      <form id="complete-task" onSubmit={submit}>
        {task.revision_note && <div className="validation-box"><strong>Rework requested:</strong> {task.revision_note}</div>}
        {task.instructions && <p className="hint">{task.instructions}</p>}
        {task.form_schema && <DataForm schema={task.form_schema} values={values} onChange={setValues} idPrefix="task" />}
        <label className="label" htmlFor="task-comment">Comment</label>
        <textarea className="field" id="task-comment" value={comment} maxLength={4000} onChange={(e) => setComment(e.target.value)} placeholder="What was done, evidence, open points…" />
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

function RevisionModal({ task, onClose, onDone }: { task: Task; onClose: () => void; onDone: () => void }) {
  const [rationale, setRationale] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const toast = useToast();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await requestRevision(task.id, task.version_num, rationale.trim());
      toast("Sent back for rework");
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Unexpected error.");
      setBusy(false);
    }
  };
  return (
    <Modal title="Request revision" subtitle="The next step is withdrawn and this task re-opens as a new iteration." onClose={onClose}
      footer={<>
        <button className="btn" type="button" onClick={onClose}>Cancel</button>
        <button className="btn primary" type="submit" form="revision-form" disabled={busy || rationale.trim().length < 5}>Send back</button>
      </>}>
      <form id="revision-form" onSubmit={submit}>
        <label className="label" htmlFor="rev-rationale">What needs to change? *</label>
        <textarea className="field" id="rev-rationale" value={rationale} maxLength={4000} onChange={(e) => setRationale(e.target.value)} />
        {error && <div className="validation-box error" role="alert">{error}</div>}
      </form>
    </Modal>
  );
}

/** Agent drafts waiting for the caller's review as case owner (author_review steps). */
function DraftsToReview() {
  const [resource, reload] = useResource<{ items: DraftReview[] }>("/api/v1/drafts?status=pending");
  if (resource.state === "loading") return null;
  if (resource.state === "error") return <ErrorState error={resource.error} onRetry={reload} />;
  if (resource.data.items.length === 0) return null;
  return (
    <section className="panel drafts-to-review" aria-label="Drafts to review">
      <div className="panel-pad panelhead"><div><h2>Drafts to review</h2><p>Agent drafts in your cases wait for you before they reach the approver.</p></div></div>
      <div className="task-list">
        {resource.data.items.map((d) => (
          <div className="task-row" key={d.id}>
            <div>
              <strong>{d.node_name || d.node_id} · v{d.artifact_version}{d.revisions > 0 && <span className="pill draft rework">revised {d.revisions}×</span>}</strong>
              <p><Link to={`/cases/${d.case.id}`}>{d.case.reference}</Link> · {d.case.title} · {d.artifact_type} · {formatDate(d.created_at)}</p>
            </div>
            <div className="task-actions">
              <span className="pill review">waiting for you</span>
              <Link className="btn tinybtn primary" to={`/cases/${d.case.id}`}>Review</Link>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

export function TasksPage() {
  const [status, setStatus] = useState("open");
  const [resource, reload] = useResource<{ items: Task[] }>(`/api/v1/tasks?status=${status}`);
  const [completing, setCompleting] = useState<Task | null>(null);
  const [revising, setRevising] = useState<Task | null>(null);

  return (
    <>
      <PageHead title="My Tasks" subtitle="Human work routed by the workflow engine to your domain roles." />
      <DraftsToReview />
      <div className="filterbar">
        <div className="tabs" role="tablist" aria-label="Filter tasks">
          {tabs.map((t) => (
            <button key={t.status} type="button" role="tab" aria-selected={status === t.status} className={status === t.status ? "tab active" : "tab"}
              onClick={() => setStatus(t.status)}>{t.label}</button>
          ))}
        </div>
      </div>
      {resource.state === "loading" && <Loading label="Loading tasks…" />}
      {resource.state === "error" && <ErrorState error={resource.error} onRetry={reload} />}
      {resource.state === "ready" && (
        <div className="panel">
          <div className="task-list">
            {resource.data.items.length === 0 && <EmptyState icon="check-square" title="Nothing here">Tasks for your domain roles appear when a case reaches them.</EmptyState>}
            {resource.data.items.map((t) => (
              <div className="task-row" key={t.id}>
                <div>
                  <strong>{t.title}{t.iteration > 1 && <span className="pill draft rework">Rework #{t.iteration}</span>}</strong>
                  <p><Link to={`/cases/${t.case.id}`}>{t.case.reference}</Link> · {t.case.title} · role {t.assignee_role}
                    {t.due_at && ` · due ${formatDate(t.due_at)}`}
                    {t.completed_by_name && ` · submitted by ${t.completed_by_name}`}</p>
                  {t.revision_note && t.status === "assigned" && <p className="rework-note">“{t.revision_note}”</p>}
                </div>
                <div className="task-actions">
                  <span className={t.status === "assigned" ? "pill active" : t.status === "submitted" ? "pill completed" : "pill"}>{t.status.replace("_", " ")}</span>
                  {t.can_complete && <button className="btn tinybtn primary" type="button" onClick={() => setCompleting(t)}>Complete</button>}
                  {t.can_request_revision && <button className="btn tinybtn" type="button" onClick={() => setRevising(t)}>Request revision</button>}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
      {completing && <CompleteModal task={completing} onClose={() => setCompleting(null)} onDone={() => { setCompleting(null); reload(); }} />}
      {revising && <RevisionModal task={revising} onClose={() => setRevising(null)} onDone={() => { setRevising(null); reload(); }} />}
    </>
  );
}
