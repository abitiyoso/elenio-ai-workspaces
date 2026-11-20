CREATE TABLE agent_run_usage (
 tenant_id uuid NOT NULL,
 run_id uuid NOT NULL,
 attempt integer NOT NULL CHECK(attempt>0),
 deployment_id uuid,
 config_version integer NOT NULL CHECK(config_version>=0),
 tokens_in bigint NOT NULL CHECK(tokens_in>=0),
 tokens_out bigint NOT NULL CHECK(tokens_out>=0),
 tool_calls integer NOT NULL CHECK(tool_calls>=0),
 usage_reports integer NOT NULL CHECK(usage_reports>=0),
 missing_usage integer NOT NULL CHECK(missing_usage>=0),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(run_id,attempt),
 FOREIGN KEY(tenant_id,run_id) REFERENCES agent_runs(tenant_id,id),
 FOREIGN KEY(tenant_id,deployment_id) REFERENCES llm_model_deployments(tenant_id,id),
 CHECK(usage_reports>0 OR (tokens_in=0 AND tokens_out=0))
);
ALTER TABLE agent_run_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_run_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON agent_run_usage
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE FUNCTION agent_studio_usage_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'run usage records are immutable' USING ERRCODE='AS001';
END;
$$;
CREATE TRIGGER agent_studio_usage_immutable BEFORE UPDATE OR DELETE ON agent_run_usage
 FOR EACH ROW EXECUTE FUNCTION agent_studio_usage_immutable();
