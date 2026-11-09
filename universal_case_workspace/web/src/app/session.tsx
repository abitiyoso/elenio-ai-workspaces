import { useEffect, useState, type ReactNode } from "react";
import { onSessionEnded } from "../api/client";
import { useResource } from "../api/useResource";
import { getMe } from "../api/workspace";
import { SessionContext } from "./sessionContext";

export function SessionProvider({ children }: { children: ReactNode }) {
  const { res } = useResource("me", getMe);
  const [ended, setEnded] = useState(false);
  useEffect(() => onSessionEnded(() => setEnded(true)), []);
  return <SessionContext.Provider value={{ me: res, ended: ended || (res.state === "error" && res.error.status === 401) }}>{children}</SessionContext.Provider>;
}
