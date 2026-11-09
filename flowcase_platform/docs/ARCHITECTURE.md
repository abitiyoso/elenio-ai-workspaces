# Flowcase — Architecture Decisions (Phase 0)

Status: accepted for implementation; items marked **PENDING** need an external decision.
Traceability: PRD IDs refer to `WORKFLOW_CASE_PLATFORM_TECHNICAL_PRD.md` (WCP-xx, UAT-xx).

## Repository inspection (Phase 0 start)

- This directory contained only the design package (PRD, HTML prototype, JSON Schema, three sample definitions). No existing code, no git repository.
- Sibling `../kms_go_package` is the existing KMS (Go 1.24, pgx v5, PostgreSQL 16, React 18 + Vite 7). It exposes only `/healthz`, `/readyz` and `/api/v1/session` today — **no retrieval/evidence API exists yet**. Flowcase follows its conventions (stdlib `net/http` routing, `slog` JSON logs, pgx, `AuthAdapter` naming) so the two mini-apps stay consistent, but does not import its code or touch its database.
- Toolchain found locally: Go 1.27.1, Node 24, Docker. `go.mod` declares `go 1.24` (the version the sibling repo supports) rather than pinning a toolchain.

## ADR-001 Modular monolith + separate worker process

One Go module, two binaries: `cmd/api` (HTTP) and `cmd/worker` (durable background execution, Phase 2+), plus `cmd/migrate`. Modules are Go packages behind interfaces, not network services. Splitting deployment is deferred until load or isolation justifies it.

```text
cmd/{api,worker,migrate}
internal/platform/{config,postgres,auth,observability}
internal/api              HTTP transport, middleware, error envelope
internal/integrations/{kms,agent}   KnowledgeProvider / AgentExecutor ports + dev stubs
internal/worker           worker loop scaffold (no execution yet)
migrations/               embedded, ordered SQL migrations (up + down)
api/openapi.yaml          OpenAPI 3.1 contract
web/                      React + TypeScript + Vite mini-app
```

Phase 1+ adds `internal/workflows/{domain,registry,validator,compiler,engine,repository}`, `internal/cases`, `internal/tasks`, `internal/approvals`, `internal/artifacts`, `internal/packs/{sim,poc}`.

## ADR-002 PostgreSQL is the only system of record

Definitions, versions, cases, execution tokens, node executions, audit, outbox and inbox dedup live in PostgreSQL. No Redis/queue dependency: the outbox + `FOR UPDATE SKIP LOCKED` leasing in `node_executions`/`outbox_events` provides at-least-once delivery (WCP-09). No pgvector, Elasticsearch or Docling here — those stay behind KMS.

Database-enforced invariants added in Phase 0 (defence in depth beneath the service layer):

- `workflow_versions`: content, hash, key, version and tenant are immutable once `status` ∈ {published, deprecated}; only `published → deprecated` is allowed; published rows cannot be deleted (PRD §9 invariant 1, UAT-02).
- `audit_events`: append-only — `UPDATE`/`DELETE` raise an error.
- Tenant isolation through composite foreign keys `(tenant_id, id)`: a case can only reference a workflow version, owner and tokens inside its own tenant.
- `cases` reference a version by id **and** store `workflow_key`, `workflow_version`, `definition_hash` (pinning, WCP-03).
- `node_executions` unique on `(case_id, node_id, iteration, branch_token)` and on `idempotency_key` (runtime contract).
- Optimistic concurrency columns `version_num` on definitions, versions and cases.

## ADR-003 Authentication via host wrapper, authorization in Flowcase (WCP-10, UAT-09)

- No login page. The host (parent AI Workspace) authenticates the user and gives the mini-app a **short-lived, audience-restricted JWT** (token exchange). The browser keeps it in memory only and sends `Authorization: Bearer`. Tokens are never accepted from query strings or cookies (no CSRF surface in this mode).
- `AuthAdapter = TokenVerifier + Directory`:
  - `TokenVerifier` (`JWTVerifier`): verifies signature against a JWKS (RS256/ES256 only, `none`/HS* rejected), `iss`, `aud`, `exp`, `nbf`, `iat` with bounded clock skew, maximum token lifetime, required `sub` and tenant claim.
  - `Directory` (`PostgresDirectory`): the subject must be an **active member** of the claimed tenant; roles come from Flowcase `role_assignments`, never from the token or headers.
- Dev mock identity (`AUTH_MODE=dev_mock`) requires `APP_ENV=development` **and** `DEV_AUTH_ACKNOWLEDGE=i-understand-this-is-not-secure`. Any other environment refuses to start. It returns one fixed identity, ignores `X-User-ID`/`X-Tenant-ID`/role headers, and the session response carries `auth_mode=dev_mock` so the UI shows a permanent warning banner.
- Unknown/forged/expired identity → `401`; verifier misconfigured → `503` (fail closed).
- **PENDING**: concrete host mechanism (iframe `postMessage` vs reverse proxy vs SDK), issuer, audience, JWKS URL, claim names. Config keeps claim names configurable; see `docs/CONTRACTS.md`.

## ADR-004 External KMS behind `KnowledgeProvider` (WCP-07/08, UAT-06)

Flowcase stores evidence references (source id, version id, anchor, classification, expiry) and calls KMS with the end user's tenant/user context and trace id. KMS enforces ACL; Flowcase additionally refuses to persist results for a tenant other than the caller's. Retrieved text is **untrusted evidence**, never instructions. Modes: `disabled` (default), `stub` (development only, visible warning). `http` is not offered until the KMS retrieval API exists.

## ADR-005 External AI runtime behind `AgentExecutor` (WCP-08)

`StartRun`, `GetRun`, `CancelRun`, `VerifyResult`. Agent identities are service principals with no approval capability; the six SIM specialists are pack-level registrations (Phase 4), the orchestrator is not a specialist. Callbacks must be signed, audience-bound, timestamped and deduplicated through `inbox_dedup`. Modes: `disabled` (default), `stub` (development only). No production adapter until the runtime contract is confirmed.

## ADR-006 Observability and data hygiene

- `slog` JSON logs; every request gets an `X-Request-ID`/trace id (accepted from the host if well-formed, generated otherwise) echoed in responses and error bodies.
- Logs never contain `Authorization`, cookies or token values; a redacting handler masks attributes with sensitive key names. Request logs record method, route pattern, status, duration, trace id — not bodies.
- Error envelope: `{"error":{"code","message","trace_id","details":[{"code","message","path"}]}}` (structured validation failures from Phase 1 reuse `details`).

## ADR-007 Frontend

React 18 + TypeScript + Vite + React Router. Ports the supplied navy/cobalt identity (tokens from the HTML prototype), glass only on the top bar, solid white tables/editors, keyboard-focus rings, dismissible mobile drawer (Esc, overlay click, focus return). Data comes only from the API (`/api` proxied by Vite in dev). Pages without backend support show explicit "not available yet" empty states — **no seeded demo data or browser-side approvals**.

## ADR-008 Workflow safety (applies from Phase 1)

Definitions are declarative JSON validated by JSON Schema **and** Go semantic checks. No JS/eval, no user SQL, no free-form URLs; conditions use an allowlisted operator set with depth limits; agents/connectors/policies must be registered references. `subworkflow` is deferred (documented) unless needed by a pack.

## ADR-009 Registry semantics (Phase 1)

- Content hash = SHA-256 of canonical JSON (sorted keys, numbers preserved) **without** `status`; `key` and
  `version` are pinned by the server, so the stored document and the hash cannot disagree about identity.
- Lifecycle `draft → validated → published → deprecated`. Editing a validated draft returns it to draft;
  publication always re-validates the stored content on the server. One open draft per workflow.
- Drafts may hold semantic errors (so designers can save work in progress) but never structural errors.
- Publish and deprecate require `workflow_publisher` **and** a human principal; service principals never qualify.
- Create commands require `Idempotency-Key`, claimed in the same transaction as the command; edits, publish and
  deprecate require `If-Match` on `version_num`.
- Domain packs are data (catalog registrations + templates). Validator and registry contain no pack-specific code.

## ADR-010 Case runtime (Phase 2)

- `node_executions` is the durable job queue, written in the same transaction as the state change that
  schedules it. Workers claim with `FOR UPDATE SKIP LOCKED` and a lease; an expired lease is reclaimed. Every
  handler runs in one transaction that locks the case row first (lock order: case → execution → task/approval).
- Successors are inserted with an idempotency key derived from the predecessor execution, so redelivery or
  duplicate completion cannot schedule a step twice. Tasks and approvals are unique per execution.
- Approvals are human-only (service check + DB trigger), require `domain_approver` plus an explicit policy
  delegation, apply default SoD, and bind to a SHA-256 of the case state; decisions are append-only and hashed.
- Return and revision share one rewind: supersede the waiting execution, withdraw its work item, schedule a new
  iteration of the target carrying the reason.
- The worker wakes on `NOTIFY flowcase_work`; the ticker remains the correctness guarantee.

## ADR-011 Parallelism, external work and artifacts (Phase 3)

- Branch tokens are keyed by the split execution; a join fires once per split execution, so redelivery,
  out-of-order arrivals and rework re-runs cannot double-schedule a successor.
- Agent, service and timer steps wait in `waiting_external`; the same claim query resumes them (poll, wake-up)
  and callbacks settle runs under the case lock. Agent output is only ever artifact content.
- Artifact dependencies come from the pinned graph; approvals bind to covered versions. Change propagation is
  downstream-only and re-opens exactly the reviews whose covered versions changed.
