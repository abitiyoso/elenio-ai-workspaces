import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import { ApiError, apiRequest, newIdempotencyKey } from "../api/client";
import { useResource } from "../api/useResource";
import {
  completeTask,
  requestRevision,
  decideApproval,
  setCasePaused,
  addCaseNote,
  submitDraft,
  reviseDraft,
  blockedReasonText,
  type CaseSummary,
  type CaseDetail,
  type DataSchema,
} from "../api/cases";
import {
  type Page,
  type WorkflowSummary,
  type WorkflowVersion,
} from "../api/workflows";
import type { Session } from "../api/types";
import { DataForm } from "../components/DataForm";
import { toData, type FormValues } from "../components/formData";
import {
  isRuntimeCapabilities,
  isWorkflowVersion,
  type RuntimeCapabilities,
} from "./connectedApi";
import "./operations.css";

function objectJSON(text: string) {
  const value: unknown = JSON.parse(text);
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Enter a JSON object.");
  return value as Record<string, unknown>;
}
function failure(e: unknown) {
  return e instanceof ApiError
    ? `${e.message}${e.traceId ? ` · Request ${e.traceId}` : ""}`
    : e instanceof Error
      ? e.message
      : "The action failed. Inputs are retained.";
}
function Fields({
  schema,
  values,
  onChange,
  json,
  setJson,
}: {
  schema: DataSchema;
  values: FormValues;
  onChange: (v: FormValues) => void;
  json: string | null;
  setJson: (v: string | null) => void;
}) {
  const complex = Object.values(schema.properties ?? {}).some(
    (p) =>
      p.type === "object" || (p.type === "array" && p.items?.type !== "string"),
  );
  return (
    <div className="operation-form">
      {complex || json !== null ? (
        <label className="e-field">
          Inputs JSON
          <textarea
            rows={8}
            value={json ?? "{}"}
            onChange={(e) => setJson(e.target.value)}
          />
        </label>
      ) : (
        <DataForm
          schema={schema}
          values={values}
          onChange={onChange}
          idPrefix="case-input"
        />
      )}
      <Button onClick={() => setJson(json === null ? "{}" : null)}>
        {json === null ? "Use JSON inputs" : "Use form inputs"}
      </Button>
      <details>
        <summary>Input contract</summary>
        <pre>{JSON.stringify(schema, null, 2)}</pre>
      </details>
    </div>
  );
}
function inputData(
  schema: DataSchema,
  values: FormValues,
  json: string | null,
) {
  const complex = Object.values(schema.properties ?? {}).some(
    (p) =>
      p.type === "object" || (p.type === "array" && p.items?.type !== "string"),
  );
  return json !== null || complex
    ? objectJSON(json ?? "{}")
    : toData(schema, values);
}

export function CaseOperations({ session }: { session: Session }) {
  const [q, setQ] = useState(""),
    [status, setStatus] = useState("all"),
    [cursor, setCursor] = useState(""),
    [create, setCreate] = useState(false);
  const [resource, reload] = useResource<Page<CaseSummary>>(
    `/api/v1/cases?status=${status}&q=${encodeURIComponent(q)}&cursor=${encodeURIComponent(cursor)}`,
  );
  return (
    <section className="page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">OPERATIONS</span>
          <h1>Cases & reviews</h1>
          <p>
            Follow work, resolve tasks and review decisions in your authorized
            cases.
          </p>
        </div>
        <Button
          variant="primary"
          disabled={!session.roles.includes("case_owner")}
          onClick={() => setCreate(true)}
        >
          Start a case
        </Button>
      </header>
      <div className="operations-filters">
        <label className="e-field">
          Search cases
          <input
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setCursor("");
            }}
            placeholder="Title or reference"
          />
        </label>
        <label className="e-field">
          Case status
          <select
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setCursor("");
            }}
          >
            {["all", "active", "under_review", "paused", "closed"].map((s) => (
              <option key={s}>{s}</option>
            ))}
          </select>
        </label>
        <Button onClick={reload}>Refresh cases</Button>
      </div>
      {resource.state === "loading" ? (
        <p role="status">Loading authorized cases…</p>
      ) : resource.state === "error" ? (
        <Notice error>{resource.error.message}</Notice>
      ) : (
        <>
          <div className="operations-list">
            {resource.data.items.map((c) => (
              <Link
                key={c.id}
                to={`/connected/cases/${c.id}`}
                className="surface case-row"
              >
                <div>
                  <small>
                    {c.reference} · {c.workflow_key} v{c.workflow_version}
                  </small>
                  <h2>{c.title}</h2>
                  <p>
                    {c.owner_name} · {c.open_tasks} open tasks ·{" "}
                    {c.pending_approvals} pending approvals
                  </p>
                </div>
                <div>
                  <Badge tone={c.status === "completed" ? "green" : "amber"}>
                    {c.status}
                  </Badge>
                  <span>{c.progress}% complete</span>
                </div>
              </Link>
            ))}
          </div>
          {!resource.data.items.length && (
            <Notice>No authorized cases match these filters.</Notice>
          )}
          <div className="dialog-footer">
            <Button disabled={!cursor} onClick={() => setCursor("")}>
              First page
            </Button>
            <Button
              disabled={!resource.data.next_cursor}
              onClick={() => setCursor(resource.data.next_cursor ?? "")}
            >
              Next page
            </Button>
          </div>
        </>
      )}
      {create && <Intake onClose={() => setCreate(false)} />}
    </section>
  );
}
function Intake({ onClose }: { onClose: () => void }) {
  const [q, setQ] = useState(""),
    [key, setKey] = useState("");
  const [list] = useResource<Page<WorkflowSummary>>(
    `/api/v1/workflows?status=published&limit=100&q=${encodeURIComponent(q)}`,
  );
  const [caps] = useResource<RuntimeCapabilities>(
    "/api/v1/runtime-capabilities",
  );
  const selected =
    list.state === "ready" ? list.data.items.find((w) => w.key === key) : null;
  return (
    <DialogPanel
      title="Start a governed case"
      description="The server starts the latest published workflow version and pins its definition. Drafts cannot execute."
      onClose={onClose}
      wide
    >
      <label className="e-field">
        Find published workflow
        <input
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setKey("");
          }}
        />
      </label>
      {list.state === "error" ? (
        <Notice error>{list.error.message}</Notice>
      ) : list.state === "loading" ? (
        <p role="status">Loading published workflows…</p>
      ) : (
        <label className="e-field">
          Published workflow
          <select value={key} onChange={(e) => setKey(e.target.value)}>
            <option value="">Choose a workflow</option>
            {list.data.items.map((w) => (
              <option key={w.key} value={w.key}>
                {w.name} · v{w.latest_published_version}
              </option>
            ))}
          </select>
          <small>Up to 100 results; search to narrow the list.</small>
        </label>
      )}
      {selected?.latest_published_version && (
        <IntakeDocument
          key={`${key}:${selected.latest_published_version}`}
          workflow={key}
          version={selected.latest_published_version}
          supported={
            caps.state === "ready" && isRuntimeCapabilities(caps.data)
              ? caps.data.supported_node_types
              : null
          }
        />
      )}
    </DialogPanel>
  );
}
function IntakeDocument({
  workflow,
  version,
  supported,
}: {
  workflow: string;
  version: number;
  supported: string[] | null;
}) {
  const [resource] = useResource<WorkflowVersion>(
    `/api/v1/workflows/${encodeURIComponent(workflow)}/versions/${version}`,
  );
  const [title, setTitle] = useState(""),
    [values, setValues] = useState<FormValues>({}),
    [json, setJson] = useState<string | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [retryKey, setRetryKey] = useState(newIdempotencyKey);
  const navigate = useNavigate();
  if (resource.state === "loading")
    return <p role="status">Loading input contract…</p>;
  if (resource.state === "error")
    return <Notice error>{resource.error.message}</Notice>;
  const v = resource.data;
  if (
    !isWorkflowVersion(v) ||
    v.workflow_key !== workflow ||
    v.version !== version ||
    v.status !== "published"
  )
    return <Notice error>Published workflow contract is unavailable.</Notice>;
  const unsupported = v.definition.nodes.filter(
    (n) => !supported?.includes(n.type),
  );
  const schema = (v.definition.input_schema ?? {
    type: "object",
  }) as unknown as DataSchema;
  function changed() {
    setRetryKey(newIdempotencyKey());
    setError("");
  }
  return (
    <div>
      <label className="e-field">
        Case title
        <input
          value={title}
          disabled={busy}
          onChange={(e) => {
            setTitle(e.target.value);
            changed();
          }}
        />
      </label>
      <fieldset disabled={busy}>
        <Fields
          schema={schema}
          values={values}
          onChange={(v) => {
            setValues(v);
            changed();
          }}
          json={json}
          setJson={(v) => {
            setJson(v);
            changed();
          }}
        />
      </fieldset>
      {!supported || unsupported.length > 0 ? (
        <Notice error>
          Runtime support is unavailable for:{" "}
          {unsupported.map((n) => n.type).join(", ") || "this workflow"}. Case
          creation is locked.
        </Notice>
      ) : null}
      <Notice>
        Publishing a newer version before Start may change which version the
        server selects. The resulting case always displays its actual pinned
        version. External dependency readiness is checked by the server.
      </Notice>
      {error && <Notice error>{error}</Notice>}
      <Button
        variant="primary"
        disabled={busy || !title.trim() || !supported || unsupported.length > 0}
        onClick={async () => {
          setBusy(true);
          setError("");
          try {
            const result = (
              await apiRequest<CaseDetail>("POST", "/api/v1/cases", {
                headers: { "Idempotency-Key": retryKey },
                body: {
                  workflow_key: workflow,
                  title: title.trim(),
                  variables: inputData(schema, values, json),
                },
              })
            ).data;
            if (!result.id || result.workflow_key !== workflow)
              throw new Error(
                "Invalid case response. Check the case list before retrying.",
              );
            navigate(`/connected/cases/${encodeURIComponent(result.id)}`);
          } catch (e) {
            setError(failure(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        {busy ? "Starting…" : "Start case"}
      </Button>
    </div>
  );
}

type Action = {
  title: string;
  description: string;
  schema?: DataSchema;
  requiredText?: boolean;
  submit: (text: string, output: Record<string, unknown>) => Promise<unknown>;
};
export function OperationCase() {
  const { id = "" } = useParams();
  return <CaseDocument key={id} id={id} />;
}
function CaseDocument({ id }: { id: string }) {
  const [resource, reload] = useResource<CaseDetail>(
    `/api/v1/cases/${encodeURIComponent(id)}`,
  );
  const [action, setAction] = useState<Action | null>(null),
    [tab, setTab] = useState("Work"),
    [notice, setNotice] = useState("");
  const data = resource.state === "ready" ? resource.data : null;
  useEffect(() => {
    if (action || !data || !["active", "under_review"].includes(data.status))
      return;
    const timer = setInterval(reload, 5000);
    return () => clearInterval(timer);
  }, [action, data, reload]);
  if (resource.state === "loading")
    return (
      <section className="page">
        <p role="status">Loading case…</p>
      </section>
    );
  if (resource.state === "error")
    return (
      <section className="page">
        <Notice error>
          {resource.error.message}
          <Button onClick={reload}>Retry case</Button>
        </Notice>
        <Link to="/connected/operations">Back to cases</Link>
      </section>
    );
  if (
    !data ||
    data.id !== id ||
    !Array.isArray(data.tasks) ||
    !Array.isArray(data.approvals) ||
    !data.actions
  )
    return <Notice error>Invalid case contract.</Notice>;
  const open = (a: Action) => {
    setNotice("");
    setAction(a);
  };
  return (
    <section className="page operation-case">
      <header className="page-heading">
        <div>
          <Link to="/connected/operations">Cases & reviews</Link>
          <h1>{data.title}</h1>
          <p>
            {data.reference} · {data.workflow_key} v{data.workflow_version} ·{" "}
            {data.owner_name}
          </p>
        </div>
        <Badge tone={data.status === "completed" ? "green" : "amber"}>
          {data.status} · {data.progress}%
        </Badge>
      </header>
      <div className="economics-actions">
        <Button disabled={!!action} onClick={reload}>
          Refresh case
        </Button>
        <Link
          className="e-button"
          to={`/connected/projects/${encodeURIComponent(data.workflow_key)}/${data.workflow_version}`}
        >
          Pinned workflow
        </Link>
        {data.actions.can_pause === true && (
          <Button
            onClick={() =>
              open({
                title: "Pause case",
                description: "Pause this case without bypassing open reviews.",
                requiredText: true,
                submit: (t) => setCasePaused(id, data.version_num, true, t),
              })
            }
          >
            Pause case
          </Button>
        )}
        {data.actions.can_resume === true && (
          <Button
            onClick={() =>
              open({
                title: "Resume case",
                description: "Resume this case on its pinned workflow.",
                requiredText: true,
                submit: (t) => setCasePaused(id, data.version_num, false, t),
              })
            }
          >
            Resume case
          </Button>
        )}
        {data.actions.can_note === true && (
          <Button
            onClick={() =>
              open({
                title: "Add case note",
                description: "This note is recorded in case history.",
                requiredText: true,
                submit: (t) => addCaseNote(id, t),
              })
            }
          >
            Add note
          </Button>
        )}
      </div>
      {notice && <Notice>{notice}</Notice>}
      <nav className="tabs" aria-label="Case sections">
        {["Work", "Activity", "Evidence"].map((t) => (
          <button
            key={t}
            className={tab === t ? "active" : ""}
            onClick={() => setTab(t)}
          >
            {t}
          </button>
        ))}
      </nav>
      {tab === "Work" ? (
        <div className="operations-work">
          <section>
            <h2>Tasks</h2>
            {data.tasks.length ? (
              data.tasks.map((task) => (
                <article className="surface operations-card" key={task.id}>
                  <h3>{task.title}</h3>
                  <p>
                    {task.assignee_role} · {task.status} · iteration{" "}
                    {task.iteration}
                  </p>
                  <p>{task.instructions}</p>
                  {task.revision_note && <Notice>{task.revision_note}</Notice>}
                  {task.can_complete === true && (
                    <Button
                      variant="primary"
                      onClick={() =>
                        open({
                          title: `Complete ${task.title}`,
                          description:
                            "Submit this task using its current server version. This does not approve the case.",
                          schema: task.form_schema ?? { type: "object" },
                          submit: (text, output) =>
                            completeTask(
                              task.id,
                              task.version_num,
                              output,
                              text,
                            ),
                        })
                      }
                    >
                      Complete task
                    </Button>
                  )}
                  {task.can_request_revision === true && (
                    <Button
                      onClick={() =>
                        open({
                          title: `Request revision: ${task.title}`,
                          description:
                            "Record a revision request for this task.",
                          requiredText: true,
                          submit: (t) =>
                            requestRevision(task.id, task.version_num, t),
                        })
                      }
                    >
                      Request task revision
                    </Button>
                  )}
                  {task.output && (
                    <details>
                      <summary>Submitted output</summary>
                      <pre>{JSON.stringify(task.output, null, 2)}</pre>
                    </details>
                  )}
                </article>
              ))
            ) : (
              <p>No tasks have been created.</p>
            )}
          </section>
          <section>
            <h2>Approvals</h2>
            {data.approvals.length ? (
              data.approvals.map((a) => (
                <article className="surface operations-card" key={a.id}>
                  <h3>{a.title}</h3>
                  <p>
                    {a.policy_name || a.policy_ref} · {a.status}
                  </p>
                  {a.policy_provisional && (
                    <Notice>
                      Provisional policy: business ratification remains pending.
                      Decisions are still audited.
                    </Notice>
                  )}
                  <details>
                    <summary>Decision scope & evidence</summary>
                    <p>
                      State hash: <code>{a.subject_hash}</code>
                    </p>
                    <pre>
                      {JSON.stringify(
                        {
                          subject: a.subject,
                          covered_artifacts: a.covered_artifacts,
                          invalidated_reason: a.invalidated_reason,
                        },
                        null,
                        2,
                      )}
                    </pre>
                  </details>
                  {a.decision ? (
                    <Notice>
                      {a.decision.decided_by_name}: {a.decision.decision} —{" "}
                      {a.decision.rationale}
                      <br />
                      Decision hash: <code>{a.decision.decision_hash}</code>
                    </Notice>
                  ) : a.can_decide !== true ? (
                    <Notice>
                      {blockedReasonText[a.blocked_reason ?? ""] ??
                        a.blocked_reason ??
                        "No decision authority for this approval."}
                    </Notice>
                  ) : (
                    <div className="economics-actions">
                      {(
                        [
                          "approve",
                          "reject",
                          ...(a.return_to ? ["return"] : []),
                        ] as ("approve" | "reject" | "return")[]
                      ).map((decision) => (
                        <Button
                          key={decision}
                          variant={
                            decision === "reject" ? "danger" : "secondary"
                          }
                          onClick={() =>
                            open({
                              title: `${decision.charAt(0).toUpperCase() + decision.slice(1)} ${a.title}`,
                              description: `Policy ${a.policy_ref}. Decision is bound to state ${a.subject_hash}. Review scope and evidence before confirming.`,
                              requiredText: true,
                              submit: (t) =>
                                decideApproval(
                                  a.id,
                                  a.version_num,
                                  decision,
                                  t,
                                ),
                            })
                          }
                        >
                          {decision.charAt(0).toUpperCase() + decision.slice(1)}
                        </Button>
                      ))}
                    </div>
                  )}
                </article>
              ))
            ) : (
              <p>No approvals have been requested.</p>
            )}
          </section>
          {data.drafts?.length > 0 && (
            <section>
              <h2>Draft reviews</h2>
              {data.drafts.map((d) => (
                <article className="surface operations-card" key={d.id}>
                  <h3>{d.node_name}</h3>
                  <p>
                    {d.artifact_type} v{d.artifact_version} · {d.status} ·{" "}
                    {d.revisions} revisions
                  </p>
                  <pre>{JSON.stringify(d.content, null, 2)}</pre>
                  {d.can_submit === true && (
                    <Button
                      onClick={() =>
                        open({
                          title: "Submit reviewed draft",
                          description:
                            "Send this reviewed artifact onward. Independent approval is still required where defined.",
                          submit: (t) => submitDraft(d.id, d.version_num, t),
                        })
                      }
                    >
                      Submit draft
                    </Button>
                  )}
                  {d.can_revise === true && (
                    <Button
                      onClick={() =>
                        open({
                          title: "Revise agent draft",
                          description:
                            "The configured runtime must be available to produce a new revision.",
                          requiredText: true,
                          submit: (t) => reviseDraft(d.id, d.version_num, t),
                        })
                      }
                    >
                      Request draft revision
                    </Button>
                  )}
                </article>
              ))}
            </section>
          )}
        </div>
      ) : tab === "Activity" ? (
        <section className="surface operations-card">
          <h2>Server activity</h2>
          <ol>
            {data.history.map((h, i) => (
              <li key={i}>
                <strong>{h.action}</strong> · {h.actor_name ?? h.actor_type} ·{" "}
                {new Date(h.occurred_at).toLocaleString()}
              </li>
            ))}
          </ol>
          <details>
            <summary>Node executions</summary>
            <pre>{JSON.stringify(data.timeline, null, 2)}</pre>
          </details>
        </section>
      ) : (
        <section className="surface operations-card">
          <h2>Versioned artifacts & evidence</h2>
          {data.artifacts?.length ? (
            data.artifacts.map((a) => (
              <details key={a.id}>
                <summary>
                  {a.artifact_type} · current v{a.current_version}
                </summary>
                {a.versions.map((v) => (
                  <div key={v.id}>
                    <h3>
                      v{v.version} · {v.status}
                    </h3>
                    <p>{v.stale_reason}</p>
                    <code>{v.checksum}</code>
                    <pre>
                      {JSON.stringify(
                        {
                          content: v.content,
                          evidence: v.evidence,
                          depends_on: v.depends_on,
                        },
                        null,
                        2,
                      )}
                    </pre>
                  </div>
                ))}
              </details>
            ))
          ) : (
            <Notice>
              No artifacts have been produced for this case. Missing sources are
              not fabricated.
            </Notice>
          )}
          <details>
            <summary>Case inputs</summary>
            <pre>{JSON.stringify(data.variables, null, 2)}</pre>
          </details>
        </section>
      )}
      {action && (
        <ActionDialog
          action={action}
          onClose={() => setAction(null)}
          onDone={(recorded) => {
            setAction(null);
            setNotice(
              recorded
                ? "Server action recorded."
                : "Form discarded. Latest server state loaded.",
            );
            reload();
          }}
        />
      )}
    </section>
  );
}
function ActionDialog({
  action,
  onClose,
  onDone,
}: {
  action: Action;
  onClose: () => void;
  onDone: (recorded: boolean) => void;
}) {
  const [text, setText] = useState(""),
    [values, setValues] = useState<FormValues>({}),
    [json, setJson] = useState<string | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [stale, setStale] = useState(false);
  return (
    <DialogPanel
      title={action.title}
      description={action.description}
      onClose={() => {
        if (!busy) onClose();
      }}
      wide
    >
      <fieldset disabled={busy}>
        {action.schema && (
          <Fields
            schema={action.schema}
            values={values}
            onChange={setValues}
            json={json}
            setJson={setJson}
          />
        )}
        <label className="e-field">
          {action.requiredText ? "Rationale" : "Comment"}
          <textarea
            aria-label={action.requiredText ? "Rationale" : "Comment"}
            value={text}
            maxLength={1000}
            onChange={(e) => setText(e.target.value)}
          />
        </label>
      </fieldset>
      {error && <Notice error>{error}</Notice>}
      {stale && (
        <Notice>
          The server state changed. Your form is retained. Copy it before
          discarding and reloading; it will not be retried automatically.
          <Button onClick={() => onDone(false)}>Discard form and reload</Button>
        </Notice>
      )}
      <div className="dialog-footer">
        <Button disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="primary"
          disabled={
            busy || stale || (!!action.requiredText && text.trim().length < 5)
          }
          onClick={async () => {
            setBusy(true);
            setError("");
            try {
              await action.submit(
                text.trim(),
                action.schema ? inputData(action.schema, values, json) : {},
              );
              onDone(true);
            } catch (e) {
              setError(failure(e));
              if (e instanceof ApiError && [409, 412].includes(e.status))
                setStale(true);
            } finally {
              setBusy(false);
            }
          }}
        >
          {busy ? "Submitting…" : "Confirm action"}
        </Button>
      </div>
    </DialogPanel>
  );
}
