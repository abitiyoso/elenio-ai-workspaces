import { Link } from "react-router-dom";
import type { Approval, CaseSummary, Task } from "../api/cases";
import type { Integrations } from "../api/types";
import type { AuditEvent, Page, WorkflowSummary } from "../api/workflows";
import { formatDate } from "../components/format";
import { canReadAudit } from "../features/session/permissions";
import { describe } from "../features/audit/describe";
import { useResource } from "../api/useResource";
import { Icon } from "../components/Icon";
import { EmptyState, ErrorState, Loading, PageHead } from "../components/States";
import { useCurrentSession, useSession } from "../features/session/SessionContext";

function greeting(now: Date): string {
  const h = now.getHours();
  return h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}

function PlatformStatus() {
  const [resource, reload] = useResource<Integrations>("/api/v1/integrations");
  if (resource.state === "loading") return <Loading label="Checking platform status…" />;
  if (resource.state === "error") return <ErrorState error={resource.error} onRetry={reload} />;
  const rows: [string, keyof Integrations][] = [["Case runtime", "case_runtime"], ["Host auth", "host_auth"], ["KMS", "knowledge"], ["AI runtime", "agent_runtime"]];
  return (
    <div className="queue">
      {rows.map(([label, key]) => {
        const s = resource.data[key];
        return (
          <div className="queue-item" key={key}>
            <div>
              <strong>{label}</strong>
              <p>{s.mode} · {s.status}</p>
            </div>
            <span className={s.status === "ok" ? "pill healthy" : s.status === "unavailable" ? "pill failed" : "pill draft"}>{s.status}</span>
          </div>
        );
      })}
    </div>
  );
}

function RecentActivity() {
  const [resource, reload] = useResource<Page<AuditEvent>>("/api/v1/audit-events?limit=5");
  if (resource.state === "loading") return <Loading label="Loading activity…" />;
  if (resource.state === "error") return <ErrorState error={resource.error} onRetry={reload} />;
  if (resource.data.items.length === 0) return <EmptyState icon="clock" title="No activity recorded">Server-side audit events will appear here.</EmptyState>;
  return (
    <div className="activity-list">
      {resource.data.items.map((e) => (
        <div className="activity" key={e.id}>
          <div className="activity-icon"><Icon name="workflow" size="sm" /></div>
          <div>
            <strong>{e.actor_name ?? e.actor_type} {describe(e)}</strong>
            <p>{e.action}</p>
          </div>
          <time dateTime={e.occurred_at}>{formatDate(e.occurred_at)}</time>
        </div>
      ))}
    </div>
  );
}

export function OverviewPage() {
  const { resource } = useSession();
  const name = resource.state === "ready" ? resource.data.display_name.split(" ")[0] : "";
  const session = useCurrentSession();
  const [workflows] = useResource<Page<WorkflowSummary>>("/api/v1/workflows?status=published&limit=100");
  const published = workflows.state === "ready" ? workflows.data.items.length : null;
  const [casesRes] = useResource<Page<CaseSummary>>("/api/v1/cases?limit=100");
  const [approvalsRes] = useResource<{ items: Approval[] }>("/api/v1/approvals?status=pending");
  const [tasksRes] = useResource<{ items: Task[] }>("/api/v1/tasks?status=open");
  const open = casesRes.state === "ready" ? casesRes.data.items.filter((c) => ["active", "under_review", "paused"].includes(c.status)) : [];
  const mine = approvalsRes.state === "ready" ? approvalsRes.data.items.filter((a) => a.can_decide) : [];
  const tasks = tasksRes.state === "ready" ? tasksRes.data.items.filter((t) => t.can_complete) : [];
  return (
    <>
      <PageHead title={`${greeting(new Date())}${name ? `, ${name}` : ""}`} subtitle="Your workflows, active cases and decisions — all in one place."
        actions={<Link className="btn primary" to="/workflows"><Icon name="workflow" size="sm" /> Explore workflows</Link>} />
      <div className="bento">
        <div className="hero">
          <label>THE WORKFLOW LAYER</label>
          <h2>Build once.<br />Run across every team.</h2>
          <p>Define how work moves, connect AI and people, and keep every decision traceable.</p>
          <Link className="btn" to="/designer">Open workflow designer <Icon name="arrow-up-right" size="sm" /></Link>
        </div>
        <div className="metric blue">
          <div className="metric-icon"><Icon name="workflow" /></div>
          <small>Published workflows</small>
          <strong>{published === null ? "—" : String(published).padStart(2, "0")}</strong>
          <div className="desc">Immutable, versioned definitions</div>
        </div>
        <div className="metric">
          <div className="metric-icon"><Icon name="briefcase" /></div>
          <small>Open cases</small>
          <strong>{casesRes.state === "ready" ? String(open.length).padStart(2, "0") : "—"}</strong>
          <div className="desc">Active, under review or paused</div>
        </div>
      </div>
      <div className="dashboard-grid">
        <div className="dashboard-col">
          <div className="panel panel-pad">
            <div className="panelhead"><div><h2>Active cases</h2><p>Current work and where it stands</p></div><Link to="/cases">View all</Link></div>
            {open.length === 0
              ? <EmptyState icon="briefcase" title="No open cases">Start a case from a published workflow.</EmptyState>
              : (
                <div className="queue">
                  {open.slice(0, 5).map((c) => (
                    <div className="queue-item" key={c.id}>
                      <div><strong>{c.title}</strong><p>{c.reference} · {c.workflow_name} v{c.workflow_version} · {c.progress}%</p></div>
                      <Link className="link" to={`/cases/${c.id}`}>Open</Link>
                    </div>
                  ))}
                </div>
              )}
          </div>
          <div className="panel panel-pad">
            <div className="panelhead"><div><h2>Recent activity</h2><p>Every action stays traceable</p></div><Link to="/audit">Audit log</Link></div>
            {canReadAudit(session)
              ? <RecentActivity />
              : <EmptyState icon="lock" title="Audit restricted">Designers, publishers and tenant admins see workflow activity.</EmptyState>}
          </div>
        </div>
        <div className="dashboard-col">
          <div className="panel panel-pad">
            <div className="panelhead"><div><h2>Needs your attention</h2><p>Decisions you are authorized to make</p></div>{mine.length > 0 && <span className="pill review">{mine.length} pending</span>}</div>
            {mine.length === 0
              ? <EmptyState icon="shield-check" title="Nothing pending">Approvals are routed by server-side policy and your delegations.</EmptyState>
              : (
                <div className="queue">
                  {mine.slice(0, 5).map((a) => (
                    <div className="queue-item" key={a.id}>
                      <div><strong>{a.title}</strong><p>{a.case.reference} · {a.policy_ref}</p></div>
                      <Link className="link" to={`/approvals?focus=${a.id}`}>Review</Link>
                    </div>
                  ))}
                </div>
              )}
          </div>
          <div className="panel panel-pad">
            <div className="panelhead"><div><h2>Assigned to you</h2><p>Tasks for your domain roles</p></div><Link to="/tasks">My tasks</Link></div>
            {tasks.length === 0
              ? <EmptyState icon="check-square" title="No open tasks">Tasks appear when a case reaches a step for your role.</EmptyState>
              : (
                <div className="queue">
                  {tasks.slice(0, 5).map((t) => (
                    <div className="queue-item" key={t.id}>
                      <div><strong>{t.title}</strong><p>{t.case.reference} · {t.assignee_role}</p></div>
                      <Link className="link" to="/tasks">Open</Link>
                    </div>
                  ))}
                </div>
              )}
          </div>
          <div className="panel panel-pad">
            <div className="panelhead"><div><h2>Platform status</h2><p>Live adapter health</p></div><Link to="/integrations">Details</Link></div>
            <PlatformStatus />
          </div>
        </div>
      </div>
    </>
  );
}
