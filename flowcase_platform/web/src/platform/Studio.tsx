import { lazy, Suspense, useEffect, useState } from "react";
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Check,
  ChevronRight,
  Code2,
  Focus,
  GitBranch,
  Plus,
  Redo2,
  Save,
  Search,
  Trash2,
  Undo2,
  X,
} from "lucide-react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import {
  NODE_TYPES,
  type Catalog,
  type NodeType,
  type WorkflowDefinition,
} from "../api/workflows";
import { removeNode } from "../features/designer/graph";
import {
  connect,
  disconnect,
  inspectGraph,
  parseDefinition,
  patchNode,
  processEdges,
  type Positions,
} from "./graphModel";
import { labels } from "./nodeLabels";
import { definitionDiff } from "./definitionDiff";
const WorkflowCanvas = lazy(() =>
  import("./WorkflowCanvas").then((m) => ({ default: m.WorkflowCanvas })),
);
function CloseInspector({ onClick }: { onClick: () => void }) {
  return (
    <Button variant="quiet" aria-label="Close inspector" onClick={onClick}>
      <X size={17} />
    </Button>
  );
}

interface Props {
  mode?: "fixture" | "connected";
  versionStatus?: string;
  supportedTypes?: NodeType[];
  catalog?: Catalog | null;
  definition: WorkflowDefinition;
  positions: Positions;
  editable: boolean;
  onChange: (d: WorkflowDefinition) => void;
  onPositions: (p: Positions) => void;
  onSave: () => void;
  onLayout: () => void;
  saving: boolean;
  saved: boolean;
  focus: boolean;
  onFocus: () => void;
}
export function Studio({
  mode = "fixture",
  versionStatus = "draft",
  supportedTypes,
  catalog = null,
  definition,
  positions,
  editable,
  onChange,
  onPositions,
  onSave,
  onLayout,
  saving,
  saved,
  focus,
  onFocus,
}: Props) {
  const [selected, setSelected] = useState<string | null>(null),
    [edge, setEdge] = useState<string | null>(null),
    [palette, setPalette] = useState(false),
    [findNode, setFindNode] = useState(false),
    [inspectorWidth, setInspectorWidth] = useState(310),
    [query, setQuery] = useState(""),
    [modal, setModal] = useState<"import" | "json" | null>(null),
    [text, setText] = useState(""),
    [error, setError] = useState(""),
    [showIssues, setShowIssues] = useState(false),
    [importPreview, setImportPreview] = useState<WorkflowDefinition | null>(
      null,
    ),
    [tab, setTab] = useState("Design");
  type Snapshot = { definition: WorkflowDefinition; positions: Positions };
  const [past, setPast] = useState<Snapshot[]>([]),
    [future, setFuture] = useState<Snapshot[]>([]);
  const issues = inspectGraph(definition, supportedTypes),
    node = definition.nodes.find((n) => n.id === selected),
    selectedEdge = processEdges(definition).find((e) => e.id === edge);
  function change(d: WorkflowDefinition) {
    if (!editable || JSON.stringify(d) === JSON.stringify(definition)) return;
    setPast((p) => [...p.slice(-49), { definition, positions }]);
    setFuture([]);
    onChange(d);
  }
  function move(p: Positions) {
    if (!editable) return;
    setPast((items) => [...items.slice(-49), { definition, positions }]);
    setFuture([]);
    onPositions(p);
  }
  function undo() {
    const prev = past.at(-1);
    if (prev) {
      setPast(past.slice(0, -1));
      setFuture([{ definition, positions }, ...future]);
      onChange(prev.definition);
      onPositions(prev.positions);
    }
  }
  function redo() {
    const next = future[0];
    if (next) {
      setFuture(future.slice(1));
      setPast([...past, { definition, positions }]);
      onChange(next.definition);
      onPositions(next.positions);
    }
  }
  useEffect(() => {
    function key(e: KeyboardEvent) {
      const tag = (e.target as HTMLElement).tagName;
      if (["INPUT", "TEXTAREA", "SELECT"].includes(tag)) return;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z" && editable) {
        e.preventDefault();
        if (e.shiftKey) redo();
        else undo();
      }
      if (e.key === "Escape") {
        setSelected(null);
        setEdge(null);
      }
      if (
        (e.key === "Delete" || e.key === "Backspace") &&
        editable &&
        !palette &&
        !modal
      ) {
        if (node && node.type !== "start") {
          e.preventDefault();
          change(removeNode(definition, node.id));
          setSelected(null);
        } else if (selectedEdge) {
          e.preventDefault();
          change(
            disconnect(definition, selectedEdge.source, selectedEdge.target),
          );
          setEdge(null);
        }
      }
    }
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });
  function exportJSON() {
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(definition, null, 2)], {
        type: "application/json",
      }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `${definition.key}-v${definition.version}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }
  function add(type: NodeType) {
    const id = `${type}-${crypto.randomUUID().slice(0, 8)}`;
    const config: Record<string, unknown> =
      type === "condition"
        ? {
            expression: {
              operator: "eq",
              field: "vendor_matched",
              value: true,
            },
            branches: {},
          }
        : type === "parallel_join"
          ? { join: "all", split_ref: "" }
          : type === "agent_task"
            ? { agent_ref: "", output_artifact_type: "" }
            : type === "human_approval"
              ? { policy_ref: "" }
              : type === "service_task"
                ? { connector_ref: "", operation: "" }
                : type === "timer"
                  ? { duration: "1h" }
                  : type === "human_task"
                    ? { assignee_role: "" }
                    : {};
    change({
      ...definition,
      nodes: [
        ...definition.nodes,
        {
          id,
          type,
          name: `New ${labels[type].toLowerCase()}`,
          config,
          next: [],
        },
      ],
    });
    onPositions({ ...positions, [id]: { x: 240, y: 40 } });
    setPalette(false);
    setSelected(id);
  }
  return (
    <section className="studio">
      <div className="studio-heading">
        <div>
          <span className="eyebrow">WORKFLOW STUDIO</span>
          <h1>{definition.name}</h1>
        </div>
        <div className="studio-status">
          <Badge tone={versionStatus === "published" ? "green" : "amber"}>
            {versionStatus} v{definition.version}
          </Badge>
          <span>
            {saved
              ? mode === "connected"
                ? "Saved on server"
                : "Saved locally"
              : "Unsaved draft"}
          </span>
        </div>
      </div>
      <div className="studio-tabs">
        <div className="tabs">
          {["Design", "Execution trace"].map((t) => (
            <button
              key={t}
              className={tab === t ? "active" : ""}
              onClick={() => setTab(t)}
            >
              {t}
            </button>
          ))}
        </div>
        <span className="subtle">
          {definition.nodes.length} nodes · {processEdges(definition).length}{" "}
          connections
        </span>
      </div>
      {tab === "Execution trace" ? (
        <div className="trace-empty">
          <GitBranch size={32} />
          <h2>No execution connected</h2>
          <p>
            Step status, input/output, evidence and cost will appear here from
            runtime events.
          </p>
          <Notice>
            {mode === "fixture"
              ? "This is a design fixture. No provider calls, payment posting or workflow execution occurs."
              : "No execution has been started here. Runtime event integration is required before displaying step status or cost."}
          </Notice>
          <Button onClick={() => setTab("Design")}>Return to design</Button>
        </div>
      ) : (
        <>
          <div className="studio-toolbar">
            <div className="toolbar-group">
              <Button
                variant="primary"
                disabled={!editable}
                onClick={() => {
                  setQuery("");
                  setPalette(true);
                }}
              >
                <Plus size={15} /> Add node
              </Button>
              <Button
                aria-label="Undo"
                disabled={!editable || !past.length}
                onClick={undo}
              >
                <Undo2 size={16} />
              </Button>
              <Button
                aria-label="Redo"
                disabled={!editable || !future.length}
                onClick={redo}
              >
                <Redo2 size={16} />
              </Button>
              <span className="toolbar-divider" />
              <Button
                aria-label="Find a node"
                onClick={() => {
                  setQuery("");
                  setFindNode(true);
                }}
              >
                <Search size={16} />
              </Button>
              <Button
                onClick={() => {
                  setText(JSON.stringify(definition, null, 2));
                  setModal("json");
                }}
                aria-label="View canonical JSON"
              >
                <Code2 size={16} />
              </Button>
              <Button
                aria-label="Import Flowcase JSON"
                disabled={!editable}
                onClick={() => {
                  setText("");
                  setImportPreview(null);
                  setError("");
                  setModal("import");
                }}
              >
                <ArrowUpFromLine size={16} />
              </Button>
              <Button aria-label="Export Flowcase JSON" onClick={exportJSON}>
                <ArrowDownToLine size={16} />
              </Button>
            </div>
            <div className="toolbar-group">
              <Button onClick={() => setShowIssues((v) => !v)}>
                <Check size={15} />
                {issues.length ? `${issues.length} issues` : "Check graph"}
              </Button>
              <Button
                aria-label={focus ? "Exit focus mode" : "Enter focus mode"}
                onClick={onFocus}
              >
                <Focus size={16} />
              </Button>
              <Button disabled={!editable || saving || saved} onClick={onSave}>
                <Save size={15} />
                {saving ? "Saving…" : "Save draft"}
              </Button>
            </div>
          </div>
          {showIssues && (
            <div className="graph-issues">
              <div>
                <strong>
                  {issues.length
                    ? `${issues.length} preflight issues`
                    : "Topology checks passed"}
                </strong>
                <Button
                  variant="quiet"
                  aria-label="Close diagnostics"
                  onClick={() => setShowIssues(false)}
                >
                  <X size={15} />
                </Button>
              </div>
              <p>
                Local topology checks only. Server validation, binding checks
                and authorization are required before publishing.
              </p>
              {issues.map((i, k) => (
                <button key={k} onClick={() => setSelected(i.node)}>
                  {i.node || "Definition"} <ChevronRight size={12} />
                  {i.message}
                </button>
              ))}
            </div>
          )}
          <div
            className={`studio-workspace ${node || selectedEdge ? "has-inspector" : ""}`}
          >
            <Suspense
              fallback={
                <div className="e-empty" role="status">
                  Loading workflow editor…
                </div>
              }
            >
              <WorkflowCanvas
                definition={definition}
                positions={positions}
                selected={selected}
                invalidIds={issues.map((i) => i.node)}
                editable={editable}
                onChange={change}
                onPositions={move}
                onSelect={(id) => {
                  setSelected(id);
                  setEdge(null);
                }}
                onEdge={(id) => {
                  setEdge(id);
                  setSelected(null);
                }}
                onLayout={onLayout}
                mode={mode}
              />
            </Suspense>
            <div className="mobile-node-list">
              <p>Review topology · canvas editing on desktop</p>
              {definition.nodes.map((n) => (
                <button
                  key={n.id}
                  onClick={() => setSelected(n.id)}
                  className={selected === n.id ? "active" : ""}
                >
                  <span className={`node-glyph ${n.type}`}>
                    <GitBranch size={16} />
                  </span>
                  <div>
                    <strong>{n.name}</strong>
                    <small>
                      {labels[n.type]} ·{" "}
                      {n.next.length ? n.next.join(", ") : "end"}
                    </small>
                  </div>
                  <ChevronRight size={16} />
                </button>
              ))}
            </div>
            {node && (
              <aside
                className="node-inspector"
                aria-label="Node inspector"
                style={{ width: inspectorWidth }}
              >
                <InspectorResizer
                  width={inspectorWidth}
                  onWidth={setInspectorWidth}
                />
                <div className="inspector-head">
                  <span className="eyebrow">NODE CONFIGURATION</span>
                  <CloseInspector onClick={() => setSelected(null)} />
                </div>
                <div className={`inspector-avatar ${node.type}`}>
                  <span className="agent-shape" />
                </div>
                <h2>{node.name}</h2>
                <Badge tone={node.type === "human_approval" ? "amber" : "blue"}>
                  {labels[node.type]}
                </Badge>
                <p className="inspector-intro">
                  {node.type === "agent_task"
                    ? mode === "fixture"
                      ? "Bind an immutable agent version. No model is connected in this fixture."
                      : "Bind an immutable agent version from the registered catalog. Model and release eligibility are governed by Agent Studio."
                    : node.type === "human_approval"
                      ? "An authorized independent reviewer decides. AI cannot approve."
                      : node.type === "condition"
                        ? "A deterministic rule routes to one explicit branch."
                        : "Edit this draft without changing published versions."}
                </p>
                <label className="e-field">
                  Node name
                  <input
                    value={node.name}
                    readOnly={!editable}
                    onChange={(e) =>
                      change(
                        patchNode(definition, node.id, {
                          name: e.target.value,
                        }),
                      )
                    }
                  />
                </label>
                <label className="e-field">
                  Node ID
                  <input value={node.id} readOnly />
                </label>
                {node.type === "condition" &&
                  (["true", "false"] as const).map((b) => (
                    <label className="e-field" key={b}>
                      {b === "true" ? "True · matched" : "False · exception"}
                      <select
                        disabled={!editable}
                        aria-label={`${b} branch target`}
                        value={String(
                          (
                            node.config?.branches as
                              Record<string, string> | undefined
                          )?.[b] ?? "",
                        )}
                        onChange={(e) =>
                          change(
                            connect(definition, node.id, e.target.value, b),
                          )
                        }
                      >
                        <option value="" disabled>
                          Select target
                        </option>
                        {definition.nodes
                          .filter((n) => n.id !== node.id && n.type !== "start")
                          .map((n) => (
                            <option key={n.id} value={n.id}>
                              {n.name}
                            </option>
                          ))}
                      </select>
                    </label>
                  ))}
                {node.type === "agent_task" && (
                  <label className="e-field">
                    Agent version reference
                    <datalist id="registered-agents">
                      {catalog?.agents.map((a) => (
                        <option key={a.ref} value={a.ref}>
                          {a.name} · skill {a.skill_version}
                        </option>
                      ))}
                    </datalist>
                    <input
                      list={
                        mode === "connected" ? "registered-agents" : undefined
                      }
                      value={String(node.config?.agent_ref ?? "")}
                      readOnly={!editable}
                      onChange={(e) =>
                        change(
                          patchNode(definition, node.id, {
                            config: {
                              ...node.config,
                              agent_ref: e.target.value,
                            },
                          }),
                        )
                      }
                    />
                    <small>
                      {mode === "fixture"
                        ? "Fixture references are not executable releases."
                        : "Catalog references must resolve to an approved, version-pinned runtime resource."}
                    </small>
                  </label>
                )}
                {node.type === "human_approval" && (
                  <label className="e-field">
                    Approval policy reference
                    <datalist id="registered-policies">
                      {catalog?.policies.map((p) => (
                        <option key={p.ref} value={p.ref}>
                          {p.name}
                          {p.provisional ? " · provisional" : ""}
                        </option>
                      ))}
                    </datalist>
                    <input
                      list={
                        mode === "connected" ? "registered-policies" : undefined
                      }
                      value={String(node.config?.policy_ref ?? "")}
                      readOnly={!editable}
                      onChange={(e) =>
                        change(
                          patchNode(definition, node.id, {
                            config: {
                              ...node.config,
                              policy_ref: e.target.value,
                            },
                          }),
                        )
                      }
                    />
                  </label>
                )}
                {node.type === "parallel_join" && (
                  <label className="e-field">
                    Wait for all branches from
                    <select
                      disabled={!editable}
                      value={String(node.config?.split_ref ?? "")}
                      onChange={(e) =>
                        change(
                          patchNode(definition, node.id, {
                            config: {
                              ...node.config,
                              split_ref: e.target.value,
                              join: "all",
                            },
                          }),
                        )
                      }
                    >
                      <option value="">Select split</option>
                      {definition.nodes
                        .filter((n) => n.type === "parallel_split")
                        .map((n) => (
                          <option key={n.id} value={n.id}>
                            {n.name}
                          </option>
                        ))}
                    </select>
                  </label>
                )}
                {node.type === "service_task" && (
                  <label className="e-field">
                    Connector reference
                    <datalist id="registered-connectors">
                      {catalog?.connectors.map((c) => (
                        <option key={c.ref} value={c.ref}>
                          {c.name}
                        </option>
                      ))}
                    </datalist>
                    <input
                      list={
                        mode === "connected"
                          ? "registered-connectors"
                          : undefined
                      }
                      value={String(node.config?.connector_ref ?? "")}
                      readOnly={!editable}
                      onChange={(e) =>
                        change(
                          patchNode(definition, node.id, {
                            config: {
                              ...node.config,
                              connector_ref: e.target.value,
                            },
                          }),
                        )
                      }
                    />
                  </label>
                )}
                <Config
                  key={`${node.id}:${JSON.stringify(node.config)}`}
                  config={node.config ?? {}}
                  editable={editable}
                  onApply={(config) =>
                    change(
                      patchNode(definition, node.id, {
                        config,
                        ...(node.type === "condition"
                          ? {
                              next: [
                                ...new Set(
                                  Object.values(
                                    (config.branches as Record<
                                      string,
                                      string
                                    >) ?? {},
                                  ),
                                ),
                              ],
                            }
                          : {}),
                      }),
                    )
                  }
                />
                <div className="inspector-section">
                  <h3>Connections</h3>
                  {processEdges(definition)
                    .filter((e) => e.source === node.id)
                    .map((e) => (
                      <button
                        className="connection-row"
                        key={e.id}
                        onClick={() => {
                          setEdge(e.id);
                          setSelected(null);
                        }}
                      >
                        <span>{e.label || "Next"}</span>
                        <ChevronRight size={13} />
                        <strong>
                          {definition.nodes.find((n) => n.id === e.target)
                            ?.name ?? e.target}
                        </strong>
                      </button>
                    ))}
                  {node.type !== "end" && node.type !== "condition" && (
                    <label className="e-field">
                      Connect to
                      <select
                        disabled={!editable}
                        value=""
                        onChange={(e) =>
                          change(connect(definition, node.id, e.target.value))
                        }
                      >
                        <option value="">Choose next step</option>
                        {definition.nodes
                          .filter((n) => n.type !== "start" && n.id !== node.id)
                          .map((n) => (
                            <option key={n.id} value={n.id}>
                              {n.name}
                            </option>
                          ))}
                      </select>
                    </label>
                  )}
                </div>
                <div className="inspector-section">
                  <h3>AI Economics</h3>
                  <p className="subtle">
                    Unpriced · no approved rate card or measured usage.
                    Estimates and actuals will remain separate.
                  </p>
                </div>
                {issues
                  .filter((i) => i.node === node.id)
                  .map((i, k) => (
                    <Notice error key={k}>
                      {i.message}
                    </Notice>
                  ))}
                <Button
                  variant="danger"
                  disabled={!editable || node.type === "start"}
                  onClick={() => {
                    change(removeNode(definition, node.id));
                    setSelected(null);
                  }}
                >
                  <Trash2 size={15} /> Remove node
                </Button>
              </aside>
            )}
            {selectedEdge && (
              <aside
                className="node-inspector"
                aria-label="Connection inspector"
                style={{ width: inspectorWidth }}
              >
                <InspectorResizer
                  width={inspectorWidth}
                  onWidth={setInspectorWidth}
                />
                <div className="inspector-head">
                  <span className="eyebrow">PROCESS CONNECTION</span>
                  <CloseInspector onClick={() => setEdge(null)} />
                </div>
                <h2>{selectedEdge.label || "Next step"}</h2>
                <p className="subtle">
                  {
                    definition.nodes.find((n) => n.id === selectedEdge.source)
                      ?.name
                  }
                </p>
                <label className="e-field">
                  Target step
                  <select
                    value={selectedEdge.target}
                    disabled={
                      !editable || selectedEdge.label === "true / false"
                    }
                    onChange={(e) => {
                      const base = disconnect(
                        definition,
                        selectedEdge.source,
                        selectedEdge.target,
                      );
                      change(
                        connect(
                          base,
                          selectedEdge.source,
                          e.target.value,
                          selectedEdge.label || undefined,
                        ),
                      );
                      setEdge(`${selectedEdge.source}::${e.target.value}`);
                    }}
                  >
                    {definition.nodes
                      .filter(
                        (n) =>
                          n.type !== "start" && n.id !== selectedEdge.source,
                      )
                      .map((n) => (
                        <option key={n.id} value={n.id}>
                          {n.name}
                        </option>
                      ))}
                  </select>
                </label>
                <Notice>
                  Condition labels are runtime semantics. Edit true/false in the
                  source node; custom labels cannot replace them.
                </Notice>
                <Button
                  variant="danger"
                  disabled={!editable}
                  onClick={() => {
                    change(
                      disconnect(
                        definition,
                        selectedEdge.source,
                        selectedEdge.target,
                      ),
                    );
                    setEdge(null);
                  }}
                >
                  <Trash2 size={15} /> Remove connection
                </Button>
              </aside>
            )}
          </div>
          <footer className="studio-footer">
            <span>
              <span className="canvas-dot" />
              {mode === "fixture"
                ? "Design fixture · drag nodes, connect ports, select to inspect"
                : "Server draft · visual positions are separate from process semantics"}
            </span>
            <span>Publish unavailable · server validation required</span>
          </footer>
        </>
      )}
      {findNode && (
        <DialogPanel
          title="Find a node"
          description="Search the current graph and focus a step without changing its execution order."
          onClose={() => setFindNode(false)}
        >
          <label className="search-input">
            <Search size={17} />
            <input
              autoFocus
              aria-label="Find graph node"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search by name or ID…"
            />
          </label>
          <div className="palette-list">
            {definition.nodes
              .filter((n) =>
                `${n.name} ${n.id}`.toLowerCase().includes(query.toLowerCase()),
              )
              .map((n) => (
                <button
                  key={n.id}
                  onClick={() => {
                    setSelected(n.id);
                    setEdge(null);
                    setFindNode(false);
                  }}
                >
                  <span className={`node-glyph ${n.type}`}>
                    <GitBranch size={17} />
                  </span>
                  <div>
                    <strong>{n.name}</strong>
                    <small>
                      {labels[n.type]} · {n.id}
                    </small>
                  </div>
                  <ChevronRight size={16} />
                </button>
              ))}
          </div>
        </DialogPanel>
      )}
      {palette && (
        <DialogPanel
          title="Add a node"
          description={
            mode === "fixture"
              ? "Fixture palette based on Flowcase schema 1.0. Live runtime capabilities must be checked before execution."
              : "Node availability comes from the connected runtime capability endpoint. Server validation still governs execution."
          }
          onClose={() => setPalette(false)}
        >
          <label className="search-input">
            <Search size={17} />
            <input
              autoFocus
              aria-label="Search node types"
              placeholder="Search nodes…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
          <div className="palette-list">
            {NODE_TYPES.filter(
              (t) =>
                t !== "start" &&
                labels[t].toLowerCase().includes(query.toLowerCase()),
            ).map((t) => (
              <button
                key={t}
                disabled={
                  !editable ||
                  (mode === "fixture"
                    ? t === "subworkflow"
                    : !supportedTypes?.includes(t))
                }
                onClick={() => add(t)}
              >
                <span className={`node-glyph ${t}`}>
                  <GitBranch size={18} />
                </span>
                <div>
                  <strong>{labels[t]}</strong>
                  <small>
                    {(
                      mode === "fixture"
                        ? t === "subworkflow"
                        : !supportedTypes?.includes(t)
                    )
                      ? mode === "fixture"
                        ? "Unavailable in runtime 1.0"
                        : "Unavailable for this runtime"
                      : t === "agent_task"
                        ? "Reason with a versioned agent"
                        : t === "condition"
                          ? "Route true and false outcomes"
                          : t === "human_approval"
                            ? "Require an independent decision"
                            : t.replaceAll("_", " ")}
                  </small>
                </div>
                <Plus size={16} />
              </button>
            ))}
          </div>
        </DialogPanel>
      )}
      {modal && (
        <DialogPanel
          title={
            modal === "import"
              ? "Import Flowcase JSON"
              : "Canonical workflow definition"
          }
          description={
            modal === "import"
              ? "Native schema 1.0 only. Imported content replaces this editor draft after diff review and confirmation; unknown metadata is preserved."
              : "Process semantics only. Visual coordinates are saved separately."
          }
          onClose={() => setModal(null)}
          wide
        >
          <label className="e-field">
            {modal === "import" ? "Definition JSON" : "Read-only JSON"}
            <textarea
              rows={16}
              aria-label="Definition JSON"
              value={text}
              readOnly={modal === "json"}
              onChange={(e) => {
                setText(e.target.value);
                setImportPreview(null);
                setError("");
              }}
            />
          </label>
          {error && <Notice error>{error}</Notice>}
          {modal === "import" && importPreview && (
            <div className="import-preview">
              <h3>Review definition changes</h3>
              <p>
                Added:{" "}
                {definitionDiff(definition, importPreview).added.join(", ") ||
                  "none"}
              </p>
              <p>
                Removed:{" "}
                {definitionDiff(definition, importPreview).removed.join(", ") ||
                  "none"}
              </p>
              <p>
                Changed:{" "}
                {definitionDiff(definition, importPreview).changed.join(", ") ||
                  "none"}
              </p>
              <p>
                Definition fields:{" "}
                {definitionDiff(definition, importPreview).fields.join(", ") ||
                  "unchanged"}
              </p>
              <Notice>
                IDs, next targets, node configuration, input schema and policy
                references are preserved. Visual positions are separate. This
                replaces only the editor draft; it does not save, publish or
                execute.
              </Notice>
              <Button
                variant="primary"
                onClick={() => {
                  change(importPreview);
                  onPositions({});
                  setSelected(null);
                  setModal(null);
                  setImportPreview(null);
                }}
              >
                Confirm replacement
              </Button>
            </div>
          )}
          {modal === "import" && (
            <Button
              variant="primary"
              onClick={() => {
                try {
                  const d = parseDefinition(text);
                  if (
                    mode === "connected" &&
                    (d.key !== definition.key ||
                      d.version !== definition.version)
                  )
                    throw new Error(
                      "Import key and version must match this server draft. Create a new workflow to import another identity.",
                    );
                  setImportPreview(d);
                } catch (e) {
                  setError(
                    e instanceof Error ? e.message : "Invalid definition",
                  );
                }
              }}
            >
              Preview changes
            </Button>
          )}
        </DialogPanel>
      )}
    </section>
  );
}
function InspectorResizer({
  width,
  onWidth,
}: {
  width: number;
  onWidth: (value: number) => void;
}) {
  const clamp = (value: number) => Math.max(260, Math.min(440, value));
  return (
    <div
      className="inspector-resizer"
      role="separator"
      aria-label="Resize inspector"
      aria-orientation="vertical"
      aria-valuemin={260}
      aria-valuemax={440}
      aria-valuenow={width}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
          e.preventDefault();
          onWidth(clamp(width + (e.key === "ArrowLeft" ? 20 : -20)));
        }
      }}
      onPointerDown={(e) => {
        e.preventDefault();
        const start = e.clientX;
        function move(event: PointerEvent) {
          onWidth(clamp(width + start - event.clientX));
        }
        function end() {
          window.removeEventListener("pointermove", move);
        }
        window.addEventListener("pointermove", move);
        window.addEventListener("pointerup", end, { once: true });
      }}
    />
  );
}

function Config({
  config,
  editable,
  onApply,
}: {
  config: Record<string, unknown>;
  editable: boolean;
  onApply: (c: Record<string, unknown>) => void;
}) {
  const [text, setText] = useState(JSON.stringify(config, null, 2)),
    [error, setError] = useState("");
  return (
    <details className="advanced">
      <summary>Advanced configuration</summary>
      <label className="e-field">
        Configuration JSON
        <textarea
          value={text}
          readOnly={!editable}
          onChange={(e) => setText(e.target.value)}
          rows={7}
        />
      </label>
      {error && <Notice error>{error}</Notice>}
      <Button
        disabled={!editable || text === JSON.stringify(config, null, 2)}
        onClick={() => {
          try {
            const c: unknown = JSON.parse(text);
            if (!c || typeof c !== "object" || Array.isArray(c))
              throw new Error("Expected an object.");
            onApply(c as Record<string, unknown>);
            setError("");
          } catch (e) {
            setError(e instanceof Error ? e.message : "Invalid configuration");
          }
        }}
      >
        Apply configuration
      </Button>
    </details>
  );
}
