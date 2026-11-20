import type { SolutionReadiness } from "./solutionApi";

export function BindingEvidence({ workflow }: { workflow: SolutionReadiness["workflows"][number] }) {
  const snapshot = workflow.binding_snapshot;
  if (!snapshot) return <p>{workflow.binding_reason || "No deployment binding snapshot was retained."}</p>;
  return (
    <details className="binding-evidence">
      <summary>Observed bindings · {workflow.key}@{workflow.version} · {snapshot.bindings.length} references</summary>
      <p>Observed {new Date(snapshot.observed_at).toLocaleString()}. Registration does not verify an approved agent/model release or authorize deployment.</p>
      <p className="publisher-hash">Binding checksum: <code>{snapshot.content_hash}</code></p>
      <ul>{snapshot.bindings.map(b => (
        <li key={`${b.kind}:${b.node_id}:${b.ref}`}>
          {b.kind} · {b.ref} · step {b.node_id}
          {b.runtime_agent_id && ` · runtime ${b.runtime_agent_id}`}
          {b.skill_version && ` · version ${b.skill_version}`}
          {b.kind === "agent" && ` · input ${b.input_contract || "provider default (unresolved)"}`}
          {b.operation && ` · operation ${b.operation}`}
          {b.provisional !== undefined && ` · ${b.provisional ? "provisional policy" : "catalog policy; authority requires verification"}`}
          {b.sod?.length ? ` · duties: ${b.sod.join(", ")}` : ""}
        </li>
      ))}</ul>
    </details>
  );
}
