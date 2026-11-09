// Derived, pack-agnostic view state for the Case Workspace. Everything here reads the case context and the pinned
// manifest only; there is no knowledge of any particular domain pack.
import type { AgentRun, CaseArtifact, CaseContext, CitedSource, DraftReview, NavItem, PackArtifactType, PartialError, WorkflowNode } from "../../api/types";
import { artifactSources, isObject } from "../../components/artifactContent";

/** Timing knobs (tests shorten them). */
export const caseTiming = { pollMs: 4000, revisionWatchMs: 180_000 };

export const navId = (n: NavItem) => n.id ?? n.key ?? n.view;

/** Safe generic shell used when the pinned pack cannot be loaded: no artifact views, only Flowcase-backed ones. */
export const genericNavigation: NavItem[] = [
  { id: "overview", view: "overview", label: "Overview", icon: "layout-dashboard" },
  { id: "tasks", view: "tasks", label: "Tasks", icon: "check-square" },
  { id: "approvals", view: "approvals", label: "Approvals", icon: "shield-check" },
  { id: "documents", view: "documents", label: "Documents", icon: "files" },
  { id: "ai", view: "ai_activity", label: "AI Activity", icon: "bot" },
  { id: "timeline", view: "timeline", label: "Timeline", icon: "clock-3" },
];

export function effectiveNavigation(ctx: CaseContext): NavItem[] {
  if (!ctx.pack || ctx.pack.status === "unavailable" || !ctx.navigation?.length) return genericNavigation;
  return ctx.navigation;
}

const viewIcons: Record<string, string> = {
  overview: "layout-dashboard", artifact_list: "list-checks", tasks: "check-square", approvals: "shield-check",
  documents: "files", ai_activity: "bot", timeline: "clock-3",
};
export const navIcon = (n: NavItem) => n.icon ?? viewIcons[n.view] ?? "dot";

/** Which upstreams each view depends on, so a failing one gets a per-panel error card. */
export const viewSources: Record<string, string[]> = {
  overview: ["flowcase"],
  artifact_list: ["flowcase"],
  tasks: ["flowcase"],
  approvals: ["flowcase"],
  documents: ["kms", "flowcase"],
  ai_activity: ["agent_studio", "flowcase"],
  timeline: ["flowcase"],
};

export function errorsFor(ctx: CaseContext, sources: string[]): PartialError[] {
  return (ctx.partial_errors ?? []).filter((e) => sources.includes(e.source));
}

export function nodeName(nodes: WorkflowNode[] | undefined, id?: string | null): string {
  if (!id) return "—";
  return nodes?.find((n) => n.id === id)?.name || id;
}

export function artifactsForType(ctx: CaseContext, typeKey: string): CaseArtifact[] {
  return ctx.artifacts
    .filter((a) => a.pack_artifact_type === typeKey)
    .sort((a, b) => (b.version ?? 0) - (a.version ?? 0));
}

export function findArtifactType(ctx: CaseContext, key?: string): PackArtifactType | undefined {
  return key ? ctx.artifact_types.find((t) => t.key === key) : undefined;
}

const liveRunStatuses = new Set(["queued", "pending", "ready", "running", "submitted", "waiting_external", "in_progress", "accepted"]);
export const isLiveRun = (r: AgentRun) => !!r.status && liveRunStatuses.has(r.status);

/** True when the artifact came from an agent and has not been approved by a person yet. */
export function isUnreviewedAgentOutput(a: CaseArtifact, ctx: Pick<CaseContext, "agent_runs" | "workflow">): boolean {
  if (a.status === "approved") return false;
  const byRun = !!a.producer_node_id && ctx.agent_runs.some((r) => r.node_id === a.producer_node_id);
  const byNode = !!a.producer_node_id && /agent/i.test(ctx.workflow?.nodes.find((n) => n.id === a.producer_node_id)?.type ?? "");
  const byEnvelope = !!a.content && isObject(a.content.artifact_draft);
  return byRun || byNode || byEnvelope;
}

/** The step that produces an artifact type, from drafts or existing artifacts of the same Flowcase type. */
export function producerFor(ctx: CaseContext, t: PackArtifactType): string | null {
  const flowType = t.source?.flowcase_artifact_type;
  if (!flowType) return null;
  const draft = ctx.drafts.find((d) => d.artifact_type === flowType);
  if (draft) return draft.node_name || draft.node_id;
  const art = ctx.artifacts.find((a) => a.type === flowType && a.producer_node_id);
  return art ? nodeName(ctx.workflow?.nodes, art.producer_node_id) : null;
}

/** Newest draft per step (Flowcase lists drafts newest first). */
export function latestDraftPerNode(drafts: DraftReview[]): Map<string, DraftReview> {
  const latest = new Map<string, DraftReview>();
  for (const d of drafts) if (!latest.has(d.node_id)) latest.set(d.node_id, d);
  return latest;
}

/**
 * Steps whose draft was sent back and whose agent is re-running: Flowcase reports a live run (or an active node), or the
 * user requested the revision in this view and no newer draft has arrived yet.
 */
export function revisingSteps(ctx: CaseContext, requested: ReadonlySet<string> = new Set()): { nodeId: string; name: string; iteration?: number }[] {
  const out: { nodeId: string; name: string; iteration?: number }[] = [];
  for (const d of latestDraftPerNode(ctx.drafts).values()) {
    if (d.status !== "revision_requested") continue;
    const live = ctx.agent_runs.some((r) => r.node_id === d.node_id && isLiveRun(r));
    const nodeActive = ["active", "running"].includes(ctx.workflow?.nodes.find((n) => n.id === d.node_id)?.status ?? "");
    if (live || nodeActive || requested.has(d.node_id)) out.push({ nodeId: d.node_id, name: d.node_name || d.node_id, iteration: (d.iteration ?? 0) + 1 });
  }
  return out;
}

export function draftReviewActive(ctx: CaseContext, requested?: ReadonlySet<string>): boolean {
  return ctx.drafts.some((d) => d.status === "pending") || revisingSteps(ctx, requested).length > 0;
}

/** First non-empty string among well-known business keys in the case variables/facets (display only). */
export function pickVariable(ctx: CaseContext, keys: string[]): string | null {
  for (const bag of [ctx.case.variables, ctx.case.business_facets]) {
    if (!bag) continue;
    for (const k of keys) {
      const v = bag[k];
      if (typeof v === "string" && v.trim()) return v;
    }
  }
  return null;
}

export const customerKeys = ["customer_name", "customer", "organization", "counterparty", "client_name", "account_name"];
export const deadlineKeys = ["deadline", "due_date", "target_date", "due_at"];

/** Only same-origin relative links from the BFF are rendered as anchors (never javascript: or remote URLs). */
export function safeLink(href?: string | null): string | null {
  return href && href.startsWith("/") && !href.startsWith("//") ? href : null;
}

export interface AggregatedSource {
  source: CitedSource;
  locators: string[];
  citedBy: string[];
}

/** Every KMS source cited by the case's artifacts and drafts, with where it is cited. */
export function aggregateSources(ctx: CaseContext): AggregatedSource[] {
  const map = new Map<string, AggregatedSource>();
  const typeTitle = (a: { pack_artifact_type?: string | null; type: string }) =>
    ctx.artifact_types.find((t) => t.key === a.pack_artifact_type)?.title ?? a.type;
  const add = (s: CitedSource, by: string) => {
    const key = `${s.source_id}@${s.version_id ?? ""}`;
    const cur = map.get(key) ?? { source: s, locators: [], citedBy: [] };
    const loc = [s.citation, s.anchor].filter(Boolean).join(" · ");
    if (loc && !cur.locators.includes(loc)) cur.locators.push(loc);
    if (!cur.citedBy.includes(by)) cur.citedBy.push(by);
    map.set(key, cur);
  };
  for (const a of ctx.artifacts) for (const s of artifactSources(a.content, a.sources ?? [])) add(s, `${typeTitle(a)} v${a.version ?? "?"}`);
  for (const d of ctx.drafts) if (d.status === "pending" && d.content) for (const s of artifactSources(d.content)) add(s, `${d.node_name || d.node_id} draft v${d.artifact_version ?? d.version_num}`);
  return [...map.values()].sort((a, b) => b.citedBy.length - a.citedBy.length || a.source.title.localeCompare(b.source.title));
}

