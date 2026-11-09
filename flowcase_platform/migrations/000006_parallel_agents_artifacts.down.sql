DROP TABLE IF EXISTS approval_artifacts;
ALTER TABLE approval_requests DROP COLUMN IF EXISTS invalidated_reason;
ALTER TABLE approval_requests DROP COLUMN IF EXISTS supersedes_approval_id;
ALTER TABLE approval_requests DROP COLUMN IF EXISTS kind;
ALTER TABLE approval_requests DROP CONSTRAINT IF EXISTS approval_requests_status_check;
ALTER TABLE approval_requests ADD CONSTRAINT approval_requests_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'returned', 'cancelled'));
DROP TABLE IF EXISTS artifact_evidence_refs;
DROP TABLE IF EXISTS artifact_dependencies;
DROP TABLE IF EXISTS artifact_versions;
DROP FUNCTION IF EXISTS flowcase_guard_artifact_version();
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS tool_invocations;
DROP TABLE IF EXISTS agent_runs;
DROP INDEX IF EXISTS execution_tokens_branch_uidx;
ALTER TABLE execution_tokens ADD CONSTRAINT execution_tokens_case_id_parent_token_id_split_node_id_bran_key
    UNIQUE (case_id, parent_token_id, split_node_id, branch_key);
ALTER TABLE execution_tokens DROP COLUMN IF EXISTS split_execution_id;
