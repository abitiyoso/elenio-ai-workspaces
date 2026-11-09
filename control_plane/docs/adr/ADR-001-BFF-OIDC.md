# ADR-001 — Go BFF as confidential OIDC client with server-side sessions

**Status:** Accepted (implemented) · 2026-10-30

## Context
Phase 0 inspection found **no corporate OIDC provider** reachable from this environment: the three services
run with development identities (KMS `mock`, Flowcase `dev_mock`, Agent Studio Tailscale-Serve headers +
access map). The master prompt says: use corporate OIDC if it exists, otherwise Keycloak. RFC 10017 (BFF) and
RFC 9700 apply.

## Decision
- **IdP:** Keycloak 26 (realm `workspace`, confidential client `control-plane`) is the fallback IdP for the
  tailnet deployment (`deploy/keycloak/`). The BFF is IdP-agnostic: any OIDC issuer with discovery, JWKS and
  PKCE S256 can replace it by changing `OIDC_ISSUER_URL`/`OIDC_CLIENT_ID`/`OIDC_CLIENT_SECRET_FILE`.
- **Flow:** Authorization Code + PKCE S256 + `state` + `nonce` (`internal/identity/oidc`, `internal/http/auth.go`).
  - `state`, `nonce`, verifier are random 256-bit; stored server-side in `auth_flows` (state & browser-binding as
    SHA-256, verifier AES-GCM sealed), 10-minute TTL, consumed atomically (replay → `state_invalid`).
  - A short-lived HttpOnly binding cookie ties the flow to the browser that started it (login CSRF defence).
  - RFC 9207 `iss` callback parameter must equal the issuer (mix-up defence); exact redirect URI derived from
    `PUBLIC_BASE_URL` (config refuses any other value).
  - ID token: signature via JWKS, `iss`, `aud` (+`azp` when multiple), `exp`, `nonce` (go-oidc + explicit checks).
- **Tokens stay server-side.** The refresh token is sealed in `sessions.refresh_token_enc` (only to revoke it at
  logout); access/ID tokens are not stored and never reach the browser.
- **Session:** opaque 256-bit cookie; DB stores SHA-256 only. `__Host-cp_session; Secure; HttpOnly; SameSite=Lax; Path=/`
  over HTTPS. New session id on every login (old one revoked → no fixation). Idle 30 min (sliding), absolute 8 h.
  Logout revokes server-side, revokes the refresh token at the IdP (RFC 7009) and returns the RP-initiated logout
  URL (`client_id` + `post_logout_redirect_uri`; no `id_token_hint`, so no token in a URL).
- **CSRF:** every unsafe method needs `Origin` (or same-origin `Referer`) equal to the public origin **and**
  `X-CSRF-Token` = HMAC(session id); JSON bodies require `application/json`.
- **Identity binding:** directory key is `(idp_issuer, idp_subject)`; email is display data. First login JIT-creates
  a user with no memberships (no access until an admin adds them). Suspended users are refused at callback and
  on every request (sessions revoked).
- **No passwords/MFA** in the Control Plane. **No dev login stub exists** — even the tailnet deployment uses real
  OIDC. Production refuses: `DEV_SEED`, `DEV_MOCK_AUTH`/`DEV_LOGIN`, http origins/issuers, missing key files,
  missing `TRUSTED_APP_UPSTREAMS` (`internal/platform/config`).

## Consequences
- The tailnet VM has no TLS certificates (no `tailscale cert`, no sudo), so it runs over HTTP with
  `APP_ENV=development` + `COOKIE_INSECURE_DEV_ACKNOWLEDGE` (cookie `cp_session`, not Secure). This is the only
  development concession; production config cannot express it.
- IdP outage: new logins fail closed with a 503 page; existing sessions continue until their own expiry.
