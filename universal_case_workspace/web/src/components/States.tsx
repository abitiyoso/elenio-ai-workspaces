import type { ReactNode } from "react";
import { describeError, type ApiError } from "../api/client";
import type { PartialError } from "../api/types";
import { Icon } from "./Icon";

export function Loading({ label = "Loading…" }: { label?: string }) {
  return (
    <div className="loading" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" /> {label}
    </div>
  );
}

export function ErrorState({ error, onRetry, compact }: { error: ApiError; onRetry?: () => void; compact?: boolean }) {
  const d = describeError(error);
  return (
    <div className={compact ? "notice error" : "card pad error-state"} role="alert">
      <strong className="error-title">{d.title}</strong>
      <p>{d.message}</p>
      {error.details.length > 0 && (
        <ul className="error-details">{error.details.map((x, i) => <li key={i}>{x.path && <code>{x.path}</code>} {x.message}</li>)}</ul>
      )}
      {error.status === 401 && <a className="btn primary sm" href="/">Open the Control Plane</a>}
      {d.reload && onRetry && <button className="btn sm" type="button" onClick={onRetry}><Icon name="refresh" size="sm" /> Reload</button>}
      {error.traceId && <p className="muted-mini">Trace {error.traceId}</p>}
    </div>
  );
}

export function EmptyState({ icon = "files", title, children }: { icon?: string; title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <div className="square"><Icon name={icon} /></div>
      <strong>{title}</strong>
      {children && <p>{children}</p>}
    </div>
  );
}

const sourceLabel: Record<string, string> = { flowcase: "Flowcase", agent_studio: "Agent Studio", kms: "KMS", registry: "Pack registry" };
const partialSourceName = (s: string) => sourceLabel[s] ?? s;

/** One banner per failing upstream ("Flowcase unavailable — showing partial data"). */
export function PartialErrorBanner({ errors, what = "showing partial data" }: { errors?: PartialError[] | undefined; what?: string }) {
  if (!errors || errors.length === 0) return null;
  return (
    <div className="banner-stack">
      {errors.map((e, i) => (
        <div className="notice warning banner" role="alert" key={`${e.source}-${i}`}>
          <Icon name="alert-circle" size="sm" />
          <span><strong>{partialSourceName(e.source)} unavailable</strong> — {what}. <span className="muted-inline">{e.message}</span></span>
        </div>
      ))}
    </div>
  );
}

/** Per-panel error card used when a panel's upstream failed but the rest of the page rendered. */
export function PanelError({ error }: { error: PartialError }) {
  return (
    <div className="notice error panel-error" role="alert">
      <strong>{partialSourceName(error.source)} unavailable.</strong> This panel could not load ({error.code}). {error.message}
    </div>
  );
}

export function PageHead({ eyebrow, title, subtitle, actions }: { eyebrow?: string; title: string; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="pagehead">
      <div>
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <h1>{title}</h1>
        {subtitle && <p>{subtitle}</p>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  );
}
