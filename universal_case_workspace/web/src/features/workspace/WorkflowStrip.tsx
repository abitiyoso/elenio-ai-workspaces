import type { WorkflowNode } from "../../api/types";
import { humanize } from "../../components/format";
import { Icon } from "../../components/Icon";

const stateClass: Record<string, string> = {
  completed: "done", active: "current", waiting_review: "waiting", waiting_approval: "waiting", failed: "failed", pending: "",
};

/** Workflow steps exactly as Flowcase names them (approval labels are Flowcase data, never hardcoded). */
export function WorkflowStrip({ nodes }: { nodes: WorkflowNode[] }) {
  if (nodes.length === 0) return null;
  return (
    <ol className="stages" aria-label="Workflow steps">
      {nodes.map((n, i) => {
        const approval = n.type === "human_approval";
        const cls = stateClass[n.status ?? "pending"] ?? "";
        return (
          <li key={n.id} className={`stage ${cls}`} aria-current={n.status === "active" ? "step" : undefined}>
            <span className="stage-num" aria-hidden="true">{n.status === "completed" ? <Icon name="check" size="sm" /> : approval ? <Icon name="shield-check" size="sm" /> : i + 1}</span>
            <span>
              <strong>{n.name || n.id}</strong>
              <small>{approval ? "Approval · " : ""}{humanize(n.status ?? "pending")}</small>
            </span>
          </li>
        );
      })}
    </ol>
  );
}
