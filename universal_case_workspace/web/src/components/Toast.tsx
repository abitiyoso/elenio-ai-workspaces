import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { ToastContext } from "./toastContext";

export function ToastProvider({ children }: { children: ReactNode }) {
  const [msg, setMsg] = useState<string | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const show = useCallback((m: string) => {
    setMsg(m);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setMsg(null), 4200);
  }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <ToastContext.Provider value={show}>
      {children}
      <div className={msg ? "toast show" : "toast"} role="status" aria-live="polite">{msg}</div>
    </ToastContext.Provider>
  );
}
