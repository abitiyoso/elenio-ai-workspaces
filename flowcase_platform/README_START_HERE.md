# Flowcase — Workflow & Case Platform implementation package

This package extends an already-built KMS with a **reusable Case & Workflow Platform** for the SIM B2B tender use case and other business processes. The term **Flowcase** is a replaceable product codename.

## Package contents

| File | Use |
|---|---|
| `Flowcase_Workflow_Platform.html` | **Interactive UI prototype**, self-contained HTML/CSS/JS; open in browser. No login, backend or real AI. |
| `WORKFLOW_CASE_PLATFORM_TECHNICAL_PRD.md` | Technical product scope, domain boundaries, architecture, model, APIs, auth, runtime semantics and UAT. |
| `CODEX_MASTER_PROMPT.md` | Implementation instructions for coding agents plus **phased prompts**. |
| `samples/sim_tender_v1.json` | SIM G0–G5 and 6-agent version-pinned workflow **illustrative configuration**. |
| `samples/poc_request_v1.json` | Non-SIM example proving engine reusability. |
| `samples/sim_tender_v2.json` | SIM v2: same steps as v1 plus optional case brief inputs (summary, customer name, RFP excerpt, constraints) forwarded to agents. |
| `samples/sim_tender_v3.json` | SIM v3: v2 with `author_review` on all six agent steps — agent drafts wait for the case owner's review (submit or request revision) before reaching the gate. |
| `samples/sim_tender_v4.json` | SIM workflow v4: opportunity/customer IDs optional and hidden (`x-hidden`) until a CRM exists. |
| `samples/sim_tender_v5_draft.json` | Illustrative changed SIM workflow version (security review draft); active v1–v4 cases must stay pinned. |
| `contracts/WORKFLOW_DEFINITION_SCHEMA.json` | Draft JSON Schema; requires semantic graph validation in backend. |
| `UI_DESIGN_NOTES.md` | Visual, UX and simulated interaction notes. |

## Fast start

1. Open `Flowcase_Workflow_Platform.html` locally to inspect the interactive design.
2. Give **all files** to Codex / coding agent.
3. Start with the **Phase 0** prompt from `CODEX_MASTER_PROMPT.md`; check implementation and tests before Phase 1.
4. Confirm your parent app identity/wrapper contract, KMS retrieval API, and shared AI runner contract.
5. Never deploy mock users, mock approvals or browser localStorage execution logic in production.

## Architecture decisions already assumed

- Go API + durable worker, PostgreSQL, React/TypeScript/Vite frontend.
- External **KMS** is already built. It owns PostgreSQL/Elasticsearch/Docling/embeddings for knowledge. **Flowcase does not deploy another search engine/Docling instance and does not use pgvector.**
- External shared AI runtime/agent registry via interface, six SIM specialist agents as definitions, not microservices.
- Auth from host wrapper with verified identity; per-resource authorization enforced in Flowcase.
- Versioned immutable workflow definitions, pinned cases, conditional/parallel routes, human approvals, event audit and idempotency.

## Source caveat

SIM requirements and G0–G5 / agent naming come from the uploaded **SIM B2B Solution Workspace PRD v3.1**. Generalized workflow platform engineering choices are **new proposals**, not binding facts from the source. Organizational roles and gate authority require validation.
