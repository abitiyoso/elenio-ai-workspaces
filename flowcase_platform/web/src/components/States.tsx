import type { ReactNode } from "react";
import type { ApiError } from "../api/client";
import { Icon, type IconName } from "./Icon";

export function Loading({ label = "Loading…" }: { label?: string }) {
  return (
    <div className="state" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}

export function ErrorState({ error, onRetry }: { error: ApiError; onRetry?: () => void }) {
  return (
    <div className="state state-error" role="alert">
      <div className="state-icon"><Icon name="x" /></div>
      <div>
        <strong>{error.status === 0 ? "API unreachable" : "Something went wrong"}</strong>
        <p>{error.message}</p>
        {error.traceId && <p className="tiny muted">Trace ID: <code>{error.traceId}</code></p>}
      </div>
      {onRetry && (
        <button className="btn tinybtn" type="button" onClick={onRetry}>
          <Icon name="refresh" size="sm" /> Retry
        </button>
      )}
    </div>
  );
}

export function EmptyState({ icon, title, children }: { icon: IconName; title: string; children: ReactNode }) {
  return (
    <div className="state state-empty">
      <div className="state-icon"><Icon name={icon} /></div>
      <div>
        <strong>{title}</strong>
        <p>{children}</p>
      </div>
    </div>
  );
}

export function PageHead({ title, subtitle, actions }: { title: string; subtitle: string; actions?: ReactNode }) {
  return (
    <div className="pagehead">
      <div>
        <h1>{title}</h1>
        <p>{subtitle}</p>
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  );
}
