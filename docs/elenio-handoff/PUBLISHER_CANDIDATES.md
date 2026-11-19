# Publisher composition and immutable review candidates

Branch `feat/elenio-publisher-candidates`, based on PR #7 at `3f815fcc457768a00ef0d845fb51fbf7f2fb2092`. The agreed design, logo, typography and shared components are preserved. This increment adds application composition/preview, safe direct-reference inventory and immutable review snapshots. It does not complete solution release approval or portable installation.

## Working functions

Business UI/both plans can compose up to 12 pages using overview, request form, task queue and results/report components. Pages have stable keys, titles and linked pinned workflows where needed. API-only output excludes UI. Existing incomplete plans remain compatible. The server rejects unsupported components, scripts/URLs/extra fields, duplicate page keys and references outside the plan.

Preview renders configured navigation and components using the existing design system. It loads no customer records, provisions no URL and performs no operational action. Intake is disabled with its missing schema/runtime mapping explained. No fictional tasks, reports, metrics or costs appear. This establishes configurable composition beyond a fixed case page; connecting it to an installed business runtime remains open.

Publisher candidates capture the exact saved plan revision and fresh server-generated partial readiness. The request body accepts only a numeric version, never client-supplied evidence, status, checksum or scope. Stable retry keys replay the original candidate even after the plan advances. New capture with a stale revision returns 412. Concurrent identical requests create one snapshot and one audit event. Changed content requires a new unique version.

Migration 0005 rejects candidate UPDATE and DELETE. SHA-256 over canonical JSON preserves exact numbers and survives JSONB key ordering. It is a checksum, not a signature or approval. History and individual reads still require current workspace authorization. Explicit JSON download exports planning metadata/evidence only: not an installable bundle, approved release or deployment record.

## Dependency boundary

Trusted Flowcase inspection extracts reference names from the exact published definition: agents, optional agent-skill binding version, connector/operation, approval policies, assignee roles, KMS-approved and case-file scopes. Config params, instructions, endpoint URLs, customer artifacts and credentials are excluded from inventory/candidate evidence.

Discovered resources stay blocked until their authoritative versions, approvals and target bindings are resolved. `inventory_complete` means direct discovery only, never transitive approval. Missing/mismatched definitions, unsupported nodes or malformed references mark discovery incomplete. A 200-resource aggregate cap emits an explicit overflow blocker. Existing audience-bound user delegation, trusted endpoint, no redirects/environment proxy and response caps remain. Capture makes no model call or domain mutation; `release_ready` remains false.

Full agent/model/skill/tool/KMS/connector resolution, application schema mapping, business-policy ratification, independent solution approval and installation remain separate gates. Future approval must revalidate current evidence rather than treating a historical observation as a grant.

## API and persistence

- GET/POST `/api/v1/solutions/{id}/candidates`
- GET `/api/v1/solutions/{id}/candidates/{candidate_id}`
- POST uses current strong plan If-Match, stable Idempotency-Key, existing CSRF/Origin policy and body `{ "version": "1.0.0" }`.
- History returns at most 50 snapshots with keyset pagination. Individual GET returns a checksum ETag and authenticated no-store response.
- Existing solution:author permits planning capture only; solution:view permits inspection. No new release rights or self-approval exemptions.
- Optional definition.application is additive. Exact contract: `control_plane/contracts/solution_registry.openapi.yaml` v1.1.0, referenced by the main OpenAPI.

The existing CP migrator applies 0005. CP owns metadata; workflow/Agent/KMS services retain execution, publication and approval. No runtime rewrite or credential copy.

## Verified evidence

50 Control Plane tests pass, zero failed/skipped, using real disposable PostgreSQL/HTTP with the existing mock OIDC harness and an explicit trusted workflow test double. Eight added top-level cases cover composition validation, reference-only inventory/redaction/bounds, numeric checksum stability, persistence/history, stale/replayed/concurrent capture, database immutability and scope/permission behavior. This is not a deployed enterprise SSO claim.

88 frontend unit/component and 40 browser regression tests pass. Three new browser journeys compose four pages, preview components, capture/download/reopen history, retry a lost response and deny capture to a planning viewer. Browser CP responses are explicitly intercepted and labelled Contract test workspace; real API/database behavior is tested separately. Lint/typecheck/production build and Go vet are recorded in delivery verification. Other four backend modules, the nine-case local runtime suite and paid model calls were not rerun. Existing ELK bundle warning remains.

Actual screenshots: `evidence/publisher-application-{1440,768,390}.png` and `evidence/publisher-candidate-1440.png`. Desktop/mobile inspected; serious/critical axe and overflow checks pass on preview. Inspection caught an invisible selected page from undefined tokens and hover override; the fix uses the shared primary-button variant and existing --e-* tokens with selected/hover regression coverage. These are contract-labelled app screenshots, not customer-installation proof.

Safe reports: `evidence/publisher-control-plane-results.json`, `publisher-unit-results.json`, `e2e-results.json` and `publisher-candidate-verification.json`.

## Review and remaining gates

Through a configured CP session, open elenio.html#/solutions, edit a UI/both plan, configure pages and save. Preview application, then capture a Publisher candidate for the saved revision and inspect its blockers. Local standalone preview still lacks a persistent CP login and shows unavailable rather than fixtures. Go tests run a real temporary CP server/DB with the mock IdP.

Owner confirmed no Docker sandbox. No clean customer install, dedicated URL, upgrade, restart, backup/restore, rollback or independent two-department scenario is claimed. Next: authoritative transitive resolution, operational page/schema mapping and independent solution approval, then pinned OCI/configuration bundles and real Compose lifecycle verification. Whole-product/master-prompt acceptance remains open. No upstream write, main merge or deployment.

## Private delivery checkpoint

Tested implementation/evidence commit:
46081c719006a43bf1259e4de84cbd154e598090.
Draft PR: https://github.com/abitiyoso/elenio-ai-workspaces/pull/8
(base feat/elenio-solution-delivery). The branch tip also includes this delivery
metadata; compare git rev-parse HEAD and the remote feature ref for its final SHA.

Owner abitiyoso and private origin verified. Staged scan: zero findings.
Full history through implementation: 25 commits / 8.04 MB; same nine reviewed
original non-secret matches, zero outside original source ancestry, no new
findings or exclusions. Exact credential absent from changed tracked files and
frontend build; original brand assets unchanged. OpenAPI YAML parses with all
250 local refs resolved; changed-package Go vet passes. Main remains
8bfae869e8ad3adaf10b496428ffbcf22c196db9, upstream push DISABLED.
No main merge or deployment; whole master prompt and installation gates remain open.
