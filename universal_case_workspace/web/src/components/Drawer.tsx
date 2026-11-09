import { useEffect, useRef, type ReactNode } from "react";
import { Icon } from "./Icon";

/** Modal side drawer: Escape and click-away close it, focus moves in on open and back on close. */
export function Drawer({ title, subtitle, onClose, children, footer, labelId }: {
  title: string; subtitle?: ReactNode; onClose: () => void; children: ReactNode; footer?: ReactNode; labelId: string;
}) {
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    panel.current?.focus();
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      prev?.focus?.();
    };
  }, [onClose]);
  return (
    <>
      <div className="drawer-back show" onClick={onClose} aria-hidden="true" />
      <div className="drawer show" role="dialog" aria-modal="true" aria-labelledby={labelId} ref={panel} tabIndex={-1}>
        <div className="drawer-head">
          <div><h2 id={labelId}>{title}</h2>{subtitle && <p>{subtitle}</p>}</div>
          <button className="btn sm" type="button" onClick={onClose} aria-label="Close"><Icon name="x" size="sm" /></button>
        </div>
        <div className="drawer-body">{children}</div>
        {footer && <div className="drawer-foot">{footer}</div>}
      </div>
    </>
  );
}
