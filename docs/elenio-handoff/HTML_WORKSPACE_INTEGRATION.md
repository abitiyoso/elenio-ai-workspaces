# HTML navigation and existing-service integration — 2026-11-20

Subsequent shared styling of the original KMS/UCW interiors and form corrections: [SHARED_SERVICE_WORKBENCHES.md](SHARED_SERVICE_WORKBENCHES.md). The checkpoint below retains this increment's original scope and evidence.

Branch: `feat/elenio-html-navigation`, based on PR #10 tip `849e3f9c40cdee86ae7fe4fc200a4ae7fc976dbf`. This implements the owner's continuation to use the previous team's functions in the agreed UI. It preserves the original backend authorities, official logo, Inter font and shared light components. It does not close whole-master or visual acceptance.

## Implemented

- All **17 exact V10/V9 menu labels and five groups** are present, with Solutions added by the permanent delivery addendum. A unit check reads the actual reference NAV, including its additive V9 entries. Active states distinguish templates, studio, cases and release views. The sidebar scrolls independently so all menus remain reachable.
- Workflow Studio has a named workflow selector and opens the existing native graph editor. Template Repository retains the existing authorized template creation. Integration Hub opens the original registered connector catalog. Testing Studio and Evaluation & Releases launch the original Agent Playground and Evaluations. Business Apps launches original UCW; Control Center links original CP settings according to verified capabilities. Deployments shows registered runtime health and links solution candidates; it does not pretend they are installed releases.
- Connected Home retains the agreed hero/capability composition, resumes actual authorized Flowcase records, and opens the real builder and Agent Studio. It no longer labels a connected user as the Abitiyoso fixture or claims one local project. Workflow definitions remain tenant-authorized under original Flowcase contracts; they are not silently re-scoped by CP workspace labels.
- The agreed Knowledge explorer is now shared between the explicit design fixture and **live KMS**. Live collections/document metadata, filtering, lifecycle labels, versions, source-PDF links, collection creation, PDF upload contract, latest document jobs/retry and retrieval contracts reuse the original APIs. No fixture records appear after a live error. Unknown owner/page/version metadata is not invented. The original governance workspace remains the authority for independent review, publication, revocation, expiry and ACL changes.
- Fixed pristine local example state being treated as dirty on initial load. Real edits still require the existing discard/save decision before navigation.

## Existing native services

`Start-LocalWorkspace.ps1` now runs all five original services in the existing loopback sandbox. The previously documented native CP/Flowcase/Agent services remain unchanged in authority. New services:

| Listener | Existing service / independent boundary |
|---|---|
| 5380 → 8380 | Original KMS web/API, separate `kms_workspace_live` PostgreSQL database, trusted CP JWT with KMS audience, original tenant/principal directory |
| 8480 | Original UCW web/BFF, separate `ucw_workspace_live` database; original CP token exchange delegates the actual user to Flowcase |

KMS maps the source `acme-demo` tenant to its original tenant UUID. Its source directory independently grants steward access. Nadia is a CP admin but is absent from that KMS directory and remains denied, including with forged browser role/identity headers. No self-approval exemption is added. UCW's local exchange client has only the original Flowcase exchange target; its plaintext secret stays in an owner-only ignored file, while CP stores its hash. The browser sends same-origin cookies to CP and receives no provider or service-client secret.

All listeners remain `127.0.0.1`, with the explicit fixture IdP and synthetic people. Gateway root paths now correctly serve the original app index; directory browsing, foreign hosts and unsafe methods remain rejected, browser cookies/proxy authorization are stripped before the audience-token upstream request. The stop script verifies sandbox PID/executable ownership and retains databases.

## Verification and provenance

Commands: existing `Start-LocalWorkspace.ps1`; `Test-LocalServices.ps1 -Modules control-plane,kms,ucw`; frontend `npm run typecheck`, `npm run lint`, `npm test`, mounted `npm run build`; `npx playwright test`; and opt-in `ELENIO_LOCAL_E2E=1 ELENIO_WORKSPACE_E2E=1 npx playwright test --config playwright.workspace.config.ts`.

- **55 CP, 107 KMS and 34 UCW Go tests** pass with real disposable PostgreSQL; no failures/skips. The gateway/fixture identity tests and targeted Go vet also pass. The unchanged Flowcase 110 and Agent 87 suites retain the prior same-session checkpoint; they are not represented as a fresh full five-module run.
- **100 frontend unit/component tests** pass, including exact reference menus, proxy credential boundary, actual document/version mapping, source citations and fail-closed live access. Positive search/PDF transport tests use explicit contract doubles, not unavailable infrastructure.
- **41 browser regressions** and **7 real workspace cases** pass. Native cases use no route interception: real login/session, original Agent functions, workflow/solution/candidate persistence, independent negative grants, native connector/test/settings routes, KMS collection creation from both shared and original UI, and UCW delegated operator context/case listing.
- Knowledge responsive evidence at **1440/1280/1024/768/390**; no horizontal overflow. Zero serious/critical axe findings in the checked Knowledge desktop/mobile views, retaining Agent and candidate checks. This is scoped automated checking, not whole-product WCAG certification.
- See `evidence/html-workspace-verification.json`, browser JSON reports and `html-live-knowledge-*.png`. Screenshots show real synthetic sandbox state, including genuinely empty document lists. UI audit captures and findings: [HTML_WORKSPACE_VISUAL_AUDIT.md](HTML_WORKSPACE_VISUAL_AUDIT.md).

No new model generation/probe, source credential import, production deployment or main/upstream write is part of this increment.

## Remaining gates

KMS metadata and governance APIs work, but **S3/object storage, Elasticsearch, Docling/PDF processing and an embedding provider are not configured** in this sandbox. KMS `/healthz` succeeds; `/readyz` and CP readiness correctly report unmet dependencies. PDF ingestion/search cannot be called end-to-end here; the UI explains the dependency gate. The approved 9Router Chat Completions model does not constitute an embedding configuration.

UCW loads its existing business workspace and real delegated Flowcase context, but its model-backed intake still needs an approved Agent release/service integration and Docling. Its original interior has not yet adopted the full shared design. The shared Knowledge primary view is integrated; its advanced original governance interior also retains legacy styling. The full exact-menu/content reconciliation, richer Agent visual identity, unified project/solution lifecycle, general Economics ledger/rates/budgets, transitive independently approved solution releases and executable OCI/configuration bundles remain required implementation work. Known original browser key-pattern validation warnings are retained for a focused correction.

No Docker sandbox was supplied. Clean standalone/enterprise install, migration/upgrade, backup/restore and rollback remain unverified installation gates. A working menu, candidate checksum or native local harness is not that evidence. No production merge/deployment approval has been given.
