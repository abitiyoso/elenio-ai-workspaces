# Knowledge Workspace

A governed knowledge source and evidence store, run as a host-wrapped mini-app. Phase 3 added immutable document versions, independent review decisions, publication/revocation/expiry, append-only audit history, and durable object/index cleanup. Phase 4 adds operational readiness: request-correlated structured logs, Prometheus metrics, a tenant-scoped status endpoint and Settings screen, ACL projection refresh on membership grants, bulk indexing, and the `kms-admin` CLI for index rebuilds and drift checks. PostgreSQL remains authoritative for tenant, membership, version lifecycle, and retrieval authorization. Elasticsearch contains retrieval data only and can always be rebuilt from PostgreSQL.

## Requirements

- Go 1.24+, Node.js 20+, Docker Compose v2
- Existing Docling Serve reachable at `DOCLING_BASE_URL` (Compose defaults to the private Docker alias `http://docling:5001`); Docling is not started locally by this project
- S3-compatible object storage; local Compose starts SeaweedFS with its S3 API
- Optional OpenAI-compatible embeddings endpoint. Semantic and hybrid searches return an explicit `503 embedding_unavailable` until configured

## Local development

1. Optionally copy `backend/.env.example` to `backend/.env`. Set host-specific Docling and embedding values as needed. The development mock identity is only accepted with `APP_ENV=development`.
2. Start the dependencies, API, ingestion worker, and UI: `docker compose up --build`.
3. Open `http://localhost:5173`. The mini-app uses the host session integration and does not show a standalone login page.
4. Check liveness at `http://localhost:8080/healthz` and readiness at `http://localhost:8080/readyz`.

Compose provides PostgreSQL, Elasticsearch 8.15.3, SeaweedFS S3, API, worker, and frontend. It does not provide Docling or an embedding service. For a VM with the existing Docling container bound to host loopback port 5001, attach that running container to the KMS Compose network and assign the alias expected by the worker:

```sh
docker network connect --alias docling <kms-compose-network> docling
```

Then set `DOCLING_BASE_URL=http://docling:5001` and the existing `DOCLING_API_KEY` in the Compose interpolation environment (the URL default already matches). Keep the key in an untracked local `.env`; Compose passes it to the API and worker without placing it in the repository. Verify private-network reachability without restarting or publishing the Docling container:

```sh
docker run --rm --network <kms-compose-network> curlimages/curl:8.10.1 -fsS http://docling:5001/docs
```

The loopback host port binding remains unchanged; containers on the attached private network use the container's port directly. If Docling is hosted elsewhere, set `DOCLING_BASE_URL` to its reachable internal URL. SeaweedFS and its bucket are initialized by the `mini` service using the configured local credentials.

PostgreSQL migrations are applied by the API and worker at startup, including upgrade from the Phase 1 foundation schema. Avoid `docker compose down -v` where local data must be preserved; it removes PostgreSQL, Elasticsearch, object storage, and frontend dependency volumes.

Before a deployment upgrade, follow [PHASE3_MIGRATION_CHECK.md](PHASE3_MIGRATION_CHECK.md) against a disposable database restored from backup. The migration is additive and backfills current/published version pointers conservatively; it must still be exercised against a copy of the target database before rollout.

## API

The OpenAPI 3.1 contract is in `backend/openapi.yaml`. Authenticated routes include:

- `GET/POST /api/v1/collections`; membership grants and case scopes are collection/case scoped
- `GET/POST /api/v1/documents`; multipart PDF upload accepts `file`, `collection_id`, `title`, and optional `case_id`
- `GET /api/v1/ingestion/jobs/{jobID}` and `POST .../{jobID}/retry`
- `GET /api/v1/documents/{documentID}/versions/{versionID}/preview`
- `POST /api/v1/search` with `keyword`, `semantic`, or `hybrid` mode
- `GET /api/v1/documents/{documentID}/versions` and multipart `POST .../versions` for immutable version history and upload
- `POST /api/v1/documents/{documentID}/submit-review` with `version_id` and note, `GET /api/v1/reviews`, and `/api/v1/reviews/{reviewID}/approve` or `/reject`; decisions require rationale and deny self-approval
- `POST .../publish`, `/revoke`, `PATCH .../validity`, and `DELETE /api/v1/documents/{documentID}` for explicit lifecycle changes and tombstone cleanup
- `GET /api/v1/activity?collection_id=...` for scoped append-only governance activity
- `GET /api/v1/ops/status` for tenant-scoped integration and queue health (see the operations runbook)
- `POST /api/v1/retrieval/query` for bounded service-identity retrieval; it accepts service credentials only, never the development human mock

The verified AuthAdapter identity is the only source of human tenant and principal IDs. Every search hit is checked again against PostgreSQL before its snippet or citation is returned, including live version, expiry, revocation and published-version checks. Keyword search works without embeddings. Semantic and hybrid search fail explicitly when no provider is configured; there is no automatic keyword-only fallback.

## Configuration and boundaries

See `backend/.env.example`. `EMBEDDING_ENDPOINT`, `EMBEDDING_API_KEY`, `EMBEDDING_MODEL_ID`, and `EMBEDDING_DIMENSIONS` configure an OpenAI-compatible embedding provider; none is hardcoded. Elasticsearch is the only search/vector store; PostgreSQL does not use pgvector. When dimensions are not configured, the index mapping stays keyword-only.

Outside development, configure a host-specific implementation of `TrustedSessionVerifier` with issuer, audience, expiry, signature/key rotation, and membership checks. Mock authentication is disabled unless `APP_ENV=development`. Do not treat client tenant, user, or role headers as identity. Authorization is enforced in service boundaries and middleware.

The ingestion worker uses PostgreSQL leases and retry limits, stores original PDFs in S3-compatible storage, submits async Docling jobs, preserves page/section provenance from Docling output, and indexes deterministic chunk IDs. Failed jobs can be retried through the authorized API. Enterprise uploads remain unverified until an independent reviewer approves a version and a steward explicitly publishes it. Case-private uploads are labeled `customer_source` and remain case scoped; they cannot be published as global evidence. Revocation and expiry block retrieval immediately through PostgreSQL, while Elasticsearch and object cleanup run through a durable retryable outbox. Tombstones and audit records remain after physical object cleanup.

Service retrieval credentials are provisioned out of band with `kms-service-client create`; the generated bearer secret is shown once and only its SHA-256 digest is stored. Credentials require an expiry, the fixed `kms-retrieval` audience, the `retrieval:query` scope, and explicit collection/case grants. A separately verified host user session in the same tenant is also required; results are limited by both that user's membership and the service grant. Revoke credentials with `kms-service-client revoke`. The development human mock does not authenticate a service credential, and production retrieval fails closed until both host verification and service provisioning are configured.

## Operations runbook

### Health and status

- `GET /healthz` is liveness only. `GET /readyz` checks PostgreSQL, Elasticsearch and object storage and reports whether embeddings are configured.
- `GET /api/v1/ops/status` (authenticated, tenant-scoped) reports dependency states including Docling `/health`, search-index consistency (indexed vs. expected chunk counts), embedding model, ingestion and outbox backlog and pending reviews. The Settings screen renders it; the Overview shows the same counts as "Workspace health". It returns states and counts only, never endpoints, secrets or document content.

### Logs

The API and worker write one JSON object per line. Every HTTP request is logged once with `request_id`, matched `route`, `status`, `duration_ms` and, after authentication, `tenant_id`; request bodies, queries and document text are never logged. Callers may send `X-Request-ID` (8–64 characters of `[A-Za-z0-9._-]`); otherwise one is generated and returned in the response header. Audit rows use `X-Correlation-ID` when supplied and the request ID otherwise, so an audit event can be traced to its log line. Every 5xx response logs its cause with the request ID. The worker logs `docling_ms`, `embedding_ms`, `index_ms`, `chunk_count` and `attempt` for each completed ingestion job. Health probe lines are logged at debug level.

### Metrics

`GET /metrics` serves the Prometheus text format:

| Metric | Meaning |
| --- | --- |
| `kms_http_requests_total{method,route,status}` | Requests by matched route pattern |
| `kms_http_request_duration_seconds{route}` | Latency histogram; use for p50/p95 search latency |
| `kms_ingestion_jobs{status}`, `kms_ingestion_oldest_pending_seconds` | Ingestion backlog and failures |
| `kms_outbox_events{status}`, `kms_outbox_oldest_pending_seconds` | Index/storage cleanup backlog (Elasticsearch lag) |
| `kms_pending_reviews`, `kms_schema_version`, `kms_status_query_up` | Review backlog, applied migration, status query health |

Without `METRICS_TOKEN` the endpoint answers only when `APP_ENV=development`; elsewhere set `METRICS_TOKEN` (32+ characters) and scrape with `Authorization: Bearer <token>`. Do not route `/metrics` through a public ingress. Suggested alerts: `kms_ingestion_jobs{status="failed"} > 0`, `kms_outbox_events{status="failed"} > 0`, `kms_outbox_oldest_pending_seconds > 300`, and `kms_status_query_up == 0`.

### Retries and failure recovery

- Ingestion jobs retry with exponential backoff (15 s doubling to 5 min) up to their attempt limit, then stop as `failed`; retry them from the Processing screen or `POST /api/v1/ingestion/jobs/{jobID}/retry`. Leases expire, so a crashed worker's job is picked up again.
- Index updates, ACL refreshes, deletions and object cleanup run through the PostgreSQL outbox with the same backoff (8 attempts). After fixing the cause of `failed` events, run `kms-admin requeue-outbox [--tenant-id UUID]`.
- Indexing uses Elasticsearch bulk upserts keyed by deterministic chunk ID, so retries never duplicate chunks.
- Granting a collection or case membership queues an ACL projection refresh, so documents indexed before the grant become searchable for the new member. Revocations never depend on that refresh: every hit is re-authorized against PostgreSQL.

### Search index rebuilds and embedding migration

Elasticsearch is a derived index. `kms-admin` ships in the API image and rebuilds it from PostgreSQL with current trust, publication and ACL metadata:

```sh
docker compose exec kms-api /kms-admin verify-index                       # expected vs. indexed chunks
docker compose exec kms-api /kms-admin reindex --target kms_chunks_v2 --alias kms_chunks_current
docker compose exec kms-api /kms-admin status                             # queue backlog as JSON
```

`reindex` builds a new physical index, bulk-loads every live, unexpired, unrevoked chunk (re-embedding with the configured provider when one is set, since vectors are not stored in PostgreSQL), verifies the count and then atomically moves the alias. Set `ELASTICSEARCH_INDEX` to the alias and restart `kms-api` and `kms-worker` once, after which future rebuilds only move the alias. For an embedding model or dimension change, configure the new `EMBEDDING_*` values for the `kms-admin` run and both services, reindex into a new target and swap the alias; old and new vectors are never mixed in one index. Run rebuilds during low write activity, then run `verify-index`: changes made while the rebuild runs go to the previous index. The alias name must not already be a physical index (the default `kms_chunks_v1` is one, so use a new name such as `kms_chunks_current`). Delete the old index only after verification.

### Backups

PostgreSQL and object storage are the systems of record. Back up both (for example `pg_dump --format=custom` plus a bucket replica or snapshot). Elasticsearch does not need backups: rebuild it with `kms-admin reindex`. Migrations run automatically at API and worker start and are additive; test an upgrade against a restored copy first, as in [PHASE3_MIGRATION_CHECK.md](PHASE3_MIGRATION_CHECK.md).

### Troubleshooting

| Symptom | Check |
| --- | --- |
| Upload stays in "queued" | Worker running? `kms_ingestion_oldest_pending_seconds`; Docling state on the Settings screen |
| Job `failed` with a Docling error | Docling `/health`, `DOCLING_API_KEY`, then retry the job |
| Published document not found by a reader | Outbox backlog (`kms-admin status`); the reader's collection or case membership |
| Settings shows "catching up" | Outbox queued or failed events; if drift persists with an empty outbox, run `verify-index` then `reindex` |
| Semantic or hybrid search returns 503 | No embedding provider configured; keyword search is unaffected |
| 500 response | Find `request_id` from the `X-Request-ID` response header in the API logs |

## Tests

Run `cd backend && go test ./... && go vet ./...` for unit tests. `internal/sqlcheck` additionally PREPAREs every static SQL statement against a migrated database when `KMS_TEST_DATABASE_URL` is set; run it against a disposable or restored copy before deploying schema or query changes:

```sh
KMS_TEST_DATABASE_URL=postgres://kms:kms@localhost:5432/kms?sslmode=disable go test ./internal/sqlcheck/
```

Build the UI from `frontend/` with `npm install && npm run build`.

## Behind the AI Workspace Control Plane (2026-10-31)
`AUTH_MODE=control_plane_jwt` verifies the Control Plane's per-request ES256 token (`AUTH_ISSUER`, `AUTH_AUDIENCE`
default `kms`, `AUTH_JWKS_URL`, `AUTH_TENANT_MAP=key=uuid`) and requires an active `tenant_principals` row; KMS
roles come only from that table. Build the UI with `KMS_BASE_PATH=/apps/kms/` to mount it at that path.
Running instance: `kms-cp` (see `../control_plane/docs/INTEGRATION_MATRIX.md`).

### Delegated service retrieval
`POST /api/v1/retrieval/query` authenticates the service client from `Authorization: Bearer kms_svc_...`, so it cannot
also carry a user token. A runtime with no live user session (Agent Studio's async agents) names the human it acts for
with `X-KMS-Delegated-User: <Control Plane user UUID>` (the `tenant_principals.principal_id`). This works in every
`AUTH_MODE`:

- The client needs the `retrieval:delegate` scope in addition to `retrieval:query`. Create it with `--delegate`:

  ```sh
  kms-service-client create --tenant-id TENANT_UUID --actor-id OPERATOR_UUID --name agent-studio \
    --expires-at 2027-01-31T00:00:00Z --grant COLLECTION_UUID [--grant COLLECTION_UUID:CASE_UUID] --delegate
  ```
- The header must hold exactly one UUID, and that principal needs an active `tenant_principals` row in the
  client's tenant. Roles come from that row. Collection and case membership plus the client's grants still apply.
- Header without the scope, repeated or malformed, or an unknown, disabled or other-tenant principal → 401
  `delegated_user_required`. A directory outage → 503 `delegated_user_unavailable`.
- Without the header, nothing changes: the configured auth adapter must verify a user session in the same tenant.
- The `retrieval.query` audit row records the principal as `details.user_id` and sets `details.delegated=true`.
  Refusals also set `details.reason`: `delegation_not_permitted`, `delegated_user_repeated`,
  `delegated_user_not_uuid`, `principal_not_provisioned` or `principal_disabled`.

```sh
curl -X POST "$KMS/api/v1/retrieval/query" -H "Authorization: Bearer $KMS_SVC_TOKEN" \
  -H "X-KMS-Delegated-User: $CASE_OWNER_UUID" -H 'Content-Type: application/json' \
  -d '{"query":"refund policy","collection_ids":["COLLECTION_UUID"]}'
```

Existing clients keep `retrieval:query` only. To delegate, create a new client with `--delegate` and revoke the old
one. No migration is needed because `service_clients.scopes` is already `TEXT[]`.
