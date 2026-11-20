import { Fragment, useEffect, useState } from "react";
import {
  Link,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import {
  ArrowRight,
  Check,
  ChevronRight,
  CircleHelp,
  ExternalLink,
  FileText,
  GitBranch,
  Layers,
  Menu,
  Plus,
  Search,
  ShieldCheck,
  X,
} from "lucide-react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import type { WorkflowDefinition } from "../api/workflows";
import { vendorPayment } from "./fixtures";
import { readDraft, writeDraft } from "./draftStore";
import { type Positions } from "./graphModel";
import { Knowledge } from "./Knowledge";
import { Studio } from "./Studio";
import { layoutDefinition } from "./layout";
import { Launchpad } from "./Launchpad";
import { ArchitectBrief } from "./Architect";
import { ProjectSections } from "./ProjectSections";
import { ConnectedWorkspace } from "./ConnectedWorkspace";
import { Economics } from "./Economics";
import { Solutions } from "./Solutions";
import {CapabilityWorkspace} from "./CapabilityWorkspace";
import {WorkspaceIdentity} from "./WorkspaceIdentity";
import {basePath} from "../api/client";
import {workspaceNavigation,isWorkspaceMenuActive} from "./workspaceNavigation";
import {BusinessApps,Deployments,ControlCenter} from "./GovernedWorkspaces";
import {ConnectedKnowledge} from "./ConnectedKnowledge";

export function Platform() {
  const location = useLocation(),
    navigate = useNavigate();
  const [definition, setDefinition] = useState<WorkflowDefinition>(
      structuredClone(vendorPayment),
    ),
    [positions, setPositions] = useState<Positions>({}),
    [loaded, setLoaded] = useState(false),
    [savedValue, setSavedValue] = useState(() => JSON.stringify({definition:vendorPayment,positions:{}})),
    [saving, setSaving] = useState(false),
    [message, setMessage] = useState(""),
    [error, setError] = useState(""),
    [role, setRole] = useState("Builder"),
    [menu, setMenu] = useState(false),
    [focus, setFocus] = useState(false),
    [command, setCommand] = useState(false),
    [commandQuery, setCommandQuery] = useState(""),
    [help, setHelp] = useState(false),
    [brief, setBrief] = useState("");
  const connectedMode = location.pathname.startsWith("/connected");
  const architectMode = location.pathname === "/architect";
  const solutionMode = location.pathname === "/solutions";
  const capabilityMode = location.pathname.startsWith("/capabilities/");
  const registryMode = ["/business-apps","/deployments","/control-center"].includes(location.pathname)||(["/","/knowledge"].includes(location.pathname)&&basePath==="/apps/flowcase");
  const serviceMode = connectedMode || architectMode || solutionMode || capabilityMode || registryMode;
  const workflow = location.pathname.endsWith("/workflow"),
    editable = role === "Builder",
    value = JSON.stringify({ definition, positions }),
    saved = value === savedValue;
  useEffect(() => {
    let cancelled = false;
    readDraft().then(
      (d) => {
        if (cancelled) return;
        if (d) {
          setDefinition(d.definition);
          setPositions(d.positions);
          setSavedValue(
            JSON.stringify({
              definition: d.definition,
              positions: d.positions,
            }),
          );
        }
        setLoaded(true);
      },
      (e) => {
        if (!cancelled) {
          setError(
            e instanceof Error ? e.message : "Draft storage unavailable",
          );
          setLoaded(true);
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);
  useEffect(() => {
    if (!loaded || !workflow || Object.keys(positions).length) return;
    let cancelled = false;
    layoutDefinition(definition)
      .then((p) => {
        if (!cancelled) setPositions(p);
      })
      .catch(() => {
        if (!cancelled)
          setError(
            "Automatic layout unavailable. Nodes can still be positioned manually.",
          );
      });
    return () => {
      cancelled = true;
    };
  }, [loaded, workflow, definition, positions]);
  useEffect(() => {
    const handle = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setCommand((v) => !v);
      }
    };
    window.addEventListener("keydown", handle);
    return () => window.removeEventListener("keydown", handle);
  }, []);
  useEffect(() => {
    if (!menu) return;
    const nav = document.getElementById("workspace-navigation");
    nav?.querySelector<HTMLElement>("a")?.focus();
    function handle(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setMenu(false);
        document
          .querySelector<HTMLElement>('[aria-label="Open navigation"]')
          ?.focus();
      }
      if (event.key === "Tab" && nav) {
        const items = Array.from(nav.querySelectorAll<HTMLElement>("a,button"));
        const first = items[0],
          last = items.at(-1);
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    }
    window.addEventListener("keydown", handle);
    return () => window.removeEventListener("keydown", handle);
  }, [menu]);
  useEffect(() => {
    if (!loaded || saved || connectedMode) return;
    const handler = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [loaded, saved, connectedMode]);
  async function save() {
    setSaving(true);
    setError("");
    try {
      await writeDraft({
        definition,
        positions,
        savedAt: new Date().toISOString(),
      });
      setSavedValue(value);
      setMessage(
        "Draft saved on this device. No backend version was published.",
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }
  async function layout() {
    try {
      setPositions(await layoutDefinition(definition));
    } catch {
      setError(
        "Could not lay out this graph. Existing positions were retained.",
      );
    }
  }
  function go(path: string) {
    navigate(path);
    setMenu(false);
    setCommand(false);
  }
  const navGroups=workspaceNavigation(connectedMode||basePath==="/apps/flowcase");
  const navItems=navGroups.flatMap(group=>group.items);
  const pageTitle=navItems.find(item=>isWorkspaceMenuActive(item,location.pathname,location.search))?.name;
  const commands = [
    ...navItems.map(({name,path})=>({name,path})),
    { name: "Vendor payment · overview", path: "/projects/vendor-payment" },
    { name: "Design system · component gallery", path: "/design-system" },
  ];
  return (
    <div
      className={`platform-shell ${workflow ? "editor-route" : ""} ${focus && workflow ? "focus-mode" : ""} ${menu ? "nav-open" : ""}`}
    >
      <a
        className="e-skip"
        href="#platform-main"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("platform-main")?.focus();
        }}
      >
        Skip to content
      </a>
      <aside
        className="platform-sidebar"
        aria-label="Workspace navigation"
        id="workspace-navigation"
      >
        <Link
          to={connectedMode ? "/connected" : "/"}
          className="platform-brand"
          onClick={() => setMenu(false)}
        >
          <img src="./brand/elenio-logo.png" alt="Elenio" />
          <span>AI OS</span>
        </Link>
        {basePath==="/apps/flowcase"?<WorkspaceIdentity/>:<button className="workspace-switch" onClick={() => setHelp(true)}>
          <span className="workspace-symbol">E</span>
          <span>
            <strong>Elenio workspace</strong>
            <small>Solution engineering</small>
          </span>
          <ChevronRight size={15} />
        </button>}
        <button
          className="sidebar-search"
          onClick={() => {
            setCommandQuery("");
            setCommand(true);
          }}
        >
          <Search size={16} />
          <span>Quick search</span>
          <kbd>⌘ K</kbd>
        </button>
        <nav aria-label="Main workspace">
          {navGroups.map(group=><Fragment key={group.name}>
            <span className="nav-section">{group.name}</span>
            {group.items.map(item=>{
              const active=isWorkspaceMenuActive(item,location.pathname,location.search),Icon=item.icon;
              return <Link key={item.id} to={item.path} className={active?"active":""} aria-current={active?"page":undefined} onClick={()=>setMenu(false)}><Icon size={18}/>{item.name}</Link>;
            })}
          </Fragment>)}
        </nav>
        <div className="sidebar-bottom">
          <div className="preview-scope">
            <ShieldCheck size={17} />
            <div>
              <strong>Private by design</strong>
              <p>
                {serviceMode
                  ? "Server authorization applies."
                  : "Local design review."}
                <br />
                {serviceMode
                  ? "No release or deployment actions."
                  : "No production connections."}
              </p>
            </div>
          </div>
          <Button variant="quiet" onClick={() => setHelp(true)}>
            <CircleHelp size={17} />
            Workspace guide
          </Button>
          <div className="user-block">
            <span className="user-avatar">
              {connectedMode
                ? "WS"
                : solutionMode || capabilityMode || registryMode
                  ? "CP"
                  : architectMode
                    ? "AI"
                    : "AB"}
            </span>
            <div>
              <strong>
                {connectedMode
                  ? "Workspace user"
                  : registryMode
                    ? "Workspace user"
                  : capabilityMode
                    ? "Capability workspace"
                  : solutionMode
                    ? "Solution workspace"
                    : architectMode
                      ? "AI authoring sandbox"
                      : "Abitiyoso"}
              </strong>
              <small>
                {connectedMode
                  ? "Sign in through workspace"
                  : solutionMode || capabilityMode || registryMode
                    ? "Control Plane session permissions"
                    : architectMode
                      ? "Agent Studio session permissions"
                      : `${role} · fixture identity`}
              </small>
            </div>
          </div>
        </div>
      </aside>
      {menu && (
        <button
          className="nav-backdrop"
          aria-label="Close navigation"
          onClick={() => setMenu(false)}
        />
      )}
      <div className="platform-body">
        <header className="platform-topbar">
          <div>
            <Button
              className="mobile-menu"
              aria-label="Open navigation"
              onClick={() => setMenu((v) => !v)}
            >
              <Menu size={19} />
            </Button>
            <span className="topbar-workspace">Workspace</span>
            <ChevronRight size={14} />
            <span>
              {pageTitle??"Workspace"}
            </span>
          </div>
          <div>
            <Link
              className="mode-switch-link"
              to={connectedMode ? "/" : "/connected"}
              aria-label={
                connectedMode ? "Switch to local preview" : "Connect services"
              }
              title={
                connectedMode ? "Switch to local preview" : "Connect services"
              }
            >
              <ExternalLink size={15} />
              <span className="mode-switch-label">
                {connectedMode ? "Local preview" : "Connect services"}
              </span>
            </Link>
            <Badge tone="amber">
              {serviceMode ? "Service mode" : "Design fixture"}
            </Badge>
            {!serviceMode && (
              <label className="role-label">
                <span className="sr-only">Preview role</span>
                <select
                  aria-label="Preview role"
                  value={role}
                  onChange={(e) => setRole(e.target.value)}
                >
                  <option>Builder</option>
                  <option>Reviewer</option>
                </select>
              </label>
            )}
          </div>
        </header>
        {!workflow && (
          <div className="fixture-bar">
            <span>
              <span className="canvas-dot" />
              {connectedMode
                ? "Service mode · session and permissions required"
                : registryMode
                  ? "Governed workspace · verified session required"
                : capabilityMode
                  ? "Governed capabilities · verified workspace session required"
                : solutionMode
                  ? "Control Plane solution plans · verified workspace session required"
                  : architectMode
                    ? "AI authoring sandbox · verified sessions and model binding required"
                    : "Local preview · sample data · services not connected"}
            </span>
            <button onClick={() => setHelp(true)}>
              What is connected? <ArrowRight size={13} />
            </button>
          </div>
        )}
        {error && (
          <div className="global-message">
            <Notice error>{error}</Notice>
            <Button aria-label="Dismiss error" onClick={() => setError("")}>
              <X size={15} />
            </Button>
          </div>
        )}
        {message && (
          <div className="global-message">
            <Notice>{message}</Notice>
            <Button
              aria-label="Dismiss notification"
              onClick={() => setMessage("")}
            >
              <X size={15} />
            </Button>
          </div>
        )}
        <main id="platform-main" tabIndex={-1}>
          {!loaded && !connectedMode ? (
            <div className="e-empty" role="status">
              Loading local review workspace…
            </div>
          ) : (
            <Routes>
              <Route path="/connected/*" element={<ConnectedWorkspace />} />
              <Route path="/economics" element={<Economics />} />
              <Route path="/solutions" element={<Solutions />} />
              <Route path="/business-apps" element={<BusinessApps />} />
              <Route path="/deployments" element={<Deployments />} />
              <Route path="/control-center" element={<ControlCenter />} />
              <Route path="/capabilities/:capability" element={<CapabilityWorkspace />} />
              <Route
                path="/"
                element={
                  <Launchpad
                    onOpen={() => go(basePath==="/apps/flowcase"?"/connected":"/projects/vendor-payment")}
                    onBuild={() => go(basePath==="/apps/flowcase"?"/connected?create=blank":"/projects/vendor-payment/workflow")}
                    onArchitect={(value) => {
                      setBrief(value);
                      go("/architect");
                    }}
                  />
                }
              />
              <Route
                path="/projects"
                element={
                  <Projects onOpen={() => go("/projects/vendor-payment")} />
                }
              />
              <Route
                path="/projects/vendor-payment"
                element={
                  <Project
                    definition={definition}
                    onOpen={() => go("/projects/vendor-payment/workflow")}
                  />
                }
              />
              <Route
                path="/projects/vendor-payment/workflow"
                element={
                  <Studio
                    definition={definition}
                    positions={positions}
                    editable={editable}
                    onChange={setDefinition}
                    onPositions={setPositions}
                    onSave={() => void save()}
                    onLayout={() => void layout()}
                    saving={saving}
                    saved={saved}
                    focus={focus}
                    onFocus={() => setFocus((v) => !v)}
                  />
                }
              />
              <Route
                path="/architect"
                element={
                  <ArchitectBrief
                    brief={brief}
                    onBrief={setBrief}
                    onBuild={() => go("/projects/vendor-payment/workflow")}
                  />
                }
              />
              <Route path="/knowledge" element={basePath==="/apps/flowcase"?<ConnectedKnowledge/>:<Knowledge />} />
              <Route path="/design-system" element={<Gallery />} />
              <Route
                path="*"
                element={
                  <div className="e-empty">
                    <h1>Page not found</h1>
                    <Button onClick={() => go("/")}>Go home</Button>
                  </div>
                }
              />
            </Routes>
          )}
        </main>
      </div>
      {command && (
        <DialogPanel
          title="Go to…"
          description="Find a page or project. Ctrl/⌘ K opens this menu."
          onClose={() => setCommand(false)}
        >
          <label className="search-input">
            <Search size={17} />
            <input
              autoFocus
              aria-label="Search workspace"
              placeholder="Search pages and projects…"
              value={commandQuery}
              onChange={(e) => setCommandQuery(e.target.value)}
            />
          </label>
          <div className="command-results">
            {commands
              .filter((c) =>
                c.name.toLowerCase().includes(commandQuery.toLowerCase()),
              )
              .map((c) => (
                <button key={`${c.name}:${c.path}`} onClick={() => go(c.path)}>
                  {c.name}
                  <ArrowRight size={15} />
                </button>
              ))}
          </div>
        </DialogPanel>
      )}
      {help && (
        <DialogPanel
          title="Workspace guide"
          description="Local design and connected services have distinct storage and permissions."
          onClose={() => setHelp(false)}
        >
          <div className="help-content">
            <h3>Local design preview</h3>
            <p>
              Home, one project context, branching editor, local draft
              save/import/export, node inspector and synthetic document
              explorer. Reviewer mode makes the graph read-only.
            </p>
            <h3>Connected workflows and operations</h3>
            <p>
              Connect services opens the authenticated Flowcase registry, server
              draft save/validation, cases, tasks, independent approvals and
              execution timelines. Server permissions and immutable versions
              apply.
            </p>
            <h3>AI Architect</h3>
            <p>
              Architect checks the Agent Studio session, approved development
              model and tool-free agent binding. Generation saves a sandbox
              proposal; preview, server validation and explicit confirmation
              create a new workflow draft. Generation does not publish or
              execute the workflow.
            </p>
            <h3>Separate readiness gates</h3>
            <p>
              Knowledge publication, capability releases, external connectors,
              measured Economics and production delivery need their own verified
              configuration and approval. The Economics planner is a scenario,
              and the Knowledge preview uses synthetic documents.
            </p>
            <Notice>
              Local fixture storage contains only this draft and visual
              positions. It is not the authoritative project registry or a
              workflow runtime.
            </Notice>
            <div className="button-row"><a className="e-button" href="./">Existing Flowcase</a><Link className="e-button" to="/design-system" onClick={()=>setHelp(false)}>Design system</Link></div>
          </div>
        </DialogPanel>
      )}
    </div>
  );
}

function Projects({ onOpen }: { onOpen: () => void }) {
  const [query, setQuery] = useState("");
  return (
    <section className="page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">SOLUTION ENGINEERING</span>
          <h1>Projects</h1>
          <p>One context, from discovery to governed operation.</p>
        </div>
        <Button variant="primary" onClick={onOpen}>
          <Plus size={16} /> Open sample project
        </Button>
      </header>
      <label className="search-input project-search">
        <Search size={17} />
        <input
          placeholder="Search projects…"
          aria-label="Search projects"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </label>
      {"vendor payment assurance finance".includes(query.toLowerCase()) ? (
        <button className="recent-project" onClick={onOpen}>
          <span className="project-icon">
            <GitBranch size={24} />
          </span>
          <div>
            <div className="project-name">Vendor payment assurance</div>
            <p>Finance · Synthetic design fixture · draft v1</p>
          </div>
          <Badge tone="blue">Design</Badge>
          <ArrowRight size={18} />
        </button>
      ) : (
        <div className="e-empty">
          <h3>No matching projects</h3>
          <Button onClick={() => setQuery("")}>Clear search</Button>
        </div>
      )}
      <Notice>
        Recruitment is a proposed pilot. SIM Tender is a source-backed domain
        pack. Neither is presented as a startable deployment in this preview.
      </Notice>
    </section>
  );
}

function Project({
  definition,
  onOpen,
}: {
  definition: WorkflowDefinition;
  onOpen: () => void;
}) {
  return (
    <section className="page project-page">
      <div className="breadcrumbs">
        <Link to="/projects">Projects</Link>
        <ChevronRight size={14} />
        <span>Vendor payment</span>
      </div>
      <header className="page-heading">
        <div>
          <span className="eyebrow">FINANCE / SOLUTION DESIGN</span>
          <h1>{definition.name}</h1>
          <p>{definition.description}</p>
        </div>
        <Button variant="primary" onClick={onOpen}>
          Open Workflow Studio <ArrowRight size={16} />
        </Button>
      </header>
      <ProjectSections definition={definition} onWorkflow={onOpen}>
        <div className="lifecycle" aria-label="Project lifecycle">
          {[
            "Discover",
            "Design",
            "Configure",
            "Validate",
            "Release",
            "Operate",
            "Optimize",
          ].map((s, i) => (
            <div
              key={s}
              className={i === 1 ? "current" : i === 0 ? "complete" : ""}
            >
              <span>
                {i === 0 ? <Check size={13} /> : String(i + 1).padStart(2, "0")}
              </span>
              {s}
            </div>
          ))}
        </div>
        <div className="project-overview">
          <div>
            <div className="section-title">
              <h2>Design a safer payment process</h2>
              <Badge tone="amber">Draft v{definition.version}</Badge>
            </div>
            <p className="project-description">
              Keep deterministic verification fast, use agents where reasoning
              helps, and leave payment authority with people.
            </p>
            <div className="workflow-summary">
              <div>
                <FileText size={21} />
                <span>Invoice & PO</span>
              </div>
              <ChevronRight size={16} />
              <div>
                <GitBranch size={21} />
                <span>Parallel checks</span>
              </div>
              <ChevronRight size={16} />
              <div>
                <Layers size={21} />
                <span>Resolve exceptions</span>
              </div>
              <ChevronRight size={16} />
              <div>
                <ShieldCheck size={21} />
                <span>Finance approval</span>
              </div>
            </div>
            <div className="project-detail-row">
              <span>Workflow</span>
              <strong>
                {definition.nodes.length} nodes · explicit true / false ·
                parallel join
              </strong>
              <button onClick={onOpen}>
                Inspect topology <ArrowRight size={14} />
              </button>
            </div>
            <div className="project-detail-row">
              <span>Knowledge</span>
              <strong>Finance policy and vendor verification</strong>
              <Link to="/knowledge">
                Browse sources <ArrowRight size={14} />
              </Link>
            </div>
            <div className="project-detail-row">
              <span>AI Economics</span>
              <strong>Unpriced · no measured usage</strong>
              <Badge>Rate card required</Badge>
            </div>
            <Notice>
              Rejection / revision returns to an authorized review loop. Posting
              must remain blocked until approval is verified by the runtime.
            </Notice>
          </div>
          <aside className="readiness">
            <span className="eyebrow">BEFORE VALIDATION</span>
            <h2>Make dependencies real.</h2>
            <p>
              This design is a fixture. Connect and verify each authority before
              a sandbox run.
            </p>
            {[
              "Approved agent versions",
              "Scoped ERP sandbox connector",
              "Published KMS sources",
              "Independent Finance policy",
              "Server validation & capability probe",
            ].map((s) => (
              <div key={s}>
                <span className="readiness-dot" />
                {s}
                <Badge tone="amber">Pending</Badge>
              </div>
            ))}
            <Button onClick={onOpen}>
              Continue designing <ArrowRight size={15} />
            </Button>
          </aside>
        </div>
      </ProjectSections>
    </section>
  );
}

function Gallery() {
  const [open, setOpen] = useState(false);
  return (
    <section className="page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">SHARED FOUNDATION</span>
          <h1>Design system</h1>
          <p>One component language across the engineering workspace.</p>
        </div>
      </header>
      <div className="gallery-grid">
        <article>
          <h2>Actions</h2>
          <div className="gallery-row">
            <Button variant="primary" onClick={() => setOpen(true)}>
              Primary action
            </Button>
            <Button onClick={() => setOpen(true)}>Secondary</Button>
            <Button disabled title="Requires live session">
              Unavailable
            </Button>
          </div>
          <p>Visible focus, keyboard access and explicit unavailable states.</p>
        </article>
        <article>
          <h2>Status</h2>
          <div className="gallery-row">
            <Badge tone="blue">Draft</Badge>
            <Badge tone="green">Published</Badge>
            <Badge tone="amber">In review</Badge>
            <Badge tone="red">Revoked</Badge>
          </div>
          <p>Labels carry meaning independently from color.</p>
        </article>
        <article>
          <h2>Feedback</h2>
          <Notice>Saved locally. Server validation is still required.</Notice>
          <Notice error>
            Could not connect. Your draft has been retained.
          </Notice>
        </article>
        <article>
          <h2>Semantic color</h2>
          <div className="token-swatches">
            {["ink", "brand", "violet", "teal", "amber", "green"].map((c) => (
              <div key={c}>
                <i style={{ background: `var(--e-${c})` }} />
                {c}
              </div>
            ))}
          </div>
          <p>
            Neutral surfaces, purposeful capability accents and restrained
            gradients.
          </p>
        </article>
      </div>
      {open && (
        <DialogPanel
          title="Accessible dialog"
          description="Keyboard focus stays in this dialog. Escape closes it and restores focus."
          onClose={() => setOpen(false)}
        >
          <label className="e-field">
            Example field
            <input placeholder="Type here" />
          </label>
          <Button variant="primary" onClick={() => setOpen(false)}>
            Done
          </Button>
        </DialogPanel>
      )}
    </section>
  );
}
