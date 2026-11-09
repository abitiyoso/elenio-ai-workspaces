#!/bin/sh
set -eu
LC_ALL=C
export LC_ALL

until pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE"; do
  sleep 2
done

: "${AGENT_STUDIO_DB_OWNER_PASSWORD_FILE:?AGENT_STUDIO_DB_OWNER_PASSWORD_FILE is required}"
PGPASSWORD="$(cat "$AGENT_STUDIO_DB_OWNER_PASSWORD_FILE")"
export PGPASSWORD

: "${AGENT_STUDIO_APP_DB_PASSWORD_FILE:?AGENT_STUDIO_APP_DB_PASSWORD_FILE is required}"
AGENT_STUDIO_APP_DB_PASSWORD="$(cat "$AGENT_STUDIO_APP_DB_PASSWORD_FILE")"
export AGENT_STUDIO_APP_DB_PASSWORD
role_sql="$(mktemp /tmp/agent-studio-create-role.XXXXXX)"
chmod 600 "$role_sql"
trap 'rm -f "$role_sql"' EXIT HUP INT TERM
cat > "$role_sql" <<'SQL'
\getenv app_password AGENT_STUDIO_APP_DB_PASSWORD
SELECT format(
    'CREATE ROLE agent_studio_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD %L',
    :'app_password'
) WHERE NOT EXISTS (
    SELECT 1 FROM pg_roles WHERE rolname = 'agent_studio_app'
)
\gexec
SELECT format(
    'ALTER ROLE agent_studio_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD %L',
    :'app_password'
)
\gexec
GRANT CONNECT ON DATABASE agent_studio TO agent_studio_app;
SQL
psql --set=ON_ERROR_STOP=1 --file="$role_sql"
rm -f "$role_sql"
trap - EXIT HUP INT TERM
unset AGENT_STUDIO_APP_DB_PASSWORD

psql --set=ON_ERROR_STOP=1 --command='
CREATE TABLE IF NOT EXISTS public.agent_studio_schema_migrations (
    version text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
)'

migration_count=0
# The C locale makes the numbered filename glob deterministic across hosts.
for migration in /migrations/??????_*.up.sql; do
  [ -f "$migration" ] || continue
  filename=${migration##*/}
  version=${filename%.up.sql}
  case "$version" in
    [0-9][0-9][0-9][0-9][0-9][0-9]_[A-Za-z0-9_-]*) ;;
    *) echo "invalid migration filename: $filename" >&2; exit 1 ;;
  esac
  migration_count=$((migration_count + 1))
  psql --set=ON_ERROR_STOP=1 \
    --set=migration_version="$version" \
    --set=migration_file="$migration" \
    --file=- <<'SQL'
SELECT EXISTS (
    SELECT 1 FROM public.agent_studio_schema_migrations WHERE version = :'migration_version'
) AS migration_applied \gset
\if :migration_applied
\echo 'Migration is already applied'
\else
BEGIN;
\i :migration_file
INSERT INTO public.agent_studio_schema_migrations (version) VALUES (:'migration_version');
COMMIT;
\endif
SQL
done

if [ "$migration_count" -eq 0 ]; then
  echo "no numbered Agent Studio up migrations found" >&2
  exit 1
fi

psql --set=ON_ERROR_STOP=1 --file=- <<'SQL'
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO agent_studio_app;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO agent_studio_app;
GRANT INSERT, UPDATE, DELETE ON TABLE
    llm_providers, llm_model_deployments, model_approvals, agents,
    agent_versions, agent_model_bindings, skills, skill_versions,
    agent_skill_bindings, agent_tool_bindings, audit_events,
    tenant_tool_drafts, tenant_tool_draft_versions,
    agent_runs, agent_evidence_refs, outbox_events, work_queue,
    eval_suites, eval_cases, eval_runs, eval_results,
    release_requests, releases, feedback_items TO agent_studio_app;
-- Trace rows are inserted, and deleted only by the retention purge trigger path.
GRANT INSERT, DELETE ON TABLE agent_run_events TO agent_studio_app;
GRANT SELECT ON public.agent_studio_schema_migrations TO agent_studio_app;
SELECT format(
    'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON TABLES TO agent_studio_app',
    current_user
) \gexec
SQL
