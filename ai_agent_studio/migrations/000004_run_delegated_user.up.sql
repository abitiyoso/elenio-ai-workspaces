-- Production runs started by a service client (Flowcase) may name the end user
-- whose knowledge access KMS retrieval is delegated to: the Control Plane user
-- UUID of the case owner, which is also the KMS principal id. Human-started,
-- sandbox and evaluation runs never delegate.

ALTER TABLE agent_runs
    ADD COLUMN delegated_user text
        CHECK (delegated_user IS NULL OR delegated_user ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
    ADD CONSTRAINT agent_runs_delegated_user_service_production
        CHECK (delegated_user IS NULL OR (caller_kind = 'service' AND run_kind = 'production'));
