import type { IntegrationState, IntegrationStatus, Integrations as IntegrationsData } from "../api/types";
import type { Catalog } from "../api/workflows";
import { useResource } from "../api/useResource";
import { Icon, type IconName } from "../components/Icon";
import { ErrorState, Loading, PageHead } from "../components/States";

const pillClass: Record<IntegrationState, string> = {
  ok: "pill healthy",
  warning: "pill draft",
  stub: "pill draft",
  disabled: "pill",
  unavailable: "pill failed",
};

const pillLabel: Record<IntegrationState, string> = {
  ok: "Connected",
  warning: "Dev only",
  stub: "Dev stub",
  disabled: "Not configured",
  unavailable: "Unavailable",
};

// Boundary facts are architecture decisions (docs/ARCHITECTURE.md), not runtime data.
const cards: { id: keyof IntegrationsData; title: string; icon: IconName; blurb: string; facts: [string, string][] }[] = [
  {
    id: "knowledge", title: "Knowledge Management System", icon: "database",
    blurb: "Governed retrieval and evidence references. KMS owns Elasticsearch, Docling, embeddings and source publication.",
    facts: [["Interface", "KnowledgeProvider"], ["Contract", "Pending KMS retrieval API"], ["Access", "Tenant + case scoped"]],
  },
  {
    id: "agent_runtime", title: "Shared AI Agent Runtime", icon: "cpu",
    blurb: "Runs named agent tasks and returns outputs; human review remains in the workflow platform.",
    facts: [["Interface", "AgentExecutor"], ["Contract", "Start / status / callback (pending)"], ["Governance", "No approval authority"]],
  },
  {
    id: "host_auth", title: "Workspace SSO / Mini-app Wrapper", icon: "lock",
    blurb: "Users authenticate with the parent application. Flowcase verifies identity and enforces authorization; no second login page.",
    facts: [["Interface", "AuthAdapter"], ["Approach", "Short-lived audience-bound token"], ["Roles", "Flowcase directory"]],
  },
  {
    id: "case_runtime", title: "Case Runtime / PostgreSQL", icon: "server",
    blurb: "Owns workflow definitions, execution tokens, tasks, approvals, artifacts, audit and the durable outbox/worker.",
    facts: [["System of record", "PostgreSQL"], ["Execution", "Go worker"], ["Search", "KMS boundary only"]],
  },
];

function Card({ title, icon, blurb, facts, status }: (typeof cards)[number] & { status: IntegrationStatus }) {
  return (
    <section className="integration-card" aria-label={title}>
      <div className="integration-head">
        <div className="integration-icon"><Icon name={icon} /></div>
        <span className={pillClass[status.status]}>{pillLabel[status.status]}</span>
      </div>
      <h3>{title}</h3>
      <p>{blurb}</p>
      <div className="keyval"><span>Adapter mode</span><strong>{status.mode}</strong></div>
      {facts.map(([k, v]) => (
        <div className="keyval" key={k}><span>{k}</span><strong>{v}</strong></div>
      ))}
      <p className="integration-msg">{status.message}</p>
    </section>
  );
}

function PackBindings() {
  const [catalog] = useResource<Catalog>("/api/v1/workflow-catalog");
  if (catalog.state !== "ready") return null;
  const c = catalog.data;
  return (
    <section className="panel panel-pad notice-gap" aria-label="Domain pack bindings">
      <div className="panelhead"><div><h2>Domain pack bindings</h2><p>From PACKS_CONFIG. Policies stay provisional until a ratification is cited.</p></div></div>
      <div className="bindings">
        <div>
          <h3 className="subhead">Agents → runtime</h3>
          {c.agents.map((a) => (
            <div className="keyval" key={a.ref}><span>{a.name}</span><strong><code>{a.runtime_agent_id}</code> · {a.skill_version}</strong></div>
          ))}
        </div>
        <div>
          <h3 className="subhead">Approval policies</h3>
          {c.policies.map((p) => (
            <div className="keyval" key={p.ref}>
              <span>{p.name}</span>
              <strong>
                {p.ratification ? <span className="pill approved">ratified · {p.ratification.reference}</span> : <span className="pill draft">provisional</span>}
                <span className="tiny muted"> SoD: {p.sod.join(", ") || "none"}</span>
              </strong>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

export function IntegrationsPage() {
  const [resource, reload] = useResource<IntegrationsData>("/api/v1/integrations");
  return (
    <>
      <PageHead title="Integrations" subtitle="Clean boundaries between the workflow engine, knowledge service and AI execution."
        actions={<button className="btn" type="button" onClick={reload}><Icon name="refresh" size="sm" /> Refresh status</button>} />
      {resource.state === "loading" && <Loading label="Checking adapters…" />}
      {resource.state === "error" && <ErrorState error={resource.error} onRetry={reload} />}
      {resource.state === "ready" && (
        <div className="integration-grid">
          {cards.map((c) => <Card key={c.id} {...c} status={resource.data[c.id]} />)}
        </div>
      )}
      <PackBindings />
    </>
  );
}
