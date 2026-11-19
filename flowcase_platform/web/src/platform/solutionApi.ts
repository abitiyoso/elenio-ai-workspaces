export interface SolutionDefinition {
  key: string;
  name: string;
  description: string;
  delivery_mode: "internal" | "solution" | "enterprise";
  output: "business_ui" | "api" | "both";
  workflows: { key: string; version: number }[];
  application?: ApplicationComposition;
}
export interface ApplicationComposition {
  title: string;
  pages: {
    key: string;
    title: string;
    kind: "overview" | "intake" | "tasks" | "report";
    workflow_key?: string;
  }[];
}
export interface ResourceDependency {
  kind: string;
  ref: string;
  version?: string;
  node_id?: string;
  state: "blocked";
  reason: string;
}
export interface Solution {
  id: string;
  tenant_id: string;
  workspace_id: string;
  revision: number;
  definition: SolutionDefinition;
  created_at: string;
  updated_at: string;
}
export interface SolutionSession {
  user: { id: string; display_name: string };
  tenant: { id: string; name: string };
  workspace: { id: string; name: string };
  permissions: string[];
  csrf_token: string;
  environment: string;
}
export interface SolutionReadiness {
  solution_id: string;
  revision: number;
  checked_at: string;
  release_ready: boolean;
  workflows: {
    key: string;
    version: number;
    state: "blocked" | "verified";
    reason: string;
    definition_hash?: string;
    resources?: ResourceDependency[];
    inventory_complete?: boolean;
  }[];
  blockers: string[];
  application_issues?: string[];
}
export interface SolutionCandidate {
  id: string;
  solution_id: string;
  version: string;
  plan_revision: number;
  status: "candidate";
  content_hash: string;
  created_at: string;
  snapshot: {
    schema_version: "elenio.solution-candidate.v1";
    plan: Solution;
    readiness: SolutionReadiness;
    limitations: string[];
  };
}
function readinessDetailsValid(report: SolutionReadiness): boolean {
  return (
    !!report &&
    typeof report.checked_at === "string" &&
    Number.isFinite(Date.parse(report.checked_at)) &&
    Array.isArray(report.blockers) &&
    report.blockers.length <= 100 &&
    report.blockers.every((v) => typeof v === "string") &&
    (report.application_issues === undefined ||
      (Array.isArray(report.application_issues) &&
        report.application_issues.every((v) => typeof v === "string"))) &&
    Array.isArray(report.workflows) &&
    report.workflows.length <= 100 &&
    report.workflows.every(
      (w) =>
        !!w &&
        typeof w.key === "string" &&
        Number.isInteger(w.version) &&
        ["verified", "blocked"].includes(w.state) &&
        typeof w.reason === "string" &&
        (w.inventory_complete === undefined ||
          typeof w.inventory_complete === "boolean") &&
        (w.resources === undefined ||
          (Array.isArray(w.resources) &&
            w.resources.length <= 500 &&
            w.resources.every(
              (ref) =>
                !!ref &&
                typeof ref.kind === "string" &&
                typeof ref.ref === "string" &&
                typeof ref.reason === "string" &&
                ref.state === "blocked" &&
                (ref.version === undefined || typeof ref.version === "string"),
            ))),
    )
  );
}
export function assertCandidate(
  item: SolutionCandidate,
  session: SolutionSession,
  solutionID: string,
): SolutionCandidate {
  if (
    !item ||
    !uuid.test(item.id) ||
    item.solution_id !== solutionID ||
    item.status !== "candidate" ||
    !/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(item.version) ||
    !/^[0-9a-f]{64}$/.test(item.content_hash) ||
    item.snapshot?.schema_version !== "elenio.solution-candidate.v1" ||
    item.snapshot.plan?.id !== solutionID ||
    item.snapshot.plan.revision !== item.plan_revision ||
    item.snapshot.readiness?.solution_id !== solutionID ||
    item.snapshot.readiness.revision !== item.plan_revision ||
    item.snapshot.readiness.release_ready !== false ||
    !readinessDetailsValid(item.snapshot.readiness) ||
    item.snapshot.readiness.workflows.length !==
      item.snapshot.plan.definition.workflows.length ||
    item.snapshot.readiness.workflows.some(
      (w, i) =>
        w.key !== item.snapshot.plan.definition.workflows[i]?.key ||
        w.version !== item.snapshot.plan.definition.workflows[i]?.version,
    ) ||
    !Array.isArray(item.snapshot.limitations) ||
    !item.snapshot.limitations.every((s) => typeof s === "string")
  )
    throw new Error("Invalid or out-of-scope Publisher candidate.");
  assertSolution(item.snapshot.plan, session);
  return item;
}
async function currentSolutionScope(session: SolutionSession) {
  const current = await solutionSession();
  if (
    current.user.id !== session.user.id ||
    current.workspace.id !== session.workspace.id ||
    current.tenant.id !== session.tenant.id
  )
    throw new Error(
      "The active user/workspace changed. Reopen solutions before continuing.",
    );
  return current;
}
export async function listCandidates(
  session: SolutionSession,
  plan: Solution,
  cursor = "",
  signal?: AbortSignal,
) {
  assertSolution(plan, session);
  if (cursor && !uuid.test(cursor))
    throw new Error("Invalid candidate cursor.");
  await currentSolutionScope(session);
  const result = await request<{
    items: SolutionCandidate[];
    next_cursor?: string;
  }>(
    `/solutions/${plan.id}/candidates${cursor ? `?cursor=${cursor}` : ""}`,
    signal ? { signal } : {},
  );
  if (
    !Array.isArray(result.items) ||
    (result.next_cursor && !uuid.test(result.next_cursor))
  )
    throw new Error("Invalid candidate history.");
  return {
    items: result.items.map((item) => assertCandidate(item, session, plan.id)),
    next_cursor: result.next_cursor ?? "",
  };
}
export async function createCandidate(
  session: SolutionSession,
  plan: Solution,
  version: string,
  requestKey: string,
) {
  assertSolution(plan, session);
  const current = await currentSolutionScope(session);
  if (!current.permissions.includes("solution:author"))
    throw new Error("Solution authoring permission is required.");
  if (
    !/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(version) ||
    version.length > 32
  )
    throw new Error("Use a numeric candidate version, for example 1.0.0.");
  const result = await request<SolutionCandidate>(
    `/solutions/${plan.id}/candidates`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": current.csrf_token,
        "If-Match": `"${plan.revision}"`,
        "Idempotency-Key": requestKey,
      },
      body: JSON.stringify({ version }),
    },
  );
  const candidate = assertCandidate(result, current, plan.id);
  if (
    candidate.plan_revision !== plan.revision ||
    candidate.version !== version
  )
    throw new Error(
      "Candidate response does not match the requested version and plan revision.",
    );
  return candidate;
}
export function validApplication(
  value: unknown,
  definition: SolutionDefinition,
): value is ApplicationComposition {
  const app = value as ApplicationComposition | null;
  return (
    !!app &&
    definition.output !== "api" &&
    typeof app.title === "string" &&
    app.title.trim() === app.title &&
    app.title.length >= 2 &&
    app.title.length <= 120 &&
    Array.isArray(app.pages) &&
    app.pages.length > 0 &&
    app.pages.length <= 12 &&
    new Set(app.pages.map((p) => p?.key)).size === app.pages.length &&
    app.pages.every(
      (p) =>
        !!p &&
        /^[a-z][a-z0-9-]{1,62}$/.test(p.key) &&
        typeof p.title === "string" &&
        p.title.trim() === p.title &&
        p.title.length >= 2 &&
        p.title.length <= 80 &&
        ["overview", "intake", "tasks", "report"].includes(p.kind) &&
        ((p.kind === "overview" && !p.workflow_key) ||
          definition.workflows.some((w) => w.key === p.workflow_key)),
    )
  );
}
export async function checkSolution(
  session: SolutionSession,
  item: Solution,
): Promise<SolutionReadiness> {
  assertSolution(item, session);
  const current = await solutionSession();
  if (
    current.user.id !== session.user.id ||
    current.workspace.id !== session.workspace.id ||
    current.tenant.id !== session.tenant.id
  )
    throw new Error(
      "The active workspace changed. Refresh solutions before checking dependencies.",
    );
  const report = await request<SolutionReadiness>(
    `/solutions/${item.id}/readiness`,
  );
  if (
    !report ||
    !readinessDetailsValid(report) ||
    report.solution_id !== item.id ||
    report.revision !== item.revision ||
    typeof report.release_ready !== "boolean" ||
    typeof report.checked_at !== "string" ||
    !Array.isArray(report.blockers) ||
    !report.blockers.every((b) => typeof b === "string") ||
    !Array.isArray(report.workflows) ||
    report.workflows.length !== item.definition.workflows.length ||
    report.workflows.some(
      (ref, i) =>
        !ref ||
        ref.key !== item.definition.workflows[i]?.key ||
        ref.version !== item.definition.workflows[i]?.version ||
        !["blocked", "verified"].includes(ref.state) ||
        typeof ref.reason !== "string" ||
        (ref.state === "verified" &&
          !/^[0-9a-f]{64}$/.test(ref.definition_hash ?? "")),
    )
  )
    throw new Error(
      "Readiness does not match this solution revision. Refresh and review the current plan.",
    );
  return report;
}
const root = import.meta.env.DEV ? "/control-api/v1" : "/api/v1";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(root + path, {
    ...options,
    credentials: "same-origin",
    cache: "no-store",
    redirect: "error",
    headers: { Accept: "application/json", ...options.headers },
  });
  if (!response.ok) {
    const messages: Record<number, string> = {
      401: "Sign in through the Control Plane to access solutions.",
      403: "Solution access is not granted in the active workspace.",
      404: "Solution or Control Plane route is unavailable in this workspace.",
      409: path.includes("/candidates")
        ? "Candidate version or retry key already exists. Review history and use a new version for changed content."
        : "The solution key or retry key is already used. Review the existing solution before creating another.",
      412: "This solution changed on the server. Your edits were retained; close and reopen it to review the latest version.",
      428: "The current solution revision is required before saving.",
    };
    throw new Error(
      messages[response.status] ??
        "Control Plane is unavailable or rejected the solution plan. No deployment was performed.",
    );
  }
  return response.json() as Promise<T>;
}
export async function solutionSession(
  signal?: AbortSignal,
): Promise<SolutionSession> {
  const session = await request<SolutionSession>(
    "/me",
    signal ? { signal } : {},
  );
  if (
    !session ||
    !uuid.test(session.user?.id) ||
    !uuid.test(session.tenant?.id) ||
    !uuid.test(session.workspace?.id) ||
    typeof session.workspace?.name !== "string" ||
    !Array.isArray(session.permissions) ||
    !session.permissions.every((p) => typeof p === "string") ||
    typeof session.csrf_token !== "string" ||
    !session.csrf_token
  )
    throw new Error(
      "Select an authorized Control Plane workspace before managing solutions.",
    );
  if (!session.permissions.includes("solution:view"))
    throw new Error(
      "Solution viewing permission is required in the active workspace.",
    );
  return session;
}
export function assertSolution(
  value: Solution,
  session: SolutionSession,
): Solution {
  if (
    !value ||
    !uuid.test(value.id) ||
    value.tenant_id !== session.tenant.id ||
    value.workspace_id !== session.workspace.id ||
    !Number.isInteger(value.revision) ||
    value.revision < 1 ||
    typeof value.definition?.key !== "string" ||
    typeof value.definition?.name !== "string" ||
    typeof value.definition?.description !== "string" ||
    !["internal", "solution", "enterprise"].includes(
      value.definition.delivery_mode,
    ) ||
    !["business_ui", "api", "both"].includes(value.definition.output) ||
    !Array.isArray(value.definition.workflows) ||
    value.definition.workflows.length > 100 ||
    value.definition.workflows.some(
      (w) =>
        !w ||
        !/^[a-z][a-z0-9-]{0,62}$/.test(w.key) ||
        !Number.isSafeInteger(w.version) ||
        w.version < 1,
    ) ||
    (value.definition.application !== undefined &&
      !validApplication(value.definition.application, value.definition))
  )
    throw new Error(
      "The solution response does not match the active workspace contract.",
    );
  return value;
}
export async function listSolutions(
  session: SolutionSession,
  cursor = "",
  signal?: AbortSignal,
) {
  if (cursor && !uuid.test(cursor)) throw new Error("Invalid solution cursor.");
  const page = await request<{ items: Solution[]; next_cursor?: string }>(
    `/solutions${cursor ? `?cursor=${cursor}` : ""}`,
    signal ? { signal } : {},
  );
  if (
    !Array.isArray(page.items) ||
    (page.next_cursor && !uuid.test(page.next_cursor))
  )
    throw new Error("Invalid solution list response.");
  return {
    items: page.items.map((item) => assertSolution(item, session)),
    next_cursor: page.next_cursor ?? "",
  };
}
export async function saveSolution(
  session: SolutionSession,
  definition: SolutionDefinition,
  options: { id?: string; revision?: number; requestKey: string },
): Promise<Solution> {
  if (
    definition.application &&
    !validApplication(definition.application, definition)
  )
    throw new Error(
      "Application pages need supported components, unique page keys and linked workflow versions.",
    );
  // Re-read the cookie identity before mutation. Never forward a Flowcase bearer token.
  const current = await solutionSession();
  if (
    current.user.id !== session.user.id ||
    current.tenant.id !== session.tenant.id ||
    current.workspace.id !== session.workspace.id
  )
    throw new Error(
      "The active user/workspace changed. Reopen solutions before saving; your edits were retained.",
    );
  if (!current.permissions.includes("solution:author"))
    throw new Error("Solution authoring permission is required.");
  if (
    options.id &&
    (!uuid.test(options.id) ||
      !Number.isInteger(options.revision) ||
      (options.revision ?? 0) < 1)
  )
    throw new Error("Invalid solution revision.");
  const result = await request<Solution>(
    options.id ? `/solutions/${options.id}` : "/solutions",
    {
      method: options.id ? "PATCH" : "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": current.csrf_token,
        ...(options.id
          ? { "If-Match": `"${options.revision}"` }
          : { "Idempotency-Key": options.requestKey }),
      },
      body: JSON.stringify(definition),
    },
  );
  return assertSolution(result, current);
}
export function parseWorkflowReferences(
  text: string,
): SolutionDefinition["workflows"] {
  const lines = text
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean);
  if (lines.length > 100)
    throw new Error("Use at most 100 workflow references.");
  const seen = new Set<string>();
  return lines.map((line) => {
    const match = /^([a-z][a-z0-9-]{0,62})@([1-9][0-9]*)$/.exec(line);
    if (
      !match?.[1] ||
      !match[2] ||
      !Number.isSafeInteger(Number(match[2])) ||
      seen.has(match[1])
    )
      throw new Error(
        "Use one unique key@version per line, for example purchase-review@1.",
      );
    seen.add(match[1]);
    return { key: match[1], version: Number(match[2]) };
  });
}
