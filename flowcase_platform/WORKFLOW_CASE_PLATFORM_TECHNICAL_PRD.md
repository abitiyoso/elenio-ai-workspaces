# PRODUCT REQUIREMENTS DOCUMENT — Workflow & Case Platform

**Document ID:** WCP-PRD-001  
**Version:** 1.0 (proposed engineering baseline)  
**Date:** 30 October 2026  
**Status:** Technical proposal for implementation, **not** an approved SIM organizational policy.  
**Audience:** Product, Platform Engineering, SIM, AI Ops, Security, domain owners, QA.  
**Product:** **Flowcase** — reusable Workflow & Case Management mini-app platform. Brand/name can change.

## 0. Executive decisions

- **Build a reusable platform, not a hardcoded SIM tender app.** A `Case` is an instance of a **published, immutable workflow definition version**.
- The first domain pack is `sim-tender` derived from **SIM B2B Solution Workspace PRD v3.1**. An unrelated `internal-poc-request` pack demonstrates real reuse.
- **One mini-app workspace** can show workflow catalog, builder, cases, tasks, approvals, and audit. Domain-specific views (RFP compliance, HLD, costing, proposal) are separately registered plugins/modules, not hardwired engine concepts.
- The engine coordinates **humans, tools, AI agents, parallel work, conditions, timers, approval gates, and rework**. Runtime is deterministic; AI proposes outputs but never self-approves.
- Workflow **definitions, instances, tasks, approvals, artifacts, and audit** belong in **PostgreSQL**. **KMS** is an external governed knowledge/evidence service accessed via API; **Elasticsearch/Docling belong behind KMS**, not duplicated here.
- Go backend, PostgreSQL, Go background worker, React + TypeScript + Vite UI. Build a **modular monolith + worker** initially; deploy separately only when justified.
- **No KMS replacement, no authentication form inside mini-app**. Trust the host only after **server-side verification/exchange**; all authorization remains enforced at the backend.
- **Workflow visual editing** may begin with form-based node editor, read-only flow diagram and validated JSON import/export. Drag-and-drop builder is a later feature, not required for MVP.

## 1. Problem and objective

### Problem
Tender and other multi-step business processes are scattered across files, chat and manually routed approvals. If SIM G0–G5 are embedded directly in code/UI, every policy or path change requires rework and risks invalidating in-flight cases. Different departments also need many of the same capabilities: state, tasks, SLA, approvals, artifacts, audit, and AI execution.

### Goals
1. Create, validate, version and publish workflow definitions as reusable configurations.
2. Execute cases reliably, including conditions, fork/join parallel branches, timers, human tasks, agent tasks, retry/rework, and deadline constraints.
3. Give each user an actionable case workspace and task inbox, with evidence and decision lineage.
4. Enforce human review, authority, segregation-of-duties and revision-specific approvals.
5. Integrate KMS retrieval/source references and shared AI Agent Runtime without coupling to their internal databases.
6. Prove reuse with `sim-tender` and a non-tender PoC workflow, without recompiling engine business logic.

### Non-goals for MVP
- Full BPMN 2.0 conformance, arbitrary graphical BPMN editing, arbitrary code execution in workflow definitions.
- Replacing SSO/IAM, CRM, KMS, Docling, Elasticsearch, PMS, pricing systems or delivery management.
- Autonomous commercial approval or proposal submission by AI.
- Automatic migration of running cases when a new workflow version is published.
- Production-grade fine-tuning of LLM weights.

## 2. Source fidelity and boundaries

**Source-derived from SIM PRD v3.1:** opportunity/case as controlled unit of work; G0–G5 human gates; six specialist agents (Intake, Solution Matching, Standard Project, Design, Cost Challenge, Proposal); parallel specialist tasks; SME reviews; immutable decision records; impacted-artifact re-review on changes; pinned skill/source versions and proposal freeze; KMS external/federated evidence rather than replacement.

**New engineering recommendations in this PRD:** generic `WorkflowDefinition`, generalized node taxonomy, reusable workflow editor, schema, event processing design, API naming, `Flowcase` UI, technology and milestones. These are implementation proposals, **not claims that SIM PRD mandated a generic workflow product**.

**Still pending policy decisions:** actual G0–G5 approvers/roles, risk exceptions, DoA rules, CRM source of truth, production identity/wrapper contract, AI runtime details, KMS API availability, document/PII retention and approval thresholds. No code may hardcode these as official company policies.

## 3. Personas and authorization

| Role | Permissions (scoped to tenant/workspace/case) |
|---|---|
| Workspace Member | View permitted cases, perform assigned tasks, see own activity. |
| Case Owner | Create case, assign tasks, update case data/artifacts within policy, request review. |
| Workflow Designer | Create/edit draft workflow definitions; validate and request publication. |
| Workflow Publisher | Publish approved definition version, deprecate/rollback to earlier published version for **new cases**. |
| SME Contributor | Add/edit assigned domain artifact and comments; not automatically approve. |
| Domain Approver | Approve/reject assigned domain artifact or gate **only with delegated authority**. |
| AI Ops | Operate runners/connectors/agent skill deployment; no implicit case approval. |
| Tenant Admin | Manage permitted organizational policy, roles and connector configuration; restricted by separation of duties. |

**Authorization is server-side** and combines role, action, resource, tenant, case membership, sensitivity, delegations, time window, and separation-of-duties. Reject unknown identities. No header like `X-User-ID` may be trusted directly from arbitrary browsers.

### Mini-app host integration
Preferred: host uses OIDC/OAuth identity and a trusted backend gateway or **token exchange** yielding an audience-restricted, short-lived KMS/Flowcase token. Backend validates issuer, signature/JWKS, audience, expiry, nonce/session binding where relevant, and tenant membership; authorization is evaluated per request. If iframe-based, enforce allowlisted origins/frame-ancestors, strict `postMessage` origin check; do not pass reusable bearer tokens in URL/query or persist them in localStorage. CSRF controls required for cookie sessions. Dev mock identity only under explicit local development guard and cannot be deployed enabled.

## 4. Main functional modules

| ID | Priority | Module | Acceptance / outcome |
|---|---|---|---|
| WCP-01 | P0 | Workflow Registry | Create, duplicate, list, validate, publish and deprecate **immutable versions**. |
| WCP-02 | P0 | Workflow Definition Validator | Validate node IDs, referenced targets, reachable start/end, supported types, schema, condition safety, split/join balance, cycles/loops policy and undefined policy references. |
| WCP-03 | P0 | Runtime Engine | Deterministic state transitions, persistent tokens, safe retries, conditional routing and version-pinned execution. |
| WCP-04 | P0 | Case Workspace | Start, view, pause/resume (authorized), cancel, inspect stages, timeline, blockers, artifacts, deadline, relevant tasks. |
| WCP-05 | P0 | Task & Assignment | Human tasks, SME requests, assignment/claim/delegation, due dates, queues, status and escalations. |
| WCP-06 | P0 | Approval & Gate | Policy-driven approval, reject, revision, exception where allowed; version-specific signer and immutable decision history. |
| WCP-07 | P0 | Artifact & Dependency | Typed artifacts, immutable versions, dependency edges, selective invalidation, re-review; attach KMS source references. |
| WCP-08 | P0 | Agent & External Task Bridge | Durable agent invocation with explicit input/output contract, skill version, KMS retrieval scope, callback validation and idempotency. |
| WCP-09 | P0 | Audit, Outbox, Recovery | Append-only audit, transactional outbox, worker dedupe/inbox, recoverable jobs, correlation IDs. |
| WCP-10 | P0 | Host Auth Adapter | OIDC/session trust validation, tenant isolation, resource ACL and dev-only mock. |
| WCP-11 | P1 | Builder UX | Visual graph with node inspector, form-based edits, validation errors and JSON import/export. No drag-drop required. |
| WCP-12 | P1 | Templates & Domain Packs | SIM Tender v1 and PoC v1, with configurable policies/connectors/forms/agent bindings. |
| WCP-13 | P1 | SLA & Analytics | Due-by backplanning, queue ageing, case lead time, failed nodes, approval delay, audit export. |
| WCP-14 | P2 | Advanced Tooling | Full visual drag-drop editor, sophisticated migration planner, organizational marketplace. |

### Generic node types (first contract)
- `start`, `end`: lifecycle boundaries.
- `human_task`: structured human activity with due date/assigned role.
- `human_approval`: version-specific decision with `policy_ref`; never auto-complete by AI.
- `agent_task`: invoke named agent via external adapter, await validated artifact/result.
- `service_task`: allowlisted connector operation; requires idempotency for side effects.
- `condition`: evaluate **safe declarative expression** (allowlist of operators; no JS/eval).
- `parallel_split` / `parallel_join`: create tokens and join all configured branches; no premature join completion.
- `timer`: delayed/resumable escalation/task, using stored wake-up deadlines.
- `subworkflow`: optional bounded child workflow pinned to its own version.

**Explicitly avoid arbitrary user-supplied code/SQL/HTTP URLs in published workflows.** Use named registered connector tools and policy-controlled parameters. Agent prompts are not workflow code.

## 5. Workflow definition governance and semantics

Lifecycle: `draft → validated → published → deprecated` (published definition is immutable). A publisher releases a **new version**, not overwrites old JSON. Case records `workflow_definition_id`, `version`, SHA-256 definition hash and policy/agent references effective for that run. New versions apply only to new cases unless an **explicitly approved migration** is supported. Draft and preview may use mocks, never true production approvals.

Validation includes:
1. Single accessible start; at least one valid end; all node IDs unique and target IDs exist.
2. Every reachable branch has an end or a permitted loop with declared guard/termination policy.
3. Condition has valid exhaustive/default branching; no JavaScript expressions.
4. All fork/join paths have deterministic join tokens, correct concurrency key, no duplicate completion.
5. `human_approval` has authorized `policy_ref`; external tasks have tool/agent allowlist and declared schemas.
6. Input schema and artifact output contracts are valid; unsafe upgrades fail release.
7. Versions, author, reviewer, published actor and timestamp are retained.

Condition example: `{ "operator":"gt", "field":"estimated_cost", "value":0 }`. Limit supported operators and nested depth. The sample `sim_tender_v1.json` demonstrates actual SIM gates; policy ref names are examples pending organizational sign-off.

## 6. Case and artifact lifecycle

- **Case**: `draft`, `active`, `paused`, `under_review`, `completed`, `cancelled`, `failed` (SIM-specific business states may be captured as business facets, not new engine states).
- **Node execution**: `pending`, `ready`, `running`, `waiting_human`, `waiting_external`, `completed`, `failed`, `cancelled`, `superseded`.
- **Task**: `proposed`, `assigned`, `accepted`, `in_progress`, `submitted`, `accepted_output`, `revision_requested`, `cancelled`.
- **Approval**: `open`, `pending`, `approved`, `rejected`, `returned`, `exception` (exception only by policy).
- **Artifact**: `draft`, `ready_for_review`, `approved`, `stale`, `superseded` and immutable revision ID.

**Change impact example:** update SIM `REQ-014` → artifact dependency graph marks HLD and associated cost/section references stale → impacted tasks and decisions need re-review → G4/G5 cannot pass on old versions. Invalidation is scoped; unrelated approvals remain valid. Emit an audit event with old/new versions and dependency reason.

## 7. Runtime reliability and concurrency contract

1. User creates case in one transaction; pin published definition version and initialize a start token.
2. Within a transaction, persist state transition, audit record and outbox event.
3. Worker claims ready nodes with lease/heartbeat, performs named action and commits result with dedupe/idempotency key. Jobs run at-least-once; **handlers must be idempotent**.
4. Human tasks/approvals wait for authorized actor input; external agent operations wait for signed/authorized callback or polling with bounded retries.
5. Parallel split generates branch-scoped tokens. Join advances **once** only after every required branch token has legitimately completed, even if callbacks are duplicated or out of order.
6. Conditional routing uses validated structured case variables and audit of evaluated condition/outcome.
7. Timeouts and retries: bounded exponential backoff, dead-letter/manual retry, worker crash recovery, explicit human escalation for approvals. Never auto-approve on timeout.
8. Manual interventions record actor, reason and affected versions. Policy changes only alter active cases if explicitly requested and authorized.

Expected failure cases include duplicate agent callback, Redis/queue outage, DB retry, out-of-order branch completions, bad connector response, expired KMS source, rejected approval, cancelled case, and in-flight workflow publication.

## 8. Integrations and ownership

### KMS (external, existing)
Flowcase may **reference** KMS documents and receive access-filtered search results, source IDs, version IDs, citations, classification, approval and expiry. Flowcase must not access KMS PostgreSQL or Elasticsearch indices directly. Implement `KnowledgeProvider` HTTP client using tenant/user context and trace IDs. If an evidence item expires/withdraws, ingest event or reconcile stale links to affected artifacts. KMS remains owner of parsing, Docling, embeddings, Elasticsearch and document publication governance.

### AI Agent Runtime
Use `AgentExecutor` adapter (`StartRun`, `GetRun`, `CancelRun`, `VerifyResult`). Agent executions are background jobs and return structured artifacts. Store case ID, node execution ID, agent ID, skill version, model/tool trace IDs, KMS evidence IDs, user/tenant scope and output checksum. Six named SIM specialist agents are configured in SIM domain pack; **orchestrator is not a seventh specialist**. Human authorized reviewers approve AI outputs.

### Other systems
Host IAM/SSO; notifications (optional); CRM/Opportunity import; pricing resource system and PMS handover when available. Use outbound allowlisted connectors and least-privilege service identities. Unknown contracts remain adapters/stubs until verified.

## 9. Proposed PostgreSQL schema (logical)

- `tenants`, `users`, `memberships`, `roles`, `role_assignments`, `delegations`, `resource_grants`.
- `workflow_definitions` (logical key/tenant), `workflow_versions` (immutable JSON, hash, state, approvals, effective date), `workflow_policies` (versioned refs).
- `cases` (tenant, workflow version ref, business type, due date, status, owner, variables, visibility), `case_members`, `case_history`.
- `node_executions` (case+node+iteration+token, status, attempt, lease, timestamps, idempotency), `execution_tokens` (branch/parent/split/join), `condition_evaluations`.
- `tasks` (assignment, delegation, due date, scope, response schema, outcome), `task_comments`, `task_history`, `review_requests`.
- `approval_requests` (policy, required artifact revisions and context), `approval_decisions` (actor, decision, authority, rationale, immutable hash), `approval_conditions`.
- `artifacts`, `artifact_versions` (immutable, object URI/checksum), `artifact_dependencies`, `artifact_evidence_refs` (KMS IDs/version/anchor), `artifact_reviews`.
- `agent_runs`, `agent_run_callbacks`, `tool_invocations` (redacted), `outbox_events`, `inbox_dedup`, `worker_jobs`, `audit_events`.
- `domain_pack_versions`, `domain_pack_bindings`, `notification_events`, `integration_bindings`.

Index tenant-scoped FKs/uniqueness; apply optimistic concurrency (`version_num`) to cases/definitions; append-only audit write policy, retention controls and sensitive fields redaction. Use PostgreSQL JSONB where sensible, not as substitute for authorization-critical relational references.

### Important invariants
- Published workflow versions **never mutate**.
- One case has exactly one pinned published workflow version per active execution until an explicit migration.
- No unauthorized cross-tenant read/write, including search, agent execution and callbacks.
- A human approval references a specific artifact snapshot/version hash.
- Approval cannot survive a material change to its dependent artifact without re-review.
- A fork/join path cannot execute a downstream node twice because of redelivery.
- A stale or revoked KMS evidence item is not silently accepted as approved proof.

## 10. API contract (proposed REST v1)

| Method/path | Purpose |
|---|---|
| `GET /api/v1/workflows` | List permitted workflows and published versions |
| `POST /api/v1/workflows` | Create draft definition |
| `POST /api/v1/workflows/{key}/versions` | Fork new draft from published version |
| `GET /api/v1/workflows/{key}/versions/{v}` | Fetch immutable or draft version |
| `PATCH /api/v1/workflows/{key}/versions/{v}` | Edit draft with `If-Match` / optimistic version |
| `POST /api/v1/workflows/{key}/versions/{v}/validate` | Validate and return diagnostics |
| `POST /api/v1/workflows/{key}/versions/{v}/publish` | Authorized publication; immutable record |
| `POST /api/v1/cases` | Create case from `workflow_key + published version` |
| `GET /api/v1/cases` | Filter permitted cases |
| `GET /api/v1/cases/{id}` | Case detail, history and active nodes |
| `POST /api/v1/cases/{id}/events` | Authorized structured external/business event |
| `POST /api/v1/cases/{id}/pause` / `/resume` | Operator-managed state |
| `GET /api/v1/cases/{id}/artifacts` | Versioned artifacts and evidence refs |
| `GET /api/v1/tasks` | Assigned tasks, filters and SLA |
| `POST /api/v1/tasks/{id}/complete` | Complete human task with schema-conforming payload |
| `POST /api/v1/tasks/{id}/revision` | Request rework with rationale |
| `GET /api/v1/approvals` | Approval inbox (server filtered by authority) |
| `POST /api/v1/approvals/{id}/decision` | Approve/reject/return/exception (policy checks) |
| `POST /api/v1/agent-callbacks/{provider}` | Verified signed callback, replay-protected |
| `GET /api/v1/audit-events` | Tenant/role-filtered immutable activity |
| `GET /healthz` / `/readyz` | Liveness and readiness |

**Mandatory request rules:** authenticated/tenant-scoped, typed JSON responses, correlation ID, pagination, idempotency keys on create/command endpoints, conflict status on stale version, audit for all mutations. `approve` never trusts role or version hash supplied solely by UI. Publish is distinct from save/validate.

## 11. UX / mini-app scope

**Theme:** navy sidebar, cobalt accent, white content surfaces, restrained glass in header/highlight cards, Apple-inspired bento overview; high legibility. No login screen in mini-app. Need desktop/tablet/mobile breakpoints.

Screens:
1. **Overview:** active cases, pending tasks, approval bottlenecks, recent events, quick start.
2. **Workflow Library:** published/draft definitions, versions, status, domain packs, duplicate/new.
3. **Workflow Designer:** node graph, selection inspector, type/next/config editor, validate, save draft, publish. Initially form-based, no complex drag/drop.
4. **Cases:** case list, filters, detail, active steps, timeline, variables, linked knowledge, blockers.
5. **My Tasks:** assigned human/SME tasks, due dates, submit/return actions.
6. **Approvals:** decisions by authorized people, version preview, rationale, rejects, exceptions.
7. **Integrations:** configured KMS and Agent Runtime status, wrappers, allowed connectors.
8. **Audit:** activity and workflow version history.

UI prototype included in bundle is a **local interactive simulation**, not a real engine or secure approval implementation. Seed data are fictional. React production UI must replace all sample behavior through real APIs.

## 12. UAT scenarios / acceptance tests

**UAT-01 — Reuse across domains:** Given `sim-tender` and `internal-poc-request`, when both execute, engine reuses same node executor/state machine without code branch `if workflow == sim-tender`.

**UAT-02 — Definition publication:** Given published v1 and draft v2, when v2 is published, running case pinned to v1 behaves exactly as before; new cases may choose v2. Saving a draft cannot silently mutate published v1.

**UAT-03 — Parallel barrier:** Given two agent branches, when first finishes, join remains waiting; when second finishes, join advances exactly once despite duplicate completion callbacks.

**UAT-04 — Human gate:** Given AI produced draft, when approval is pending or rejected, a blocking downstream release must remain stopped. The agent cannot use service identity to self-approve.

**UAT-05 — Selective rework:** Given requirement change, when HLD and BoQ depend on changed requirement, only relevant artifacts/reviews become stale; unchanged approved artifact stays valid.

**UAT-06 — KMS ACL:** Given private customer evidence and unauthorized user, KMS evidence may not be retrieved, included in AI context, or shown via Flowcase artifacts.

**UAT-07 — Replay/recovery:** Given worker crash midway through service task, when job is retried, side effect is idempotent, state is recovered, and audit records the attempts.

**UAT-08 — Conflicting policy:** Given a workflow reference to unpublished/unknown approval policy, definition publish is rejected with actionable validation error.

**UAT-09 — Mini-app wrapper:** Given valid trusted host identity, user opens Flowcase without second login; direct requests with forged role/tenant or invalid token fail.

**UAT-10 — SIM PRD sample:** Given RFP source and G0 disposition, Intake produces editable requirements; G1 valid signoff unlocks parallel solution analysis; agent outputs require SME review; G5 cannot release without valid dependent approvals.

**UAT-11 — Structural validation:** An undefined edge, duplicate node ID, unsafe expression, unbalanced join, or unsupported node type is blocked at publication.

**UAT-12 — Timer:** A durable timer survives process restart and invokes escalation once; never grants approval.

## 13. Implementation plan (vertical slices)

| Phase | Scope | Exit gate |
|---|---|---|
| **P0 Foundation** | Go modules, PG migrations, host auth adapter, domain entities, workflow registry and validator; React shell | Tests prove definition immutability, tenant isolation and validation. |
| **P1 Runtime** | Case creation, deterministic node engine, human tasks, approvals, durable jobs/outbox, event log | Two tiny workflows execute with pause/resume, one approval and crash recovery. |
| **P2 Integrations** | KMS adapter, agent bridge, artifacts/dependencies, parallel split/join, condition, error/retry paths | Simulated SIM source→Intake→review→parallel branches; ACL and replay tests pass. |
| **P3 SIM Pack & UI** | G0–G5 domain policies (configurable), designer/inspector, task/approval/case screens, audit | End-to-end SIM UAT with realistic test fixtures and signoff boundaries. |
| **P4 Hardening** | SLA, escalation, observability, security, background ops, publish/rollback workflow versions | Operational runbook, authorization negative tests, monitored pilot. |

Avoid building a full BPMN designer or plugin marketplace before the two domain packs prove the runtime.

## 14. Success metrics

- % cases with pinned definition version and complete audit.
- Median cycle time from case creation to approved closure, stratified by workflow type.
- Task/approval wait time and SLA-overdue rate.
- Failed/retried nodes and duplicate callback prevented count.
- Agent outputs requiring SME correction; invalidated approvals after changes.
- Workflow reuse: independent domain packs deployed without engine code changes.
- Authorization failure, cross-tenant access and KMS evidence leakage tests: **zero accepted violations**.

## 15. Inputs needed before production integration

1. Host wrapper auth contract (iframe vs reverse proxy vs SDK, token/session mechanism).
2. KMS API endpoints, permission propagation and source event semantics.
3. Agent runtime API, skills registry contract, allowed tools and callbacks.
4. Organizational policy for G0–G5, DoA/SME delegation, exceptions and financial access.
5. PostgreSQL version, queue strategy, deployment requirements, secrets management and telemetry.
6. Two realistic approved sample cases and anonymization policy.

## 16. Deliverables checklist

- [ ] Go backend with migrations, typed API, reliable worker and test suites.
- [ ] React TypeScript frontend matching supplied `Flowcase_Workflow_Platform.html` reference.
- [ ] Versioned workflow definition registry + validator + sample packs.
- [ ] Case/task/approval/artifact/audit runtime with correct auth.
- [ ] KMS and AgentExecutor adapters, with dev stubs and contract tests.
- [ ] Local Docker Compose for Go, PostgreSQL and optional app dependencies **without deploying duplicate KMS/Docling/Elasticsearch**.
- [ ] OpenAPI 3.1, README, operational runbooks, security and integration notes.

---

**Reference documents:** SIM B2B Solution Workspace PRD v3.1 (30 October 2026), sections 04–07, 10–16, 19 and 21. Supplied user KMS architecture decision (Go, PostgreSQL, Elasticsearch inside KMS, Docling self-hosted). All generic workflow product decisions above are **engineering extrapolation** clearly separated from SIM-specific source requirements.
