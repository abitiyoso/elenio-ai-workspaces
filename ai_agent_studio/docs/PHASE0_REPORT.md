# Agent Studio Phase 0 Report

Date: 2026-10-30. Scope: `CODEX_MASTER_PROMPT.md` → Phase 0 only. No production runtime or UI was implemented.

## Findings

| Area | Evidence | Result |
|---|---|---|
| Agent Studio baseline | Package contains PRD, standalone illustrative HTML, contracts and samples; no Go module or React application exists in this folder. | Phase 1+ implementation has not started. |
| Eino | No Eino module is pinned in this package or adjacent Go modules. Official release page lists stable `v0.9.22`; v0.10 entries are alpha. Official docs show the `NewChatModelAgent` → `NewRunner` → `Query`/`Run` flow. | Proposed stable pin is `github.com/cloudwego/eino v0.9.22`, Go 1.24.0 baseline. Compile verification is blocked because the Go module proxy could not resolve (`proxy.golang.org`: DNS/network unavailable). No pin was added without a successful resolution and compile check. |
| KMS | `kms_go_package/backend/openapi.yaml` and `internal/transport/server.go` expose authenticated `POST /api/v1/search`; search returns reauthorized chunks and citations. | A real search API exists, contrary to `flowcase_platform/docs/CONTRACTS.md` and the Flowcase Phase 0 report's statement that KMS has no retrieval API. No `fetch_evidence` endpoint was found. The Agent Studio evidence schema does not match current KMS fields/statuses. |
| Flowcase | `flowcase_platform/api/openapi.yaml` currently exposes foundation routes only. `flowcase_platform/docs/CONTRACTS.md` marks the runtime invocation/callback wire contract pending; the Go `AgentExecutor` is a port with a dev stub. | Agent invocation/completion JSON Schemas are proposal-only. Signing, identity delegation, artifact references, dedupe, retry, deadline, and cancellation details need owner agreement. |
| Identity | Flowcase documents a host JWT proposal and server-side verification; exact host integration claims remain pending. KMS search uses a verified identity and performs collection/case ACL checks. | Trust only verified server-side identity and explicit delegation. Do not accept user/tenant/role values from agent input or request headers. Host issuer/audience/JWKS/session exchange remain unresolved. |
| SIM bootstrap | `samples/sim_six_agents.json` defines six distinct IDs and forbidden actions, but SME roles and pack status are illustrative proposals. | No organizational SME ownership or approval authority is confirmed. |

## Contract artifacts

- Existing `flowcase_agent_invocation.schema.json`, `agent_completion_event.schema.json`, and `kms_evidence_response.schema.json` remain marked as proposals and must not be treated as confirmed external APIs.
- Added KMS search request/response schemas to reflect the current OpenAPI/Go API. These describe the HTTP contract shape only; they do not grant runtime credentials or confirm how the runtime receives a delegated end-user identity.
- The schemas and ADR are under `contracts/` and `docs/` respectively.

## Validation and gaps

- All five JSON Schema files parsed with `jq` and validated against representative valid fixtures using Ajv 6.15.0 available in the adjacent Flowcase web dependencies. The schemas declare Draft 2020-12, while this Ajv version supports Draft-07; validation removed the `$schema` declaration in memory and exercised the shared subset of keywords. Draft 2020-12 meta-schema validation was not performed.
- Eino API signatures were checked against official documentation, but dependency resolution and a compile check remain blocked by network/DNS failure.
- No Go, frontend, integration, or end-to-end tests were run. This phase contains no executable implementation to test.

## Blockers before Phase 1/2 integration

1. Confirm host token exchange, issuer/audience/JWKS, tenant/workspace membership, user delegation, and Flowcase service identity.
2. Confirm Flowcase's invocation/status/cancel API, idempotency scope, artifact proposal format, callback signing/key distribution, dedupe, and retry semantics.
3. Confirm KMS service auth/delegation, collection and case scope mapping, source/version/anchor mapping, whether `fetch_evidence` is required, and status/freshness semantics.
4. Resolve and compile Eino `v0.9.22` in a Go module; verify compatibility with the chosen provider adapter version and tool/structured-output capabilities.
5. Confirm approved model providers, secret vault, endpoint egress policy, SME ownership, separation-of-duties policy, retention, and production deployment constraints.

## Stop gate

Phase 0 artifacts are recorded. Stop here as required by the master prompt; do not begin registry, UI, runtime, or integration implementation until the blockers and contract owners are resolved.

