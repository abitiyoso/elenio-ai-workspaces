# Phase 2 — Verification Report (Durable case runtime, tasks, approvals)

Date: 2026-10-30. Scope: `CODEX_MASTER_PROMPT.md` → "Phase 2 — Durable case runtime and task/approval".
No external AI/KMS integration, parallel branches, agent/service tasks or timers (Phase 3).

## Delivered

| Item | Where |
|---|---|
| Case creation pinned to an exact published version (id, number, SHA-256 hash; DB trigger re-checks), input validated against `input_schema`, server-generated references `CASE-YYYY-NNNN`, idempotency key | `internal/cases/engine.go` |
| Runtime capability check: workflows using node types the runtime cannot run yet are refused (422 `unsupported_node_type`) instead of half-executing | `cases.SupportedNodeTypes`, `GET /runtime-capabilities` |
| Node engine for `start`, `end`, `human_task`, `human_approval`, `condition` (deterministic, logged evaluations with inputs) | `engine.go` |
| Durable execution: `node_executions` as the transactional queue; `FOR UPDATE SKIP LOCKED` claims with leases; expired leases reclaimed after crashes; successor idempotency key derived from the predecessor; exponential backoff; permanent failure fails node and case (never auto-approves) | `ProcessReady`, `fail` |
| Tasks: domain-role eligibility (configurable `domain_role_assignments`), `form_schema` validation, declared fields merged into case variables, comments, `If-Match` | `commands.go` |
| Approvals: human-only (service + DB trigger `FC003`), `domain_approver` role + explicit `policy_delegations` per `policy_ref`, default SoD (not own/created case, not a task submitter in the case), decision bound to a case-state `subject_hash` (`stale_approval` on drift), immutable hashed `approval_decisions` (append-only trigger), approve / reject (closes case) / return (rework) | `commands.go`, migration `000005` |
| Rework: approval `return` and task `revision` share one rewind: waiting step superseded, its task/approval withdrawn, target re-opened as a new iteration with the reason | `rewind` |
| Pause/resume (owner or tenant admin, `If-Match`); paused cases are skipped by the worker and refuse human actions | `SetPaused` |
| Case visibility: members/owner/creator, people with work in the case (domain role or policy delegation), tenant admins; others get 404 | `queries.go` |
| Outbox dispatcher: at-least-once, leased, backoff, dead-letter after 8 attempts; Phase 2 sink logs (no consumer contract yet) | `outbox.go` |
| Worker: executes steps and dispatches the outbox each tick; woken immediately by `LISTEN/NOTIFY` | `cmd/worker`, `internal/worker/listen.go` |
| REST API (+15 operations, 34 total), OpenAPI 3.1, 403 reason codes | `internal/api/cases.go`, `api/openapi.yaml` |
| Dev personas (`DEV_AUTH_PERSONAS`, dev_mock only, server-configured, selected by key) so SoD can be exercised; seeded domain roles and delegations are development-only | `internal/platform/auth`, `cmd/migrate seed-dev` |
| UI: Cases (filters, schema-driven New case, runnable check), Case detail (live process graph with step state, timeline, data, condition log, history, notes, pause/resume, auto-refresh while the engine works), My Tasks (form + comment, rework notes, revision), Approvals (decision card, mandatory rationale, provisional-policy notice, SoD explanations), Overview from live data, persona switcher | `web/src/pages` |

## Verification actually executed

| Check | Result |
|---|---|
| `make check` (gofmt, vet, Go unit + PostgreSQL integration with `FLOWCASE_REQUIRE_DB=1`, typecheck, ESLint 0 warnings, Vitest, build) | **pass** |
| Go tests incl. subtests | 162 passed, 0 failed, 0 skipped |
| Frontend tests | 28 passed |
| OpenAPI: parses, all refs resolve, 34 operations routed (contract test) | pass |
| Local browser E2E with real API + worker: start PoC case (cost 4500) → owner completes scope → condition routes to budget gate → owner sees empty approval inbox → switch to approver persona → approve with rationale → provisioning task released → complete → case Completed 100%, all graph steps done, condition inputs and full history shown; SIM correctly shown as "not runnable yet" | pass |
| Worker dispatched outbox events locally (11 logged) | pass |
| VM redeploy: migration 000005 applied, current images, personas in session, smoke case `CASE-2026-0001` created through the tailnet proxy and the VM worker opened its first task within seconds | pass |
| GitHub Actions | not executed (no remote repository) |

## Exit criteria → tests (`internal/cases/cases_integration_test.go` unless noted)

| Criterion | Test |
|---|---|
| Retry | `TestRetriesWithBackoffThenFailsCase` (succeeds on attempt 3; exhausted attempts fail node + case) |
| Crash recovery | `TestCrashedWorkerLeaseIsReclaimedOnce` (unexpired lease not stolen; expired lease reclaimed; successor scheduled once) |
| Idempotency | `TestConcurrentWorkersProcessEachStepOnce` (4 workers, 15 cases → exactly 15 tasks, 30 executions), `TestCaseCreationGuards` (replay / key reuse), duplicate task submit and decision → 409 |
| SoD | `TestPoCRunsWithGatedApprovalAndSoD` (missing_role, no_delegation, service_principal), `TestConditionFalseSkipsGateAndSubmitterCannotApprove` (sod_task_submitter, inbox explains), `TestDatabaseRefusesServicePrincipalDecisions` (DB trigger) |
| Premature gate release | `TestPoCRunsWithGatedApprovalAndSoD` (no downstream execution while pending, after repeated worker runs), `TestRejectionClosesCase` |
| Version pinning at runtime | `TestRunningCaseStaysOnPinnedVersion` (v1 case follows v1 after v2 publish; new cases take v2) |
| Rework / revision | `TestReturnAndRevisionCreateReworkIterations` (iterations 2 and 3, rationale carried, pending review withdrawn, new subject hash) |
| Pause/resume | `TestPauseHoldsCaseAndResumeContinues` |
| Outbox | `TestOutboxRetriesAndDeadLetters` |
| HTTP contract with personas | `internal/api/cases_integration_test.go` `TestCaseRuntimeOverHTTP` |

## Issues found and fixed during this phase

- Tests initially passed non-hex idempotency hashes; the database constraint rejected them (constraint kept, tests fixed).
- Case page rendered before the asynchronous worker created the first task → auto-refresh while steps are queued/running.
- Case detail side panels were cramped below 1120 px → single-column layout.

## Decisions to confirm

1. **Default SoD rules** (not own/created case; not a task submitter in the case) are platform defaults until per-policy SoD comes from the business policy configuration.
2. **Reject closes the case** (status `cancelled`, outcome `rejected_at_<gate>`). If some gates should instead return or allow re-submission, that is per-gate configuration.
3. **Task revision** is allowed only while the next step still waits on a person; later corrections go through an approval `return`.
4. **Case events**: only `note` is accepted; structured business events that change variables need a contract (they would invalidate approval subjects).

## Phase 3 next

Parallel split/join tokens, durable `agent_task` / `service_task` / `timer`, `KnowledgeProvider` / `AgentExecutor` adapters with stubs and contract tests, artifacts with versions/dependencies and targeted invalidation, duplicate/out-of-order callbacks. SIM tender becomes runnable then.
