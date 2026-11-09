# Authentication Architecture — AI Workspace Control Plane

## Recommended default: Go BFF + enterprise OIDC

Use an existing approved OIDC Identity Provider; Keycloak only if corporate SSO is absent. **Do not build username/password storage or MFA.** Browser runs the React shell while the Go BFF is the confidential OAuth/OIDC client and holds tokens server-side. This follows RFC 10017's BFF architecture and RFC 9700 security guidance.

```text
  User Browser                       Corporate IdP / Keycloak
  1. GET /auth/login --------------------------> 2. authorize
  4. GET /auth/callback?code&state <------------ 3. auth code
                |
       Go BFF: verify state, nonce, issuer, PKCE, redirect URI
       Server-to-server code/token exchange; validate JWT/JWKS
                |
       Store access/refresh tokens securely on server
       Browser receives opaque HttpOnly Secure SameSite session cookie
                |
  5. GET /api/v1/me (cookie) --> BFF verifies session/membership
  6. GET /api/v1/apps --> BFF computes visible app entitlements
  7. GET /apps/flowcase --> registered host route + backend authz
       Flowcase resource API independently verifies audience and permissions
```

## Trust boundaries

1. **Browser is untrusted:** never accept `user_id`, `role`, `tenant`, `workspace` or `permissions` supplied in JSON/headers/postMessage as authoritative. UI route guards are usability only.
2. **BFF is the OIDC client:** verify `state`, S256 PKCE verifier, OIDC nonce, `iss`, `aud`, `exp`, signature, JWKS trust and callback match; prevent token mix-up and unregistered redirects. Use exact allowed redirect URIs and no open redirect.
3. **Session cookie:** random 256-bit opaque ID, `__Host-` prefix where feasible, `Secure`, `HttpOnly`, `SameSite=Lax|Strict`, `Path=/`, no Domain attribute; store only hash in PostgreSQL. Rotate on auth/elevation. Idle and absolute expiry. Revoke on logout/disable.
4. **CSRF:** enforce Origin/Referer and anti-CSRF token for mutating endpoints, including workspace switching, role grants, app publish/disable and logout. SameSite alone is defense-in-depth.
5. **Server-to-server:** validated BFF/edge constructs short-lived, audience-restricted identity context or token exchange as allowed by corporate IdP. Core service checks JWT, audience, scopes, tenant/workspace, and its own domain resource ACL. Avoid handing BFF-wide access tokens to all apps. Implemented: each `/apps/<key>/` request carries a fresh ES256 token with `aud=<key>`; an experience app that must call another core as the same user uses the **delegated token exchange** (below, ADR-004) — never the signing key, a multi-audience token or a service credential that can act as any user.
6. **Mini-app registry:** a published app manifest never determines arbitrary proxy destination. Allowed routes map to configured, trusted upstream targets. No attacker-controlled `next` URL, wildcard origin or implicit iframe trust.
7. **Admin controls:** platform admin not equal to IdP admin; only IdP authenticates subjects. Admin grants scoped permissions, can require stronger recent-auth/SoD; audit all changes.
8. **Service accounts:** separate principal type from humans. A service identity may invoke an agent task but cannot approve human business decisions.
9. **Secrets:** IdP client secret, provider tokens and signing keys are stored in server secret manager; DB holds references/metadata only. Never log tokens/cookies, and strip Authorization headers from audit.
10. **Revocation:** membership/user suspension and app disable must propagate to BFF and all domain services within an agreed policy interval; do not trust stale positive decisions indefinitely.

## App wrapping modes

### Preferred P0: same-origin route

React app launcher navigates to a registered path like `/apps/kms/`. Host BFF or trusted ingress routes to KMS frontend/API; service uses a verified identity contract. Separate deployments are allowed. Build a **known app allowlist**, not an open reverse proxy.

### Alternative: separate trusted origin

A distinct application origin must use a deliberate OIDC/BFF integration architecture and audience verification. It cannot rely solely on a parent iframe posting identity or passing tokens in query params. Cross-site cookie policies, CSP, CORS and logout require explicit review.

### Iframe

Only consider if mandatory after threat modeling. Configure strict CSP `frame-ancestors`, sandbox flags, origins and message event verification. `postMessage` never transfers bearer tokens or grants authority. Browser cross-origin embedding is not an auth boundary.

## Specific integration boundaries

| Core | Allowed verified context | Must still enforce locally |
|---|---|---|
| KMS | subject, tenant/workspace, app/open scope, correlation | source ACL, document classification, collection permissions, version access |
| Flowcase | subject, tenant/workspace, case/workflow context | case/task access, gate policy, SoD, artifact revision approval |
| Agent Studio | subject, tenant/workspace, agent scopes | agent/skill/tool/model version, release authority, tool execution permissions |
| Universal Case Workspace | CP token `aud=case-workspace` (same host session); user-scoped Flowcase/KMS tokens only via the CP token exchange (`act.sub=app:case-workspace`) | per-module domain permission; the cores enforce their own ACL on every exchanged-token call |

## Delegated token exchange (experience apps, ADR-004)

`POST /api/v1/token-exchange` lets an allowlisted experience-app BFF (today only `case-workspace`) swap the token
it received through its own `/apps/<client>/` route for a ≤ 5 min token towards one allowlisted core
(`flowcase`, `kms`), for the same user, session and workspace.

```text
Browser --cookie--> CP /apps/case-workspace/*  --Bearer aud=case-workspace-->  UCW BFF
UCW BFF --Basic client secret + subject_token--> CP /api/v1/token-exchange
        CP: verify own signature/iss/exp/lifetime, aud == client, human, no act claim
            session behind sid still live + same workspace, user active
            engine.App(user, ws, case-workspace) AND engine.App(user, ws, flowcase) allow
        <-- {access_token: aud=flowcase, act.sub=app:case-workspace, exp <= 5 min}
UCW BFF --Bearer aud=flowcase--> Flowcase (own verifier + own case/task ACL)
```

Boundaries:
- The client secret is deployment config (`TOKEN_EXCHANGE_CLIENTS`, hash only in the CP); the endpoint is not
  cookie-authenticated, so a browser session alone obtains nothing.
- A client can only exchange tokens minted for itself, only towards its allowlisted audiences, and only when the
  user could open the target app directly; tokens that already carry `act` are refused (no chaining).
- Logout, session/membership revocation, entitlement deny overrides and app disable stop the next exchange.
- Every attempt is audited (`token.exchanged` / `token.exchange_denied`).

## High-risk negative tests

- Forged `X-User-ID`, `X-Tenant-ID`, `X-Workspace`, iframe `postMessage`, invalid/expired cookie, session fixation.
- OAuth code injection, state replay, wrong issuer, invalid `nonce`, redirect URI mismatch, login/logout CSRF.
- Cross-tenant workspace switching, direct app URL after entitlement revoke, stale role after admin removal.
- API calls using a token with wrong `aud` or a service identity used as human reviewer.
- Token exchange with a wrong client secret, an unlisted audience, a subject token minted for another app, an
  exchanged (`act`) token, a revoked session, or a user who cannot open the target app (`tests/integration/exchange_test.go`).
- Route registry entries containing `..`, protocol-relative URL `//`, `%2e%2e`, javascript schemes, wildcard upstream destinations.
- Open app tab with privileged session while membership revoked, refresh must deny within policy window.

## Official references (validated Oct 2026)

- RFC 10017 (Aug 2026): https://www.rfc-editor.org/rfc/rfc10017.html
- RFC 9700 (Jan 2025): https://www.rfc-editor.org/rfc/rfc9700.html
- Keycloak OIDC: https://www.keycloak.org/securing-apps/oidc-layers
