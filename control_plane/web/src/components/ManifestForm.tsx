import { useId, useState } from "react";
import { apiPost, toApiError, type ApiRequestError } from "../api/client";
import { api } from "../api/endpoints";
import type { AppManifest, RegistryApp } from "../api/types";
import { ChipsInput, ErrorState, Field, Modal } from "./ui";

const APP_KEY = /^[a-z][a-z0-9-]{2,63}$/;
const SEMVER = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;
const PERM = /^[a-z][a-z0-9_-]*:[a-z][a-z0-9_.-]*$/;
const ICONS = ["default", "book-open", "workflow", "bot", "panels-top-left"];

type Draft = Omit<AppManifest, "schema_version" | "deployment_mode" | "route_base" | "status" | "requires_domain_authorization" | "workspace_allowlist" | "health_route" | "notes"> & {
  workspace_allowlist: string[];
  health_route: "" | "/healthz" | "/readyz";
  notes: string;
};

function fromManifest(m: AppManifest | undefined): Draft {
  return {
    app_key: m?.app_key ?? "",
    display_name: m?.display_name ?? "",
    description: m?.description ?? "",
    category: m?.category ?? "business",
    icon: m?.icon ?? "default",
    owner_team: m?.owner_team ?? "",
    environment: m?.environment ?? "dev",
    version: m ? "" : "0.1.0",
    required_entitlements: m?.required_entitlements ?? [],
    workspace_allowlist: m?.workspace_allowlist ?? [],
    health_route: m?.health_route ?? "",
    notes: m?.notes ?? "",
  };
}

/** Field names from 422 issues may be "manifest.app_key" or "required_entitlements[0]". */
function normField(f: string): string {
  return f.replace(/^manifest\./, "").replace(/\[\d+\]$/, "").replace(/\.\d+$/, "");
}

export const FORM_FIELDS = [
  "app_key",
  "display_name",
  "description",
  "category",
  "icon",
  "owner_team",
  "environment",
  "version",
  "required_entitlements",
  "workspace_allowlist",
  "health_route",
  "notes",
  "reason",
];

export function ManifestForm({
  open,
  mode,
  base,
  permissionKeys,
  workspaceKeys,
  onClose,
  onSaved,
}: {
  open: boolean;
  mode: "create" | "version";
  base?: AppManifest;
  permissionKeys: string[];
  workspaceKeys: string[];
  onClose: () => void;
  onSaved: (app: RegistryApp) => void;
}) {
  const [d, setD] = useState<Draft>(() => fromManifest(base));
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiRequestError | null>(null);
  const uid = useId();
  const id = (f: string) => `${uid}-${f}`;

  const issues = new Map<string, string>();
  for (const i of error?.issues ?? []) {
    const k = normField(i.field);
    if (!issues.has(k)) issues.set(k, i.message);
  }
  const unmatched = (error?.issues ?? []).filter((i) => !FORM_FIELDS.includes(normField(i.field)));

  const keyError = d.app_key && !APP_KEY.test(d.app_key) ? "3–64 lowercase letters, digits or dashes, starting with a letter." : undefined;
  const versionError = d.version && !SEMVER.test(d.version) ? "Use semantic versioning, e.g. 1.2.0." : undefined;
  const sameVersion = mode === "version" && base && d.version === base.version ? "Versions are immutable — choose a new version." : undefined;
  const routeBase = d.app_key && APP_KEY.test(d.app_key) ? `/apps/${d.app_key}/` : "/apps/<app_key>/";

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD((x) => ({ ...x, [k]: v }));

  const close = () => {
    setError(null);
    setReason("");
    setD(fromManifest(base));
    onClose();
  };

  const submit = async () => {
    if (keyError || versionError || sameVersion) return;
    const manifest: AppManifest = {
      schema_version: "workspace.app.v1",
      app_key: d.app_key,
      display_name: d.display_name.trim(),
      description: d.description.trim(),
      category: d.category,
      icon: d.icon,
      owner_team: d.owner_team.trim(),
      deployment_mode: "same_origin_route",
      route_base: `/apps/${d.app_key}/`,
      environment: d.environment,
      version: d.version.trim(),
      status: "draft",
      required_entitlements: d.required_entitlements,
      requires_domain_authorization: true,
      ...(d.workspace_allowlist.length ? { workspace_allowlist: d.workspace_allowlist } : {}),
      ...(d.health_route ? { health_route: d.health_route } : {}),
      ...(d.notes.trim() ? { notes: d.notes.trim() } : {}),
    };
    setBusy(true);
    setError(null);
    try {
      const path = mode === "create" ? api.admin.apps : `${api.admin.app(d.app_key)}/versions`;
      const app = await apiPost<RegistryApp>(path, { manifest, reason: reason.trim() });
      onSaved(app);
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
      wide
      title={mode === "create" ? "Register mini-app" : `New draft version of ${d.app_key}`}
      description={
        mode === "create"
          ? "Creates a draft. The route is always /apps/<app_key>/ on this origin; the upstream target comes from trusted deployment config, never from this form."
          : "Versions are immutable. The new version is a draft until published."
      }
      onClose={close}
      footer={
        <>
          <button type="button" className="btn" onClick={close}>
            Cancel
          </button>
          <button type="submit" form={`${uid}-form`} className="btn primary" disabled={busy}>
            {busy ? "Saving…" : mode === "create" ? "Create draft" : "Create draft version"}
          </button>
        </>
      }
    >
      <form
        id={`${uid}-form`}
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        {error &&
          (unmatched.length > 0 || error.issues.length === 0 ? (
            <ErrorState error={error} title="The registry rejected this manifest" compact />
          ) : (
            <div className="notice error compact" role="alert">
              <strong>The registry rejected this manifest.</strong> Fix the highlighted fields.
              <div className="errmeta">
                <span>
                  code <code>{error.code}</code>
                </span>
                {error.trace_id && (
                  <span>
                    trace <code>{error.trace_id}</code>
                  </span>
                )}
              </div>
            </div>
          ))}
        <div className="formgrid two">
          <Field label="App key" htmlFor={id("app_key")} hint="Immutable identifier." error={keyError ?? issues.get("app_key")}>
            <input
              id={id("app_key")}
              className="input"
              required
              value={d.app_key}
              readOnly={mode === "version"}
              onChange={(e) => set("app_key", e.target.value.toLowerCase())}
              data-autofocus={mode === "create" ? true : undefined}
            />
          </Field>
          <Field label="Route base (derived)" htmlFor={id("route_base")} hint="Same-origin route; not editable." error={issues.get("route_base")}>
            <input id={id("route_base")} className="input mono" value={routeBase} readOnly aria-readonly="true" />
          </Field>
          <Field label="Display name" htmlFor={id("display_name")} error={issues.get("display_name")}>
            <input id={id("display_name")} className="input" required value={d.display_name} onChange={(e) => set("display_name", e.target.value)} />
          </Field>
          <Field label="Owner team" htmlFor={id("owner_team")} error={issues.get("owner_team")}>
            <input id={id("owner_team")} className="input" required value={d.owner_team} onChange={(e) => set("owner_team", e.target.value)} />
          </Field>
          <Field label="Version" htmlFor={id("version")} hint={base ? `Latest: ${base.version}` : "Semantic version."} error={versionError ?? sameVersion ?? issues.get("version")}>
            <input
              id={id("version")}
              className="input"
              required
              value={d.version}
              onChange={(e) => set("version", e.target.value)}
              data-autofocus={mode === "version" ? true : undefined}
            />
          </Field>
          <Field label="Environment" htmlFor={id("environment")} error={issues.get("environment")}>
            <select id={id("environment")} className="select" value={d.environment} onChange={(e) => set("environment", e.target.value as Draft["environment"])}>
              <option value="dev">dev</option>
              <option value="staging">staging</option>
              <option value="production">production</option>
            </select>
          </Field>
          <Field label="Category" htmlFor={id("category")} error={issues.get("category")}>
            <select id={id("category")} className="select" value={d.category} onChange={(e) => set("category", e.target.value as Draft["category"])}>
              <option value="platform">platform</option>
              <option value="experience">experience</option>
              <option value="business">business</option>
              <option value="admin">admin</option>
            </select>
          </Field>
          <Field label="Icon" htmlFor={id("icon")} error={issues.get("icon")}>
            <select id={id("icon")} className="select" value={d.icon} onChange={(e) => set("icon", e.target.value)}>
              {ICONS.map((i) => (
                <option key={i} value={i}>
                  {i}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Health route" htmlFor={id("health_route")} error={issues.get("health_route")}>
            <select id={id("health_route")} className="select" value={d.health_route} onChange={(e) => set("health_route", e.target.value as Draft["health_route"])}>
              <option value="">none</option>
              <option value="/healthz">/healthz</option>
              <option value="/readyz">/readyz</option>
            </select>
          </Field>
          <Field label="Deployment mode" htmlFor={id("deployment_mode")} hint="Fixed for every mini-app.">
            <input id={id("deployment_mode")} className="input mono" value="same_origin_route" readOnly aria-readonly="true" />
          </Field>
        </div>
        <Field label="Description" htmlFor={id("description")} error={issues.get("description")}>
          <textarea id={id("description")} className="textarea short" value={d.description} onChange={(e) => set("description", e.target.value)} />
        </Field>
        <Field
          label="Required entitlements"
          htmlFor={id("required_entitlements")}
          hint="Permission keys a user needs to open the app. Press Enter to add."
          error={issues.get("required_entitlements")}
        >
          <ChipsInput
            id={id("required_entitlements")}
            values={d.required_entitlements}
            onChange={(v) => set("required_entitlements", v)}
            suggestions={permissionKeys}
            pattern={PERM}
            placeholder="e.g. kms:open"
          />
        </Field>
        <Field
          label="Workspace allowlist (optional)"
          htmlFor={id("workspace_allowlist")}
          hint="Workspace keys. Empty means any workspace where the user is entitled."
          error={issues.get("workspace_allowlist")}
        >
          <ChipsInput
            id={id("workspace_allowlist")}
            values={d.workspace_allowlist}
            onChange={(v) => set("workspace_allowlist", v)}
            suggestions={workspaceKeys}
            pattern={APP_KEY}
            placeholder="workspace key"
          />
        </Field>
        <Field label="Notes (optional)" htmlFor={id("notes")} error={issues.get("notes")}>
          <textarea id={id("notes")} className="textarea short" value={d.notes} onChange={(e) => set("notes", e.target.value)} />
        </Field>
        <Field label="Reason" htmlFor={id("reason")} hint="3–500 characters, recorded in the audit trail." error={issues.get("reason")}>
          <textarea id={id("reason")} className="textarea short" required minLength={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        </Field>
      </form>
    </Modal>
  );
}
