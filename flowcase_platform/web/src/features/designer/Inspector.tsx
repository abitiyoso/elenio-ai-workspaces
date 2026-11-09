import { useState } from "react";
import { NODE_TYPES, type Catalog, type Diagnostic, type NodeType, type WorkflowDefinition, type WorkflowNode } from "../../api/workflows";
import { nodeTypeLabel, removeNode } from "./graph";

interface Props {
  def: WorkflowDefinition;
  selected: string | null;
  editable: boolean;
  catalog: Catalog | null;
  issues: Diagnostic[];
  onChange: (def: WorkflowDefinition) => void;
  onSelect: (id: string | null) => void;
}

function updateNode(def: WorkflowDefinition, id: string, patch: Partial<WorkflowNode>): WorkflowDefinition {
  return { ...def, nodes: def.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)) };
}

/** Registered references the server will accept for each node type (from /workflow-catalog). */
function hintsFor(type: NodeType, catalog: Catalog | null): string | null {
  if (!catalog) return null;
  const list = (items: string[]) => (items.length ? items.join(", ") : "none registered");
  switch (type) {
    case "human_approval":
      return `policy_ref: ${list(catalog.policies.map((p) => p.ref + (p.provisional ? " (provisional)" : "")))}`;
    case "agent_task":
      return `agent_ref: ${list(catalog.agents.map((a) => a.ref))} · output_artifact_type: ${list(catalog.artifact_types)} · context_sources: case-files, kms-approved`;
    case "human_task":
      return `assignee_role: ${list(catalog.assignee_roles)}`;
    case "service_task":
      return `connector_ref: ${list(catalog.connectors.map((c) => c.ref))}`;
    case "condition":
      return `expression: {"operator":"gt","field":"…","value":0} · branches: {"true":"…","false":"…"} (no code, only allowlisted operators)`;
    case "parallel_join":
      return `{"join":"all","split_ref":"<parallel_split id>"}`;
    case "timer":
      return `duration (e.g. 48h, 5d) or wait_until_field`;
    default:
      return null;
  }
}

function ConfigEditor({ node, editable, onCommit }: { node: WorkflowNode; editable: boolean; onCommit: (c: Record<string, unknown> | undefined) => void }) {
  const initial = JSON.stringify(node.config ?? {}, null, 2);
  const [text, setText] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  const commit = () => {
    try {
      const parsed: unknown = JSON.parse(text || "{}");
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) throw new Error("config must be a JSON object");
      setError(null);
      onCommit(Object.keys(parsed).length ? (parsed as Record<string, unknown>) : undefined);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Invalid JSON");
    }
  };
  return (
    <>
      <label className="label" htmlFor={`cfg-${node.id}`}>Configuration (JSON)</label>
      <textarea className="field code" id={`cfg-${node.id}`} value={text} readOnly={!editable} spellCheck={false} rows={8}
        aria-invalid={!!error} aria-describedby={error ? `cfg-err-${node.id}` : undefined}
        onChange={(e) => setText(e.target.value)} onBlur={() => editable && text !== initial && commit()} />
      {error && <p className="field-error" id={`cfg-err-${node.id}`}>{error}</p>}
    </>
  );
}

function NodeEditor({ def, node, editable, catalog, issues, onChange, onSelect }: Props & { node: WorkflowNode }) {
  const others = def.nodes.filter((n) => n.id !== node.id && n.type !== "start");
  const hint = hintsFor(node.type, catalog);
  return (
    <>
      <h2>{node.name}</h2>
      <p>{editable ? "Edits apply to this draft only. The server validates every transition before publication." : "Read-only: published versions are immutable."}</p>
      {issues.length > 0 && (
        <ul className="issue-list" aria-label="Problems with this step">
          {issues.map((d, i) => <li key={i} className={d.severity}><strong>{d.code}</strong> {d.message}</li>)}
        </ul>
      )}
      <label className="label" htmlFor="node-id">Step ID</label>
      <input className="field" id="node-id" value={node.id} readOnly />
      <label className="label" htmlFor="node-name">Name</label>
      <input className="field" id="node-name" value={node.name} readOnly={!editable} maxLength={200}
        onChange={(e) => onChange(updateNode(def, node.id, { name: e.target.value }))} />
      <label className="label" htmlFor="node-type">Type</label>
      <select className="field" id="node-type" value={node.type} disabled={!editable}
        onChange={(e) => onChange(updateNode(def, node.id, { type: e.target.value as NodeType }))}>
        {NODE_TYPES.map((t) => <option key={t} value={t}>{nodeTypeLabel[t]}</option>)}
      </select>
      {node.type !== "end" && (
        <fieldset className="next-list" disabled={!editable}>
          <legend className="label">Next steps</legend>
          {others.map((o) => (
            <label key={o.id} className="check">
              <input type="checkbox" checked={node.next.includes(o.id)}
                onChange={(e) => onChange(updateNode(def, node.id, { next: e.target.checked ? [...node.next, o.id] : node.next.filter((t) => t !== o.id) }))} />
              <span>{o.name} <code>{o.id}</code></span>
            </label>
          ))}
        </fieldset>
      )}
      <ConfigEditor key={`${node.id}:${JSON.stringify(node.config ?? {})}`} node={node} editable={editable}
        onCommit={(config) => onChange(updateNode(def, node.id, { config: config ?? {} }))} />
      {hint && <p className="hint">Registered: {hint}</p>}
      {editable && node.type !== "start" && (
        <>
          <div className="inspector-sep" />
          <button className="btn danger tinybtn" type="button" onClick={() => { onChange(removeNode(def, node.id)); onSelect(null); }}>
            Delete step
          </button>
        </>
      )}
    </>
  );
}

function WorkflowSettings({ def, editable, onChange }: Pick<Props, "def" | "editable" | "onChange">) {
  const [refs, setRefs] = useState((def.policy_refs ?? []).join(", "));
  return (
    <>
      <h2>Workflow settings</h2>
      <p>Select a step in the graph to inspect it.</p>
      <label className="label" htmlFor="wf-name">Name</label>
      <input className="field" id="wf-name" value={def.name} readOnly={!editable} maxLength={200}
        onChange={(e) => onChange({ ...def, name: e.target.value })} />
      <label className="label" htmlFor="wf-desc">Description</label>
      <textarea className="field" id="wf-desc" value={def.description ?? ""} readOnly={!editable} maxLength={2000}
        onChange={(e) => onChange({ ...def, description: e.target.value })} />
      <label className="label" htmlFor="wf-policies">Policy references</label>
      <input className="field" id="wf-policies" value={refs} readOnly={!editable}
        onChange={(e) => setRefs(e.target.value)}
        onBlur={() => editable && onChange({ ...def, policy_refs: refs.split(",").map((s) => s.trim()).filter(Boolean) })} />
      <p className="hint">Every approval's policy_ref must be listed here and registered by a domain pack.</p>
      <div className="inspector-sep" />
      <div className="keyval"><span>Schema</span><strong>{def.schema_version}</strong></div>
      <div className="keyval"><span>Steps</span><strong>{def.nodes.length}</strong></div>
      <div className="keyval"><span>Approval gates</span><strong>{def.nodes.filter((n) => n.type === "human_approval").length}</strong></div>
      <div className="keyval"><span>Agent tasks</span><strong>{def.nodes.filter((n) => n.type === "agent_task").length}</strong></div>
    </>
  );
}

export function Inspector(props: Props) {
  const node = props.def.nodes.find((n) => n.id === props.selected);
  return (
    <div className="panel inspector" aria-label="Step inspector">
      {node ? <NodeEditor key={node.id} {...props} node={node} /> : <WorkflowSettings key={props.def.key} def={props.def} editable={props.editable} onChange={props.onChange} />}
    </div>
  );
}
