import { useState } from "react";
import { asApiError, describeError } from "../../api/client";
import type { CaseContext, DraftReview } from "../../api/types";
import { requestDraftRevision, submitDraft } from "../../api/workspace";
import { ArtifactView } from "../../components/ArtifactView";
import { formatDate } from "../../components/format";
import { useToast } from "../../components/toastContext";
import { draftReviewActive, revisingSteps } from "./model";

// Author draft review (ported from Flowcase): agent steps with author review stop after their result and wait for the
// case owner, who submits the draft to the approver or sends it back to the agent with feedback.

export const maxFeedback = 1000;
const minFeedback = 5;

export interface DraftActions {
  caseId: string;
  /** Replace the case context with the server's refreshed copy. */
  onUpdated: (ctx: CaseContext) => void;
  /** Reload the case (after a conflict). */
  onReload: () => void;
  /** A revision was requested for this step: start watching the agent re-run. */
  onRevisionRequested: (nodeId: string) => void;
  requested: ReadonlySet<string>;
}

function DraftCard({ draft, actions }: { draft: DraftReview; actions: DraftActions }) {
  const [feedback, setFeedback] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ message: string; reload: boolean } | null>(null);
  const toast = useToast();
  const name = draft.node_name || draft.node_id;
  const version = draft.artifact_version ?? draft.version_num;
  const trimmed = feedback.trim();
  const tooShort = trimmed.length < minFeedback;
  const feedbackId = `draft-feedback-${draft.id}`;

  const run = async (fn: () => Promise<CaseContext>, done: string, after?: () => void) => {
    setBusy(true);
    setError(null);
    try {
      const ctx = await fn();
      toast(done);
      after?.();
      actions.onUpdated(ctx);
    } catch (err) {
      const e = asApiError(err);
      const d = describeError(e);
      setError({ message: e.status === 409 || e.status === 412 || e.status === 403 ? d.message : e.message, reload: e.status === 409 || e.status === 412 });
      if (e.status === 412) actions.onReload();
    } finally {
      setBusy(false);
    }
  };

  return (
    <article className="draft-card" aria-label={`Draft ${name} v${version}`}>
      <div className="draft-card-head">
        <strong>{name}</strong>
        <span className="pill blue">v{version}</span>
        {(draft.revisions ?? 0) > 0 && <span className="pill amber">revised {draft.revisions}×</span>}
        <span className="pill amber">AI draft — needs human review</span>
        <span className="tiny muted">{draft.artifact_type} · {formatDate(draft.created_at)}</span>
      </div>
      <div className="draft-content">
        {draft.content ? <ArtifactView content={draft.content} /> : <p className="tiny muted">The draft content is not available; open the artifact views.</p>}
      </div>
      {draft.can_submit ? (
        <div className="draft-actions">
          <button className="btn primary" type="button" disabled={busy}
            onClick={() => void run(() => submitDraft(actions.caseId, draft.id, draft.version_num), `${name} v${version} submitted to the approver`)}>
            Submit to approver
          </button>
          {draft.can_revise ? (
            <form className="draft-revision" noValidate onSubmit={(e) => {
              e.preventDefault();
              setTouched(true);
              if (tooShort) return;
              void run(() => requestDraftRevision(actions.caseId, draft.id, draft.version_num, trimmed), `${name} sent back to the agent`, () => {
                setFeedback("");
                setTouched(false);
                actions.onRevisionRequested(draft.node_id);
              });
            }}>
              <label className="label" htmlFor={feedbackId}>Request revision</label>
              <textarea className="textarea" id={feedbackId} value={feedback} maxLength={maxFeedback}
                aria-describedby={`${feedbackId}-help${touched && tooShort ? ` ${feedbackId}-err` : ""}`} aria-invalid={touched && tooShort}
                placeholder="What should the agent change?" onChange={(e) => setFeedback(e.target.value)} />
              <div className="draft-revision-foot">
                <span className="hint" id={`${feedbackId}-help`}>The agent will regenerate this draft using your feedback</span>
                <span className="tiny muted" aria-live="polite">{feedback.length}/{maxFeedback}</span>
              </div>
              {touched && tooShort && <p className="field-error" id={`${feedbackId}-err`} role="alert">Write at least {minFeedback} characters of feedback for the agent.</p>}
              <button className="btn" type="submit" disabled={busy}>Request revision</button>
            </form>
          ) : (
            <p className="hint">This step reached the revision limit; submit the draft or ask for changes in Flowcase.</p>
          )}
        </div>
      ) : (
        <div className="notice">Waiting for the case owner's review.</div>
      )}
      {error && (
        <div className="notice error" role="alert">
          {error.message}
          {error.reload && <button className="btn sm" type="button" onClick={actions.onReload}>Reload</button>}
        </div>
      )}
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
            <strong>{d.node_name || d.node_id} v{d.artifact_version ?? d.version_num}</strong> {historyLine(d)}
            {d.decided_at && <span className="tiny muted"> · {formatDate(d.decided_at)}</span>}
          </li>
        ))}
      </ol>
    </details>
  );
}

/** The owner's draft reviews, shown while any draft waits or is being revised. */
export function DraftReviewPanel({ ctx, actions, showEmpty }: { ctx: CaseContext; actions: DraftActions; showEmpty?: boolean }) {
  const pending = ctx.drafts.filter((d) => d.status === "pending");
  const revising = revisingSteps(ctx, actions.requested);
  const decided = ctx.drafts.filter((d) => d.status !== "pending");
  const active = draftReviewActive(ctx, actions.requested);
  if (!active && !showEmpty) return null;
  const canAct = pending.some((d) => d.can_submit) && ctx.permissions?.can_review_drafts !== false;
  return (
    <section className="card pad draft-review" id="your-review" aria-labelledby="your-review-title">
      <div className="cardhead">
        <div>
          <h2 id="your-review-title">{canAct ? "Your review" : "Draft review"}</h2>
          <p>Agent drafts wait for the case owner before they reach the approver. Submitting is not an approval.</p>
        </div>
        {pending.length > 0 && <span className="pill amber">{pending.length} waiting</span>}
      </div>
      <div aria-live="polite">
        {revising.map((r) => (
          <div className="draft-revising" key={r.nodeId} role="status">
            <span className="spinner" aria-hidden="true" /> <span className="pill blue">revising</span> Agent is revising {r.name}{r.iteration ? ` (iteration ${r.iteration})` : ""}…
          </div>
        ))}
      </div>
      {!active && <p className="hint">No agent drafts wait for review.</p>}
      {pending.map((d) => <DraftCard key={d.id} draft={d} actions={actions} />)}
      {decided.length > 0 && <DraftHistory decided={decided} />}
    </section>
  );
}

/** Past draft reviews once nothing waits on the owner any more. */
export function DraftHistoryPanel({ ctx, requested }: { ctx: CaseContext; requested: ReadonlySet<string> }) {
  const decided = ctx.drafts.filter((d) => d.status !== "pending");
  if (decided.length === 0 || draftReviewActive(ctx, requested)) return null;
  return (
    <section className="card pad">
      <div className="cardhead"><div><h2>Draft reviews</h2><p>How the case owner handled agent drafts</p></div></div>
      <DraftHistory decided={decided} />
    </section>
  );
}
