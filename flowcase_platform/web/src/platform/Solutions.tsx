import { useEffect, useRef, useState } from "react";
import { Link, useBlocker } from "react-router-dom";
import { ArrowRight, FolderOpen, Plus, RefreshCw } from "lucide-react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import { newIdempotencyKey } from "../api/client";
import { ApplicationComposer, ApplicationPreview } from "./ApplicationComposer";
import { PublisherCandidates } from "./PublisherCandidates";
import {
  listSolutions,
  checkSolution,
  parseWorkflowReferences,
  saveSolution,
  solutionSession,
  type Solution,
  type SolutionDefinition,
  type SolutionSession,
  type SolutionReadiness,
} from "./solutionApi";

const empty: SolutionDefinition = {
  key: "",
  name: "",
  description: "",
  delivery_mode: "solution",
  output: "both",
  workflows: [],
};
const modes = {
  internal: "Internal engineering",
  solution: "Customer solution",
  enterprise: "Enterprise AI OS",
};
const outputs = {
  business_ui: "Business application",
  api: "API service",
  both: "Business application + API",
};
export function Solutions() {
  const [preview, setPreview] = useState<Solution>();
  const [publisher, setPublisher] = useState<Solution>();
  const [checks, setChecks] = useState<Record<string, SolutionReadiness>>({});
  const [checking, setChecking] = useState("");
  const [session, setSession] = useState<SolutionSession>(),
    [items, setItems] = useState<Solution[]>([]),
    [cursor, setCursor] = useState("");
  const [loading, setLoading] = useState(true),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [retry, setRetry] = useState(0);
  const [editor, setEditor] = useState<{
      item?: Solution;
      definition: SolutionDefinition;
      references: string;
    }>(),
    [saving, setSaving] = useState(false),
    [formError, setFormError] = useState("");
  const requestKey = useRef("");
  const dirty =
    !!editor &&
    (JSON.stringify(editor.definition) !==
      JSON.stringify(editor.item?.definition ?? empty) ||
      editor.references !==
        (editor.item?.definition.workflows
          .map((w) => `${w.key}@${w.version}`)
          .join("\n") ?? ""));
  const blocker = useBlocker(dirty && !saving);
  let editorWorkflows: SolutionDefinition["workflows"] = [];
  if (editor) {
    try {
      editorWorkflows = parseWorkflowReferences(editor.references);
    } catch {
      /* Invalid pins remain editable until save. */
    }
  }
  useEffect(() => {
    const controller = new AbortController();
    solutionSession(controller.signal)
      .then(async (me) => {
        const page = await listSolutions(me, "", controller.signal);
        if (controller.signal.aborted) return;
        setSession(me);
        setItems(page.items);
        setCursor(page.next_cursor);
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(
            e instanceof Error ? e.message : "Solutions are unavailable.",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [retry]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  function open(item?: Solution) {
    setEditor({
      ...(item ? { item } : {}),
      definition: structuredClone(item?.definition ?? empty),
      references:
        item?.definition.workflows
          .map((w) => `${w.key}@${w.version}`)
          .join("\n") ?? "",
    });
    setFormError("");
    requestKey.current = newIdempotencyKey();
  }
  async function save() {
    if (!editor || !session) return;
    setSaving(true);
    setFormError("");
    try {
      const definition = {
        ...editor.definition,
        workflows: parseWorkflowReferences(editor.references),
      };
      const result = await saveSolution(session, definition, {
        ...(editor.item
          ? { id: editor.item.id, revision: editor.item.revision }
          : {}),
        requestKey: requestKey.current,
      });
      setItems((old) => [result, ...old.filter((i) => i.id !== result.id)]);
      setEditor(undefined);
      setNotice(
        "Solution plan saved in Control Plane. Workflow references still require dependency verification before release.",
      );
    } catch (e) {
      setFormError(
        e instanceof Error
          ? e.message
          : "Unable to save. Your edits were retained.",
      );
    } finally {
      setSaving(false);
    }
  }
  async function check(item: Solution) {
    if (!session) return;
    setChecking(item.id);
    setError("");
    try {
      const report = await checkSolution(session, item);
      setChecks((old) => ({ ...old, [item.id]: report }));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Dependency check failed.");
    } finally {
      setChecking("");
    }
  }
  async function more() {
    if (!session) return;
    setLoading(true);
    try {
      const current = await solutionSession();
      if (
        current.workspace.id !== session.workspace.id ||
        current.tenant.id !== session.tenant.id ||
        current.user.id !== session.user.id
      )
        throw new Error("Workspace changed. Refresh solutions.");
      const page = await listSolutions(current, cursor);
      setItems((old) => [
        ...old,
        ...page.items.filter(
          (i) => !old.some((existing) => existing.id === i.id),
        ),
      ]);
      setCursor(page.next_cursor);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Unable to load more solutions.",
      );
    } finally {
      setLoading(false);
    }
  }
  function close() {
    if (saving) return;
    if (dirty && !window.confirm("Discard unsaved solution changes?")) return;
    setEditor(undefined);
  }
  return (
    <div className="solutions-page">
      <header className="page-heading">
        <div>
          <p className="eyebrow">SOLUTION ENGINEERING</p>
          <h1>Solutions</h1>
          <p>
            One project context for your application and its pinned workflows.
          </p>
        </div>
        <Button
          variant="primary"
          disabled={
            !session?.permissions.includes("solution:author") || loading
          }
          onClick={() => open()}
        >
          <Plus size={16} />
          New solution
        </Button>
      </header>
      {loading && !session && (
        <Notice>Checking Control Plane workspace…</Notice>
      )}
      {error && <Notice error>{error}</Notice>}
      {!session && !loading && (
        <section className="connected-failure">
          <FolderOpen size={30} />
          <h2>Connect your solution workspace</h2>
          <p>
            Solutions use the Control Plane session and workspace permissions.
            No local sample projects are substituted.
          </p>
          <Button
            onClick={() => {
              setLoading(true);
              setError("");
              setSession(undefined);
              setItems([]);
              setCursor("");
              setRetry((v) => v + 1);
            }}
          >
            <RefreshCw size={16} />
            Retry connection
          </Button>
        </section>
      )}
      {notice && <Notice>{notice}</Notice>}
      {session && (
        <>
          <div className="solutions-context">
            <Badge tone="blue">{session.workspace.name}</Badge>
            <span>
              {session.tenant.name} · {session.user.display_name}
            </span>
            <Badge>{session.environment}</Badge>
          </div>
          <Notice>
            Solution plans do not publish or deploy. Pinned references must be
            verified against their service before a solution release.
          </Notice>
          {!items.length && (
            <section className="e-empty">
              <FolderOpen size={28} />
              <h2>No solutions in this workspace</h2>
              <p>
                Create a solution plan, then link its existing workflow
                versions.
              </p>
            </section>
          )}
          <div className="solutions-list">
            {items.map((item) => (
              <article className="solution-card" key={item.id}>
                <div>
                  <Badge>Plan · revision {item.revision}</Badge>
                  <h2>{item.definition.name}</h2>
                  <p>{item.definition.description || "No description yet."}</p>
                  <div className="solutions-context">
                    <Badge>{modes[item.definition.delivery_mode]}</Badge>
                    <Badge>{outputs[item.definition.output]}</Badge>
                  </div>
                </div>
                <div>
                  <h3>Pinned workflows</h3>
                  {item.definition.workflows.length ? (
                    <ul>
                      {item.definition.workflows.map((w) => (
                        <li key={w.key}>
                          <code>
                            {w.key}@{w.version}
                          </code>
                          <Badge
                            tone={
                              checks[item.id]?.revision === item.revision &&
                              checks[item.id]?.workflows.find(
                                (ref) => ref.key === w.key,
                              )?.state === "verified"
                                ? "green"
                                : "amber"
                            }
                          >
                            {checks[item.id]?.revision === item.revision &&
                            checks[item.id]?.workflows.find(
                              (ref) => ref.key === w.key,
                            )?.state === "verified"
                              ? "Published version verified"
                              : "Not verified for release"}
                          </Badge>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p>No workflow references linked.</p>
                  )}
                  <Button onClick={() => open(item)}>
                    {session.permissions.includes("solution:author")
                      ? "Edit solution plan"
                      : "View solution plan"}
                    <ArrowRight size={15} />
                  </Button>
                  <Link className="e-button" to={`/solutions/${item.id}/describe`}>Open Solution journey</Link>
                  <Button onClick={() => setPreview(item)}>
                    Preview application
                  </Button>
                  <Button onClick={() => setPublisher(item)}>
                    Publisher candidates
                  </Button>
                  <Button
                    disabled={!!checking}
                    onClick={() => void check(item)}
                  >
                    {checking === item.id
                      ? "Checking…"
                      : "Check workflow dependencies"}
                  </Button>
                  {checks[item.id]?.revision === item.revision && (
                    <section aria-label="Dependency check">
                      <p>
                        Checked{" "}
                        {new Date(
                          checks[item.id]?.checked_at ?? "",
                        ).toLocaleString()}{" "}
                        · solution revision {item.revision}
                      </p>
                      <ul>
                        {checks[item.id]?.workflows.map((ref) => (
                          <li key={ref.key}>
                            {ref.key}@{ref.version}: {ref.reason}
                          </li>
                        ))}
                      </ul>
                      <Notice>
                        Solution release is not verified.{" "}
                        {checks[item.id]?.blockers.join(" ")}
                      </Notice>
                      <ul>
                        {checks[item.id]?.workflows.flatMap((w) =>
                          (w.resources ?? []).map((resource, index) => (
                            <li key={`${w.key}-${resource.kind}-${index}`}>
                              <Badge tone="amber">{resource.kind}</Badge>{" "}
                              {resource.ref}
                              {resource.version
                                ? `@${resource.version}`
                                : ""}: {resource.reason}
                            </li>
                          )),
                        )}
                      </ul>
                      <ul>
                        {checks[item.id]?.application_issues?.map(
                          (issue, index) => (
                            <li key={index}>{issue}</li>
                          ),
                        )}
                      </ul>
                    </section>
                  )}
                </div>
              </article>
            ))}
          </div>
          {cursor && (
            <Button disabled={loading} onClick={() => void more()}>
              {loading ? "Loading…" : "Load more solutions"}
            </Button>
          )}
        </>
      )}
      {editor && (
        <DialogPanel
          title={editor.item ? "Solution plan" : "New solution"}
          description="Planning metadata only. Linked resources keep their existing permissions, versions and approval gates."
          onClose={close}
          wide
        >
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void save();
            }}
          >
            <fieldset
              disabled={
                saving || !session?.permissions.includes("solution:author")
              }
              className="solution-fields"
            >
              <label className="e-field">
                Solution name
                <input
                  required
                  minLength={2}
                  maxLength={120}
                  value={editor.definition.name}
                  onChange={(e) =>
                    setEditor({
                      ...editor,
                      definition: {
                        ...editor.definition,
                        name: e.target.value,
                      },
                    })
                  }
                />
              </label>
              <label className="e-field">
                Solution key
                <input
                  required
                  pattern="[a-z][a-z0-9\-]{1,62}"
                  disabled={!!editor.item}
                  value={editor.definition.key}
                  onChange={(e) =>
                    setEditor({
                      ...editor,
                      definition: { ...editor.definition, key: e.target.value },
                    })
                  }
                />
              </label>
              <label className="e-field">
                Description
                <textarea
                  maxLength={12000}
                  value={editor.definition.description}
                  onChange={(e) =>
                    setEditor({
                      ...editor,
                      definition: {
                        ...editor.definition,
                        description: e.target.value,
                      },
                    })
                  }
                />
              </label>
              <label className="e-field">
                Delivery mode
                <select
                  value={editor.definition.delivery_mode}
                  onChange={(e) =>
                    setEditor({
                      ...editor,
                      definition: {
                        ...editor.definition,
                        delivery_mode: e.target
                          .value as SolutionDefinition["delivery_mode"],
                      },
                    })
                  }
                >
                  {Object.entries(modes).map(([key, name]) => (
                    <option value={key} key={key}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="e-field">
                Application output
                <select
                  value={editor.definition.output}
                  onChange={(e) => {
                    const definition = {
                      ...editor.definition,
                      output: e.target.value as SolutionDefinition["output"],
                    };
                    if (definition.output === "api")
                      delete definition.application;
                    setEditor({ ...editor, definition });
                  }}
                >
                  {Object.entries(outputs).map(([key, name]) => (
                    <option value={key} key={key}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="e-field">
                Workflow versions
                <textarea
                  placeholder="purchase-review@1"
                  value={editor.references}
                  onChange={(e) =>
                    setEditor({ ...editor, references: e.target.value })
                  }
                />
                <small>
                  One key@version per line. References do not grant access or
                  establish publication/readiness.
                </small>
              </label>
              <ApplicationComposer
                definition={{
                  ...editor.definition,
                  workflows: editorWorkflows,
                }}
                onChange={(application) => {
                  const definition = { ...editor.definition };
                  if (application) definition.application = application;
                  else delete definition.application;
                  setEditor({ ...editor, definition });
                }}
              />
            </fieldset>
            {formError && <Notice error>{formError}</Notice>}
            <div className="dialog-actions">
              <Button disabled={saving} onClick={close}>
                Close
              </Button>
              <Button
                type="submit"
                variant="primary"
                disabled={
                  saving || !session?.permissions.includes("solution:author")
                }
              >
                {saving ? "Saving…" : "Save solution plan"}
              </Button>
            </div>
          </form>
        </DialogPanel>
      )}
      {preview && session && (
        <DialogPanel
          title="Application preview"
          description="Saved application pages with real workflow execution and case permissions. A separate customer installation is still required."
          onClose={() => setPreview(undefined)}
          wide
        >
          <ApplicationPreview key={`${preview.id}:${preview.revision}`} definition={preview.definition} runtime={{plan: preview, session}} />
        </DialogPanel>
      )}
      {publisher && session && (
        <PublisherCandidates
          key={`${publisher.id}:${publisher.revision}`}
          plan={publisher}
          session={session}
          onClose={() => setPublisher(undefined)}
        />
      )}
      {blocker.state === "blocked" && (
        <DialogPanel
          title="Unsaved solution changes"
          description="Leave this page and discard your unsaved plan edits?"
          onClose={() => blocker.reset()}
        >
          <div className="dialog-actions">
            <Button onClick={() => blocker.reset()}>Keep editing</Button>
            <Button
              variant="danger"
              onClick={() => {
                setEditor(undefined);
                blocker.proceed();
              }}
            >
              Discard and leave
            </Button>
          </div>
        </DialogPanel>
      )}
    </div>
  );
}
