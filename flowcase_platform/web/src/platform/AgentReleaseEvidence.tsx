import { Badge } from "../../../../packages/ui/primitives";
import type { AgentReleaseObservation } from "./solutionApi";

export function AgentReleaseEvidence({ observations }: { observations: AgentReleaseObservation[] | undefined }) {
  if (!observations?.length) return null;
  return (
    <details className="binding-evidence">
      <summary>Agent release evidence · {observations.length} reference{observations.length === 1 ? "" : "s"}</summary>
      <p>Source governance observations. Solution approval, input contracts, target permissions and installation still require verification.</p>
      {observations.map(o => (
        <section key={`${o.runtime_agent_id}:${o.expected_version}`}>
          <h4>{o.runtime_agent_id} · requested {o.expected_version || "active version"}</h4>
          <p>{o.reason}</p>
          {o.evidence && <>
            <Badge tone={o.evidence.state === "verified" ? "green" : "amber"}>Source checks {o.evidence.state}</Badge>
            <p>{o.evidence.environment} · agent {o.evidence.agent_version.version} · observed {new Date(o.evidence.observed_at).toLocaleString()}</p>
            <p>Independent approval: {o.evidence.independent_approval ? "verified" : "unverified"}{o.evidence.self_approval_exempt ? " · development exemption used" : ""}</p>
            <p>Evaluation: {o.evidence.evaluation_gate || "unavailable"} · current primary config {o.evidence.evaluation_primary_config_matches ? "matches" : "unverified"}</p>
            <p>Primary model config: {o.evidence.primary_model ? `v${o.evidence.primary_model.config_version} · ${o.evidence.primary_model.approved ? "currently approved" : "not currently approved"}` : "unavailable"}</p>
            <p>Retained dependency pins: {o.evidence.skills.length} skills · {o.evidence.tools.length} tools</p>
            <p className="publisher-hash">Release ID: <code>{o.evidence.release_id}</code> · request <code>{o.evidence.request_id || "unavailable"}</code> · evaluation <code>{o.evidence.evaluation_id || "unavailable"}</code></p>
            <p className="publisher-hash">Source checksum: <code>{o.evidence.content_hash}</code></p>
            {o.evidence.blockers.length > 0 && <ul>{o.evidence.blockers.map(code => <li key={code}>{code.replaceAll("_", " ")}</li>)}</ul>}
          </>}
        </section>
      ))}
    </details>
  );
}
