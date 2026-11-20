-- A reported runtime snapshot is operational metadata, never approval or a priced charge.
ALTER TABLE agent_runs
 ADD COLUMN usage_observation jsonb CHECK(usage_observation IS NULL OR jsonb_typeof(usage_observation)='object'),
 ADD COLUMN usage_received_at timestamptz;
