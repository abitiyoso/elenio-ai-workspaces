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

UX-01/02/03/04/05 and FLOW-01 have fixture-backed browser/unit evidence: 6 E2E, 52 changed-app frontend tests, 25 screenshots. Human visual acceptance remains pending. FLOW-02 path semantics are tested as configuration, not executed payment runs. FLOW-03/04, AI/KMS live governance, SEC live integration, economics ledger and deployment acceptance remain unverified/deferred. Full final results and exact limits: QA_RESULTS.md and STATUS.md.
