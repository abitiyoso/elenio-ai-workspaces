# Local end-to-end acceptance — 2026-11-19

The owner requested completion end to end, then explicitly confirmed no approved AI/provider exists and instructed completion of the local flow with the AI blocker recorded. This delivery exercises the existing real Flowcase HTTP API, durable worker and PostgreSQL. It does not represent production SSO, a real model run, ERP posting, or full acceptance of all development-kit phases.

## Reproduce on Windows / PowerShell 7

Prerequisites: installed frontend dependencies, Go compatible with the repository, PostgreSQL 16 Windows server binaries, and Playwright Chromium. No global service, Docker installation or production connection is required. PostgreSQL is downloaded separately from the [official Windows download page](https://www.postgresql.org/download/windows/) and its linked [EDB binary archive page](https://www.enterprisedb.com/download-postgresql-binaries).

Commands from the repository root (adjust binary paths for your checkout):

```powershell
./scripts/Start-LocalE2E.ps1 -PostgresBin ../tools/postgresql/pgsql/bin -GoExe ../tools/go/bin/go.exe
./scripts/Test-LocalServices.ps1 -PostgresBin ../tools/postgresql/pgsql/bin -GoExe ../tools/go/bin/go.exe
```

The second command intentionally exits nonzero on the unresolved Windows FileVault test; it does not hide the failure. Raw test logs and a sanitized summary are stored under ignored `.local-e2e/`.

In the frontend directory:

```powershell
cd flowcase_platform/web
npm run dev
# In another terminal, after the API /readyz is ready:
$env:ELENIO_LOCAL_E2E='1'
npm run test:e2e -- --config playwright.live.config.ts
npm run test:e2e
```

If Chromium lives outside the current environment's default cache, set `PLAYWRIGHT_BROWSERS_PATH` to its installed directory. The live suite refuses to run without its explicit local flag and checks that the actual server identifies itself as development/dev_mock with the local E2E identity before any mutation. It contains no API response interception.

Open `http://127.0.0.1:5173/elenio.html#/connected`. The local API is `127.0.0.1:8080`, worker health `127.0.0.1:8081`, PostgreSQL `127.0.0.1:55448`. All bind to loopback. The API uses the existing explicit development-auth acknowledgement and server-configured developer/approver/viewer identities. External KMS, agent runtime and connectors are disabled; no production endpoints or identity exemptions are configured.

The start script generates a random database password and creates six isolated databases (one live preview and five test databases). Password, binaries, PID files, database contents and raw logs stay under ignored `.local-e2e/`; none are included in git. Test harnesses may reset schemas within their designated test database; never point them at another database. KMS setup reuses its real migration implementation and migration ledger; the helper accepts only the dedicated local test URL and explicit local flag. Agent test migrations use its existing SQL files. Other modules use their existing isolated harnesses.

The start script is idempotent (verified: no new migrations, seed imports skipped, existing API/worker retained). To stop owned processes and retain local data:

```powershell
./scripts/Stop-LocalE2E.ps1 -PostgresBin ../tools/postgresql/pgsql/bin
```

Stop was not invoked for delivery so the review preview remains available. Its script parses successfully; this is not a production lifecycle test.

## Actual acceptance results

Implementation: `a4e66edf9366e6521bc7f9ff3ea1007b4c157cd5`. Windows fixes: `ab1d243`. Branch: `feat/elenio-core-engineering`; draft PR #2. No production branch merge or deployment.

Six real browser/API/database scenarios pass:

1. UI blank creation → node edit → If-Match save → server validation → reload with persisted content.
2. Another writer advances the version; stale UI save receives 412 and retains the unsaved edit.
3. Published definition stays immutable; draft fork does not change the published case version; a real worker completes the case and the new trace view displays its actual server timeline.
4. Server-configured viewer gets 403 on creation.
5. Native JSON import preserves condition config and input schema; true and false cases execute the correct branch and complete.
6. Existing PoC workflow: scope task → unauthorized approval denied → distinct authorized approver → resource task → completed, with decision actor and SHA256 hash retained. The actual six-step trace is shown in the shared shell.

Six live cases, 22 contract/fixture browser regressions and 60 unit/component tests pass. Serious/critical axe checks pass on the real completed execution screen in addition to existing routes. Live screenshots use a 1440×900 viewport (full-page images may be taller); `live-*.png` are real local backend evidence. `phase2-*.png` remain labelled mocked-contract evidence. No model or external-service telemetry is inferred.

| Backend module | Top-level pass | Fail | Skip |
|---|---:|---:|---:|
| Flowcase | 110 | 0 | 0 |
| Control Plane | 37 | 0 | 0 |
| Agent Studio | 81 | 1 | 0 |
| KMS | 107 | 0 | 0 |
| Universal Case Workspace | 34 | 0 | 0 |
| Total | 369 | 1 | 0 |

All tests ran with separate real disposable PostgreSQL databases. Existing integration suites still use test IdPs/model transports/Docling doubles where specified; this does not prove production upstream integration. Flowcase draft-revision test previously dereferenced a transient nil before the next asynchronous poll was due; bounded waiting now verifies all ten revisions without changing engine limits. Two real Windows bugs were fixed: normalized slash filename handling and owned os.Root handle lifetime. SPA confinement through os.Root is retained. Go vet passes for changed Flowcase case package and UCW; frontend lint, TypeScript and both-entry production builds pass. The lazy ELK chunk warning remains.

One existing failure remains: `TestFileVaultEncryptsScopesAndRevokes` requires POSIX 0700 permissions, which the Windows filesystem mode API does not supply. Vault protections were not weakened, the test is not skipped, and real provider credential storage is not claimed functional on this host. A supported Linux environment is needed to verify that path.

## Gate limits

The requested local manual engineering/runtime flow is verified. The complete Elenio product and all phases are not declared complete. AI Architect/model evaluation/agent release need an approved configured provider (owner confirmed absent). Production IdP/CP app mount, KMS search/Docling/storage, external connectors, many-workflow project registry, economics ledger and portable production acceptance remain open according to the kit. No mock was promoted to a real provider. No remote environment, upstream repository, production branch or deployment was modified.
