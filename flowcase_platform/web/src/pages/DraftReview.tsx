import { useState } from "react";
import { ApiError } from "../api/client";
import { reviseDraft, submitDraft, type CaseDetail, type DraftReview } from "../api/cases";
import { ArtifactView } from "../components/ArtifactView";
import { formatDate } from "../components/format";
import { useToast } from "../components/Toast";
import { draftReviewActive, revisingSteps } from "./draftState";

// Author draft review: agent steps configured with author_review stop after their result and wait for
// the case owner, who submits the draft to the approver or sends it back to the agent with feedback.

const maxFeedback = 1000;

function DraftCard({ draft, onChanged }: { draft: DraftReview; onChanged: () => void }) {
  const [feedback, setFeedback] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const toast = useToast();
  const name = draft.node_name || draft.node_id;
  const run = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      toast(done);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Unexpected error.");
      if (err instanceof ApiError && err.status === 412) onChanged();
    } finally {
      setBusy(false);
    }
  };
  const trimmed = feedback.trim();
  return (
    <article className="draft-card" aria-label={`Draft ${name} v${draft.artifact_version}`}>
      <div className="draft-card-head">
        <strong>{name}</strong>
        <span className="pill review">v{draft.artifact_version}</span>
        {draft.revisions > 0 && <span className="pill draft">revised {draft.revisions}×</span>}
        <span className="tiny">{draft.artifact_type} · {formatDate(draft.created_at)}</span>
      </div>
      {draft.content ? <ArtifactView content={draft.content} /> : <p className="tiny">The draft content is not available; open the artifacts below.</p>}
      {draft.can_submit ? (
        <div className="draft-actions">
          <button className="btn primary" type="button" disabled={busy}
            onClick={() => run(() => submitDraft(draft.id, draft.version_num), `${name} v${draft.artifact_version} submitted to the approver`)}>
            Submit to approver
          </button>
          {draft.can_revise ? (
            <form className="draft-revision" onSubmit={(e) => {
              e.preventDefault();
              if (trimmed.length >= 5) void run(() => reviseDraft(draft.id, draft.version_num, trimmed).then(() => setFeedback("")), `${name} sent back to the agent`);
            }}>
              <label className="label" htmlFor={`draft-feedback-${draft.id}`}>Request revision</label>
              <textarea className="field" id={`draft-feedback-${draft.id}`} value={feedback} maxLength={maxFeedback}
                aria-describedby={`draft-feedback-help-${draft.id}`} placeholder="What should the agent change?" onChange={(e) => setFeedback(e.target.value)} />
              <div className="draft-revision-foot">
                <span className="hint" id={`draft-feedback-help-${draft.id}`}>The agent will regenerate this draft using your feedback</span>
                <span className="tiny" aria-live="polite">{feedback.length}/{maxFeedback}</span>
              </div>
              <button className="btn" type="submit" disabled={busy || trimmed.length < 5}>Request revision</button>
            </form>
          ) : (
            <p className="hint">This step reached the revision limit; submit the draft or revise the artifact by hand.</p>
          )}
        </div>
      ) : (
        <div className="notice notice-gap">Waiting for the case owner's review.</div>
      )}
      {error && <div className="validation-box error" role="alert">{error}</div>}
    </article>
  );
}

function historyLine(d: DraftReview): string {
  const who = d.decided_by_name ?? "the case owner";
  switch (d.status) {
    case "submitted": return `submitted by ${who}${d.feedback ? `: “${d.feedback}”` : ""}`;
    case "revision_requested": return `revision requested by ${who}: “${d.feedback ?? ""}”`;
    case "cancelled": return "cancelled (the case or step closed)";
    default: return "waiting for review";
  }
}

function DraftHistory({ decided }: { decided: DraftReview[] }) {
  return (
    <details className="draft-history">
      <summary>Review history ({decided.length})</summary>
      <ol>
        {decided.map((d) => (
          <li key={d.id}>
            <strong>{d.node_name || d.node_id} v{d.artifact_version}</strong> {historyLine(d)}
            {d.decided_at && <span className="tiny"> · {formatDate(d.decided_at)}</span>}
          </li>
        ))}
      </ol>
    </details>
  );
}

/** Past draft reviews once nothing waits on the owner any more. */
export function DraftHistoryPanel({ c }: { c: CaseDetail }) {
  const decided = (c.drafts ?? []).filter((d) => d.status !== "pending");
  if (decided.length === 0 || draftReviewActive(c)) return null;
  return (
    <div className="panel panel-pad">
      <div className="panelhead"><div><h2>Draft reviews</h2><p>How the case owner handled agent drafts</p></div></div>
      <DraftHistory decided={decided} />
    </div>
  );
}

/** The case owner's draft reviews, shown at the top of the case while any draft waits or is being revised. */
export function DraftReviewPanel({ c, onChanged }: { c: CaseDetail; onChanged: () => void }) {
  const drafts = c.drafts ?? [];
  const pending = drafts.filter((d) => d.status === "pending");
  const revising = revisingSteps(c);
  const decided = drafts.filter((d) => d.status !== "pending");
  if (!draftReviewActive(c)) return null;
  const canAct = pending.some((d) => d.can_submit);
  return (
    <section className="panel panel-pad draft-review" id="your-review" aria-label={canAct ? "Your review" : "Draft review"}>
      <div className="panelhead"><div>
        <h2>{canAct ? "Your review" : "Draft review"}</h2>
        <p>Agent drafts wait for the case owner before they reach the approver.</p>
      </div></div>
      {revising.map((r) => (
        <div className="draft-revising" key={r.nodeId} role="status">
          <span className="pill running">revising</span> Agent is revising {r.name} (iteration {r.iteration})…
        </div>
      ))}
      {pending.map((d) => <DraftCard key={d.id} draft={d} onChanged={onChanged} />)}
      {decided.length > 0 && <DraftHistory decided={decided} />}
    </section>
  );
}
