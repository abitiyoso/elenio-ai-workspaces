# Phase 0 audit — 2026-11-18

Source commit: `8bfae869e8ad3adaf10b496428ffbcf22c196db9` (one root commit).
Owner verified independently through GitHub connector and Git Credential Manager + official CLI: `abitiyoso`.
Private independent destination created and verified empty: https://github.com/abitiyoso/elenio-ai-workspaces.
User confirmed copying/development rights in this chat after absence of LICENSE was reported. This is authorization for this private project, not a public license grant.
`upstream` fetches source; its push URL is `DISABLED`. No upstream write, reset, history rewrite, deployment or main merge.

## Current authorities and integration boundaries

| Module | Source inspected | Authority / preservation constraint |
|---|---|---|
| Control Plane | `api/openapi.yaml`, `internal/platform/config/config.go`, `AUTH_AND_TRUST_BOUNDARIES.md` | OIDC BFF, audience-bound token exchange, entitlements, server-verified tenant/workspace. Production rejects dev seeds/mock auth; upstream targets are deployment configuration, not browser input. |
| Flowcase | `internal/workflows/definition/definition.go`, `validator/validator.go`, `compiler/compiler.go`, `internal/api/cases.go`, `web/src/api/{client,workflows}.ts`, existing Designer | Canonical definition schema 1.0, IDs/next/config/policies; immutable publication hash; optimistic draft save via If-Match. Runtime capabilities at `/api/v1/runtime-capabilities`. Condition config maps true/false targets explicitly; join requires all + split_ref. |
| Agent Studio | runtime/model OpenAPI contracts, `internal/ports/auth.go`, release/evaluation source | Bounded agent execution, signed callbacks, approved models/releases, pinned skills, redacted traces. Mock endpoint tests do not establish production provider availability. |
| KMS | `backend/openapi.yaml`, frontend source | Document/version/ACL/publication/retrieval authority. PDF ingestion; document access and validity checked by backend. No other format support inferred. |
| Universal Case Workspace | domain packs, integration harness, web API | Operator experience over Flowcase; no duplicate case authority. SIM pack fixture is not evidence of startable published deployment. |

All five frontend manifests audited. CP + UCW React 19; Flowcase + Agent Studio + KMS React 18.3. No framework migration needed. Existing Flowcase routes include workflows, designer, cases/artifacts/drafts, tasks, approvals, integrations and audit. They remain intact.

## History / sensitive-data review

Gitleaks scanned all history: 1 commit, ~5.69 MB; nine candidates. No exclusions or scanner suppressions committed.

Individually inspected candidates:

* `ai_agent_studio/CHECKSUMS.json` lines 16,35,41,45,46,72: JSON file-path-to-64-hex digest entries. They are artifact checksums, not credential assignments. Current content hashes do NOT match: this upstream manifest is stale and must not be used as integrity evidence. Do not alter original history to repair it.
* `control_plane/internal/platform/config/config.go:51`: SHA256 of the empty string, explicitly documented as a validation sentinel.
* `control_plane/tests/integration/exchange_test.go:25`: literal test exchange secret used by a local integration harness.
* `universal_case_workspace/internal/integration/harness_test.go:44`: literal fake Docling API key used by an in-process test server.

Tracked `.env` files are examples; no tracked PEM/private-key/database dump found in filename review. Source includes sample identities and domain examples; fixtures must not be represented as customer production records. Automated scanning is bounded evidence, not a guarantee of absence. Dependency licenses require retaining package notices; no n8n, Linear, Notion, Langfuse or Spline source/assets copied. Official supplied Elenio assets retained unchanged in private handoff.

## Baseline evidence before frontend edits

Flowcase `npm ci --ignore-scripts`, `npm test -- --reporter=dot`: 9 files / 44 tests pass. Existing act warnings in runtime persona test. `npm run build` and `npm run lint` pass. Other module results are recorded in STATUS and evidence after completion. No VM deployment scripts executed.

## ADR 001 — incremental shared UI

Keep all existing apps and API clients. Add dependency-light React 18/19 compatible source primitives/tokens under `packages/ui`, consumed by the Phase 1 Flowcase entry. Accessible Radix dialogs, Lucide icons, React Flow and ELK are locked in this app only; no suite-wide upgrade. Tailwind/shadcn generation deferred: CSS tokens and Radix provide the required shared component contract without changing five build pipelines. Charts and Motion deferred until measured data exists. All assets bundled locally; no CDN/fonts/network assets required.

## ADR 002 — project context and fixture boundary

Phase 1 entry `elenio.html` is explicitly a local design-review fixture. Single project draft store, IndexedDB persistence, canonical Flowcase JSON, separate layout sidecar. No auth tokens or execution state persisted, no fake AI, no live publish/run/ingest controls. Role selector is a labelled preview, not authorization. Existing authenticated runtime entry remains default. Production project ownership/mapping is proposed for Control Plane, requiring Phase 2 API review; no project backend created in Phase 1.

## ADR 003 — graph adapter and layout

Graph edges come solely from `nodes[].next`; condition labels come solely from `config.branches`. Branch editing updates both atomically. ELK positions are sidecar metadata, never part of semantic hashes. Unknown extra semantic fields survive by object copy. UI diagnostics are preflight only; existing server validator remains authoritative. Runtime capabilities are not assumed from schema: fixture palette labels assumed scope and disables subworkflow; live capability integration remains Phase 2. No execution simulation.

## ADR 004 — economics and delivery

No financial ledger or runtime packaging service created before the visual gate. Proposed tenant-scoped append-only ledger owned by a dedicated economics module; node estimates, measured sandbox and actual production remain distinct. Fixture shows unpriced/no telemetry, never savings. Runtime-only customer delivery must inventory CP/token/KMS dependencies; a manifest alone cannot establish deployability. Keep existing deployment scripts unchanged.
