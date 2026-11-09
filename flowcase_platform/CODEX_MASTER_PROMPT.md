# Master Prompt for Codex — Flowcase Workflow & Case Platform

> **Usage:** Attach this file, `WORKFLOW_CASE_PLATFORM_TECHNICAL_PRD.md`, `Flowcase_Workflow_Platform.html`, `contracts/WORKFLOW_DEFINITION_SCHEMA.json`, and the three sample workflow files to the coding agent. Execute the **phase prompt at the bottom**, not all phases at once. Name and visual identity are changeable.

## Master system-level implementation brief

You are a principal Go platform engineer, staff frontend engineer, and workflow runtime architect. Implement **Flowcase**, a production-oriented, reusable Workflow & Case Management mini-app platform. Treat the attached Technical PRD as the accepted technical scope (pending explicitly listed decisions) and the HTML file as a **visual and interaction reference**, not production code.

### Non-negotiable architecture

1. **Backend:** Go 1.x using idiomatic Go; modular monolith with clear module boundaries and separate durable worker process. Target the repo's supported Go version rather than inventing an exact toolchain pin. PostgreSQL is source of truth.
2. **Frontend:** React + TypeScript + Vite; preserve the navy/cobalt professional, high-contrast bento design and interaction flows from the HTML. Use accessible, responsive components. Do **not** add an extra login page.
3. **Auth:** `AuthAdapter` integrated with parent AI Workspace/wrapper. Authenticate via trusted backend validation or secure token exchange (short-lived, aud-restricted token). Implement strict authorization for tenant, workspace, case, artifact, task, approval and domain. Development mock identity must be opt-in and fail closed in production.
4. **Workflows:** Generic configuration-driven state machine. Each published definition version is immutable. Cases are pinned to an exact workflow version. Implement `start/end`, human tasks, approvals, agent/service tasks, conditions, parallel split/join, and timer nodes. `subworkflow` can be deferred if explicitly documented.
5. **Safe runtime:** no raw JS/code eval or user-specified SQL/HTTP endpoints in workflow definitions. Condition rules use validated, limited declarative operators. Only allowlisted tools/connectors may be invoked.
6. **Reliability:** PostgreSQL transactions, audit + transactional outbox, durable worker with leasing/retries/dedupe, idempotent task handling, optimistic concurrency, duplicate callback protection, crash recovery and branch-aware joins.
7. **KMS boundary:** KMS is already built and remains an external service. Use `KnowledgeProvider` API with permissions and citations. **Do not rebuild KMS, Docling, Elasticsearch, embeddings or pgvector** here. KMS owns those internals; Flowcase only stores evidence references/versions and consumes KMS APIs/events.
8. **AI boundary:** AI is external/shared runtime behind an `AgentExecutor` adapter. SIM's six specialist agents are registrations/domain bindings, not six new microservices. Orchestrator is a coordinator, not a seventh specialist. No AI approval, final price authorization or customer submission.
9. **Governance:** human signoffs, exceptions and SoD policies are server-enforced and version-specific. Never infer company DoA roles from sample data.
10. **Testability:** deterministic unit tests, integration tests (Postgres, worker, event handling), auth negative tests, schema validation, REST contracts, API docs and traceability to PRD IDs. Never claim tests/integrations passed without executing them.

### Required architecture modules

`auth`, `workflow_registry`, `workflow_validator`, `engine`, `cases`, `tasks`, `approvals`, `artifacts`, `agent_bridge`, `knowledge_bridge`, `audit`, `worker`, `api`, `notifications` (last optional), `domain_packs` (SIM + PoC samples). Expose stable interfaces; do **not** create a network service for each Go package.

### Suggested directory layout (adapt to existing repository)

```text
cmd/api/main.go
cmd/worker/main.go
internal/platform/{config,postgres,auth,events,observability}
internal/workflows/{domain,registry,validator,compiler,engine,repository}
internal/cases/{domain,service,repository}
internal/tasks/{domain,service,repository}
internal/approvals/{domain,policies,service,repository}
internal/artifacts/{domain,versions,dependencies}
internal/integrations/{kms,agent}
internal/packs/{sim,poc}
api/openapi.yaml
migrations/
web/src/{app,pages,components,features,api,styles}
examples/workflows/
internal/*/*_test.go
```

### Runtime behavior contract

For each case create: resolve published workflow version, store version/hash, validate authorized caller, persist case and start token transactionally. Enqueue next nodes from an outbox. Each node execution is keyed by `case_id + node_id + iteration + branch_token` with idempotency key and state transitions. Human tasks wait for valid human actions; approval nodes require policy evaluation, reviewer authority, artifact version and rationale. Agent tasks invoke an allowlisted agent with explicit input schema, KMS source IDs and skill version, then validate a signed/reconciled result. Side effects must be de-duped. For `parallel_join`, wait for expected branch token set and schedule successor once only. Condition evaluation is deterministic and logged.

Store artifact immutable versions, dependency graph, proof references, approvals and audit. A changed requirement or revoked evidence invalidates impacted artifacts/approvals, not everything globally. Replayed events and concurrent decisions must not produce duplicate transitions or bypass gates.

### Definition validation contract

Validate JSON schema and semantic constraints: graph targets exist, unique IDs, exactly one start, reachable valid ends, allowed node types, type-specific fields, split/join pairing, no unintended cycles, exhaustive/default condition branches, registered agent/provider/policy refs, valid input/output schema. The bundled JSON Schema is **conceptual** and requires cross-node semantic validation in Go beyond JSON Schema alone. Keep API versioned to evolve format safely.

### UI contract

Build these screens from HTML reference with real API integration and proper loading/empty/error/optimistic conflict states:
- Overview: bento KPIs, pending approvals, tasks, active case list, recent activity.
- Workflow Library: drafts/published versions, create from template, duplicate, version history.
- Workflow Designer: readable DAG, select node inspector, edit draft config, validate, save, publish; **no need to build a complex drag-drop canvas initially**.
- Cases: list + detail, active nodes, approvals, artifact dependencies, history and status.
- Tasks: assigned work, submit/rework, comments and evidence.
- Approvals: decision card, preview artifact revision and policy, rationale and outcome.
- Integrations: KMS and AI runtime adapter health/config (safe, no secrets exposed).
- Audit: actor/action/subject/version/time.

Do not replace the supplied professional navy/cobalt theme with generic purple-gradient AI dashboard elements. Use restrained glass surfaces only for top bar or highlight panels; data tables and editors should be solid white and highly legible. Provide keyboard focus and mobile navigation.

### API minimum

Implement and document:
- `GET/POST /api/v1/workflows`, versions `GET/POST/PATCH`, `POST validate`, `POST publish`.
- `GET/POST /api/v1/cases`, detail, pause/resume, events, artifacts.
- `GET /api/v1/tasks`, complete, request revision.
- `GET /api/v1/approvals`, decision.
- `GET /api/v1/audit-events`.
- verified agent callbacks and `GET /healthz`, `GET /readyz`.

Use structured validation failures (`code`, `message`, `field/path`, `trace_id`), `If-Match` or version checks, server-generated IDs, correct HTTP status codes, idempotency keys, pagination, audit and RBAC on all routes. Keep cross-app API contracts explicit.

### Required demo workflows

1. `samples/sim_tender_v1.json`: SIM PRD G0–G5, parallel Solution Matching+Design, parallel Standard Project+Cost Challenge, Intake and Proposal agents; all required human gates. Policy bindings are configurable and provisional pending business signoff.
2. `samples/poc_request_v1.json`: unrelated PoC request using human task, condition and budget gate. Ensure **both work with the same runtime** to prove reuse.

### Security / AI guardrails

- Do not trust KMS retrieved text as instructions to system or agent. Treat documents as untrusted evidence.
- Agent input gets minimal role/case scoped context. No unauthorized pricing, talent CV, partner confidentiality, or cross-tenant disclosures.
- Tokens must not be logged; secrets in secret manager / local env for dev, never committed.
- Protect callback endpoints from replay with signature/audience, timestamps or allowed service identity and DB dedup.
- Make all approvals human-only; AI tool/service credentials never map to an approver.
- Sensitive fields redacted in logs and exports.
- Include audit of publish, case transition, approval and source invalidation.

### Execution style

First inspect repository, versions, supplied artifacts, and confirmed host/KMS/agent contracts. Write a short implementation plan and list unresolved external dependencies. Then deliver **one vertical slice at a time**, including code, tests, migration changes, OpenAPI updates, and a brief verification report. When external APIs are missing, create explicit adapter interfaces and a development stub with a visible warning. Never silently invent a working production integration. End each phase and wait for the next user instruction; do not autonomously advance all phases.

### Definition of done

- Integration test proving workflow v1 case remains pinned after v2 publish.
- Integration test with parallel split/join and duplicate callback -> exactly one successor activation.
- Unauthorized caller denied task completion and human approval; expired token rejected.
- Change invalidation blocks G5 and reopens only affected reviews.
- Two different domain packs execute without adding workflow-key hardcoded engine branches.
- Frontend has no mock approval in production; demo-only behavior is behind dev mode.
- App build, Go tests, frontend typecheck, lint, migrations and integration tests actually run.

---

## Phase prompts: run separately

### Phase 0 — Architecture and repository initialization

Implement **only Phase 0**: inspect repo, write architecture decisions and interface contract notes, scaffold Go API+worker and React+TS+Vite shell, validate environment configuration, add PostgreSQL migrations for workflow registry/case core, create `AuthAdapter` with a dev-only fallback, define KMS/Agent interfaces, wire health/readiness and structured logging, and set up test/CI commands. Port the supplied HTML visual identity and sidebar navigation. **Do not implement business workflow execution yet.** Run tests and report unresolved identity/wrapper/KMS contracts.

### Phase 1 — Workflow registry, compiler and validator

Implement `WorkflowDefinition`, immutable `WorkflowVersion`, schema+semantic validator, draft editing and publishing with authorization/audit. Import both sample packs and provide list/detail/duplicate/version actions. Build Workflow Library and read-only/editable node inspector (draft only). Unit test missing edges, invalid conditions, unmatched joins, publication immutability and version pinning. Stop when test suite passes.

### Phase 2 — Durable case runtime and task/approval

Implement case creation, pinned execution, node engine (start/end/human task/approval/condition), persistent tasks, decision authorization, transactional outbox/worker and audit. Wire Cases, Tasks, Approvals UI to API. Test retry, crash recovery, idempotency, SoD and premature gate release. Do not begin external AI/KMS integration until these foundations work.

### Phase 3 — Parallelism, artifacts and integration bridges

Implement parallel split/join tokens; durable `agent_task`, `service_task` and `timer`; `KnowledgeProvider` and `AgentExecutor` adapters; artifacts/versions/dependencies and targeted invalidation. Build contract tests with KMS/AI stubs; test duplicate callback, out-of-order agent result, permission propagation, stale evidence, safe prompt/tool boundaries and task/approval re-review. If real integration endpoints are known, add live E2E test with explicit credentials via environment, never hardcode.

### Phase 4 — SIM pack, end-to-end UI, hardening

Configure SIM `sim-tender` domain pack, G0–G5 policy refs and six agent mappings using external config. Complete workflow designer, case timeline, task inbox, approvals, integrations and audit UI. Run SIM v3.1 UAT scenarios and the generic PoC workflow. Add OpenAPI, metrics, permission negative tests, backup/recovery guidance, operations runbook and deployment docs. No unverified claims of policy approval.

---

## Starter message for Codex

> Read `WORKFLOW_CASE_PLATFORM_TECHNICAL_PRD.md`, `CODEX_MASTER_PROMPT.md`, `Flowcase_Workflow_Platform.html`, and the files under `samples/` and `contracts/`. We are building a **generic reusable workflow/case engine**, not a hardcoded SIM tender app. **Start Phase 0 only.** Respect Go + PostgreSQL, external KMS/Agent adapters, host wrapper auth (no login page), immutable workflow versions, human approvals and Navy/Cobalt UI. Inspect the repo, implement code and tests, run them, and report changes plus blockers. Do not begin Phase 1 without my instruction.
