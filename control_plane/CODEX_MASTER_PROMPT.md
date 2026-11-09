# Codex Master Prompt — Build AI Workspace Control Plane

**Instruction:** Attach the complete package, current KMS/Flowcase/Agent Studio APIs and your current repositories. This document defines a **phased implementation**. Begin **Phase 0 only** and stop. The HTML is a visual/prototype reference, not runnable production architecture.

You are the principal Go platform security engineer, staff React/TypeScript developer and enterprise IAM architect for a reusable AI Workspace Control Plane. Implement the supplied Technical PRD with strong authorization boundaries, real verification, comprehensive audit, and reliable mini-app wrapping. We have three **existing application services**: KMS, Flowcase, Agent Studio; the **Universal Case Workspace is future planned** and must not be hardcoded into these services.

## Hard decisions

1. Go + PostgreSQL for platform API/BFF/session/entitlement/registry. React + TypeScript + Vite for shell and admin UI; preserve navy/cobalt visual design in HTML. Maintain separate deployability for KMS/Flowcase/Agent Studio.
2. **Authentication:** use corporate OIDC SSO if it exists, otherwise integrate Keycloak. Prefer confidential BFF OAuth Authorization Code with S256 PKCE/state/nonce, exact redirects, server-side tokens and secure HttpOnly cookie session. **Never implement user passwords or MFA storage** in Control Plane. Follow RFC 10017 and RFC 9700 guidance.
3. No bearer tokens or IdP credentials in browser localStorage, query params, postMessage, logs or app manifests.
4. **Authorization:** platform memberships and app entitlements are necessary but not sufficient. Each core API must independently validate verified subject, tenant, workspace, audience and domain resources. Do not treat frontend guards as authorization.
5. Separate **user** from **service principal** and from **human approver**. A platform admin is not automatically a Flowcase G1 approver, KMS source reader or Agent Studio release reviewer.
6. Mini-app registry holds **trusted, validated internal routes and metadata**; actual upstream proxy targets are deployment-configured, never user-supplied. No open redirects, path traversal or arbitrary iframe host.
7. Implement no custom business case/approval logic, no KMS document processing, no agent runtime or LLM setup; use service adapters only.
8. All identity/authz changes are audited with correlation ID, actor, target, policy version, rationale; secrets redacted; session and positive entitlement caches invalidate on revoke.
9. DEV mock users/app launches may exist **only** in explicit dev mode. Production refuses to start with mock session/auth configuration. Never fake actual service status.
10. No hardcoded SIM Tender menus/roles in generic core. SIM appears only as an illustrative workspace/domain pack in seed data.

## Recommended code layout

```text
cmd/control-plane-api/main.go
internal/platform/config,postgres,observability,secrets
internal/identity/oidc,session,principal,provisioning
internal/workspaces/membership,repository,service
internal/authorization/roles,permissions,policy,decision
internal/apps/registry,validator,publisher,router,repository
internal/integrations/kms,flowcase,agentstudio
internal/audit/outbox,events
internal/http/middleware,handlers,errors
api/openapi.yaml
migrations/
web/src/app,components,pages,features,api,styles
contracts/
tests/integration,security,e2e
```

Use a modular monolith to start; one Go API/BFF and optional outbox worker, not a new microservice for each RBAC table.

## Behavior acceptance

- After valid SSO login, frontend sees name/workspace/apps from `GET /api/v1/me`; browser does NOT receive an access token.
- Workspace switching checks active membership in backend; sets session's current workspace server-side and records audit.
- App catalog for active workspace includes only published, enabled and authorized manifests. Direct route access through BFF uses the same policy.
- Downstream KMS/Flowcase/Agent Studio APIs verify their own resource ACL and reject a forged BFF `X-User-ID` header / invalid service token.
- Published app manifest immutable; separate draft/version/publish; no duplicate route bases and no unsanitized externally supplied upstream URL.
- Session revocation, suspended user, expired membership, disabled app and wrong `aud` all fail closed.
- Role grant and app publication endpoint cannot be accessed by a case worker; admin cannot self-approve sensitive role grant when policy forbids it.
- Admin UI displays only authorized functions, with correct empty/loading/error states and audit trail.
- All tests actually run; report skipped integration tests rather than claiming success.

## Phase prompts — execute one at a time

### Phase 0 — Repository inspection / IAM contract decision

Inspect current repositories, deployment domains, user directory/IdP capabilities, Go/Node versions, ingress, SSO integration and existing adapters. Verify corporate OIDC issuer/discovery and callback options without creating live secrets. Read all package docs and JSON schemas. Write `ADR-001-BFF-OIDC.md`, `ADR-002-APP-ROUTING.md`, `ADR-003-AUTHZ-OWNERSHIP.md`, `docs/INTEGRATION_MATRIX.md`, and `docs/OPEN_DECISIONS.md`. Determine whether app routes may share origin. Identify exact KMS/Flowcase/Agent Studio auth interfaces, or define verified adapters with explicit dev stubs. **Do not begin Phase 1 before user approval.**

### Phase 1 — Secure host auth + Go session BFF

Create Go API, PostgreSQL migrations, config and logging, OIDC authorization code callback with PKCE S256/state/nonce, IdP issuer+audience+signature validation, secure session store/cookie, `/auth/login`, `/auth/callback`, `/auth/logout`, `/api/v1/me` and CSRF. React shell with authenticated user menu. Tests for token/nonce/code/state replay and session rotation/expiry/logout, corrupt header and cookies, CSRF and exact redirect. Any dev login stub must be explicitly guarded and fail production startup.

### Phase 2 — Users, workspaces, membership and RBAC

Implement workspace registry, directory binding `(iss,sub)`, role/permission mapping, membership grants/deny, effective entitlement decision engine and `/api/v1/authz/check`. Build People, Workspaces and Access Control UI against real API, not local demo data. Test cross-workspace denial, self-escalation, disabled user, suspended membership, revocation/cache propagation and denied admin action. Return stable reason codes and audit records.

### Phase 3 — Mini-app Registry and BFF wrapping

Implement immutable manifest/schema validator, draft/publish/disable operations, approved route table, shell catalog and launcher. Integrate **real KMS**, **Flowcase**, **Agent Studio** one at a time using their confirmed auth contracts, not directly by DB or fabricated URL. Verify each service audience, scoped credentials and domain-level deny path; routes should be same-origin if possible. React app catalog/registry details and route-health status. Do not deploy Universal Case Workspace yet; register as disabled/planned template only.

### Phase 4 — Operations and production hardening

Add audit UI, structured traces, rate limits, session revocation, app disable propagation, CSP, redirects, graceful IdP unavailability, secret manager rotation, alerting, IaC/deployment docs, backups/migrations and full UI security regressions. Run `go test ./...`, frontend typecheck/build/lint, integration tests with Postgres+mock IdP and a live staging SSO/mini-app smoke when credentials provided. Report precisely what passed and what was not run.

## Per-phase output contract

At the end of each phase, provide: (a) files changed, (b) architecture/contract choices and unresolved questions, (c) DB/migration/API changes, (d) real versus mock adapters, (e) tests **actually run**, (f) security gaps and precise next phase. Stop until asked to continue.

## Starter message

> Read `AI_WORKSPACE_CONTROL_PLANE_TECHNICAL_PRD.md`, `AUTH_AND_TRUST_BOUNDARIES.md`, `RBAC_AND_ENTITLEMENTS.md`, `AI_Workspace_Control_Plane_Prototype.html`, `contracts/` and `samples/`. We are building the **shared identity + mini-app wrapping foundation**, not another SIM business service. **Start Phase 0 only.** Inspect actual IdP/KMS/Flowcase/Agent Studio deployment contracts, produce ADRs and integration matrix, and report blockers. Do not implement Phase 1 until instructed.
