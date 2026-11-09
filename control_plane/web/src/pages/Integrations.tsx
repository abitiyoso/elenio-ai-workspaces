import { useSession } from "../session/context";
import { useApi } from "../lib/useApi";
import { api } from "../api/endpoints";
import type { IntegrationsStatus } from "../api/types";
import { fmtDateTime, fmtDuration } from "../lib/format";
import { Icon } from "../components/Icons";
import { Async, Card, EmptyState, HealthPill, IntegrationPill, KV, PageHead, Pill, Skeleton, StatusPill } from "../components/ui";
import { NotPermitted } from "./NotPermitted";

export function IntegrationsPage() {
  const { me } = useSession();
  if (!me.capabilities.view_integrations) return <NotPermitted area="Identity & Integrations" />;
  return <Integrations />;
}

function Integrations() {
  const status = useApi<IntegrationsStatus>(api.admin.integrations);
  return (
    <>
      <PageHead eyebrow="ADMINISTRATION" title="Identity & Integrations">
        How people sign in, how sessions are protected, and how each mini-app receives identity. Metadata only — secrets and
        upstream targets are never shown.
      </PageHead>
      <div className="flowline" aria-label="Request flow">
        {[
          ["users", "Browser", "HttpOnly session cookie"],
          ["key", "Identity provider", "OIDC code + PKCE"],
          ["shield", "Control plane BFF", "authorises every request"],
          ["box", "Mini-app", "audience-bound token"],
        ].map(([icon, t, s]) => (
          <div key={t} className="flowstep">
            <Icon name={icon!} />
            <strong>{t}</strong>
            <small>{s}</small>
          </div>
        ))}
      </div>
      <Async state={status} skeleton={<Skeleton lines={8} />} title="Could not load integration status">
        {(s) => (
          <div className="stack">
            <div className="halfgrid">
              <Card
                title="Identity provider"
                actions={
                  s.identity_provider.status === "ok" ? (
                    <Pill tone="ok">connected</Pill>
                  ) : s.identity_provider.status === "unavailable" ? (
                    <Pill tone="fail">unavailable</Pill>
                  ) : (
                    <Pill tone="warn">not configured</Pill>
                  )
                }
              >
                <div className="kvlist">
                  <KV k="Issuer">{s.identity_provider.issuer ? <span className="mono wrap">{s.identity_provider.issuer}</span> : <span className="muted">not configured</span>}</KV>
                  <KV k="Client id">{s.identity_provider.client_id ? <code>{s.identity_provider.client_id}</code> : <span className="muted">not configured</span>}</KV>
                  <KV k="Scopes">{s.identity_provider.scopes.join(" ") || "—"}</KV>
                  <KV k="Last checked">{fmtDateTime(s.identity_provider.last_checked_at)}</KV>
                  {s.identity_provider.detail && <KV k="Detail">{s.identity_provider.detail}</KV>}
                </div>
              </Card>
              <Card title="Browser session">
                <div className="kvlist">
                  <KV k="Cookie">
                    <code>{s.session.cookie_name}</code>
                  </KV>
                  <KV k="Secure flag">
                    {s.session.cookie_secure ? <Pill tone="ok">Secure</Pill> : <Pill tone="warn">not Secure (HTTP development)</Pill>}
                  </KV>
                  <KV k="SameSite">{s.session.same_site}</KV>
                  <KV k="Idle timeout">{fmtDuration(s.session.idle_ttl_seconds)}</KV>
                  <KV k="Absolute lifetime">{fmtDuration(s.session.absolute_ttl_seconds)}</KV>
                  <KV k="Active sessions">{s.session.active_sessions}</KV>
                </div>
              </Card>
            </div>

            <Card title="Service tokens" subtitle="Short-lived tokens the control plane mints for each upstream audience.">
              <div className="kvlist">
                <KV k="Issuer">
                  <span className="mono wrap">{s.service_tokens.issuer}</span>
                </KV>
                <KV k="Algorithm">{s.service_tokens.algorithm}</KV>
                <KV k="Lifetime">{fmtDuration(s.service_tokens.ttl_seconds)}</KV>
                <KV k="Key ids">
                  {s.service_tokens.key_ids.length ? (
                    s.service_tokens.key_ids.map((k) => (
                      <code key={k} className="kid">
                        {k}
                      </code>
                    ))
                  ) : (
                    <span className="muted">no signing keys</span>
                  )}
                </KV>
                <KV k="JWKS">
                  <a href={s.service_tokens.jwks_path} target="_blank" rel="noopener noreferrer">
                    {s.service_tokens.jwks_path}
                  </a>
                </KV>
              </div>
            </Card>

            <Card title="App adapters" subtitle="How each mini-app consumes identity." pad={false}>
              {s.adapters.length === 0 ? (
                <EmptyState title="No adapters" icon="plug" />
              ) : (
                <div className="tablewrap">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th scope="col">App</th>
                        <th scope="col">Integration</th>
                        <th scope="col">Contract</th>
                        <th scope="col">Audience</th>
                        <th scope="col">Health</th>
                      </tr>
                    </thead>
                    <tbody>
                      {s.adapters.map((a) => (
                        <tr key={a.app_key}>
                          <td>
                            <span className="cellmain">{a.display_name}</span>
                            <span className="cellsub">{a.app_key}</span>
                          </td>
                          <td>
                            <IntegrationPill integration={a.integration} />
                            {a.pending_reason && <span className="cellsub">{a.pending_reason}</span>}
                          </td>
                          <td>{a.contract}</td>
                          <td>{a.audience ? <code>{a.audience}</code> : <span className="muted">none</span>}</td>
                          <td>
                            <HealthPill health={a.health} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>

            <Card title="Service identities" subtitle="Machine clients (metadata only; credentials are never displayed)." pad={false}>
              {s.service_identities.length === 0 ? (
                <EmptyState title="No service identities registered" icon="key" />
              ) : (
                <div className="tablewrap">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th scope="col">Client</th>
                        <th scope="col">Audience</th>
                        <th scope="col">Scopes</th>
                        <th scope="col">Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      {s.service_identities.map((si) => (
                        <tr key={si.client_id}>
                          <td>
                            <code>{si.client_id}</code>
                            <span className="cellsub">{si.description}</span>
                          </td>
                          <td>
                            <code>{si.audience}</code>
                          </td>
                          <td>
                            {si.scopes.map((sc) => (
                              <span key={sc} className="rolechip">
                                {sc}
                              </span>
                            ))}
                          </td>
                          <td>
                            <StatusPill value={si.status} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>
          </div>
        )}
      </Async>
    </>
  );
}
