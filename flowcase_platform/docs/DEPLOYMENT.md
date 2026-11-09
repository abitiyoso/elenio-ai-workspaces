# Deployment

## Images

`Dockerfile` builds one distroless, non-root backend image with `/app/api`, `/app/worker`, `/app/migrate`.
`web/Dockerfile` builds the static UI served by unprivileged nginx with a strict CSP; it proxies `/api` to the API.

## Environment reference

| Variable | Production value / rule |
|---|---|
| `APP_ENV` | `production` (default). `development` enables dev-only features and must never be used for real data |
| `DATABASE_URL` | from the secret manager; TLS (`sslmode=verify-full`) |
| `AUTH_MODE` | `trusted_jwt` |
| `AUTH_ISSUER`, `AUTH_AUDIENCE`, `AUTH_JWKS_URL` | from the host wrapper contract (PENDING, `CONTRACTS.md`) |
| `AUTH_TENANT_CLAIM`, `AUTH_MAX_TOKEN_TTL`, `AUTH_CLOCK_SKEW` | per host contract; TTL ≤ 15 min recommended |
| `SELF_APPROVAL_EXEMPT_SUBJECTS` | unset. Development only (refused otherwise): comma-separated host token subjects that the segregation-of-duties rules (`not_case_owner`, `not_task_submitter`) do not block; such decisions carry `self_approval_exempt: true` in the decision authority and audit event |
| `FRAME_ANCESTORS` | the host origins that embed the mini-app |
| `KMS_MODE`, `CONNECTORS_MODE` | `disabled` until real adapters exist (`stub` is refused outside development/test) |
| `AGENT_RUNTIME_MODE` | `disabled`, or `agent_studio` with `AGENT_STUDIO_BASE_URL`, `AGENT_STUDIO_SERVICE_TOKEN_FILE` and `AGENT_CALLBACK_SECRET` (see `docs/CONTRACTS.md` §3); `stub` is refused outside development/test |
| `AGENT_CALLBACK_SECRET` | ≥ 32 random bytes from the secret manager, shared with the AI runtime |
| `PACKS_CONFIG` | path of the mounted pack bindings file (e.g. `/app/config/packs.json`) |
| `METRICS_ADDRESS` | internal-only address, e.g. `:9090`; never expose publicly |
| `WORKER_HEALTH_ADDRESS` | internal probe/metrics address for the worker |
| `DEV_AUTH_*` | must be unset |

## Production checklist

- [ ] Host wrapper contract confirmed; `trusted_jwt` configured; a forged/expired token returns 401 in staging.
- [ ] Tenants, memberships and role assignments provisioned from the organisation's identity process.
- [ ] **Policy delegations** for every gate entered from the approved DoA (not from samples) and reviewed.
- [ ] `PACKS_CONFIG` reviewed: runtime agent ids, skill versions, SoD per policy; ratifications only with references.
- [ ] Real KMS / AI runtime / connector adapters implemented and contract-tested (no stubs).
- [ ] TLS everywhere; secrets in the secret manager; no `.env` files in images.
- [ ] `migrate up` executed; `migrate status` current; `/readyz` green.
- [ ] Two or more workers; metrics scraped; alerts from `OPERATIONS_RUNBOOK.md` configured.
- [ ] Backups (PITR) enabled and a restore drill completed (`BACKUP_RECOVERY.md`).
- [ ] UI served behind the host with `frame-ancestors` restricted; CSP unchanged.
- [ ] Penetration test / security review of auth, callback endpoint and authorization matrix.

## Tailnet development VM (current)

`deploy/deploy-vm.sh` ships the tree to `kibi-vm`, keeps the VM's `.env` (Postgres password and callback secret
generated on first deploy), rebuilds, and recreates app containers (Postgres untouched). The UI binds to the
Tailscale address only; API and metrics bind to `127.0.0.1` on the VM. It runs `APP_ENV=development` with dev
personas and stubs because the external contracts are pending — it is a demo environment, not production.
