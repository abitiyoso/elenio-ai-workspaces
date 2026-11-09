DROP TABLE IF EXISTS workflow_versions;
DROP FUNCTION IF EXISTS flowcase_guard_workflow_version();
DROP TABLE IF EXISTS workflow_definitions;
-- Created in 000002; triggers from later migrations that use it are dropped by their own down files first.
DROP FUNCTION IF EXISTS flowcase_forbid_truncate();
