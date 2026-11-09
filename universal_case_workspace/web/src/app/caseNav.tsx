import { useState, type ReactNode } from "react";
import { CaseNavContext, type CaseNavState } from "./caseNavContext";

export function CaseNavProvider({ children }: { children: ReactNode }) {
  const [nav, setNav] = useState<CaseNavState | null>(null);
  return <CaseNavContext.Provider value={{ nav, setNav }}>{children}</CaseNavContext.Provider>;
}
