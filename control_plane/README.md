# AI Workspace Control Plane

Shared identity + mini-app wrapping foundation for KMS, Flowcase and Agent Studio: Go BFF (OIDC code + PKCE,
server-side sessions, CSRF), PostgreSQL workspaces/memberships/RBAC with dual control, an immutable mini-app
registry, authorised same-origin `/apps/<key>/` routes with audience-bound ES256 tokens, append-only audit,
and a React/TypeScript shell. **Status: Phases 0–4 implemented; deployed to kibi-vm (tailnet, development mode).**

- Package/spec docs: `README_START_HERE.md`, `AI_WORKSPACE_CONTROL_PLANE_TECHNICAL_PRD.md`, `CODEX_MASTER_PROMPT.md`
- Decisions: `docs/adr/ADR-001-BFF-OIDC.md`, `ADR-002-APP-ROUTING.md`, `ADR-003-AUTHZ-OWNERSHIP.md`
- Findings & gaps: `docs/INTEGRATION_MATRIX.md`, `docs/OPEN_DECISIONS.md`, `docs/IMPLEMENTATION_REPORT.md`
- API: `docs/API.md`, `api/openapi.yaml`, wire types `web/src/api/types.ts`
- Ops: `docs/DEPLOYMENT.md`, `docs/OPERATIONS.md`

## Layout
```text
cmd/control-plane-api           serve | migrate | seed-dev | bootstrap-admin | gen-signing-key
internal/platform/{config,postgres,observability,secrets,apperr}
internal/identity/{oidc,session,principal,provisioning}
internal/authorization          default-deny decision engine + fact loader
internal/workspaces             directory, workspaces, memberships, role bindings, grant requests, overrides
internal/apps                   manifest validator, registry lifecycle, catalog, launch
internal/integrations           service-token issuer (JWKS), trusted upstreams, health probes
internal/audit                  append-only audit + transactional outbox
internal/http                   routes, middleware, auth handlers, admin API, /apps proxy, SPA
migrations/                     embedded SQL
web/                            React 19 + Vite shell and admin UI
deploy/                         VM compose, Keycloak realm, dev seed, deploy + backup scripts
tests/integration               real server + PostgreSQL + mock OIDC provider
```

## Run checks
```bash
make db-up              # PostgreSQL 16 on 127.0.0.1:55436 for tests
make check              # Go unit + integration (CP_REQUIRE_DB=1), gofmt, vet, web typecheck/lint/test/build
```

## Run locally
Needs an OIDC provider (e.g. Keycloak with `deploy/keycloak/realm-workspace.template.json` rendered by
`deploy/keycloak/render-realm.py`). See `docs/DEPLOYMENT.md` for variables; development over plain HTTP needs
`APP_ENV=development` and `COOKIE_INSECURE_DEV_ACKNOWLEDGE=i-understand-cookies-are-not-secure`.
`cd web && npm run dev` serves the shell on :5174 and proxies to the BFF on :8200.
