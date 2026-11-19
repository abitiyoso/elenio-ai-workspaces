import { useParams } from "react-router-dom";
import { Notice } from "../../../../packages/ui/primitives";
import { basePath } from "../api/client";
import { AppLauncher } from "./ConnectedCatalog";

const capabilities = {
  agents: {
    title: "Agent Studio", view: "agents",
    description: "Create, clone and version agents; configure their existing model, skill, tool and knowledge bindings.",
  },
  skills: {
    title: "Skills & Tools", view: "skills",
    description: "Author versioned skills and inspect registered tools using the existing capability workbench.",
  },
  models: {
    title: "Models & Providers", view: "models",
    description: "Manage provider metadata, backend credential status, capability probes and governed model approvals.",
  },
  evaluations: {
    title: "Testing & Evaluation", view: "evaluations",
    description: "Run the existing pinned evaluation suites and inspect their schema, evidence and critical-failure gates.",
  },
  releases: {
    title: "Agent releases", view: "releases",
    description: "Use existing independent domain review and AI Ops release approval. Opening this workspace grants no approval rights.",
  },
} as const;

export function CapabilityWorkspace() {
  const { capability } = useParams();
  const key = capability as keyof typeof capabilities;
  const config = Object.hasOwn(capabilities, key) ? capabilities[key] : undefined;
  if (!config) return <div className="solutions-page"><h1>Capability not found</h1></div>;
  return (
    <div className="solutions-page">
      <header className="page-heading"><div>
        <p className="eyebrow">CAPABILITY LIBRARY</p>
        <h1>{config.title}</h1><p>{config.description}</p>
      </div></header>
      <Notice>This opens the existing governed service. Workspace entitlement and the service's own permissions both apply.</Notice>
      {!basePath && import.meta.env.DEV && (
        <p><a className="e-button primary" href="http://127.0.0.1:8200/apps/flowcase/elenio.html">Open local workspace sign-in</a></p>
      )}
      <AppLauncher appKey="agent-studio" view={config.view} />
    </div>
  );
}
