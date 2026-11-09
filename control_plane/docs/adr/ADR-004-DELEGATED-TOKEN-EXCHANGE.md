# ADR-004 — Delegated token exchange for experience apps

**Status:** Accepted (implemented) · 2026-10-31

## Context
Experience apps such as the Universal Case Workspace (UCW, `case-workspace`) are a BFF + SPA that must call core
services (Flowcase now, KMS later) **as the signed-in user** on every request. Through `/apps/case-workspace/*`
the UCW BFF only receives a Control Plane token with `aud=case-workspace` (ADR-002/003). Core services correctly
reject that audience, and the BFF has no other way to get a user-scoped token. Options considered:

- Forward the UCW token to Flowcase — would require Flowcase to accept a foreign audience (audience confusion).
- Give UCW the signing key or a long-lived service credential — UCW could then act as any user (what
  `tests/contract/devtoken` is, and why it is an operator-only tool).
- Mint several audiences into the proxy token — every app would receive tokens valid at every other app.
- Ask the corporate IdP for RFC 8693 exchange — not available (Keycloak dev realm, no IdP-issued app tokens).

## Decision
The Control Plane exposes a narrow, allowlisted exchange: `POST /api/v1/token-exchange` (loosely RFC 8693).

- **Server-to-server only.** Client authentication is HTTP Basic with a deployment-configured secret per client;
  the CP stores only `sha256(secret)` (`TOKEN_EXCHANGE_CLIENTS[_FILE]`) and compares in constant time. No cookie is
  read, so the cookie CSRF/Origin checks do not apply; failed client authentication is rate-limited per IP and
  successful exchanges per (client, session).
- **Clients and audiences come from deployment config,** validated at start: the client id must be a proxied
  trusted upstream whose audience is its own key; each allowed audience must be another proxied upstream with an
  audience; no wildcards; production refuses an empty secret.
- **Subject token** must be a token this CP issued (current or previous `kid`), ES256, exact `iss`, unexpired,
  lifetime ≤ 15 min, `aud` == client id (a client can only exchange tokens minted *for itself* by the app
  route), `principal_type=human`, the deployment tenant, and **no `act` claim** (no chaining).
- **Live state is re-checked:** the session behind `sid` (an HMAC of the session id under the verifying `kid`) must
  be active and still in the token's workspace; the user and tenant must be active.
- **Same decision as the proxy:** `engine.App` must allow the user to open the **client** app and the **target**
  app in that workspace. The exchange can therefore never give a user more than opening the target app directly.
- **Issued token:** same `sub`/tenant/workspace/`sid`/`auth_time`, `aud` = the target upstream's audience, scopes
  from the target decision, the request's correlation id, TTL ≤ min(issuer TTL, 5 min), plus
  `"act": {"sub": "app:<client_id>"}`. Existing verifiers (Flowcase `trusted_jwt`, KMS/Agent Studio
  `control_plane_jwt`) ignore unknown claims, so they accept it unchanged.
- **Audit and metrics:** every attempt writes `token.exchanged` or `token.exchange_denied` (actor type
  `service`, `service_id` = client, target = audience, reason code, `client_id` metadata); no token is returned
  if the allow row cannot be written. Counter `cp_token_exchanges_total{client,audience,outcome}`.

## Consequences
- Downstream services still authorise the user in their own domain (case/task ACL, SoD, collection ACL). They may
  use `act.sub` to recognise delegated calls (e.g. to forbid approvals performed through an experience app).
- Revocation (logout, admin session revoke, membership/entitlement change, app disable) stops new exchanges on the
  next request; already-issued exchanged tokens live at most 5 minutes.
- Validly signed tokens without a live session (`contract-check`, `devtoken`) cannot be exchanged, which the live
  contract check for `case-workspace` relies on.
- One more secret per experience app to rotate: rotate by replacing the secret file and redeploying both the CP
  (new hash) and the app. Multiple secrets per client (overlap during rotation) are not supported yet.
- The exchange is single-tenant like the rest of the deployment (`tenant_id` must equal `TENANT_KEY`).
