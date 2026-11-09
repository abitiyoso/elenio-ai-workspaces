-- Refuse rollback that would discard tenant-authored data.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tenant_tool_draft_versions) OR
       EXISTS (SELECT 1 FROM tenant_tool_drafts) THEN
        RAISE EXCEPTION 'cannot roll back tenant tool drafts while tenant-authored rows exist';
    END IF;
END;
$$;

DROP TABLE tenant_tool_draft_versions;
DROP TABLE tenant_tool_drafts;
DROP FUNCTION agent_studio_immutable_tenant_tool_draft_version();
