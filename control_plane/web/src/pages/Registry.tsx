import { useState } from "react";
import { useSession } from "../session/context";
import { useApi } from "../lib/useApi";
import { useWorkspaceDirectory } from "../lib/workspaces";
import { api } from "../api/endpoints";
import { ApiRequestError, apiPost, toApiError } from "../api/client";
import type { Permission, RegistryApp } from "../api/types";
import { fmtDateTime, shortHash } from "../lib/format";
import { Icon } from "../components/Icons";
import { ManifestForm } from "../components/ManifestForm";
import {
  Async,
  Card,
  EmptyState,
  HealthPill,
  IntegrationPill,
  KV,
  PageHead,
  Pill,
  ReasonDialog,
  Skeleton,
  StatusPill,
  useToast,
  type PendingAction,
} from "../components/ui";
import { NotPermitted } from "./NotPermitted";

export function RegistryPage() {
  const { me } = useSession();
  if (!me.capabilities.manage_registry) return <NotPermitted area="Mini-App Registry" />;
  return <Registry />;
}

function Registry() {
  const { me } = useSession();
  const toast = useToast();
  const list = useApi<{ items: RegistryApp[] }>(api.admin.apps);
  const perms = useApi<{ items: Permission[] }>(api.admin.permissions);
  const wsDir = useWorkspaceDirectory(me, true);
  const [selected, setSelected] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const permissionKeys = perms.data?.items.map((p) => p.key) ?? [];
  const workspaceKeys = wsDir.all.map((w) => w.key);

  return (
    <>
      <PageHead
        eyebrow="ADMINISTRATION"
        title="Mini-App Registry"
        actions={
          <button type="button" className="btn primary" onClick={() => setCreateOpen(true)}>
            <Icon name="plus" size="sm" /> Register app
          </button>
        }
      >
        Manifests are versioned and immutable. Routes are fixed to <code>/apps/&lt;key&gt;/</code> on this origin; upstream
        targets live in trusted deployment config and are never editable here.
      </PageHead>

      <div className="stack">
        <Card pad={false}>
          <Async state={list} skeleton={<div className="cardbody"><Skeleton lines={5} /></div>} title="Could not load the registry">
            {({ items }) =>
              items.length === 0 ? (
                <EmptyState title="No apps registered" icon="box" />
              ) : (
                <div className="tablewrap">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th scope="col">App</th>
                        <th scope="col">Status</th>
                        <th scope="col">Published</th>
                        <th scope="col">Latest</th>
                        <th scope="col">Integration</th>
                        <th scope="col">Health</th>
                        <th scope="col">
                          <span className="sr-only">Actions</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {items.map((a) => (
                        <tr key={a.app_key} aria-selected={selected === a.app_key} className={selected === a.app_key ? "selected" : undefined}>
                          <td>
                            <span className="cellmain">{a.display_name}</span>
                            <span className="cellsub">
                              <code>{a.route_base}</code> · {a.owner_team}
                            </span>
                          </td>
                          <td>
                            <StatusPill value={a.status} />
                          </td>
                          <td>{a.published_version ?? "—"}</td>
                          <td>{a.latest_version}</td>
                          <td>
                            <IntegrationPill integration={a.integration} />
                          </td>
                          <td>
                            <HealthPill health={a.health} />
                          </td>
                          <td className="right">
                            <button type="button" className="btn sm" onClick={() => setSelected(a.app_key)} aria-controls="registry-detail">
                              Details
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )
            }
          </Async>
        </Card>

        {selected && (
          <AppDetail
            key={selected}
            appKey={selected}
            permissionKeys={permissionKeys}
            workspaceKeys={workspaceKeys}
            onChanged={list.reload}
            onClose={() => setSelected(null)}
          />
        )}
      </div>

      {createOpen && (
        <ManifestForm
          open
          mode="create"
          permissionKeys={permissionKeys}
          workspaceKeys={workspaceKeys}
          onClose={() => setCreateOpen(false)}
          onSaved={(app) => {
            toast(`Registered ${app.display_name} as a draft.`);
            list.reload();
            setSelected(app.app_key);
          }}
        />
      )}
    </>
  );
}

function AppDetail({
  appKey,
  permissionKeys,
  workspaceKeys,
  onChanged,
  onClose,
}: {
  appKey: string;
  permissionKeys: string[];
  workspaceKeys: string[];
  onChanged: () => void;
  onClose: () => void;
}) {
  const toast = useToast();
  const detail = useApi<RegistryApp>(api.admin.app(appKey));
  const [action, setAction] = useState<PendingAction | null>(null);
  const [versionOpen, setVersionOpen] = useState(false);

  /** Lifecycle call with If-Match; on 412 reload so the admin sees the newer revision. */
  const lifecycle = async (app: RegistryApp, verb: "publish" | "disable" | "enable", body: Record<string, unknown>) => {
    try {
      await apiPost<RegistryApp>(`${api.admin.app(app.app_key)}/${verb}`, body, app.revision);
    } catch (err) {
      const e = toApiError(err);
      if (e.status === 412 || e.status === 409) {
        detail.reload();
        onChanged();
        throw new ApiRequestError(e.status, {
          code: e.code,
          message: `${e.message} The app was changed by someone else; the latest revision has been reloaded — review and retry.`,
          trace_id: e.trace_id,
        });
      }
      throw e;
    }
    detail.reload();
    onChanged();
  };

  return (
    <Card
      id="registry-detail"
      title={detail.data ? detail.data.display_name : appKey}
      subtitle={detail.data ? `Revision ${detail.data.revision} · updated ${fmtDateTime(detail.data.updated_at)}` : undefined}
      actions={
        <button type="button" className="iconbtn" onClick={onClose} aria-label="Close details">
          <Icon name="x" />
        </button>
      }
    >
      <Async state={detail} skeleton={<Skeleton lines={6} />} title="Could not load app">
        {(app) => {
          const drafts = app.versions.filter((v) => v.release_state === "draft");
          const latest = app.versions.find((v) => v.version === app.latest_version) ?? app.versions[0];
          return (
            <>
              <div className="actions start">
                <button type="button" className="btn sm soft" onClick={() => setVersionOpen(true)} disabled={!latest}>
                  <Icon name="plus" size="sm" /> New draft version
                </button>
                <button
                  type="button"
                  className="btn sm primary"
                  disabled={drafts.length === 0}
                  title={drafts.length === 0 ? "No draft version to publish" : undefined}
                  onClick={() =>
                    setAction({
                      title: `Publish ${app.display_name}`,
                      description: "Publishing makes this version the one users can open, subject to their entitlements.",
                      confirmLabel: "Publish",
                      choice: {
                        label: "Draft version",
                        options: drafts.map((v) => ({ value: v.version, label: `${v.version} (${shortHash(v.checksum)})` })),
                      },
                      run: async (reason, { choice }) => {
                        await lifecycle(app, "publish", { version: choice, reason });
                        return `Published ${app.display_name} ${choice}.`;
                      },
                    })
                  }
                >
                  Publish…
                </button>
                {app.status === "disabled" ? (
                  <button
                    type="button"
                    className="btn sm"
                    onClick={() =>
                      setAction({
                        title: `Enable ${app.display_name}`,
                        description: "Re-activates the published version.",
                        confirmLabel: "Enable",
                        run: async (reason) => {
                          await lifecycle(app, "enable", { reason });
                          return `${app.display_name} enabled.`;
                        },
                      })
                    }
                  >
                    Enable
                  </button>
                ) : (
                  <button
                    type="button"
                    className="btn sm danger"
                    onClick={() =>
                      setAction({
                        title: `Disable ${app.display_name}`,
                        description: "Disabled apps cannot be opened by anyone until re-enabled.",
                        confirmLabel: "Disable",
                        danger: true,
                        run: async (reason) => {
                          await lifecycle(app, "disable", { reason });
                          return `${app.display_name} disabled.`;
                        },
                      })
                    }
                  >
                    Disable
                  </button>
                )}
              </div>

              <div className="halfgrid">
                <div className="kvlist">
                  <KV k="Status">
                    <StatusPill value={app.status} />
                  </KV>
                  <KV k="Route">
                    <code>{app.route_base}</code>
                  </KV>
                  <KV k="Published version">{app.published_version ?? "—"}</KV>
                  <KV k="Latest version">{app.latest_version}</KV>
                  <KV k="Owner team">{app.owner_team}</KV>
                  {app.disabled_reason && <KV k="Disabled reason">{app.disabled_reason}</KV>}
                </div>
                <div className="kvlist">
                  <KV k="Integration">
                    <IntegrationPill integration={app.integration} />
                  </KV>
                  <KV k="Upstream configured">
                    {app.upstream_configured ? <Pill tone="ok">yes</Pill> : <Pill tone="warn">not configured</Pill>}
                  </KV>
                  <KV k="Token audience">{app.audience ? <code>{app.audience}</code> : <span className="muted">none</span>}</KV>
                  <KV k="Health">
                    <HealthPill health={app.health} />
                  </KV>
                  <KV k="Health checked">{fmtDateTime(app.health.checked_at)}</KV>
                  {app.health.detail && <KV k="Health detail">{app.health.detail}</KV>}
                </div>
              </div>

              <h3 className="subtitle">Versions</h3>
              <div className="tablewrap flush">
                <table className="tbl">
                  <thead>
                    <tr>
                      <th scope="col">Version</th>
                      <th scope="col">State</th>
                      <th scope="col">Checksum</th>
                      <th scope="col">Entitlements</th>
                      <th scope="col">Created</th>
                      <th scope="col">Published</th>
                    </tr>
                  </thead>
                  <tbody>
                    {app.versions.map((v) => (
                      <tr key={v.id}>
                        <td className="cellmain">{v.version}</td>
                        <td>
                          <StatusPill value={v.release_state} />
                        </td>
                        <td>
                          <code title={v.checksum}>{shortHash(v.checksum)}</code>
                        </td>
                        <td>
                          {v.manifest.required_entitlements.map((p) => (
                            <span key={p} className="rolechip">
                              {p}
                            </span>
                          ))}
                        </td>
                        <td>{fmtDateTime(v.created_at)}</td>
                        <td>{fmtDateTime(v.published_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {versionOpen && latest && (
                <ManifestForm
                  open
                  mode="version"
                  base={latest.manifest}
                  permissionKeys={permissionKeys}
                  workspaceKeys={workspaceKeys}
                  onClose={() => setVersionOpen(false)}
                  onSaved={(a) => {
                    toast(`Draft version ${a.latest_version} created.`);
                    detail.reload();
                    onChanged();
                  }}
                />
              )}
            </>
          );
        }}
      </Async>
      <ReasonDialog action={action} onClose={() => setAction(null)} />
    </Card>
  );
}
