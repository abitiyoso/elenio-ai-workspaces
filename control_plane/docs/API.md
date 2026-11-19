# Control Plane HTTP API (implemented contract)

Types are defined in `web/src/api/types.ts`; the machine-readable spec is `api/openapi.yaml`.
All JSON. Errors are `ApiError` `{code, message, trace_id}` with status 400/401/403/404/409/412/422/429/503.
Forbidden results never reveal whether a resource in another tenant/workspace exists (404 vs 403 is uniform per route).

## Conventions

- Authentication: opaque session cookie set by `/auth/callback` (`__Host-cp_session` over HTTPS,
  `cp_session` in explicit development over HTTP). The browser never receives OAuth tokens.
- Every non-GET request under `/api/v1` and `/auth/logout` requires:
  - `X-CSRF-Token: <me.csrf_token>`
  - `Origin` equal to the configured public origin (or, if absent, a same-origin `Referer`)
  - `Content-Type: application/json` when a body is sent.
- Admin writes require a `reason` (3–500 chars) in the body; it is stored in the audit trail.
- Optimistic concurrency for registry lifecycle: `If-Match: "<revision>"`; mismatch → `412 REVISION_MISMATCH`.
- A `401` from any `/api` route means the session is missing/expired/revoked → UI should send the user to
  `/auth/login?return_to=<current path>`.
- Response header `X-Request-ID` carries the trace id.

## Auth (browser navigations, not fetch)

| Method | Path | Notes |
|---|---|---|
| GET | `/auth/login?return_to=/relative/path` | 302 to IdP (PKCE S256, state, nonce). `return_to` must be a same-origin relative path, else ignored. 503 HTML page if IdP unavailable. |
| GET | `/auth/callback` | IdP redirect target. On success 302 to `return_to` (default `/`). On failure 302 to `/signed-out?error=<code>`. |
| POST | `/auth/logout` | CSRF. 200 `{ "redirect": "<idp end-session url or /signed-out>" }`; session revoked + cookie cleared. |

## Session

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/api/v1/me` | – | `Me` |
| POST | `/api/v1/session/workspace` | `{workspace_id}` | `204`; 403 `NOT_MEMBER` / `MEMBERSHIP_SUSPENDED` |
| GET | `/api/v1/workspaces` | – | `{items: Workspace[]}` |
| GET | `/api/v1/apps` | – | `{items: AppSummary[]}` (active workspace, entitled only) |
| GET | `/api/v1/apps/{app_key}/launch` | – | `LaunchResult`; 403 with `code` = reason code |
| POST | `/api/v1/authz/check` | `{action, resource_type, resource_id}` | `Decision` for the caller only (no subject override) |

`authz/check` supports `resource_type: "mini_app"` with `action: "app:open"`, `resource_type: "workspace"`
with `action: "workspace:switch"`, and `resource_type: "platform_admin"` with a permission key as `action`
(e.g. `registry:manage`).

## Admin

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/v1/admin/users?q=` | – | `{items: DirectoryUser[]}` (platform admin: tenant; workspace admin: users in managed workspaces) |
| POST | `/api/v1/admin/users` | `{idp_issuer, idp_subject, display_name, email?, reason}` | `201 DirectoryUser` (directory binding, no credentials) |
| POST | `/api/v1/admin/users/{id}/suspend` | `{reason}` | `200 DirectoryUser`; revokes all sessions |
| POST | `/api/v1/admin/users/{id}/activate` | `{reason}` | `200 DirectoryUser` |
| POST | `/api/v1/admin/users/{id}/sessions/revoke` | `{reason}` | `200 {revoked: number}` |
| PUT | `/api/v1/admin/users/{id}/tenant-roles` | `{role_key, reason, expires_at?}` | `GrantResult` (tenant roles are sensitive → pending) |
| GET | `/api/v1/admin/workspaces` | – | `{items: (Workspace & {member_count:number})[]}` |
| POST | `/api/v1/admin/workspaces` | `{key, name, description, reason}` | `201 Workspace` |
| GET | `/api/v1/admin/workspaces/{id}/members` | – | `{items: (Membership & {user: {id, display_name, email, status}})[]}` |
| POST | `/api/v1/admin/workspaces/{id}/members` | `{user_id, reason}` | `201 Membership` |
| POST | `/api/v1/admin/memberships/{id}/suspend` · `/reinstate` · `/revoke` | `{reason}` | `200 Membership` |
| PUT | `/api/v1/admin/memberships/{id}/roles` | `{role_key, reason, expires_at?}` | `201 GrantResult(granted)` or `202 GrantResult(pending_approval)` |
| DELETE | `/api/v1/admin/role-bindings/{id}` | `{reason}` | `204` |
| GET | `/api/v1/admin/grant-requests?state=pending` | – | `{items: GrantRequest[]}` |
| POST | `/api/v1/admin/grant-requests/{id}/approve` · `/reject` | `{reason}` | `200 GrantRequest` (approver ≠ requester ≠ target) |
| GET | `/api/v1/admin/roles` | – | `{items: Role[], policy_version}` |
| GET | `/api/v1/admin/permissions` | – | `{items: Permission[]}` |
| GET | `/api/v1/admin/workspaces/{id}/overrides` | – | `{items: EntitlementOverride[]}` |
| POST | `/api/v1/admin/workspaces/{id}/overrides` | `{user_id, permission_key, effect, reason, expires_at?}` | `201 EntitlementOverride` (allow only for non-sensitive `*:open`/`catalog:view`) |
| DELETE | `/api/v1/admin/overrides/{id}` | `{reason}` | `204` |
| POST | `/api/v1/admin/access-preview` | `{user_id, workspace_id, app_key}` | `Decision` with `trace` |
| GET | `/api/v1/admin/apps` | – | `{items: RegistryApp[]}` |
| GET | `/api/v1/admin/apps/{key}` | – | `RegistryApp` |
| POST | `/api/v1/admin/apps` | `{manifest: AppManifest, reason}` | `201 RegistryApp`; 422 `{issues}`; 409 key/route conflict |
| POST | `/api/v1/admin/apps/{key}/versions` | `{manifest, reason}` | `201 RegistryApp` (new immutable draft version) |
| POST | `/api/v1/admin/apps/{key}/publish` | `{version, reason}` + `If-Match` | `200 RegistryApp` |
| POST | `/api/v1/admin/apps/{key}/disable` | `{reason}` + `If-Match` | `200 RegistryApp` |
| POST | `/api/v1/admin/apps/{key}/enable` | `{reason}` + `If-Match` | `200 RegistryApp` (re-activates the published version) |
| GET | `/api/v1/admin/audit?cursor=&limit=50&action=&actor_user_id=&workspace_id=` | – | `Page<AuditEvent>` (newest first) |
| GET | `/api/v1/admin/integrations` | – | `IntegrationsStatus` |

## Delegated token exchange (server-to-server, ADR-004)

`POST /api/v1/token-exchange` — called by an experience-app BFF, never by the browser. **Not** cookie-authenticated,
so no CSRF token or `Origin` is required; rate-limited (failed client auth per IP, exchanges per client+session).

- Client authentication: `Authorization: Basic base64(client_id:client_secret)`; clients and their allowed audiences
  come from `TOKEN_EXCHANGE_CLIENTS` (deployment config).
- Request (`Content-Type: application/json`, unknown fields rejected):
  `{"subject_token": "<CP token received via /apps/<client_id>/>", "audience": "flowcase"}`. Optional RFC 8693
  fields, validated when present: `grant_type` = `urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token_type` = `urn:ietf:params:oauth:token-type:jwt` (or `...:access_token`), `requested_token_type` =
  `urn:ietf:params:oauth:token-type:jwt`. Send `X-Request-ID` to carry the user request's correlation id.
- `200`: `{"access_token": "<ES256 JWT>", "token_type": "Bearer", "expires_in": 300, "issued_token_type": "urn:ietf:params:oauth:token-type:jwt"}`.
  The token has the subject token's `sub`, `tenant_id`, `workspace_id`, `sid`, `auth_time`; `aud` = the target
  app's audience; `scope` from the target app decision; `act: {"sub": "app:<client_id>"}`; lifetime ≤ 5 min.
- Errors (`ApiError`; nothing is ever partially issued):

| Status | `code` | Meaning |
|---|---|---|
| 400 | `INVALID_REQUEST` | body not JSON / unknown field / missing `subject_token` or `audience` / unsupported type fields |
| 401 | `INVALID_CLIENT` | missing/unknown client or wrong secret (`WWW-Authenticate: Basic`) |
| 401 | `INVALID_SUBJECT_TOKEN` | not signed by this CP (any current/previous kid), wrong `iss`, expired, lifetime > 15 min, wrong tenant, missing claims |
| 401 | `SUBJECT_TOKEN_AUDIENCE_MISMATCH` | subject token `aud` is not the client id |
| 401 | `SUBJECT_TOKEN_DELEGATED` | subject token carries `act` (exchanged tokens cannot be exchanged again) |
| 401 | `SUBJECT_NOT_HUMAN` | `principal_type` is not `human` |
| 401 | `SESSION_INACTIVE` | the session behind `sid` is revoked, expired or unknown (e.g. contract-check/devtoken tokens) |
| 401 | `SESSION_WORKSPACE_CHANGED` | the session's active workspace differs from the token's `workspace_id` |
| 401 | `SUBJECT_INACTIVE` | user or tenant no longer active |
| 403 | `AUDIENCE_NOT_ALLOWED` | audience not in the client's allowlist |
| 403 | `AUDIENCE_NOT_CONFIGURED` | no proxied trusted upstream with an audience for it |
| 403 | `CLIENT_APP_DENIED` | the user may no longer open the client app in this workspace (message carries the reason code) |
| 403 | `TARGET_APP_DENIED` | the user may not open the target app in this workspace (e.g. `MISSING_PERMISSION`, `APP_DISABLED`) |
| 429 | `RATE_LIMITED` | retry after `Retry-After` |

Every attempt writes an audit event: `token.exchanged` (decision `allow`) or `token.exchange_denied` (decision
`deny`), actor type `service`, `service_id` = client id, `target_id` = audience, `reason_code`, metadata `client_id`.

## Platform

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz` | liveness, no dependencies |
| GET | `/readyz` | database + migrations; IdP reported as `degraded`, never secrets |
| GET | `/.well-known/jwks.json` | public keys for audience-bound service tokens (also verifies exchanged tokens) |
| POST | `/api/v1/token-exchange` | delegated token exchange for experience-app BFFs (see above) |
| ANY | `/apps/{app_key}/*` | authorised same-origin route: 302 to login if no session, 403 page on deny, 503 page if integration pending, else reverse-proxied with a fresh audience-bound token |
| GET | `/metrics` | Prometheus, on `METRICS_ADDRESS` listener only |
# Solution plans extension

Additive routes: GET/POST `/api/v1/solutions`, GET/PATCH `/api/v1/solutions/{id}`, GET `/api/v1/solutions/{id}/readiness`. Exact contract: [solution_registry.openapi.yaml](../contracts/solution_registry.openapi.yaml), referenced by the main OpenAPI. Existing cookie session, Origin/CSRF and rate limits apply. Plans use active workspace and `solution:view`/`solution:author`; no tenant/user/workspace fields are accepted in request bodies. Creation requires Idempotency-Key; PATCH requires the quoted revision ETag. Readiness uses current app permission and existing audience-bound delegation to trusted Flowcase only. It reports partial publication evidence, never solution release/deployment readiness. No new domain approval or licensing authority.
