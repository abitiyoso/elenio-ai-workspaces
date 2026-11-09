export type Provider = {
  id: string;
  key: string;
  display_name: string;
  adapter_type: string;
  base_url: string;
  status: 'pending_test' | 'active' | 'disabled' | 'error' | string;
  created_at: string;
};

export type Deployment = {
  id: string;
  provider_id: string;
  key: string;
  provider_model_id: string;
  display_name: string;
  capabilities: Record<string, unknown>;
  max_output_tokens: number;
  timeout_ms: number;
  status: 'pending_test' | 'active' | 'disabled' | 'error' | string;
  created_at: string;
};

export type CredentialStatus = {
  credential_configured: boolean;
  provider_status: string;
  previous_secret_cleanup_pending?: boolean;
  vault_cleanup_pending?: boolean;
};

export type AgentModelBinding = {
  agent_version_id: string;
  model_deployment_id: string;
  fallback_model_deployment_id?: string;
  environment: 'development' | 'staging' | 'production';
  override_policy: Record<string, unknown>;
};

export type ModelApproval = {
  id: string;
  deployment_id: string;
  environment: 'development' | 'staging' | 'production';
  purpose: string;
  state: 'pending' | 'approved' | 'rejected' | 'revoked';
  capability_check_ref?: string;
  decision_reason: string;
  decided_by?: string;
  decided_at?: string;
  created_at: string;
};

export type ProviderProbeResult = {
  id: string;
  status: 'active' | 'error';
  mode?: 'connectivity' | 'capabilities';
  capabilities: Record<string, boolean>;
  evidence?: Record<string, string>;
  evidence_scope?: string;
  checked_at: string;
  may_incur_token_cost?: boolean;
  error_code?: string;
};

export type ProviderInput = Pick<Provider, 'key' | 'display_name' | 'adapter_type' | 'base_url'>;
export type DeploymentInput = {
  provider_id: string;
  key: string;
  provider_model_id: string;
  display_name: string;
  capabilities: { tools: boolean; json: boolean };
  max_output_tokens: number;
  timeout_ms: number;
  cost_policy: Record<string, never>;
};

export type Agent = {
  id: string;
  key: string;
  display_name: string;
  domain: string;
  owner_subject: string | null;
  lifecycle_status: 'draft' | 'active' | 'archived';
  created_at: string;
};

export type Page<T> = { items: T[]; total: number; limit: number; offset: number };
export type AgentPage = Page<Agent>;
export type AgentInput = Pick<Agent, 'key' | 'display_name' | 'domain'>;

export type Skill = {
  id: string;
  agent_id: string;
  key: string;
  domain: string;
  owner_subject: string | null;
  created_at: string;
};

export type SkillVersion = {
  id: string;
  skill_id: string;
  semver: string;
  instructions: string;
  procedures: unknown[];
  examples: unknown[];
  forbidden_actions: unknown[];
  output_schema: Record<string, unknown>;
  checksum?: string;
  state: 'draft' | 'published' | 'deprecated';
  created_at: string;
  published_at?: string | null;
};

export type AgentVersion = {
  id: string;
  agent_id: string;
  semver: string;
  manifest: Record<string, unknown>;
  input_schema: Record<string, unknown>;
  output_schema: Record<string, unknown>;
  model_policy: Record<string, unknown>;
  tool_policy: Record<string, unknown>;
  checksum?: string;
  state: 'draft' | 'published' | 'deprecated';
  created_at: string;
  published_at?: string | null;
};

export type ToolVersion = {
  id: string;
  tool_id: string;
  key: string;
  kind: string;
  owner: string;
  risk_level: string;
  lifecycle_status: string;
  semver: string;
  adapter_ref: string;
  input_schema: Record<string, unknown>;
  output_schema: Record<string, unknown>;
  policy: Record<string, unknown>;
  checksum?: string;
  published_at?: string | null;
};

export type SkillBinding = { agent_version_id: string; skill_id: string; skill_version_id: string };
export type ToolBinding = { agent_version_id: string; tool_version_id: string };

export type SkillVersionInput = {
  semver: string;
  instructions: string;
  procedures: unknown[];
  examples: unknown[];
  forbidden_actions: unknown[];
  output_schema: Record<string, unknown>;
};

export type AgentVersionInput = {
  semver: string;
  manifest: Record<string, unknown>;
  input_schema: Record<string, unknown>;
  output_schema: Record<string, unknown>;
  model_policy: Record<string, unknown>;
  tool_policy: Record<string, unknown>;
};

export class ApiError extends Error {
  constructor(message: string, readonly status?: number) {
    super(message);
    this.name = 'ApiError';
  }
}

// BASE_URL is Vite's `base` ("/" by default, "/apps/agent-studio/" behind the
// Control Plane); API calls stay under the same prefix as the SPA.
export const appBasePath = import.meta.env.BASE_URL.replace(/\/$/, '');

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${import.meta.env.VITE_API_BASE_URL ?? appBasePath}/api/v1${path}`, {
      ...init,
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...init?.headers },
    });
  } catch {
    throw new ApiError('API belum dapat dijangkau. Periksa layanan Agent Studio.');
  }

  if (!response.ok) {
    if (response.status === 401) throw new ApiError('Sesi host belum terverifikasi.', 401);
    if (response.status === 403) throw new ApiError('Akun ini tidak memiliki izin untuk tindakan tersebut.', 403);
    if (response.status >= 500) throw new ApiError('Layanan registry belum tersedia. Pastikan API Agent Studio sudah aktif.', response.status);
    const body = await response.json().catch(() => null) as { error?: string } | null;
    throw new ApiError(body?.error ?? `Permintaan gagal (${response.status}).`, response.status);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export type Session = { subject: string; tenant_id: string; identity_mode: 'tailscale-serve' | 'development' | 'control-plane'; permissions?: string[]; self_approval_exempt?: boolean };

export type RunStatus = 'queued' | 'running' | 'succeeded' | 'needs_review' | 'failed' | 'cancelled';
export type TrustStatus = 'approved' | 'unverified' | 'customer_source' | 'expired' | 'conflicted';

export type RunValidation = {
  schema_valid?: boolean;
  schema_errors?: string[];
  risk_flags?: string[];
  cited_sources?: string[];
  unknown_citations?: string[];
  unsupported_claim_paths?: string[];
  raw_output_preview?: string;
};

export type Run = {
  id: string;
  agent_id: string;
  agent_key: string;
  agent_version_id: string;
  agent_semver: string;
  kind: 'production' | 'sandbox' | 'evaluation' | string;
  caller_kind: string;
  caller_subject: string;
  idempotency_key: string;
  external_correlation_id?: string;
  case_ref?: string;
  input: unknown;
  evidence_input: unknown;
  status: RunStatus | string;
  model_mode: 'provider' | 'dev_mock' | string;
  pinned: unknown;
  output?: unknown;
  output_checksum?: string;
  validation: RunValidation | null;
  error_code?: string;
  attempts: number;
  max_attempts: number;
  deadline_at: string;
  cancel_requested: boolean;
  tokens_in: number;
  tokens_out: number;
  tool_calls: number;
  eval_run_id?: string;
  eval_case_id?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
};

export type RunEvent = { seq: number; type: string; payload: unknown; created_at: string };

export type EvidenceRef = {
  origin: 'kms' | 'fixture' | 'flowcase' | string;
  source_id: string;
  source_version: string;
  title: string;
  citation: string;
  anchor: string;
  trust_status: TrustStatus | string;
  publication_status: string;
  cited: boolean;
};

export type EvidenceFixtureItem = {
  source_id: string;
  source_version: string;
  title: string;
  snippet: string;
  trust_status: TrustStatus;
  publication_status: string;
  anchor: string;
};

export type RunFilter = { agent_key?: string; kind?: string; status?: string; limit?: number; offset?: number };

export type EvalClassification = 'positive' | 'negative' | 'adversarial' | 'evidence_conflict' | 'missing_evidence';

export type EvalSuite = {
  id: string;
  agent_key: string;
  key: string;
  description: string;
  case_count: number;
  created_by: string;
  created_at: string;
};

export type EvalCase = {
  id: string;
  suite_id: string;
  key: string;
  classification: EvalClassification | string;
  critical: boolean;
  input: unknown;
  evidence_fixture: unknown;
  expectations: unknown;
  created_by: string;
  created_at: string;
};

export type EvalCaseInput = {
  key: string;
  classification: EvalClassification;
  critical: boolean;
  input: Record<string, unknown>;
  evidence_fixture: unknown[];
  expectations: Record<string, unknown>;
};

export type EvalCheck = { name: string; passed: boolean; detail?: string };

export type EvalResult = {
  case_id: string;
  case_key: string;
  classification: string;
  critical: boolean;
  run_id: string;
  run_status: string;
  passed: boolean | null;
  baseline_passed?: boolean | null;
  checks: EvalCheck[] | null;
};

export type EvalSummary = {
  total?: number;
  passed?: number;
  failed?: number;
  critical_failed?: string[];
  regressions?: string[];
  critical_regressions?: string[];
  gate?: 'pass' | 'blocked' | string;
  reasons?: string[];
  model_modes?: string[];
};

export type EvalRun = {
  id: string;
  suite_id: string;
  suite_key: string;
  agent_key: string;
  agent_version_id: string;
  agent_semver: string;
  version_fingerprint: string;
  baseline_eval_run_id?: string;
  status: 'running' | 'completed' | 'failed' | string;
  summary: EvalSummary | null;
  created_by: string;
  created_at: string;
  completed_at?: string;
  stale: boolean;
  results?: EvalResult[];
};

export type ReleaseEnvironment = 'staging' | 'production';

export type ReleaseRequest = {
  id: string;
  agent_key: string;
  agent_version_id: string;
  agent_semver: string;
  eval_run_id: string;
  environment: ReleaseEnvironment | string;
  status: 'pending_review' | 'pending_ops' | 'rejected' | 'published' | 'withdrawn' | string;
  requested_by: string;
  request_note: string;
  reviewed_by?: string;
  review_reason?: string;
  reviewed_at?: string;
  decided_by?: string;
  decision_reason?: string;
  decided_at?: string;
  self_approval_exempt?: boolean;
  created_at: string;
};

export type Release = {
  id: string;
  agent_key: string;
  agent_version_id: string;
  agent_semver: string;
  environment: ReleaseEnvironment | string;
  release_request_id?: string;
  status: 'active' | 'superseded' | 'rolled_back' | string;
  rolled_back_from?: string;
  created_by: string;
  created_at: string;
  ended_at?: string;
};

export type FeedbackClassification = 'factual_error' | 'missing_evidence' | 'format' | 'policy' | 'style' | 'other';

export type FeedbackItem = {
  id: string;
  agent_key: string;
  run_id?: string;
  classification: FeedbackClassification | string;
  note: string;
  reusable_candidate: boolean;
  status: string;
  created_by: string;
  created_at: string;
};

export type FeedbackInput = {
  agent_key: string;
  run_id?: string;
  classification: FeedbackClassification;
  note: string;
  reusable_candidate: boolean;
};

export type RunMetrics = {
  window_hours: number;
  by_status: Record<string, number>;
  by_kind: Record<string, number>;
  p50_duration_ms: number | null;
  p95_duration_ms: number | null;
  tokens_in: number;
  tokens_out: number;
  tool_denials: number;
  grounded_rate: number | null;
  eval_runs_completed: number;
  eval_gate_blocked: number;
  rollbacks: number;
  active_releases: number;
  pending_release_requests: number;
  outbox_pending: number;
  outbox_dead: number;
};

export type IntegrationStatus = {
  configuration: {
    worker_enabled: boolean;
    dev_mock_model: boolean;
    kms_mode: string;
    flowcase_callback_configured: boolean;
    service_clients: number;
    release_environment: string;
    trace_retention_days: number;
    eino_version: string;
    provider_adapter: string;
  };
  queue_depth: Record<string, number>;
};

export type AuditEvent = { actor: string; action: string; resource: string; created_at: string };

export type SIMPackResult = {
  pack_version: string;
  agents: { agent_key: string; outcome: string; agent_version_id?: string; eval_suite_id?: string }[];
};

type Items<T> = { items: T[] };
type Totaled<T> = { items: T[]; total: number };

function query(params: Record<string, string | number | undefined>) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') search.set(key, String(value));
  });
  const text = search.toString();
  return text ? `?${text}` : '';
}

const id = encodeURIComponent;
const post = (body?: unknown): RequestInit => ({ method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) });

export const api = {
  session: () => request<Session>('/session'),
  providers: (limit = 100, offset = 0) => request<Page<Provider>>(`/model-providers?limit=${limit}&offset=${offset}`),
  deployments: (limit = 100, offset = 0) => request<Page<Deployment>>(`/model-deployments?limit=${limit}&offset=${offset}`),
  createProvider: (input: ProviderInput) => request<Provider>('/model-providers', {
    method: 'POST', body: JSON.stringify(input),
  }),
  createDeployment: (input: DeploymentInput) => request<Deployment>('/model-deployments', {
    method: 'POST', body: JSON.stringify(input),
  }),
  setProviderCredential: (providerId: string, credential: string) => request<CredentialStatus>(`/model-providers/${encodeURIComponent(providerId)}/credentials`, {
    method: 'PUT', body: JSON.stringify({ credential }),
  }),
  providerCredentialStatus: (providerId: string) => request<CredentialStatus>(`/model-providers/${encodeURIComponent(providerId)}/credentials`),
  deleteProviderCredential: (providerId: string) => request<CredentialStatus>(`/model-providers/${encodeURIComponent(providerId)}/credentials`, { method: 'DELETE' }),
  probeProvider: (providerId: string, input: { deployment_id: string; mode: 'connectivity' | 'capabilities'; capabilities?: { json?: boolean; tools?: boolean }; confirm_token_cost: boolean }) => request<ProviderProbeResult>(`/model-providers/${encodeURIComponent(providerId)}/probe`, {
    method: 'POST', body: JSON.stringify(input),
  }),
  setModelApproval: (deploymentId: string, environment: ModelApproval['environment'], state: 'approved' | 'revoked', reason: string) => request<ModelApproval>(`/model-deployments/${encodeURIComponent(deploymentId)}/approvals?environment=${encodeURIComponent(environment)}`, {
    method: 'POST', body: JSON.stringify({ state, reason }),
  }),
  modelBinding: (versionId: string) => request<AgentModelBinding>(`/agent-versions/${encodeURIComponent(versionId)}/model-binding`),
  setModelBinding: (versionId: string, deploymentId: string, environment: AgentModelBinding['environment']) => request<AgentModelBinding>(`/agent-versions/${encodeURIComponent(versionId)}/model-binding`, {
    method: 'PUT', body: JSON.stringify({ model_deployment_id: deploymentId, environment }),
  }),
  deleteModelBinding: (versionId: string) => request<void>(`/agent-versions/${encodeURIComponent(versionId)}/model-binding`, { method: 'DELETE' }),
  modelApprovals: (deploymentId: string, environment: ModelApproval['environment']) => request<ModelApproval[]>(`/model-deployments/${encodeURIComponent(deploymentId)}/approvals?environment=${encodeURIComponent(environment)}`),
  agents: (limit = 25, offset = 0) => request<AgentPage>(`/agents?limit=${limit}&offset=${offset}`),
  createAgent: (input: AgentInput) => request<Agent>('/agents', {
    method: 'POST', body: JSON.stringify(input),
  }),
  bootstrapSIM: () => request<{ agents: Agent[] }>('/agents/bootstrap/sim', { method: 'POST' }),
  archiveAgent: (key: string) => request<void>(`/agents/${encodeURIComponent(key)}/archive`, { method: 'POST' }),
  skills: (agentKey: string, limit = 100, offset = 0) => request<Page<Skill>>(`/catalog/skills?agent_key=${encodeURIComponent(agentKey)}&limit=${limit}&offset=${offset}`),
  createSkill: (agentKey: string, input: { key: string; domain: string }) => request<{ skill: Skill; initial_version: SkillVersion }>(`/catalog/skills?agent_key=${encodeURIComponent(agentKey)}`, {
    method: 'POST', body: JSON.stringify(input),
  }),
  skillVersions: (skillId: string, limit = 100, offset = 0) => request<Page<SkillVersion>>(`/catalog/skills/${encodeURIComponent(skillId)}/versions?limit=${limit}&offset=${offset}`),
  createSkillVersion: (skillId: string, input: SkillVersionInput) => request<SkillVersion>(`/catalog/skills/${encodeURIComponent(skillId)}/versions`, {
    method: 'POST', body: JSON.stringify(input),
  }),
  updateSkillVersion: (versionId: string, input: SkillVersionInput) => request<SkillVersion>(`/catalog/skill-versions/${encodeURIComponent(versionId)}`, {
    method: 'PUT', body: JSON.stringify(input),
  }),
  agentVersions: (agentKey: string, limit = 100, offset = 0) => request<Page<AgentVersion>>(`/catalog/agents/${encodeURIComponent(agentKey)}/versions?limit=${limit}&offset=${offset}`),
  createAgentVersion: (agentKey: string, input: AgentVersionInput) => request<AgentVersion>(`/catalog/agents/${encodeURIComponent(agentKey)}/versions`, {
    method: 'POST', body: JSON.stringify(input),
  }),
  updateAgentVersion: (versionId: string, input: AgentVersionInput) => request<AgentVersion>(`/catalog/agent-versions/${encodeURIComponent(versionId)}`, {
    method: 'PUT', body: JSON.stringify(input),
  }),
  tools: (limit = 100, offset = 0) => request<Page<ToolVersion>>(`/catalog/tools?limit=${limit}&offset=${offset}`),
  skillBindings: (agentVersionId: string) => request<SkillBinding[]>(`/catalog/agent-versions/${encodeURIComponent(agentVersionId)}/skills`),
  toolBindings: (agentVersionId: string) => request<ToolBinding[]>(`/catalog/agent-versions/${encodeURIComponent(agentVersionId)}/tools`),
  applySIMPack: () => request<SIMPackResult>('/agents/bootstrap/sim/configure', post()),

  runs: (filter: RunFilter = {}) => request<Totaled<Run>>(`/runs${query({ limit: 25, offset: 0, ...filter })}`),
  run: (runId: string) => request<{ run: Run; evidence: EvidenceRef[] }>(`/runs/${id(runId)}`),
  runEvents: (runId: string) => request<Items<RunEvent>>(`/runs/${id(runId)}/events`),
  cancelRun: (runId: string) => request<Run>(`/runs/${id(runId)}/cancel`, post()),
  sandboxRun: (input: { agent_key: string; agent_version_id: string; input: Record<string, unknown>; evidence_fixture: unknown[]; idempotency_key?: string }) => request<Run>('/sandbox/runs', post(input)),

  evalSuites: (agentKey = '') => request<Items<EvalSuite>>(`/evaluations/suites${query({ agent_key: agentKey })}`),
  createEvalSuite: (input: { agent_key: string; key: string; description: string }) => request<EvalSuite>('/evaluations/suites', post(input)),
  evalCases: (suiteId: string) => request<Items<EvalCase>>(`/evaluations/suites/${id(suiteId)}/cases`),
  createEvalCase: (suiteId: string, input: EvalCaseInput) => request<EvalCase>(`/evaluations/suites/${id(suiteId)}/cases`, post(input)),
  deleteEvalCase: (caseId: string) => request<void>(`/evaluations/cases/${id(caseId)}`, { method: 'DELETE' }),
  startEvalRun: (input: { suite_id: string; agent_version_id: string }) => request<EvalRun>('/evaluations/runs', post(input)),
  evalRuns: (agentKey = '', limit = 25, offset = 0) => request<Totaled<EvalRun>>(`/evaluations/runs${query({ agent_key: agentKey, limit, offset })}`),
  evalRun: (evalRunId: string) => request<EvalRun>(`/evaluations/runs/${id(evalRunId)}`),

  releases: (agentKey = '') => request<Items<Release>>(`/releases${query({ agent_key: agentKey })}`),
  releaseRequests: (status = '') => request<Items<ReleaseRequest>>(`/releases/requests${query({ status })}`),
  createReleaseRequest: (input: { agent_version_id: string; eval_run_id: string; environment: ReleaseEnvironment; note: string }) => request<ReleaseRequest>('/releases/requests', post(input)),
  reviewRelease: (requestId: string, input: { approve: boolean; reason: string }) => request<ReleaseRequest>(`/releases/requests/${id(requestId)}/domain-review`, post(input)),
  decideRelease: (requestId: string, input: { approve: boolean; reason: string }) => request<ReleaseRequest>(`/releases/requests/${id(requestId)}/ops-decision`, post(input)),
  withdrawRelease: (requestId: string) => request<ReleaseRequest>(`/releases/requests/${id(requestId)}/withdraw`, post()),
  rollbackRelease: (releaseId: string, reason: string) => request<Release>(`/releases/${id(releaseId)}/rollback`, post({ reason })),

  feedback: (agentKey = '') => request<Items<FeedbackItem>>(`/feedback${query({ agent_key: agentKey })}`),
  createFeedback: (input: FeedbackInput) => request<FeedbackItem>('/feedback', post(input)),

  metrics: (windowHours = 24) => request<RunMetrics>(`/metrics${query({ window_hours: windowHours })}`),
  integrationStatus: () => request<IntegrationStatus>('/integrations/status'),
  auditEvents: (limit = 50, offset = 0) => request<Totaled<AuditEvent>>(`/audit-events${query({ limit, offset })}`),
};
