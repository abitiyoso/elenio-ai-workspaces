# Deployment

## Tailnet VM (`kibi-vm`, current)

```bash
deploy/deploy-vm.sh            # or: make deploy-vm VM=user@host
```

| Component | Where | Notes |
|---|---|---|
| Shell + BFF | `http://100.126.146.52:5200` | Go binary in distroless image, host network, binds the Tailscale IP only |
| Keycloak 26 | `http://100.126.146.52:5201` | realm `workspace`, confidential client `control-plane`, PKCE S256 enforced, exact redirect URI |
| PostgreSQL 16 | `127.0.0.1:55440` (loopback) | databases `control_plane` and `keycloak` (separate roles) |
| Metrics | `127.0.0.1:9201/metrics` | Prometheus text, loopback only |
| Flowcase (CP-integrated) | web `127.0.0.1:5280`, API `127.0.0.1:8280` | compose project `flowcase-cp`, `deploy/flowcase-cp/deploy.sh`, served at `/apps/flowcase/` |
| KMS (CP-integrated) | web `127.0.0.1:5380`, API `127.0.0.1:8380` | compose project `kms-cp`, `deploy/kms-cp/deploy.sh`, served at `/apps/kms/` |
| Agent Studio (CP-integrated) | app `127.0.0.1:8290`, DB `127.0.0.1:55441` | compose project `agent-studio-cp`, `deploy/agent-studio-cp/deploy.sh`, served at `/apps/agent-studio/` |

| Universal Case Workspace (CP-integrated) | BFF `127.0.0.1:8480` | compose project `case-workspace-cp` (`deploy/case-workspace-cp`, built by the UCW team), served at `/apps/case-workspace/`; calls Flowcase/KMS via the CP token exchange |

Live contract checks: `deploy/contract-check.sh [flowcase|kms|agent-studio|case-workspace]` (`case-workspace` is
not in the default list until it is deployed).

Compose project `control-plane` (`deploy/compose.vm.yaml`) shares nothing with the `kms-phase1`, `flowcase` or
`agent-studio` projects (own network, volume, ports). Releases live in `~/control-plane/releases/<UTC ts>`,
`~/control-plane/current` points at the active one, the last five are kept. Private state outside releases:

- `~/control-plane/.env` (0600): database/Keycloak passwords, bind IP, public URLs, UID/GID.
- `~/control-plane/secrets/` (0711 dir, 0600 files): `oidc_client_secret`, `csrf_hmac_key`,
  `token_encryption_key`, `service_token_signing_key` (ES256 PKCS#8), `database_url`, `keycloak_admin_password`,
  rendered `realm-workspace.json` (0644, contains the client secret; readable by the Keycloak container), and
  `demo-users.txt` — sign-in passwords for the fictional demo users (rafi, nadia, maya, dimas, sari).
- `~/control-plane/secrets/token_exchange_case_workspace` (0600): plaintext token-exchange client secret for
  `case-workspace` (64 hex chars, no trailing newline), generated once. The UCW deploy copies it into its own
  secrets and sends it as `Authorization: Basic base64("case-workspace:<secret>")`. Never printed by the scripts.
- `~/control-plane/config/upstreams.json`: trusted mini-app targets (`TRUSTED_APP_UPSTREAMS`). Edited by ops;
  each deploy only **adds** app keys from `deploy/upstreams.vm.json` that are missing (never overwrites entries).
- `~/control-plane/config/token_exchange_clients.json`: `TOKEN_EXCHANGE_CLIENTS` — rewritten on every deploy from
  the secret file: `{"case-workspace": {"secret_sha256": "<sha256 hex>", "audiences": ["flowcase", "kms"]}}`.

Registry changes in `deploy/dev-seed.json` reach an existing database on the next API start (`DEV_SEED=true`):
users/workspaces/bindings are only added, and an app whose seed `version` is greater than its latest registered
version gets that version added (and published when the seed says `published`), audited as `app.published` by the
system. Disabled/retired apps and equal/lower versions are left alone. This is how `case-workspace` moves from the
draft 0.1.0 to the published 0.2.0. Role-permission changes ship as migrations (`0003_case_workspace_entitlements`).

To rotate the case-workspace exchange secret: delete the secret file, run `deploy/deploy-vm.sh` (new secret + hash),
then redeploy the UCW so it copies the new value; exchanges fail with `INVALID_CLIENT` in between.

Why this is `APP_ENV=development`: the VM has no TLS certificates and no sudo, so the origin is HTTP and the
session cookie cannot be `Secure`/`__Host-`; the illustrative seed is loaded (`DEV_SEED=true`). Authentication
itself is real OIDC with real sessions, CSRF, RBAC and audit. All three settings are refused by production config.

Rollback: `cd ~/control-plane/releases/<previous> && CP_RELEASE=<previous> docker compose --env-file ~/control-plane/.env -f compose.vm.yaml up -d`.
Migrations are additive; restore from backup if a rollback crosses a destructive migration (none exist yet).

## Production (target)

- Terminate HTTPS at the approved ingress; `PUBLIC_BASE_URL=https://workspace.example`; the BFF then issues
  `__Host-cp_session; Secure` and sends HSTS.
- Corporate OIDC issuer (https) or a managed Keycloak; register exactly `https://workspace.example/auth/callback`
  and `/signed-out` as post-logout URI; enforce PKCE S256 on the client.
- Mount secrets from the secret manager as files (`*_FILE`); never environment variables in manifests.
- `APP_ENV=production`, no `DEV_SEED`, `TRUSTED_APP_UPSTREAMS_FILE` present (startup refuses otherwise).
- First admin: `control-plane-api bootstrap-admin -subject <idp sub> -name "<name>"` (refuses if any platform admin
  exists), then grant a second admin through the dual-control flow.
- Run ≥ 2 replicas behind the ingress: sessions are in PostgreSQL; move rate limiting to the ingress (or Redis).
- Image: root `Dockerfile` (multi-stage, distroless, non-root).

## Configuration reference

| Variable | Default | Purpose |
|---|---|---|
| `APP_ENV` | `production` | `development` enables only: http origin with acknowledgement, dev seed, ephemeral keys |
| `PUBLIC_BASE_URL` | – | public origin; CSRF origin check, redirect URI, token issuer |
| `HTTP_ADDRESS` / `METRICS_ADDRESS` | `:8200` / `127.0.0.1:9200` | listeners |
| `DATABASE_URL[_FILE]` | – | PostgreSQL DSN |
| `TENANT_KEY`, `TENANT_NAME` | – | single tenant of this deployment |
| `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET_FILE` | – | IdP |
| `OIDC_SCOPES` | `openid profile email` | must include `openid` |
| `CSRF_HMAC_KEY_FILE`, `TOKEN_ENCRYPTION_KEY_FILE` | required in prod | ≥ 32 bytes |
| `SERVICE_TOKEN_SIGNING_KEY_FILE` | required in prod | ES256 private key (`gen-signing-key`) |
| `SERVICE_TOKEN_PREVIOUS_JWKS_FILE` | – | previous public keys kept in JWKS during rotation |
| `SERVICE_TOKEN_TTL` | `5m` | ≤ 15m |
| `SESSION_IDLE_TTL` / `SESSION_ABSOLUTE_TTL` / `AUTH_FLOW_TTL` | `30m` / `8h` / `10m` | |
| `TRUSTED_APP_UPSTREAMS[_FILE]` | – | JSON `{app_key: {mode, url, health_url, audience, contract, pending_reason, strip_prefix}}` |
| `TOKEN_EXCHANGE_CLIENTS[_FILE]` | – (exchange disabled) | JSON `{client_id: {secret_sha256, audiences: [app_key…]}}`; client must be a proxied upstream with `audience` = its key; audiences must be other proxied upstreams; no wildcards; production refuses `sha256("")` (ADR-004) |
| `RATE_LIMIT_AUTH_RPS` / `_API_RPS` / `_ADMIN_WRITE_RPS` | `2` / `20` / `2` | per client IP / session |
| `APP_REGISTRY_ENVIRONMENT` | `dev` in development, else `production` | accepted manifest `environment` |
| `DEV_SEED`, `DEV_SEED_FILE` | `false` | development only |
| `COOKIE_INSECURE_DEV_ACKNOWLEDGE` | – | must be `i-understand-cookies-are-not-secure` for http in development |
