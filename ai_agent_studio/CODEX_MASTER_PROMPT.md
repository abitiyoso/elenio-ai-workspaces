# MASTER PROMPT — Build Agent Studio with Go + Eino ADK

> Paste this into Codex **after** attaching `AI_AGENT_STUDIO_TECHNICAL_PRD.md`, `Agent_Studio_Eino_Prototype.html`, `contracts/*`, `samples/*`, and the existing Flowcase/KMS API contracts. Use current Eino docs linked in README to verify exact API signatures against your pinned dependency version.

You are the lead Go platform architect and senior React engineer responsible for building **Agent Studio** as an enterprise-grade mini-app and its reusable **Eino ADK-based Agent Runtime**.

## Primary objectives

Implement the attached Technical PRD faithfully. The service is reusable for SIM B2B and other mini-apps. The SIM case/workflow is NOT hardcoded into the runtime. Six distinct SIM agent identities must ship as a configuration/bootstrap domain pack and have SME-owned skills and tool policies. Flowcase owns business workflow and approvals; KMS owns document conversion and Elasticsearch retrieval. Agent Studio owns agent authoring, testing, versioned release, safe execution and traces.

## Mandatory technology decisions

- **Go** for API and worker; **Eino ADK** for `ChatModelAgent`/`Runner`/tools. Use Eino Graph only for bounded AI sub-pipelines when required.
- **PostgreSQL** for durable registry, versions, run events, audit, evaluations, releases, outbox/inbox.
- React + TypeScript + Vite for frontend; match provided Navy/Cobalt standalone HTML reference. No standalone login screen.
- KMS integration by API (`kms.search`, `kms.fetch_evidence`), not direct KMS Postgres/Elasticsearch access, not duplicate Docling.
- Flowcase integration by authenticated API/event contract, not direct DB writes; never set Flowcase approval state from the agent runtime.
- Model & Provider Management is a P0 module: provider registry, secure write-only credential references, model deployment registry, tenant approval/allowlist, capability probes, cost/limits, and per-agent immutable model binding. Use ChatModel adapters for Eino; never hardcode a vendor.
- Development mock providers permitted only with explicit DEV mode and clearly labelled UI. Production must fail closed without required credentials/services.

## Non-negotiable guardrails

1. Never implement autonomous proposal release, price approval, bid decision, or SME impersonation.
2. Immutable published versions; every run pins `agent_version`, `skill_version`, `tool_version`, `model_policy`, `source refs` and correlation ID.
3. Reject calls without server-verified identity, tenant/workspace membership, action and tool authorization.
4. Tool calls enforce their own ACL and input schema (never trust agent text). No arbitrary tool HTTP URL, SQL, shell execution or secret exposure.
5. No prompt injection from RFP/KMS sources may override tool scope, system instructions or authority.
6. All agent outputs have JSON Schema and material claims require traceable evidence; uncertain/missing sources are unresolved, not invented.
7. Async requests use outbox+worker and idempotency key, replay-safe result callbacks; cancellations/deadlines honored.
8. Skills can be changed in drafts, sandboxed, evaluated, independently reviewed by domain SME, then separately released by AI Ops; deploy blockers are explicit.
9. Never silently upgrade an in-flight Flowcase case or rewrite an approved artifact with a new skill.
10. Sensitive tool/model inputs must be redacted in the UI/audit, encrypted as needed and permission-scoped.

## Repository structure recommendation

```
apps/agent-studio-web/      # React/Vite mini-app
services/agent-studio-api/  # Go API / RBAC / registry / skills / eval / release
services/agent-worker/      # Go + Eino ADK / tools / model adapters
internal/domain/            # pure business models
internal/ports/             # interfaces: Model, KMS, Flowcase, Auth, Tool
internal/adapters/          # Postgres, HTTP, ADK, queue
migrations/                 # ordered reversible SQL migrations
contracts/                  # JSON Schema, OpenAPI, sample callbacks
samples/sim/                # six SIM agent manifest fixtures
scripts/                    # local dev & verification
```

Alternative layout is fine if clear bounded modules and dependency direction are preserved. Do not introduce seven microservices for six agents.

## Implementation sequence — DO NOT jump phases

### Phase 0 — Contract and version verification
- Read all attached files and existing repository thoroughly.
- Inspect current KMS and Flowcase public contracts. If missing, build accurate mock adapters (do not invent live integration success).
- Pin Go/Eino dependency versions based on current official Go module and docs; verify `adk.ChatModelAgent`/`Runner` options and tool signatures by compile/test, not from memory.
- Define and validate JSON Schema contracts in `contracts/`.
- Decide identity trust boundary with host wrapper. Dev mock only for local environment.
- Output ADR for Eino-vs-Flowcase ownership, auth, model provider, tool policy and version pins.
- **Stop after Phase 0**: show blockers and contract mismatches.

### Phase 1 — Registry, data and secure API
- Create Go modules, migrations, PostgreSQL repositories, idempotent bootstrap of six distinct SIM agents.
- Implement `llm_providers`, `llm_model_deployments`, `agent_model_bindings`, credential secret reference and model status/approval tables; provide model registry API and UI. Secrets must be write-only through a backend-managed vault integration, never in browser storage.
- Build `Models & Providers` page from supplied HTML, including provider registration, model deployment forms, status, capability/connection tests (backend), admin policies and tenant scope. Explicitly distinguish mock UI from actual connection health.
- Implement agent/skill/tool CRUD for drafts, immutable version registry, permission checks, audit and pagination.
- Implement `AuthAdapter` verification interface; dev-only mock behind enforced environment guard.
- Connect React overview, registry, agent detail and tool view to API (no fake production states).
- Test cross-tenant leak attempts, immutable published version, unauthorized editing and concurrent release races.
- **Stop and report tests.**

### Phase 2 — Eino runtime + first vertical integration
- Implement `ChatModelProvider`, `EinoAgentFactory`, `ToolResolver`, `RunService`, `EventSink` and bounded `Runner` execution.
- Resolve agent-version model deployment under tenant/environment approval before constructing Eino ChatModel. Support at least one validated provider adapter end-to-end, with a pluggable provider interface for others. Verify tool calling/JSON compatibility and securely resolve credential from vault. Unapproved/revoked/misconfigured model => explicit fail-closed error.
- Implement input schema checks, output schema validation, time/token/tool limits, redacted trace, cancel/retry, idempotent job claim.
- Implement KMS tool adapter: read-only authorized retrieval with source ID/page/version/freshness.
- Implement Flowcase invocation + callback with verify/dedupe/outbox; only draft artifact results are emitted.
- End-to-end smoke: `sim.intake` reads case RFP evidence fixture and returns structured requirements for human review.
- Test tool denial and prompt injection; no direct KMS DB access.
- **Stop and report tests.**

### Phase 3 — Skills, sandbox and governance
- Build versioned Skills Studio editor, agent builder, tool-scoping form, sandbox playground with real dry-run adapter where possible.
- Implement evaluation dataset/schema, deterministic validators, baseline diff, critical-regression block and human reviewer rubric.
- Changing a draft agent model binding or model capability policy invalidates candidate evaluation and prior signoff.
- Implement independent semantic signoff and AI Ops approval, environment-scoped publish/rollback with pinned versions.
- Show accurate published/draft statuses from backend, not browser state.
- Test failed eval, missing signoff, self-approval prohibition and canary rollback.
- **Stop and report tests.**

### Phase 4 — Six SIM agents and hardening
- Supply realistic skills and output schemas for six SIM agents and SME ownership mapping.
- Add Flowcase workflow pack links and KMS collection requirements; do not claim external connections unless verified.
- Implement run history, evidence lineage, metrics, rate limits, trace retention, cancel/resume and ops diagnostics.
- Cover SIM PRD UAT: changed HLD invalidates dependent approval through Flowcase, unsupported cost/accuracy claims rejected, skill correction staged, no automatic release.
- Run integration tests, security review, concurrent load tests and deployment runbook.

## Output requirements per phase

1. Concise file/directory tree changed.
2. Schema & endpoint implementation notes, example curl requests.
3. `go test ./...`, frontend build/typecheck and integration test results **actually run** (include failures honestly).
4. Which adapters are real vs mocked, what permissions are enforced.
5. Security and compatibility issues still unresolved.
6. Explicit next-phase plan, **without starting it** until instructed.

## Starter task for this run

**Implement Phase 0 only.** Read the attachments, inspect repo and current Eino API version, propose ADR/contracts and report any incompatible assumptions. Do not generate a fake completed implementation. Do not build production login UI.
