export interface SolutionDefinition {
  key: string;
  name: string;
  description: string;
  delivery_mode: "internal" | "solution" | "enterprise";
  output: "business_ui" | "api" | "both";
  workflows: { key: string; version: number }[];
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
  }[];
  blockers: string[];
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
      409: "The solution key or retry key is already used. Review the existing solution before creating another.",
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
    )
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
