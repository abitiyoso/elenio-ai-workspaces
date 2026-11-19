import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Badge, Button, Notice } from "../../../../packages/ui/primitives";
import { basePath } from "../api/client";
import { useResource } from "../api/useResource";
import type { CaseDetail, CaseSummary } from "../api/cases";

export function ConnectedExecution({
  workflowKey,
  version,
}: {
  workflowKey: string;
  version: number;
}) {
  const [cursor, setCursor] = useState("");
  const [selected, setSelected] = useState("");
  const [list, reload] = useResource<{
    items: CaseSummary[];
    next_cursor: string | null;
  }>(
    `/api/v1/cases?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
  );
  const cases =
    list.state === "ready" && Array.isArray(list.data.items)
      ? list.data.items.filter(
          (c) =>
            c.workflow_key === workflowKey && c.workflow_version === version,
        )
      : [];
  return (
    <section
      className="connected-execution"
      aria-label="Server execution trace"
    >
      <header>
        <div>
          <h2>Case executions</h2>
          <p>
            Server records pinned to {workflowKey} v{version}. Refresh to
            retrieve current state.
          </p>
        </div>
        <Button onClick={reload}>Refresh cases</Button>
      </header>
      {list.state === "loading" ? (
        <p role="status">Loading authorized cases…</p>
      ) : list.state === "error" ? (
        <Notice error>{list.error.message}</Notice>
      ) : (
        <>
          <label className="e-field">
            Case execution
            <select
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
            >
              <option value="">Choose a case</option>
              {cases.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.title} · {c.status} · {c.reference}
                </option>
              ))}
            </select>
          </label>
          {!cases.length && (
            <Notice>
              No cases for this exact workflow version on this page. Drafts
              cannot start cases; use the existing runtime workspace for
              published versions.
            </Notice>
          )}
          {(cursor || list.data.next_cursor) && (
            <div className="dialog-footer">
              <Button disabled={!cursor} onClick={() => setCursor("")}>
                First page
              </Button>
              <Button
                disabled={!list.data.next_cursor}
                onClick={() => setCursor(list.data.next_cursor ?? "")}
              >
                Next cases page
              </Button>
            </div>
          )}
        </>
      )}
      {selected && (
        <ExecutionDetail
          key={selected}
          id={selected}
          workflowKey={workflowKey}
          version={version}
        />
      )}
    </section>
  );
}
function ExecutionDetail({
  id,
  workflowKey,
  version,
}: {
  id: string;
  workflowKey: string;
  version: number;
}) {
  const [resource, reload] = useResource<CaseDetail>(
    `/api/v1/cases/${encodeURIComponent(id)}`,
  );
  const data = resource.state === "ready" ? resource.data : null;
  useEffect(() => {
    if (!data || !["active", "under_review", "paused"].includes(data.status))
      return;
    const timer = setInterval(reload, 5000);
    return () => clearInterval(timer);
  }, [data, reload]);
  if (resource.state === "loading")
    return <p role="status">Loading server timeline…</p>;
  if (resource.state === "error")
    return (
      <Notice error>
        {resource.error.message}
        <Button onClick={reload}>Retry execution</Button>
      </Notice>
    );
  if (
    !data ||
    data.workflow_key !== workflowKey ||
    data.workflow_version !== version ||
    !Array.isArray(data.timeline)
  )
    return (
      <Notice error>Execution does not match this workflow version.</Notice>
    );
  return (
    <section className="execution-detail">
      <header>
        <h3>{data.title}</h3>
        <Badge tone={data.status === "completed" ? "green" : "amber"}>
          {data.status}
        </Badge>
        <Button onClick={reload}>Refresh execution</Button>
        <a href={`${basePath}/cases/${encodeURIComponent(id)}`}>
          Open runtime workspace
        </a>
        <Link to={`/connected/cases/${encodeURIComponent(id)}`}>
          Open case workspace
        </Link>
      </header>
      <p>
        {data.progress}% complete · pinned v{data.workflow_version} ·{" "}
        {data.reference}
      </p>
      <div className="catalog-table-wrap">
        <table className="dependency-table">
          <thead>
            <tr>
              <th>Step</th>
              <th>Status</th>
              <th>Iteration / attempt</th>
              <th>Completed</th>
            </tr>
          </thead>
          <tbody>
            {data.timeline.map((step) => (
              <tr key={step.id}>
                <td>
                  {step.node_name}
                  <small>
                    {step.node_type} · {step.node_id}
                  </small>
                </td>
                <td>
                  {step.status}
                  {step.last_error && <p role="alert">{step.last_error}</p>}
                </td>
                <td>
                  {step.iteration} / {step.attempt}
                </td>
                <td>
                  {step.completed_at
                    ? new Date(step.completed_at).toLocaleString()
                    : "Pending"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Notice>
        This timeline comes from Flowcase. Model usage and monetary cost are
        unavailable here; no values are estimated or inferred.
      </Notice>
    </section>
  );
}
