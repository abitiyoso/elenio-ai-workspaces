# Case operations checkpoint — 2026-11-19

## ADR 009 — reuse the Flowcase case authority from the shared shell

The owner asked to finish model-independent functions and check their behavior, connecting models last. Add authenticated `/connected/operations` and `/connected/cases/:id` routes to the existing Elenio entry. They provide the operation journey previously covered by API tests but absent from the shared UI. No database/schema/Go API/security rule changes. Original Flowcase and Universal Case Workspace routes remain available; this page is not a replacement customer mini-app or complete Phase 5 acceptance.

Capabilities:

- Server-filtered case search/status, cursor paging and case deep links, with loading/empty/error/forbidden states.
- Intake selects an existing published workflow, loads its exact input contract and runtime capabilities, accepts forms or explicit object JSON, and submits to existing case creation with a stable action Idempotency-Key. Edits reset the key; transport retry retains it. The API selects latest published at creation; the UI explicitly explains that a concurrent publish may change the selected version and displays the actual pinned result. Unsupported/unknown runtime node capabilities lock creation. External dependency readiness remains a server decision; no model run is fabricated.
- Case detail exposes actual tasks, approvals, versioned artifacts/evidence, draft reviews, history and timeline. It links the exact pinned workflow and is accessible from the existing execution view.
- Task completion/revision, case pause/resume/notes, approval approve/reject/return, and draft submit/revision reuse the existing typed API functions with loaded version_num / If-Match. Buttons require explicit boolean server permissions, not browser role selection. Approval dialog includes policy and subject state hash, requires human rationale, and requires explicit confirmation; provisional policies remain labelled.
- Conflicts (409/412) retain the open form and disable submission. Explicit discard/reload does not claim the action succeeded. No automatic mutation retry or silent overwriting. Polling is suspended while the action dialog is open; pending input is held in memory only. No case/evidence content is persisted in browser storage.
- Development identity switch is shown only when the server reports dev_mock AND development, using its dev_personas list. It calls the existing host bridge persona selector; fresh session/tenant/user keys clear identity-dependent UI. Trusted JWT/production sessions never display it, even if a response contains a dev_personas property. No self-approval exemption is configured.

## Verification

Nine browser tests use the real local Flowcase API/worker/PostgreSQL (explicit server development identity; no route interception). The new cases exercise intake from the UI → scope task → no owner approval affordance → server-configured independent reviewer approval → resource task → completed, then search/status filtering and reopen. Server decision actor/hash are checked. Additional UI tests exercise stale task completion (form retained, no resubmit), pause/resume, persistent case note and viewer creation restrictions. Existing six real-service tests remain intact. Screenshots at 1440/768/390 show the actual completed case; desktop list captured. Serious/critical axe checks pass on case detail.

Contract tests separately prove production identity-selector suppression, forbidden case deep-link behavior/no mutation fallback, and versioned draft submit/revision request bodies. Draft fixture output is explicitly labelled contract data. No real model generation/evaluation/revision is claimed.

Frontend unit/component tests remain 66; full browser regression now 28, plus 9 live local cases. Lint, TypeScript and production build pass. The Economics persistence regression test now waits for its real IndexedDB save confirmation before reloading (the previous test could abort a pending write). Source behavior was not masked with fixed sleeps or test skips. See JSON reports for final results.

## Remaining acceptance

Model connection is deliberately last, per owner; actual AI generation/evaluation requires it. Draft review request wiring is contract-tested; new model revisions are not live-tested. KMS document ingestion/search/ACL UI, CP deployment/entitlements and a separate customer operator shell remain governed by their existing services and are not established by these tests. Complete project hierarchy, general workflow economics/ledger/budgets and portable delivery remain outstanding model-independent work. Windows FileVault POSIX permission failure remains unchanged. No main merge, upstream write or deployment.
