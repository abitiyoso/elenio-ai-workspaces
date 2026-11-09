# Integration and Deployment Guide — KMS / Flowcase / Agent Studio wrapping

This guide is a **proposal** for three currently designed platform services. The real deployment domains, API signatures, authentication adapters and readiness endpoints MUST be discovered in Codex Phase 0.

## Preferred topology

```text
  https://workspace.example.internal/
    /                  -> React Control Plane shell (BFF session)
    /api/v1/...        -> Go Control Plane BFF
    /auth/...          -> Go OIDC client / session endpoints
    /apps/kms/*        -> KMS frontend (trusted route; external deployment)
    /apps/flowcase/*   -> Flowcase frontend (trusted route; external deployment)
    /apps/agent-studio/* -> Agent Studio frontend (trusted route; external deployment)

  Go Control Plane / trusted ingress
    - Route allowlist, session check, membership and app entitlement
    - BFF/service-identity token exchange or narrow trusted auth proxy
    - Proper per-service audience + limited scope
    - No arbitrary destination from user input

  Core services
    - Validate verified subject and request scope independently
    - KMS: source/document ACL
    - Flowcase: case/task/approval/SoD
    - Agent Studio: agent/skill/tool/model/release policy
```

## Wrapping steps per service

1. **Inventory original app**: frontend base path, API origin, CORS/CSP, cookie assumptions, current auth middleware and deep-link behavior.
2. **Mount same-origin route** via trusted ingress configuration, not dynamic `target_url` in a user-supplied manifest.
3. **Issue audience-specific identity**: BFF validates session and app entitlement, then obtains constrained downstream identity credentials or uses approved verified ingress JWT. Never let the client forge identity via headers, `postMessage`, query or localStorage.
4. **Enforce domain authorization** inside each API. Return 403 when direct `/api/...` is called without sufficient case/knowledge/agent permissions even if the app UI is open.
5. **Contract test**: valid token, wrong `aud`, expired token, forged `X-User-*`, revoked member, cross-workspace and cross-tenant access.
6. **Configure logout**: invalidate local session and coordinate IdP logout where appropriate; don't assume all tabs or external origins have ended server-side sessions.
7. **Add a health adapter** that reports verified service connectivity; do not infer service health merely because the route exists.

## Per-core integration matrix

| Core | Manifest key/path example | Critical auth adapter boundary | Do NOT duplicate |
|---|---|---|---|
| KMS | `kms`, `/apps/kms/` | subject+workspace+collection ACL; source classification and allowed KMS operations | Docling, Elasticsearch, embeddings |
| Flowcase | `flowcase`, `/apps/flowcase/` | subject+case+task+approval delegation; immutable workflow version checks | BPM engine, gate/SME approval |
| Agent Studio | `agent-studio`, `/apps/agent-studio/` | subject+agent+tool/model access, release authority; service agent invocation | Eino runtime, LLM provider keys |
| Universal Case Workspace (future) | `case-workspace`, `/apps/case-workspace/` | read-only shell context + case/domain adapters | login, user directory, KMS/Flowcase/agent runtime |

## Rollout sequencing

1. Implement verified host SSO and membership management in staging.
2. Migrate **KMS** to trusted route + delegated subject; verify document ACL and no second-login flow.
3. Migrate **Flowcase** to trusted route; verify case/task/approval endpoint negatives and SoD.
4. Migrate **Agent Studio** to trusted route; verify model/tool/provider admin endpoints are still independently scoped.
5. Publish actual registry manifests only after app routes and health adapters are verified.
6. Register Universal Case Workspace as `draft`; build reusable experience layer next.

## Deployment knobs that remain external

- `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET_REF`, `OIDC_REDIRECT_URI`, and allowed issuer/TLS trust.
- `SESSION_COOKIE_NAME`, `SESSION_IDLE_TTL`, `SESSION_ABSOLUTE_TTL`, `SESSION_STORE_DSN_REF`, `CSRF_HMAC_KEY_REF`.
- `TRUSTED_APP_ROUTES` / Kubernetes ingress manifests mapping approved route base to fixed backend; these should NOT be derived from app registration JSON on each request.
- `KMS_TOKEN_AUDIENCE`, `FLOWCASE_TOKEN_AUDIENCE`, `AGENT_STUDIO_TOKEN_AUDIENCE`, subject delegation contract.
- `AUDIT_RETENTION`, `POLICY_CACHE_MAX_AGE`, app disable/revocation propagation SLO, CSP/CORS configuration.

Do not print secrets in logs or readiness responses. Production must refuse to start when `DEV_MOCK_AUTH=true` or trusted upstream configuration is absent.
