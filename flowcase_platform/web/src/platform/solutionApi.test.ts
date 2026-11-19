import { describe, expect, it, vi, afterEach } from "vitest";
import {
  assertSolution,
  checkSolution,
  parseWorkflowReferences,
  saveSolution,
  solutionSession,
  type Solution,
  type SolutionSession,
} from "./solutionApi";
const session: SolutionSession = {
  user: {
    id: "00000000-0000-4000-8000-000000000101",
    display_name: "Contract builder",
  },
  tenant: { id: "00000000-0000-4000-8000-000000000001", name: "Test tenant" },
  workspace: {
    id: "00000000-0000-4000-8000-000000000002",
    name: "Test workspace",
  },
  permissions: ["solution:view", "solution:author"],
  csrf_token: "synthetic-csrf",
  environment: "development",
};
const plan: Solution = {
  id: "00000000-0000-4000-8000-000000000201",
  tenant_id: session.tenant.id,
  workspace_id: session.workspace.id,
  revision: 1,
  definition: {
    key: "purchase-review",
    name: "Purchase review",
    description: "Synthetic",
    delivery_mode: "solution",
    output: "both",
    workflows: [{ key: "purchase-review", version: 1 }],
  },
  created_at: "2026-11-19T00:00:00Z",
  updated_at: "2026-11-19T00:00:00Z",
};
const reply = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
afterEach(() => vi.unstubAllGlobals());
describe("solution plans", () => {
  it("requires readiness to match the exact plan revision and each immutable reference", async () => {
    const report = {
      solution_id: plan.id,
      revision: plan.revision,
      checked_at: "2026-11-19T00:00:00Z",
      release_ready: false,
      blockers: ["Target installation unverified"],
      workflows: [
        {
          key: "purchase-review",
          version: 1,
          state: "verified",
          reason: "Published version observed",
          definition_hash: "a".repeat(64),
        },
      ],
    };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(reply(session))
      .mockResolvedValueOnce(reply(report));
    vi.stubGlobal("fetch", fetch);
    expect(await checkSolution(session, plan)).toEqual(report);
    for (const invalid of [
      { ...report, revision: 2 },
      {
        ...report,
        workflows: [{ ...report.workflows[0], definition_hash: "" }],
      },
      { ...report, workflows: [] },
      null,
    ]) {
      fetch
        .mockResolvedValueOnce(reply(session))
        .mockResolvedValueOnce(reply(invalid));
      await expect(checkSolution(session, plan)).rejects.toThrow(
        "Readiness does not match",
      );
    }
  });
  it("parses pinned versions and rejects duplicate, unsafe or unpinned references", () => {
    expect(
      parseWorkflowReferences("purchase-review@2\n exception-review@1\n"),
    ).toEqual([
      { key: "purchase-review", version: 2 },
      { key: "exception-review", version: 1 },
    ]);
    for (const text of [
      "purchase-review",
      "purchase-review@0",
      "../escape@1",
      "same@1\nsame@2",
      "valid@9007199254740992",
    ]) {
      expect(() => parseWorkflowReferences(text)).toThrow();
    }
  });
  it("rejects foreign workspace or malformed server results", () => {
    expect(assertSolution(plan, session)).toEqual(plan);
    expect(() =>
      assertSolution(
        { ...plan, workspace_id: "00000000-0000-4000-8000-000000000003" },
        session,
      ),
    ).toThrow("active workspace");
    expect(() => assertSolution({ ...plan, revision: 0 }, session)).toThrow();
  });
  it("rejects unselected workspaces and missing view permission", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(reply({ ...session, workspace: null }))
        .mockResolvedValueOnce(reply({ ...session, permissions: [] })),
    );
    await expect(solutionSession()).rejects.toThrow(
      "authorized Control Plane workspace",
    );
    await expect(solutionSession()).rejects.toThrow("viewing permission");
  });
  it("uses a fresh cookie/CSRF session with idempotency and never supplies a tenant or bearer token", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(reply(session))
      .mockResolvedValueOnce(reply(plan, 201));
    vi.stubGlobal("fetch", fetch);
    await saveSolution(session, plan.definition, {
      requestKey: "stable-create-key",
    });
    const options = fetch.mock.calls[1]?.[1] as RequestInit;
    expect(options.credentials).toBe("same-origin");
    expect(options.redirect).toBe("error");
    expect(options.headers).toMatchObject({
      "X-CSRF-Token": "synthetic-csrf",
      "Idempotency-Key": "stable-create-key",
    });
    expect(options.headers).not.toHaveProperty("Authorization");
    expect(JSON.parse(options.body as string)).not.toHaveProperty("tenant_id");
  });
  it("blocks stale user/workspace before mutation", async () => {
    const fetch = vi.fn().mockResolvedValue(
      reply({
        ...session,
        workspace: {
          ...session.workspace,
          id: "00000000-0000-4000-8000-000000000003",
        },
      }),
    );
    vi.stubGlobal("fetch", fetch);
    await expect(
      saveSolution(session, plan.definition, {
        requestKey: "stable-create-key",
      }),
    ).rejects.toThrow("workspace changed");
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("sends the exact strong revision and retains a specific conflict error", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(reply(session))
      .mockResolvedValueOnce(reply({ message: "private upstream body" }, 412));
    vi.stubGlobal("fetch", fetch);
    await expect(
      saveSolution(session, plan.definition, {
        id: plan.id,
        revision: 3,
        requestKey: "unused",
      }),
    ).rejects.toThrow("edits were retained");
    expect(fetch.mock.calls[1]?.[1]?.headers).toMatchObject({
      "If-Match": '"3"',
    });
    expect(fetch.mock.calls[1]?.[1]?.headers).not.toHaveProperty(
      "Idempotency-Key",
    );
  });
});
