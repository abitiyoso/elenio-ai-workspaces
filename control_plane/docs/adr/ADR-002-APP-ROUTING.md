# ADR-002 — Same-origin mini-app routes behind an authorising BFF route

**Status:** Accepted (implemented) · 2026-10-30

## Decision
- Every registered mini-app lives at `/apps/<app_key>/` on the shell origin. **Same origin is possible and
  chosen** for P0; no iframes, no cross-origin embedding, no `postMessage` identity.
- The registry (`mini_apps`, immutable `mini_app_versions`, `mini_app_routes`) stores only validated relative
  routes. `route_base` must equal `/apps/<app_key>/`; the validator rejects `..`, `%2e`, `//`, schemes,
  backslashes, unknown fields (e.g. `upstream_url`) and markup.
- **Upstream targets come only from deployment config** (`TRUSTED_APP_UPSTREAMS`, read at start). A request or a
  manifest can never choose a destination. Each entry has `mode`:
  - `proxy` — the BFF forwards after authorisation, strips every inbound identity header (Cookie,
    Authorization, `X-User-*`, `X-Workspace-*`, `X-Role*`, `Tailscale-*`, forwarding headers) and injects a fresh
    audience-bound token (see ADR-003). Upstream `Set-Cookie` for `cp_*` names is dropped.
  - `pending` — the route is authorised but answered with an honest *Integration pending* page (503). Used
    until the service verifies Control Plane tokens.
- `/apps/<key>/*` re-runs the same decision as the catalog on **every request** (no cached allow), so revocation,
  suspension and app disable apply to the next request. Unauthenticated HTML navigation → login with a safe
  `return_to`; denials → 403 page/JSON and an `authz.denied` audit event. Unsafe methods through the route
  require same-origin `Origin`.
- Health is probed server-side from the configured `health_url` (status code only) and shown as route health.

## Why not iframe / separate origins
Iframes are not a security boundary and would need cross-site cookies, CSP `frame-ancestors` coordination and
message-origin verification. Separate origins need their own OIDC/BFF integration. Neither is needed for P0.

## Consequences
Mini-app frontends must be servable under a sub-path (`/apps/<key>/`) or the entry must use `strip_prefix` with
assets that tolerate it. Flowcase and KMS frontends currently assume `/` (see INTEGRATION_MATRIX).
