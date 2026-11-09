import { createContext, useContext } from "react";
import type { MeContext } from "../api/types";
import type { Resource } from "../api/useResource";

export interface SessionValue {
  me: Resource<MeContext>;
  ended: boolean;
}

export const SessionContext = createContext<SessionValue>({ me: { state: "loading" }, ended: false });

export function useSession() {
  return useContext(SessionContext);
}
