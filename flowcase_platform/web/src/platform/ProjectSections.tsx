import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { WorkflowDefinition } from "../api/workflows";
import { Badge, Button, Notice } from "../../../../packages/ui/primitives";

export function ProjectSections({
  definition,
  onWorkflow,
  children,
}: {
  definition: WorkflowDefinition;
  onWorkflow: () => void;
  children: ReactNode;
}) {
  const [tab, setTab] = useState("Overview");
  const bindings = definition.nodes.flatMap((n) =>
    Object.entries(n.config ?? {})
      .filter(([key]) =>
        ["agent_ref", "connector_ref", "policy_ref"].includes(key),
      )
      .map(([kind, ref]) => ({
        node: n.name,
        kind:
          kind === "agent_ref"
            ? "Agent"
            : kind === "connector_ref"
              ? "Integration"
              : "Approval policy",
        ref: String(ref),
      })),
  );
  return (
    <>
      <nav className="project-context-tabs" aria-label="Project sections">
        {[
          "Overview",
          "Workflow",
          "Dependencies",
          "Testing & Evaluation",
          "Economics",
          "Versions",
        ].map((t) => (
          <button
            key={t}
            className={tab === t ? "active" : ""}
            aria-current={tab === t ? "page" : undefined}
            onClick={() => {
              if (t === "Workflow") onWorkflow();
              else setTab(t);
            }}
          >
            {t}
          </button>
        ))}
      </nav>
      {tab === "Overview" ? (
        children
      ) : (
        <section className="surface project-context-panel">
          <h2>
            {tab === "Dependencies"
              ? "Solution dependencies"
              : tab === "Versions"
                ? "Workflow versions"
                : tab}
          </h2>
          {tab === "Dependencies" ? (
            <>
              <p>
                Bindings in this draft, grouped by the node that uses them.
                Fixture references require approved service resources before
                execution.
              </p>
              <table className="dependency-table">
                <thead>
                  <tr>
                    <th>Step</th>
                    <th>Capability</th>
                    <th>Version / reference</th>
                  </tr>
                </thead>
                <tbody>
                  {bindings.map((b, i) => (
                    <tr key={i}>
                      <td>{b.node}</td>
                      <td>{b.kind}</td>
                      <td>
                        <code>{b.ref}</code>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <Notice>
                Knowledge and model scope must be resolved against the connected
                registries. No configured dependency is inferred from a
                reference alone.
              </Notice>
            </>
          ) : tab === "Economics" ? (
            <>
              <Badge>Design estimate · unpriced</Badge>
              <p>
                Compare conventional process cost with estimated,
                sandbox-measured and actual production cost. Include branches,
                retries, human review, fixed costs and implementation
                investment.
              </p>
              <Notice>
                No approved rate card, usage events or business baseline are
                connected. ROI and savings cannot be calculated from this draft.
              </Notice>
              <Link className="e-button" to="/economics">
                Open scenario planner
              </Link>
              <p>
                The planner uses explicit assumptions. It does not infer costs
                from this workflow; actual ledger integration remains
                unavailable.
              </p>
            </>
          ) : tab === "Versions" ? (
            <>
              <p>
                The local draft is version {definition.version}. Its process
                definition is editable; published versions in Flowcase remain
                immutable.
              </p>
              <table className="dependency-table">
                <thead>
                  <tr>
                    <th>Version</th>
                    <th>State</th>
                    <th>Source</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>v{definition.version}</td>
                    <td>
                      <Badge tone="amber">Draft</Badge>
                    </td>
                    <td>Local design preview</td>
                  </tr>
                </tbody>
              </table>
              <Notice>
                Server version history and publication controls require an
                authenticated Flowcase session. No history entries are
                fabricated.
              </Notice>
            </>
          ) : (
            <>
              <p>
                Validate topology, dependency readiness and expected outcomes
                before release. Evaluate matched/unmatched branches, parallel
                joins, rejection and revision with version-pinned test data.
              </p>
              <Notice>
                No runtime test has been executed in this preview. Local graph
                checks are available in Workflow Studio; sandbox execution and
                evaluations require connected services.
              </Notice>
              <Button onClick={onWorkflow}>
                Inspect graph and validation issues
              </Button>
            </>
          )}
        </section>
      )}
    </>
  );
}
