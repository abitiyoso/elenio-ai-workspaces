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
