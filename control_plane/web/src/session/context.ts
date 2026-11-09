import { createContext, useContext } from "react";
import type { Me } from "../api/types";

export interface SessionValue {
  me: Me;
  /** Bumped whenever session-scoped data (active workspace) changes; data hooks refetch on change. */
  epoch: number;
  refresh: () => Promise<void>;
  switchWorkspace: (workspaceId: string) => Promise<void>;
  signOut: () => Promise<void>;
}

export const SessionContext = createContext<SessionValue | null>(null);
/** Epoch alone, so data hooks work outside a session (e.g. tests) with epoch 0. */
export const EpochContext = createContext<number>(0);

export function useSession(): SessionValue {
  const v = useContext(SessionContext);
  if (!v) throw new Error("useSession must be used inside <SessionProvider>");
  return v;
}

export function useMe(): Me {
  return useSession().me;
}
