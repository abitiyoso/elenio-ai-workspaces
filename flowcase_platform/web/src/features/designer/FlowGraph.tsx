import { useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Diagnostic, WorkflowDefinition } from "../../api/workflows";
import { Icon, type IconName } from "../../components/Icon";
import { layoutRows, nodeFamily, nodeTypeLabel } from "./graph";

interface Edge {
  d: string;
  back: boolean;
  key: string;
}

const familyIcon: Record<string, IconName> = { human: "user", condition: "workflow", agent: "cpu" };

interface Props {
  def: WorkflowDefinition;
  selected: string | null;
  issues: Map<string, Diagnostic[]>;
  onSelect: (id: string) => void;
  /** Runtime state per node (case view): done | active | failed. */
  states?: Map<string, string>;
}

/** Read-only, layered rendering of the process graph. Edges are measured from the laid-out cards. */
export function FlowGraph({ def, selected, issues, onSelect, states }: Props) {
  const rows = useMemo(() => layoutRows(def), [def]);
  const container = useRef<HTMLDivElement>(null);
  const cards = useRef(new Map<string, HTMLButtonElement>());
  const [edges, setEdges] = useState<Edge[]>([]);

  useLayoutEffect(() => {
    const el = container.current;
    if (!el) return;
    const rowOf = new Map<string, number>();
    rows.forEach((r, i) => r.forEach((id) => rowOf.set(id, i)));
    const measure = () => {
      const base = el.getBoundingClientRect();
      const next: Edge[] = [];
      for (const n of def.nodes) {
        const from = cards.current.get(n.id);
        if (!from) continue;
        const a = from.getBoundingClientRect();
        for (const t of n.next) {
          const to = cards.current.get(t);
          if (!to) continue;
          const b = to.getBoundingClientRect();
          const x1 = a.left + a.width / 2 - base.left;
          const y1 = a.bottom - base.top;
          const x2 = b.left + b.width / 2 - base.left;
          const y2 = b.top - base.top;
          const back = (rowOf.get(t) ?? 0) <= (rowOf.get(n.id) ?? 0);
          const bend = Math.max(24, Math.abs(y2 - y1) / 2);
          next.push({ key: `${n.id}->${t}`, back, d: `M${x1},${y1} C${x1},${y1 + bend} ${x2},${y2 - bend} ${x2},${y2}` });
        }
      }
      setEdges(next);
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [def, rows]);

  const byId = new Map(def.nodes.map((n) => [n.id, n]));
  return (
    <div className="flow-graph" ref={container} role="list" aria-label="Process graph, top to bottom">
      <svg className="flow-edges" aria-hidden="true">
        <defs>
          <marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0,0 L10,5 L0,10 z" className="flow-arrow" />
          </marker>
        </defs>
        {edges.map((e) => <path key={e.key} d={e.d} className={e.back ? "flow-edge back" : "flow-edge"} markerEnd="url(#arrow)" />)}
      </svg>
      {rows.map((row, i) => (
        <div className="flow-row" key={i}>
          {row.map((id) => {
            const n = byId.get(id);
            if (!n) return null;
            const fam = nodeFamily(n.type);
            const nodeIssues = issues.get(id) ?? [];
            const errors = nodeIssues.filter((d) => d.severity === "error").length;
            const state = states?.get(id);
            const cls = ["flow-node", id === selected ? "selected" : "", errors ? "has-error" : nodeIssues.length ? "has-warning" : "",
              state ? `state-${state}` : ""].join(" ").trim();
            return (
              <button key={id} type="button" role="listitem" className={cls} aria-pressed={id === selected}
                ref={(el) => { if (el) cards.current.set(id, el); else cards.current.delete(id); }}
                onClick={() => onSelect(id)}
                aria-label={`${nodeTypeLabel[n.type]} ${n.name}${errors ? `, ${errors} error${errors > 1 ? "s" : ""}` : ""}${state ? `, ${state}` : ""}`}>
                <div className="node-top">
                  <span className={`node-type ${fam === "agent" ? "" : fam}`}>{nodeTypeLabel[n.type]}</span>
                  <span className={`node-badge ${fam === "agent" ? "" : fam}`}><Icon name={familyIcon[fam] ?? "file"} size="sm" /></span>
                </div>
                <strong>{n.name}</strong>
                <span className="node-id">{n.id}</span>
                {state && <span className={`node-state ${state}`}>{state === "done" ? "Done" : state === "active" ? "In progress" : "Failed"}</span>}
                {nodeIssues.length > 0 && (
                  <span className={errors ? "node-issue error" : "node-issue"}>{errors ? `${errors} error${errors > 1 ? "s" : ""}` : `${nodeIssues.length} note${nodeIssues.length > 1 ? "s" : ""}`}</span>
                )}
              </button>
            );
          })}
        </div>
      ))}
    </div>
  );
}
