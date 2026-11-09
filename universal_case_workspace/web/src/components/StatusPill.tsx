import { humanize } from "./format";

// Status → pill tone. Unknown statuses stay neutral grey rather than guessing a meaning.
const tones: Record<string, string> = {
  active: "blue", running: "blue", in_progress: "blue", ready: "blue", queued: "blue", waiting_external: "blue", open: "blue", assigned: "blue",
  under_review: "amber", waiting_review: "amber", waiting_approval: "amber", pending: "amber", ready_for_review: "amber", draft: "amber",
  revision_requested: "amber", paused: "amber", partial: "amber", stale: "amber", returned: "amber",
  completed: "green", approved: "green", submitted: "green", succeeded: "green", published: "green", ok: "green", covered: "green", done: "green",
  failed: "red", rejected: "red", cancelled: "grey", invalidated: "red", unavailable: "red", error: "red", not_covered: "red",
  superseded: "grey", abandoned: "grey", created: "green",
};

function toneFor(status?: string | null): string {
  return (status && tones[status]) || "grey";
}

export function StatusPill({ status, label }: { status?: string | null | undefined; label?: string }) {
  return <span className={`pill ${toneFor(status)}`}>{label ?? humanize(status)}</span>;
}
