import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from "react";
import type { ApiRequestError } from "../api/client";
import { toApiError } from "../api/client";
import type { AppHealth } from "../api/types";
import { localInputToIso, validReason } from "../lib/format";
import { Icon } from "./Icons";

// ---------- badges ----------

export type Tone = "ok" | "warn" | "fail" | "info" | "neutral";

export function Pill({ tone = "neutral", children, title }: { tone?: Tone; children: ReactNode; title?: string }) {
  return (
    <span className={`pill${tone === "neutral" ? "" : ` ${tone}`}`} title={title}>
      {children}
    </span>
  );
}

const statusTone: Record<string, Tone> = {
  active: "ok",
  published: "ok",
  allow: "ok",
  healthy: "ok",
  ok: "ok",
  approved: "ok",
  granted: "ok",
  draft: "warn",
  pending: "warn",
  pending_approval: "warn",
  unknown: "neutral",
  not_configured: "warn",
  suspended: "warn",
  superseded: "neutral",
  archived: "neutral",
  expired: "neutral",
  cancelled: "neutral",
  disabled: "fail",
  retired: "neutral",
  revoked: "fail",
  rejected: "fail",
  deny: "fail",
  unhealthy: "fail",
  unavailable: "fail",
};

export function StatusPill({ value, label }: { value: string; label?: string }) {
  return <Pill tone={statusTone[value] ?? "neutral"}>{label ?? value.replace(/_/g, " ")}</Pill>;
}

export function HealthPill({ health }: { health: AppHealth }) {
  const label =
    health.status === "not_configured" ? "health not configured" : health.status === "unknown" ? "health unknown" : health.status;
  return (
    <Pill tone={statusTone[health.status] ?? "neutral"} title={health.detail || undefined}>
      {label}
    </Pill>
  );
}

export function IntegrationPill({ integration }: { integration: "proxy" | "pending" }) {
  return integration === "proxy" ? (
    <Pill tone="info" title="Requests are forwarded with an audience-bound service token">
      integrated
    </Pill>
  ) : (
    <Pill tone="warn" title="The service has not adopted the Control Plane identity contract yet">
      integration pending
    </Pill>
  );
}

// ---------- states ----------

export function ErrorState({
  error,
  title = "Something went wrong",
  onRetry,
  compact,
}: {
  error: ApiRequestError | unknown;
  title?: string;
  onRetry?: () => void;
  compact?: boolean;
}) {
  const e = toApiError(error);
  const forbidden = e.status === 403;
  return (
    <div className={`notice error${compact ? " compact" : ""}`} role="alert">
      <strong>{forbidden ? "Not permitted" : title}</strong>
      <div>{e.message}</div>
      <div className="errmeta">
        <span>
          code <code>{e.code}</code>
        </span>
        {e.status > 0 && <span>HTTP {e.status}</span>}
        {e.trace_id && (
          <span>
            trace <code>{e.trace_id}</code>
          </span>
        )}
      </div>
      {e.issues.length > 0 && (
        <ul className="issues">
          {e.issues.map((i, idx) => (
            <li key={idx}>
              <code>{i.field}</code>: {i.message}
            </li>
          ))}
        </ul>
      )}
      {onRetry && e.status !== 401 && (
        <button type="button" className="btn sm" onClick={onRetry}>
          <Icon name="refresh" size="sm" /> Retry
        </button>
      )}
    </div>
  );
}

export function EmptyState({ title, children, icon = "info" }: { title: string; children?: ReactNode; icon?: string }) {
  return (
    <div className="empty">
      <div className="emptyico" aria-hidden="true">
        <Icon name={icon} />
      </div>
      <strong>{title}</strong>
      {children && <p>{children}</p>}
    </div>
  );
}

export function Skeleton({ lines = 3, height }: { lines?: number; height?: number }) {
  return (
    <div className="skeleton" role="status" aria-label="Loading">
      {Array.from({ length: lines }, (_, i) => (
        <span key={i} style={height ? { height } : undefined} />
      ))}
    </div>
  );
}

/** Standard loading / error / content switch for useApi results. */
export function Async<T>({
  state,
  children,
  skeleton,
  title,
}: {
  state: { data: T | undefined; error: ApiRequestError | undefined; loading: boolean; reload: () => void };
  children: (data: T) => ReactNode;
  skeleton?: ReactNode;
  title?: string;
}) {
  if (state.error) return <ErrorState error={state.error} title={title} onRetry={state.reload} />;
  if (state.data === undefined) return <>{skeleton ?? <Skeleton />}</>;
  return <>{children(state.data)}</>;
}

// ---------- layout ----------

export function PageHead({
  eyebrow,
  title,
  children,
  actions,
}: {
  eyebrow: string;
  title: string;
  children?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="pagehead">
      <div>
        <div className="eyebrow">{eyebrow}</div>
        <h1>{title}</h1>
        {children && <p>{children}</p>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </header>
  );
}

export function Card({
  title,
  subtitle,
  actions,
  children,
  pad = true,
  className,
  id,
}: {
  title?: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  pad?: boolean;
  className?: string;
  id?: string;
}) {
  const headId = useId();
  return (
    <section className={`card${className ? ` ${className}` : ""}`} aria-labelledby={title ? headId : undefined} id={id}>
      {title && (
        <div className={`cardhead${pad ? "" : " padded"}`}>
          <div>
            <h2 id={headId}>{title}</h2>
            {subtitle && <p>{subtitle}</p>}
          </div>
          {actions && <div className="actions">{actions}</div>}
        </div>
      )}
      <div className={pad ? "cardbody" : undefined}>{children}</div>
    </section>
  );
}

export function KV({ k, children }: { k: string; children: ReactNode }) {
  return (
    <div className="kv">
      <span>{k}</span>
      <strong>{children}</strong>
    </div>
  );
}

export function Field({
  label,
  hint,
  error,
  children,
  htmlFor,
}: {
  label: string;
  hint?: ReactNode;
  error?: string;
  children: ReactNode;
  htmlFor: string;
}) {
  return (
    <div className={`field${error ? " invalid" : ""}`}>
      <label htmlFor={htmlFor}>{label}</label>
      {children}
      {hint && !error && <small id={`${htmlFor}-hint`}>{hint}</small>}
      {error && (
        <small className="fielderror" id={`${htmlFor}-error`} role="alert">
          {error}
        </small>
      )}
    </div>
  );
}

// ---------- modal / drawer ----------

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function Modal({
  open,
  title,
  description,
  onClose,
  children,
  footer,
  variant = "dialog",
  wide,
}: {
  open: boolean;
  title: string;
  description?: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  variant?: "dialog" | "drawer";
  wide?: boolean;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const descId = useId();
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) return;
    const previouslyFocused = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    const first =
      panel?.querySelector<HTMLElement>("[data-autofocus]") ?? panel?.querySelector<HTMLElement>(FOCUSABLE) ?? panel;
    first?.focus();
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prevOverflow;
      previouslyFocused?.focus?.();
    };
  }, [open]);

  if (!open) return null;

  const onKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      onCloseRef.current();
      return;
    }
    if (e.key === "Tab" && panelRef.current) {
      const items = Array.from(panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE));
      if (items.length === 0) return;
      const firstEl = items[0]!;
      const lastEl = items[items.length - 1]!;
      if (e.shiftKey && document.activeElement === firstEl) {
        e.preventDefault();
        lastEl.focus();
      } else if (!e.shiftKey && document.activeElement === lastEl) {
        e.preventDefault();
        firstEl.focus();
      }
    }
  };

  return (
    <div
      className={`backdrop ${variant}`}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={panelRef}
        className={`${variant === "drawer" ? "drawerpanel" : "modal"}${wide ? " wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descId : undefined}
        tabIndex={-1}
        onKeyDown={onKeyDown}
      >
        <div className="modalhead">
          <div>
            <h2 id={titleId}>{title}</h2>
            {description && (
              <div className="modaldesc" id={descId}>
                {description}
              </div>
            )}
          </div>
          <button type="button" className="iconbtn" onClick={onClose} aria-label="Close">
            <Icon name="x" />
          </button>
        </div>
        <div className="modalbody">{children}</div>
        {footer && <div className="modalfooter">{footer}</div>}
      </div>
    </div>
  );
}

// ---------- reason-gated admin action ----------

export interface ChoiceOption {
  value: string;
  label: string;
  /** Shown under the select when this option is chosen (e.g. "requires independent approval"). */
  note?: string;
}

export interface PendingAction {
  title: string;
  description?: ReactNode;
  confirmLabel: string;
  danger?: boolean;
  /** Optional select rendered above the reason box. */
  choice?: { label: string; options: ChoiceOption[]; emptyText?: string };
  /** Optional expiry (datetime-local) input. */
  withExpiry?: boolean;
  /** Return a toast message on success. Throw to show the error inline. */
  run: (reason: string, extras: { choice: string; expiresAt?: string }) => Promise<string | void>;
}

/** Admin writes require a 3–500 char reason; this dialog collects it and runs the action. */
export function ReasonDialog({ action, onClose }: { action: PendingAction | null; onClose: () => void }) {
  const toast = useToast();
  const [reason, setReason] = useState("");
  const [choice, setChoice] = useState("");
  const [expires, setExpires] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiRequestError | null>(null);
  const [touched, setTouched] = useState(false);
  const id = useId();
  const choiceId = useId();
  const expId = useId();

  const options = action?.choice?.options ?? [];
  const selected = options.find((o) => o.value === choice) ?? options[0];
  const effectiveChoice = selected?.value ?? "";

  const close = () => {
    setReason("");
    setChoice("");
    setExpires("");
    setError(null);
    setTouched(false);
    setBusy(false);
    onClose();
  };

  const submit = async () => {
    setTouched(true);
    if (!action || !validReason(reason)) return;
    if (action.choice && !effectiveChoice) return;
    setBusy(true);
    setError(null);
    try {
      const msg = await action.run(reason.trim(), { choice: effectiveChoice, expiresAt: localInputToIso(expires) });
      if (msg) toast(msg);
      close();
    } catch (err) {
      setError(toApiError(err));
      setBusy(false);
    }
  };

  const invalid = touched && !validReason(reason);
  const noChoice = Boolean(action?.choice) && options.length === 0;
  return (
    <Modal
      open={action !== null}
      title={action?.title ?? ""}
      description={action?.description}
      onClose={close}
      footer={
        <>
          <button type="button" className="btn" onClick={close}>
            Cancel
          </button>
          <button
            type="button"
            className={`btn ${action?.danger ? "dangerfill" : "primary"}`}
            onClick={() => void submit()}
            disabled={busy || noChoice}
          >
            {busy ? "Working…" : action?.confirmLabel}
          </button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        {action?.choice &&
          (noChoice ? (
            <p className="notice warning">{action.choice.emptyText ?? "Nothing to choose from."}</p>
          ) : (
            <Field label={action.choice.label} htmlFor={choiceId} hint={selected?.note}>
              <select
                id={choiceId}
                className="select"
                value={effectiveChoice}
                onChange={(e) => setChoice(e.target.value)}
                data-autofocus
              >
                {options.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </Field>
          ))}
        {action?.withExpiry && (
          <Field label="Expires (optional)" htmlFor={expId} hint="Leave empty for no expiry. Time-boxed grants are preferred.">
            <input
              id={expId}
              type="datetime-local"
              className="input"
              value={expires}
              onChange={(e) => setExpires(e.target.value)}
            />
          </Field>
        )}
        <Field
          label="Reason (recorded in the audit trail)"
          htmlFor={id}
          hint="3–500 characters."
          error={invalid ? "Enter a reason between 3 and 500 characters." : undefined}
        >
          <textarea
            id={id}
            className="textarea"
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
            aria-invalid={invalid}
            aria-describedby={invalid ? `${id}-error` : `${id}-hint`}
            data-autofocus={action?.choice ? undefined : true}
          />
        </Field>
        {error && <ErrorState error={error} title="The server refused this action" compact />}
      </form>
    </Modal>
  );
}

// ---------- toasts ----------

const ToastContext = createContext<(msg: string) => void>(() => {});

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<{ id: number; msg: string }[]>([]);
  const nextId = useRef(1);
  const push = useCallback((msg: string) => {
    const id = nextId.current++;
    setToasts((t) => [...t, { id, msg }]);
    window.setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 5000);
  }, []);
  return (
    <ToastContext.Provider value={push}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className="toast">
            {t.msg}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast() {
  return useContext(ToastContext);
}

// ---------- tabs ----------

export function Tabs<K extends string>({
  tabs,
  value,
  onChange,
  label,
}: {
  tabs: { key: K; label: ReactNode }[];
  value: K;
  onChange: (k: K) => void;
  label: string;
}) {
  const onKey = (e: ReactKeyboardEvent<HTMLButtonElement>, idx: number) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
    e.preventDefault();
    const next = tabs[(idx + (e.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length]!;
    onChange(next.key);
    const btn = e.currentTarget.parentElement?.querySelector<HTMLButtonElement>(`[data-tab="${next.key}"]`);
    btn?.focus();
  };
  return (
    <div className="tabs" role="tablist" aria-label={label}>
      {tabs.map((t, idx) => (
        <button
          key={t.key}
          type="button"
          role="tab"
          data-tab={t.key}
          aria-selected={value === t.key}
          tabIndex={value === t.key ? 0 : -1}
          className={`tab${value === t.key ? " active" : ""}`}
          onClick={() => onChange(t.key)}
          onKeyDown={(e) => onKey(e, idx)}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}

// ---------- chips input ----------

export function ChipsInput({
  id,
  values,
  onChange,
  suggestions,
  placeholder,
  pattern,
  describedBy,
}: {
  id: string;
  values: string[];
  onChange: (v: string[]) => void;
  suggestions?: string[];
  placeholder?: string;
  pattern?: RegExp;
  describedBy?: string;
}) {
  const [draft, setDraft] = useState("");
  const listId = `${id}-list`;
  const add = (raw: string) => {
    const v = raw.trim();
    if (!v || values.includes(v) || (pattern && !pattern.test(v))) return;
    onChange([...values, v]);
    setDraft("");
  };
  return (
    <div className="chips">
      {values.map((v) => (
        <span key={v} className="rolechip removable">
          {v}
          <button type="button" aria-label={`Remove ${v}`} onClick={() => onChange(values.filter((x) => x !== v))}>
            <Icon name="x" size="sm" />
          </button>
        </span>
      ))}
      <input
        id={id}
        className="chipinput"
        value={draft}
        list={suggestions ? listId : undefined}
        placeholder={placeholder}
        aria-describedby={describedBy}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === ",") {
            e.preventDefault();
            add(draft);
          } else if (e.key === "Backspace" && !draft && values.length) {
            onChange(values.slice(0, -1));
          }
        }}
        onBlur={() => add(draft)}
      />
      {suggestions && (
        <datalist id={listId}>
          {suggestions
            .filter((s) => !values.includes(s))
            .map((s) => (
              <option key={s} value={s} />
            ))}
        </datalist>
      )}
    </div>
  );
}
