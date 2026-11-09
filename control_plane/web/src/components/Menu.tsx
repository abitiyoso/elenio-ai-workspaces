import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

/**
 * Keyboard-operable disclosure menu: Enter/Space/ArrowDown opens, arrows move,
 * Escape closes and returns focus to the trigger, click outside closes.
 */
export function Menu({
  label,
  trigger,
  triggerClassName,
  children,
  align = "right",
}: {
  label: string;
  trigger: ReactNode;
  triggerClassName?: string;
  children: (close: () => void) => ReactNode;
  align?: "left" | "right";
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const triggerId = useId();

  useEffect(() => {
    if (!open) return;
    const first = rootRef.current?.querySelector<HTMLElement>('[role="menuitem"]:not([disabled]), [role="menuitemradio"]:not([disabled])');
    first?.focus();
    const onDoc = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  const close = () => {
    setOpen(false);
    document.getElementById(triggerId)?.focus();
  };

  const onMenuKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const items = Array.from(
      rootRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled]), [role="menuitemradio"]:not([disabled])') ?? [],
    );
    const idx = items.indexOf(document.activeElement as HTMLElement);
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      items[(idx + 1) % items.length]?.focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      items[(idx - 1 + items.length) % items.length]?.focus();
    } else if (e.key === "Home") {
      e.preventDefault();
      items[0]?.focus();
    } else if (e.key === "End") {
      e.preventDefault();
      items[items.length - 1]?.focus();
    } else if (e.key === "Tab") {
      setOpen(false);
    }
  };

  return (
    <div className="menuroot" ref={rootRef}>
      <button
        id={triggerId}
        type="button"
        className={triggerClassName}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        aria-label={label}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !open) {
            e.preventDefault();
            setOpen(true);
          } else if (e.key === "Escape" && open) {
            e.preventDefault();
            setOpen(false);
          }
        }}
      >
        {trigger}
      </button>
      {open && (
        <div id={menuId} role="menu" aria-label={label} className={`menu ${align}`} onKeyDown={onMenuKey}>
          {children(close)}
        </div>
      )}
    </div>
  );
}
