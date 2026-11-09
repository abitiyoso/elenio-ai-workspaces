# ADR-003 — Authorization ownership: platform entitlements vs domain ACLs

**Status:** Accepted (implemented) · 2026-10-30

## Decision
1. **Control Plane decides only** "may verified principal P discover/open app A in workspace W" and "may P
   administer platform resource R". Engine: `internal/authorization` — default deny, facts loaded from
   PostgreSQL per decision (no positive-decision cache), explicit deny wins, allow overrides cannot confer
   sensitive permissions, stable reason codes (`ROLE_GRANT`, `OVERRIDE_ALLOW`, `EXPLICIT_DENY`, `NOT_MEMBER`,
   `MEMBERSHIP_SUSPENDED`, `APP_DISABLED`, `APP_UNPUBLISHED`, `WORKSPACE_NOT_ALLOWED`, `MISSING_PERMISSION`,
   `SUBJECT_DISABLED`), policy version `rbac-2026.10.1` (`rbac-2026.10.2` since migration 0003: `case-workspace:open` for case reviewers and platform admins).
2. **Scopes.** `platform_admin` is a tenant-scoped role (registry, audit, users, grants approval); app-open
   permissions it carries still require an active membership in the workspace. Workspace roles count only
   through an active membership. Tenant permissions are never derived from workspace roles.
3. **Separation of duties.** No self-grants (any role); sensitive roles (`platform_admin`, `workspace_admin`)
   create a `role_grant_requests` row that a *different* holder of `grants:approve` must approve — enforced in
   the service and by a database CHECK (`decided_by <> requested_by AND decided_by <> user_id`). The last active
   platform admin cannot be revoked. Users cannot suspend themselves.
4. **Domain authorization stays in each service.** A platform admin is not a Flowcase gate approver, KMS source
   reader or Agent Studio release reviewer — no platform permission for those actions exists
   (`authz/check` for `case:approve` → deny).
5. **Downstream identity contract** (`internal/integrations/tokens.go`): ES256 JWT, `kid` = RFC 7638 thumbprint,
   JWKS at `/.well-known/jwks.json`, TTL 5 min (≤ 15 min enforced), claims `iss` (public origin), `sub` (Control
   Plane user id), `aud` (per app), `iat/nbf/exp/jti`, `ctx_ver=workspace.authctx.v1`, `principal_type`,
   `tenant_id` (tenant key), `workspace_id` (workspace key), `scope`, `idp_iss`, `idp_sub`, `sid` (HMAC of
   session id), `policy_version`, `correlation_id`, `auth_time`, and — only on tokens issued by the delegated
   token exchange (ADR-004) — `act: {"sub": "app:<client>"}`. Services must verify signature, `iss`, `aud`,
   lifetime and then apply their own ACLs; forged `X-User-ID` headers are stripped by the BFF and must be
   ignored by services anyway.
6. **Service vs human.** BFF-minted tokens are *user delegation* tokens (`principal_type=human`). Service
   identities (`service_identities`) are metadata only, may never hold platform admin (engine denies), and must
   never map to a human approver downstream.

## Revocation
Memberships, bindings, overrides, user status and app status are read on every decision → effective on the
next request. Sessions of suspended users are revoked in the same transaction. Downstream services receive
5-minute tokens, so a revoked user's last token expires within 5 minutes; propagation events
(`identity.user_suspended`, `membership.*`, `role_binding.revoked`, `app.*`) are written to the transactional
outbox for services that want push invalidation.
