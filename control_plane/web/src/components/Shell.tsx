import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import type { Me } from "../api/types";
import { toApiError } from "../api/client";
import { useSession } from "../session/context";
import { initials } from "../lib/format";
import { Icon } from "./Icons";
import { Menu } from "./Menu";
import { titleForPath, visibleNavItems } from "./nav";
import { useToast } from "./ui";

export function Sidebar({ me, open, onNavigate }: { me: Me; open: boolean; onNavigate: () => void }) {
  const items = visibleNavItems(me.capabilities);
  const ws = items.filter((i) => i.group === "workspace");
  const admin = items.filter((i) => i.group === "admin");
  const renderItem = (i: (typeof items)[number]) => (
    <li key={i.to}>
      <NavLink to={i.to} end={i.to === "/"} className={({ isActive }) => (isActive ? "active" : undefined)} onClick={onNavigate}>
        <Icon name={i.icon} />
        <span>{i.label}</span>
      </NavLink>
    </li>
  );
  return (
    <aside className={`sidebar${open ? " open" : ""}`} id="sidebar" aria-label="Primary">
      <div className="brand">
        <div className="brandmark" aria-hidden="true">
          <Icon name="layers" />
        </div>
        <div className="brandtext">
          <strong>Control Plane</strong>
          <small>AI WORKSPACE</small>
        </div>
      </div>
      <div className="orgchooser">
        <div className="orgsymbol" aria-hidden="true">
          {initials(me.workspace?.name ?? me.tenant.name)}
        </div>
        <div>
          <strong>{me.workspace?.name ?? "No active workspace"}</strong>
          <small>{me.tenant.name}</small>
        </div>
      </div>
      <nav aria-label="Main navigation">
        <div className="navtitle" id="nav-ws">
          WORKSPACE
        </div>
        <ul className="nav" aria-labelledby="nav-ws">
          {ws.map(renderItem)}
        </ul>
        {admin.length > 0 && (
          <>
            <div className="navtitle" id="nav-admin">
              ADMINISTRATION
            </div>
            <ul className="nav" aria-labelledby="nav-admin">
              {admin.map(renderItem)}
            </ul>
          </>
        )}
      </nav>
      <div className="sidefoot">
        <div className="trustbox">
          <strong>Server-verified access</strong>
          <p>Identity, roles and app entitlements are decided by the control plane on every request. This UI only displays them.</p>
          <div className="trustdot">
            <i aria-hidden="true" /> policy {me.policy_version}
          </div>
        </div>
      </div>
    </aside>
  );
}

function WorkspaceSwitcher() {
  const { me, switchWorkspace } = useSession();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  if (me.workspaces.length === 0) {
    return <span className="topselect static">No workspace</span>;
  }
  return (
    <Menu
      label={`Active workspace: ${me.workspace?.name ?? "none"}. Switch workspace`}
      triggerClassName="topselect"
      trigger={
        <>
          <span className="truncate">{busy ? "Switching…" : (me.workspace?.name ?? "Choose workspace")}</span>
          <Icon name="chevron-down" size="sm" />
        </>
      }
    >
      {(close) =>
        me.workspaces.map((w) => (
          <button
            key={w.id}
            type="button"
            role="menuitemradio"
            aria-checked={me.workspace?.id === w.id}
            className="menuitem"
            onClick={async () => {
              close();
              if (me.workspace?.id === w.id) return;
              setBusy(true);
              try {
                await switchWorkspace(w.id);
                toast(`Switched to ${w.name}`);
              } catch (err) {
                const e = toApiError(err);
                toast(`Could not switch workspace: ${e.code} — ${e.message}${e.trace_id ? ` (trace ${e.trace_id})` : ""}`);
              } finally {
                setBusy(false);
              }
            }}
          >
            <span className="menucheck" aria-hidden="true">
              {me.workspace?.id === w.id && <Icon name="check" size="sm" />}
            </span>
            <span>
              <strong>{w.name}</strong>
              <small>{w.key}</small>
            </span>
          </button>
        ))
      }
    </Menu>
  );
}

function UserMenu() {
  const { me, signOut } = useSession();
  const toast = useToast();
  return (
    <Menu
      label={`Account menu for ${me.user.display_name}`}
      triggerClassName="topselect user"
      trigger={
        <>
          <span className="avatar-xs" aria-hidden="true">
            {initials(me.user.display_name)}
          </span>
          <span className="truncate">{me.user.display_name}</span>
          <Icon name="chevron-down" size="sm" />
        </>
      }
    >
      {() => (
        <>
          <div className="menuheader">
            <strong>{me.user.display_name}</strong>
            <small>{me.user.email ?? "no email on record"}</small>
            <span className={`envbadge ${me.environment}`}>{me.environment}</span>
          </div>
          <button
            type="button"
            role="menuitem"
            className="menuitem"
            onClick={() => {
              signOut().catch((err: unknown) => {
                const e = toApiError(err);
                toast(`Sign out failed: ${e.code} — ${e.message}`);
              });
            }}
          >
            <span className="menucheck" aria-hidden="true">
              <Icon name="logout" size="sm" />
            </span>
            <span>Sign out</span>
          </button>
        </>
      )}
    </Menu>
  );
}

export function Shell() {
  const { me } = useSession();
  const [drawer, setDrawer] = useState(false);
  const location = useLocation();
  const { section, title } = titleForPath(location.pathname);

  useEffect(() => {
    document.title = `${title} · AI Workspace Control Plane`;
  }, [title]);

  useEffect(() => {
    if (!drawer) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setDrawer(false);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawer]);

  return (
    <div className="app">
      <a href="#main" className="skiplink">
        Skip to content
      </a>
      <Sidebar me={me} open={drawer} onNavigate={() => setDrawer(false)} />
      {drawer && <div className="screenveil" onClick={() => setDrawer(false)} aria-hidden="true" />}
      <div className="main">
        {me.environment === "development" && (
          <div className="devbanner" role="note">
            Development deployment — HTTP on tailnet, demo directory; not production
          </div>
        )}
        <header className="topbar">
          <div className="crumbwrap">
            <button
              type="button"
              className="iconbtn mobilemenu"
              aria-label={drawer ? "Close navigation" : "Open navigation"}
              aria-expanded={drawer}
              aria-controls="sidebar"
              onClick={() => setDrawer((d) => !d)}
            >
              <Icon name={drawer ? "x" : "menu"} />
            </button>
            <div className="crumb">
              <span className="parent">{section}</span>
              <Icon name="chevron-right" size="sm" />
              <strong>{title}</strong>
            </div>
          </div>
          <div className="topright">
            <WorkspaceSwitcher />
            <span className={`envbadge ${me.environment} hide-sm`}>{me.environment}</span>
            <UserMenu />
          </div>
        </header>
        <main className="content" id="main" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
    </div>
  );
}
