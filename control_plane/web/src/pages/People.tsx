import { useEffect, useId, useState } from "react";
import { useSession } from "../session/context";
import { useApi } from "../lib/useApi";
import { useWorkspaceDirectory } from "../lib/workspaces";
import { api } from "../api/endpoints";
import { apiDelete, apiPost, apiPut, toApiError, type ApiRequestError } from "../api/client";
import type { DirectoryUser, GrantRequest, GrantResult, Membership, Role, RoleBinding } from "../api/types";
import { fmtDate, fmtDateTime, initials } from "../lib/format";
import { Icon } from "../components/Icons";
import {
  Async,
  Card,
  EmptyState,
  ErrorState,
  Field,
  KV,
  Modal,
  PageHead,
  Pill,
  ReasonDialog,
  Skeleton,
  StatusPill,
  useToast,
  type ChoiceOption,
  type PendingAction,
} from "../components/ui";
import { canSeePeople } from "../components/nav";
import { NotPermitted } from "./NotPermitted";

function grantMessage(res: GrantResult, roleName: string): string {
  return res.outcome === "granted"
    ? `Granted ${roleName}.`
    : `${roleName} requires independent approval — request submitted and pending.`;
}

function roleOptions(roles: Role[] | undefined, scope: Role["scope"]): ChoiceOption[] {
  return (roles ?? [])
    .filter((r) => r.scope === scope)
    .map((r) => ({
      value: r.key,
      label: `${r.display_name} (${r.key})${r.sensitive ? " — sensitive" : ""}`,
      note: r.sensitive
        ? "Sensitive role: requires independent approval by another admin before it takes effect."
        : r.description,
    }));
}

export function PeoplePage() {
  const { me } = useSession();
  const caps = me.capabilities;
  if (!canSeePeople(caps)) return <NotPermitted area="People & Teams" />;
  return (
    <>
      <PageHead eyebrow="ADMINISTRATION" title="People & Teams">
        Directory users come from your identity provider. Memberships and role bindings are managed here and enforced by
        the server.
      </PageHead>
      <div className="stack">
        {caps.approve_grants && <PendingApprovals />}
        {caps.view_people && <Directory />}
      </div>
    </>
  );
}

function Directory() {
  const { me } = useSession();
  const caps = me.capabilities;
  const toast = useToast();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const searchId = useId();
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query), 250);
    return () => window.clearTimeout(t);
  }, [query]);
  const users = useApi<{ items: DirectoryUser[] }>(api.admin.users(debounced));
  const roles = useApi<{ items: Role[]; policy_version: string }>(api.admin.roles);
  const wsDir = useWorkspaceDirectory(me, true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [snapshot, setSnapshot] = useState<DirectoryUser | null>(null);
  const [action, setAction] = useState<PendingAction | null>(null);
  const [provisionOpen, setProvisionOpen] = useState(false);

  const selected = users.data?.items.find((u) => u.id === selectedId) ?? (snapshot?.id === selectedId ? snapshot : null);
  const roleName = (key: string) => roles.data?.items.find((r) => r.key === key)?.display_name ?? key;

  const after = (msg: string, updated?: DirectoryUser) => {
    if (updated) setSnapshot(updated);
    users.reload();
    return msg;
  };

  return (
    <>
      <Card
        title="Directory"
        subtitle={caps.manage_workspaces ? "All users in your tenant" : "Users in the workspaces you administer"}
        pad={false}
        actions={
          caps.manage_workspaces && (
            <button type="button" className="btn primary sm" onClick={() => setProvisionOpen(true)}>
              <Icon name="plus" size="sm" /> Pre-provision user
            </button>
          )
        }
      >
        <div className="cardtoolbar">
          <label className="search" htmlFor={searchId}>
            <Icon name="search" size="sm" />
            <span className="sr-only">Search people</span>
            <input
              id={searchId}
              type="search"
              placeholder="Search by name or email"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
          {users.loading && users.data && <span className="tiny muted">Updating…</span>}
        </div>
        <Async state={users} skeleton={<div className="cardbody"><Skeleton lines={5} /></div>} title="Could not load users">
          {({ items }) =>
            items.length === 0 ? (
              <EmptyState title={debounced ? "No users match" : "No users"} icon="users" />
            ) : (
              <div className="tablewrap">
                <table className="tbl">
                  <thead>
                    <tr>
                      <th scope="col">User</th>
                      <th scope="col">Status</th>
                      <th scope="col">Tenant roles</th>
                      <th scope="col">Memberships</th>
                      <th scope="col">Last sign-in</th>
                      <th scope="col">
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((u) => (
                      <tr key={u.id}>
                        <td>
                          <div className="user-cell">
                            <div className="avatar-sm" aria-hidden="true">
                              {initials(u.display_name)}
                            </div>
                            <div>
                              <strong>{u.display_name}</strong>
                              <small>{u.email ?? u.idp_subject}</small>
                            </div>
                          </div>
                        </td>
                        <td>
                          <StatusPill value={u.status} />
                          {u.principal_type === "service" && <Pill tone="info">service</Pill>}
                        </td>
                        <td>
                          {u.tenant_roles.filter((r) => r.state === "active").map((r) => (
                            <span key={r.id} className="rolechip">
                              {roleName(r.role_key)}
                            </span>
                          ))}
                          {u.tenant_roles.filter((r) => r.state === "active").length === 0 && <span className="muted">—</span>}
                        </td>
                        <td>{u.memberships.filter((m) => m.state !== "revoked").length}</td>
                        <td>{fmtDateTime(u.last_login_at)}</td>
                        <td className="right">
                          <button
                            type="button"
                            className="btn sm"
                            onClick={() => {
                              setSelectedId(u.id);
                              setSnapshot(u);
                            }}
                          >
                            Manage
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

      <Modal
        open={selected !== null}
        variant="drawer"
        title={selected?.display_name ?? ""}
        description={selected?.email ?? undefined}
        onClose={() => setSelectedId(null)}
      >
        {selected && (
          <UserDetail
            user={selected}
            roles={roles.data?.items}
            roleName={roleName}
            wsName={wsDir.name}
            managed={wsDir.managed}
            setAction={setAction}
            after={after}
          />
        )}
      </Modal>

      <ProvisionDialog
        open={provisionOpen}
        onClose={() => setProvisionOpen(false)}
        onCreated={(u) => {
          toast(`Pre-provisioned ${u.display_name}.`);
          users.reload();
        }}
      />
      <ReasonDialog action={action} onClose={() => setAction(null)} />
    </>
  );
}

function UserDetail({
  user,
  roles,
  roleName,
  wsName,
  managed,
  setAction,
  after,
}: {
  user: DirectoryUser;
  roles: Role[] | undefined;
  roleName: (k: string) => string;
  wsName: (id: string) => string;
  managed: { id: string; name: string }[];
  setAction: (a: PendingAction) => void;
  after: (msg: string, updated?: DirectoryUser) => string;
}) {
  const { me } = useSession();
  const caps = me.capabilities;
  const isSelf = user.id === me.user.id;
  const canManageWs = (wsId: string) => caps.manage_memberships.includes(wsId);
  const memberOf = new Set(user.memberships.filter((m) => m.state !== "revoked").map((m) => m.workspace_id));
  const addable = managed.filter((w) => !memberOf.has(w.id));

  const revokeBinding = (b: RoleBinding, where: string) =>
    setAction({
      title: `Revoke ${roleName(b.role_key)}`,
      description: `Remove ${roleName(b.role_key)} from ${user.display_name} ${where}.`,
      confirmLabel: "Revoke role",
      danger: true,
      run: async (reason) => {
        await apiDelete(api.admin.roleBinding(b.id), { reason });
        return after(`Revoked ${roleName(b.role_key)}.`);
      },
    });

  const membershipAction = (m: Membership, verb: "suspend" | "reinstate" | "revoke") =>
    setAction({
      title: `${verb[0]!.toUpperCase()}${verb.slice(1)} membership in ${m.workspace_name}`,
      description:
        verb === "revoke"
          ? "Revoking removes the user from the workspace and ends all workspace role bindings."
          : verb === "suspend"
            ? "Suspended members keep their bindings but cannot use the workspace until reinstated."
            : "The user regains access with their existing role bindings.",
      confirmLabel: `${verb[0]!.toUpperCase()}${verb.slice(1)}`,
      danger: verb !== "reinstate",
      run: async (reason) => {
        await apiPost<Membership>(`${api.admin.membership(m.id)}/${verb}`, { reason });
        return after(`Membership ${verb === "revoke" ? "revoked" : verb === "suspend" ? "suspended" : "reinstated"}.`);
      },
    });

  const grantWorkspaceRole = (m: Membership) =>
    setAction({
      title: `Grant role in ${m.workspace_name}`,
      description: `Grant a workspace role to ${user.display_name}.`,
      confirmLabel: "Grant role",
      choice: { label: "Role", options: roleOptions(roles, "workspace"), emptyText: "No workspace roles are defined." },
      withExpiry: true,
      run: async (reason, { choice, expiresAt }) => {
        const res = await apiPut<GrantResult>(`${api.admin.membership(m.id)}/roles`, {
          role_key: choice,
          reason,
          ...(expiresAt ? { expires_at: expiresAt } : {}),
        });
        return after(grantMessage(res.data, roleName(choice)));
      },
    });

  const grantTenantRole = () =>
    setAction({
      title: "Grant tenant role",
      description: "Tenant roles are sensitive: the grant stays pending until an independent approver accepts it.",
      confirmLabel: "Request grant",
      choice: { label: "Role", options: roleOptions(roles, "tenant"), emptyText: "No tenant roles are defined." },
      withExpiry: true,
      run: async (reason, { choice, expiresAt }) => {
        const res = await apiPut<GrantResult>(`${api.admin.user(user.id)}/tenant-roles`, {
          role_key: choice,
          reason,
          ...(expiresAt ? { expires_at: expiresAt } : {}),
        });
        return after(grantMessage(res.data, roleName(choice)));
      },
    });

  return (
    <div className="stack tight">
      <div className="kvlist">
        <KV k="Status">
          <StatusPill value={user.status} />
        </KV>
        <KV k="Principal">{user.principal_type}</KV>
        <KV k="Identity provider">
          <span className="mono wrap">{user.idp_issuer}</span>
        </KV>
        <KV k="Subject">
          <span className="mono wrap">{user.idp_subject}</span>
        </KV>
        <KV k="Last sign-in">{fmtDateTime(user.last_login_at)}</KV>
      </div>

      {caps.suspend_users && (
        <div className="actions start">
          {user.status === "active" ? (
            <button
              type="button"
              className="btn danger sm"
              disabled={isSelf}
              title={isSelf ? "You cannot suspend yourself" : undefined}
              onClick={() =>
                setAction({
                  title: `Suspend ${user.display_name}`,
                  description: "Suspension blocks sign-in and revokes all active sessions immediately.",
                  confirmLabel: "Suspend user",
                  danger: true,
                  run: async (reason) => {
                    const u = await apiPost<DirectoryUser>(`${api.admin.user(user.id)}/suspend`, { reason });
                    return after(`${user.display_name} suspended.`, u);
                  },
                })
              }
            >
              Suspend user
            </button>
          ) : (
            <button
              type="button"
              className="btn sm"
              onClick={() =>
                setAction({
                  title: `Activate ${user.display_name}`,
                  confirmLabel: "Activate user",
                  run: async (reason) => {
                    const u = await apiPost<DirectoryUser>(`${api.admin.user(user.id)}/activate`, { reason });
                    return after(`${user.display_name} activated.`, u);
                  },
                })
              }
            >
              Activate user
            </button>
          )}
          <button
            type="button"
            className="btn sm"
            onClick={() =>
              setAction({
                title: `Revoke sessions for ${user.display_name}`,
                description: "Signs the user out everywhere. They can sign in again unless suspended.",
                confirmLabel: "Revoke sessions",
                danger: true,
                run: async (reason) => {
                  const r = await apiPost<{ revoked: number }>(`${api.admin.user(user.id)}/sessions/revoke`, { reason });
                  return after(`Revoked ${r.revoked} session${r.revoked === 1 ? "" : "s"}.`);
                },
              })
            }
          >
            Revoke sessions
          </button>
        </div>
      )}

      <section className="subsection" aria-label="Tenant roles">
        <div className="subhead">
          <h3>Tenant roles</h3>
          {caps.manage_workspaces && (
            <button type="button" className="btn sm soft" onClick={grantTenantRole}>
              <Icon name="plus" size="sm" /> Grant tenant role
            </button>
          )}
        </div>
        <BindingChips
          bindings={user.tenant_roles}
          roleName={roleName}
          canRevoke={caps.manage_workspaces}
          onRevoke={(b) => revokeBinding(b, "at tenant scope")}
        />
      </section>

      <section className="subsection" aria-label="Workspace memberships">
        <div className="subhead">
          <h3>Workspace memberships</h3>
          {managed.length > 0 && (
            <button
              type="button"
              className="btn sm soft"
              onClick={() =>
                setAction({
                  title: `Add ${user.display_name} to a workspace`,
                  confirmLabel: "Add member",
                  choice: {
                    label: "Workspace",
                    options: addable.map((w) => ({ value: w.id, label: w.name })),
                    emptyText: "The user is already a member of every workspace you manage.",
                  },
                  run: async (reason, { choice }) => {
                    await apiPost<Membership>(api.admin.members(choice), { user_id: user.id, reason });
                    return after(`Added to ${wsName(choice)}.`);
                  },
                })
              }
            >
              <Icon name="plus" size="sm" /> Add to workspace
            </button>
          )}
        </div>
        {user.memberships.length === 0 ? (
          <p className="muted">No workspace memberships.</p>
        ) : (
          <ul className="memberships">
            {user.memberships.map((m) => {
              const manageable = canManageWs(m.workspace_id);
              return (
                <li key={m.id} className="membership">
                  <div className="membershiphead">
                    <div>
                      <strong>{m.workspace_name}</strong>
                      <small>
                        {m.workspace_key} · joined {fmtDate(m.joined_at)}
                      </small>
                    </div>
                    <StatusPill value={m.state} />
                  </div>
                  <BindingChips
                    bindings={m.roles}
                    roleName={roleName}
                    canRevoke={manageable && m.state !== "revoked"}
                    onRevoke={(b) => revokeBinding(b, `in ${m.workspace_name}`)}
                  />
                  {manageable && m.state !== "revoked" && (
                    <div className="actions start">
                      {m.state === "active" && (
                        <button type="button" className="btn sm soft" onClick={() => grantWorkspaceRole(m)}>
                          Grant role
                        </button>
                      )}
                      {m.state === "active" ? (
                        <button type="button" className="btn sm" onClick={() => membershipAction(m, "suspend")}>
                          Suspend
                        </button>
                      ) : (
                        <button type="button" className="btn sm" onClick={() => membershipAction(m, "reinstate")}>
                          Reinstate
                        </button>
                      )}
                      <button type="button" className="btn sm danger" onClick={() => membershipAction(m, "revoke")}>
                        Revoke membership
                      </button>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}

function BindingChips({
  bindings,
  roleName,
  canRevoke,
  onRevoke,
}: {
  bindings: RoleBinding[];
  roleName: (k: string) => string;
  canRevoke: boolean;
  onRevoke: (b: RoleBinding) => void;
}) {
  const active = bindings.filter((b) => b.state === "active");
  if (active.length === 0) return <p className="muted tiny">No active roles.</p>;
  return (
    <div className="chiprow">
      {active.map((b) => (
        <span key={b.id} className={`rolechip${canRevoke ? " removable" : ""}`} title={b.reason || undefined}>
          {roleName(b.role_key)}
          {b.expires_at && <span className="chipmeta"> · until {fmtDate(b.expires_at)}</span>}
          {canRevoke && (
            <button type="button" aria-label={`Revoke ${roleName(b.role_key)}`} onClick={() => onRevoke(b)}>
              <Icon name="x" size="sm" />
            </button>
          )}
        </span>
      ))}
    </div>
  );
}

function ProvisionDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (u: DirectoryUser) => void;
}) {
  const empty = { idp_issuer: "", idp_subject: "", display_name: "", email: "", reason: "" };
  const [form, setForm] = useState(empty);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiRequestError | null>(null);
  const ids = { iss: useId(), sub: useId(), name: useId(), email: useId(), reason: useId() };
  const set = (k: keyof typeof empty) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));
  const issueFor = (field: string) => error?.issues.find((i) => i.field === field)?.message;
  const close = () => {
    setForm(empty);
    setError(null);
    onClose();
  };
  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const u = await apiPost<DirectoryUser>(api.admin.usersCreate, {
        idp_issuer: form.idp_issuer.trim(),
        idp_subject: form.idp_subject.trim(),
        display_name: form.display_name.trim(),
        ...(form.email.trim() ? { email: form.email.trim() } : {}),
        reason: form.reason.trim(),
      });
      onCreated(u);
      close();
    } catch (err) {
      setError(toApiError(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      open={open}
      title="Pre-provision directory user"
      description="Creates a directory binding to an identity at your IdP so memberships can be assigned before first sign-in. No password or credential is stored here — the user always authenticates at the identity provider."
      onClose={close}
      footer={
        <>
          <button type="button" className="btn" onClick={close}>
            Cancel
          </button>
          <button type="submit" form="provision-form" className="btn primary" disabled={busy}>
            {busy ? "Creating…" : "Create directory user"}
          </button>
        </>
      }
    >
      <form
        id="provision-form"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="IdP issuer" htmlFor={ids.iss} hint="Exact issuer URL of the identity provider." error={issueFor("idp_issuer")}>
          <input id={ids.iss} className="input" required value={form.idp_issuer} onChange={set("idp_issuer")} data-autofocus />
        </Field>
        <Field label="IdP subject" htmlFor={ids.sub} hint="The stable `sub` claim for this person." error={issueFor("idp_subject")}>
          <input id={ids.sub} className="input" required value={form.idp_subject} onChange={set("idp_subject")} />
        </Field>
        <Field label="Display name" htmlFor={ids.name} error={issueFor("display_name")}>
          <input id={ids.name} className="input" required value={form.display_name} onChange={set("display_name")} />
        </Field>
        <Field label="Email (optional)" htmlFor={ids.email} error={issueFor("email")}>
          <input id={ids.email} type="email" className="input" value={form.email} onChange={set("email")} />
        </Field>
        <Field label="Reason" htmlFor={ids.reason} hint="3–500 characters, recorded in the audit trail." error={issueFor("reason")}>
          <textarea
            id={ids.reason}
            className="textarea"
            required
            minLength={3}
            maxLength={500}
            value={form.reason}
            onChange={set("reason")}
          />
        </Field>
        {error && <ErrorState error={error} title="Could not create user" compact />}
      </form>
    </Modal>
  );
}

function PendingApprovals() {
  const { me } = useSession();
  const requests = useApi<{ items: GrantRequest[] }>(api.admin.grantRequests);
  const [action, setAction] = useState<PendingAction | null>(null);

  const decide = (r: GrantRequest, verb: "approve" | "reject") =>
    setAction({
      title: `${verb === "approve" ? "Approve" : "Reject"} ${r.role_key} for ${r.user_name}`,
      description: (
        <>
          Requested by {r.requested_by_name}
          {r.workspace_name ? ` in ${r.workspace_name}` : " at tenant scope"}: “{r.reason}”
        </>
      ),
      confirmLabel: verb === "approve" ? "Approve grant" : "Reject request",
      danger: verb === "reject",
      run: async (reason) => {
        await apiPost<GrantRequest>(`${api.admin.grantRequest(r.id)}/${verb}`, { reason });
        requests.reload();
        return verb === "approve" ? `Approved ${r.role_key} for ${r.user_name}.` : "Request rejected.";
      },
    });

  return (
    <Card
      title="Pending approvals"
      subtitle="Sensitive role grants need an approver who is neither the requester nor the recipient."
      pad={false}
    >
      <Async state={requests} skeleton={<div className="cardbody"><Skeleton lines={3} /></div>} title="Could not load requests">
        {({ items }) =>
          items.length === 0 ? (
            <EmptyState title="No pending requests" icon="check" />
          ) : (
            <div className="tablewrap">
              <table className="tbl">
                <thead>
                  <tr>
                    <th scope="col">User</th>
                    <th scope="col">Role</th>
                    <th scope="col">Scope</th>
                    <th scope="col">Requested by</th>
                    <th scope="col">Expires</th>
                    <th scope="col">
                      <span className="sr-only">Decision</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((r) => {
                    const own = r.requested_by === me.user.id || r.user_id === me.user.id;
                    return (
                      <tr key={r.id}>
                        <td className="cellmain">{r.user_name}</td>
                        <td>
                          <span className="rolechip">{r.role_key}</span>
                        </td>
                        <td>{r.workspace_name ?? "Tenant"}</td>
                        <td>
                          {r.requested_by_name}
                          <span className="cellsub">{r.reason}</span>
                        </td>
                        <td>{fmtDateTime(r.expires_at)}</td>
                        <td className="right">
                          <div className="actions">
                            <button
                              type="button"
                              className="btn sm primary"
                              disabled={own}
                              title={own ? "You cannot approve a request you made or that grants you a role" : undefined}
                              onClick={() => decide(r, "approve")}
                            >
                              Approve
                            </button>
                            <button type="button" className="btn sm danger" onClick={() => decide(r, "reject")}>
                              Reject
                            </button>
                          </div>
                          {own && <span className="cellsub">Needs another approver</span>}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )
        }
      </Async>
      <ReasonDialog action={action} onClose={() => setAction(null)} />
    </Card>
  );
}
