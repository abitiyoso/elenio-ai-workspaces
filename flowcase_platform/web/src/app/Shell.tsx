import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { basePath } from "../api/client";
import { setPersona } from "../api/hostBridge";
import { Icon } from "../components/Icon";
import { ErrorState, Loading } from "../components/States";
import { initials, useSession } from "../features/session/SessionContext";
import { labelFor, navItems, type NavItem } from "./nav";

function NavGroup({ items, onNavigate }: { items: NavItem[]; onNavigate: () => void }) {
  return (
    <nav className="nav">
      {items.map((item) => (
        <NavLink key={item.path} to={item.path} end={item.path === "/"} onClick={onNavigate}
          className={({ isActive }) => (isActive ? "active" : undefined)}>
          <Icon name={item.icon} />
          <span>{item.label}</span>
        </NavLink>
      ))}
    </nav>
  );
}

export function Shell({ children }: { children: ReactNode }) {
  const { resource, reload } = useSession();
  const location = useLocation();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const sidebar = useRef<HTMLElement>(null);

  const closeDrawer = useCallback(() => {
    if (!drawerOpen) return;
    setDrawerOpen(false);
    menuButton.current?.focus();
  }, [drawerOpen]);

  useEffect(() => {
    if (!drawerOpen) return;
    sidebar.current?.querySelector<HTMLElement>("a, button")?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && closeDrawer();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawerOpen, closeDrawer]);

  const session = resource.state === "ready" ? resource.data : null;
  const name = session?.display_name || "Workspace user";

  let content: ReactNode;
  if (resource.state === "loading") {
    content = <Loading label="Verifying workspace session…" />;
  } else if (resource.state === "error" && resource.error.status === 401) {
    content = (
      <div className="panel panel-pad session-wait">
        <div className="docmark"><Icon name="lock" /></div>
        <h1>Waiting for your workspace session</h1>
        <p>
          Flowcase opens inside the AI Workspace and uses its sign-in. There is no separate login here.
          Re-open Flowcase from the workspace, or retry once the host has shared your session.
        </p>
        <button className="btn primary" type="button" onClick={reload}><Icon name="refresh" size="sm" /> Retry</button>
      </div>
    );
  } else if (resource.state === "error") {
    content = <ErrorState error={resource.error} onRetry={reload} />;
  } else {
    content = children;
  }

  return (
    <div className="app">
      <a className="skip-link" href="#main-content">Skip to content</a>
      <aside className={drawerOpen ? "sidebar open" : "sidebar"} id="sidebar" ref={sidebar} aria-label="Primary">
        {basePath && (
          <a className="cp-back" href="/" aria-label="Back to the AI Workspace Control Plane">
            <span aria-hidden="true">←</span> AI Workspace
          </a>
        )}
        <div className="brand">
          <div className="brandmark"><Icon name="workflow" size="lg" /></div>
          <div>
            <div className="brandname">Flowcase<span className="brand-dot">.</span></div>
            <div className="brandcap">WORKFLOW PLATFORM</div>
          </div>
        </div>
        <div className="workspace">
          <div className="workspace-logo">S</div>
          <div><strong>Solution Workspace</strong><small>Organization space</small></div>
        </div>
        <div className="navcap">WORKSPACE</div>
        <NavGroup items={navItems.filter((n) => n.group === "workspace")} onNavigate={closeDrawer} />
        <div className="navcap navcap-gap">PLATFORM</div>
        <NavGroup items={navItems.filter((n) => n.group === "platform")} onNavigate={closeDrawer} />
        <div className="sidebar-foot">
          <div className="help-box">
            <strong>Reusable by design</strong>
            <p>One engine. Multiple process definitions. Human decisions stay in control.</p>
          </div>
          <div className="profile">
            <div className="avatar" aria-hidden="true">{initials(name)}</div>
            <div>
              <strong>{name}</strong>
              <small>{session?.auth_mode === "dev_mock" ? "Local dev identity" : "Workspace session"}</small>
            </div>
          </div>
        </div>
      </aside>
      <div className={drawerOpen ? "mobile-shade open" : "mobile-shade"} onClick={closeDrawer} aria-hidden="true" />

      <main className="main">
        <header className="topbar">
          <div className="top-left">
            <button ref={menuButton} className="top-btn menu-btn" type="button" aria-label={drawerOpen ? "Close menu" : "Open menu"}
              aria-expanded={drawerOpen} aria-controls="sidebar" onClick={() => setDrawerOpen((o) => !o)}>
              <Icon name="menu" />
            </button>
            <div className="crumb">
              <span className="parentcrumb">Workspace <span className="slash">/</span></span>
              <b>{labelFor(location.pathname)}</b>
            </div>
          </div>
          <div className="top-right">
            <button className="topsearch" type="button" disabled title="Search arrives with the workflow registry">
              <Icon name="search" /><span>Search workflows & cases...</span><kbd>⌘ K</kbd>
            </button>
            <Link className="top-btn" to="/approvals" title="Open approval inbox" aria-label="Open approval inbox"><Icon name="bell" /></Link>
            <div className="profile-pill" aria-label={`Signed in as ${name}`}>{initials(name)}</div>
          </div>
        </header>
        {session?.auth_mode === "dev_mock" && (
          <div className="devbanner" role="note">
            <span>
              <strong>Development identity.</strong> Requests run as a fixed local user ({session.environment}). This mode is refused by the
              server outside development and must never be deployed.
            </span>
            {session.dev_personas && session.dev_personas.length > 1 && (
              <label className="persona-switch">
                Act as
                <select value={session.persona ?? "default"} onChange={(e) => setPersona(e.target.value)} aria-label="Development persona">
                  {session.dev_personas.map((p) => <option key={p.key} value={p.key}>{p.display_name} ({p.roles.join(", ")})</option>)}
                </select>
              </label>
            )}
          </div>
        )}
        <div className="main-content" id="main-content" tabIndex={-1}>{content}</div>
      </main>
    </div>
  );
}
