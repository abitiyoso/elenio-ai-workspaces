DROP TABLE IF EXISTS document_chunks;
DROP TABLE IF EXISTS parser_artifacts;
DROP TABLE IF EXISTS ingestion_jobs;
ALTER TABLE document_versions DROP COLUMN IF EXISTS original_filename;
ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_case_scope_fk;
ALTER TABLE documents DROP COLUMN IF EXISTS case_id;
DROP TABLE IF EXISTS case_memberships;
DROP TABLE IF EXISTS case_scopes;
DROP TABLE IF EXISTS collection_memberships;
