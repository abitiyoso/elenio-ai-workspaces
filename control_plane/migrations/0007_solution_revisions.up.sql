CREATE TABLE solution_project_revisions (
 tenant_id uuid NOT NULL,
 workspace_id uuid NOT NULL,
 solution_id uuid NOT NULL,
 revision integer NOT NULL CHECK(revision>0),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 origin text NOT NULL CHECK(origin IN ('write','migration_current')),
 recorded_by uuid REFERENCES users(id),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(solution_id,revision),
 FOREIGN KEY(tenant_id,workspace_id,solution_id) REFERENCES solution_projects(tenant_id,workspace_id,id),
 CHECK((snapshot->>'id'=solution_id::text AND snapshot->>'tenant_id'=tenant_id::text AND snapshot->>'workspace_id'=workspace_id::text AND (snapshot->>'revision')::integer=revision) IS TRUE),
 CHECK((origin='write' AND recorded_by IS NOT NULL) OR (origin='migration_current' AND recorded_by IS NULL))
);
-- Existing overwritten revisions cannot be reconstructed; retain only the observed current revision.
INSERT INTO solution_project_revisions(tenant_id,workspace_id,solution_id,revision,snapshot,origin)
 SELECT tenant_id,workspace_id,id,revision,jsonb_build_object('id',id,'tenant_id',tenant_id,'workspace_id',workspace_id,'revision',revision,'definition',definition,'created_at',created_at,'updated_at',updated_at),'migration_current'
 FROM solution_projects;
CREATE FUNCTION solution_project_revisions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'solution revisions are immutable; save a new revision' USING ERRCODE='23000';
END;
$$;
CREATE TRIGGER solution_project_revisions_immutable BEFORE UPDATE OR DELETE ON solution_project_revisions
 FOR EACH ROW EXECUTE FUNCTION solution_project_revisions_immutable();
