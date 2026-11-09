import { useId, useState } from "react";
import { useSession } from "../session/context";
import { useApi } from "../lib/useApi";
import type { AdminWorkspace } from "../lib/workspaces";
import { api } from "../api/endpoints";
import { apiPost, toApiError, type ApiRequestError } from "../api/client";
import type { Membership, Workspace } from "../api/types";
import { fmtDate, initials } from "../lib/format";
import { Icon } from "../components/Icons";
import { Async, Card, EmptyState, ErrorState, Field, Modal, PageHead, Pill, Skeleton, StatusPill, useToast } from "../components/ui";
import { canSeeWorkspaceAdmin } from "../components/nav";

type Member = Membership & { user: { id: string; display_name: string; email: string | null; status: "active" | "suspended" } };

export function WorkspacesPage() {
  const { me, switchWorkspace } = useSession();
  const toast = useToast();
  const [switching, setSwitching] = useState<string | null>(null);
  const [switchError, setSwitchError] = useState<ApiRequestError | null>(null);
  const admin = canSeeWorkspaceAdmin(me.capabilities);
  const [createOpen, setCreateOpen] = useState(false);

  const doSwitch = async (w: Workspace) => {
    setSwitching(w.id);
    setSwitchError(null);
    try {
      await switchWorkspace(w.id);
      toast(`Switched to ${w.name}`);
    } catch (err) {
      setSwitchError(toApiError(err));
    } finally {
      setSwitching(null);
    }
  };

  return (
    <>
      <PageHead
        eyebrow="WORKSPACE"
        title="Workspaces"
        actions={
          me.capabilities.manage_workspaces && (
            <button type="button" className="btn primary" onClick={() => setCreateOpen(true)}>
              <Icon name="plus" size="sm" /> New workspace
            </button>
          )
        }
      >
        Workspaces scope membership, roles and app entitlements. Switching changes your server-side session; the app list
        is recomputed by the control plane.
      </PageHead>

      {switchError && <ErrorState error={switchError} title="Could not switch workspace" />}

      <h2 className="sectiontitle">My workspaces</h2>
      {me.workspaces.length === 0 ? (
        <div className="card cardbody">
          <EmptyState title="You are not a member of any workspace" icon="layers">
            Ask a workspace admin to add you.
          </EmptyState>
        </div>
      ) : (
        <div className="wsgrid">
          {me.workspaces.map((w) => {
            const current = me.workspace?.id === w.id;
            return (
              <article key={w.id} className={`card wscard${current ? " current" : ""}`} aria-current={current ? "true" : undefined}>
                <div className="orgsymbol light" aria-hidden="true">
                  {initials(w.name)}
                </div>
                <h3>{w.name}</h3>
                <p>{w.description || "No description."}</p>
                <div className="wsfoot">
                  <code className="tiny">{w.key}</code>
                  {current ? (
                    <Pill tone="ok">active</Pill>
                  ) : (
                    <button
                      type="button"
                      className="btn sm soft"
                      onClick={() => void doSwitch(w)}
                      disabled={switching !== null}
                    >
                      {switching === w.id ? "Switching…" : "Switch here"}
                    </button>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      )}

      {admin && <AdminWorkspaces />}
      {me.capabilities.manage_workspaces && <CreateWorkspaceDialog open={createOpen} onClose={() => setCreateOpen(false)} />}
    </>
  );
}

function AdminWorkspaces() {
  const list = useApi<{ items: AdminWorkspace[] }>(api.admin.workspaces);
  const [openId, setOpenId] = useState<string | null>(null);
  return (
    <>
      <h2 className="sectiontitle">Administered workspaces</h2>
      <Card pad={false}>
        <Async state={list} skeleton={<div className="cardbody"><Skeleton lines={4} /></div>} title="Could not load workspaces">
          {({ items }) =>
            items.length === 0 ? (
              <EmptyState title="No workspaces" icon="layers" />
            ) : (
              <div className="tablewrap">
                <table className="tbl">
                  <thead>
                    <tr>
                      <th scope="col">Workspace</th>
                      <th scope="col">Key</th>
                      <th scope="col">Status</th>
                      <th scope="col">Members</th>
                      <th scope="col">
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((w) => (
                      <WorkspaceRow key={w.id} w={w} open={openId === w.id} onToggle={() => setOpenId(openId === w.id ? null : w.id)} />
                    ))}
                  </tbody>
                </table>
              </div>
            )
          }
        </Async>
      </Card>
    </>
  );
}

function WorkspaceRow({ w, open, onToggle }: { w: AdminWorkspace; open: boolean; onToggle: () => void }) {
  const panelId = useId();
  return (
    <>
      <tr>
        <td>
          <span className="cellmain">{w.name}</span>
          <span className="cellsub">{w.description}</span>
        </td>
        <td>
          <code>{w.key}</code>
        </td>
        <td>
          <StatusPill value={w.status} />
        </td>
        <td>{w.member_count}</td>
        <td className="right">
          <button type="button" className="btn sm" aria-expanded={open} aria-controls={panelId} onClick={onToggle}>
            {open ? "Hide members" : "Members"}
          </button>
        </td>
      </tr>
      {open && (
        <tr className="expandrow">
          <td colSpan={5} id={panelId}>
            <MembersList workspaceId={w.id} />
          </td>
        </tr>
      )}
    </>
  );
}

function MembersList({ workspaceId }: { workspaceId: string }) {
  const members = useApi<{ items: Member[] }>(api.admin.members(workspaceId));
  return (
    <Async state={members} skeleton={<Skeleton lines={3} />} title="Could not load members">
      {({ items }) =>
        items.length === 0 ? (
          <p className="muted">No members.</p>
        ) : (
          <ul className="memberlist">
            {items.map((m) => (
              <li key={m.id}>
                <div className="user-cell">
                  <div className="avatar-sm" aria-hidden="true">
                    {initials(m.user.display_name)}
                  </div>
                  <div>
                    <strong>{m.user.display_name}</strong>
                    <small>
                      {m.user.email ?? "no email"} · joined {fmtDate(m.joined_at)}
                    </small>
                  </div>
                </div>
                <div className="chiprow">
                  {m.roles
                    .filter((r) => r.state === "active")
                    .map((r) => (
                      <span key={r.id} className="rolechip">
                        {r.role_key}
                      </span>
                    ))}
                </div>
                <div className="memberstate">
                  <StatusPill value={m.state} />
                  {m.user.status === "suspended" && <StatusPill value="suspended" label="user suspended" />}
                </div>
              </li>
            ))}
          </ul>
        )
      }
    </Async>
  );
}

const KEY_PATTERN = /^[a-z][a-z0-9-]{2,63}$/;

function CreateWorkspaceDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { refresh } = useSession();
  const toast = useToast();
  const empty = { key: "", name: "", description: "", reason: "" };
  const [form, setForm] = useState(empty);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiRequestError | null>(null);
  const ids = { key: useId(), name: useId(), desc: useId(), reason: useId() };
  const keyInvalid = form.key !== "" && !KEY_PATTERN.test(form.key);
  const issueFor = (f: string) => error?.issues.find((i) => i.field === f)?.message;
  const close = () => {
    setForm(empty);
    setError(null);
    onClose();
  };
  const submit = async () => {
    if (keyInvalid) return;
    setBusy(true);
    setError(null);
    try {
      const w = await apiPost<Workspace>(api.admin.workspaces, {
        key: form.key,
        name: form.name.trim(),
        description: form.description.trim(),
        reason: form.reason.trim(),
      });
      toast(`Workspace ${w.name} created.`);
      close();
      await refresh();
    } catch (err) {
      setError(toApiError(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      open={open}
      title="Create workspace"
      description="New workspaces start empty. Add members and grant roles from People & Teams."
      onClose={close}
      footer={
        <>
          <button type="button" className="btn" onClick={close}>
            Cancel
          </button>
          <button type="submit" form="ws-form" className="btn primary" disabled={busy}>
            {busy ? "Creating…" : "Create workspace"}
          </button>
        </>
      }
    >
      <form
        id="ws-form"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field
          label="Key"
          htmlFor={ids.key}
          hint="Lowercase letters, digits and dashes; 3–64 chars, starts with a letter."
          error={keyInvalid ? "Use 3–64 lowercase letters, digits or dashes, starting with a letter." : issueFor("key")}
        >
          <input
            id={ids.key}
            className="input"
            required
            value={form.key}
            onChange={(e) => setForm({ ...form, key: e.target.value.toLowerCase() })}
            data-autofocus
          />
        </Field>
        <Field label="Name" htmlFor={ids.name} error={issueFor("name")}>
          <input id={ids.name} className="input" required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </Field>
        <Field label="Description" htmlFor={ids.desc} error={issueFor("description")}>
          <textarea
            id={ids.desc}
            className="textarea short"
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </Field>
        <Field label="Reason" htmlFor={ids.reason} hint="3–500 characters, recorded in the audit trail." error={issueFor("reason")}>
          <textarea
            id={ids.reason}
            className="textarea short"
            required
            minLength={3}
            maxLength={500}
            value={form.reason}
            onChange={(e) => setForm({ ...form, reason: e.target.value })}
          />
        </Field>
        {error && <ErrorState error={error} title="Could not create workspace" compact />}
      </form>
    </Modal>
  );
}
