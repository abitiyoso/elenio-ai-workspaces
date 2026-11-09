import { createContext, useContext, type ReactNode } from "react";
import type { Session } from "../../api/types";
import { useResource, type Resource } from "../../api/useResource";

interface SessionValue {
  resource: Resource<Session>;
  reload: () => void;
}

const SessionContext = createContext<SessionValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [resource, reload] = useResource<Session>("/api/v1/session");
  return <SessionContext.Provider value={{ resource, reload }}>{children}</SessionContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useSession(): SessionValue {
  const v = useContext(SessionContext);
  if (!v) throw new Error("useSession must be used inside SessionProvider");
  return v;
}

// eslint-disable-next-line react-refresh/only-export-components
export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const letters = parts.length > 1 ? (parts[0]?.[0] ?? "") + (parts[parts.length - 1]?.[0] ?? "") : (parts[0]?.slice(0, 2) ?? "");
  return letters.toUpperCase() || "?";
}

/** The verified session, or null while loading or on error. */
// eslint-disable-next-line react-refresh/only-export-components
export function useCurrentSession(): Session | null {
  const { resource } = useSession();
  return resource.state === "ready" ? resource.data : null;
}
