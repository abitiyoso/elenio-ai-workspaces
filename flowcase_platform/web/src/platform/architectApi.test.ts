import { describe, expect, it } from "vitest";
import {
  assertSharedIdentity,
  parseProposal,
  ARCHITECT_KEY,
  ARCHITECT_VERSION,
  type AgentSession,
  type AuthoringRun,
} from "./architectApi";
import type { Session } from "../api/types";

const flow: Session = {
  user_id: "user-1",
  identity_subject: "cp-user-1",
  identity_issuer: "https://workspace.example",
  tenant_id: "tenant-1",
  display_name: "Builder",
  roles: ["workflow_designer"],
  auth_mode: "trusted_jwt",
  environment: "production",
  expires_at: null,
};
const agent: AgentSession = {
  subject: "cp-user-1",
  issuer: "https://workspace.example",
  tenant_id: "tenant-1",
  identity_mode: "control-plane",
  permissions: [],
};
const run: AuthoringRun = {
  id: "run-1",
  agent_key: ARCHITECT_KEY,
  agent_version_id: "v1",
  agent_semver: ARCHITECT_VERSION,
  kind: "sandbox",
  model_mode: "provider",
  status: "succeeded",
  input: {},
  validation: { schema_valid: true },
  tokens_in: 20,
  tokens_out: 10,
  tool_calls: 0,
  created_at: "2026-11-19T00:00:00Z",
  output: {
    stage: "proposal",
    title: "Review",
    summary: "Manual review draft",
    questions: [],
    assumptions: [],
    agents: [],
    dependencies: [],
    workflow: {
      schema_version: "1.0",
      key: "request-review",
      name: "Request review",
      version: 1,
      status: "draft",
      nodes: [
        { id: "start", type: "start", name: "Start", next: ["end"] },
        { id: "end", type: "end", name: "End", next: [] },
      ],
    },
  },
};
describe("Architect identity boundary", () => {
  it("requires both tenant and subject agreement for trusted identities", () => {
    expect(() => assertSharedIdentity(agent, flow)).not.toThrow();
    expect(() =>
      assertSharedIdentity({ ...agent, tenant_id: "other" }, flow),
    ).toThrow(/tenant/);
    expect(() =>
      assertSharedIdentity({ ...agent, subject: "other" }, flow),
    ).toThrow(/identity/);
    for(const patch of [{issuer:""},{issuer:"https://foreign.example"},{subject:flow.user_id}]){
      expect(()=>assertSharedIdentity({...agent,...patch},flow)).toThrow(/identity/);
    }
    const missingIssuer={...flow}, missingSubject={...flow}, missingAgentIssuer={...agent};
    delete missingIssuer.identity_issuer; delete missingSubject.identity_subject; delete missingAgentIssuer.issuer;
    expect(()=>assertSharedIdentity(agent,missingIssuer)).toThrow(/identity/);
    expect(()=>assertSharedIdentity(agent,missingSubject)).toThrow(/identity/);
    expect(()=>assertSharedIdentity(missingAgentIssuer,flow)).toThrow(/identity/);
  });
  it("does not accept a development identity alongside a production identity", () => {
    expect(() =>
      assertSharedIdentity({ ...agent, identity_mode: "development" }, flow),
    ).toThrow();
    expect(() =>
      assertSharedIdentity(
        { ...agent, identity_mode: "development", subject: "local-builder" },
        { ...flow, auth_mode: "dev_mock", environment: "development" },
      ),
    ).not.toThrow();
    expect(() =>
      assertSharedIdentity(
        { ...agent, identity_mode: "development" },
        { ...flow, auth_mode: "dev_mock", environment: "staging" },
      ),
    ).toThrow();
  });
});
describe("untrusted model proposals", () => {
  it("retains native explicit edges in a bounded draft", () => {
    expect(parseProposal(run).workflow?.nodes[0]?.next).toEqual(["end"]);
  });
  it.each([
    { model_mode: "dev_mock" },
    { tool_calls: 1 },
    { status: "failed" },
    { kind: "production" },
    { validation: { schema_valid: false } },
    { agent_key: "other.agent" },
  ])("rejects an inappropriate run: %j", (patch) => {
    expect(() => parseProposal({ ...run, ...patch })).toThrow();
  });
  it("rejects tenant overrides and published output rather than repairing it silently", () => {
    const proposal = parseProposal(run);
    for (const patch of [
      { tenant_scope: "other-tenant" },
      { status: "published" },
      { version: 2 },
    ])
      expect(() =>
        parseProposal({
          ...run,
          output: { ...proposal, workflow: { ...proposal.workflow, ...patch } },
        }),
      ).toThrow();
  });
  it("clarification cannot carry an applicable workflow", () => {
    const proposal = parseProposal(run);
    expect(() =>
      parseProposal({
        ...run,
        output: { ...proposal, stage: "clarification" },
      }),
    ).toThrow();
    expect(
      parseProposal({
        ...run,
        output: {
          ...proposal,
          stage: "clarification",
          workflow: null,
          questions: ["Who owns approval?"],
        },
      }).workflow,
    ).toBeNull();
  });
});
