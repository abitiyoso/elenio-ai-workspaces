# Isolated VM deployment

Agent Studio runs as its own Compose project and uses its own named PostgreSQL
volume. The API binds to `127.0.0.1:8190`; publish it through Tailscale Serve
on HTTPS port `5190`. It does not reuse Flowcase or KMS containers, databases,
ports, directories, or volumes.

## Host prerequisites

- Docker Compose v2 and Tailscale Serve are installed on the VM.
- The private VM env file points to independent random owner/app database
  password files and to the service user's numeric UID/GID. Keep the password
  files mode 0600 and owned by that VM service user outside release folders.
- The private VM env file points to a raw 32-byte vault master key file and an
  encrypted-vault directory owned by the same service UID/GID. Keep the key
  mode 0600, the directory mode 0700, and back up both together.
- `AGENT_STUDIO_ACCESS_MAP_FILE` points to a mode-restricted JSON file mapping
  specific Tailscale logins to one tenant UUID and exact API permissions. The
  sample `access-map.example.json` is intentionally not deployable.
- Tailscale ACLs permit only the intended users to reach the Serve endpoint.
  Network access does not grant Agent Studio permissions; the access map does.

The Serve identity headers are trusted only because the API binds to loopback
and the remote user reaches it through Tailscale Serve. This is a host trust
boundary, not a signed-token verifier; local host processes remain trusted.
An absent header, unmapped login, unknown tenant, or missing action permission
is denied. Version publish permissions are intentionally not included in the
sample map; add them only after release authority is explicitly assigned.

## First deployment

1. Place the compose package in a new `~/agent-studio/releases/<release>` folder.
   Keep `~/agent-studio/.env` and `~/agent-studio/access-map.json` outside release
   directories so updates do not replace configuration.
2. Create independent random hex values in the owner/app password files and
   create a raw 32-byte vault key with a cryptographic random generator. Copy
   `.env.example` to the private VM env file and set the access-map/password/key
   paths, vault directory, and output of `id -u` / `id -g` for the VM service
   user. Restrict configuration and secret files to that user (`chmod 600`);
   create the vault directory with mode 0700.
3. Run `docker compose --env-file ~/agent-studio/.env -f
   ~/agent-studio/releases/<release>/deploy/compose.yaml up -d --build`.
4. Confirm `app` and `db` report healthy. The API is still loopback-only.
5. Inspect the current `tailscale serve status` before changing Serve config.
   Add HTTPS port `5190` forwarding to `http://127.0.0.1:8190` without replacing
   existing Serve handlers; confirm the resulting status and access through the
   VM's `*.ts.net` URL.

The one-shot migration job creates or refreshes the runtime `agent_studio_app`
role from its mounted password file, then applies ordered migrations and grants.
That role is not a table owner or superuser and receives only schema usage and
SELECT on global catalog tables, plus table DML grants on tenant-scoped tables.
The migration job alone runs as root to read host-owned mode-0600 secret files;
it writes a mode-0600 temporary SQL script inside its disposable container and
removes it after role setup. The long-running app runs as the configured
unprivileged VM service UID/GID. PostgreSQL RLS still requires transaction-local
tenant context for tenant data. The `agent_studio_db_data` named volume is owned
only by this project. Password, access-map, and vault-key files are mounted as
Compose secrets; the app does not need secret values in environment variables.
The OpenAI-compatible probe
adapter is installed and the configured exact HTTPS host allowlist is
`9router.kibicode.my.id`. It sends no provider request until the operator
explicitly starts a probe after configuring a credential. Capability probes
require a separate cost acknowledgement and may incur token charges.

## Local development identity

Outside the VM there is no Tailscale Serve header. For local work only, set
`AGENT_STUDIO_ENV=development` and `AGENT_STUDIO_DEV_LOGIN=<mapped login>`;
every request is then treated as that access-map login and still goes through
the same tenant and action checks. The app refuses to start with
`AGENT_STUDIO_DEV_LOGIN` unless the environment is `development`, and the
Compose package defaults to production with no dev login.

Only with this identity may `AGENT_STUDIO_HTTP_ADDR` name a non-loopback
address, and it must be one specific interface IP (never `0.0.0.0`). Binding
the tailnet IP this way makes every device that reaches it the configured
operator; use it only on a tailnet you fully control and only until a real
host identity is available.

## Control Plane token identity (`AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt`)

The default `AGENT_STUDIO_HUMAN_AUTH=tailscale` is the Serve-header identity
described above. A separate instance can instead sit behind the AI Workspace
Control Plane BFF, which proxies `/apps/agent-studio/*` (prefix stripped) and
injects a per-request ES256 delegation token as `Authorization: Bearer`:

- `AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt`
- `AGENT_STUDIO_CP_ISSUER` — exact `iss`, the Control Plane origin.
- `AGENT_STUDIO_CP_JWKS_URL` — `<issuer>/.well-known/jwks.json`; must be
  HTTPS unless `AGENT_STUDIO_ENV=development`.
- `AGENT_STUDIO_CP_TENANT_KEY` (required): the Control Plane tenant key this instance serves; tokens whose
  `tenant_id` claim differs are rejected even if the user id is mapped.
- `AGENT_STUDIO_CP_AUDIENCE` (default `agent-studio`) and
  `AGENT_STUDIO_CP_MAX_TOKEN_TTL` (default `15m`, at most `1h`).

Exactly one bearer header with a compact JWS (at most 8 KiB, ES256 or RS256)
is accepted; signature, `iss`, `aud`, `exp`/`iat`/`nbf` (60 s skew), lifetime,
`principal_type=human` and a UUID `sub` are checked. Keys are cached by `kid`
for 10 minutes and refetched on an unknown `kid` at most every 30 seconds.
The token `sub` (Control Plane user id) is then looked up in the same
access-map file, whose `login` values must be those user ids; unmapped users
are denied and tenant/action rules are unchanged. Tailscale and other identity
headers are ignored, `AGENT_STUDIO_DEV_LOGIN` is refused, and opaque service
tokens still work. The API keeps the loopback-only bind. Build the web UI with
`--build-arg AGENT_STUDIO_WEB_BASE_PATH=/apps/agent-studio/` so assets and API
calls carry the proxy prefix.

## Runtime worker and integrations (Phases 2–4)

The app process also runs the Eino worker (`AGENT_STUDIO_WORKER_CONCURRENCY`,
default 2; 0 disables it). Runs are claimed from `work_queue` with 30-second
leases, so a restarted container resumes in-flight runs after lease expiry.
`AGENT_STUDIO_TRACE_RETENTION_DAYS` (default 30) purges settled trace events.
`GET /api/v1/integrations/status` shows what is configured.

Optional integrations, each off until its owner issues credentials. Mount the
secret files the same way as the existing Compose secrets and add the env vars
to the `app` service:

- Flowcase → Agent Studio: `AGENT_STUDIO_SERVICE_TOKENS_FILE`, a JSON array of
  `{"client_id","token_sha256","tenant_id","permissions":["run.invoke","run.read","run.cancel"],"expires_at"}`.
  Only SHA-256 digests are stored; give the raw token to Flowcase out of band.
- Agent Studio → Flowcase callbacks: `AGENT_STUDIO_FLOWCASE_CALLBACK_URL` and
  `AGENT_STUDIO_FLOWCASE_CALLBACK_SECRET_FILE` (at least 32 bytes, shared with
  Flowcase). The URL is POSTed to verbatim; Agent Studio appends no path, so it
  must be the full Flowcase endpoint including `/api/v1/agent-callbacks/<provider>`,
  for example `http://127.0.0.1:8180/api/v1/agent-callbacks/agent-studio`
  (HTTPS, or HTTP to loopback only). A bare origin such as `http://127.0.0.1:8180`
  is accepted at startup, but deliveries then go to the wrong path and fail.
  Undelivered results wait in the outbox with `flowcase_callback_not_configured`.
- KMS retrieval: `AGENT_STUDIO_KMS_BASE_URL` (for example `http://127.0.0.1:8080`),
  `AGENT_STUDIO_KMS_SERVICE_TOKEN_FILE` (a KMS service credential; for delegated
  retrieval it must hold the `retrieval:delegate` scope) and
  `AGENT_STUDIO_KMS_USER_HEADER`, the header that carries the delegated user to
  KMS. Set it to `X-KMS-Delegated-User`. `Authorization` (and
  `Proxy-Authorization`) is refused at startup because it would replace the KMS
  service token.
  - Delegation is per run. A service client (Flowcase) may add
    `"delegated_user": "<Control Plane user UUID>"` to `POST /api/v1/runs`: the
    case owner, which is also their KMS principal id. It is lowercased, stored on
    the run (`agent_runs.delegated_user`, migration `000004_run_delegated_user`)
    and is part of the idempotency fingerprint, so replaying a key with a different
    `delegated_user` returns 409. Every `kms.search` call of that production run
    sends it in the configured header next to the service token. Human callers
    sending `delegated_user` get 400, and human, sandbox and evaluation runs never
    delegate. Production runs without it still fail KMS calls closed with
    `kms_delegation_unavailable` (the run goes to `needs_review`).
  - `AGENT_STUDIO_KMS_FORWARD_CASE_ID` (default `false`). The run's `case_id` is a
    Flowcase case id, not a KMS case id, so it is stored on the run but not sent
    to KMS. Set `true` only if KMS case ids and Flowcase case ids are the same.

Release authority (`release.review`, `release.approve`, `release.rollback`),
model approval and model binding are not in the operator map. Add them only to
the logins that hold that authority; one login can never approve its own release.

## Update and rollback

For an update, unpack into a new release directory and run `up -d --build` with
the same project name and env file. The named database volume and private config
stay in place. To roll back, run `up -d --build` using the prior release's
compose file. Do not run `docker compose down -v`; that deletes the Agent Studio
database volume. No rollback command should target Flowcase or KMS.

The app refuses to start until schema migration `000004_run_delegated_user` is
applied; the `migrate` job applies it before `app` starts. It only adds the
nullable `agent_runs.delegated_user` column, so the previous release keeps
working against the migrated database. Its down migration drops that column
and the record of which user each past run delegated to.
