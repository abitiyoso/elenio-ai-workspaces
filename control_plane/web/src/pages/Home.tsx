import { Link } from "react-router-dom";
import { useSession } from "../session/context";
import { useApi } from "../lib/useApi";
import { api } from "../api/endpoints";
import type { AuditEvent, Page } from "../api/types";
import { fmtDateTime, greeting } from "../lib/format";
import { AppCard } from "../components/AppCard";
import { AuditList } from "../components/AuditList";
import { useLauncher } from "../components/Launcher";
import { Icon } from "../components/Icons";
import { Async, Card, EmptyState, Skeleton } from "../components/ui";

export function HomePage() {
  const { me } = useSession();
  const { launch, busyKey, dialogs } = useLauncher();
  const audit = useApi<Page<AuditEvent>>(me.capabilities.view_audit ? `${api.admin.audit}?limit=8` : null);
  const firstName = me.user.display_name.split(/\s+/)[0] ?? me.user.display_name;
  const hour = new Date().getHours();
  const pendingCount = me.apps.filter((a) => a.integration === "pending").length;

  return (
    <>
      <section className="bento" aria-label="Overview">
        <div className="hero">
          <label>{me.tenant.name.toUpperCase()}</label>
          <h2>
            {greeting(hour)}, {firstName}
          </h2>
          <p>
            {me.workspace ? (
              <>
                You are working in <strong>{me.workspace.name}</strong>. Apps below are the ones the control plane has
                verified you may open in this workspace.
              </>
            ) : (
              <>You have no active workspace. Ask a workspace admin to add you, then pick it from the switcher.</>
            )}
          </p>
          <Link to="/catalog" className="btn primary">
            Browse catalog <Icon name="chevron-right" size="sm" />
          </Link>
        </div>
        <div className="metric accent">
          <div className="metricico">
            <Icon name="grid" />
          </div>
          <small>Apps available</small>
          <strong>{me.apps.length}</strong>
          <p>{pendingCount > 0 ? `${pendingCount} awaiting identity integration` : "in the active workspace"}</p>
        </div>
        <div className="metric">
          <div className="metricico">
            <Icon name="layers" />
          </div>
          <small>Workspaces</small>
          <strong>{me.workspaces.length}</strong>
          <p>{me.permissions.length} effective permissions here</p>
        </div>
      </section>

      <div className="gridmain">
        <div className="stack">
          <Card title="Your apps" subtitle={me.workspace ? `Entitled in ${me.workspace.name}` : "No active workspace"}>
            {me.apps.length === 0 ? (
              <EmptyState title="No apps available" icon="grid">
                No published app is entitled to you in this workspace. Switch workspace or ask an admin for access.
              </EmptyState>
            ) : (
              <div className="appgrid">
                {me.apps.map((a) => (
                  <AppCard key={a.app_key} app={a} onLaunch={launch} busy={busyKey === a.app_key} />
                ))}
              </div>
            )}
          </Card>
          {me.capabilities.view_audit && (
            <Card
              title="Recent activity"
              subtitle="Latest append-only audit events"
              actions={
                <Link className="link" to="/audit">
                  Open audit log
                </Link>
              }
            >
              <Async state={audit} skeleton={<Skeleton lines={4} />} title="Could not load audit events">
                {(p) =>
                  p.items.length === 0 ? <EmptyState title="No audit events yet" /> : <AuditList events={p.items} />
                }
              </Async>
            </Card>
          )}
        </div>
        <div className="stack">
          <Card title="Session">
            <div className="kvlist">
              <div className="kv">
                <span>Signed in as</span>
                <strong>{me.user.email ?? me.user.display_name}</strong>
              </div>
              <div className="kv">
                <span>Active workspace</span>
                <strong>{me.workspace?.name ?? "—"}</strong>
              </div>
              <div className="kv">
                <span>Idle timeout</span>
                <strong>{fmtDateTime(me.session.idle_expires_at)}</strong>
              </div>
              <div className="kv">
                <span>Session expires</span>
                <strong>{fmtDateTime(me.session.expires_at)}</strong>
              </div>
              <div className="kv">
                <span>Policy version</span>
                <strong>{me.policy_version}</strong>
              </div>
            </div>
          </Card>
          <Card title="Trust model">
            <ul className="trustlist">
              <li>
                <Icon name="lock" size="sm" /> Sign-in happens at your identity provider. The browser holds only an
                HttpOnly session cookie — never OAuth tokens.
              </li>
              <li>
                <Icon name="shield" size="sm" /> Every app launch and admin action is authorised by the control plane at
                request time. Hidden buttons are a convenience, not a control.
              </li>
              <li>
                <Icon name="key" size="sm" /> Apps receive short-lived, audience-bound service tokens from the server.
                Apps marked "integration pending" receive nothing until they adopt that contract.
              </li>
              <li>
                <Icon name="scroll" size="sm" /> Admin changes require a reason and are written to an append-only audit
                trail.
              </li>
            </ul>
          </Card>
        </div>
      </div>
      {dialogs}
    </>
  );
}
