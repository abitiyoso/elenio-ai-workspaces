DROP TABLE IF EXISTS feedback_items;
DROP TABLE IF EXISTS releases;
DROP TABLE IF EXISTS release_requests;
DROP TABLE IF EXISTS eval_results;
DROP TABLE IF EXISTS eval_runs;
DROP TABLE IF EXISTS eval_cases;
DROP TABLE IF EXISTS eval_suites;
DROP TABLE IF EXISTS work_queue;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS agent_evidence_refs;
DROP TABLE IF EXISTS agent_run_events;
DROP TABLE IF EXISTS agent_runs;
DROP FUNCTION IF EXISTS agent_studio_release_request_history();
DROP FUNCTION IF EXISTS agent_studio_governance_history();
DROP FUNCTION IF EXISTS agent_studio_run_events_append_only();
-- Seeded global tool versions are published and immutable; they are left in place.
