import { useId, useState } from "react";
import { useSession } from "../session/context";
import { useApi } from "../lib/useApi";
import { useWorkspaceDirectory } from "../lib/workspaces";
import { api } from "../api/endpoints";
import { apiDelete, apiPost, toApiError, type ApiRequestError } from "../api/client";
import type {
  AppSummary,
  Decision,
  DirectoryUser,
  EntitlementOverride,
  Permission,
  RegistryApp,
  Role,
} from "../api/types";
import { fmtDateTime, localInputToIso } from "../lib/format";
import { DecisionView } from "../components/DecisionView";
import { Icon } from "../components/Icons";
import {
  Async,
  Card,
  EmptyState,
  ErrorState,
  Field,
  PageHead,
  ReasonDialog,
  Skeleton,
  StatusPill,
  useToast,
  type PendingAction,
} from "../components/ui";
import { canSeeAccess } from "../components/nav";

export function AccessPage() {
  const { me } = useSession();
  const admin = canSeeAccess(me.capabilities);
  return (
    <>
      <PageHead eyebrow="ADMINISTRATION" title="Access Control">
        Least-privilege roles, explainable decisions and workspace overrides. Every result here is computed by the
        server's policy engine.
      </PageHead>
      <div className="stack">
        {admin && <AccessPreviewPanel />}
        <CheckMyAccess />
        {admin && <RoleMatrix />}
        {admin && <OverridesPanel />}
      </div>
    </>
  );
}

// ---------- access preview ----------

export function AccessPreviewPanel() {
  const { me } = useSession();
  const users = useApi<{ items: DirectoryUser[] }>(api.admin.users(""));
  const wsDir = useWorkspaceDirectory(me, true);
  const regApps = useApi<{ items: RegistryApp[] }>(me.capabilities.manage_registry ? api.admin.apps : null);
  const myApps = useApi<{ items: AppSummary[] }>(me.capabilities.manage_registry ? null : api.apps);
  const appOptions =
    regApps.data?.items.map((a) => ({ key: a.app_key, name: a.display_name })) ??
    myApps.data?.items.map((a) => ({ key: a.app_key, name: a.display_name })) ??
    [];
  const wsOptions = wsDir.all;

  const [userId, setUserId] = useState("");
  const [wsId, setWsId] = useState("");
  const [appKey, setAppKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<Decision | null>(null);
  const [error, setError] = useState<ApiRequestError | null>(null);
  const ids = { user: useId(), ws: useId(), app: useId() };

  const run = async () => {
    if (!userId || !wsId || !appKey) return;
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      setResult(await apiPost<Decision>(api.admin.accessPreview, { user_id: userId, workspace_id: wsId, app_key: appKey }));
    } catch (err) {
      setError(toApiError(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title="Access preview" subtitle="Ask the policy engine whether a user may open an app in a workspace, and why.">
      {users.error && <ErrorState error={users.error} title="Could not load users" compact />}
      <form
        className="formgrid three"
        onSubmit={(e) => {
          e.preventDefault();
          void run();
        }}
      >
        <Field label="User" htmlFor={ids.user}>
          <select id={ids.user} className="select" value={userId} onChange={(e) => setUserId(e.target.value)} required>
            <option value="">Select a user…</option>
            {users.data?.items.map((u) => (
              <option key={u.id} value={u.id}>
                {u.display_name}
                {u.email ? ` (${u.email})` : ""}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Workspace" htmlFor={ids.ws}>
          <select id={ids.ws} className="select" value={wsId} onChange={(e) => setWsId(e.target.value)} required>
            <option value="">Select a workspace…</option>
            {wsOptions.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="App" htmlFor={ids.app}>
          <select id={ids.app} className="select" value={appKey} onChange={(e) => setAppKey(e.target.value)} required>
            <option value="">Select an app…</option>
            {appOptions.map((a) => (
              <option key={a.key} value={a.key}>
                {a.name}
              </option>
            ))}
          </select>
        </Field>
        <div className="formactions">
          <button type="submit" className="btn primary" disabled={busy || !userId || !wsId || !appKey}>
            <Icon name="shield" size="sm" /> {busy ? "Evaluating…" : "Evaluate access"}
          </button>
        </div>
      </form>
      {error && <ErrorState error={error} title="Preview failed" />}
      {result && <DecisionView decision={result} />}
    </Card>
  );
}

// ---------- check my access ----------

type ResourceType = "mini_app" | "workspace" | "platform_admin";

function CheckMyAccess() {
  const { me } = useSession();
  const [type, setType] = useState<ResourceType>("mini_app");
  const [resource, setResource] = useState("");
  const [perm, setPerm] = useState("registry:manage");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<Decision | null>(null);
  const [error, setError] = useState<ApiRequestError | null>(null);
  const ids = { type: useId(), res: useId(), perm: useId() };

  const options =
    type === "mini_app"
      ? me.apps.map((a) => ({ value: a.app_key, label: a.display_name }))
      : type === "workspace"
        ? me.workspaces.map((w) => ({ value: w.id, label: w.name }))
        : [];

  const run = async () => {
    setBusy(true);
    setError(null);
    setResult(null);
    const body =
      type === "platform_admin"
        ? { action: perm.trim(), resource_type: type, resource_id: me.tenant.id }
        : { action: type === "mini_app" ? "app:open" : "workspace:switch", resource_type: type, resource_id: resource };
    try {
      setResult(await apiPost<Decision>(api.authzCheck, body));
    } catch (err) {
      setError(toApiError(err));
    } finally {
      setBusy(false);
    }
  };

  const ready = type === "platform_admin" ? perm.trim().length > 0 : resource !== "";
  return (
    <Card title="Check my access" subtitle="Evaluate a decision for yourself only — the server never accepts another subject here.">
      <form
        className="formgrid three"
        onSubmit={(e) => {
          e.preventDefault();
          void run();
        }}
      >
        <Field label="Resource type" htmlFor={ids.type}>
          <select
            id={ids.type}
            className="select"
            value={type}
            onChange={(e) => {
              setType(e.target.value as ResourceType);
              setResource("");
              setResult(null);
            }}
          >
            <option value="mini_app">Mini-app (app:open)</option>
            <option value="workspace">Workspace (workspace:switch)</option>
            <option value="platform_admin">Platform admin permission</option>
          </select>
        </Field>
        {type === "platform_admin" ? (
          <Field label="Permission key" htmlFor={ids.perm} hint="e.g. registry:manage, audit:view">
            <input id={ids.perm} className="input" value={perm} onChange={(e) => setPerm(e.target.value)} />
          </Field>
        ) : (
          <Field label={type === "mini_app" ? "App" : "Workspace"} htmlFor={ids.res}>
            <select id={ids.res} className="select" value={resource} onChange={(e) => setResource(e.target.value)}>
              <option value="">Select…</option>
              {options.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </Field>
        )}
        <div className="formactions">
          <button type="submit" className="btn soft" disabled={busy || !ready}>
            {busy ? "Checking…" : "Check"}
          </button>
        </div>
      </form>
      {error && <ErrorState error={error} title="Check failed" />}
      {result && <DecisionView decision={result} />}
    </Card>
  );
}

// ---------- role matrix ----------

function RoleMatrix() {
  const roles = useApi<{ items: Role[]; policy_version: string }>(api.admin.roles);
  const perms = useApi<{ items: Permission[] }>(api.admin.permissions);
  const combined = {
    data: roles.data && perms.data ? { roles: roles.data, perms: perms.data.items } : undefined,
    error: roles.error ?? perms.error,
    loading: roles.loading || perms.loading,
    reload: () => {
      roles.reload();
      perms.reload();
    },
  };
  return (
    <Card title="Role & permission matrix" subtitle={roles.data ? `Policy ${roles.data.policy_version}` : undefined} pad={false}>
      <Async state={combined} skeleton={<div className="cardbody"><Skeleton lines={6} /></div>} title="Could not load roles">
        {({ roles: r, perms: p }) =>
          r.items.length === 0 ? (
            <EmptyState title="No roles defined" />
          ) : (
            <div className="tablewrap">
              <table className="tbl matrix">
                <caption className="sr-only">Permissions granted by each role</caption>
                <thead>
                  <tr>
                    <th scope="col">Permission</th>
                    {r.items.map((role) => (
                      <th key={role.key} scope="col" title={role.description}>
                        {role.display_name}
                        <span className="cellsub">
                          {role.scope}
                          {role.sensitive ? " · sensitive" : ""}
                        </span>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {p.map((perm) => (
                    <tr key={perm.key}>
                      <th scope="row">
                        <code>{perm.key}</code>
                        {perm.sensitive && <span className="sensitivetag">sensitive</span>}
                        <span className="cellsub">{perm.description}</span>
                      </th>
                      {r.items.map((role) => {
                        const has = role.permissions.includes(perm.key);
                        return (
                          <td key={role.key} className="center">
                            {has ? (
                              <span className="matrixyes">
                                <Icon name="check" size="sm" />
                                <span className="sr-only">granted</span>
                              </span>
                            ) : (
                              <span className="matrixno" aria-label="not granted">
                                ·
                              </span>
                            )}
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </Async>
    </Card>
  );
}

// ---------- overrides ----------

function OverridesPanel() {
  const { me } = useSession();
  const toast = useToast();
  const wsDir = useWorkspaceDirectory(me, true);
  const managed = wsDir.managed;
  const [wsChoice, setWsChoice] = useState("");
  const wsId = wsChoice || managed[0]?.id || "";
  const overrides = useApi<{ items: EntitlementOverride[] }>(wsId ? api.admin.overrides(wsId) : null);
  const users = useApi<{ items: DirectoryUser[] }>(wsId ? api.admin.users("") : null);
  const perms = useApi<{ items: Permission[] }>(api.admin.permissions);
  const [action, setAction] = useState<PendingAction | null>(null);

  const [form, setForm] = useState({ user_id: "", permission_key: "", effect: "deny" as "allow" | "deny", expires: "", reason: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiRequestError | null>(null);
  const ids = { ws: useId(), user: useId(), perm: useId(), effect: useId(), exp: useId(), reason: useId() };

  const selectedPerm = perms.data?.items.find((p) => p.key === form.permission_key);
  const allowEligible = (p: Permission | undefined) =>
    !!p && !p.sensitive && (p.key.endsWith(":open") || p.key === "catalog:view");

  const create = async () => {
    setBusy(true);
    setError(null);
    try {
      await apiPost<EntitlementOverride>(api.admin.overrides(wsId), {
        user_id: form.user_id,
        permission_key: form.permission_key,
        effect: form.effect,
        reason: form.reason.trim(),
        ...(localInputToIso(form.expires) ? { expires_at: localInputToIso(form.expires) } : {}),
      });
      toast("Override created.");
      setForm({ user_id: "", permission_key: "", effect: "deny", expires: "", reason: "" });
      overrides.reload();
    } catch (err) {
      setError(toApiError(err));
    } finally {
      setBusy(false);
    }
  };

  if (managed.length === 0) {
    return (
      <Card title="Entitlement overrides">
        <EmptyState title="No workspaces to manage" icon="layers">
          Overrides are managed per workspace by its administrators.
        </EmptyState>
      </Card>
    );
  }

  return (
    <Card
      title="Entitlement overrides"
      subtitle="Per-user exceptions in one workspace. Deny always wins; allow is limited to non-sensitive open/view permissions."
      actions={
        <>
          <label htmlFor={ids.ws} className="sr-only">
            Workspace
          </label>
          <select id={ids.ws} className="select narrow" value={wsId} onChange={(e) => setWsChoice(e.target.value)}>
            {managed.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </>
      }
    >
      <Async state={overrides} skeleton={<Skeleton lines={3} />} title="Could not load overrides">
        {({ items }) =>
          items.length === 0 ? (
            <p className="muted">No overrides in this workspace.</p>
          ) : (
            <div className="tablewrap flush">
              <table className="tbl">
                <thead>
                  <tr>
                    <th scope="col">User</th>
                    <th scope="col">Permission</th>
                    <th scope="col">Effect</th>
                    <th scope="col">Expires</th>
                    <th scope="col">Reason</th>
                    <th scope="col">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((o) => (
                    <tr key={o.id}>
                      <td className="cellmain">{o.user_name}</td>
                      <td>
                        <code>{o.permission_key}</code>
                      </td>
                      <td>
                        <StatusPill value={o.effect} />
                      </td>
                      <td>{fmtDateTime(o.expires_at)}</td>
                      <td>{o.reason}</td>
                      <td className="right">
                        <button
                          type="button"
                          className="btn sm danger"
                          onClick={() =>
                            setAction({
                              title: "Delete override",
                              description: `Remove the ${o.effect} override for ${o.permission_key} on ${o.user_name}.`,
                              confirmLabel: "Delete override",
                              danger: true,
                              run: async (reason) => {
                                await apiDelete(api.admin.override(o.id), { reason });
                                overrides.reload();
                                return "Override deleted.";
                              },
                            })
                          }
                        >
                          <Icon name="trash" size="sm" /> Delete
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

      <h3 className="subtitle">New override</h3>
      <form
        className="formgrid three"
        onSubmit={(e) => {
          e.preventDefault();
          void create();
        }}
      >
        <Field label="User" htmlFor={ids.user}>
          <select
            id={ids.user}
            className="select"
            required
            value={form.user_id}
            onChange={(e) => setForm({ ...form, user_id: e.target.value })}
          >
            <option value="">Select a user…</option>
            {users.data?.items
              .filter((u) => u.memberships.some((m) => m.workspace_id === wsId && m.state !== "revoked"))
              .map((u) => (
                <option key={u.id} value={u.id}>
                  {u.display_name}
                </option>
              ))}
          </select>
        </Field>
        <Field label="Permission" htmlFor={ids.perm}>
          <select
            id={ids.perm}
            className="select"
            required
            value={form.permission_key}
            onChange={(e) => {
              const key = e.target.value;
              const p = perms.data?.items.find((x) => x.key === key);
              setForm({ ...form, permission_key: key, effect: allowEligible(p) ? form.effect : "deny" });
            }}
          >
            <option value="">Select a permission…</option>
            {perms.data?.items.map((p) => (
              <option key={p.key} value={p.key}>
                {p.key}
                {p.sensitive ? " (sensitive)" : ""}
              </option>
            ))}
          </select>
        </Field>
        <Field
          label="Effect"
          htmlFor={ids.effect}
          hint={form.permission_key && !allowEligible(selectedPerm) ? "Only deny is allowed for this permission." : undefined}
        >
          <select
            id={ids.effect}
            className="select"
            value={form.effect}
            onChange={(e) => setForm({ ...form, effect: e.target.value as "allow" | "deny" })}
          >
            <option value="deny">deny</option>
            <option value="allow" disabled={!!form.permission_key && !allowEligible(selectedPerm)}>
              allow
            </option>
          </select>
        </Field>
        <Field label="Expires (optional)" htmlFor={ids.exp}>
          <input
            id={ids.exp}
            type="datetime-local"
            className="input"
            value={form.expires}
            onChange={(e) => setForm({ ...form, expires: e.target.value })}
          />
        </Field>
        <Field label="Reason" htmlFor={ids.reason} hint="3–500 characters.">
          <input
            id={ids.reason}
            className="input"
            required
            minLength={3}
            maxLength={500}
            value={form.reason}
            onChange={(e) => setForm({ ...form, reason: e.target.value })}
          />
        </Field>
        <div className="formactions">
          <button type="submit" className="btn primary" disabled={busy}>
            <Icon name="plus" size="sm" /> {busy ? "Saving…" : "Create override"}
          </button>
        </div>
      </form>
      {error && <ErrorState error={error} title="Could not create override" compact />}
      <ReasonDialog action={action} onClose={() => setAction(null)} />
    </Card>
  );
}
