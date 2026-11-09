# Phase 4 — Verification Report (SIM pack configuration, UI completion, hardening)

Date: 2026-10-30. Scope: `CODEX_MASTER_PROMPT.md` → "Phase 4 — SIM pack, end-to-end UI, hardening".

## Delivered

| Item | Where |
|---|---|
| External pack bindings (`PACKS_CONFIG`, mounted read-only): six SIM agents → runtime agent id + skill version; G0–G5 and PoC policies → SoD rule set; ratification only with a cited reference/date/approver; strict decoding, unknown refs/rules fail startup | `internal/packs/bindings.go`, `config/packs.json` |
| Runtime uses the bindings: `StartRun` with the mapped runtime id (recorded on `agent_runs`, migration 000007) and reconciles results against it; approvals enforce the policy's configured SoD rules | `internal/cases/agents.go`, `commands.go` |
| Metrics (Prometheus text, no new dependency) on internal-only listeners: HTTP requests by route pattern/status class and latency histogram; worker ticks/claims/errors/outbox dispatch; gauges for cases, open/failed executions, queue lag, outbox (incl. dead), agent runs, pending approvals/re-reviews, stale artifacts | `internal/platform/metrics`, `internal/cases/metrics.go` |
| Permission negative matrix: 17 mutating/read operations × 3 refused callers (member, approver outside authority, fully privileged other tenant) = 44 refusals, plus empty cross-tenant lists and an unchanged case | `internal/api/authz_matrix_integration_test.go` |
| UAT-10 (SIM gate order) as an explicit test; UAT report generated from an actual test run | `TestUAT10SIMGateOrder`, `docs/UAT_REPORT.md` |
| UI: Integrations page shows pack bindings (runtime ids, skill versions, provisional vs ratified with reference, SoD); audit subject filter; audit wording for case/approval/artifact events | `web/src/pages/Integrations.tsx`, `Audit.tsx` |
| Docs: operations runbook (alerts, playbooks), deployment (env reference, production checklist), backup & recovery (with an executed drill) | `docs/OPERATIONS_RUNBOOK.md`, `docs/DEPLOYMENT.md`, `docs/BACKUP_RECOVERY.md` |

The workflow designer, case timeline, task inbox and approvals UI were completed in Phases 1–3; Phase 4 adds the
bindings view and audit filtering rather than new screens.

## Verification actually executed

| Check | Result |
|---|---|
| `make check` (gofmt, vet, Go unit + PostgreSQL integration with `FLOWCASE_REQUIRE_DB=1`, typecheck, ESLint 0 warnings, Vitest, build) | **pass** |
| Go tests incl. subtests | 232 passed, 0 failed, 0 skipped |
| Frontend tests | 30 passed |
| UAT-01 … UAT-12 mapped to tests | all pass (`docs/UAT_REPORT.md`) |
| Backup/restore drill (local): `pg_dump` → `pg_restore` → identical counts, append-only trigger intact, `migrate status` catches an outdated schema | pass |
| VM redeploy: migration 000007; catalog shows bound runtime ids and provisional policies with SoD; API `:9180` and worker `:9181` metrics answer on `127.0.0.1` and are **not** reachable from the tailnet; `/readyz` ready | pass |
| GitHub Actions | not executed (no remote repository) |

## No unverified policy claims

All G0–G5 and PoC policies remain **provisional** in the shipped configuration. Delegations in the dev seed are
development conveniences. Ratification can only be expressed by an operator-supplied record citing the decision;
Flowcase never infers it.

## Remaining before production (external)

1. Host wrapper identity contract → `trusted_jwt` configuration and staging test.
2. Real KMS, AI runtime and connector adapters (replace stubs), with live E2E tests using credentials from the environment.
3. Business: ratified G0–G5 policies, delegations from the DoA, per-policy SoD.
4. Operations: production PostgreSQL with PITR and a production restore drill, alerting, at least two workers,
   penetration test.
5. Optional platform work: admin UI/API for delegations and domain roles (today SQL/seed), supervised retry of
   failed cases, KMS source-event feed into `HandleSourceChange`, finer-grained artifact dependencies.
