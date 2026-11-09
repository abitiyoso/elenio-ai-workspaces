import { createContext, useContext } from "react";

// The open case publishes its pack-generated navigation here so the shell can render it in the left sidebar,
// separate from the generic app navigation.
export interface CaseNavItem {
  id: string;
  label: string;
  icon?: string;
  to: string;
  badge?: number;
}

export interface CaseNavState {
  caseId: string;
  reference: string;
  packTitle?: string;
  items: CaseNavItem[];
  activeId: string;
}

export const CaseNavContext = createContext<{ nav: CaseNavState | null; setNav: (n: CaseNavState | null) => void }>({ nav: null, setNav: () => undefined });

export function useCaseNav() {
  return useContext(CaseNavContext);
}
