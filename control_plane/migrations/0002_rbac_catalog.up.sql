-- Generic platform permission and role catalogue (no domain-specific menus or roles).
-- App-open permissions only allow discovering/opening a mini-app; each service keeps its own domain ACL.

INSERT INTO permissions (key, resource_type, action, description, sensitive) VALUES
    ('catalog:view',          'catalog',        'view',            'See the app catalog of a workspace', false),
    ('kms:open',              'mini_app',       'open',            'Open the Knowledge Hub app (KMS still checks source ACLs)', false),
    ('kms:contribute',        'mini_app',       'contribute',      'Contribute content to KMS (KMS still checks collection ACLs)', false),
    ('flowcase:open',         'mini_app',       'open',            'Open Flowcase (Flowcase still checks case/task/gate access)', false),
    ('agent-studio:open',     'mini_app',       'open',            'Open Agent Studio (Agent Studio still checks agent/model policy)', false),
    ('case-workspace:open',   'mini_app',       'open',            'Open the Universal Case Workspace when published', false),
    ('review-inbox:view',     'mini_app',       'view',            'See review inbox indicators', false),
    ('agent:author',          'mini_app',       'author',          'Author agents in Agent Studio, subject to its resource policy', false),
    ('agent:release-request', 'mini_app',       'release-request', 'Request agent releases, subject to Agent Studio approval policy', false),
    ('workspace:manage',      'workspace',      'manage',          'Manage workspace settings', true),
    ('memberships:manage',    'workspace',      'manage-members',  'Manage memberships and role bindings in scope', true),
    ('platform:admin',        'platform_admin', 'admin',           'Platform administration', true),
    ('registry:manage',       'platform_admin', 'registry',        'Create, publish and disable mini-app manifests', true),
    ('audit:view',            'platform_admin', 'audit',           'Read the platform audit trail', true),
    ('users:manage',          'platform_admin', 'users',           'Pre-provision, suspend and reactivate directory users', true),
    ('workspaces:create',     'platform_admin', 'workspaces',      'Create workspaces', true),
    ('grants:approve',        'platform_admin', 'grants',          'Independently approve sensitive role grants', true),
    ('integrations:view',     'platform_admin', 'integrations',    'View IdP, session and adapter configuration metadata', true);

INSERT INTO roles (key, display_name, description, scope, sensitive) VALUES
    ('platform_admin',        'Platform admin',        'Tenant-wide platform configuration, registry, audit. No domain approvals.', 'tenant', true),
    ('workspace_admin',       'Workspace admin',       'Membership and scoped role grants inside one workspace.', 'workspace', true),
    ('knowledge_contributor', 'Knowledge contributor', 'Open and contribute to KMS; KMS still validates document ACL.', 'workspace', false),
    ('case_worker',           'Case worker',           'Open Flowcase; Flowcase still checks case membership.', 'workspace', false),
    ('case_reviewer',         'Case reviewer',         'Open Flowcase review inbox; never implies gate approval.', 'workspace', false),
    ('agent_author',          'Agent author',          'Open Agent Studio and author agents; cannot publish by default.', 'workspace', false),
    ('agent_ops',             'Agent ops',             'Open Agent Studio and request releases.', 'workspace', false),
    ('workspace_viewer',      'Workspace viewer',      'See the workspace catalog only.', 'workspace', false);

INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, p.key FROM roles r JOIN (VALUES
    ('platform_admin', 'platform:admin'), ('platform_admin', 'catalog:view'), ('platform_admin', 'kms:open'),
    ('platform_admin', 'flowcase:open'), ('platform_admin', 'agent-studio:open'), ('platform_admin', 'registry:manage'),
    ('platform_admin', 'memberships:manage'), ('platform_admin', 'audit:view'), ('platform_admin', 'users:manage'),
    ('platform_admin', 'workspaces:create'), ('platform_admin', 'grants:approve'), ('platform_admin', 'integrations:view'),
    ('workspace_admin', 'catalog:view'), ('workspace_admin', 'workspace:manage'), ('workspace_admin', 'memberships:manage'),
    ('knowledge_contributor', 'catalog:view'), ('knowledge_contributor', 'kms:open'), ('knowledge_contributor', 'kms:contribute'),
    ('case_worker', 'catalog:view'), ('case_worker', 'flowcase:open'), ('case_worker', 'case-workspace:open'),
    ('case_reviewer', 'catalog:view'), ('case_reviewer', 'flowcase:open'), ('case_reviewer', 'review-inbox:view'),
    ('agent_author', 'catalog:view'), ('agent_author', 'agent-studio:open'), ('agent_author', 'agent:author'),
    ('agent_ops', 'catalog:view'), ('agent_ops', 'agent-studio:open'), ('agent_ops', 'agent:release-request'),
    ('workspace_viewer', 'catalog:view')
) AS m(role_key, permission_key) ON m.role_key = r.key
JOIN permissions p ON p.key = m.permission_key;

INSERT INTO policy_meta (version) VALUES ('rbac-2026.10.1');
