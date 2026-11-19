# Phase 0–1 verification — 2026-11-18 Asia/Jakarta

## Frontend

Commands run in each application's own directory, using its locked dependencies (`npm ci --ignore-scripts`).

| App | Unit/component tests | Static/build checks |
|---|---:|---|
| Flowcase baseline, before edits | 44 / 44 | lint + build pass |
| Flowcase after Phase 1 | 52 / 52 | lint + typecheck + build pass |
| Control Plane | 17 / 17 | lint + build pass |
| Universal Case Workspace | 37 / 37 | lint + build pass |
| Agent Studio | no unit script | tsc + build pass |
| KMS | no unit script | tsc + build pass |

Flowcase: `npm test -- --reporter=json --outputFile=../../docs/elenio-handoff/evidence/unit-results.json`, `npm run lint`, `npm run typecheck`, `npm run build`, `npx playwright test`. Existing React `act(...)` warnings occurred in the baseline persona test; no failing frontend assertions.

Seven browser tests pass: primary journey/persistence/role preview/knowledge; palette/diagnostics/undo/import; four routes across six viewport sizes; keyboard command/dialog focus; axe desktop checks across five routes; actual node drag, port drag, inspector resizing and position undo; bundled Inter loading, scenario brief continuity and Project context sections. Fit View animates; port test waits for the animation before using screen coordinates. No execution is simulated. Source and targets are verified through exported canonical JSON.

Screenshots are browser captures of the actual implementation, not HTML prototypes or generated mockups. 24 viewport screenshots plus inspector. Browser primary journey captured no uncaught page errors. Axe checked WCAG A/AA rules tagged through 2.2, serious/critical only; manual keyboard paths also tested. This is not a blanket conformance or production security claim.

## Backend baseline

Official portable Go 1.27.4, archive SHA256 verified against go.dev release metadata. Command: `go test -json ./...` per module, with no production DB variables introduced. Counts below are top-level test terminal events, excluding subtests and package-level events.

| Module | Passed | Skipped | Failed | Exit |
|---|---:|---:|---:|---:|
| Control Plane | 17 | 20 | 0 | 0 |
| Flowcase | 59 | 51 | 0 | 0 |
| Agent Studio | 65 | 16 | 1 | 1 |
| KMS backend | 103 | 4 | 0 | 0 |
| UCW | 24 | 8 | 2 | 1 |

Failures predate frontend work and no backend source was changed:

* Agent Studio `TestFileVaultEncryptsScopesAndRevokes`: rejects directory without POSIX mode 0700. Windows environment cannot establish the required Unix permission model. Rule retained.
* UCW `TestSanitizeFilename`: `/` becomes a Windows backslash instead of empty.
* UCW `TestSPAServing`: temp-directory cleanup fails because Windows still reports an open file handle.

Skipped tests require CP/FLOWCASE/AGENT_STUDIO/KMS disposable database settings; list retained in `evidence/go-baseline-summary.json`. Unit test exit 0 does not mean those integration/security cases ran. Do not use a production database to clear these skips. A Linux CI/disposable-service follow-up is needed; WSL is not installed and Docker is unavailable in this environment.

## Security / provenance / performance

All-history Gitleaks scan through code commit: 3 commits, ~6.98 MB, nine original reviewed non-secret matches. Six artifact digest entries, empty-string SHA256 sentinel and two test-only literals; no committed scanner ignore. The upstream digest manifest is stale. Owner explicitly confirmed private copying rights after missing LICENSE was reported.

Changed app production dependency audit reports zero advisories; full dev audit reports 13, including inherited critical tinypool/Vitest tooling exposure and transitive globbing. Compatible fixes were applied, no forced major migration. Other frontend baseline installs also reported inherited advisories (CP 12; UCW 16; Agent Studio/KMS 4 high each); their lockfiles were not changed.

ELK is lazy-loaded separately (~1.47 MB, ~449 KB gzip) and still emits a large-chunk warning. The review entry itself is ~105.5 KB + shared runtime chunk, rather than bundling the layout engine into Home. No performance SLA inferred from bundle sizes. No external fonts/CDN assets are required.

No backend, schema, tenant header, credential vault, approval gate or production deployment was modified. Brand PNG hash matches the original handoff asset. Only the private owner repository receives development commits; upstream push disabled; main preserved as original baseline.

The owner-requested typography/content correction re-ran changed-app lint, typecheck, 52 tests, multi-entry build and seven browser tests. The earlier first-slice content was replaced by final-kit Home composition; the owner visual gate remains reopened. Fontsource Inter 5.3.0 is bundled and its OFL notice retained. See KIT_ALIGNMENT_PHASE1.md for intentionally missing later-phase features.

## Phase 2 contract checkpoint — 2026-11-19 Asia/Jakarta

Tested implementation `e6c6b7b072d469c22b5d2b95c980038ca5db021a` on `feat/elenio-core-engineering`. Same locked dependencies and existing authenticated backend routes; no Go/SQL/OpenAPI/backend changes.

- `npm run lint`: passed (zero warnings permitted).
- `npm run build`: passed TypeScript and production builds for original `index.html` and new `elenio.html`. Existing lazy ELK warning remains (1,474.15 kB / 449.25 kB gzip).
- `npm test -- --reporter=json --outputFile=../../docs/elenio-handoff/evidence/unit-results.json`: **60 passed, 0 failed**.
- `npm run test:e2e`: **22 passed, 0 failed/skipped/flaky**, Chromium, one worker, 39.0 seconds. JSON report retained in evidence.

Service API responses are intercepted by browser tests and explicitly label identity **Contract test fixture**, environment **test**, auth **dev_mock**. They verify request/response behavior, not server database isolation. Cases include 401/malformed session, forbidden creation, viewer restrictions, unknown runtime capabilities, published immutability, stable create retry Idempotency-Key, native blank/template/import requests, exact If-Match, 412 edit retention, diff confirmation, validation diagnostics, canonical branch/schema/policy preservation, scoped layout reload and unsaved browser Back. Additional unit coverage rejects invalid result contracts and unsafe app routes, and verifies that CP calls do not forward a Flowcase bearer token.

Serious/critical axe A/AA rules tagged through WCAG 2.2 pass on five local routes plus connected registry/catalog/editor. Manual keyboard/dialog tests and focus canvas expansion pass; this is not blanket accessibility conformance. Responsive overflow regression passes local routes at 1440/1280/1024/768/390/360 px, service registry/editor at 1440/768/390 px. The newly introduced 360 px topbar overflow was reproduced and corrected. Templates navigation from an already mounted registry has a dedicated regression case.

Screenshots: 25 refreshed local captures plus eight service captures, all of the actual implementation. Connected captures are contract-test evidence. Desktop and mobile editor screenshots inspected after the compact validation header/focus changes. Official logo SHA256 remains `00e7cb514b48a4e5e52db62552397263b6aef2b914595afc714ea13711ecb254`.

All-history Gitleaks through this implementation scanned six commits / 7.23 MB: nine matches, all at original upstream SHA, same individually reviewed checksum/sentinel/test-literal findings. No new findings, suppressions or ignored matches. Backend baseline above remains historical; unchanged services were not rerun. No sandbox was supplied (owner explicitly confirmed), so live authorization/concurrency/KMS/launch/provider acceptance stays open. General Architect generation and many-workflow project orchestration are not delivered by these contract tests.

## Local end-to-end checkpoint — 2026-11-19

This supersedes the earlier no-disposable-DB limitation. Official PostgreSQL 16.16 portable binaries were downloaded via the official PostgreSQL→EDB link; postgres.exe Authenticode signature verified valid for EnterpriseDB Corporation. Recorded archive SHA256: `5c0d06f3d26a929ecb36dd931409f1e51a4181355794a259b90aae8bffc57a9f` (recorded digest, not a claim of independent vendor checksum comparison). No system service installed. Password-authenticated loopback-only cluster, six separate local databases, ignored state.

Final counts: Flowcase 110/0/0, Control Plane 37/0/0, Agent Studio 81/1/0, KMS 107/0/0, UCW 34/0/0 (pass/fail/skip, top-level tests). Total **369 passed, 1 failed, 0 skipped**. Real database integration suites executed; existing fake IdP/model/Docling transports still apply where their harnesses use them. The initial KMS test setup omitted its migration ledger; setup was corrected to use the real migrator and all 107 tests pass. Flowcase revision-limit failure was a test timing race; bounded polling preserves the actual ten-revision limit. UCW Windows filename and SPA directory-handle defects are fixed. Agent Studio vault POSIX permission failure remains unchanged and visible.

Frontend: **60 unit/component, 22 contract/fixture browser, 6 live local browser tests passed**. Lint, TypeScript + production build and changed-package Go vet pass. Six live cases test browser→actual HTTP API→worker→PostgreSQL, including true/false branching and independent approval. New Execution trace displays server records and exact pinned version; no AI cost/usage fabricated. Serious/critical axe rules pass on that real execution screen. Live screenshots at 1440×900 viewport; full-page output can be taller. See LOCAL_E2E.md and evidence/local-e2e-verification.json.

History scan through `9728943`: 9 commits / 7.31 MB, same nine reviewed original non-secret findings; no new findings or exclusions. `.local-e2e/` passwords, binaries, databases and raw logs are ignored and not committed. Owner explicitly confirmed no provider exists and instructed local completion plus AI blocker. Full product, production identity, external KMS/provider and later-phase acceptance remain open.

## Economics scenario validation — 2026-11-19

Six new deterministic cases reproduce the kit arithmetic and edge cases using fixed-point BigInt calculations. Changed Flowcase frontend has 66 passing unit/component tests. Browser checks include scenario editing, revision reload, JSON export/import validation, responsive screenshots at 1440/768/390 and serious/critical axe checks. Original project Economics test now expects its working planner link. Complete final browser/static/build results are recorded in the updated test reports and delivery checkpoint. No backend tests rerun for this frontend-only continuation. Existing lazy ELK chunk warning remains.
