import { useEffect, useState } from "react";
import {
  Link,
  Route,
  Routes,
  useNavigate,
  useParams,
  useSearchParams,
  useBlocker,
} from "react-router-dom";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  Plus,
  RefreshCw,
  ShieldCheck,
} from "lucide-react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import { ApiError, newIdempotencyKey } from "../api/client";
import { onTokenChange, setPersona } from "../api/hostBridge";
import { useResource } from "../api/useResource";
import {
  createDraftVersion,
  getVersion,
  saveDraft,
  validateDocument,
  type Catalog,
  type Page,
  type Template,
  type WorkflowDefinition,
  type WorkflowDetail,
  type WorkflowSummary,
  type WorkflowVersion,
  type ValidationReport,
} from "../api/workflows";
import { canDesign } from "../features/session/permissions";
import {
  SessionProvider,
  useSession,
} from "../features/session/SessionContext";
import type { Session } from "../api/types";
import {
  createDefinition,
  canEditVersion,
  unsupportedNodes,
  isSession,
  isRuntimeCapabilities,
  isWorkflowVersion,
  assertSaveResult,
  isValidationReport,
  type RuntimeCapabilities,
} from "./connectedApi";
import { parseDefinition, inspectGraph, type Positions } from "./graphModel";
import { layoutDefinition } from "./layout";
import { Studio } from "./Studio";
import { ConnectedExecution } from "./ConnectedExecution";
import { ConnectedCatalog } from "./ConnectedCatalog";
import { CaseOperations, OperationCase } from "./CaseOperations";
import { readLayout, writeLayout } from "./layoutStore";

function Failure({ error, onRetry }: { error: ApiError; onRetry: () => void }) {
  return (
    <div className="connected-failure">
      <ShieldCheck size={30} />
      <h1>
        {error.status === 401
          ? "Workspace sign-in required"
          : error.status === 403
            ? "Access is not granted"
            : "Service unavailable"}
      </h1>
      <Notice error>{error.message}</Notice>
      <p>
        This mode uses the existing Flowcase session and authorization. It never
        falls back to local examples or browser-supplied tenant roles.
      </p>
      <Button onClick={onRetry}>
        <RefreshCw size={16} />
        Retry connection
      </Button>
      <Link to="/">Return to local design preview</Link>
    </div>
  );
}
function Gate() {
  const { resource, reload } = useSession();
  if (resource.state === "loading")
    return (
      <div className="e-empty" role="status">
        Verifying workspace session…
      </div>
    );
  if (resource.state === "error")
    return <Failure error={resource.error} onRetry={reload} />;
  if (!isSession(resource.data))
    return (
      <Failure
        error={
          new ApiError(
            502,
            "invalid_session",
            "The service returned an invalid session contract.",
          )
        }
        onRetry={reload}
      />
    );
  return (
    <div key={`${resource.data.tenant_id}:${resource.data.user_id}`}>
      <div className="connected-identity">
        <Badge
          tone={resource.data.auth_mode === "dev_mock" ? "amber" : "green"}
        >
          {resource.data.auth_mode === "dev_mock"
            ? "Server development identity"
            : "Verified session"}
        </Badge>
        <span>
          {resource.data.display_name} · {resource.data.environment}
        </span>
        <Link to="/">Local preview</Link>
        {resource.data.auth_mode === "dev_mock" &&
          resource.data.environment === "development" &&
          Array.isArray(resource.data.dev_personas) && (
            <label className="persona-control">
              Development identity
              <select
                aria-label="Server development identity"
                value={resource.data.persona ?? "default"}
                onChange={(e) => setPersona(e.target.value)}
              >
                {resource.data.dev_personas.map((p) => (
                  <option key={p.key} value={p.key}>
                    {p.display_name}
                  </option>
                ))}
              </select>
            </label>
          )}
      </div>
      <Routes>
        <Route index element={<Registry session={resource.data} />} />
        <Route path="catalog" element={<ConnectedCatalog />} />
        <Route
          path="operations"
          element={<CaseOperations session={resource.data} />}
        />
        <Route path="cases/:id" element={<OperationCase />} />
        <Route
          path="projects/:key/:version"
          element={<ConnectedEditor session={resource.data} />}
        />
        <Route
          path="*"
          element={
            <div className="e-empty">
              <h1>Workspace page not found</h1>
              <Link to="/connected">Go to workflow projects</Link>
            </div>
          }
        />
      </Routes>
    </div>
  );
}
export function ConnectedWorkspace() {
  return (
    <SessionProvider>
      <Gate />
    </SessionProvider>
  );
}

function Registry({ session }: { session: Session }) {
  const [intent, setIntent] = useSearchParams();
  const initialMode =
    intent.get("create") === "template"
      ? "template"
      : intent.get("create") === "import"
        ? "import"
        : "blank";
  const [query, setQuery] = useState(""),
    [cursor, setCursor] = useState(""),
    [previous, setPrevious] = useState<string[]>([]);
  const search = new URLSearchParams();
  if (query.trim()) search.set("q", query.trim());
  if (cursor) search.set("cursor", cursor);
  const [list, reload] = useResource<Page<WorkflowSummary>>(
    `/api/v1/workflows${search.size ? `?${search.toString()}` : ""}`,
  );
  const [templates] = useResource<{ items: Template[] }>(
    "/api/v1/workflow-templates",
  );
  const [creating, setCreating] = useState(false);
  const navigate = useNavigate();
  const requestedMode = intent.get("create");
  const showCreate =
    canDesign(session) &&
    (creating || ["blank", "template", "import"].includes(requestedMode ?? ""));
  return (
    <section className="page connected-registry">
      <header className="page-heading">
        <div>
          <span className="eyebrow">CONNECTED ENGINEERING</span>
          <h1>Workflow projects</h1>
          <p>
            Resume a registered solution or create a draft through Flowcase.
          </p>
        </div>
        <Button
          variant="primary"
          disabled={!canDesign(session)}
          title={
            !canDesign(session) ? "Workflow designer role required" : undefined
          }
          onClick={() => setCreating(true)}
        >
          <Plus size={16} />
          New workflow
        </Button>
      </header>
      <div className="connected-registry-nav">
        <Link to="/connected/catalog">
          Browse registered agents, connectors and policies{" "}
          <ArrowRight size={14} />
        </Link>
      </div>
      <Notice>
        Each workspace item maps to one canonical Flowcase workflow.
        Multi-workflow project grouping is not connected yet.
      </Notice>
      <label className="e-field">
        Find a workflow
        <input
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setCursor("");
            setPrevious([]);
          }}
          placeholder="Search name or key…"
        />
      </label>
      {list.state === "loading" ? (
        <div role="status" className="e-empty">
          Loading registered workflows…
        </div>
      ) : list.state === "error" ? (
        <Failure error={list.error} onRetry={reload} />
      ) : (
        <>
          <div className="registered-workflows">
            {list.data.items
              .filter((w) =>
                `${w.name} ${w.key}`
                  .toLowerCase()
                  .includes(query.toLowerCase()),
              )
              .map((w) => (
                <article key={w.key} className="registered-workflow">
                  <div>
                    <h2>{w.name}</h2>
                    <p>{w.description || w.key}</p>
                    <span>
                      {w.node_count} nodes · {w.version_count} versions
                    </span>
                  </div>
                  <div>
                    {w.open_draft_version !== null && (
                      <Button
                        onClick={() =>
                          navigate(
                            `projects/${encodeURIComponent(w.key)}/${w.open_draft_version}`,
                          )
                        }
                      >
                        Open draft v{w.open_draft_version}
                        <ArrowRight size={14} />
                      </Button>
                    )}
                    {w.latest_published_version !== null && (
                      <Button
                        onClick={() =>
                          navigate(
                            `projects/${encodeURIComponent(w.key)}/${w.latest_published_version}`,
                          )
                        }
                      >
                        View published v{w.latest_published_version}
                      </Button>
                    )}
                  </div>
                </article>
              ))}
          </div>
          {!list.data.items.length && (
            <div className="e-empty">
              <h2>No registered workflows</h2>
              <p>
                Create a blank draft or choose an available server template.
              </p>
            </div>
          )}
          <div className="pagination-controls">
            <Button
              disabled={!previous.length}
              onClick={() => {
                setCursor(previous.at(-1) ?? "");
                setPrevious(previous.slice(0, -1));
              }}
            >
              Previous page
            </Button>
            <Button
              disabled={!list.data.next_cursor}
              onClick={() => {
                setPrevious([...previous, cursor]);
                setCursor(list.data.next_cursor ?? "");
              }}
            >
              Next page
            </Button>
          </div>
        </>
      )}
      {showCreate && (
        <CreateWorkflow
          initialMode={initialMode}
          templates={templates.state === "ready" ? templates.data.items : []}
          templatesError={
            templates.state === "error"
              ? templates.error.message
              : templates.state === "loading"
                ? "Loading server templates…"
                : ""
          }
          onClose={() => {
            setCreating(false);
            setIntent({}, { replace: true });
          }}
          onCreated={(v) =>
            navigate(
              `projects/${encodeURIComponent(v.workflow_key)}/${v.version}`,
            )
          }
        />
      )}
    </section>
  );
}

function CreateWorkflow({
  initialMode,
  templates,
  templatesError,
  onClose,
  onCreated,
}: {
  initialMode: string;
  templates: Template[];
  templatesError: string;
  onClose: () => void;
  onCreated: (v: WorkflowVersion) => void;
}) {
  const [mode, setMode] = useState(initialMode),
    [key, setKey] = useState(""),
    [name, setName] = useState(""),
    [template, setTemplate] = useState(""),
    [json, setJson] = useState(""),
    [preview, setPreview] = useState<WorkflowDefinition | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [retryKey, setRetryKey] = useState(newIdempotencyKey());
  function reset() {
    setPreview(null);
    setError("");
    setRetryKey(newIdempotencyKey());
  }
  async function create() {
    setBusy(true);
    setError("");
    try {
      const result = await createDefinition(
        {
          key,
          name,
          ...(mode === "template"
            ? { template }
            : mode === "import" && preview
              ? { definition: preview }
              : {}),
        },
        retryKey,
      );
      onCreated(result.version);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Could not create the workflow.",
      );
    } finally {
      setBusy(false);
    }
  }
  const validKey = /^[a-z][a-z0-9-]{0,119}$/.test(key);
  return (
    <DialogPanel
      title="Create a workflow"
      description="Creates draft v1 in the authorized registry. This does not publish or execute a workflow."
      onClose={() => {
        if (!busy) onClose();
      }}
      wide
    >
      <div className="tabs">
        {["blank", "template", "import"].map((m) => (
          <button
            key={m}
            disabled={busy}
            className={mode === m ? "active" : ""}
            onClick={() => {
              setMode(m);
              reset();
            }}
          >
            {m === "blank"
              ? "Blank canvas"
              : m === "template"
                ? "Server template"
                : "Native JSON"}
          </button>
        ))}
      </div>
      <label className="e-field">
        Workflow name
        <input
          value={name}
          disabled={busy}
          onChange={(e) => {
            setName(e.target.value);
            reset();
          }}
          minLength={3}
        />
      </label>
      <label className="e-field">
        Workflow key
        <input
          value={key}
          disabled={busy}
          onChange={(e) => {
            setKey(e.target.value);
            reset();
          }}
          placeholder="vendor-payments"
        />
        <small>
          Lowercase letters, digits and hyphens; start with a letter.
        </small>
      </label>
      {mode === "template" && (
        <>
          <label className="e-field">
            Registered template
            <select
              value={template}
              disabled={busy}
              onChange={(e) => {
                setTemplate(e.target.value);
                reset();
              }}
            >
              <option value="">Choose a server template</option>
              {templates.map((t) => (
                <option key={`${t.pack}/${t.key}`} value={t.key}>
                  {t.name} · {t.pack}
                </option>
              ))}
            </select>
          </label>
          {templatesError && <Notice>{templatesError}</Notice>}
          {template && (
            <Notice>
              {templates.find((t) => t.key === template)?.description} ·
              Dependencies are validated by the server; a template is not an
              active release.
            </Notice>
          )}
        </>
      )}
      {mode === "import" && (
        <>
          <label className="e-field">
            Native Flowcase definition
            <textarea
              aria-label="New workflow JSON"
              value={json}
              disabled={busy}
              rows={8}
              onChange={(e) => {
                setJson(e.target.value);
                reset();
              }}
            />
          </label>
          <Button
            disabled={busy}
            onClick={() => {
              try {
                setPreview(parseDefinition(json));
                setError("");
              } catch (e) {
                setPreview(null);
                setError(e instanceof Error ? e.message : "Invalid JSON");
              }
            }}
          >
            Preview import
          </Button>
          {preview && (
            <div className="import-preview">
              <h3>{preview.name}</h3>
              <p>
                {preview.nodes.length} nodes · schema {preview.schema_version} ·
                policies, input schema and configuration retained.
              </p>
              <p>
                The server assigns the entered workflow key and draft v1. Source
                IDs, next targets and node config remain in the imported
                definition.
              </p>
              {inspectGraph(preview).map((issue, i) => (
                <Notice key={i} error>
                  {issue.node}: {issue.message}
                </Notice>
              ))}
            </div>
          )}
        </>
      )}
      {error && <Notice error>{error}</Notice>}
      <div className="dialog-footer">
        <Button disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="primary"
          disabled={
            busy ||
            !validKey ||
            name.trim().length < 3 ||
            (mode === "template" && !template) ||
            (mode === "import" && !preview)
          }
          onClick={() => void create()}
        >
          {busy ? "Creating…" : "Create draft"}
        </Button>
      </div>
    </DialogPanel>
  );
}

function ConnectedEditor({ session }: { session: Session }) {
  const { key = "", version = "1" } = useParams();
  const navigate = useNavigate();
  if (!Number.isInteger(Number(version)) || Number(version) < 1)
    return (
      <Failure
        error={
          new ApiError(
            400,
            "invalid_version",
            "Version must be a positive integer.",
          )
        }
        onRetry={() => navigate("/connected")}
      />
    );
  return (
    <EditorDocument
      key={`${session.tenant_id}:${session.user_id}:${key}:${version}`}
      workflowKey={key}
      versionNo={Number(version)}
      session={session}
    />
  );
}
function EditorDocument({
  workflowKey,
  versionNo,
  session,
}: {
  workflowKey: string;
  versionNo: number;
  session: Session;
}) {
  const navigate = useNavigate();
  const [loaded, setLoaded] = useState<WorkflowVersion | null>(null),
    [document, setDocument] = useState<WorkflowDefinition | null>(null),
    [positions, setPositions] = useState<Positions>({}),
    [error, setError] = useState<ApiError | null>(null),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(""),
    [conflict, setConflict] = useState(false),
    [focus, setFocus] = useState(false),
    [report, setReport] = useState<ValidationReport | null>(null),
    [reload, setReload] = useState(0),
    [importOpen, setImportOpen] = useState(false),
    [layoutReady, setLayoutReady] = useState(false);
  const [capabilities, refreshCapabilities] = useResource<RuntimeCapabilities>(
    "/api/v1/runtime-capabilities",
  );
  const [catalog] = useResource<Catalog>("/api/v1/workflow-catalog");
  const [detail] = useResource<WorkflowDetail>(
    `/api/v1/workflows/${encodeURIComponent(workflowKey)}`,
  );
  useEffect(() => {
    const ctrl = new AbortController();
    getVersion(workflowKey, versionNo, ctrl.signal).then(
      (v) => {
        if (ctrl.signal.aborted) return;
        if (
          !isWorkflowVersion(v) ||
          v.workflow_key !== workflowKey ||
          v.version !== versionNo
        ) {
          setError(
            new ApiError(
              502,
              "invalid_version_contract",
              "The service returned an invalid workflow version.",
            ),
          );
          return;
        }
        setLoaded(v);
        setDocument(structuredClone(v.definition));
        setError(null);
        setConflict(false);
        setReport(null);
        setNotice("");
        const scope = {
          tenant: session.tenant_id,
          user: session.user_id,
          versionID: v.id,
        };
        void Promise.all([
          layoutDefinition(v.definition),
          readLayout(scope).catch(() => ({})),
        ])
          .then(([automatic, persisted]) => {
            if (!ctrl.signal.aborted) {
              setPositions({ ...automatic, ...persisted });
              setLayoutReady(true);
            }
          })
          .catch(() => {
            if (!ctrl.signal.aborted) {
              setNotice(
                "Automatic layout unavailable; use manual node positioning.",
              );
              setLayoutReady(true);
            }
          });
      },
      (e) => {
        if (!ctrl.signal.aborted)
          setError(
            e instanceof ApiError
              ? e
              : new ApiError(0, "unknown", "Could not load the workflow."),
          );
      },
    );
    return () => ctrl.abort();
  }, [workflowKey, versionNo, reload, session.tenant_id, session.user_id]);
  useEffect(
    () =>
      onTokenChange(() => {
        setLoaded(null);
        setDocument(null);
        setReport(null);
        setNotice("");
        setConflict(false);
        setReload((n) => n + 1);
      }),
    [],
  );
  const capabilitiesReady =
    capabilities.state === "ready" && isRuntimeCapabilities(capabilities.data);
  const semanticDirty =
    !!document &&
    !!loaded &&
    JSON.stringify(document) !== JSON.stringify(loaded.definition);
  const editable =
    !!loaded &&
    canEditVersion(loaded.status, canDesign(session)) &&
    capabilitiesReady &&
    !conflict &&
    !busy;
  useEffect(() => {
    if (!semanticDirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [semanticDirty]);
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      semanticDirty &&
      (currentLocation.pathname !== nextLocation.pathname ||
        currentLocation.search !== nextLocation.search),
  );

  async function run(kind: string, fn: () => Promise<void>) {
    setBusy(kind);
    setError(null);
    setNotice("");
    try {
      await fn();
    } catch (e) {
      const err =
        e instanceof ApiError
          ? e
          : new ApiError(
              0,
              "unknown",
              "The action failed. Your edits are retained.",
            );
      setError(err);
      if (err.status === 412) setConflict(true);
    } finally {
      setBusy("");
    }
  }
  if (error && !loaded)
    return <Failure error={error} onRetry={() => setReload((n) => n + 1)} />;
  if (!loaded || !document || !layoutReady)
    return (
      <div className="e-empty" role="status">
        Loading workflow version…
      </div>
    );
  const unsupported =
    capabilitiesReady && capabilities.state === "ready"
      ? unsupportedNodes(document, capabilities.data)
      : [];
  return (
    <div className={focus ? "connected-editor focused" : "connected-editor"}>
      <div className="connected-editor-context">
        <Link to="/connected">
          <ArrowLeft size={15} />
          Workflow projects
        </Link>
        <span>
          {workflowKey} · v{loaded.version}
        </span>
        <Badge tone={loaded.status === "published" ? "green" : "amber"}>
          {loaded.status}
        </Badge>
        <select
          aria-label="Server workflow version"
          value={loaded.version}
          disabled={!!busy || semanticDirty}
          onChange={(e) =>
            navigate(
              `/connected/projects/${encodeURIComponent(workflowKey)}/${e.target.value}`,
            )
          }
        >
          {detail.state === "ready" ? (
            detail.data.versions.map((v) => (
              <option key={v.id} value={v.version}>
                v{v.version} · {v.status}
              </option>
            ))
          ) : (
            <option value={loaded.version}>v{loaded.version}</option>
          )}
        </select>
        <Button
          disabled={!editable || !!busy}
          onClick={() =>
            void run("validate", async () => {
              const result = await validateDocument(document);
              if (!isValidationReport(result))
                throw new Error("Invalid validation response.");
              setReport(result);
              setNotice(
                result.valid
                  ? "Server definition validation passed. This does not publish or execute the workflow."
                  : "",
              );
            })
          }
        >
          <Check size={15} />
          {busy === "validate" ? "Validating…" : "Validate with server"}
        </Button>
      </div>
      {capabilities.state === "loading" && (
        <Notice>
          Checking runtime node support. Editing is locked until capabilities
          are known.
        </Notice>
      )}
      {(capabilities.state === "error" ||
        (capabilities.state === "ready" && !capabilitiesReady)) && (
        <Notice error>
          Runtime capabilities unavailable. Editing is locked.{" "}
          <Button onClick={refreshCapabilities}>Retry capabilities</Button>
        </Notice>
      )}
      {error && (
        <Notice error>
          {error.message}
          {error.traceId && ` · Request ${error.traceId}`}
        </Notice>
      )}
      {notice && <Notice>{notice}</Notice>}
      {conflict && (
        <Notice error>
          This draft changed elsewhere. Your edits remain visible and can be
          exported.{" "}
          <Button onClick={() => setImportOpen(true)}>Review reload</Button>
        </Notice>
      )}
      {unsupported.length > 0 && (
        <Notice error>
          Runtime does not support nodes: {unsupported.join(", ")}. These nodes
          cannot be executed; server validation remains authoritative.
        </Notice>
      )}
      {!canEditVersion(loaded.status, canDesign(session)) && (
        <Notice>
          {loaded.status === "published" || loaded.status === "deprecated"
            ? "This version is immutable. Create or open a draft to make changes."
            : "Workflow designer role is required to edit."}
          {canDesign(session) &&
            !["draft", "validated"].includes(loaded.status) &&
            detail.state === "ready" &&
            (detail.data.open_draft_version !== null ? (
              <Button
                onClick={() =>
                  navigate(
                    `/connected/projects/${encodeURIComponent(workflowKey)}/${detail.data.open_draft_version}`,
                  )
                }
              >
                Open existing draft
              </Button>
            ) : (
              <Button
                disabled={!!busy}
                onClick={() =>
                  void run("fork", async () => {
                    const result = await createDraftVersion(
                      workflowKey,
                      loaded.version,
                    );
                    assertSaveResult(result);
                    navigate(
                      `/connected/projects/${encodeURIComponent(workflowKey)}/${result.version.version}`,
                    );
                  })
                }
              >
                Create draft from v{loaded.version}
              </Button>
            ))}
        </Notice>
      )}

      {report && (
        <details className="server-diagnostics">
          <summary>
            <strong>
              {report.valid
                ? "Server validation passed"
                : `${report.diagnostics.filter((d) => d.severity === "error").length} server errors`}
            </strong>
          </summary>
          {report.diagnostics.map((d, i) => (
            <p key={i}>
              {d.node_id || d.path}: {d.message}
            </p>
          ))}
        </details>
      )}
      <Studio
        executionTrace={
          <ConnectedExecution
            workflowKey={workflowKey}
            version={loaded.version}
          />
        }
        definition={document}
        positions={positions}
        editable={editable}
        onChange={(d) => {
          setDocument(d);
          setReport(null);
        }}
        onPositions={(p) => {
          setPositions(p);
          void writeLayout(
            {
              tenant: session.tenant_id,
              user: session.user_id,
              versionID: loaded.id,
            },
            p,
          ).catch(() =>
            setNotice(
              "Visual positions could not be saved on this device. The process definition is unaffected.",
            ),
          );
        }}
        onSave={() =>
          void run("save", async () => {
            const result = await saveDraft(
              workflowKey,
              loaded.version,
              loaded.version_num,
              document,
            );
            assertSaveResult(result);
            if (
              result.version.workflow_key !== workflowKey ||
              result.version.version !== loaded.version
            )
              throw new Error(
                "The saved version does not match the editor context.",
              );
            setLoaded(result.version);
            setDocument(structuredClone(result.version.definition));
            setReport(result.validation);
            setNotice("Draft saved by Flowcase. No version was published.");
          })
        }
        onLayout={() =>
          void layoutDefinition(document)
            .then(setPositions)
            .catch(() => setNotice("Layout failed; positions were retained."))
        }
        saving={busy === "save"}
        saved={!semanticDirty}
        focus={focus}
        onFocus={() => setFocus((v) => !v)}
        mode="connected"
        versionStatus={loaded.status}
        supportedTypes={
          capabilitiesReady && capabilities.state === "ready"
            ? capabilities.data.supported_node_types
            : []
        }
        catalog={
          catalog.state === "ready" &&
          catalog.data &&
          Array.isArray(catalog.data.agents) &&
          Array.isArray(catalog.data.policies) &&
          Array.isArray(catalog.data.connectors)
            ? catalog.data
            : null
        }
      />
      {blocker.state === "blocked" && (
        <DialogPanel
          title="Leave unsaved changes?"
          description="The server draft has not been updated. Stay to save or export your work, or discard this editor copy."
          onClose={() => blocker.reset()}
        >
          <Button onClick={() => blocker.reset()}>Stay in editor</Button>
          <Button variant="danger" onClick={() => blocker.proceed()}>
            Discard edits and leave
          </Button>
        </DialogPanel>
      )}
      {importOpen && (
        <DialogPanel
          title="Reload the latest server draft?"
          description="Reload discards the unsaved editor document. Export JSON first if you need to keep your edits."
          onClose={() => setImportOpen(false)}
        >
          <Button onClick={() => setImportOpen(false)}>Keep my edits</Button>
          <Button
            variant="danger"
            onClick={() => {
              setImportOpen(false);
              setLoaded(null);
              setDocument(null);
              setReload((n) => n + 1);
            }}
          >
            Discard edits and reload
          </Button>
        </DialogPanel>
      )}
    </div>
  );
}
