import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { Icon } from "../components/Icon";
import { initials } from "../components/format";
import { useCaseNav } from "./caseNavContext";
import { useSession } from "./sessionContext";
import {manageDrawerFocus} from "../design/mobileNavigation";

function crumb(pathname: string): string {
  if (pathname.startsWith("/cases/new")) return "New case";
  if (pathname.startsWith("/cases/")) return "Case Workspace";
  if (pathname.startsWith("/packs")) return "Domain Packs";
  return "My Cases";
}

export function SessionEnded() {
  return (
    <div className="session-ended" role="alert">
      <div className="logo-mark"><Icon name="lock" size="lg" /></div>
      <h1>Your session ended</h1>
      <p>Your session ended — reload from the Control Plane. The Case Workspace uses the AI Workspace sign-in; there is no separate login here.</p>
      <a className="btn primary" href="/">Open the Control Plane</a>
    </div>
  );
}

export function Shell({ children }: { children: ReactNode }) {
  const { me, ended } = useSession();
  const { nav } = useCaseNav();
  const location = useLocation();
  // The mobile drawer belongs to the page it was opened on: navigating closes it.
  const [drawerFor, setDrawerFor] = useState<string | null>(null);
  const drawerOpen = drawerFor === location.pathname;
  const setDrawerOpen = useCallback((open: boolean) => setDrawerFor(open ? location.pathname : null), [location.pathname]);
  const menuButton = useRef<HTMLButtonElement>(null);
  const closeDrawer = useCallback(() => setDrawerFor(null), []);
  useEffect(() => {
    if (!drawerOpen) return;
    const drawer=document.getElementById("sidebar");
    if(drawer&&menuButton.current)return manageDrawerFocus(drawer,menuButton.current,closeDrawer);
  }, [drawerOpen, closeDrawer]);

  if (ended) return <SessionEnded />;
  const ctx = me.state === "ready" ? me.data : null;
  const name = ctx?.display_name ?? "Workspace member";
  const casesActive = location.pathname === "/" || location.pathname.startsWith("/cases");

  return (
    <div className="layout">
      <a className="skip-link" href="#main-content">Skip to content</a>
      <aside className={drawerOpen ? "sidebar open" : "sidebar"} id="sidebar" aria-label="Primary">
        <div className="logo">
          <img src={`${import.meta.env.BASE_URL}brand/elenio-logo.png`} alt="Elenio" />
          <div className="logo-desc">BUSINESS WORKSPACE</div>
        </div>
        {ctx && (
          <div className="tenant">
            <div className="tenant-icon" aria-hidden="true">{initials(ctx.workspace_key.replace(/[-_]/g, " "))}</div>
            <div><strong>{ctx.workspace_key}</strong><small>{ctx.tenant_key}</small></div>
          </div>
        )}
        <a className="cp-back" href="/" aria-label="Back to the AI Workspace Control Plane"><span aria-hidden="true">←</span> Control Plane</a>
        <div className="nav-title">WORKSPACE</div>
        <nav className="side-nav" aria-label="Workspace">
          <NavLink to="/" end className={() => (casesActive && !nav ? "active" : "")} aria-current={location.pathname === "/" ? "page" : undefined}>
            <Icon name="briefcase" /><span>My Cases</span>
          </NavLink>
          <NavLink to="/packs" className={({ isActive }) => (isActive ? "active" : "")}><Icon name="blocks" /><span>Domain Packs</span></NavLink>
        </nav>
        {nav && (
          <>
            <div className="nav-title nav-gap">CASE · {nav.reference}</div>
            <nav className="side-nav case-nav" aria-label="Case views">
              {nav.items.map((it) => (
                <Link key={it.id} to={it.to} className={it.id === nav.activeId ? "active" : ""} aria-current={it.id === nav.activeId ? "page" : undefined}>
                  <Icon name={it.icon ?? "dot"} /><span>{it.label}</span>{it.badge ? <em aria-label={`${it.badge} pending`}>{it.badge}</em> : null}
                </Link>
              ))}
            </nav>
            {nav.packTitle && <p className="nav-note">Views generated from {nav.packTitle}</p>}
          </>
        )}
        <div className="sidefooter">
          <div className="provider-box">
            <strong>Your business workspace</strong>
            <p>Continue your assigned cases, tasks and reviews.</p>
          </div>
          <div className="sidebar-user">
            <div className="avatar" aria-hidden="true">{initials(name)}</div>
            <div><b>{name}</b><small>Workspace session</small></div>
          </div>
        </div>
      </aside>
      <div className={drawerOpen ? "mobile-shade open" : "mobile-shade"} onClick={closeDrawer} aria-hidden="true" />
      <main className="main">
        <header className="topbar">
          <div className="top-left">
            <button ref={menuButton} className="mobile-menu" type="button" aria-label="Open navigation" aria-controls="sidebar" aria-expanded={drawerOpen}
              onClick={() => setDrawerOpen(!drawerOpen)}><Icon name="menu" /></button>
            <div className="breadcrumb"><span className="crumb-prefix">AI Workspace <span aria-hidden="true">/</span> Case Workspace <span aria-hidden="true">/</span> </span><strong>{crumb(location.pathname)}</strong></div>
          </div>
          <div className="top-right">
            <div className="round-user" aria-hidden="true">{initials(name)}</div>
          </div>
        </header>
        <div className="content" id="main-content" tabIndex={-1}>{children}</div>
      </main>
    </div>
  );
}
