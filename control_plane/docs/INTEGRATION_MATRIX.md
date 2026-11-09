# Integration matrix (Phase 0 findings, verified 2026-10-30)

Inspected: `../kms_go_package`, `../flowcase_platform`, `../ai_agent_studio` source and the running
`kibi-vm` (100.126.146.52, Docker Compose, Tailscale; no sudo, no Tailscale TLS certificates).

| | KMS (Knowledge Hub) | Flowcase | Agent Studio |
|---|---|---|---|
| Repo / stack | Go API + worker, React/Vite, Postgres, Elasticsearch, SeaweedFS, Docling | Go API + worker, React/Vite, Postgres | Go API, Postgres, encrypted vault |
| VM endpoints | web `100.126.146.52:5173` (Vite dev), API `127.0.0.1:8080` | web `100.126.146.52:5180`, API `127.0.0.1:8180` | API/UI `100.126.146.52:5190` |
| Health | `GET /readyz` (200) | `GET /readyz` (200) | `GET /healthz` (200) |
| Current auth adapter | `AuthAdapter` with `TrustedSessionVerifier` **interface only — returns "not configured"**; VM runs `AUTH_MODE=mock` | `AUTH_MODE=trusted_jwt` **implemented**: Bearer JWS RS256/ES256, `kid` + JWKS, exact `iss`/`aud`, `exp`/`iat`, TTL ≤ 15 m, tenant claim `tenant_id`; roles from Flowcase's own `role_assignments`; VM runs `dev_mock` | Tailscale Serve `Tailscale-User-Login` header + JSON access map; no signed-token verifier |
| Control Plane token compatible? | Not yet (needs verifier) | **Yes** — CP tokens satisfy the trusted_jwt checks (ES256, `kid`, `iss`, `aud`, ≤15 m, `tenant_id`); verified by `tests/integration` with a verifier mirroring Flowcase's options | No — CP will not forge Tailscale headers |
| Directory mapping needed downstream | KMS tenant/membership from verified identity | Flowcase `users(issuer, subject)` must contain CP issuer + CP user id (`sub`), tenant `external_ref` = CP `tenant_id` (tenant key) | access-map login → would need a token adapter |
| Sub-path serving (`/apps/<key>/`) | Frontend assumes `/` | Frontend assumes `/` (nginx at its own port) | assumes `/` |
| Domain ACL still enforced by the service | collection/source ACL, classification, versions | case/task/gate, SoD, approval delegation | agent/skill/tool/model, release authority |
| **Status in this deployment** | **`proxy`** via `kms-cp` (2026-10-31) | **`proxy`** via `flowcase-cp` (2026-10-31) | **`proxy`** via `agent-studio-cp` (2026-10-31) |

## Flowcase — integrated (2026-10-31)
- **Instance:** compose project `flowcase-cp` (`deploy/flowcase-cp/`), separate from the standalone `flowcase`
  project on :5180, which keeps its development identity for the Flowcase team. Web on `127.0.0.1:5280`
  (built with `FLOWCASE_BASE_PATH=/apps/flowcase/`), API on `127.0.0.1:8280`, own PostgreSQL. All loopback, so
  it is reachable only through the Control Plane route.
- **Auth:** `AUTH_MODE=trusted_jwt`, `AUTH_ISSUER=http://100.126.146.52:5200`, `AUTH_AUDIENCE=flowcase`,
  `AUTH_JWKS_URL=<CP>/.well-known/jwks.json`, `AUTH_TENANT_CLAIM=tenant_id`. `APP_ENV=development` only because
  the JWKS URL is http on the tailnet.
- **Directory:** Flowcase tenant `external_ref=acme-demo`. Users are `(issuer=CP origin, subject=CP user id)`,
  provisioned by `deploy/flowcase-cp/provision.py` from `directory.json`. These are Flowcase's own role grants,
  never copied from Control Plane roles. Rafi: designer/publisher/tenant_admin. Maya: case_owner. Dimas:
  domain_approver with pack policy delegations. Nadia (platform admin) and Sari are deliberately absent, so
  Flowcase denies them.
- **Data:** SIM and PoC packs imported once with Flowcase's own importer. The importer identity is disabled afterwards.
- **Flowcase source change** (backward compatible): optional `FLOWCASE_BASE_PATH` (Vite `base`, router
  `basename`, API fetch prefix, Dockerfile build arg). Defaults to `/`; Flowcase typecheck, lint, 30/30 tests
  and build still pass.
- **Live contract test** (`deploy/contract-check.sh` → `tests/contract/tokencheck`, run on the VM with the CP
  signing key): now 17/17 (an nbf case was added; the `principal_type` check is off because Flowcase takes the
  principal kind from its own directory). The original 16/16 run was against
  both the web nginx and the API. Covered:
  - valid token accepted;
  - no token, and forged `X-User-ID`/dev-persona headers rejected;
  - wrong `aud` (agent-studio, kms) and wrong `iss` rejected;
  - expired tokens and lifetime > 15 m rejected;
  - foreign key with the same `kid`, `alg:none` and HS256 confusion rejected;
  - another tenant and an unprovisioned subject rejected;
  - Flowcase's own role check: a member cannot create workflows; a designer passes authorization.
- **Re-deploy:** `deploy/flowcase-cp/deploy.sh` (rebuilds from `../flowcase_platform`, re-runs provisioning).

## KMS — integrated (2026-10-31)
- **Instance:** compose project `kms-cp` (`deploy/kms-cp/`), with its own PostgreSQL, Elasticsearch (384 MB heap),
  SeaweedFS bucket and index `kms_cp_chunks_v1`. It reuses the shared Docling service over its `docling_default`
  network, with the key copied from the existing KMS `.env` on the VM without being printed. Web is on
  `127.0.0.1:5380` (`KMS_BASE_PATH=/apps/kms/`, nginx image from `deploy/kms-cp/web.Dockerfile`), API on
  `127.0.0.1:8380`. The `kms-phase1` project is untouched.
- **Auth (new KMS code):** `AUTH_MODE=control_plane_jwt`, `AUTH_ISSUER`, `AUTH_AUDIENCE=kms`, `AUTH_JWKS_URL`,
  `AUTH_TENANT_MAP=acme-demo=<kms tenant uuid>`. The verifier is in `backend/internal/auth/controlplane.go`.
  A JWKS or directory outage returns 503, not 401.
- **Directory:** new KMS table `tenant_principals` (migration `000004_trusted_principals`). An active row is
  required, and KMS roles (`platform_admin`, `knowledge_steward`) come only from it.
  `deploy/kms-cp/provision.py` + `directory.json` set it up:
  - Maya: `knowledge_steward`.
  - Sari and Rafi: no KMS role. The Control Plane platform admin is not a KMS admin.
  - Nadia: absent, so KMS denies her.
- **Known limitation:** the delegated service retrieval endpoint (`POST /api/v1/retrieval/query`) cannot be
  used in this mode. It expects a `kms_svc_` credential in the same `Authorization` header the user token now
  occupies. Fixing this needs a two-credential design.
- **Live contract test:** 19/19 against both web and API. Adds service `principal_type` and KMS domain checks:
  a member cannot create a collection, a steward passes, and the platform admin outsider gets 401.

## Agent Studio — integrated (2026-10-31)
- **Instance:** compose project `agent-studio-cp` (`deploy/agent-studio-cp/`), with its own database volume
  (the upstream compose pins the global name `agent_studio_db_data`, so it is not reused), vault, key and
  access map. App on `127.0.0.1:8290` (host network), DB on `127.0.0.1:55441`. The UI is built with
  `AGENT_STUDIO_WEB_BASE_PATH=/apps/agent-studio/`.
- **Auth (new Agent Studio code):** `AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt`, `AGENT_STUDIO_CP_ISSUER`,
  `AGENT_STUDIO_CP_JWKS_URL`, `AGENT_STUDIO_CP_AUDIENCE=agent-studio`, `AGENT_STUDIO_CP_TENANT_KEY=acme-demo`.
  - The tenant check was added after the first live run caught tokens for another tenant being accepted.
  - Tailscale headers are ignored in this mode.
  - Opaque service tokens still route to the service-token verifier.
- **Access map:** same format, with `login` = Control Plane user id, rendered by `render-access-map.py` from
  `directory.json`. Sari is an author (no release or publish permission); Rafi is read-only; Nadia is absent.
- **`AGENT_STUDIO_ENV=development`:** needed for the http JWKS URL. It also enables the runtime's dev mock model
  for this instance, as on the existing Agent Studio VM deployment.
- **Live contract test:** 19/19. Domain checks: the read-only member can list but not create agents, the author
  passes, and the author has no release authority.

## Real agent runtime — Contact Center case (2026-10-31)
Chain: **Flowcase** (`AGENT_RUNTIME_MODE=agent_studio`) → **Agent Studio** (`POST /api/v1/runs`, service token) → **9Router**
(`cx/gpt-6-luna[1m]`, tool calling) + **KMS** retrieval on behalf of the case owner → result back to Flowcase by polling and
signed callback (`/api/v1/agent-callbacks/agent-studio`).

| Piece | What was added | Where |
|---|---|---|
| Flowcase | `agent_studio` client: maps input to the SIM schema, sends the case owner as `delegated_user`, `:` keys, unpinned → active release; workflow `sim-tender` v2 adds the case brief fields; upstream artifacts flow to later agents | `flowcase_platform/internal/integrations/agent/agentstudio.go`, `internal/cases/agents.go`, `internal/packs/sim/sim_tender_v2.json` |
| Agent Studio | `delegated_user` on runs (migration `000004_run_delegated_user`), KMS user header guard, `case_id` not forwarded by default; capability-check bug fixed (json+tools probe) | `ai_agent_studio/internal/httpapi/runtime.go`, `internal/runtime/executor.go`, `internal/httpapi/model_security.go` |
| KMS | delegated service retrieval: scope `retrieval:delegate` + header `X-KMS-Delegated-User` (active principal, membership still enforced) | `kms_go_package/backend/internal/auth/delegation.go`, `cmd/kms-service-client --delegate` |
| Knowledge | 7 docs (DPI use-case/capability/solution catalogs, costing guide, Standard Project, Standard Costing, dummy POC) rendered to PDF, ingested by Docling, approved and published in collection "Telkom Digital DPI & Costing" | `deploy/kms-cp/seed-knowledge.py` |
| Agent Studio setup | provider + vault credential + deployment + probe + approvals (Nadia); SIM pack, bindings, evals (Sari); release requests (Sari). Domain review and ops decision are made by people in the UI, not scripted | `deploy/agent-studio-cp/setup-runtime.py` |
| Deploy wiring | Flowcase→AS service token (AS stores SHA-256), shared callback secret, KMS delegate credential, AS tenant = Flowcase tenant UUID; flowcase-cp on host network | `deploy/flowcase-cp/*`, `deploy/agent-studio-cp/deploy.sh` |

Evaluation gate on the real model: all six SIM agents passed 4/4. Proposal failed the critical `missing-evidence`
case once (`output_not_json`) and passed on retry, so watch for flakiness there.

Development seeding note: KMS documents were uploaded as Maya and approved as Sari by an operator script using
Control Plane tokens (`tests/contract/devtoken`). Agent Studio release review and approval were intentionally left
to people.

## What "pending" means here
Historical (before 2026-10-31): apps were registered and authorised but not forwarded while their instances did
not verify Control Plane tokens; forwarding would either be rejected or silently accepted by their development identities, which the
master prompt forbids ("never fake actual service status").

## Steps to switch an app to `proxy` (per app, one at a time)
1. Service: implement/enable a verifier for CP tokens (Flowcase: set `AUTH_MODE=trusted_jwt`,
   `AUTH_ISSUER=<CP public origin>`, `AUTH_AUDIENCE=flowcase`, `AUTH_JWKS_URL=<CP>/.well-known/jwks.json`,
   `AUTH_TENANT_CLAIM=tenant_id`; provision users with `issuer=<CP origin>`, `subject=<CP user id>`).
2. Service: serve its UI and API under `/apps/<key>/` (Vite `base`, router basename, API base path), or set
   `strip_prefix: true` with prefix-tolerant assets.
3. Ops: change that app's entry in `upstreams.json` to `{"mode":"proxy","url":"http://127.0.0.1:<port>","audience":"<key>"}` and restart the API.
4. Run the contract negatives against the live service: wrong `aud`, expired token, forged `X-User-ID`,
   revoked membership, cross-workspace access.

## Universal Case Workspace — Control Plane side ready (2026-10-31)
- **Registry:** `case-workspace` 0.2.0 **published** (`deploy/dev-seed.json`; the existing VM row moves from draft
  0.1.0 on the next API start via the seed upgrade path, see `docs/DEPLOYMENT.md`). MVP: case workbench over
  Flowcase cases/tasks, intake chat with the Case Intake Assistant (Agent Studio), author draft review.
  Entitlement `case-workspace:open`: `case_worker`, `case_reviewer`, `platform_admin` (Maya, Dimas, Rafi, Nadia;
  not Sari).
- **Upstream:** `{"mode":"proxy","url":"http://127.0.0.1:8480","health_url":"http://127.0.0.1:8480/readyz",
  "audience":"case-workspace","strip_prefix":true}` (instance `case-workspace-cp`, `deploy/case-workspace-cp`,
  owned by the UCW team). Until that instance runs, the route answers 502.
- **Identity in:** the UCW BFF verifies the CP token like the other apps (ES256 via
  `<CP>/.well-known/jwks.json`, `iss=<CP origin>`, `aud=case-workspace`, lifetime ≤ 15 min, `tenant_id`,
  `principal_type=human`). UCW keeps **no user directory**: the CP decides who may open it.
- **Identity out (Flowcase now, KMS later):** per user request the BFF calls
  `POST <CP>/api/v1/token-exchange` with `Authorization: Basic base64("case-workspace:<secret>")` and
  `{"subject_token": "<the inbound CP token>", "audience": "flowcase"}` (forward `X-Request-ID`), then calls
  Flowcase with the returned ≤ 5 min token (`aud=flowcase`, same `sub`/`sid`/workspace, `act.sub=app:case-workspace`).
  Cache per (subject-token `jti`, audience) at most until its `exp`; never across users. The CP re-checks the live
  session, the user's access to UCW **and** to Flowcase/KMS, so UCW can never show data the user could not open
  directly. Exchange errors (401/403, see `docs/API.md`) must be surfaced as unavailable/partial, never retried
  with other credentials. Flowcase, KMS and Agent Studio verifiers ignore the extra `act` claim (checked in their
  verifier code: Flowcase `jwt.MapClaims`, KMS typed claims via golang-jwt, Agent Studio `json.Decoder` without
  `DisallowUnknownFields`).
- **Client secret:** generated once by `deploy/deploy-vm.sh` at `~/control-plane/secrets/token_exchange_case_workspace`
  (0600, 64 hex chars, no newline); the CP only gets its SHA-256 (`TOKEN_EXCHANGE_CLIENTS_FILE`). The UCW deploy
  copies the file; the CP endpoint is reachable at `CP_PUBLIC_URL` (`http://100.126.146.52:5200`).
- **Contract check:** `deploy/contract-check.sh case-workspace` runs the fixed negative suite against
  `/api/v1/workspace/me/context` with `-outsider-check=false`, plus: a validly signed member token whose session is
  not live (`sid` of "contract-check") gets `401` from `GET /api/v1/workspace/cases` (the exchange refuses it). The
  probe must still return 2xx with `flowcase.available=false` and `partial_errors`.
