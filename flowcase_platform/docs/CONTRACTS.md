# Interface Contract Notes and Unresolved External Dependencies

Everything below the "Assumed" column is a Flowcase-side proposal. Nothing here is a confirmed host, KMS or AI-runtime contract.

## 1. Host wrapper identity (AuthAdapter) — PENDING

| Item | Assumed by Flowcase today | Needs confirmation from host team |
|---|---|---|
| Delivery | Host performs token exchange and passes a JWT to the mini-app; browser holds it in memory and sends `Authorization: Bearer` | iframe `postMessage` vs reverse proxy vs SDK |
| Token | JWS, `RS256` or `ES256`, `kid` in header | Algorithm, key rotation cadence |
| Keys | JWKS at `AUTH_JWKS_URL` (HTTPS outside development), cached `AUTH_JWKS_CACHE_TTL` | URL, caching headers |
| Claims | `iss`=`AUTH_ISSUER`, `aud` contains `AUTH_AUDIENCE`, `exp`, `iat`, optional `nbf`, `sub`, tenant claim `AUTH_TENANT_CLAIM` (default `tenant_id`) | Exact claim names, whether tenant is a claim or a path |
| Lifetime | Rejected if `exp - iat` > `AUTH_MAX_TOKEN_TTL` (default 15m); skew `AUTH_CLOCK_SKEW` (default 60s) | Real TTL, refresh flow (host re-posts a new token) |
| Session binding | Not enforced yet | Whether a nonce / session id (`sid`) must be bound |
| Roles | **Not read from token.** Flowcase `role_assignments` per tenant | Whether host groups should be synced into Flowcase roles, and by whom |
| iframe | Frame embedding allowlist `FRAME_ANCESTORS`; `postMessage` origin allowlist `VITE_HOST_ORIGINS` | Host origins |

Frontend side (`web/src/api/hostBridge.ts`): accepts `{type:"flowcase:token", token}` only from allowlisted origins, keeps it in memory, never in `localStorage`/URL. Message shape is a proposal.

## 2. KMS (KnowledgeProvider) — PENDING

The existing KMS (`../kms_go_package`) currently exposes no retrieval API. Flowcase port (`internal/integrations/kms`):

```go
type KnowledgeProvider interface {
    Search(ctx, Scope, SearchRequest) (SearchResult, error)   // ACL-filtered by KMS for Scope.UserID/TenantID
    GetSource(ctx, Scope, SourceRef) (Source, error)           // version, classification, status, expiry
    Health(ctx) Health
}
```

Needed from KMS: endpoint paths, auth (user token pass-through vs. service token + on-behalf-of user), citation/anchor format, source status values (published/withdrawn/expired), and source-change events (webhook or poll) for stale-evidence invalidation (Phase 3).

## 3. AI Agent Runtime (AgentExecutor) — Agent Studio client implemented

```go
type AgentExecutor interface {
    Provider() string                              // also the callback route segment
    StartRun(ctx, StartRunRequest) (Run, error)   // idempotency key = node execution key
    GetRun(ctx, Scope, runID) (Run, error)
    CancelRun(ctx, Scope, runID) error
    VerifyResult(ctx, CallbackEnvelope) (VerifiedResult, error) // signature, audience, timestamp
    Health(ctx) Health
}
```

`AGENT_RUNTIME_MODE` = `disabled` (default) | `stub` (development/test only, fictional output) |
`agent_studio` (any environment). Signed callbacks (implemented in `internal/integrations/agent`, used by
the stub and by Agent Studio): `POST /api/v1/agent-callbacks/{provider}` with `X-Flowcase-Timestamp`
(unix seconds, ±5 min) and `X-Flowcase-Signature: v1=<hex HMAC-SHA256(secret, timestamp + "." + body)>`,
body `{message_id, audience: "flowcase", tenant_id, run: {run_id, status, agent_id, output,
output_checksum}}`. `{provider}` must equal `Provider()` (`stub` or `agent-studio`); polling (`GetRun`)
is the fallback and needs no callback.

### Agent Studio (`AGENT_RUNTIME_MODE=agent_studio`)

| Variable | Rule |
| --- | --- |
| `AGENT_STUDIO_BASE_URL` | Required. https; http only to a loopback/private/tailnet IP, a single-label (container) name or a `.internal`/`.local`/`.localhost` name, or any host when `APP_ENV=development`. No query, fragment or credentials. |
| `AGENT_STUDIO_SERVICE_TOKEN_FILE` | Required. File with the Agent Studio service token (trimmed, ≥ 32 printable characters, no spaces). Sent as `Authorization: Bearer`; never logged. |
| `AGENT_CALLBACK_SECRET` | Required in this mode (≥ 32 characters). Agent Studio signs callbacks to `/api/v1/agent-callbacks/agent-studio` with it. |
| `AGENT_STUDIO_TIMEOUT` | Optional, default `15s` (1s–2m), per HTTP call. Health probes use at most 3s. |

Endpoints: `POST {base}/api/v1/runs` (202 new / 200 replay), `GET {base}/api/v1/runs/{id}`,
`POST {base}/api/v1/runs/{id}/cancel`, health `GET {base}/health/ready`. The response is Flowcase's `Run`
unchanged (`agent_id` = Agent Studio agent key), so reconciliation (checksum, agent id = bound runtime id,
≤ 256 KiB, JSON object) is the same as for any runtime.

Run request mapping (Flowcase `StartRunRequest` → Agent Studio body; unknown fields are rejected there):

| Agent Studio field | Source |
| --- | --- |
| `idempotency_key` | `<node execution id>:<attempt>` (older runs stored `#`; the client rewrites `#` → `:`). |
| `agent_id` | Bound `runtime_agent_id` from `PACKS_CONFIG` (`sim.intake`, `sim.solution_matching`, `sim.standard_project`, `sim.design`, `sim.cost_challenge`, `sim.proposal`). |
| `skill_version` | Binding's `skill_version`; `unpinned` → `""` (the agent's active release); otherwise the released semver. |
| `input` | SIM `input_schema` only (below). |
| `evidence` | `[{source_id, version_id, title, classification}]` — KMS sources the case owner may read. |
| `correlation_id` | Flowcase case id. `case_id` is never sent. |
| `delegated_user` | The case owner's directory subject (`users.subject`, the Control Plane user id under `trusted_jwt`), sent only when it is a UUID and the owner is an active human tenant member. |

`input` (SIM `input_schema`, `additionalProperties: false`): `case_summary` (required, ≤ 4000 chars:
case title + reference, task/step/iteration, expected artifact, reviewer revision note, the case brief
`case_summary` variable (≤ 3000), the remaining declared case variables, and the list of prior artifacts),
`customer` (≤ 200: `customer_name`, else `customer`, else `customer_id`), `rfp_excerpt` (≤ 20000),
`constraints` (≤ 50 strings of ≤ 500; array, or one newline-separated string), `upstream_artifacts`
(object keyed by artifact type: `{producer_node, version, status, content}` for the current versions of
artifacts produced by earlier steps of the pinned graph — the same rule as artifact dependencies; Agent
Studio's result envelope is stripped to `artifact_draft`). Flowcase bounds upstream content to 12
artifacts, 48 KiB each and 144 KiB in total (oversized ones become `{truncated, original_bytes,
excerpt}`); the client then keeps the body under 240 KiB by shrinking upstream content further. Fields
without a value are omitted.

Errors: network failures, timeouts, 408, 429 and 5xx are retryable (the step retries with backoff and a
new attempt/idempotency key); other 4xx (400/401/403/404/409/422) are terminal — the step fails without
retrying the same request and the case is closed as failed. On polling, a 404 means the run is unknown
to the runtime: the run is marked failed and a fresh run is started (counts as an attempt).

### Case text that reaches agents

Every agent receives the case title. `sim-tender` v2 (`internal/packs/sim/sim_tender_v2.json`, same steps
as v1) adds optional case variables `case_summary` (≤ 4000), `customer_name` (≤ 200), `rfp_excerpt`
(≤ 20000) and `constraints` (string array) to the input schema; only declared variables are forwarded.
Fresh `seed-dev` runs publish v1 and v2 (and v3, see author draft review below). A
database seeded before v2 has v1 published and the old illustrative draft open as v2 (published versions
are immutable; one open draft per workflow). Upgrade it through the API as a user holding
`workflow_designer` (edit, validate) and `workflow_publisher` (publish; human only — the same person may
hold both, no designer/publisher SoD is enforced):

1. `GET /api/v1/workflows/sim-tender/versions/2` → note the `ETag` (e.g. `"3"`).
2. `PATCH /api/v1/workflows/sim-tender/versions/2` with `If-Match: <etag>` and body
   `{"definition": <contents of samples/sim_tender_v2.json>, "change_note": "SIM case brief inputs"}`
   (replaces the unpublished illustrative draft).
3. `POST /api/v1/workflows/sim-tender/versions/2/validate` → `version.status` = `validated`; note
   `version.version_num`.
4. `POST /api/v1/workflows/sim-tender/versions/2/publish` with `If-Match: "<version_num>"`.
5. Optional: re-run `migrate seed-dev` to publish v3 (author draft review) and recreate the illustrative security draft as v4.

Running cases stay pinned to v1; new cases use v2.

### Author draft review (`author_review`)

An `agent_task` may set `"author_review": true` (boolean; the validator rejects other types and the key
on other node types). The run result still becomes an artifact version, but the step then waits
(`waiting_human`) with a pending **draft review** for the case owner (migration 000008, table
`draft_reviews`) instead of scheduling the next step. Callback and polling results both take this path.
The owner (or a tenant admin; human principals only, `403 not_case_owner` otherwise):

- **submits** the draft: the review records the artifact's *current* version (the owner may have edited
  it via artifact revision meanwhile), the step completes and the next step is scheduled exactly as
  without author review; or
- **requests a revision** with feedback (5–1000 characters, `400 feedback_required`): the step re-runs as
  a new iteration. The agent input carries the feedback as `revision_note` (Agent Studio: in
  `case_summary`) and the step's own previous draft first in `upstream_artifacts`; the result is a new
  artifact version (v2, v3 …) with a new pending review. At most 10 revision requests per step and case
  (`409 revision_limit`).

Paused or closed cases accept neither (`409 case_not_active`); closing a case or superseding the step
cancels the pending review. A draft review never decides a gate: the next approval keeps its policy,
delegation and segregation-of-duties rules (the owner still cannot approve their own case). Audit and
outbox: `draft.ready`, `draft.submitted`, `draft.revision_requested`.

Endpoints (all `If-Match` on the review's `version_num`, `ETag` on GET):
`GET /api/v1/drafts?status=pending|decided|all` (drafts of cases the caller owns; all for tenant admins),
`GET /api/v1/drafts/{id}`, `POST /api/v1/drafts/{id}/submit` `{"note"?}`,
`POST /api/v1/drafts/{id}/revision` `{"feedback"}`. `GET /api/v1/cases/{id}` includes `drafts` (newest first).

`sim-tender` v3 (`sim_tender_v3.json`) is v2 with `author_review` on all six agent steps; the illustrative
security-review draft is now v5 (`sim_tender_v5_draft.json`). `sim-tender` v4 (`sim_tender_v4.json`) makes
`opportunity_id` and `customer_id` optional and marks them `"x-hidden": true` (accepted by the API, never shown
in forms; reserved for a future CRM link; a hidden property may not be required). Fresh seeds publish v1–v4
and create the v5 draft; earlier cases stay pinned. A database seeded earlier has the old illustrative draft open as v3:
upgrade it with the same steps as above on version 3 using `samples/sim_tender_v3.json` (change note
"Author draft review"), then re-run `migrate seed-dev` to recreate the security draft as v4.

Conversational (chat-first) case intake lives in the Universal Case Workspace app, which stores its own intake sessions and calls Agent Studio directly; Flowcase only receives the resulting `POST /api/v1/cases` (migration 000010 drops the former `intake_sessions` table).

Still open with the runtime team: model/tool trace id conventions beyond `trace_ids`, and the service
identity agents use (must never map to an approver).

## 4. Policy / DoA — PENDING (business)

G0–G5 approvers, delegation, SoD rules and exception rights are **not** encoded. `policy_ref` values in samples (`sim.g0.disposition`, `poc.budget`, …) are illustrative and will be bound through external configuration in Phase 4.

## 5. Operations — PENDING

PostgreSQL version in production (local: 16), secret manager, telemetry backend (OpenTelemetry collector?), deployment target.

## Update 2026-10-31 — host contract confirmed by the AI Workspace Control Plane
The Control Plane BFF reverse-proxies `/apps/flowcase/*` and injects `Authorization: Bearer <ES256 JWT>` per
request; the browser never holds the token, so `hostBridge.ts` is not needed behind it. The claims match
`trusted_jwt`: `iss` = CP origin, `aud=flowcase`, `kid` from `<CP>/.well-known/jwks.json`, `exp - iat` = 5 min,
`tenant_id` = tenant key, `sub` = CP user id. Running instance: `flowcase-cp` on kibi-vm (see
`../control_plane/docs/INTEGRATION_MATRIX.md`). The UI is mounted under a sub-path via the new optional
`FLOWCASE_BASE_PATH` build variable (default `/`).
