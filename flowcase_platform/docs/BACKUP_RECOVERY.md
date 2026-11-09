# Backup and Recovery

PostgreSQL is Flowcase's only system of record (definitions, versions, cases, tasks, approvals, decisions,
artifacts, audit, outbox). KMS documents are **not** stored here and are recovered by KMS's own procedures;
Flowcase keeps only source ids/versions and re-checks them after a restore.

## Targets (proposal — confirm with operations)

| | Target |
|---|---|
| RPO | ≤ 5 minutes (continuous WAL archiving / managed PITR) |
| RTO | ≤ 1 hour for a full restore of a single tenant database |
| Retention | 35 days PITR + monthly logical dumps kept 12 months (align with the records policy) |

## Backups

1. **Point-in-time recovery (primary).** Use the managed PostgreSQL PITR feature, or `archive_mode=on` with
   WAL shipping (e.g. pgBackRest / WAL-G) to object storage with encryption at rest.
2. **Logical dumps (secondary, portable).**
   ```bash
   pg_dump --format=custom --no-owner --dbname "$DATABASE_URL" --file flowcase-$(date -u +%Y%m%dT%H%M%SZ).dump
   ```
   Store encrypted; the dump contains case data, rationales and artifact content. Treat it as confidential.
3. Never back up `.env` files or secrets with the data; secrets live in the secret manager.

## Restore procedure

1. Stop the worker first (no new executions), then the API.
2. Restore into an **empty** database:
   ```bash
   createdb flowcase_restore
   pg_restore --no-owner --exit-on-error --dbname flowcase_restore flowcase-<timestamp>.dump
   ```
   For PITR, restore to the chosen timestamp with the provider's tooling.
3. Check the schema against the binary you will run:
   ```bash
   DATABASE_URL=.../flowcase_restore go run ./cmd/migrate status   # or /app/migrate status in the image
   ```
   "not current" means the backup predates the binary's migrations: run `migrate up` (never edit migrations).
4. Point `DATABASE_URL` at the restored database; start the API, check `/readyz`; start the worker.
5. Post-restore checks:
   - Executions that were `running` at backup time have leases that have since expired; the worker reclaims them
     automatically (handlers are idempotent; connectors receive the same idempotency key).
   - Agent runs `started` before the backup are polled again; late callbacks for settled runs are ignored.
   - Outbox events not marked `done` are re-delivered (consumers must de-duplicate by event id).
   - Evidence is re-checked by the worker's reconciliation; withdrawn sources make artifacts stale.
   - Compare row counts for `cases`, `audit_events`, `approval_decisions` with the source if available.

## Drill performed (2026-10-30, local Docker PostgreSQL 16)

| Step | Result |
|---|---|
| `pg_dump --format=custom` of the dev database | 134 KB dump |
| `pg_restore` into a new database | success, `--exit-on-error` clean |
| Row counts source vs restore (`cases`, `audit_events`, `approval_decisions`, `artifact_versions`, published versions) | identical (2 / 71 / 6 / 8 / 3) |
| Append-only trigger on restored `audit_events` | still refuses `UPDATE` |
| `migrate status` on the restore | reported "not current" (source was at 6, binary ships 7) → `migrate up` is step 3 above |
| Total time | 4 s |

A production drill (size, timing, PITR) is still required before go-live.
