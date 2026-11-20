import { useEffect, useState } from "react";
import { Badge, Button, Notice } from "../../../../packages/ui/primitives";
import { useResource } from "../api/useResource";
import { CaseDocument, IntakeDocument } from "./CaseOperations";
import { isRuntimeCapabilities, type RuntimeCapabilities } from "./connectedApi";
import { listSolutionExecutions, startSolutionExecution, type Solution, type SolutionSession, type SolutionExecution } from "./solutionApi";

export function SolutionRuntime({plan, session, page}: {
  plan: Solution; session: SolutionSession;
  page: NonNullable<Solution["definition"]["application"]>["pages"][number];
}) {
  const [caps] = useResource<RuntimeCapabilities>("/api/v1/runtime-capabilities");
  const [items, setItems] = useState<SolutionExecution[]>([]);
  const [cursor, setCursor] = useState("");
  const [selected, setSelected] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void listSolutionExecutions(session, plan, "", controller.signal).then(result => {
      if (!controller.signal.aborted) {setItems(result.items); setCursor(result.next_cursor);}
    }).catch((e: unknown) => {
      if (!controller.signal.aborted) setError(e instanceof Error ? e.message : "Execution history could not be loaded.");
    }).finally(() => {if (!controller.signal.aborted) setLoading(false);});
    return () => controller.abort();
  }, [plan, session, refresh]);
  const pin = plan.definition.workflows.find(w => w.key === page.workflow_key);
  const visible = items.filter(item => !page.workflow_key || item.execution.workflow_key === page.workflow_key);
  return <section aria-label="Solution development executions">
    <Notice>Development execution uses saved Solution revision {plan.revision} and your current Flowcase permissions. This is a real run, not an installed customer release.</Notice>
    {page.kind === "intake" && pin && <IntakeDocument key={`${plan.id}:${plan.revision}:${pin.key}:${pin.version}`}
      workflow={pin.key} version={pin.version}
      supported={session.permissions.includes("solution:author") && caps.state === "ready" && isRuntimeCapabilities(caps.data) ? caps.data.supported_node_types : null}
      onStart={async (input, requestKey) => {
        const result = await startSolutionExecution(session, plan, pin.key, input, requestKey);
        setItems(previous => [result, ...previous.filter(item => item.execution.id !== result.execution.id)]);
        setSelected(result.case?.id ?? "");
      }}/ >}
    <div className="dialog-actions"><h4>Your Solution executions</h4>
      <Button disabled={loading} onClick={() => {setLoading(true); setError(""); setItems([]); setSelected(""); setRefresh(n => n + 1);}}>Refresh executions</Button></div>
    <p>Only runs you started in this Solution are listed. Open a case for its tasks, independent reviews, results and evidence.</p>
    {loading && <p role="status">Loading authorized executions…</p>}
    {error && <Notice error>{error}</Notice>}
    {!loading && !error && !visible.length && <p>No executions for this page in the loaded history.</p>}
    {visible.map(item => <article className="surface operations-card" key={item.execution.id}>
      <h4>{item.case?.title ?? "Execution pending confirmation"}</h4>
      <p>{item.execution.workflow_key}@{item.execution.workflow_version} · Solution revision {item.execution.plan_revision}</p>
      <Badge>{item.case?.status ?? "Pending — retry the original request to recover"}</Badge>{" "}
      {item.case && <Button onClick={() => setSelected(item.case?.id ?? "")}>Open {item.case.reference}</Button>}
    </article>)}
    {cursor && <Button disabled={loading} onClick={async () => {
      setLoading(true);setError("");
      try { const next = await listSolutionExecutions(session, plan, cursor);setItems(previous => [...previous, ...next.items]);setCursor(next.next_cursor); }
      catch (e) {setError(e instanceof Error ? e.message : "History could not be loaded.");}
      finally {setLoading(false);}
    }}>Load more executions</Button>}
    {selected && <><Button onClick={() => setSelected("")}>Close case details</Button><CaseDocument key={selected} id={selected}/></>}
  </section>;
}
