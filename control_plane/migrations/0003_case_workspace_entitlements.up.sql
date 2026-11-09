-- Universal Case Workspace (case-workspace) is published in the development registry: case reviewers and
-- platform admins can open it alongside case workers (who already held case-workspace:open). Opening the app
-- grants nothing in Flowcase/KMS: the UCW BFF obtains user-scoped tokens through the token exchange, which
-- re-checks the target app decision (ADR-004), and each service still applies its own domain policy.

UPDATE permissions SET description = 'Open the Universal Case Workspace (Flowcase/KMS data still subject to their own access checks)'
WHERE key = 'case-workspace:open';

INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, m.permission_key FROM roles r JOIN (VALUES
    ('platform_admin', 'case-workspace:open'),
    ('case_reviewer',  'case-workspace:open')
) AS m(role_key, permission_key) ON m.role_key = r.key
ON CONFLICT (role_id, permission_key) DO NOTHING;

UPDATE policy_meta SET version = 'rbac-2026.10.2', updated_at = now();
