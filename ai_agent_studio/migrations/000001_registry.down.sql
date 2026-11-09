DO $$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'llm_providers', 'llm_model_deployments', 'model_approvals', 'agents',
        'agent_versions', 'agent_model_bindings', 'skills', 'skill_versions',
        'agent_skill_bindings', 'agent_tool_bindings', 'audit_events'
    ] LOOP
        EXECUTE format('DROP POLICY IF EXISTS tenant_scope ON %I', table_name);
    END LOOP;
END;
$$;

DROP TABLE IF EXISTS agent_tool_bindings;
DROP TABLE IF EXISTS agent_skill_bindings;
DROP TABLE IF EXISTS agent_model_bindings;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS skill_versions;
DROP TABLE IF EXISTS skills;
DROP TABLE IF EXISTS tool_versions;
DROP TABLE IF EXISTS tools;
DROP TABLE IF EXISTS agent_versions;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS model_approvals;
DROP TABLE IF EXISTS llm_model_deployments;
DROP TABLE IF EXISTS llm_providers;
DROP TABLE IF EXISTS agent_pack_templates;

DROP FUNCTION IF EXISTS agent_studio_append_only_audit();
DROP FUNCTION IF EXISTS agent_studio_guard_draft_bindings();
DROP FUNCTION IF EXISTS agent_studio_immutable_published_tool_version();
DROP FUNCTION IF EXISTS agent_studio_immutable_published_skill_version();
DROP FUNCTION IF EXISTS agent_studio_immutable_published_version();
