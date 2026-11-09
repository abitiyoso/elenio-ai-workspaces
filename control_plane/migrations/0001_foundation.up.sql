-- Control Plane foundation: tenants, directory, workspaces, RBAC, registry, sessions, audit, outbox.
-- Identity keys are (idp_issuer, idp_subject); email is display data only.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key        text NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9-]{1,62}$'),
    name       text NOT NULL,
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id),
    idp_issuer     text NOT NULL CHECK (length(idp_issuer) BETWEEN 1 AND 512),
    idp_subject    text NOT NULL CHECK (length(idp_subject) BETWEEN 1 AND 256),
    display_name   text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    email          text,
    principal_type text NOT NULL DEFAULT 'human' CHECK (principal_type IN ('human', 'service')),
    status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    status_reason  text,
    last_login_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (idp_issuer, idp_subject)
);
CREATE INDEX users_tenant_idx ON users (tenant_id, status);

CREATE TABLE workspaces (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id),
    key           text NOT NULL CHECK (key ~ '^[a-z][a-z0-9-]{1,62}$'),
    name          text NOT NULL CHECK (length(name) BETWEEN 2 AND 120),
    description   text NOT NULL DEFAULT '',
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    owner_user_id uuid REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE workspace_memberships (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL,
    user_id      uuid NOT NULL REFERENCES users(id),
    state        text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'suspended', 'revoked')),
    invited_by   uuid REFERENCES users(id),
    reason       text NOT NULL DEFAULT '',
    joined_at    timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    revoked_at   timestamptz,
    FOREIGN KEY (tenant_id, workspace_id) REFERENCES workspaces(tenant_id, id)
);
-- One live (active or suspended) membership per user and workspace; revoked rows stay as history.
CREATE UNIQUE INDEX memberships_live_uq ON workspace_memberships (workspace_id, user_id) WHERE state <> 'revoked';
CREATE INDEX memberships_user_idx ON workspace_memberships (user_id, state);

CREATE TABLE permissions (
    key           text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$'),
    resource_type text NOT NULL,
    action        text NOT NULL,
    description   text NOT NULL DEFAULT '',
    sensitive     boolean NOT NULL DEFAULT false
);

CREATE TABLE roles (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key          text NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_]{1,62}$'),
    display_name text NOT NULL,
    description  text NOT NULL DEFAULT '',
    scope        text NOT NULL CHECK (scope IN ('tenant', 'workspace')),
    sensitive    boolean NOT NULL DEFAULT false
);

CREATE TABLE role_permissions (
    role_id        uuid NOT NULL REFERENCES roles(id),
    permission_key text NOT NULL REFERENCES permissions(key),
    PRIMARY KEY (role_id, permission_key)
);

CREATE TABLE policy_meta (
    singleton  boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version    text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role_grant_requests (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id),
    workspace_id    uuid,
    user_id         uuid NOT NULL REFERENCES users(id),
    role_id         uuid NOT NULL REFERENCES roles(id),
    requested_by    uuid NOT NULL REFERENCES users(id),
    reason          text NOT NULL,
    state           text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'rejected', 'cancelled', 'expired')),
    binding_expires_at timestamptz,
    decided_by      uuid REFERENCES users(id),
    decided_at      timestamptz,
    decision_reason text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id) REFERENCES workspaces(tenant_id, id),
    -- Dual control is also enforced by the database: nobody decides their own or self-targeted request.
    CHECK (decided_by IS NULL OR (decided_by <> requested_by AND decided_by <> user_id))
);
CREATE INDEX grant_requests_pending_idx ON role_grant_requests (tenant_id, state, created_at DESC);

CREATE TABLE role_bindings (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id),
    workspace_id     uuid,
    user_id          uuid NOT NULL REFERENCES users(id),
    role_id          uuid NOT NULL REFERENCES roles(id),
    state            text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'revoked')),
    expires_at       timestamptz,
    granted_by       uuid REFERENCES users(id),
    grant_request_id uuid REFERENCES role_grant_requests(id),
    reason           text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    revoked_at       timestamptz,
    revoked_by       uuid REFERENCES users(id),
    FOREIGN KEY (tenant_id, workspace_id) REFERENCES workspaces(tenant_id, id)
);
CREATE UNIQUE INDEX role_bindings_active_uq
    ON role_bindings (tenant_id, COALESCE(workspace_id, '00000000-0000-0000-0000-000000000000'::uuid), user_id, role_id)
    WHERE state = 'active';
CREATE INDEX role_bindings_user_idx ON role_bindings (user_id, state);

CREATE TABLE entitlement_overrides (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id),
    workspace_id   uuid NOT NULL,
    user_id        uuid NOT NULL REFERENCES users(id),
    permission_key text NOT NULL REFERENCES permissions(key),
    effect         text NOT NULL CHECK (effect IN ('allow', 'deny')),
    reason         text NOT NULL,
    expires_at     timestamptz,
    created_by     uuid NOT NULL REFERENCES users(id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    revoked_at     timestamptz,
    revoked_by     uuid REFERENCES users(id),
    FOREIGN KEY (tenant_id, workspace_id) REFERENCES workspaces(tenant_id, id)
);
CREATE INDEX overrides_lookup_idx ON entitlement_overrides (workspace_id, user_id) WHERE revoked_at IS NULL;

-- Registry. Manifests are immutable once written; lifecycle lives on mini_apps / release_state.
CREATE TABLE mini_apps (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_key              text NOT NULL UNIQUE CHECK (app_key ~ '^[a-z][a-z0-9-]{2,63}$'),
    route_base           text NOT NULL UNIQUE CHECK (route_base ~ '^/apps/[a-z][a-z0-9-]{2,63}/$'),
    owner_team           text NOT NULL,
    status               text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'disabled', 'retired')),
    published_version_id uuid,
    revision             integer NOT NULL DEFAULT 1,
    disabled_reason      text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CHECK (route_base = '/apps/' || app_key || '/')
);

CREATE TABLE mini_app_versions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mini_app_id   uuid NOT NULL REFERENCES mini_apps(id),
    version       text NOT NULL CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'),
    manifest      jsonb NOT NULL,
    checksum      text NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    release_state text NOT NULL DEFAULT 'draft' CHECK (release_state IN ('draft', 'published', 'superseded')),
    created_by    uuid REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    published_by  uuid REFERENCES users(id),
    published_at  timestamptz,
    UNIQUE (mini_app_id, version)
);
ALTER TABLE mini_apps ADD CONSTRAINT mini_apps_published_version_fk
    FOREIGN KEY (published_version_id) REFERENCES mini_app_versions(id);

CREATE FUNCTION mini_app_versions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'mini_app_versions are append-only';
    END IF;
    IF NEW.manifest IS DISTINCT FROM OLD.manifest OR NEW.checksum IS DISTINCT FROM OLD.checksum
       OR NEW.version IS DISTINCT FROM OLD.version OR NEW.mini_app_id IS DISTINCT FROM OLD.mini_app_id THEN
        RAISE EXCEPTION 'mini app manifest versions are immutable';
    END IF;
    IF OLD.release_state = 'superseded' AND NEW.release_state <> 'superseded' THEN
        RAISE EXCEPTION 'superseded versions cannot be re-released';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER mini_app_versions_immutable BEFORE UPDATE OR DELETE ON mini_app_versions
    FOR EACH ROW EXECUTE FUNCTION mini_app_versions_immutable();

CREATE TABLE mini_app_routes (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_version_id      uuid NOT NULL REFERENCES mini_app_versions(id),
    environment         text NOT NULL CHECK (environment IN ('dev', 'staging', 'production')),
    registered_path     text NOT NULL CHECK (registered_path ~ '^/apps/[a-z][a-z0-9-]{2,63}/$'),
    upstream_config_ref text NOT NULL, -- key into trusted deployment config, never a URL
    enabled             boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mini_app_routes_enabled_uq ON mini_app_routes (environment, registered_path) WHERE enabled;

-- Sessions: only a SHA-256 of the opaque cookie value is stored.
CREATE TABLE sessions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash          bytea NOT NULL UNIQUE,
    user_id             uuid NOT NULL REFERENCES users(id),
    tenant_id           uuid NOT NULL REFERENCES tenants(id),
    workspace_id        uuid,
    idp_issuer          text NOT NULL,
    auth_time           timestamptz NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    last_seen_at        timestamptz NOT NULL DEFAULT now(),
    idle_expires_at     timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at          timestamptz,
    revoke_reason       text,
    refresh_token_enc   bytea, -- AES-256-GCM, key from secret manager; never returned
    user_agent          text NOT NULL DEFAULT '',
    FOREIGN KEY (tenant_id, workspace_id) REFERENCES workspaces(tenant_id, id)
);
CREATE INDEX sessions_user_live_idx ON sessions (user_id) WHERE revoked_at IS NULL;

-- Pending authorization requests (state/nonce/PKCE). One-time use, short-lived, bound to a browser cookie.
CREATE TABLE auth_flows (
    state_hash        bytea PRIMARY KEY,
    binding_hash      bytea NOT NULL,
    nonce             text NOT NULL,
    code_verifier_enc bytea NOT NULL,
    return_to         text NOT NULL DEFAULT '/',
    created_at        timestamptz NOT NULL DEFAULT now(),
    expires_at        timestamptz NOT NULL,
    consumed_at       timestamptz
);

CREATE TABLE identity_providers (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid REFERENCES tenants(id),
    issuer_url      text NOT NULL UNIQUE,
    client_id       text NOT NULL,
    secret_ref      text NOT NULL, -- reference (file path / vault path), never the secret
    discovery_hash  text,
    status          text NOT NULL DEFAULT 'unknown' CHECK (status IN ('ok', 'unavailable', 'unknown')),
    last_checked_at timestamptz,
    last_error      text
);

CREATE TABLE service_identities (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       text NOT NULL UNIQUE,
    audience        text NOT NULL,
    scopes          jsonb NOT NULL DEFAULT '[]',
    certificate_ref text,
    status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    description     text NOT NULL DEFAULT ''
);

CREATE TABLE platform_audit_events (
    seq            bigserial PRIMARY KEY,
    id             uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    actor_user_id  uuid,
    actor_type     text NOT NULL CHECK (actor_type IN ('human', 'service', 'system')),
    service_id     text,
    tenant_id      uuid,
    workspace_id   uuid,
    action         text NOT NULL,
    target_type    text NOT NULL,
    target_id      text NOT NULL,
    decision       text CHECK (decision IN ('allow', 'deny')),
    reason_code    text,
    policy_version text,
    correlation_id text NOT NULL,
    rationale      text,
    metadata       jsonb NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_tenant_seq_idx ON platform_audit_events (tenant_id, seq DESC);
CREATE INDEX audit_action_idx ON platform_audit_events (action, seq DESC);

CREATE FUNCTION audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'platform_audit_events is append-only';
END $$;
CREATE TRIGGER audit_append_only BEFORE UPDATE OR DELETE ON platform_audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_append_only();

CREATE TABLE outbox_events (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   text NOT NULL,
    payload      jsonb NOT NULL,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts     integer NOT NULL DEFAULT 0,
    retry_at     timestamptz NOT NULL DEFAULT now(),
    dedupe_key   text NOT NULL UNIQUE,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);
CREATE INDEX outbox_pending_idx ON outbox_events (retry_at) WHERE status = 'pending';
