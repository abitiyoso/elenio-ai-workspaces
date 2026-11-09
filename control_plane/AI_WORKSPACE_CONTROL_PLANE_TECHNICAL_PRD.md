# TECHNICAL PRODUCT REQUIREMENTS DOCUMENT
# AI Workspace Control Plane

**Version:** v1.0 · proposed implementation baseline  
**Date:** 2026-10-30  
**Status:** Engineering proposal awaiting SSO/IdP, existing-service integration and enterprise access-policy confirmation  
**Audience:** Platform Engineering, AI Ops, Security/IAM, Product, KMS/Flowcase/Agent Studio teams

## 1. Context and product thesis

Three reusable platform services already have designs/packages in this AI Workspace initiative:
1. **KMS** — governed knowledge and sources, PDF conversion via Docling and semantic retrieval via Elasticsearch.
2. **Flowcase** — immutable workflow definitions, cases, tasks, human approvals and auditing.
3. **AI Agent Studio** — Go + Eino ADK agent runtime, versioned skills/tools/models and controlled releases.

What is **not** established yet is an actual shared identity/session lifecycle, workspace membership, mini-app discovery/routing and cross-service trust architecture. Building Universal Case Workspace first would cause another temporary login/role/wrapper implementation. Build **AI Workspace Control Plane** as the foundation and register Universal Case Workspace afterward.

The Control Plane is **not** a business workflow engine, identity provider, LLM gateway, KMS, or SIM domain service. It is a focused platform application with app shell, registry, membership and entitlements, plus a secure authentication **BFF**.

**Primary persona outcomes**
- Employees sign in once using enterprise IdP; navigate permitted mini-apps through one app launcher.
- Admins manage who belongs to which workspace and what mini-app entitlements they have.
- Mini-app maintainers register internal apps with validated routes, ownership, versions, health metadata and required entitlements.
- Service teams consume a stable, verified identity/audience contract without independently implementing password login.
- Platform security can explain why access was denied and audit who changed memberships, app manifests and grants.

## 2. Scope and non-scope

### P0 / MVP (required)
- React/TS shell with app launcher, authenticated user menu, workspace switcher, navigation and allowed app catalog.
- Go BFF implementing OIDC Authorization Code (+ PKCE, state, nonce) against existing corporate IdP or Keycloak; session cookies backed by secure server-side session store.
- Authenticated API `/me`, workspace switch, app catalog, roles and entitlement checks (user-scoped).
- PostgreSQL workspace/membership/role/permission binding and application registry with immutable publish history and safe routes.
- Mini-app access integration for three cores: KMS, Flowcase, Agent Studio, with readiness contracts and no frontend-origin spoofed identity.
- Per-app route activation (same-origin gateway preferred), verified backend service audience tokens or backend session proxy, correlation IDs and audit.
- Admin management UI for memberships, role bindings, registry draft/publish/disable and allow/deny decisions.
- Negative authorization, CSRF, session and token test suite + deployment runbook.

### P1
- OIDC group synchronization and optional SCIM; invitations/approvals via enterprise IAM flow, app ownership delegation, platform notifications, feature flags, role request/approval and emergency disable.

### P2
- Federated identity for multiple organizations, multi-region, audited cross-org resource sharing, fine-grained policy DSL, usage/billing aggregation, automated provider provisioning, plugin/marketplace expansion.

### Explicitly out of scope
- Password sign-up, password resets, MFA implementation, credential databases; these belong to IdP.
- KMS knowledge indexing, per-document ACLs and Docling.
- Flowcase domain case/gate approvals, task authorization, orchestration.
- Agent Studio LLM/provider keys, model policies, agents and skill approvals.
- SIM Tender-specific forms, requirement/compliance tables, bid strategy.
- A full generalized microfrontend framework or arbitrary external iframe hosting in P0.
- Building a generic API gateway from scratch when an existing approved ingress can be configured.

## 3. Canonical product architecture

```text
Browser (React shell)                      Enterprise IAM
      |                                        |
      | 1) same-origin HTTPS cookie           | OIDC auth code + PKCE
      v                                        v
AI Workspace BFF / Go  <-----------------> Approved IdP (corp SSO / Keycloak)
  - OIDC client & server-side session
  - CSRF + origin verification
  - /me, /workspaces, /catalog, /authz
  - verified identity / membership / app entitlements
  - app manifest registry, audit, session revocation
  - app route allowlist and auth-bound reverse proxy / route middleware
      |
      +----- KMS API            (source ACL still owned by KMS)
      +----- Flowcase API       (case/task/gate authorization still owned by Flowcase)
      +----- Agent Studio API   (agent/model/tool policy still owned by Agent Studio)
      +----- Universal Case Workspace (future reusable experience app)

PostgreSQL: workspace/membership/registry/policy/audit
Session store: server-managed (PostgreSQL initially; Redis optional when scaling)
Secrets: external vault/secret manager (no browser or DB plaintext client secrets)
```

**Preferred UX deployment:** shell at `/`, KMS at `/apps/kms/*`, Flowcase at `/apps/flowcase/*`, Agent Studio at `/apps/agent-studio/*` using registered internal routes and a trusted ingress/BFF. Each app may keep its own deployment/repo and Go API; the shell is not forced to combine them. **Do not** dynamically proxy arbitrary registry URLs. Route allowlisting and per-app trust/configuration require administrator approval.

## 4. Identity and auth decisions

1. **Use existing enterprise OIDC IdP if available**. Keycloak is the fallback option, not a custom auth server to implement.
2. Go host/BFF is a confidential OIDC client. Authorization Code with S256 PKCE, signed/one-time state, nonce, issuer/redirect validation and mix-up prevention; exact allowed redirect URIs.
3. Access and refresh tokens live server-side, never in React, localStorage or non-HttpOnly cookies. Browser stores only an opaque `Secure; HttpOnly; SameSite=Lax` session cookie. `SameSite=Strict` where deployment UX allows. All state-changing routes require origin/referer validation and CSRF token protection beyond relying only on SameSite.
4. Session ID rotation on login/elevation, absolute and idle expiry, server-side logout/revocation, replay detection, refresh-token rotation where supported, and audit of session changes.
5. Go validates ID token (`iss`,`aud`,`exp`,`nonce`, signature/JWKS), Access Token where appropriate, and does not use caller-controlled `X-User-*` headers as identity.
6. Mini-app APIs receive **audience-bound** verified service tokens (short-lived, scoped claims + optional signed on-behalf-of subject) or a trusted auth proxy with independent verification. No blanket forwarding of a privileged BFF token. Distinguish **user delegation** from **service identity**; do not silently promote a service account to human approver.
7. Cross-origin app deployment requires explicit trust policy, CSP `frame-ancestors`, secure CORS if unavoidable, and OIDC/session strategy. P0 defaults to same-origin shell routes and no generic iframe embedding.

Security rationale: RFC 10017 (August 2026) describes BFF as a strong browser OAuth architecture with tokens kept out of JS; RFC 9700 (January 2025) defines OAuth best current practice for PKCE, redirect validation and CSRF. See `AUTH_AND_TRUST_BOUNDARIES.md`.

## 5. Authorization model and responsibilities

**Platform entitlements** allow *discover/open/administer* mini-apps, workspace membership and specific Control Plane actions. **Domain authorization remains inside each mini-app**.

A decision context includes `(verified_subject, tenant, active_workspace, app_id, action, resource, role_bindings, effective_permissions, policy_version, session_auth_level)`.

**Roles (illustrative):**
- `platform_admin` — platform configuration, app registry approval, workspace admin (limited to assigned tenant), audit.
- `workspace_admin` — workspace membership and scoped role grants; cannot change cross-tenant registry or IdP trust.
- `knowledge_contributor` — `kms:open`, optionally `kms:contribute`; KMS still validates document ACL.
- `case_worker` — `flowcase:open`, `workspace:open`; Flowcase still checks case membership.
- `case_reviewer` — `flowcase:open`, potentially `review_inbox:view`; **does not** automatically confer G0–G5 approval.
- `agent_author` — `agent-studio:open`, `agent:author` subject to Agent Studio resource policy; cannot publish by default.
- `agent_ops` — `agent-studio:open`, `agent:release_request` subject to Agent Studio approved operator policy.

MVP policy: **default deny**, role-to-entitlement mapping versioned in DB, explicit deny/revocation wins over inherited allow; membership must be active. Entitlement grants are scoped to workspace and optional app; restrict admin role assignment and require independent review for sensitive privileges. Separate `app:open` from `resource:read`/`gate:approve`.

**Critical:** a Control Plane UI may hide a button but is NEVER the authority. Each service must separately validate identity, tenant and resource/action; e.g. `flowcase:open` does not mean approving a customer's bid. Example policy contract and negative tests supplied.

## 6. Mini-app registry

**AppDefinition** (`contracts/mini_app_manifest.schema.json`): immutable `app_key`, display name, description, icon name/ref, category, owner team, `route_base` (approved relative path), deployment mode, environment, version, required entitlements, health reference, lifecycle status (`draft`, `published`, `disabled`, `retired`). Optional workspace allowlist and feature flags.

Mandatory validation:
- no arbitrary external URL/proxy hosts, no `..`, encoded path traversal, open redirects, unsanitized script snippets, or dynamic origin from end-user input;
- unique app key and route base per environment;
- app route and upstream service target are **separate**; upstream actual target resolved from trusted deployment config, never user-provided manifest URL;
- registry change requires authorized publisher and immutable version; existing pinned service routes are not overwritten silently;
- disable immediately prevents new launches/sessions by policy; backend must also deny stale entitlements/tokens based on revocation policy;
- supported rendering modes P0: `same_origin_route`, and `external_trusted_link` only after review. `iframe` not enabled by default.

Do not store live client secrets, user bearer tokens or provider credentials in app manifests.

## 7. Frontend information architecture

1. **Home** — launcher, favorite apps, assigned work indicators (adapter data, not invented global task state), platform alerts, recent audit/launch events.
2. **App Catalog** — available apps filtered using server-provided verified entitlement decision; locked apps may optionally appear in admin view with no launch affordance.
3. **People & Teams** — user directory (IdP sourced), active/suspended state, memberships, human role bindings (admin only).
4. **Workspaces** — workspace membership, switching, app availability and ownership.
5. **Access Control** — role permissions matrix, user-specific access-preview tool, effective decision trace, least privilege.
6. **Mini-App Registry** — draft/publish/disable and route policies, owner contacts, registered versions and health-adapter metadata.
7. **Identity & Integrations** — OIDC provider status, cookie session/security overview, adapters to KMS/Flowcase/Agent Studio, service identities (metadata only).
8. **Audit Log** — append-only platform actions, actor and target, timestamp/correlation and redacted details.

**Design:** same Navy + Cobalt system as KMS/Flowcase/Agent Studio, refined Apple-inspired bento cards but strong text contrast and data density. Sidebar dark navy, topbar translucent white, bright cobalt actionable surfaces, card/table off-white borders; no purple AI gimmicks. Mobile drawer and accessible keyboard focus.

## 8. Domain model / PostgreSQL (logical)

- `tenants(id, key, name, status, created_at)`; tenant IDs always derived from verified subject/membership.
- `users(id, idp_issuer, idp_subject, display_name, email, status, last_login_at)`; unique `(issuer, sub)`; do not use mutable email as identity key.
- `workspaces(id, tenant_id, key, name, status, owner_user_id, created_at)`.
- `workspace_memberships(id, tenant_id, workspace_id, user_id, state, invited_by, joined_at, revoked_at)`; unique active membership constraint.
- `roles(id, tenant_id, key, display_name, scope)`; `permissions(id, key, resource_type, action)`; `role_permissions(role_id, permission_id)`.
- `role_bindings(id, tenant_id, workspace_id, user_id, role_id, state, expires_at, granted_by)`; append audit on change.
- `entitlement_overrides(id, tenant_id, workspace_id, user_id, app_id, permission_key, effect, expires_at, reason)`; explicit deny allowed, require approval for sensitive grants.
- `mini_apps(id, tenant_id_nullable, app_key, owner_team, status, created_at)` and `mini_app_versions(id, mini_app_id, version, manifest_json, checksum, release_state, published_by, published_at)`.
- `mini_app_routes(id, app_version_id, environment, registered_path, upstream_config_ref, enabled)`; external route targets in trusted ops config.
- `sessions(id_hash, user_id, tenant_id, workspace_id, idle_expires_at, absolute_expires_at, revoked_at, refresh_token_secret_ref, created_at)`.
- `identity_providers(id, tenant_id_nullable, issuer_url, client_id, secret_ref, discovery_hash, status)`; never store raw client secret.
- `service_identities(id, client_id, audience, scopes_json, certificate_ref, status)`; `service_authorizations` if needed.
- `platform_audit_events(id, actor_user_id_nullable, service_id_nullable, tenant_id, workspace_id, action, target_type, target_id, decision, policy_version, correlation_id, redacted_metadata, created_at)`.
- `outbox_events(id, event_type, payload_json, status, retry_at, dedupe_key)`.

Use UUIDs, foreign keys, optimistic versioning, indexes on subject/workspace/key/status and tenant segregation (RLS optional defense-in-depth, not substitute for filters). Use transactional outbox for external notifications. Make admin grants auditable and revocable.

## 9. API contracts (proposed; confirm against existing services)

| Method | Route | Responsibility |
|---|---|---|
| GET | `/auth/login` | Start server-side authorization request with PKCE/state/nonce; redirect to configured IdP |
| GET | `/auth/callback` | Validate issuer,state,nonce, code; exchange tokens server-side; rotate/create session |
| POST | `/auth/logout` | Revoke session, expire cookie, optional IdP logout |
| GET | `/api/v1/me` | Verified profile, tenant/workspace choices, only permitted app summary |
| POST | `/api/v1/session/workspace` | Switch to another active membership with CSRF validation and audit |
| GET | `/api/v1/apps` | Entitlement-filtered app catalog for active session/workspace |
| GET | `/api/v1/apps/{appKey}/launch` | Server-side verified launch intent & registered route; no untrusted redirect |
| GET | `/api/v1/workspaces` | List active membership workspaces |
| GET/POST | `/api/v1/admin/workspaces` | Admin list/create with scoped authorization |
| GET/POST | `/api/v1/admin/users` | Directory read / membership management, no password CRUD |
| PUT/DELETE | `/api/v1/admin/memberships/{id}/roles` | Grant/revoke scoped role (schema and reason, SoD) |
| POST | `/api/v1/authz/check` | Server-computed decision for verified caller; not escalation mechanism |
| GET/POST | `/api/v1/admin/apps` | Registry list and draft creation |
| POST | `/api/v1/admin/apps/{key}/publish` | Validate/version/authorize publish |
| POST | `/api/v1/admin/apps/{key}/disable` | Revoke new access and register audit |
| GET | `/api/v1/admin/audit` | Authorized redacted immutable audit cursor |
| GET | `/healthz`, `/readyz` | Service-level health and verified dependencies without sensitive details |

For writes use CSRF token, strict content type, idempotency keys and revision/If-Match conflict handling. Return structured `{code,message,trace_id}` errors with correct `401/403/404/409/422/429`. Do not leak cross-tenant resource existence in forbidden results.

## 10. Shell wrapping/integration contract

**Preferred MVP:** one same-origin shell with registered app routes. Sub-apps remain separately deployed and receive identity via *verified server token or gateway middleware*, never trusting browser `X-User-ID`/`X-Role`. Each app exposes the required `AuthAdapter` interface:

```go
// Conceptual port; exact types are implementation-specific.
type VerifiedPrincipal struct {
    Issuer, Subject, TenantID, WorkspaceID string
    Audience string
    Scopes []string
    SessionID string // internal opaque reference; do not expose raw session
}
type AuthAdapter interface {
    VerifyRequest(ctx context.Context, req *http.Request) (VerifiedPrincipal, error)
    Authorize(ctx context.Context, p VerifiedPrincipal, action string, resource ResourceRef) error
}
```

**Shared browser UX protocol** is non-security context (workspace name, navigation state, theme, app catalog). Do not send access tokens through window messages. A mini-app may open into another tab/route but follows BFF authentication and trusted route/audience.

**Service versions already assumed:** KMS: Docling + Elasticsearch; Flowcase: Go durable business workflow; Agent Studio: Go/Eino ADK with models/providers. Do not rebuild any inside Control Plane.

## 11. Reliability, monitoring and hardening

- Target pilot reliability metrics after actual baseline (auth success/failure rate, p95 `/me`, catalog and app launch latency, 401/403 by app, active session count, IdP health, membership propagation).
- Allow cached, short-lived display-only catalog data with explicit invalidation; **never cache positive access decisions longer than approved security window after revocation**.
- Rate limit login/callback, catalog and admin writes; protect session endpoint from fixation, replay and open redirects.
- Secrets in external secret manager, logs redacted of PII/tokens, request IDs correlated across shell and apps. Do not capture passwords (none handled here).
- Session store durability across multi-instance deployment; session invalidation on user disable, workspace removal or security incident. Graceful degradation: if IdP goes down, no new login; existing sessions depend on explicit policy/expiry, never silently bypass auth.
- High-privilege admin actions may require recent authentication and mandatory change ticket/reason in prod. Dual control for system-wide app policies if mandated.

## 12. Acceptance / UAT scenarios

1. **Single sign-on:** With approved IdP user, login once in shell; open KMS and Flowcase routes without a second password prompt; APIs reject invalid token and expired session.
2. **Least privilege:** Workspace case worker sees Flowcase but not Agent Studio; direct URL to `/apps/agent-studio/*` returns 403 (not merely hides tile).
3. **Workspace isolation:** User belongs to SIM but not Legal; switching to Legal denied; unauthorized Legal source/case remains inaccessible in each domain API.
4. **Privilege separation:** `flowcase:open` alone never grants `case:approve`; `agent-studio:open` never grants `model:approve`.
5. **User suspension/revocation:** Admin revokes membership; next authorized check denies; existing sessions invalidated within policy target.
6. **Registry lifecycle:** Admin publishes a manifest with validated internal route; new app appears only for users with required entitlement; disabled app no longer launches, including direct route attempts.
7. **Host hardening:** forged `X-User-ID`, `X-Workspace-ID` and `X-Role` headers, untrusted app-origin messages or malformed cookies never authenticate.
8. **OAuth defenses:** PKCE/state/nonce redirect and callback checks; reject replayed state, wrong issuer, unregistered redirect, login CSRF, session fixation and expired ID token.
9. **Audit:** Role grant, membership revoke, app publish/disable and authz deny are immutable, actor-attributed with trace ID; no tokens or secrets recorded.
10. **Service identity:** Agent Studio agent callback credential cannot act as a human approver or inherit admin session.
11. **Multi-app smoke:** KMS, Flowcase, Agent Studio `AuthAdapter`s validate exact `iss`,`aud`, workspace scopes from trusted upstream; denied cross-tenant API call verified at service boundary.
12. **Mobile UX:** App launcher, sidebar, workspaces and access management work with focus/keyboard and responsive layout.

## 13. Delivery plan

### Phase 0 — Architecture and contract freeze
Inspect actual repositories, IdP availability, KMS/Flowcase/Agent Studio deployment and ingress, current token/cookie expectations and service URLs. Produce ADRs: IdP choice, BFF session, app route approach, authz policies, propagation/revocation, service token exchange. Validate schemas. Stop and report unresolved contracts.

### Phase 1 — Go BFF, OIDC and session security
Migrations, IdP integration, secure session store, CSRF, cookie protection, `/me`, logout, audit and auth negative tests. Connect React shell to authenticated `/me`. Never fake production auth.

### Phase 2 — Workspace membership & RBAC
Directory sync/provisioning adapter, workspace CRUD, scoped bindings, revocation, policy engine and authorization check; UI People/Workspaces/Access Control. Test cross-workspace/user action denials.

### Phase 3 — Mini-app Registry and wrapping
Versioned app manifests, validated routes, publish/disable, entitlements-based launcher and trusted route enforcement. Integrate real KMS/Flowcase/Agent Studio adapters **one by one**; ensure services independently verify identity and resource scope.

### Phase 4 — Reliability, ops and UAT
Audit UI, metrics, session lifecycle/rotation, rate limits, app availability, revocation, hardened CSP/CORS, CI, deployments, runbook, backup and E2E negative tests. Confirm security review before production.

## 14. Open decisions

- Existing enterprise IdP or whether to deploy Keycloak (who owns tenant/users/groups).
- UI hosting domain, ingress/proxy and current deployment paths of existing mini-apps.
- Shared user/workspace data source and whether there is a preexisting directory or HR/enterprise API.
- Single enterprise tenant or true external multitenancy at MVP; recommended start with one enterprise tenant and multiple workspaces while keeping tenant-safe schema.
- App-specific role mappings, owner approval authorities and separation-of-duties requirements.
- Session idle/absolute TTL, revocation propagation target, permitted emergency admin path.
- Cross-origin/iframe requirement, if any. Recommend same-origin routes first.

**Definition of done for platform MVP:** a real enterprise user can sign in once, switch to an authorized workspace, see/open only entitled apps, and receive an independently validated identity at each core service, while unauthorized direct access is denied and audited. A working HTML prototype is not this definition of done.
