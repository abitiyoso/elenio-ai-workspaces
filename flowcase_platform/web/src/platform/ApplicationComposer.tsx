import { useState } from "react";
import { Button, Badge, Notice } from "../../../../packages/ui/primitives";
import type { ApplicationComposition, SolutionDefinition, Solution, SolutionSession } from "./solutionApi";
import { SolutionRuntime } from "./SolutionRuntime";

const kinds = {
  overview: "Overview",
  intake: "Request form",
  tasks: "Task queue",
  report: "Results & reports",
};
export function ApplicationComposer({
  definition,
  onChange,
}: {
  definition: SolutionDefinition;
  onChange: (application: ApplicationComposition | undefined) => void;
}) {
  const app = definition.application;
  if (definition.output === "api")
    return (
      <Notice>
        API-only service. Business UI pages are excluded from this plan.
      </Notice>
    );
  if (!app)
    return (
      <section>
        <h3>Business application</h3>
        <p>
          Compose pages using the existing components. Published runtime access
          remains a separate gate.
        </p>
        <Button
          onClick={() =>
            onChange({
              title: definition.name || "Business workspace",
              pages: [{ key: "overview", title: "Overview", kind: "overview" }],
            })
          }
        >
          Configure application pages
        </Button>
      </section>
    );
  function update(
    index: number,
    value: Partial<ApplicationComposition["pages"][number]>,
  ) {
    if (!app) return;
    onChange({
      ...app,
      pages: app.pages.map((p, i) => (i === index ? { ...p, ...value } : p)),
    });
  }
  function add() {
    if (!app) return;
    let number = app.pages.length + 1;
    while (app.pages.some((p) => p.key === `page-${number}`)) number++;
    onChange({
      ...app,
      pages: [
        ...app.pages,
        { key: `page-${number}`, title: "New page", kind: "overview" },
      ],
    });
  }
  return (
    <section className="application-composer">
      <h3>Business application</h3>
      <label className="e-field">
        Application title
        <input
          minLength={2}
          maxLength={120}
          required
          value={app.title}
          onChange={(e) => onChange({ ...app, title: e.target.value })}
        />
      </label>
      {app.pages.map((page, index) => (
        <div className="application-page-row" key={page.key}>
          <label className="e-field">
            Page {index + 1} title
            <input
              required
              minLength={2}
              maxLength={80}
              value={page.title}
              onChange={(e) => update(index, { title: e.target.value })}
            />
          </label>
          <label className="e-field">
            Page {index + 1} component
            <select
              value={page.kind}
              onChange={(e) =>
                update(index, {
                  kind: e.target
                    .value as ApplicationComposition["pages"][number]["kind"],
                })
              }
            >
              {Object.entries(kinds).map(([key, name]) => (
                <option key={key} value={key}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <label className="e-field">
            Page {index + 1} workflow
            <select
              required={page.kind !== "overview"}
              value={page.workflow_key ?? ""}
              onChange={(e) => update(index, { workflow_key: e.target.value })}
            >
              <option value="">All linked workflows · overview only</option>
              {definition.workflows.map((w) => (
                <option key={w.key} value={w.key}>
                  {w.key}@{w.version}
                </option>
              ))}
            </select>
          </label>
          <Button
            disabled={app.pages.length <= 1}
            onClick={() =>
              onChange({
                ...app,
                pages: app.pages.filter((_, i) => i !== index),
              })
            }
          >
            Remove page {index + 1}
          </Button>
        </div>
      ))}
      <div className="dialog-actions">
        <Button disabled={app.pages.length >= 12} onClick={add}>
          Add application page
        </Button>
        <Button onClick={() => onChange(undefined)}>
          Remove application UI
        </Button>
      </div>
    </section>
  );
}
export function ApplicationPreview({
  definition,
  runtime,
}: {
  definition: SolutionDefinition;
  runtime?: {plan: Solution; session: SolutionSession};
}) {
  const [selected, setSelected] = useState("");
  const app = definition.application;
  if (definition.output === "api")
    return (
      <Notice>
        API-only service plan. Endpoint provisioning and API contracts are not
        installed by this preview.
      </Notice>
    );
  if (!app)
    return (
      <Notice>
        Configure application pages before previewing the business UI.
      </Notice>
    );
  const page = app.pages.find((p) => p.key === selected) ?? app.pages[0];
  if (!page) return null;
  const pin = definition.workflows.find((w) => w.key === page.workflow_key);
  return (
    <section
      className="application-preview"
      aria-label="Business application preview"
    >
      <header>
        <Badge tone="amber">{runtime ? "Development execution" : "Design preview"}</Badge>
        <h2>{app.title}</h2>
        <p>{runtime ? "Saved application pages connected to authorized workflow cases." : "No live customer data or operational actions are loaded."}</p>
      </header>
      <nav aria-label="Application pages">
        {app.pages.map((p) => (
          <Button
            key={p.key}
            variant={page.key === p.key ? "primary" : "secondary"}
            aria-pressed={page.key === p.key}
            onClick={() => setSelected(p.key)}
          >
            {p.title}
          </Button>
        ))}
      </nav>
      <div className="application-preview-content">
        <p className="eyebrow">{kinds[page.kind]}</p>
        <h3>{page.title}</h3>
        {pin && (
          <Badge>
            {pin.key}@{pin.version}
          </Badge>
        )}
        {runtime ? <SolutionRuntime plan={runtime.plan} session={runtime.session} page={page}/> : <>
        {page.kind === "overview" && (
          <>
            <h4>Linked business processes</h4>
            <ul>
              {definition.workflows.map((w) => (
                <li key={w.key}>
                  {w.key} · version {w.version}
                </li>
              ))}
            </ul>
            <p>
              Operational metrics appear only after verified runtime events are
              available.
            </p>
          </>
        )}
        {page.kind === "intake" && (
          <>
            <p>
              The published workflow input schema and authorized intake mapping
              must be resolved before this form can accept requests.
            </p>
            <Button
              disabled
              title="Design preview; no runtime schema mapping or installation verified"
            >
              Submit request
            </Button>
          </>
        )}
        {page.kind === "tasks" && (
          <>
            <h4>Tasks & reviews</h4>
            <p>
              No tasks are loaded in this design preview. Runtime case
              membership and independent approval rules will govern access.
            </p>
          </>
        )}
        {page.kind === "report" && (
          <>
            <h4>Business results</h4>
            <p>
              No execution results are loaded. Reports need authorized case
              outputs, evidence and measured data.
            </p>
          </>
        )}
        </>}
      </div>
    </section>
  );
}
