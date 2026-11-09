# Flowcase — Workflow & Case Platform

Reusable workflow/case engine mini-app for the AI Workspace (Go + PostgreSQL + React/TypeScript/Vite).
**Current state: Phase 4 complete (all planned phases).** SIM tender and PoC run end to end in development with
clearly labelled AI/KMS/connector stubs; pack bindings are external (`PACKS_CONFIG`); metrics, runbook, deployment
and backup docs are in `docs/`. Production still needs the external contracts listed in `docs/PHASE4_REPORT.md`. See `CODEX_MASTER_PROMPT.md` for phases,
`docs/ARCHITECTURE.md` for decisions, `docs/CONTRACTS.md` for pending external contracts, and
`docs/PHASE0_REPORT.md` … `docs/PHASE4_REPORT.md`, `docs/UAT_REPORT.md`, `docs/OPERATIONS_RUNBOOK.md`,
`docs/DEPLOYMENT.md`, `docs/BACKUP_RECOVERY.md` for what was verified.

## Layout

```text
cmd/api, cmd/worker, cmd/migrate     binaries
internal/platform/{config,auth,postgres,observability}
internal/api                         HTTP transport (health, session, integrations, workflow registry, audit)
internal/workflows/{definition,compiler,validator,catalog,registry}
internal/packs/{sim,poc}             domain packs: catalog registrations + templates (copies of samples/)
internal/audit, internal/platform/events   audit trail and transactional outbox
internal/cases                       case runtime: engine, tasks, approvals, SoD, outbox dispatcher
internal/integrations/{kms,agent,connector}  KnowledgeProvider / AgentExecutor / connector ports + dev stubs
internal/worker                      worker loop + LISTEN/NOTIFY wake-ups
migrations/                          embedded SQL (up + down), applied by cmd/migrate
config/packs.json                    external pack bindings (agents → runtime ids, policy SoD, ratifications)
api/openapi.yaml                     OpenAPI 3.1 contract
web/                                 React mini-app (no login page)
```

## Requirements

Go 1.24+, Node 20+ (22 recommended), Docker (local PostgreSQL 16 only — Flowcase never deploys KMS,
Docling, Elasticsearch or an AI runtime).

## Run locally

```bash
make db-up                                  # PostgreSQL on 127.0.0.1:55434
cp .env.example .env && set -a && source .env && set +a
go run ./cmd/migrate up && go run ./cmd/migrate seed-dev   # also imports the SIM + PoC packs
make run-api        # :8080   (separate terminal, same env)
make run-worker     # :8081 probes (separate terminal, same env)
cd web && npm ci && npm run dev             # http://127.0.0.1:5173 (proxies /api to :8080)
```

Or everything in containers: `docker compose up --build`.

The local profile uses `AUTH_MODE=dev_mock`: fixed identities, shown in the UI as a yellow
"Development identity" banner with an "Act as" switch between the server-configured personas
(`DEV_AUTH_PERSONAS`: Dian Approver holds `domain_approver` and delegations, Vera Viewer is a plain member),
so segregation of duties can be tried locally. The server refuses this mode unless `APP_ENV=development` **and**
`DEV_AUTH_ACKNOWLEDGE=i-understand-this-is-not-secure` are set. `APP_ENV` defaults to `production`.

Production uses `AUTH_MODE=trusted_jwt` (issuer, audience, JWKS URL); see `.env.example` and
`docs/CONTRACTS.md` — the host contract is still pending.

## Tailnet VM (kibi-vm)

```bash
deploy/deploy-vm.sh                          # or: make deploy-vm VM=user@host
```

Ships the working tree, keeps the VM's own `.env` (Postgres password generated on first deploy), rebuilds,
and recreates the app containers. UI: `http://100.126.146.52:5180` (Tailscale only); API bound to `127.0.0.1:8180`
on the VM; Postgres not published. It runs `APP_ENV=development` with the dev identity because the host token
contract is pending — the yellow banner says so.

## Checks

```bash
make check            # gofmt + vet, Go unit + PostgreSQL integration tests, web typecheck/lint/test/build
make test             # Go unit tests only (DB tests skip without FLOWCASE_TEST_DATABASE_URL)
make test-integration # requires the DB; FLOWCASE_REQUIRE_DB=1 turns skips into failures
```

Integration tests create and drop an isolated schema per test, so they can share the dev database.
CI (`.github/workflows/ci.yml`) runs the same steps against a PostgreSQL service container.
