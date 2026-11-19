import { apiGet } from "../api/client";
import type { Session } from "../api/types";
import type { Catalog, WorkflowDefinition } from "../api/workflows";
import {
  isSession,
  isRuntimeCapabilities,
  type RuntimeCapabilities,
} from "./connectedApi";
import { parseDefinition } from "./graphModel";

export const ARCHITECT_KEY = "elenio.solution-architect";
export const ARCHITECT_VERSION = "0.3.0";
export interface AgentSession {
  subject: string;
  issuer?: string;
  tenant_id: string;
  identity_mode: string;
  permissions: string[];
}
export interface AuthoringRun {
  id: string;
  agent_key: string;
  agent_version_id: string;
  agent_semver: string;
  kind: string;
  model_mode: string;
  status: string;
  input: { requirement?: string };
  output?: unknown;
  validation?: { schema_valid?: boolean };
  error_code?: string;
  tokens_in: number;
  tokens_out: number;
  tool_calls: number;
  created_at: string;
  pinned?: { model?: { target?: { provider_model_id?: string } } };
}
export interface ArchitectProposal {
  stage: "clarification" | "proposal";
  title: string;
  summary: string;
  questions: string[];
  assumptions: string[];
  agents: { role: string; purpose: string; reuse_key: string }[];
  dependencies: {
    name: string;
    kind: string;
    status: "available" | "required";
    reason: string;
  }[];
  workflow: WorkflowDefinition | null;
}
export interface ArchitectReady {
  agent: AgentSession;
  flowcase: Session;
  version: { id: string; semver: string };
  model: string;
  catalog: Catalog;
  capabilities: RuntimeCapabilities;
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const apiBase = import.meta.env.DEV
  ? "/agent-api/v1"
  : "/apps/agent-studio/api/v1";

// Same-origin only: the Control Plane injects its audience-bound token server-side.
// The development proxy reaches the explicitly configured loopback dev identity.
export async function agentRequest<T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(apiBase + path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    cache: "no-store",
    ...(signal ? { signal } : {}),
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    // Do not reflect arbitrary upstream/provider error bodies into the UI.
    const reasons: Record<number, string> = {
      401: "Sign in to Agent Studio through the workspace.",
      403: "Your Agent Studio permissions do not allow this action.",
      409: "Agent/model configuration changed. Refresh readiness before trying again.",
      429: "The tenant request limit was reached. Try again later.",
    };
    throw new Error(
      reasons[response.status] ??
        "Agent Studio is unavailable or not configured for this workspace.",
    );
  }
  return response.json() as Promise<T>;
}

export function assertSharedIdentity(agent: AgentSession, flow: Session) {
  if (
    !agent ||
    typeof agent.subject !== "string" ||
    !agent.subject ||
    !Array.isArray(agent.permissions) ||
    !agent.permissions.every((p) => typeof p === "string") ||
    !isSession(flow) ||
    agent.tenant_id !== flow.tenant_id
  )
    throw new Error(
      "Agent Studio and Flowcase must use the same verified tenant.",
    );
  const local =
    agent.identity_mode === "development" &&
    flow.auth_mode === "dev_mock" &&
    flow.environment === "development";
  if (
    !local &&
    (agent.identity_mode !== "control-plane" ||
      flow.auth_mode !== "trusted_jwt" ||
      !agent.issuer || !flow.identity_issuer || agent.issuer !== flow.identity_issuer || !flow.identity_subject || agent.subject !== flow.identity_subject)
  )
    throw new Error(
      "A shared verified Control Plane identity is required across these applications.",
    );
}

export async function loadArchitect(
  signal?: AbortSignal,
): Promise<ArchitectReady> {
  const [agent, flowcase] = await Promise.all([
    agentRequest<AgentSession>("/session", undefined, signal),
    apiGet<Session>("/api/v1/session", signal),
  ]);
  assertSharedIdentity(agent, flowcase);
  if (
    !["sandbox.run", "run.read", "agent.version.read"].every((p) =>
      agent.permissions.includes(p),
    )
  )
    throw new Error(
      "Sandbox run, run read and agent version read permissions are required.",
    );
  const [versions, catalog, capabilities] = await Promise.all([
    agentRequest<{
      items: {
        id: string;
        semver: string;
        output_schema: { properties?: { workflow?: unknown } };
      }[];
    }>(
      `/catalog/agents/${ARCHITECT_KEY}/versions?limit=100`,
      undefined,
      signal,
    ),
    apiGet<Catalog>("/api/v1/workflow-catalog", signal),
    apiGet<RuntimeCapabilities>("/api/v1/runtime-capabilities", signal),
  ]);
  const version = versions.items?.find(
    (v) =>
      v.semver === ARCHITECT_VERSION &&
      uuid.test(v.id) &&
      v.output_schema?.properties?.workflow,
  );
  if (
    !version ||
    !isRuntimeCapabilities(capabilities) ||
    !Array.isArray(catalog.assignee_roles)
  )
    throw new Error(
      "The bounded Architect agent contract and workflow catalog must be configured first.",
    );
  const [binding, tools, skills] = await Promise.all([
    agentRequest<{ model_deployment_id: string; environment: string }>(
      `/agent-versions/${version.id}/model-binding`,
      undefined,
      signal,
    ),
    agentRequest<unknown[]>(
      `/catalog/agent-versions/${version.id}/tools`,
      undefined,
      signal,
    ),
    agentRequest<unknown[]>(
      `/catalog/agent-versions/${version.id}/skills`,
      undefined,
      signal,
    ),
  ]);
  if (
    binding.environment !== "development" ||
    !uuid.test(binding.model_deployment_id) ||
    !Array.isArray(tools) ||
    tools.length ||
    !Array.isArray(skills) ||
    skills.length
  )
    throw new Error(
      "Architect requires its tool-free, skill-free development sandbox binding.",
    );
  const [deployments, approvals] = await Promise.all([
    agentRequest<{
      items: { id: string; provider_model_id: string; status: string }[];
    }>("/model-deployments?limit=100", undefined, signal),
    agentRequest<{ state: string; capability_check_ref?: string }[]>(
      `/model-deployments/${binding.model_deployment_id}/approvals?environment=development`,
      undefined,
      signal,
    ),
  ]);
  const deployment = deployments.items?.find(
    (d) => d.id === binding.model_deployment_id && d.status === "active",
  );
  if (
    !deployment ||
    !approvals.some((a) => a.state === "approved" && a.capability_check_ref)
  )
    throw new Error(
      "A current probed model approval is required for development authoring.",
    );
  return {
    agent,
    flowcase,
    version,
    catalog,
    capabilities,
    model: deployment.provider_model_id,
  };
}

const strings = (value: unknown, max: number) =>
  Array.isArray(value) &&
  value.length <= max &&
  value.every((v) => typeof v === "string" && v.length <= 2000);
export function parseProposal(run: AuthoringRun): ArchitectProposal {
  if (
    run.agent_key !== ARCHITECT_KEY ||
    !["0.2.0", ARCHITECT_VERSION].includes(run.agent_semver) ||
    run.kind !== "sandbox" ||
    run.model_mode !== "provider" ||
    !["succeeded", "needs_review"].includes(run.status) ||
    run.validation?.schema_valid !== true ||
    run.tool_calls !== 0
  )
    throw new Error(
      "Only a schema-valid, tool-free real provider sandbox result can be used.",
    );
  const value = run.output as ArchitectProposal | null;
  if (
    !value ||
    !["clarification", "proposal"].includes(value.stage) ||
    typeof value.title !== "string" ||
    typeof value.summary !== "string" ||
    !strings(value.questions, 6) ||
    !strings(value.assumptions, 12) ||
    !Array.isArray(value.agents) ||
    value.agents.length > 6 ||
    value.agents.some(
      (a) =>
        !a ||
        typeof a.role !== "string" ||
        typeof a.purpose !== "string" ||
        typeof a.reuse_key !== "string",
    ) ||
    !Array.isArray(value.dependencies) ||
    value.dependencies.length > 12 ||
    value.dependencies.some(
      (d) =>
        !d ||
        typeof d.name !== "string" ||
        typeof d.kind !== "string" ||
        typeof d.reason !== "string" ||
        !["required", "available"].includes(d.status),
    )
  )
    throw new Error(
      "The provider result does not match the Architect proposal contract.",
    );
  if (value.stage === "clarification") {
    if (value.workflow !== null)
      throw new Error("Clarification must not contain an applicable workflow.");
    return value;
  }
  const workflow = parseDefinition(JSON.stringify(value.workflow));
  if (
    workflow.version !== 1 ||
    workflow.status !== "draft" ||
    workflow.tenant_scope !== undefined ||
    workflow.nodes.length > 20 ||
    workflow.nodes.length < 2
  )
    throw new Error(
      "Architect output must be a bounded new draft with no tenant override.",
    );
  return { ...value, workflow };
}

export async function getAuthoringRun(id: string, signal?: AbortSignal) {
  if (!uuid.test(id)) throw new Error("Invalid authoring run reference.");
  const result = await agentRequest<{ run: AuthoringRun }>(
    `/runs/${id}`,
    undefined,
    signal,
  );
  if (result.run.agent_key !== ARCHITECT_KEY || result.run.kind !== "sandbox")
    throw new Error("This run is not an Architect sandbox proposal.");
  return result.run;
}
export const listAuthoringRuns = (signal?: AbortSignal) =>
  agentRequest<{ items: AuthoringRun[] }>(
    `/runs?agent_key=${ARCHITECT_KEY}&kind=sandbox&limit=10`,
    undefined,
    signal,
  );
