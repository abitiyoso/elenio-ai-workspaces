# Phase 0 — Verification Report

Date: 2026-10-30. Scope: `CODEX_MASTER_PROMPT.md` → "Phase 0 — Architecture and repository initialization" only.
No business workflow execution was implemented.

## Delivered

| Item | Where |
|---|---|
| Architecture decisions, interface contract notes, pending dependencies | `docs/ARCHITECTURE.md`, `docs/CONTRACTS.md` |
| Go module (go 1.24.0), API + worker + migrate binaries | `cmd/api`, `cmd/worker`, `cmd/migrate` |
| Environment validation, fail-closed (default `APP_ENV=production`, `AUTH_MODE` required, stubs dev/test only) | `internal/platform/config` |
| `AuthAdapter`: host JWT verifier (RS256/ES256, JWKS cache, iss/aud/exp/nbf/iat, max TTL) + PostgreSQL tenant directory; opt-in dev mock | `internal/platform/auth` |
| Structured JSON logging with secret redaction, trace ids, error envelope, security headers, panic recovery | `internal/platform/observability`, `internal/api` |
| PostgreSQL migrations (up/down, checksummed, advisory-locked): tenancy & identity, workflow registry, case core, audit, outbox, inbox dedup | `migrations/`, `internal/platform/postgres` |
| DB-enforced invariants: published version immutability, case version pinning, append-only audit, tenant-scoped composite FKs, execution idempotency uniqueness | `migrations/000002`, `000003` |
| `KnowledgeProvider` and `AgentExecutor` ports with `disabled` (default) and dev-only `stub` adapters | `internal/integrations/{kms,agent}` |
| `/healthz`, `/readyz` (postgres + schema version + host JWKS), `/api/v1/session`, `/api/v1/integrations` | `internal/api`, `api/openapi.yaml` |
| React + TS + Vite shell porting the HTML navy/cobalt identity; sidebar nav, glass top bar, accessible mobile drawer, dev-identity banner, no login page, no demo data | `web/` |
| Host token bridge (postMessage, origin allowlist, memory only) | `web/src/api/hostBridge.ts` |
| Test/CI commands, Docker image, local compose (Postgres only + app) | `Makefile`, `.github/workflows/ci.yml`, `Dockerfile`, `docker-compose.yml` |

## Verification actually executed (local, macOS, Go 1.27.1 toolchain, Node 24, PostgreSQL 16 in Docker)

| Check | Result |
|---|---|
| `make check` — gofmt, `go vet`, Go unit + PostgreSQL integration tests with `FLOWCASE_REQUIRE_DB=1`, web typecheck, ESLint (0 warnings), Vitest, production build | **pass** |
| Go tests | 73 test/subtest cases passed, 0 failed, 0 skipped |
| Frontend tests | 13 passed |
| `migrate up` → `status` → `seed-dev` against compose DB; full down/up cycle in tests | pass |
| Live API: `/healthz` 200, `/readyz` ready, `/api/v1/session` ignores `X-Tenant-ID`, `/api/v1/integrations` returns modes only; worker `/readyz` 200 | pass |
| Startup with `APP_ENV=production` + `AUTH_MODE=dev_mock` / with empty env | refuses to start (exit 2) |
| API log scanned for `Bearer`/`Authorization` after requests carrying tokens | none found |
| UI in browser (desktop + 375px): renders, no horizontal scroll, Enter opens drawer and focuses first link, Esc closes and restores focus | pass |
| `docker build` of the app image; container exits 2 without config | pass |
| GitHub Actions workflow | **not executed** (no remote repository; file only) |

## Traceability (tests → PRD / Definition of Done)

| Requirement | Test |
|---|---|
| WCP-01, UAT-02 published versions never mutate | `TestPublishedWorkflowVersionIsImmutable` |
| WCP-03 pinning; DoD "v1 case remains pinned after v2 publish" (DB layer) | `TestCasesArePinnedToPublishedVersions` (engine-level test follows in Phase 2) |
| PRD §9 no cross-tenant write | `TestTenantIsolationByCompositeForeignKeys` |
| WCP-09 append-only audit | `TestAuditEventsAreAppendOnly` |
| Runtime key `case+node+iteration+token`, callback dedup | `TestExecutionAndInboxDedupConstraints` |
| WCP-10, UAT-09, DoD "expired token rejected" | `TestTrustedHostTokenEndToEnd`, `TestTrustedAdapterRejectsInvalidTokens`, `TestBearerTokenOnlyFromSingleAuthorizationHeader` |
| Reject unknown identities / inactive membership | `TestPostgresDirectoryResolvesOnlyActiveMembers` |
| Dev mock opt-in, fail closed | `TestFailsClosed`, `TestDevMockIgnoresRequestSuppliedIdentity` |
| Tokens never logged | `TestLoggerRedactsSensitiveAttributes`, `TestRequestLogOmitsAuthorizationAndQuery` |
| UAT-06 defence in depth for KMS results | `TestKMSStubStaysWithinCallerTenant` |
| REST contract kept in sync | `TestOpenAPIPathsAreServed` |
| DoD "no mock approval in production UI" | `App.test.tsx` (empty states, no approve control, no login form) |

## Issues found and fixed during verification

- Dev seed SQL reused one parameter as both `uuid` and `text` (SQLSTATE 42P08) — split parameters.
- `.env.example` had an unquoted value with a space, breaking `source .env` — quoted.
- `tsc -b` emitted `.js`/`.d.ts` beside sources — removed them, set `noEmit`, ignored the patterns.

## Known limitations / not in Phase 0

- No workflow validator, registry API, engine, tasks, approvals, artifacts or audit-listing endpoint yet (Phases 1–3).
- `subworkflow` remains deferred.
- Worker only runs a schema-health tick; leasing/outbox processing starts in Phase 2.
- Clicking (not keyboard) the mobile menu button leaves focus on the button rather than moving it into the drawer; keyboard users get focus moved correctly.
- Hash-chained audit and DB role separation (app role without UPDATE/DELETE grants) are planned for Phase 4 hardening.

## Unresolved external contracts (blocking production, not Phase 1)

1. **Host wrapper identity**: delivery mechanism (iframe/proxy/SDK), issuer, audience, JWKS URL, tenant claim, session binding, host-group → Flowcase role mapping.
2. **KMS**: no retrieval/evidence API exists in `../kms_go_package` yet; need endpoints, auth model (pass-through vs on-behalf-of), citation format, source status/expiry events.
3. **AI Agent Runtime**: start/status/cancel API, callback signing and key distribution, skill registry ids/versions, agent service identity.
4. **Business policy**: G0–G5 approvers, DoA, SoD and exception rules (sample `policy_ref`s are illustrative).
5. **Operations**: production PostgreSQL version, secret manager, telemetry backend, deployment target.
