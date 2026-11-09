# PRODUCT REQUIREMENTS DOCUMENT — AI Agent Studio & Go/Eino Runtime

**Document ID:** AAS-PRD-001  
**Version:** 1.1 — model-provider management addition  
**Date:** 30 October 2026  
**Product working name:** **Agent Studio** (mini-app) + **Agent Runtime** (Go service)  
**Status:** Engineering proposal. The referenced SIM PRD v3.1 is a working draft; its RACI/approver allocations still require organizational decision.  
**First domain pack:** SIM B2B Solution Workspace (six agents).  
**Scope:** Reusable platform usable by Flowcase, SIM and future workflow mini-apps.

## 0. Decisions

1. **Runtime:** Go + CloudWeGo **Eino ADK** (`ChatModelAgent`, `Runner`, tool execution, checkpoint integration) for bounded AI agent runs. Eino graph/composition can be used for AI-specific deterministic pipelines; **Flowcase** remains the durable **business** workflow engine and source of truth for human task / G0–G5 approval.
2. **UI:** React + TypeScript + Vite after converting the attached standalone HTML reference. Enterprise Navy (#101C34), Cobalt (#2864EC), paper-white content, bento tiles with clear contrast; **no login page** in mini-app.
3. **Database:** PostgreSQL as authority for agent/skill/tool metadata, immutable release versions, evaluation cases, runs, audit and approvals. No new standalone KMS, Elasticsearch, or Docling for this service.
4. **KMS:** Existing KMS (`Go + PostgreSQL + Elasticsearch + Docling`) is an API-backed tool for authorized evidence retrieval. Runtime receives citations, source IDs, ACL-scoped results and versions; does **not** access KMS DB/Elasticsearch directly.
5. **Flowcase:** External workflow orchestration invokes named **published agent versions** with scoped input and idempotency key. Agent completion returns **draft artifacts / findings**, not an approval. Flowcase owns case state, human review, decision state and material change invalidation.
6. **Skills:** Versioned instructions, procedures, examples, tool permissions, source collections, output schema, guardrails and evaluation datasets—not automatic LLM weight fine-tuning. Reusable feedback is promoted only after independent domain review, tests and AI Ops release.
7. **Deployment:** Start with `agent-studio-api` (Go modular monolith), `agent-worker` (Go/Eino) and React mini-app. A PostgreSQL outbox and durable work queue enforce idempotent processing. Separate components later only when justified.
8. **Model:** Pluggable `ChatModelProvider` / Eino ChatModel adapter with per-tenant approved model allowlist, request limits, timeouts and credential isolation. No vendor or model is mandatory.
9. **Authentication:** Parent host handles sign-in. Agent Studio validates trusted OIDC/session exchange server-side, then evaluates tenant/workspace/agent/source permissions on every API/tool call. Local mock identity **dev-only**.

## 1. Why this product exists

The SIM PRD mandates six specialist agents governed by domain SMEs, with editable case output, skill evaluation, versioned publication and rollback. If agent configuration and prompt execution are reimplemented separately in Intake, Design and Proposal mini-apps, quality, traceability and security fragment. The reusable Agent Studio centralizes **authoring, testing, release, runtime execution and observability** while leaving domain approvals in Flowcase.

**Outcomes:**
- Create agents using stable IDs, scoped skills, named tools and output contracts.
- Test changes before publication on positive, negative, adversarial and evidence-conflict scenarios.
- Execute a pinned version from Flowcase with case-scoped access to the KMS.
- Inspect run traces, sources, risk flags, model/tool versions and resulting artifact IDs.
- Prevent an agent from self-approving customer commitments or creating new global knowledge by itself.

## 2. Boundary map

| Component | Owns | Must not own |
|---|---|---|
| Agent Studio API | agent/skill/tool registry, sandbox evaluations, skill semantic-review and AI Ops release records, run metadata | final bid/pricing authority, KMS master records |
| Agent Worker using Eino | model/tool calls, structured output generation, execution events, bounded interrupt/checkpoint, trace and retries | durable business process, G0–G5 gate approval |
| Flowcase | workflow definitions/instances, task, SME review, gate decision, artifact dependency and case approval | executing low-level LLM loops, owning agent prompts |
| KMS | governed sources, content parsing, ACL search, citations, freshness, source lifecycle | agent release approval, Flowcase gate status |
| Host app | login/SSO, mini-app navigation and trusted identity bootstrap | authorization bypass over agent tools |

**Important distinction:** Eino `Runner` checkpoint/resume is **agent-run-local** and is not a substitute for days-long human approval queues. For human reviews, persist a proposed artifact and send a review task to Flowcase; a new authorized run/resume action occurs only through an explicit contract.

## 3. Roles and permissions

| Role | Rights (resource-scoped) |
|---|---|
| Agent Viewer | View allowed published agent metadata and own run output; redacted trace as authorized |
| Agent Author | Create/edit **draft** agent definition and skill changes within assigned domain |
| Skill Trainer | Propose/edit skill guidance, examples, evaluation cases and feedback classification |
| Domain Reviewer | Independently sign/reject semantic correctness of a tested skill version; may review case output only when separately assigned |
| AI Ops / Release Approver | Approve technical release, publish / rollback signed versions, monitor runs; no implicit case decision authority |
| Tool Admin | Register audited connector implementation, parameter schema, execution policy, scopes and risk category |
| Platform Admin | Connection policy, model allowlist, tenant configuration and security administration (subject to separation of duties) |
| Flowcase Service Identity | Invoke allowlisted, pinned agent versions with case-scoped authorization; receive validated callback |

Permission tuple: `subject, tenant, workspace, domain, agent, action, source_scope, delegation, effective/expiry`. Do not infer permission solely from UI visibility. Separate **case artifact approval**, **skill semantic approval**, **production release** and **proposal final release**.

## 4. Functional backlog

| ID | Priority | Feature & acceptance |
|---|---|---|
| AAS-01 | P0 | Agent registry: list/create/clone/archive agent IDs; per-agent owner/domain, status, description, bounded model selection and allowed tools |
| AAS-02 | P0 | Skill registry: immutable versions of instructions, SOP/checklists, few-shot cases, forbidden commitments, input/output JSON Schema, retrieval policy |
| AAS-03 | P0 | Tool registry: named connectors (KMS search, artifact read/write **draft**, calculator, allowlisted external APIs), input schema, scopes, execution caps |
| AAS-04 | P0 | Go + Eino ChatModelAgent runtime with Runner event consumption, streaming trace, bounded step/token/time budget, cancellation and retry |
| AAS-05 | P0 | Version-pinned request/response contract for Flowcase; idempotent run creation, callback signature verification, result dedupe |
| AAS-06 | P0 | KMS evidence tool applies tenant/case/source scope authorization; stores citations, freshness and source versions; no fabricated source IDs |
| AAS-07 | P0 | Sandbox/playground: inputs, optional permitted case context, run against selected **draft** version with safe mocks or allowed test connectors |
| AAS-08 | P0 | Evaluations: gold/negative/adversarial tests, deterministic validators, human rubric, comparison to published baseline; regression gate |
| AAS-09 | P0 | Skill lifecycle: draft → sandbox → evaluated → domain review → release approval → published → monitored → retired/rolled back |
| AAS-10 | P0 | Artifact output: strict schema validation, source references, assumptions and unresolved questions; high-risk claims require human review |
| AAS-11 | P0 | Run history, trace, model/skill/tool versions, redacted input, usage, errors, audit and replay metadata |
| AAS-12 | P0 | Host identity adapter, per-tenant model/tool/source policy, secrets management, rate limits, audit, redaction and prompt-injection defenses |
| AAS-13 | P1 | Batch evaluation, trend dashboards, comparison of vNext to baseline, approval comment thread and canary release |
| AAS-14 | P1 | Feedback loop from Flowcase SME correction → reusable skill change candidate (no automatic publication) |
| AAS-15 | P1 | Blueprints for six fixed SIM specialists and bounded SME mapping; optional template export to other workspaces |
| AAS-17 | P0 | Model & Provider Management: register approved provider endpoints and model deployments, encrypted secret references, model capability probes, per-tenant allowlist, budgets, lifecycle/approval, and agent-version model binding. |
| AAS-18 | P0 | Runtime model resolution: verify selected deployment is approved for tenant/environment, resolve secret server-side, instantiate Eino ChatModel, pin provider/model/config versions per run; fail closed on missing/revoked credentials. |
| AAS-16 | P2 | Advanced multi-agent planning, graph visual designer, organization marketplace, automatic quality clustering |

**Scope note:** SIM PRD FR-13 and FR-19–23 imply core skill ownership/version/release and six specialist identities are P0 **for SIM integration**, even if advanced dashboards and feedback ergonomics follow later.

## 5. SIM six-agent pack

| Agent key | Contract/output | Typical human domain review |
|---|---|---|
| `sim.intake` | atomic requirements + RFP source anchors + ambiguities + compliance skeleton | Interface / Bid / Industry, G1 |
| `sim.solution_matching` | capability fit-gap, reuse/custom/hybrid options with vetted product sources | Tribe Capability / Product, G2 |
| `sim.standard_project` | baseline WBS, roles, mandays, staffing assumptions and scope exclusions | PMO / Delivery / Chapter, G3 |
| `sim.design` | HLD option, integrations, NFR/security/data design, assumptions and risk | SA / DPE / PAM / AIX / Security, G2–G3 |
| `sim.cost_challenge` | missing-cost/variance findings, assumptions, scenario comparisons, suggested optimizations | Costing / Finance / Delivery, G3; **cannot approve price** |
| `sim.proposal` | evidence-backed proposal section drafts + mandatory coverage + red-team findings | Bid Owner / SA / Legal as needed, G4–G5 |

These IDs must be exposed as six separate **agent identities**, but may share Go runtime, tools, policies and Eino execution code. SIM Orchestrator is a coordinator integrated via Flowcase; **not a seventh mandatory specialist**.

## 6. Domain model (PostgreSQL)

Minimum logical tables (names illustrative):

- `llm_providers(id, tenant_id, key, adapter_type, base_url, secret_ref, enabled, approved_by, created_at)` — metadata/secret references only; never raw keys.
- `llm_model_deployments(id, tenant_id, provider_id, provider_model_id, display_name, capabilities_json, max_output_tokens, timeout_ms, cost_policy_json, approval_state, effective_at, config_version)`.
- `agent_model_bindings(agent_version_id, model_deployment_id, fallback_model_deployment_id, override_policy_json)` — immutable with released version.
- `model_health_checks(id, deployment_id, outcome, checked_at, redacted_error_code)`; `model_usage_records(run_id, deployment_id, tokens_in, tokens_out, latency_ms, estimated_cost)`.
- `agents(id, tenant_id, key, display_name, domain, owner_role, lifecycle_status, created_by, created_at)`
- `agent_versions(id, agent_id, semver, manifest_json, input_schema_json, output_schema_json, model_policy_ref, tool_policy_ref, checksum, state, created_at)`
- `skills(id, agent_id, key, domain, owner_subject, created_at)`; `skill_versions(id, skill_id, semver, instructions, procedures_json, examples_json, forbidden_actions_json, allowed_context_json, output_schema_json, checksum, state)`
- `agent_skill_bindings(agent_version_id, skill_version_id)`
- `tools(id, key, kind, owner, risk_level, schema_json, lifecycle_status)`; `tool_versions(id, tool_id, adapter_ref, policy_json, checksum, published_at)`
- `agent_tool_bindings(agent_version_id, tool_version_id, action_scope_json)`
- `eval_suites(id, agent_id, key, classification)`; `eval_cases(id, suite_id, input_json, expected_json, validators_json, approved_test_data_ref)`; `eval_runs(id, agent_version_id, baseline_version_id, summary_json, status, created_at)`; `eval_results(id, eval_run_id, case_id, result_json, pass)`
- `release_requests(id, agent_version_id, target_environment, rollout_scope, semantic_reviewer_id, semantic_review_status, ops_approver_id, ops_status, decision_json, created_at)`; `releases(id, agent_id, version_id, scope, effective_at, rolled_back_from, status)`
- `agent_runs(id, tenant_id, agent_id, agent_version_id, source, external_correlation_id, idempotency_key, case_ref, input_ref, status, checkpoint_ref, output_artifact_ref, error_code, created_at, completed_at)`
- `agent_run_events(id, run_id, seq, event_type, redacted_payload_json, created_at)`; `agent_evidence_refs(run_id, source_id, source_version, page_anchor, trust_state, citation_uri)`
- `feedback_items(id, agent_id, run_id, source_case_ref, classification, diff_ref, root_cause, reusable_candidate, status)`
- `audit_events(id, tenant_id, actor, action, resource_ref, before_hash, after_hash, correlation_id, created_at)`; `outbox_events`, `inbox_dedupe`.

All cross-tenant queries must be scoped. Write-once signed versions, append-only approvals and audit; avoid overwriting published agent/skill versions. Sensitive payloads stored as encrypted references and redacted in logs.

## 7. Runtime processing

**Invocation**
1. Flowcase requests `agent_key`, pinned published `agent_version`, case reference, input artifact refs, tenant, role/scope, deadline, `idempotency_key` and callback destination ID (pre-registered, not user-supplied URL).
2. Studio API verifies service identity, tenant/case authorization, signature/credentials, budget, version visibility and schema; commits `agent_runs` + outbox event transactionally.
3. Worker claims run with bounded lease and retrieves published immutable agent/skill/tool manifests; reads allowed context via KMS tool under delegated, minimal scopes.
4. Use Eino ADK `ChatModelAgent` via `Runner`; record **safe** run events, tool results with provenance and structured schema output.
5. Validate output schema and semantic rules (e.g. material claims need evidence). Fail with explicit policy code, or return `needs_review` with unresolveds; never mark approved.
6. Persist artifact draft reference and evidence manifest; send signed versioned callback/event to Flowcase using transactional outbox and at-least-once delivery.
7. Flowcase verifies callback and decides assigned SME review and downstream workflow advancement; its approval table remains authoritative.

**Reliability:** idempotent API, deterministic dedupe key `(tenant, external_correlation_id, idempotency_key)`, retry with jitter, poison queue / manual replay, worker heartbeats, cancellation and time budget. At-least-once deliveries. No arbitrary remote URL tool invocation. No free-form arbitrary code execution.

**Checkpoint:** Eino ADK `RunnerConfig.CheckPointStore` may be used for within-run interrupt/resume (version-compatible adapter). Store checkpoint metadata with TTL, encrypted payload and pinned runtime version. Do not confuse with Flowcase task waiting days for human approval; Flowcase owns those states.

## 8. Tool policies

`ToolDefinition`: `key`, `version`, `adapter_ref`, `input_schema`, `output_schema`, `sensitivity`, `allowed_agents`, `tenant_allowlist`, `required_scopes`, `side_effect_category`, `timeout_ms`, `max_calls`, `audit_mode`.

- `kms.search` / `kms.fetch_evidence`: read-only and ACL-aware; result includes `source_id`, `source_version`, `citation_anchor`, `status`, `effective/expiry`, `classification`, permitted excerpt. Avoid raw cross-customer retrieval.
- `flowcase.artifact.get`: read only authorized structured artifact snapshot; no unauthorized browsing.
- `flowcase.artifact.propose`: create **draft proposal** artifact only; no approval/status bypass.
- `costing.read_reference`: restricted roles; redact financial data before irrelevant model contexts.
- `math.calculate`: deterministic safe utility; use for cost calculations, not unconstrained model arithmetic.

Tool calls must check permissions again **inside tool adapter**. Document/agent text is untrusted and cannot override tool policy. Reject hidden prompt instructions in RFP/context; disable unsafe tools in sandbox by default.

## 9. Agent skill/version lifecycle

`draft → sandbox → evaluated → domain_review → release_approval → published → monitored → retired / rolled_back`.

1. Trainer writes change as a new immutable candidate version. A prompt edit never mutates a published version.
2. Sandbox uses approved test fixture or safe mocked KMS/Flowcase tools; logs redacted output, no real side-effect tool calls by default.
3. Evaluation includes mandatory requirement recall, evidence citation presence/validity, output JSON schema, tool allowlist, no sensitive data leakage, contradiction handling, forbidden promise detection, cost constraints, test cases under missing evidence.
4. Compare vNext with baseline. Failing critical regression blocks release even if aggregate score looks high.
5. Independent **domain** reviewer signs semantic correctness. **AI Ops** separately authorizes production release / rollback; segregate self-approval when policy requires.
6. Canary/rollout policy affects **new runs only**; pinned case/runs remain on previous versions unless specifically re-run through Flowcase impact controls.

## 10. APIs (proposal)

Base: `/api/v1` (all tenant/workspace scope resolved by verified host token and server-side membership; external paths are not trusted by themselves).

| Method | Route | Intent |
|---|---|---|
| `GET/POST` | `/model-providers` | List/register provider metadata, tenant-scoped Platform Admin only for writes |
| `POST` | `/model-providers/{id}/credentials` | Write-only secret rotation through server-side vault; never return secret content |
| `POST` | `/model-providers/{id}/test` | Authorized connection/capability probe with outbound host allowlist and redacted results |
| `GET/POST` | `/model-deployments` | List/register model IDs, capabilities, budgets and policies |
| `POST` | `/model-deployments/{id}/approval` | Authorize model for tenant/environment after capability test and policy review |
| `GET/PUT` | `/agents/{key}/draft/model-binding` | Select an allowed deployment for agent **draft**; requires re-evaluation before publishing |
| `GET/POST` | `/agents` | List/create scoped agents |
| `GET` | `/agents/{key}` | Detail and current published version |
| `POST` | `/agents/{key}/versions` | Create new draft from manifest |
| `GET/PUT` | `/skills/{id}/draft` | Read/edit **draft** skill only |
| `GET/POST` | `/tools` | List tools / tool-admin create |
| `POST` | `/runs` | Run published version; sync ack with run ID, async result |
| `GET` | `/runs/{id}` | Run status/result artifact references |
| `GET` | `/runs/{id}/events` | Event stream/paginated redacted trace |
| `POST` | `/sandbox/runs` | Sandbox against draft version with test fixture (never auto-publish) |
| `POST` | `/evaluations/runs` | Start evaluation suite run |
| `GET` | `/evaluations/runs/{id}` | Metrics, failed cases and baseline comparison |
| `POST` | `/releases/requests` | Request publish, review with authority |
| `POST` | `/releases/{id}/domain-review` | Semantic signoff |
| `POST` | `/releases/{id}/ops-decision` | Publish/deny after gates |
| `POST` | `/releases/{id}/rollback` | Controlled rollback, new-run routing only |
| `GET/POST` | `/feedback` | List/create corrections and learning candidates |
| `GET` | `/integrations/status` | Connector health and capability negotiation |

**Ingestion KMS PDF processing and Elasticsearch index management are NOT implemented in Agent Studio.** These remain in KMS service.

## 11. Integration contracts

### Flowcase → Agent Studio (invoke)
- authenticated service-to-service call (mTLS or OAuth2 client assertion / signed service token), tenant/case membership validated;
- `correlation_id`, `workflow_instance_id`, `workflow_node_id`, `case_id`, `agent_key`, `agent_version_id`, `skill_version_ids`, `input_artifact_refs`, `knowledge_scope`, `output_schema_ref`, `deadline`, `idempotency_key`;
- response: `run_id`, `accepted`, `version_hash`, `status_url`.

### Agent Studio → Flowcase (completion callback)
- `event_id`, `run_id`, `case_id`, `node_id`, `status`, `artifact_draft_ref`, `source_manifest`, `skill_version_hash`, `risk_flags`, `schema_validation`, `created_at`; signed, deduped and tested with out-of-order retries.
- **No `approved=true` field** can authorize a gate. A status of `needs_review` creates an independent Flowcase task, not an agent self-approval.

### Agent Studio → KMS
- authorized `search(query, scope, permitted_collection_ids, source_types, version_policy, freshness)` and `fetch_evidence(source_id, version, anchor)`; response contains permitted snippets + citation IDs, classification, status, version; distinguish `published`, `expired`, `conflicted`, `unverified`.
- If API missing, use adapter mocks in test env; fail closed for production evidence-dependent claims.

## 12. Security / NFR

- Wrapper login only; OIDC token verification / trusted backend token exchange; no frontend-auth header shortcuts. `DEV_MOCK_AUTH` must hard-fail at startup in non-dev environments.
- Production auth scopes `tenant`, `workspace`, `agent`, `case`, `action`, `knowledge collections`, and **tool action**. Use short-lived delegated tokens or validated service-identity claims; never trust arbitrary scope sent by an agent prompt.
- Audit every author/edit/eval/review/release/rollback/invoke/tool attempt. Store redacted trace, never secrets, raw personal info or commercial prices in standard logs.
- Prompt-injection defense: RFP/KMS content is evidence, cannot modify system instruction, tool allowlist or approval policy.
- Timeout/step budgets, rate limits, quota per tenant/agent, cost caps, input size limits and max tool calls. Disable dangerous external tools by default.
- Retry and cancellation are safe; per-run deployment version is pinned; source expiry triggers revalidation through KMS/Flowcase policy.
- Service metrics: run success/timeout/error, p95 run duration, eval critical pass, grounded claim rate, tool denial rate, rollback frequency, reviewer rework; set pilot baselines before SLO targets.
- Trace retention/deletion and data residency must be approved by Security/Legal; encryption at rest/in transit and secret rotation mandatory.

## 13. UI / IA

1. **Overview:** active agents, test health, release queue, run activity, operational alerts.
2. **Agent Registry:** list six SIM agent identities plus reusable agents, filter by domain/status, owner, published version.
3. **Agent Builder:** definition/manifest, model policy, tool bindings, source collections, JSON schema, safe version draft editor.
4. **Skills Studio:** skill versions, procedures, examples and reusable feedback; explicit distinction from KMS knowledge.
5. **Models & Providers:** provider endpoints, secure credential setup status, capability tests, model deployment registry, allowlists, usage/budgets and per-agent binding.
6. **Tool Library:** KMS, Flowcase, costing, math connectors with scope and risk.
7. **Playground:** input + pinned version + simulated output trace/evidence; clearly labelled demonstration without live model.
8. **Evaluations:** gold cases, evaluation history, critical blockers, baseline differences.
9. **Releases:** semantic reviewer queue, Ops release approval, rollout/rollback, change history.
10. **Runs & Audit:** run events, model/tool/skill hashes, evidence, failures and correlation IDs.

Desktop design: deep navy sidebar, cobalt accent cards, pure-white primary surfaces, restrained glass for overlay only, rounded bento overview, crisp typography. Responsive: sidebar drawer; tables scroll/stack. No decorative AI hero graphics.

## 14. Acceptance / UAT

- **UAT-01:** six SIM agents discoverable, each with owner, skill and allowed tool manifest; identities remain distinct.
- **UAT-02:** Flowcase invokes `sim.intake@1.0.0` twice with same idempotency key → one `run_id`, no duplicate side effects.
- **UAT-03:** unapproved/expired KMS source returned → agent marks unverifiable and cannot elevate to contractual fact.
- **UAT-04:** SME edits Design output v1 → artifact v2, diff, evidence and approval recorded in Flowcase; source artifact v1 retained.
- **UAT-05:** trainer edits Design skill → draft vNext; an existing published run remains pinned to previous skill version.
- **UAT-06:** vNext fails critical regression/has no domain signoff → Ops production publish rejected with explicit reason.
- **UAT-07:** domain signoff + passing eval + Ops authorization → new version published; subsequent runs follow rollout scope, existing pinned case unchanged.
- **UAT-08:** agent tries costing tool outside delegated permission → denied even if UI displayed tool or prompt requests it; denial audited.
- **UAT-09:** AI returns unsupported `99% accuracy` claim without benchmark → output marked needs_review/unsupported, not release-ready.
- **UAT-10:** re-running an agent after release doesn't silently update frozen Flowcase proposal; impacted section approval is reopened where necessary.
- **UAT-11:** iframe/host wrapper identity tampering or caller-controlled `X-User-ID` is rejected in production.
- **UAT-12:** worker restart mid-run resumes or safely replays according to idempotent contract; duplicate callback does not complete Flowcase node twice.

## 15. Delivery milestones (sequence, not deadlines)

| Phase | Deliverable | Exit |
|---|---|---|
| P0 — Interface alignment | Verify Flowcase/KMS contracts, Eino version, host auth, allowed models, representative tender data, policies | Signed API mocks and integration tests |
| P1 — Foundation | Go modules, PostgreSQL migrations, agent registry, skill/tool version models, verified auth interface, audit | Six agents bootstrapped, views are DB-backed |
| P2 — Runtime & integrations | Eino ChatModelAgent/Runner, Flowcase invoke/callback, KMS evidence adapter, structured outputs, durable worker | end-to-end `sim.intake` sample case produces reviewable artifact |
| P3 — Studio & governance | Builder/skills UI, sandbox, eval harness, review/publish/rollback, run tracing | regression rejection and version-pinned tests pass |
| P4 — SIM rollout | Six agent definitions, SME mapping, observability, multi-agent boundary tests, security review | acceptance suite and two approved pilot cases pass |

## 16. Unresolved decisions

1. Host authentication SDK / iframe vs route-proxy, issuer/audience and tenant membership API.
2. Approved LLM provider(s), model capabilities, regions, budgets, streaming and key-management policy.
3. KMS retrieval API response shape, source version/ACL convention and evidence expiry policy.
4. Flowcase agent-task request/callback schema, agent artifact interface and retries/timeout semantics.
5. Authorized SME domain owners, independence/DoA thresholds, semantic signoff and AI Ops release authority.
6. Retention and reusability policies for tender examples; customer-sensitive feedback must not enter global skill sets without approval.
7. Deployment topology, secret vault, production telemetry/sanitization and disaster recovery targets.

## 17. Source basis

**Source-derived:** SIM B2B Solution Workspace PRD v3.1 (30 Oct 2026), especially §§6–7, 10–12, 15–17, 19 and 21. **Related design decisions:** Flowcase Workflow & Case Platform Technical PRD v1 and KMS technical package from this project. **External verified framework reference:** CloudWeGo Eino ADK Quickstart, Runner/Extension and examples (URLs in `README_START_HERE.md`). Names, endpoints, table names and new lifecycle extensions are engineering **recommendations**, not source-mandated company policy.

## 18. Model & Provider setup — added in v1.1

**Setup location:** Agent Studio → **Models & Providers** (Platform Admin / AI Ops). Agent Studio → **Agent Builder** binds an approved model deployment per draft agent version. Flowcase only selects an agent version and never holds provider keys. KMS only exposes knowledge retrieval; Docling and Elasticsearch stay within KMS.

**Configuration hierarchy:** `Provider Connection → Model Deployment → Tenant/Environment Approval → Agent Version Binding → Runtime Pinned Snapshot`.

1. **Provider:** adapter type (`openai`, `openai_compatible`, `azure_openai`, `ollama` or custom), server-validated base URL, tenancy, status, credentials reference (write-only secret management), TLS/egress policy.
2. **Deployment:** exact provider model ID (not marketing label), capability flags verified by test (streaming/tool calls/JSON/schema), timeouts, output-token caps, usage quotas, cost estimation, fallback strategy when approved.
3. **Agent binding:** a draft agent may select only a deployment approved for that tenant, environment and purpose. Saving a changed binding invalidates prior evaluation/signoff; published agent versions retain pinned model/provider policy.
4. **Worker:** resolves provider + model metadata, gets short-lived secret from vault, builds appropriate Eino `ChatModel` adapter, constructs `ChatModelAgent`, then executes via `Runner`. No API keys, base URLs or model IDs from untrusted prompts.
5. **Operations:** connection health tests and capability probes must happen in the backend, not from the browser. Redact responses. Disable misconfigured connections; support rotation, emergency revoke, usage telemetry and model deprecation re-evaluation.

**Security:** enforce HTTPS in production (or separately approved trusted private-network policy), DNS/IP restrictions to prevent SSRF, outbound allowlist, strict tenant separation, vault/KMS secret encryption, no secrets in audit/trace/log/browser storage, and server-side authorization for model approval. Runtime must fail closed if the selected model loses approval or credential access; do not silently switch to an unapproved fallback.

**Prototype limitation:** the supplied standalone HTML models provider/model **metadata only**, with explicit demo approval controls. It never asks for or stores API keys and it never makes live LLM requests. Production must replace local state with API-backed source-of-truth.

**Added UAT:**
- An Agent Author cannot bind an unapproved model or use a model outside their tenant scope.
- Changing a model deployment or model binding invalidates candidate evaluations and domain release signoff.
- Provider credentials cannot be read back through API/UI and never appear in logs, localStorage or run traces.
- A revoked credential/model prevents execution and records a redacted reason; no silent fallback to unapproved providers.
- An authorized Platform Admin can test a provider, document capability results and approve a model after required policy review.
