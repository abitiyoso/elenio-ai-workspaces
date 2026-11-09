-- Dropping the column discards which user each past run delegated KMS
-- retrieval to; the runs themselves are kept.
ALTER TABLE agent_runs
    DROP CONSTRAINT IF EXISTS agent_runs_delegated_user_service_production,
    DROP COLUMN IF EXISTS delegated_user;
