-- Immutable planning snapshots, not approved releases. No runtime/customer artifact payloads.
ALTER TABLE solution_projects ADD CONSTRAINT solution_projects_scope_id UNIQUE (tenant_id,workspace_id,id);
CREATE TABLE solution_candidates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 workspace_id uuid NOT NULL,
 solution_id uuid NOT NULL,
 version text NOT NULL CHECK (version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' AND length(version)<=32),
 plan_revision integer NOT NULL CHECK(plan_revision>0),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 content_hash text NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 creation_key text NOT NULL,
 created_by uuid NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,workspace_id,solution_id) REFERENCES solution_projects(tenant_id,workspace_id,id),
 UNIQUE(solution_id,version),
 UNIQUE(workspace_id,creation_key)
);
CREATE INDEX solution_candidates_scope_idx ON solution_candidates(tenant_id,workspace_id,solution_id,id);
CREATE FUNCTION solution_candidates_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'solution candidates are immutable; create a new version' USING ERRCODE='23000';
END;
$$;
CREATE TRIGGER solution_candidates_immutable BEFORE UPDATE OR DELETE ON solution_candidates
 FOR EACH ROW EXECUTE FUNCTION solution_candidates_immutable();
