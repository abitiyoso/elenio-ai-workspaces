# Live capability / acceptance matrix

Statuses reflect this session only. Source implementation is not a live deployment claim.

| Capability | Status | Evidence / owner | Acceptance |
|---|---|---|---|
| Existing Flowcase UI / APIs | existing-tested (frontend + non-DB backend) | 44 baseline / 52 final frontend tests, 59 non-DB Go tests; Flowcase | GIT-01, FLOW-01 |
| OIDC/workspaces/token exchange | existing-tested (unit), DB unverified | 17 Go + 17 UI tests; CP config + API; Control Plane | SEC-01 |
| Agent bounds/evals/releases | existing-tested (partial), Windows vault blocked | 65 Go pass / 1 vault failure; Agent runtime/model contracts; Agent Studio | AI-01–03 |
| Cases / independent approvals | existing-tested (partial), DB unverified | Flowcase/UCW unit tests; 99 cross-module DB skips; Flowcase + UCW source | APP-01, SEC-03 |
| KMS PDF/ACL/publication/retrieval | existing-tested (unit), DB unverified | 103 Go tests, 4 DB skips; KMS OpenAPI; KMS | KMS-01 |
| Shared design primitives / gallery | enhancement, tested | packages/ui + Flowcase platform entry; UI | UX-01,05 |
| Home/project context | new fixture, tested | explicit isolated preview store; UI / proposed CP | UX-02 |
| Branching editor / inspector | enhancement, tested | canonical next/config adapter, ELK + React Flow; Flowcase UI | UX-03, FLOW-01 |
| Knowledge explorer | enhancement, fixture tested | explicit synthetic documents, no real ingestion; KMS UI | UX-04 |
| General AI Architect | blocked integration | no configured model / generic builder contract; future orchestration | AI-01 |
| Runtime graphs/telemetry | existing-unverified | real events required; Flowcase/Agent | FLOW-04 |
| Economics ledger / scenarios | new, deferred | kit AI_ECONOMICS; proposed economics owner | ECON-01–03 |
| Portable delivery | new, deferred | dependency and packaging ADR; release owner | DEP-01,02 |

## Phase 1 acceptance checkpoint

UX-01/02/03/04/05 and FLOW-01 have fixture-backed browser/unit evidence: 7 E2E, 52 changed-app frontend tests, 25 screenshots. Human visual acceptance remains pending. FLOW-02 path semantics are tested as configuration, not executed payment runs. FLOW-03/04, AI/KMS live governance, SEC live integration, economics ledger and deployment acceptance remain unverified/deferred. Full final results and exact limits: QA_RESULTS.md and STATUS.md.

Owner feedback reopened Phase 1 visual gate. Typography, Home structure/content and contextual Project navigation corrected; full HTML feature parity remains explicitly unclaimed. Architect requirement entry is now a functional fixture page, while model generation remains blocked pending approved configuration.

## Phase 2 contract checkpoint — 2026-11-19

Owner continuation and explicit no-sandbox contract-test authorization supersede the earlier pending-continuation note. This is a tested engineering checkpoint, not full live acceptance.

| Capability | Status | Evidence / authority | Acceptance |
|---|---|---|---|
| Session + workflow project registry/search/cursor | enhancement, contract-tested; live unverified | SessionProvider, existing API client, key/version deep link; Flowcase | UX-02, SEC-01 |
| Blank/template/native import creation | enhancement, contract-tested | POST workflows, stable idempotency key, preview, registered template key | FLOW-01, UX-02 |
| Save/validate/version editing | enhancement, contract-tested | If-Match + 412 retention, immutable published state, server report; Flowcase | FLOW-01, SEC-03 |
| Import/diff + canonical roundtrip | enhancement, tested | Native schema/next/config/policy retention, explicit replacement, identity check | FLOW-01 |
| Capability/agent/skill/connector/policy bindings | enhancement, contract-tested; release readiness unverified | Runtime capabilities + workflow-catalog response, server validation | AI-01, FLOW-01 |
| Scoped visual layout + unsaved navigation | new, browser-tested | Coordinates-only sidecar, reload, browser Back/stay/discard | UX-02/03, SEC-01 |
| Governed Agent/KMS/integration app launch | enhancement, unit contract-tested; deployed launch unverified | CP cookie session and exact registered route; existing app authorities | APP-01, KMS-01 |
| General Architect generation, minimal agents/readiness | blocked configuration/contract | No approved model or generic blueprint API; brief retained, explicit unavailable | AI-01 |
| Many-workflow solution project lifecycle | new, open contract | One-workflow projection delivered, server registry not invented | UX-02 |
| KMS ingest/review/publish/ACL retrieval | existing-unverified live | Existing app retained; no new shell claims of ingestion or retrieval | KMS-01 |
| Arbitrary API/MCP tooling | deferred, assessed separately | No new unapproved execution or egress path | SEC-02 |

Current evidence: 60 unit/component + 22 browser tests. Fifteen browser cases cover service contracts; seven preserve Phase 1 journeys. Axe serious/critical checks cover five local and three connected routes. Full Phase 2 gate remains open for the live/configuration items above; Phase 3 not started. See PHASE2_CORE_ENGINEERING.md for decisions and QA_RESULTS.md for limits.
