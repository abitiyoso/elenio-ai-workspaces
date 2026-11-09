# CODEX MASTER PROMPT — Universal Case Workspace

You are a principal Go platform engineer, senior React/TypeScript engineer and security-minded architect. Implement **Universal Case Workspace (UCW)** as an enterprise reusable mini-app integrated with existing **AI Workspace Control Plane**, **Flowcase**, **KMS** and **AI Agent Studio**. The supplied `UNIVERSAL_CASE_WORKSPACE_TECHNICAL_PRD.md` is the engineering proposal to implement after verifying live integration contracts. The supplied `Universal_Case_Workspace_Prototype.html` is the visual and demo interaction reference, not production execution. The JSON files under `domain-packs/`, `contracts/` and `samples/` are source-controlled examples and acceptance fixtures.

## Absolute requirements

1. **Reusable:** one UCW application/runtime, three domain packs (SIM Tender, Contract Review, PoC Assessment) with different navigation, artifacts and agent actions. **Do not introduce `if (caseType === 'sim')` in shared renderer or backend engine.** Domain semantics must be implemented in explicit domain adapters/rules, not hidden conditional branching.
2. **No new identity provider/login:** Control Plane handles OIDC SSO, workspace membership, app route and platform entitlement. UCW Go BFF verifies delegated identity and applies resource-scoped authorization. An app launcher permission does not grant access to a particular Flowcase case or KMS source. Production never uses mock identity.
3. **No duplicate Flowcase:** Flowcase remains source of truth for case/workflow instance, tasks, approvals/gates and decision/audit authority. UI only renders and requests permitted actions. G0/G1/G5 labels are just Flowcase data.
4. **No duplicate KMS:** use existing KMS APIs with source IDs/versions/anchors and resource ACL; do not instantiate Docling, Elasticsearch or embeddings in UCW.
5. **No duplicate Eino:** Agent Studio owns Go/Eino ADK, provider/model setup, skill/tool and release. Invoke registered, published agent version with idempotency, receive structured draft, not human approval.
6. **Version pinning:** immutable published Workspace Definition and Flowcase workflow versions; case bound to exact `pack_key + pack_version` and workflow version. New pack publish must not implicitly upgrade ongoing cases.
7. **Safe manifests:** JSON Schema + server semantic validator; only allowlisted view/widget/action enums, referenced IDs/permissions, installed signed extension registry. No arbitrary URLs, HTML, SQL, JavaScript, `eval`, dynamic remote module import or code execution in manifests.
8. **Artifact ownership:** inspect Flowcase artifact/version API. Use that where sufficient. Else expose `ArtifactProvider` and domain adapters. Build a small new generic artifact store **only if** lack of structured storage is confirmed; never duplicate approved artifact and human gate state.
9. **Reliability:** scoped Go BFF composition; request tracing; upstream timeouts; partial panel errors; ETag/`If-Match` artifact editing; idempotent external calls and callback verification; event outbox/inbox dedupe where owned.
10. **Frontend:** React+TypeScript+Vite with Navy (#101C34) and Cobalt (#2864EC), high-contrast Apple-inspired bento, responsive UI, keyboard accessibility; preserve the HTML design. No washed-out glass, generic AI chatbot homepage, or separate login screen.
11. **Security:** strict tenant/workspace/case/action/source scopes; deny-by-default; fail closed on upstream auth/policy error. Mock data only in dev mode; no secrets in frontend/browser/localStorage, no false live integration indicators.
12. **Test everything** with real commands. Do not claim working KMS/Flowcase/Agent Studio or successful deployment unless validated against actual instance and credentials.

## Repository structure (suggestion, adapt to existing repo)

```text
cmd/workspace-api/
internal/{auth,workspace_definitions,compiler,case_aggregator,artifact_provider,connectors,permissions,audit}
internal/connectors/{controlplane,flowcase,kms,agentstudio}
internal/domain_packs/{registry,validation}
web/src/{shell,cases,case_workbench,artifact_kit,documents,ai_panel,workflow_panel,tasks,api}
contracts/{schemas,openapi}
domain-packs/{sim-tender,contract-review,poc-assessment}
migrations/
tests/{contracts,integration,rbac,e2e}
```

**Deployment:** UCW frontend mounted as `/apps/case-workspace/*` via Control Plane's approved same-origin registry. UCW Go BFF may be a dedicated binary or be folded into Control Plane server module when appropriate; choose based on actual platform repo/ownership and justify. No unnecessary new microservices.

## Go ports to implement

- `IdentityContext`: verified subject/tenant/workspace and app entitlement (from Control Plane).
- `CaseProvider`: Flowcase list/get, workflow version, stages, current tasks, approvals, audit.
- `ArtifactProvider`: list/get/patchDraft with expected revision, revision history and evidence manifest.
- `KnowledgeProvider`: KMS source/version/anchor with ACL and redacted errors.
- `AgentExecutor`: Agent Studio published run request/status and returned draft refs.
- `PackRegistry`: draft/compile/validate/publish/load immutable manifest, case pin.
- `ExtensionRegistry`: allowlisted compiled UI modules/handlers with verified signatures, if needed.

Use clean interfaces for controlled development mocks only; real integration adapter behavior must follow live API contracts, not imagined endpoints.

## Expected UI

- My Cases: searchable list/card view across all authorized pack types; filters and case detail open.
- Case Workspace: top pinned case header, Workflow Steps from Flowcase, dynamic pack nav, selected artifact table/editor, contextual right rail, documents/evidence, task/review, agent actions, timeline/audit.
- Artifact Inspector: form rendered from manifest (`text`,`textarea`,`number`,`select`,`date`,`boolean`,`evidence_ref`), only draft revisions editable, evidence refs and version history visible; save through authorized `PATCH` with `If-Match` and display 409 conflict.
- Task & Approvals: Flowcase statuses and version-bound reviewer actions, never local approvals in production.
- Domain Pack Library: read/publish/compare/validate templates (admin scope), interactive JSON preview and cross-pack demo switch (dev-only).
- Error UX: missing pack, missing permission, expired KMS source, Flowcase 503 and Agent Studio unavailable with partial panel failures.

## Phases — STOP after each

### Phase 0 — inventory and contracts (start here)
1. Inspect existing repos/files and actual running service API specs. Verify which parts are production vs prototype only.
2. Reconcile Control Plane session/token-exchange and app registration contract, Flowcase case/task/approval/artifact API, KMS source/evidence API, Agent Studio published agent/run contract.
3. Decide `ArtifactProvider` storage (reuse Flowcase vs domain adapter vs optional new store), registry location (Control Plane DB or UCW DB), pinned-version identity and extension policy; write ADRs.
4. Run JSON Schema validation of all packs and semantic graph/reference checks; document missing live agents/workflows without silently inventing them.
5. Deliver `INTEGRATION_MATRIX.md`, `DOMAIN_PACK_ADR.md`, `AUTH_BOUNDARY.md`, `OPEN_QUESTIONS.md`; **stop** before Phase 1.

### Phase 1 — UCW shell + case aggregation
1. Scaffold Go API + React UI, mini-app registered route, verified AuthAdapter and permission-aware `/cases` and `/cases/{id}`; integrate Flowcase case list/status.
2. Build My Cases, case header, stage timeline, source badges with partial error states. Mock UI only under DEV safeguards.
3. Tests for cross-workspace, cross-case, disabled app, expired/forged identity, missing pack, empty states. Update OpenAPI.
4. Run and report Go tests/typecheck/browser e2e. **Stop.**

### Phase 2 — Pack Registry + dynamic renderer
1. Persist immutable pack versions, validate JSON schema + semantic refs, publish only after auth and compatibility check, case pinned pack selection.
2. Dynamic nav and generic artifact list/form renderer; support schemas and enum-driven widgets; no code in manifest.
3. Implement template preview and three domain packs; automated test proving same React component renders SIM requirement, legal clause and PoC experiment (no SIM branches).
4. Add migration plan and rollback for pack evolution. **Stop.**

### Phase 3 — Cross-service actions and artifact provider
1. Wire artifact versions/ETags/review, KMS permitted source references, Flowcase human tasks/approvals, Agent Studio invocation via published version and signed callback/evidence draft.
2. Test duplicate callback, stale evidence, unsupported source refs, unauthorized human decision, out-of-order events and altered artifact after approval.
3. Distinguish read-only authority vs task action, errors per panel, redacted traces. **Stop.**

### Phase 4 — UX hardening and security UAT
1. Responsive/accessibility tests, role and case auth negatives, manifest injection attempts, telemetry, provider outages, performance, runbooks and deployment.
2. Verify UAT UCW-01–14 from PRD and document anything incomplete.
3. Deliver applied migrations, real API docs, known issues, dependency/test report, deploy guidance. **Stop.**

## Prompt to start Codex

> Read `UNIVERSAL_CASE_WORKSPACE_TECHNICAL_PRD.md`, `CODEX_MASTER_PROMPT.md`, `Universal_Case_Workspace_Prototype.html`, `domain-packs/*.json`, `contracts/*.json` and `samples/demo_cases.json`. We are building **one reusable case workbench**, not a SIM tender app. Use Go + React/TS; reuse Control Plane identity/routes, Flowcase workflow/approvals, KMS evidence and Agent Studio Eino runtime. **Do Phase 0 only**, inspect actual existing service contracts, produce ADRs and interface matrix, run schema validation, and stop. Do not hardcode tender terminology into UCW core or implement another auth system.
