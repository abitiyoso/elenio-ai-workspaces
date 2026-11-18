import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Badge, Button, Notice } from "../../../../packages/ui/primitives";
import { basePath } from "../api/client";
import { useResource } from "../api/useResource";
import type { Catalog } from "../api/workflows";

import {
  controlPlaneGet,
  safeAppRoute,
  type AppSummary,
} from "./controlPlaneApps";
function AppLauncher() {
  const [apps, setApps] = useState<AppSummary[] | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState("");
  const hosted = basePath === "/apps/flowcase";
  useEffect(() => {
    if (!hosted) return;
    const ctrl = new AbortController();
    controlPlaneGet<{ items: AppSummary[] }>("/api/v1/apps", ctrl.signal).then(
      (data) => {
        if (!ctrl.signal.aborted) {
          if (!Array.isArray(data?.items)) {
            setError("Invalid app registry response.");
            return;
          }
          setApps(
            data.items.filter(
              (a) =>
                a.status === "published" &&
                safeAppRoute(a.app_key, a.route_base),
            ),
          );
        }
      },
      (e) => {
        if (!ctrl.signal.aborted)
          setError(
            e instanceof Error ? e.message : "App registry unavailable.",
          );
      },
    );
    return () => ctrl.abort();
  }, [hosted]);
  async function launch(app: AppSummary) {
    setBusy(app.app_key);
    setError("");
    try {
      const result = await controlPlaneGet<{
        app_key: string;
        route_base: string;
        integration: string;
      }>(`/api/v1/apps/${app.app_key}/launch`);
      if (
        result.app_key !== app.app_key ||
        !safeAppRoute(app.app_key, result.route_base) ||
        result.integration !== "proxy"
      )
        throw new Error("This app has no verified proxied launch route.");
      window.location.assign(result.route_base);
    } catch (e) {
      setError(e instanceof Error ? e.message : "App launch failed.");
    } finally {
      setBusy("");
    }
  }
  return (
    <section className="surface catalog-section">
      <h2>Governed capability workspaces</h2>
      <p>
        Use the existing Agent Studio, Knowledge Hub and integration workspaces
        through the Control Plane app registry. Their domain authorization
        remains independent.
      </p>
      {!hosted ? (
        <Notice>
          Launch is unavailable in standalone mode. Open this frontend through
          the configured Control Plane Flowcase mount to discover entitled apps.
          No deployment route or authorization is inferred from sample
          manifests.
        </Notice>
      ) : apps === null && !error ? (
        <div role="status">Loading entitled apps…</div>
      ) : (
        <div className="app-launch-list">
          {apps
            ?.filter((a) => a.app_key !== "flowcase")
            .map((app) => (
              <article key={app.app_key}>
                <div>
                  <h3>{app.display_name}</h3>
                  <p>{app.description}</p>
                </div>
                <Button
                  disabled={!!busy || app.integration !== "proxy"}
                  onClick={() => void launch(app)}
                >
                  {busy === app.app_key
                    ? "Authorizing…"
                    : app.integration === "proxy"
                      ? "Open workspace"
                      : "Integration pending"}
                </Button>
              </article>
            ))}
        </div>
      )}
      {error && <Notice error>{error}</Notice>}
    </section>
  );
}

export function ConnectedCatalog() {
  const [resource, reload] = useResource<Catalog>("/api/v1/workflow-catalog");
  const [tab, setTab] = useState("Agents");
  const catalog = resource.state === "ready" ? resource.data : null;
  const valid =
    !!catalog &&
    Array.isArray(catalog.agents) &&
    Array.isArray(catalog.connectors) &&
    Array.isArray(catalog.policies);
  return (
    <section className="page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">REGISTERED CAPABILITIES</span>
          <h1>Dependency catalog</h1>
          <p>
            Inspect references accepted by Flowcase before binding them to a
            draft.
          </p>
        </div>
        <Link to="/connected">Workflow projects</Link>
      </header>
      {resource.state === "loading" ? (
        <div role="status" className="e-empty">
          Loading registered capabilities…
        </div>
      ) : resource.state === "error" || !valid ? (
        <Notice error>
          {resource.state === "error"
            ? resource.error.message
            : "Invalid catalog response."}
          <Button onClick={reload}>Retry catalog</Button>
        </Notice>
      ) : (
        <section className="surface catalog-section">
          <div className="tabs">
            {["Agents", "Integrations", "Approval policies"].map((t) => (
              <button
                key={t}
                className={tab === t ? "active" : ""}
                onClick={() => setTab(t)}
              >
                {t}
              </button>
            ))}
          </div>
          <div className="catalog-table-wrap">
            <table className="dependency-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Reference</th>
                  <th>
                    {tab === "Agents"
                      ? "Pinned skill"
                      : tab === "Integrations"
                        ? "Registered operations"
                        : "Governance"}
                  </th>
                </tr>
              </thead>
              <tbody>
                {tab === "Agents"
                  ? catalog.agents.map((a) => (
                      <tr key={a.ref}>
                        <td>{a.name}</td>
                        <td>
                          <code>{a.ref}</code>
                        </td>
                        <td>{a.skill_version}</td>
                      </tr>
                    ))
                  : tab === "Integrations"
                    ? catalog.connectors.map((c) => (
                        <tr key={c.ref}>
                          <td>{c.name}</td>
                          <td>
                            <code>{c.ref}</code>
                          </td>
                          <td>{c.operations.join(", ")}</td>
                        </tr>
                      ))
                    : catalog.policies.map((p) => (
                        <tr key={p.ref}>
                          <td>{p.name}</td>
                          <td>
                            <code>{p.ref}</code>
                          </td>
                          <td>
                            <Badge tone={p.provisional ? "amber" : "green"}>
                              {p.provisional ? "Provisional" : "Ratified"}
                            </Badge>
                            <small>{p.sod.join(" · ")}</small>
                          </td>
                        </tr>
                      ))}
              </tbody>
            </table>
          </div>
          <Notice>
            Registration is not evidence of an approved active runtime release.
            Server validation resolves bindings, permissions and version
            readiness. Connector operations use existing registered adapters; no
            arbitrary API/MCP execution is added here.
          </Notice>
        </section>
      )}
      <AppLauncher />
    </section>
  );
}
