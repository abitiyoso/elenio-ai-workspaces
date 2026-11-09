# Phase 1 Scope and Readiness

Date: 2026-10-30. This note records the implemented registry surface and the
remaining gates for a complete, operational Phase 1 deployment.

## Implemented

- PostgreSQL registry schema with tenant RLS, transaction-local tenant context,
  append-only audit events, immutable published versions, and draft-only agent
  bindings. The dedicated deployment uses a separate database volume and a
  non-owner, non-superuser runtime role.
- Tenant-scoped provider/model metadata, agent list/create/archive/bootstrap,
  skill CRUD/version draft operations, agent-version draft operations, tenant
  skill/tool bindings, read-only global tool catalog, write-only provider
  credential rotation/removal, model approval, and model-binding handlers.
- React Models & Providers, Agent Registry, Skills Studio, and Tool Library
  surfaces use API data and explicit empty/error states without tenant fixtures.
- Go API entrypoint composes the registry/catalog/model-security handlers,
  serves the built frontend, exposes liveness/readiness endpoints, and binds
  only to loopback.
- Host identity uses Tailscale Serve's `Tailscale-User-Login` header, then an
  explicit file-backed access map resolves one login to a tenant UUID and exact
  actions. The initial mapping grants only the verified operator login. A
  tailnet login alone does not grant application access. This trusts the VM and
  its local processes; the header is not a signed token.
- A standalone Compose/Docker package creates an isolated Agent Studio database
  volume and keeps the app API on localhost:8190. Tailscale Serve forwarding on
  HTTPS 5190 is a separate host action and was not applied by this package lane.

## Explicitly withheld

- `skill.version.publish` and `agent.version.publish` permissions are absent
  from the initial access map because SME ownership and AI Ops release authority
  have not been established. The API routes fail closed without those actions.
- The platform-global tool catalog remains read-only. Tenant-owned tool drafts
  are stored in separate RLS-protected tables and are not executable or
  bindable until a separately approved bridge is implemented.
- Provider credentials are accepted write-only and encrypted through an
  AES-256-GCM file vault with a separately provisioned raw 32-byte master key;
  encrypted files persist outside the container release. The initial access map
  allows only the single operator to write/delete provider credentials.
- An OpenAI-compatible probe adapter is installed and the initial map grants
  the operator probe permission. The exact outbound HTTPS host allowlist is
  `9router.kibicode.my.id`; requests occur only when the operator explicitly
  starts a probe after configuring a credential. Capability probes require a
  token-cost acknowledgement. Approval, model-binding, and version-publish
  routes remain fail-closed because those actions are absent from the map.
- The frontend exposes provider credential status/write/delete and explicit
  connectivity/capability probes. Approval status and model-binding actions
  remain unavailable because the initial map grants neither read nor write
  authority for those operations.
- No Flowcase/KMS live adapter, Eino runtime, or production model provider is
  implemented in Phase 1.

## Deployment gates

- The VM's current Docker/Tailscale state was inspected read-only. The isolated
  Agent Studio ports and volume do not overlap Flowcase or KMS. No deployment
  was performed from this implementation lane.
- Before deployment, the VM needs private owner/runtime DB password files, a
  raw 32-byte vault key, a private writable vault directory, and the explicit
  access-map file. The included map grants one verified user a dedicated project
  tenant UUID and Phase 1 editing/credential actions, but no probe, approval,
  model-binding, or publish authority.
- Tailscale Serve must forward HTTPS port 5190 to `http://127.0.0.1:8190`
  without replacing any existing Serve handler. The service remains inaccessible
  through its direct API listener because it binds to loopback.
- The migration ran against a temporary PostgreSQL 17 container for focused
  DB-backed acceptance checks; no schema or data was changed on the VM. Live
  health/login checks remain unverified. Eino and external Flowcase/KMS
  contracts remain Phase 2 dependencies.
- Probe adapter wiring and host allowlist are configured, but no live provider
  request has been made. Connectivity probes list model IDs; capability probes
  make small inference requests and can incur token charges after explicit
  acknowledgement.
- The deployment migration runner applies numbered `NNNNNN_name.up.sql` files
  in lexical order and records each version transactionally. The additive
  tenant tool draft schema is migration `000002_tenant_tool_drafts` and its
  tables receive explicit runtime DML grants while retaining tenant RLS.

## Validation evidence

Run on 2026-10-30 for release `p1-20261030-02`:

- `go build ./...`, `go vet ./...`, and `gofmt -l .` are clean.
- `go test -race -count=1 ./...` passes with the DB-backed acceptance tests
  enabled against a disposable PostgreSQL 17 container. Migrations were applied
  with `deploy/run-migrations.sh` and the tests connected as the non-superuser
  `agent_studio_app` role, so tenant RLS was active. Covered: cross-tenant leak
  attempts, published-version immutability, unauthorized editing, concurrent
  publish (one winner), tenant tool draft isolation, clone without authority,
  idempotent tenant-local six-agent SIM bootstrap, access-map identity and
  action checks, the development identity guard, and router collection routes.
- Frontend `npm ci` + `tsc -b && vite build` pass; the Docker image builds for
  linux/amd64 using the committed `package-lock.json`.
- Local end-to-end smoke: missing or unmapped identity returns 401, a withheld
  publish returns 403, a non-allowlisted provider URL returns 400, and the UI
  loads Models & Providers and Agent Registry from the API with no console
  errors.
- Fixed in this release: `GET/POST /api/v1/model-providers` and
  `/api/v1/model-deployments` were redirected (307) into the model-security
  subtree, breaking provider/deployment list and create. Exact routes now
  reach the registry handler; a regression test covers it.

## Deployment state

- Release `p1-20261030-03` runs on the VM through the existing `agent-studio`
  Compose project and `agent_studio_db_data` volume. Migrations exited 0 and
  `app`/`db` are healthy.
- Temporary no-login mode, chosen by the owner until host SSO or a service
  user exists: `AGENT_STUDIO_ENV=development`, `AGENT_STUDIO_DEV_LOGIN` set to
  the single mapped operator, and the app bound to `100.126.146.52:5190`
  (tailnet only; the LAN interface does not answer). Every tailnet device that
  can reach the VM acts as that operator with all mapped permissions,
  including credential write/delete and token-cost probes. The UI shows a
  "Mode dev · tanpa login" chip and `GET /api/v1/session` reports
  `identity_mode: development`.
- To leave this mode, remove the three `AGENT_STUDIO_ENV`,
  `AGENT_STUDIO_DEV_LOGIN`, and `AGENT_STUDIO_HTTP_ADDR` lines from the VM env
  file (backup: `.env.bak-p1-02`) and redeploy; the app returns to loopback
  with Tailscale Serve identity. A real SSO verifier replaces both later.
- The production tenant has no agents yet; run the SIM bootstrap from the
  Agent Registry page when ready. No live provider probe has been made.
