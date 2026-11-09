# Phase 3 migration validation

Run this check against a disposable database restored from a fresh backup of the current Phase 2 database. Do not point the validation process at a production database.

1. Take and retain a database backup:

   ```sh
   pg_dump --format=custom --file kms-pre-phase3.dump "$SOURCE_DATABASE_URL"
   ```

2. Create a separate validation database and restore the backup there:

   ```sh
   createdb kms_phase3_validation
   pg_restore --clean --if-exists --no-owner --dbname "$KMS_PHASE3_VALIDATION_URL" kms-pre-phase3.dump
   ```

3. Record the existing row counts and migration ledger. Confirm the restored database has its expected Phase 1/2 data before starting the API.

   ```sql
   SELECT version FROM schema_migrations ORDER BY version;
   SELECT count(*) FROM documents;
   SELECT count(*) FROM document_versions;
   SELECT count(*) FROM document_chunks;
   ```

4. Run only the migration-capable API process with its database URL set to the disposable database. Keep host authentication fail-closed and do not expose the API port externally:

   ```sh
   (cd backend && env APP_ENV=production AUTH_MODE=trusted \
   DATABASE_URL="$KMS_PHASE3_VALIDATION_URL" \
   ELASTICSEARCH_URL=http://127.0.0.1:9200 \
   HTTP_ADDRESS=127.0.0.1:18080 \
   MIGRATIONS_DIR=./migrations \
   go run ./cmd/kms-api)
   ```

   Stop the process after startup confirms migrations completed. The API does not need Elasticsearch readiness to run migrations; the URL is validated during configuration only.

5. Confirm migration versions 1, 2, and 3 are recorded; row counts are unchanged; each document's `current_version_id` references one of its versions; any backfilled `published_version_id` references an approved, approved-trust published version; and append-only triggers exist. Review sampled customer-source rows to confirm they remain case scoped and are not marked globally published.

6. Exercise the restored database with the Phase 3 governance smoke sequence using two verified test identities: upload V1, ingest, submit review, independently approve, publish; upload V2 and confirm V1 remains published; revoke/expire/delete in a test collection and confirm retrieval is blocked before the cleanup worker completes. Confirm an audit row survives tombstoning and failed cleanup attempts remain queued/retryable.

## kibi-vm run, 2026-10-30

- Backup `~/kms-phase1/backups/pre-phase3-20261030T144933Z.dump` and app tarball `app-phase2-20261030T144933Z.tgz` were taken before rollout.
- The dump was restored into a disposable `postgres:16-alpine` container on an isolated network and migrated by the Phase 3 API (`APP_ENV=production AUTH_MODE=trusted`). Ledger 1,2,3; row counts unchanged (the source had 0 documents, so backfill paths were exercised only trivially); no invalid current/published pointers; no published `customer_source`; the three append-only triggers exist.
- Every static SQL statement in `backend/internal` was `PREPARE`d against the migrated schema to catch parameter type inference errors.
- The governance smoke sequence (two identities: owner via the main API, reviewer via a temporary second API instance) passed 36/36 checks after rollout, including service-credential retrieval, expiry/revocation/delete blocking, outbox drain, tombstone retention and append-only audit rows. The disposable `Phase 3 smoke …` collections remain in the VM database because collections have no delete endpoint.
