# Implementation report — Phases 0–4 (2026-10-30/31)

The user asked for all phases in one pass followed by deployment, so the master prompt's "stop after each
phase" gates were not applied. This report gives its per-phase output contract in one place.

## Phase 0 — inspection and contract decisions
- **Inspected:** sibling repos `kms_go_package`, `flowcase_platform`, `ai_agent_studio`; the VM's running containers,
  ports, health endpoints, Tailscale (no Serve config, no TLS cert domains) and sudo (unavailable).
- **Outputs:** `docs/adr/ADR-001..003`, `docs/INTEGRATION_MATRIX.md`, `docs/OPEN_DECISIONS.md`.
- **Key findings:** no corporate IdP → Keycloak; same-origin routes possible and chosen. Flowcase `trusted_jwt` is
  compatible with Control Plane tokens. KMS has no verifier implementation. Agent Studio trusts only Tailscale headers.

## Phase 1 — BFF auth and sessions
- OIDC code + PKCE S256/state/nonce, browser-bound one-time flows, RFC 9207 `iss` check, exact redirect URI, ID
  token verification, JIT directory binding by `(iss, sub)`, new session id per login, idle/absolute expiry,
  logout with IdP refresh-token revocation + RP-initiated logout, CSRF (Origin + HMAC token), strict JSON bodies.
- Production startup refuses dev seed, mock-auth variables, http, missing keys and missing upstream config.
- There is no dev login stub; the VM uses real Keycloak logins.

## Phase 2 — workspaces, membership, RBAC
- Tables: tenants, users, workspaces, workspace_memberships, roles, permissions, role_permissions, role_bindings,
  role_grant_requests, entitlement_overrides, policy_meta.
- Decision engine with stable reason codes. No positive-decision cache, so revocations apply on the next request.
- Self-grants are refused. Sensitive grants need an independent approver, enforced in code and by a DB CHECK.
  The last platform admin cannot be removed. Allow overrides cannot grant sensitive permissions.
- APIs and UI: People, Workspaces, Access Control (matrix, access preview with trace, overrides), `authz/check`.

## Phase 3 — registry and wrapping
- Strict manifest parsing and validation (schema plus traversal/URL/markup rules). Versions are immutable (trigger).
  Draft → publish (If-Match revision) → disable/enable. Route table per environment. Catalog and launch
  return the same decision.
- `/apps/<key>/*`: authorises every request, strips forged identity headers, mints an ES256 audience-bound token
  (JWKS at `/.well-known/jwks.json`), proxies only to deployment-configured upstreams, and shows an honest
  pending page otherwise.
- **Real vs mock adapters:** all three apps are registered and health-probed against their live VM endpoints.
  Forwarding is `pending` for all three (reasons in INTEGRATION_MATRIX): forwarding would hit development
  identities. Proxy mode is implemented and tested against an upstream that verifies tokens with
  Flowcase-equivalent rules. Universal Case Workspace is registered as a draft only.

## Phase 4 — hardening and operations
- CSP / X-Frame-Options / COOP / CORP / Referrer-Policy / Permissions-Policy (HSTS over HTTPS).
- Rate limits on auth (per IP), API (per session) and admin writes. Structured logs with correlation IDs and
  redaction. Prometheus metrics. Graceful IdP outage handling.
- Audit UI with cursor pagination. Transactional outbox for propagation. Maintenance loop. Key rotation via
  previous JWKS.
- Deployment and backup scripts, runbook, alert suggestions, OpenAPI.

## DB / migration / API changes
`migrations/0001_foundation.up.sql`, `0002_rbac_catalog.up.sql` (policy `rbac-2026.10.1`). API: `docs/API.md`, `api/openapi.yaml`.

## Tests actually run (2026-10-31, macOS, Go 1.27.1, Node 24, PostgreSQL 16 in Docker)
- `go test ./...` with `CP_REQUIRE_DB=1`: **32 test cases passed, 0 skipped, 0 failed.**
  - Unit: config refusals; manifest/route attacks; decision engine (12 cases); return_to; limiter.
  - Integration (real server, PostgreSQL, mock OIDC IdP):
    - **Login and tokens:** happy path with no tokens in `/me`; state replay; login CSRF; wrong `iss` param;
      wrong token issuer/audience; expired ID token; nonce mismatch; code injection (PKCE).
    - **Redirects and sessions:** open redirect; session rotation; logout CSRF/origin; revoked cookie replay;
      IdP refresh revocation; idle and absolute expiry.
    - **Request hardening:** forged headers; corrupt cookies; CSRF and Origin variants; IdP outage → 503.
    - **Access control:** least privilege and direct-route 403; workspace switch; case worker denied 11 admin
      actions; self-grant and dual control (incl. DB CHECK); workspace-admin scope; last-admin guard.
    - **Revocation and audit:** immediate revocation, suspension, deny/allow overrides and outbox events;
      caller-scoped `authz/check`; audit immutability and pagination.
    - **Registry:** 12 invalid manifests, duplicates, If-Match 428/412, publish, entitlement visibility,
      immutability trigger, disable propagation.
    - **Proxy:** token verified downstream; wrong audience rejected; forged headers stripped; cookie overwrite
      blocked; cross-origin POST refused; pending page; revoke mid-session.
- `web`: typecheck, eslint (0 problems), vitest **17/17**, production build.
- `gofmt -l`, `go vet`: clean.
- **Manual local E2E:** real Keycloak 26 (Docker, localhost) → login as Rafi through the Keycloak form → shell
  Home, Integrations, Access Control render with live data; no console errors besides the expected pre-login 401.
- **VM smoke (deployed):** `/readyz` all ok, login 302 to Keycloak with PKCE S256, unauthenticated `/apps/*` →
  login redirect, forged `X-User-ID` → 401, JWKS served, CSP headers present, all three upstream health probes
  healthy, Keycloak sign-in page reachable from the browser.

## Follow-up 2026-10-31 — Flowcase integrated (first proxied app)
`flowcase-cp` instance in `trusted_jwt` mode behind `/apps/flowcase/`; Control Plane upstream switched to
`proxy`. Live contract test 16/16 (see INTEGRATION_MATRIX). Flowcase web checks: typecheck, lint, 30/30 tests,
build. Control Plane code unchanged (config only).

**Not verified:** a signed-in browser session clicking through Flowcase on the VM (needs your sign-in). The
proxy chain itself (session → decision → header stripping → minted token → upstream) is covered by
`TestProxyForwardsAudienceBoundTokenAndStripsForgedIdentity`.

## Follow-up 2026-10-31 — KMS and Agent Studio integrated; all three apps proxied
- **KMS** (new verifier + `tenant_principals` + `KMS_BASE_PATH`): its own `go test` passes, with the DB suite
  run against throwaway Postgres; frontend build passes with and without the base path.
  Deployed as `kms-cp`.
- **Agent Studio** (new `control_plane_jwt` mode + tenant-key check + `AGENT_STUDIO_WEB_BASE_PATH`): `go test ./...`
  passes (DB acceptance tests skip without `AGENT_STUDIO_TEST_DATABASE_DISPOSABLE=1`); web build/typecheck pass.
  Deployed as `agent-studio-cp`. Its `CHECKSUMS.json` is stale for the edited files.
- **Flowcase** `flowcase-cp`: redeployed from current source.
- **Live contract sweep** (`deploy/contract-check.sh`): Flowcase 17/17 ×2 targets, KMS 19/19 ×2, Agent Studio
  19/19. The first Agent Studio run failed "other tenant claim" (200); fixed by `AGENT_STUDIO_CP_TENANT_KEY`.
- **Control Plane:** `upstreams.json` sets all three to `proxy`; health probes are healthy. Standalone
  `kms-phase1`, `flowcase` and `agent-studio` stayed up.

## Not run / not claimed
- Interactive sign-in **on the VM** (credentials are in the VM's `secrets/demo-users.txt`; sign in yourself).
- Live contract tests against running KMS/Flowcase/Agent Studio verifiers (none run in verifying mode).
- Staging SSO with a corporate IdP; load/failover; formal accessibility audit; multi-instance rate limiting.

## Security gaps / next steps
1. ~~Flowcase~~ done 2026-10-31.
2. ~~KMS verifier, Agent Studio token adapter~~ done 2026-10-31. Remaining: KMS delegated retrieval in token mode.
3. TLS for the VM (or an ingress) to get `Secure`/`__Host-` cookies. Corporate IdP when available.
4. Ingress/Redis rate limiting for multi-instance deployments. An outbox consumer (bus/webhook). Audit retention policy.
