import type { CaseContext, NavItem } from "../../../api/types";
import type { DraftActions } from "../DraftReview";

export interface ModuleProps {
  ctx: CaseContext;
  nav: NavItem;
  navigation: NavItem[];
  actions: DraftActions;
  caseBase: string;
}
