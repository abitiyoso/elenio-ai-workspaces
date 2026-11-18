import { useEffect, useState } from "react";
import {
  Link,
  NavLink,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import {
  ArrowRight,
  BookOpen,
  Boxes,
  Check,
  ChevronRight,
  CircleHelp,
  ExternalLink,
  FileText,
  FolderOpen,
  GitBranch,
  Home,
  Layers,
  Menu,
  Plus,
  Search,
  ShieldCheck,
  Sparkles,
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

const agents = [
  {
    name: "Document intelligence",
    role: "Extract structured invoice fields and source evidence.",
    shape: "document",
    tag: "Evidence",
  },
  {
    name: "Exception analyst",
    role: "Investigate mismatches using scoped policy knowledge.",
    shape: "exception",
    tag: "Reasoning",
  },
  {
    name: "Finance reviewer",
    role: "A human makes the independent payment decision.",
    shape: "reviewer",
    tag: "Human authority",
  },
];

export function Platform() {
  const location = useLocation(),
    navigate = useNavigate();
  const [definition, setDefinition] = useState<WorkflowDefinition>(
      structuredClone(vendorPayment),
    ),
    [positions, setPositions] = useState<Positions>({}),
    [loaded, setLoaded] = useState(false),
    [savedValue, setSavedValue] = useState(""),
    [saving, setSaving] = useState(false),
    [message, setMessage] = useState(""),
    [error, setError] = useState(""),
    [role, setRole] = useState("Builder"),
    [menu, setMenu] = useState(false),
    [focus, setFocus] = useState(false),
    [command, setCommand] = useState(false),
    [commandQuery, setCommandQuery] = useState(""),
    [help, setHelp] = useState(false);
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
    if (!loaded || saved) return;
    const handler = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [loaded, saved]);
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
  const commands = [
    { name: "Home", path: "/" },
    { name: "Projects", path: "/projects" },
    { name: "Vendor payment · overview", path: "/projects/vendor-payment" },
    { name: "Workflow Studio", path: "/projects/vendor-payment/workflow" },
    { name: "Knowledge Hub", path: "/knowledge" },
    { name: "Design system · component gallery", path: "/design-system" },
  ];
  return (
    <div
      className={`platform-shell ${focus && workflow ? "focus-mode" : ""} ${menu ? "nav-open" : ""}`}
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
        <Link to="/" className="platform-brand" onClick={() => setMenu(false)}>
          <img src="./brand/elenio-logo.png" alt="Elenio" />
          <span>AI WORKSPACES</span>
        </Link>
        <button className="workspace-switch" onClick={() => setHelp(true)}>
          <span className="workspace-symbol">E</span>
          <span>
            <strong>Elenio workspace</strong>
            <small>Solution engineering</small>
          </span>
          <ChevronRight size={15} />
        </button>
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
        <nav>
          <NavLink end to="/" onClick={() => setMenu(false)}>
            <Home size={18} />
            Home
          </NavLink>
          <NavLink to="/projects" onClick={() => setMenu(false)}>
            <FolderOpen size={18} />
            Projects
          </NavLink>
          <span className="nav-section">CAPABILITY LIBRARY</span>
          <NavLink to="/knowledge" onClick={() => setMenu(false)}>
            <BookOpen size={18} />
            Knowledge
          </NavLink>
          <a href="./">
            <GitBranch size={18} />
            Existing Flowcase
            <ExternalLink size={12} />
          </a>
          <NavLink to="/design-system" onClick={() => setMenu(false)}>
            <Boxes size={18} />
            Design system
          </NavLink>
        </nav>
        <div className="sidebar-bottom">
          <div className="preview-scope">
            <ShieldCheck size={17} />
            <div>
              <strong>Private by design</strong>
              <p>
                Local design review.
                <br />
                No production connections.
              </p>
            </div>
          </div>
          <Button variant="quiet" onClick={() => setHelp(true)}>
            <CircleHelp size={17} />
            Workspace guide
          </Button>
          <div className="user-block">
            <span className="user-avatar">AB</span>
            <div>
              <strong>Abitiyoso</strong>
              <small>{role} · fixture identity</small>
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
              {workflow
                ? "Workflow Studio"
                : location.pathname.includes("projects")
                  ? "Projects"
                  : location.pathname.includes("knowledge")
                    ? "Knowledge"
                    : "Overview"}
            </span>
          </div>
          <div>
            <Badge tone="amber">Design fixture</Badge>
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
          </div>
        </header>
        <div className="fixture-bar">
          <span>
            <span className="canvas-dot" />
            Phase 1 preview · synthetic data · no live AI or runtime
          </span>
          <button onClick={() => setHelp(true)}>
            What is connected? <ArrowRight size={13} />
          </button>
        </div>
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
          {!loaded ? (
            <div className="e-empty" role="status">
              Loading local review workspace…
            </div>
          ) : (
            <Routes>
              <Route
                path="/"
                element={
                  <HomePage
                    onOpen={() => go("/projects/vendor-payment")}
                    onBuild={() => go("/projects/vendor-payment/workflow")}
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
              <Route path="/knowledge" element={<Knowledge />} />
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
                <button key={c.path} onClick={() => go(c.path)}>
                  {c.name}
                  <ArrowRight size={15} />
                </button>
              ))}
          </div>
        </DialogPanel>
      )}
      {help && (
        <DialogPanel
          title="Your Phase 1 review workspace"
          description="A functional frontend slice with an explicit fixture boundary."
          onClose={() => setHelp(false)}
        >
          <div className="help-content">
            <h3>Available for review</h3>
            <p>
              Home, one project context, branching editor, local draft
              save/import/export, node inspector and synthetic document
              explorer. Reviewer mode makes the graph read-only.
            </p>
            <h3>Requires live integration</h3>
            <p>
              Authentication, server draft validation/save, model calls,
              published knowledge retrieval, independent approvals, execution
              events and measured economics. The existing authenticated Flowcase
              application remains available from the sidebar.
            </p>
            <Notice>
              Local fixture storage contains only this draft and visual
              positions. It is not the authoritative project registry or a
              workflow runtime.
            </Notice>
          </div>
        </DialogPanel>
      )}
    </div>
  );
}

function HomePage({
  onOpen,
  onBuild,
}: {
  onOpen: () => void;
  onBuild: () => void;
}) {
  const [agent, setAgent] = useState(0),
    [brief, setBrief] = useState(""),
    [architect, setArchitect] = useState(false);
  return (
    <section className="page home-page">
      <div className="home-intro">
        <span className="eyebrow">YOUR ENGINEERING WORKSPACE</span>
        <h1>
          Turn business intent
          <br />
          into intelligent systems.
        </h1>
        <p>
          Design with clarity. Orchestrate with control.
          <br />
          Build something your business can trust.
        </p>
      </div>
      <div className="launch-composition">
        <div className="brief-panel">
          <div className="brief-title">
            <Sparkles size={18} />
            <strong>What would you like to build?</strong>
            <Badge>AI Architect</Badge>
          </div>
          <textarea
            aria-label="Solution brief"
            placeholder="Describe a process, its decisions and what success looks like…"
            value={brief}
            onChange={(e) => setBrief(e.target.value)}
          />
          <div className="brief-actions">
            <span>Start with a goal, SOP or business challenge</span>
            <Button variant="primary" onClick={() => setArchitect(true)}>
              <ArrowRight size={17} /> Explore a solution
            </Button>
          </div>
          <div className="manual-entry">
            <span>Prefer to start hands-on?</span>
            <button onClick={onBuild}>
              Open sample canvas <ArrowRight size={14} />
            </button>
          </div>
        </div>
        <div className="intelligence-map">
          <div className="map-heading">
            <span className="eyebrow">INTELLIGENCE, CONNECTED</span>
            <span>Explore roles</span>
          </div>
          <div className="agent-network">
            <svg viewBox="0 0 420 200" aria-hidden="true">
              <path d="M95 90 C145 90 160 50 215 65 S280 135 335 95" />
              <path d="M95 90 C180 190 265 185 335 95" />
            </svg>
            {agents.map((a, i) => (
              <button
                key={a.name}
                className={`agent-orbit orbit-${i} ${agent === i ? "selected" : ""}`}
                aria-label={`Inspect ${a.name}`}
                aria-pressed={agent === i}
                onClick={() => setAgent(i)}
              >
                <span className={`agent-object ${a.shape}`}>
                  <i />
                  <i />
                  <i />
                </span>
                <span>{a.name}</span>
              </button>
            ))}
          </div>
          <div className="agent-summary">
            <Badge tone="blue">{agents[agent]?.tag}</Badge>
            <p>{agents[agent]?.role}</p>
          </div>
        </div>
      </div>
      <div className="section-title">
        <h2>Continue building</h2>
        <Link to="/projects">
          All projects <ArrowRight size={14} />
        </Link>
      </div>
      <button className="recent-project" onClick={onOpen}>
        <span className="project-icon">
          <GitBranch size={24} />
        </span>
        <div>
          <div className="project-name">
            Vendor payment assurance <Badge tone="amber">Draft</Badge>
          </div>
          <p>Finance · Evidence, exceptions and independent approval</p>
        </div>
        <div className="project-progress">
          <span>Design</span>
          <div>
            <i />
            <i />
            <i className="pending" />
            <i className="pending" />
            <i className="pending" />
          </div>
        </div>
        <ArrowRight size={18} />
      </button>
      <div className="section-title">
        <h2>Build from a clear starting point</h2>
        <span>Synthetic examples · no active deployments</span>
      </div>
      <div className="starting-points">
        <button onClick={onBuild}>
          <span className="template-number">01</span>
          <div>
            <h3>Vendor payment</h3>
            <p>Parallel checks, conditional routing and a human decision.</p>
          </div>
          <ArrowRight size={17} />
        </button>
        <Link to="/knowledge">
          <span className="template-number">02</span>
          <div>
            <h3>Explore trusted knowledge</h3>
            <p>Browse source versions, publication status and evidence.</p>
          </div>
          <ArrowRight size={17} />
        </Link>
      </div>
      {architect && (
        <DialogPanel
          title="AI Architect needs a model connection"
          description="Your brief is preserved here. No AI output has been generated."
          onClose={() => setArchitect(false)}
        >
          <Notice>
            {brief ||
              "Describe the process, intended outcome and decision authority before creating a blueprint."}
          </Notice>
          <div className="help-content">
            <h3>Clarify before building</h3>
            <p>
              Which input starts the process? Who approves irreversible actions?
              What data must stay private? What baseline cost and quality define
              success?
            </p>
            <p>
              A configured, approved model and authorized draft API are required
              for proposal generation and diff-confirm apply.
            </p>
          </div>
          <Button
            onClick={() => {
              setArchitect(false);
              onBuild();
            }}
          >
            Explore the labelled sample instead
          </Button>
        </DialogPanel>
      )}
    </section>
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
              {definition.nodes.length} nodes · explicit true / false · parallel
              join
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
            This design is a fixture. Connect and verify each authority before a
            sandbox run.
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
