# Original KMS and business workspace shared design — 2026-11-20

Branch `feat/elenio-existing-workbench-polish`, based on PR #11 tip `e6464d91adeabf7acaa7abd9f0649c1897347529`. This continues the owner's direction to reuse existing functions and preserve the agreed design. No backend code, routes, grants, approval rules or React versions change.

## Changes

- Original KMS governance and UCW business workbenches retain their existing React pages and API flows. Their dark sidebars and replacement K/Casebase branding are replaced with the byte-identical official Elenio logo/icon, same bundled Inter, canonical shared light tokens and 228px sidebar. Original useful local menus remain: business users still see cases/domain-pack views, while knowledge governance remains distinct from workflow building.
- Original CSS declarations were updated rather than stacking a new alternate theme. Readable label sizes, neutral surfaces, selected blue states and focus states follow the existing design foundation. KMS contrast findings, colliding identity text and mobile-hidden navigation were corrected from actual browser evidence. UCW mobile search now has a full row, two readable filters and a separate view toggle.
- `packages/ui/mobileNavigation.ts` is shared by both original service drawers: first focus, Tab wrapping, Escape and trigger restoration. It owns no session, API or authorization state.
- A canonical source-snapshot tool preserves independently packaged frontend contexts. Seven SHA-256 pins cover shared tokens, drawer behavior, snapshot tool, exact official logo/icon, Inter WOFF2 and license. `predev`/`prebuild` reject drift; explicit `--sync` copies approved canonical files. Both apps pass isolated snapshots with CRLF checkout text and reject deliberate token changes. This verifies the preflight, not a Linux/container installation.
- Corrected original Agent/skill/evaluation/provider/deployment and solution key HTML patterns for modern Unicode-set validation, including the provider URL character class. The browser previously ignored malformed patterns. Native E2E now rejects an invalid Agent key in the form, accepts a valid key and verifies model/provider pattern syntax. Backend validation and endpoint allowlists remain authoritative.
- UCW's manifest XSS regression still checks hostile markup remains escaped. Its assertion now allows the known official logo while rejecting the hostile image and any image inside manifest code.

## Validation

Original UCW **37** unit/component tests pass, including intake, artifacts, cases, review/revision, pack/schema and escaped manifest content. Flowcase **100** unit/component tests and **41** browser regressions pass. UCW/Flowcase lint and all four mounted frontend typechecked builds pass. Original KMS/Agent have no configured lint/unit frontend scripts; their build/typecheck and native browser coverage are used without inventing nonexistent checks.

**Seven native workspace E2E cases** pass with real CP sessions, audience-bound original services and PostgreSQL; no route interception. They retain Agent authoring, native workflow→solution→candidate persistence, independent operator/domain denial, connector/testing/settings navigation, KMS collection persistence from shared and original UI, and UCW actual user delegation/case listing. Expanded checks verify:

- KMS Collections and UCW My Cases at **1440/1280/1024/768/390**, no horizontal overflow;
- served logo bytes match the official canonical image; actual bundled Inter loads; both sidebars are white;
- zero serious/critical axe findings in checked desktop/mobile views;
- mobile drawer keyboard wrapping/Escape/focus restoration;
- invalid browser form keys remain blocked, while valid original authoring succeeds.

Evidence: `shared-kms-collections-*.png`, `shared-business-cases-*.png`, browser JSON reports and `shared-service-verification.json`. Desktop/mobile screenshots were opened and inspected, and the observed layout/contrast problems were fixed before final captures. Earlier dark KMS/UCW screenshots remain in the PR #11 history and the focused audit JPEGs. Native startup rebuilds and restarts the owned services; UCW intentionally loads its SPA index at server startup, so replacing assets alone is insufficient for its running process.

The Go backend is unchanged; retain the previous verified CP 55 / KMS 107 / UCW 34 and Flowcase 110 / Agent 87 checkpoints rather than representing them as fresh runs. No new model generation/probe or production deployment is initiated.

## Remaining acceptance

These workbenches now share the agreed visual foundation. Full HTML page-content parity and full-product visual/UAT acceptance remain open, including richer Agent visuals, unified project/solution lifecycle and broader library/test/deployment composition. This does not imply a working model-backed business pilot, live KMS PDF/search pipeline or portable installation. KMS storage/Elasticsearch/Docling/embedding configuration, governed UCW Agent integration, general Economics ledger/rates/budgets, transitive independent solution releases and OCI/configuration delivery remain tracked requirements. No Docker sandbox is available for clean install/upgrade/backup/restore/rollback proof. Main/upstream remain unchanged; production deployment needs separate approval.
