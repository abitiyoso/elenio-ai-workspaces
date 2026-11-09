# Phase 3 — Verification Report (Parallelism, artifacts, integration bridges)

Date: 2026-10-30. Scope: `CODEX_MASTER_PROMPT.md` → "Phase 3 — Parallelism, artifacts and integration bridges".
No real KMS / AI runtime endpoints exist yet, so no live E2E against them (see "Pending").

## Delivered

| Item | Where |
|---|---|
| Parallel split/join: one branch token per outgoing edge, keyed by the split execution (re-runs after rework get fresh tokens); join counts arrivals and schedules its successor once (`join:<split execution>` idempotency key) | `internal/cases/parallel.go`, migration `000006` |
| Durable `agent_task`: run key = execution + attempt; minimal case-scoped input (declared input fields only) + structured `evidence_policy`; KMS sources checked with the **case owner's** permissions and re-checked for tenant; withheld sources recorded with reason; bounded polling + timeout; results reconciled (agent id, output checksum, size) and stored as artifact versions; failures retry with a fresh run | `internal/cases/agents.go` |
| Signed agent callbacks (**proposed** contract): HMAC-SHA256 over `timestamp.body`, ±5 min window, audience `flowcase`, output checksum, tenant match, inbox dedup by message id; duplicate / late / superseded / out-of-order progress acknowledged without effect | `internal/integrations/agent`, `POST /api/v1/agent-callbacks/{provider}` |
| Durable `service_task`: only registered connectors/operations; execution id as idempotency key; params redacted in `tool_invocations` | `parallel.go`, `internal/integrations/connector` |
| Durable `timer`: persisted `wake_at`, claimed when due (survives restarts), fires once, escalation signal only | `parallel.go` |
| Runtime capability check: cases needing an unconfigured agent runtime or connector are refused (422) | `runtimeGaps` |
| Artifacts: immutable versions (DB trigger), checksum, producer, dependencies derived from the pinned graph (upstream producers only), evidence refs with status | `internal/cases/artifacts.go` |
| Approvals bind to the exact covered artifact versions (in the subject hash and `approval_artifacts`); approving marks them approved | `artifacts.go`, `commands.go` |
| Targeted invalidation: a new version (agent re-run or human revision) or invalid evidence marks only downstream artifacts stale; approvals that covered an affected version are invalidated and a **re-review** opens for that gate only; pending approvals are re-bound; any gate refuses approval while covered artifacts are stale (`stale_artifacts`) or re-reviews are pending (`rereview_pending`) | `invalidate`, `reviewImpact` |
| Evidence reconciliation: worker re-checks cited KMS sources (owner scope) every minute; withdrawn/expired/re-versioned/forbidden → artifact stale + review impact; `HandleSourceChange` ready for a KMS event feed | `HandleSourceChange`, `ReconcileEvidence` |
| Dev stubs: AI runtime (stateless, fictional labelled output, HMAC callbacks), KMS, `poc.notify` connector; SIM tender is now runnable end-to-end in development | `agent.NewDevelopmentStub`, `connector.NewStub` |
| API: artifacts list, artifact revision (`If-Match`), callback endpoint; OpenAPI 37 operations | `internal/api/cases.go`, `api/openapi.yaml` |
| UI: artifacts panel (versions, stale reasons, dependencies, evidence status, owner revision), agent runs (stub labelled, withheld sources), approval card shows covered versions, re-review badge, blocked reasons | `web/src/pages/CaseArtifacts.tsx`, `Approvals.tsx` |

## Verification actually executed

| Check | Result |
|---|---|
| `make check` (gofmt, vet, Go unit + PostgreSQL integration with `FLOWCASE_REQUIRE_DB=1`, typecheck, ESLint 0 warnings, Vitest, build) | **pass** |
| Go tests incl. subtests | 174 passed, 0 failed, 0 skipped |
| Frontend tests | 29 passed |
| Local live run (API + worker + stubs): SIM v2 case to G5 (6 agents, parallel joins, human security review) → HLD revised **in the browser** → security-assessment, wbs, cost-review, proposal stale; requirement-register and fit-gap untouched; G2/G3/G4 invalidated with re-reviews; G0/G1 approved; G3/G4/G5 blocked `stale_artifacts` | pass |
| VM redeploy: migration 000006, current images, callback secret generated on the VM; full SIM v1 tender **completed end-to-end on the VM** (G0–G5, 6 artifacts, unavailable KMS source withheld) | pass |
| GitHub Actions | not executed (no remote repository) |

## Exit criteria → tests (`internal/cases/phase3_integration_test.go` unless noted)

| Criterion | Test |
|---|---|
| Duplicate callback / join exactly once | `TestJoinFiresOnceWithDuplicateAndOutOfOrderCallbacks` (redelivered message → duplicate; join waits for both branches; review scheduled once) |
| Out-of-order agent result | same test (progress after success → ignored; second success → ignored; output not overwritten), `TestFailedAgentRunRetriesWithFreshRun` (result for superseded run ignored) |
| Permission propagation | `TestSIMRunsEndToEndWithPermissionScopedEvidence` (forbidden, withdrawn, foreign-tenant, missing sources never reach the agent; scope = case owner; exclusions recorded) |
| Stale evidence | `TestWithdrawnEvidenceIsNeverAcceptedAsProof` |
| Safe prompt/tool boundaries | evidence text (“IGNORE PREVIOUS INSTRUCTIONS…”) never enters task input; `TestAgentOutputCannotApproveOrChangeCaseData`; unregistered connectors refused; callback tampering/cross-tenant rejected; `internal/integrations/agent/agent_test.go` |
| Task/approval re-review; DoD “change invalidation blocks G5 and reopens only affected reviews” | `TestChangeInvalidationBlocksG5AndReopensOnlyAffectedReviews` |
| DoD “parallel split/join with duplicate callback → exactly one successor” | `TestJoinFiresOnceWithDuplicateAndOutOfOrderCallbacks` |
| UAT-07 replay/recovery of side effects | `TestServiceSideEffectAppliedOnceAcrossCrash` |
| UAT-12 durable timer | `TestTimerSurvivesRestartAndFiresOnce` |
| SIM G0–G5 with six agents on the same runtime (UAT-01/10) | `TestSIMRunsEndToEndWithPermissionScopedEvidence` |
| HTTP callback contract | `internal/api/cases_integration_test.go` `TestAgentCallbackEndpoint` |

## Design notes and limitations

- Dependencies are derived from graph ancestry (conservative: everything upstream). Finer-grained links (e.g. requirement-level) need artifact schemas from the domain packs.
- Stale artifacts are fixed by a human revision or a re-run; agents are not automatically re-run on invalidation.
- Re-reviews restore confidence in a gate without moving the flow; rejecting a re-review closes the case; "return" is not offered on re-reviews.
- The in-memory connector stub de-duplicates per process; real connectors must honour the idempotency key.

## Pending (external)

1. AI runtime: real start/status/cancel endpoints, callback signing and key distribution (the HMAC envelope here is a proposal), skill registry/versions.
2. KMS: retrieval/GetSource API, permission model (owner-scoped vs. on-behalf-of), source change events (would call `HandleSourceChange`).
3. Connector contracts (CRM, pricing, PMS) and their idempotency semantics.
4. Business: G0–G5 delegations/DoA, per-policy SoD, artifact schemas.
