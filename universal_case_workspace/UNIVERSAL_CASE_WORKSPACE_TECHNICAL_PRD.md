# Universal Case Workspace — Technical Product Requirements Document

**Document:** UCW-PRD-001  
**Version:** 1.0 (engineering design proposal)  
**Date:** 31 October 2026  
**Status:** For implementation planning and security / cross-service contract approval  
**Parent platform:** AI Workspace Control Plane  
**Existing cores:** KMS · Flowcase · AI Agent Studio (Go/Eino ADK)  
**First packs:** SIM Tender, Contract Review, PoC Assessment.

## 0. Executive decision

Build **Universal Case Workspace (UCW)** as the **reusable experience layer**, *not* as a fourth business engine and not as a domain-specific SIM Tender app. The Control Plane owns login/session, app routing, workspace membership and platform entitlements. Flowcase owns canonical case, workflow runtime, task and approval decisions, audit and artifact registry *if already supported*. KMS owns documents, Docling, Elasticsearch, access-controlled evidence. Agent Studio owns agent/model/skill configuration, Eino execution and run traces.

**UCW owns:** workspace definition registry and rendering, case-context composition, generic artifact editor/table/inspector, evidence drawer, AI action panel, cross-service view models, UI state and small integration adapters. Only add a UCW artifact data service if deployed Flowcase/artifact providers cannot persist structured domain artifacts. No cross-service DB access. Per-domain rules and typed artifacts live in **Domain Packs** / domain adapters.

**MVP approach:** Go **Workspace BFF** (can initially be a module inside existing Control Plane or a separately deployed Go service if scaling/security requires), React + TypeScript + Vite mini-app mounted at `/apps/case-workspace/*`, PostgreSQL only for UCW-owned manifests/config/cache pointers if needed. Avoid creating a fourth database of users, cases or human approvals. Use validated, immutable JSON Domain Pack manifests; a case pins a published pack version. Keep specialized React extension slots possible, not arbitrary JavaScript in manifests.

### Non-negotiable outcome

A **SIM Tender** case, a **Contract Review** case, and a **PoC Assessment** case use the same React/Go application. Changing a published domain pack version alters the experience for *new cases only* by default; pinned cases and approved artifact revisions remain untouched. No conditional code like `if case.type === "sim-tender" ...` inside generic shell or engine.

## 1. Product scope and jobs to be done

Users open one app launcher entry **Case Workspace** and see only cases they are allowed to access. When they open a case, UCW resolves:
1. signed session/workspace identity from Control Plane;
2. authorized Flowcase case and **pinned workflow definition** version;
3. **published, version-pinned Workspace Definition** matching that case's domain type;
4. navigation/modules/artifact schemas/actions filtered by **server-issued effective permissions**;
5. artifact revisions from the authoritative domain artifact provider or Flowcase registry;
6. source references, citations and permitted document details from KMS;
7. run status and allowed AI actions from Agent Studio;
8. tasks, human decisions and timeline from Flowcase.

### In MVP
- Home / **My Cases** with filters, statuses, owner, progress, due date, domain template and recent activity.
- **Case Workspace** with pinned case header, workflow stage strip, dynamic domain navigation, dynamic artifact table/editor and right-side evidence/review context.
- **Generic Artifact Kit**: typed schemas, row list, details, revisions, optimistic edits, source provenance, JSON Schema-based validation, reviewer state. No direct materialization of untrusted HTML.
- **Tasks & Approvals**: generic Flowcase-fed inbox, step status, reviewer policy, version-bound decision request; no backend self-approval logic.
- **Document/Evidence** viewer: KMS-backed metadata, citation anchors, source validity, ACL, page deep-links where verified.
- **AI Activity/Actions**: show domain allowlisted agent actions; launch only published agent versions through Flowcase/Agent Studio integration, bind outputs to drafts.
- **Workspace Definition Registry**: list/validate/release immutable templates with permission-gated admin tools; display working JSON/structural validation. An editable visual composer is P1.
- **Audit/Activity**: correlated events and pointers; no invented cross-service ordering.
- Responsive, accessible design, empty/loading/error/conflict states, appropriate localization and case timezone labels.

### Out of scope
- A new login/password engine, user/role CRUD or company SSO screen (Control Plane).
- New durable business workflow engine or gate-policy authoring (Flowcase).
- New LLM gateway, skill editor, agent release or Eino runtime (Agent Studio).
- New document parser, Elasticsearch index or source repository (KMS).
- Universal editable arbitrary SQL/code/plugins or unvalidated form-driven policy execution.
- Hardcoded SIM compliance/calculation policy in UCW. These are Domain Pack + backend domain rule adapter concerns.
- Case-independent organization CRM, customer bid pricing, proposal export logic or legal signature authority.

## 2. User personas

| Persona | Allowed experiences | Explicit restrictions |
|---|---|---|
| Case participant | Cases, assigned tasks, permitted artifact/source views, comments | No automatic edit/approve access |
| Case editor | Edit an authorized draft artifact schema, request reviewer task | Cannot change published pack, bypass Flowcase, alter prior immutable revision |
| SME/reviewer | Review versions on assigned Flowcase tasks, comment/attest/decide only when authorized | No arbitrary self-approval, case permission still checked |
| Domain pack maintainer | Author new draft manifest, map schemas/actions/allowed extensions, test on fixture cases | Cannot publish without platform policy or add arbitrary endpoints |
| Platform publisher | Validate/publish/rollback pack with audit and scope | No implicit access to private case documents |
| AI operator | See allowed run/status/trace summaries | Cannot approve business artifact simply because agent succeeded |

**Security source of truth:** Control Plane establishes identity, workspace membership and app launch entitlement. Each downstream backend verifies its **own resource/action** permissions. UCW filters view affordances using server permissions, but never enforces security solely through hidden UI controls.

## 3. Four-layer architecture

```text
Browser / Control Plane App Shell (OIDC login + server session)
   │ same-origin app launch at /apps/case-workspace/*
   ▼
Universal Case Workspace (React / TypeScript / Vite)
   - My Cases, case header, dynamic nav, schema forms/tables, evidence & activity drawers
   │ authenticated same-origin BFF calls (no browser-carried provider secrets)
   ▼
UCW Go BFF / Composition + Workspace Definition Registry
   - resolve case, pinned pack, permissions and display model
   - adapters, ETags, caching, audit/correlation, no business approvals
   ├── Control Plane: verified session/workspace and mini-app entitlements
   ├── Flowcase: canonical cases, workflow/stage, human tasks/approvals, artifact registry
   ├── KMS: sources, exact version/citation, secure source read
   ├── Agent Studio: available published agents, execution status, invocation bridge
   └── Domain data provider(s): requirement, clause, experiment and specialized rules
```

### Service boundaries and ownership

| Entity / capability | Authority | UCW behavior |
|---|---|---|
| User, workspace memberships, launch grants | Control Plane | Consume verified `/me` / delegated service claims; never create users |
| Case and workflow instance, current stage, tasks/approvals | Flowcase | Read, render, submit permitted actions only through Flowcase APIs |
| Published workflow definition version | Flowcase | Pin case and display workflow stages; never duplicate graph execution |
| Workspace Definition and its published version | UCW/Control Plane registry | Validate, publish with audit; pin to case |
| Knowledge source bytes, Docling output, citations, ACL | KMS | Read by resource-scoped API, never index from UCW |
| Agent identity, skill/version/tool/model and run | Agent Studio | Display/run through allowed action contracts; no direct LLM key setup |
| Structured domain artifact revision | Flowcase artifact provider if supported; otherwise domain adapter or new generic artifact store | Render via typed contract; authoritative writes in owner service; no hidden dual-write |
| SIM tender bid strategies/G0–G5 meaning, legal exception rules, PoC assessment thresholds | Domain Packs + domain services and Flowcase policy | Render declared modules and accepted fields; never hardcode domain logic in shell |

**Key unresolved architecture check:** Does the deployed Flowcase already support arbitrary JSON artifact versions + source refs + ETags? If yes, prefer its artifact repository. If not, define one **ArtifactProvider** adapter per domain; optionally build a small generic Workspace Artifact Store with immutable revisions. The choice is explicit and tested—not implied by an HTML demo.

## 4. Workspace Definition manifest

Each pack is versioned JSON validated against `contracts/workspace_definition.schema.json`, then checked by a server-side **semantic compiler/validator**. The manifest is an authoritative presentation/config description, **not executable code** and not a source of permission grants.

Required fields:
- `schema_version`, `pack_key` (e.g. `sim.tender`), immutable semantic `version`, `status` draft/published/retired;
- `bindings.flowcase_workflow_key`, `artifact_provider`, optional `agent_keys`, KMS scope **policy reference**, not source IDs;
- `navigation` list of `view` kinds from a supported registry (`overview`, `artifact_list`, `tasks`, `approvals`, `documents`, `ai_activity`, `timeline`, `extension`);
- `artifact_types` typed field declarations (`text`, `textarea`, `select`, `number`, `date`, `boolean`, `evidence_ref`) with required/read-only validation and input constraints;
- `actions`: allowlisted executor categories `agent_invocation`, `flowcase_action`, `artifact_command`, `registered_extension` with `required_permission` and optionally scoped output artifact type;
- optional `brand` used only for safe icons/accent tokens.

Semantic checks **beyond JSON Schema**:
1. All navigation `artifact_type` references exist; keys and action IDs unique; no hidden collisions.
2. Every agent action refers to a registered, *allowlisted and published* Agent Studio identity and permitted output schema.
3. All `required_permission` values must exist in the central entitlement/action vocabulary. A manifest can require permission, **never grant** it.
4. Flowcase workflow binding must be an allowed reference with compatible case type; no arbitrary workflow instance mutation or gate bypass.
5. `extension_ref` must match a compile-time installed signed frontend/server extension ID; no JS snippets, SQL, or arbitrary HTTP URLs.
6. Definition publish creates checksum/signature, immutable version and optional route compatibility report. Cases pin exact pack version and workflow version **independently**.
7. Schema evolution on fields must declare migration/compatibility, or create a new major version. Old case artifacts keep old schema validators/readers.

**Fallback:** Unknown/missing pack version → safe **read-only generic case shell** with permitted Flowcase header/tasks and visible `pack_unavailable` warning, not silent fallback to latest. Missing permissions → filter or disable action, API also returns 403.

## 5. UI: reusable experience specification

### A. My Cases
- Group and filter by domain, workspace, status, owner, due date.
- Case tiles/table are generated from Flowcase records and attached pack metadata. No tenant-wide cross-case browsing.
- Pack selector in preview is **illustrative**; actual users see only permitted cases.

### B. Case Workspace / case header
- Pinned case ID, entity/customer, owner, due, stage/readiness, risk, workflow/version and workspace-pack/version.
- Center: dynamically selected module. Default `overview` shows workflow steps, assignments, blockers, artifact summary.
- Right rail: contextual review, AI run state, source citations, latest decisions and gaps.
- Mobile: collapse right rail to drawer; tabs/nav remain keyboard accessible.

### C. Schema-driven Artifact Kit
- Table columns inferred from safe field registry (first few configured fields), status, revision and owner.
- Clicking a row opens reusable **artifact inspector** with typed form fields and source evidence references.
- Save creates new immutable revision via provider using `If-Match`/ETag. Supports validation errors and conflict (409).
- AI draft output is visually distinct and cannot self-promote to `Human verified`/`Approved`.
- The generic UI can display assessment status, but domain-specific compliance validity remains backend rule in a domain adapter.

### D. Flowcase task/approval widgets
- Inbox fetched from Flowcase, not locally inferred. Approval button only when Flowcase reports **authorized action** and expected artifact revision.
- Decision event binds content hash, actor and policy reference, and may reopen on upstream changes.
- Every workspace template displays **generic** step names from Flowcase (G1 is just a configured stage label).

### E. Evidence & AI widgets
- Evidence opens KMS source anchor with ACL/security; display title, version, page, status; no guessed link or expired source as verified.
- AI panel lists actions in pack manifest after intersecting with permission and registered agent/skill version; tool calling and LLM keys remain in Agent Studio.
- Agent result is a new **draft** artifact requiring human action.

### F. Template Studio (MVP read/validate, P1 visual builder)
- Inspect/publish versions and compare changes, preview manifest against test case fixture.
- For MVP, author JSON through controlled admin workflow; no drag/drop dependency needed.
- Show validation errors with JSON pointer paths and require server-side validation and audit.

## 6. API (proposed BFF contracts)

Base `/api/v1/workspace` under verified same-origin Control Plane route. Treat endpoints as proposals until aligned to installed services.

| Method | Route | Purpose |
|---|---|---|
| GET | `/me/context` | Verified workspace, principal, platform app launch and roles (no secrets) |
| GET | `/cases` | Entitlement-filtered, paginated Flowcase case summaries + pack labels |
| GET | `/cases/{id}` | Aggregate case context, pinned workflow + pack version, permissions |
| GET | `/cases/{id}/navigation` | Compiled manifest/nav after permission intersection |
| GET | `/cases/{id}/artifacts?type=` | Generic artifact list (from registered `ArtifactProvider`) |
| GET | `/cases/{id}/artifacts/{artifactId}` | Typed metadata, revision, ETag and evidence refs |
| PATCH | `/cases/{id}/artifacts/{artifactId}` | Validated draft edit; requires `If-Match`; returns immutable new revision |
| GET | `/cases/{id}/documents` | KMS metadata and verified source refs |
| GET | `/cases/{id}/tasks` | Flowcase assigned/visible tasks |
| POST | `/cases/{id}/tasks/{taskId}/actions` | Forward allowlisted Flowcase task actions with proper authority |
| GET | `/cases/{id}/approvals` | Flowcase approvals, policy and version binding |
| POST | `/cases/{id}/approvals/{id}/decisions` | Verified Flowcase decision only; UCW cannot fabricate success |
| GET | `/cases/{id}/agent-actions` | Intersect pack actions with Agent Studio registries/permissions |
| POST | `/cases/{id}/agent-actions/{key}/runs` | Authorized, pinned agent invoke; async 202 + run ref |
| GET | `/cases/{id}/activity` | Correlated event stream, each stamped with authoritative source |
| GET | `/packs` | Allowed published pack metadata, with admin drafts as scoped |
| GET/POST | `/admin/packs` | Draft list/create version; publisher-only writes |
| POST | `/admin/packs/{key}/versions/{v}/validate` | JSON Schema + semantic validator |
| POST | `/admin/packs/{key}/versions/{v}/publish` | Immutable publish with compatibility check, audit and checksum |

**Failure modes:** distinguish unauthorized (401), forbidden (403), not found masked (404), optimistic conflict (409), invalid manifest/data (422), upstream unavailable (503), run accepted (202). Return `{code, message, trace_id, details?}` without leaking cross-tenant existence. No CORS wildcards. Server applies context, token audience and resource ACL, **never trusts tenant/workspace or role from JSON body**.

## 7. Event and artifact integration contracts

Reference files:
- `contracts/workspace_definition.schema.json`
- `contracts/case_workspace_context.schema.json`
- `contracts/artifact_record.schema.json`
- `contracts/agent_action_request.schema.json`
- `contracts/workspace_events.schema.json`

**Case context resolution** must include `pack_key`, exact `pack_version`, `flowcase_case_id`, `workflow_version`, `permitted_actions`, `tenant/workspace` from trusted backend and `etag` for stale state.

**ArtifactProvider** exposes `list(case,type)`, `get(case,id,rev)`, `updateDraft(case,id,expected_rev,patch)`, `listRevisions`, `evidenceRefs`. This is a port; implement adapters for Flowcase or domain service. Cannot use arbitrary HTTP endpoint from manifest.

**Events:** consume `CaseCreated`, `CaseDefinitionPinned`, `ArtifactDraftCreated`, `ArtifactRevisionCreated`, `SourceInvalidated`, `AgentRunCompleted`, `TaskAssigned`, `ApprovalDecisionRecorded`, `WorkflowVersionMigrated`, `WorkspacePackPublished`. The event producer remains authoritative; use transactional outbox where owned and idempotent inbox dedupe per `(producer,event_id)`. A source update invalidates dependent artifact/review links through Flowcase/domain logic; UI only displays status.

**Version pinning**: `case_id + workflow_definition_version + workspace_pack_version + artifact_schema_version + agent_version` are distinct pins. Pack upgrades never rewrite case artifacts or workflow instances implicitly. Migration requires a named, human-approved plan and robust rollback.

## 8. Security and controls

- Same-origin mini-app launched through Control Plane. Use BFF verified session + aud-restricted per-service token exchange, or approved trusted gateway. Never trust browser-forged `X-User-ID`, iframe `postMessage`, query parameters or localStorage identity.
- RBAC and ABAC intersection: tenant, workspace, case, artifact classification, action, source ACL, reviewer delegation. UCW cannot compute the sole authoritative grant decision from a manifest.
- KMS retrieval rechecks ACL; no direct Elasticsearch/Docling or link to unsecured blob object.
- Agent Studio rechecks permitted agent/model/tool; no passing credentials via UCW; prompt injection content never registers tools or calls host service APIs.
- Hostile manifest protection: JSON Schema and semantic validator, no raw HTML/JS code, no external arbitrary extension URLs, no embedded SQL, no client-provided upstream target.
- Secrets: only server-side vault refs. Audit reads/writes, revision publish, run invoke, and approval attempts with redacted payloads.
- CSRF protection on session writes, clickjacking / CSP, XSS-safe renderer and URL allowlists; avoid embedding untrusted documents directly into the shell without appropriate sandbox/trust boundary.
- Failure isolation: KMS down → show document panel unavailable, don't treat evidence as valid. Agent Studio down → disable run actions with warning. Flowcase down → read-only/error for decision actions. Never fallback to local approvals.
- Service runbooks for cache invalidation, version rollback, auth revocation, rate-limits, pagination, worker idempotency and source expiry.

## 9. Reliability and observability

- No fan-out from browser; BFF aggregates with per-adapter deadlines, circuit-breakers, bulkheads and errors per panel.
- Prefer `stale-while-revalidate` only for **read-only** non-sensitive lists with scope-partitioned cache keys; never cache permission grants longer than revocation policy.
- End-to-end correlation IDs, source service name and last-updated time. Screens label data as partial when upstream unavailable.
- Use `If-Match` ETag for artifact edit, task/approval decision and pack publish where supported, plus idempotency key on mutations.
- Track p95 case load, API failure by dependency, effective deny rate, pack validation failures, duplicate event rate, artifact conflict rate and reviewer completion time. Agree pilot SLO with owners; do not invent metrics.

## 10. Database recommendation

Start **no new UCW database if Control Plane can host pack registry**; reuse its authenticated Postgres and controlled migration conventions. UCW-specific logical records only if necessary:
- `workspace_packs(id, tenant_scope, pack_key, owner_team, status)`
- `workspace_pack_versions(id, pack_id, semver, manifest_json, schema_version, checksum, state, published_at, published_by)`
- `workspace_case_bindings(case_id, tenant_id, pack_version_id, bound_at, bound_by)` — only if Flowcase doesn't store pack version reference.
- `workspace_extension_registry(id, extension_key, signed_build_ref, required_permissions)`
- `workspace_audit_events(id, actor_ref, action, resource_ref, correlation_id, redacted_json, at)`

**If new generic Artifact Store is required**, add `artifact_records`, append-only `artifact_revisions` with schema+source refs+hash+ETag, and outbox. Never use new JSON tables as a shadow Flowcase approval store. Domain services own specialized validation.

## 11. Acceptance / UAT scenarios

| ID | Scenario | Pass criterion |
|---|---|---|
| UCW-01 | Load three different domain cases | Same frontend/backend code; nav and artifact fields differ from published pack JSON. No SIM branch in renderer. |
| UCW-02 | Case version pin | Publishing SIM pack v1.1 does not change existing `SIM-2026-042` pinned v1.0 automatically. |
| UCW-03 | Permission | User with only platform `case-workspace:launch` but no Flowcase case membership cannot retrieve a case by guessed ID. |
| UCW-04 | Artifact edit | Human draft save creates vNext, previous revision remains and ETag conflict produces 409. |
| UCW-05 | AI safety | AI run produces draft artifact and trace; no approval or compliance promotion. |
| UCW-06 | Approval | Clicking approve without delegated Flowcase task returns 403; UCW must not mark local approval. |
| UCW-07 | Evidence | Revoked/expired KMS source is inaccessible/flagged, material claim can't be shown as grounded. |
| UCW-08 | Pack security | Manifest with external JS URL, raw SQL, unknown action executor, or invalid artifact type is rejected. |
| UCW-09 | Workflow | Different Flowcase stages render without hardcoded G0/G1 checks; review state from Flowcase. |
| UCW-10 | Fault tolerance | KMS 503 displays partial panel error; Flowcase decision remains blocked, other panels may render. |
| UCW-11 | Mobile & a11y | Responsive no horizontal page overflow, keyboard nav, visible focus, screen reader labels. |
| UCW-12 | Extension | Registered signed module renders only after policy check; unknown extension cannot execute. |
| UCW-13 | Source change | Source revision invalidation marks impacted artifacts/reviews stale via owning service and UI shows version-bound banner. |
| UCW-14 | Audit | Pack publish, artifact changes and AI invocation carry correlation and correct actor/provider refs. |

## 12. Delivery plan

- **Phase 0: Contracts/ADR.** Inspect installed Control Plane / Flowcase / KMS / Agent Studio, verify artifact capability, auth audience, registry location and three pack samples. Formalize actor/permissions and approved agent run bridge.
- **Phase 1: Shell + case aggregation.** Register mini-app, mount React into Control Plane, Go BFF verifies session; `/cases`, `/cases/{id}` and case cards with pinned pack and Flowcase stages.
- **Phase 2: Domain Pack registry and dynamic UI.** JSON schema/semantic validator, immutable publish, dynamic nav, artifact list/form renderer; three different domains pass UAT-01.
- **Phase 3: Work controls.** Artifact provider with ETag/revisions, KMS evidence, Flowcase task/approval actions, Agent Studio invocation/read-only trace. Security denial and duplicate callback tests.
- **Phase 4: Hardening.** cross-tenant tests, inaccessible case, audit, faults, accessibility, observability and operations/runbooks. Domain extensions for real SIM requirements follow, outside generic core.

## 13. Engineering decisions to confirm before release

1. Where to persist Workspace Definition registry: Control Plane Postgres vs UCW Postgres; avoid duplicate registries.
2. Flowcase artifact registry data capabilities: structured JSON, version, draft/approval lifecycle, citations, ETag and import.
3. Exact Control Plane BFF session identity contract and token exchange to Flowcase, KMS, Agent Studio.
4. Tenant/workspace scope semantics, case membership API and redaction rules.
5. Standard artifact schema formats and extension packaging/signing rules.
6. Cross-product UX ownership of task/approval: no duplicate decision engine; display only.
7. How published pack version is pinned at case creation and migrated later.
8. Real KMS citation/Docling source URLs and Agent Studio tool/run output/callback contract.

## 14. Basis and disclaimers

**From the uploaded SIM B2B Solution Workspace PRD v3.1:** case header, switchable artifact canvas, decision/SME rail, on-demand AI contextual actions, mandatory agent/human governance and baseline traceability. **From the Control Plane package:** same-origin app routes, verified IdP/OIDC session, workspace membership, mini-app registry, platform RBAC. **Engineering proposal:** generalizing the shell to three domain packs, BFF orchestration, JSON Schema manifests, extensibility/adapter design and specific endpoints/storage tables. No claim is made that any of these service contracts are live or production-tested.
