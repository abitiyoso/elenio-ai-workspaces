# Live capability / acceptance matrix

Statuses reflect this session only. Source implementation is not a live deployment claim.

| Capability | Status | Evidence / owner | Acceptance |
|---|---|---|---|
| Existing Flowcase UI / APIs | existing-tested (frontend) | 44 baseline tests, API/Designer/graph tests; Flowcase | GIT-01, FLOW-01 |
| OIDC/workspaces/token exchange | existing-unverified | CP config + API; Control Plane | SEC-01 |
| Agent bounds/evals/releases | existing-unverified | Agent runtime/model contracts; Agent Studio | AI-01–03 |
| Cases / independent approvals | existing-unverified | Flowcase + UCW source | APP-01, SEC-03 |
| KMS PDF/ACL/publication/retrieval | existing-unverified | KMS OpenAPI; KMS | KMS-01 |
| Shared design primitives / gallery | enhancement, in progress | packages/ui + Flowcase platform entry; UI | UX-01,05 |
| Home/project context | new fixture, in progress | explicit isolated preview store; UI / proposed CP | UX-02 |
| Branching editor / inspector | enhancement, in progress | canonical next/config adapter, ELK + React Flow; Flowcase UI | UX-03, FLOW-01 |
| Knowledge explorer | enhancement, fixture in progress | explicit synthetic documents, no real ingestion; KMS UI | UX-04 |
| General AI Architect | blocked integration | no configured model / generic builder contract; future orchestration | AI-01 |
| Runtime graphs/telemetry | existing-unverified | real events required; Flowcase/Agent | FLOW-04 |
| Economics ledger / scenarios | new, deferred | kit AI_ECONOMICS; proposed economics owner | ECON-01–03 |
| Portable delivery | new, deferred | dependency and packaging ADR; release owner | DEP-01,02 |
