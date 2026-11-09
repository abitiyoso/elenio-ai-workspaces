# AI Workspace Control Plane — Implementation Package v1.0

**Purpose:** Deliver the **platform foundation** missing between reusable services (KMS, Flowcase, Agent Studio) and their users. The Control Plane provides verified SSO/session integration, workspace membership, platform entitlements, mini-app registry, and one consistent launcher/shell. **Universal Case Workspace comes next**, as a reusable experience mini-app registered through this Control Plane.

## Package files

- `AI_Workspace_Control_Plane_Prototype.html` — standalone responsive HTML/CSS/JS interactive UX reference. Demo only; NO real login/Keycloak, APIs, token, authorization or proxy.
- `AI_WORKSPACE_CONTROL_PLANE_TECHNICAL_PRD.md` — scope, service ownership, identity architecture, RBAC, database, API, security, UAT and delivery plan.
- `CODEX_MASTER_PROMPT.md` — instructions to implement in Go + Postgres + React/TypeScript/Vite, phase-by-phase.
- `AUTH_AND_TRUST_BOUNDARIES.md` — backend-for-frontend architecture, OIDC PKCE, cookie sessions, token exchange, service auth and threats.
- `RBAC_AND_ENTITLEMENTS.md` — roles, mapping, example permission matrix, negative/abuse tests, and important boundary to domain ACL.
- `contracts/mini_app_manifest.schema.json` — versioned app registration contract.
- `contracts/auth_context.schema.json` — verified backend context passed to mini-app APIs, not browser-submitted.
- `contracts/authorization_decision.schema.json` — decision semantics, reason codes and audit context.
- `samples/mini_apps.json` — four example mini-apps: KMS, Flowcase, Agent Studio, Universal Case Workspace (planned).
- `samples/demo_tenants_users.json` — fictional users, workspace membership, role bindings; no credentials.
- `samples/rbac_policy.json` — illustrative entitlements and inheritance.
- `contracts/openapi_control_plane_draft.yaml` — proposed OpenAPI entry points for Go BFF.
- `INTEGRATION_AND_DEPLOYMENT_GUIDE.md` — exact wrapping and API boundary plan for KMS, Flowcase, Agent Studio.
- `UI_DESIGN_NOTES.md` — visual system, demo behavior, accessibility and production handoff.
- `QA_RESULTS.md` — browser smoke results and limits.
- `previews/` — desktop, catalog, access and mobile screenshots.
- `tests/ui_smoke.py` — Chromium/Playwright UI walkthrough and basic checks.

## Quick start

1. Open `AI_Workspace_Control_Plane_Prototype.html` in a browser to inspect the app shell and simulated access rules.
2. Give **all files** to Codex (and real KMS/Flowcase/Agent Studio contracts if available).
3. Paste `CODEX_MASTER_PROMPT.md`, instruct Codex to **implement Phase 0 only** initially; complete Phase 0 architecture/contract decisions before starting another phase.
4. Use the organization’s existing **OIDC SSO provider** if available; otherwise Keycloak is an optional provider, not a required extra service. Never build password storage or an identity server from scratch.
5. Run UI smoke locally: `python tests/ui_smoke.py` if Playwright + Chromium are installed. UI tests do not prove production auth/security.

## Explicit decisions

- **Go BFF/API + PostgreSQL + React/TypeScript** for app shell and control-plane metadata.
- **OIDC Authorization Code with PKCE**, server-side token exchange, `HttpOnly; Secure; SameSite` session cookies and CSRF/origin protection. Keep tokens out of browser localStorage.
- Shell-registered same-origin routes preferred for mini-apps at MVP. **Never treat iframe embedding as a security sandbox or authority**. Whitelist origins; no arbitrary redirect URLs.
- Control Plane owns **user/workspace membership and app entitlements**, NOT KMS document ACLs, Flowcase case permissions, or Agent Studio agent-model/tool approval.
- The UI hides unauthorized apps for clarity, but each downstream API **must independently verify authenticated identity, audience, tenancy and resource/action authorization**.
- Sample data and screenshots are illustrative only. The prototype simulates app launches and admin changes in local browser state—there is no real SSO, API proxy or service integration.

## Standards references (verified October 2026)

- OAuth 2.0 for Browser-Based Applications **RFC 10017**: https://www.rfc-editor.org/rfc/rfc10017.html
- OAuth 2.0 Security Best Current Practice **RFC 9700**: https://www.rfc-editor.org/rfc/rfc9700.html
- Keycloak OIDC (optional reference IdP): https://www.keycloak.org/securing-apps/oidc-layers

## Production rollout order

**Control Plane** → migrate/wrap KMS, Flowcase, Agent Studio identity adapters one by one → verify negative authorization tests → **Universal Case Workspace** (experience layer) → reusable structured intake → SIM Tender Domain Pack.
