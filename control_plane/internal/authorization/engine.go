package authorization

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/control-plane/internal/platform/postgres"
)

// Engine loads decision facts from PostgreSQL. It deliberately keeps no positive-decision cache: every
// request sees current memberships, bindings, overrides and app state.
type Engine struct {
	Pool          *pgxpool.Pool
	PolicyVersion string
	Sensitive     map[string]bool
	Now           func() time.Time
}

func NewEngine(ctx context.Context, pool *pgxpool.Pool) (*Engine, error) {
	e := &Engine{Pool: pool, Sensitive: map[string]bool{}, Now: time.Now}
	if err := pool.QueryRow(ctx, `SELECT version FROM policy_meta`).Scan(&e.PolicyVersion); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT key FROM permissions WHERE sensitive`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		e.Sensitive[k] = true
	}
	return e, rows.Err()
}

// Facts loads the subject, workspace membership, role bindings and overrides. workspaceID may be "" for
// tenant-scoped checks.
func (e *Engine) Facts(ctx context.Context, q postgres.Querier, userID, workspaceID string) (Facts, error) {
	if q == nil {
		q = e.Pool
	}
	f := Facts{UserID: userID, WorkspaceID: workspaceID, PolicyVersion: e.PolicyVersion, SensitivePerms: e.Sensitive}
	err := q.QueryRow(ctx, `SELECT u.tenant_id::text, CASE WHEN t.status = 'active' THEN u.status ELSE 'tenant_suspended' END, u.principal_type
FROM users u JOIN tenants t ON t.id = u.tenant_id WHERE u.id = $1::uuid`, userID).Scan(&f.TenantID, &f.UserStatus, &f.PrincipalType)
	if errors.Is(err, pgx.ErrNoRows) {
		f.UserStatus = ""
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if workspaceID != "" {
		err = q.QueryRow(ctx, `SELECT w.key, w.status, COALESCE(m.state, '')
FROM workspaces w LEFT JOIN workspace_memberships m ON m.workspace_id = w.id AND m.user_id = $2::uuid AND m.state <> 'revoked'
WHERE w.id = $1::uuid AND w.tenant_id = $3::uuid`, workspaceID, userID, f.TenantID).Scan(&f.WorkspaceKey, &f.WorkspaceStatus, &f.MembershipState)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return f, err
		}
	}
	rows, err := q.Query(ctx, `
SELECT r.key, r.scope, COALESCE(array_agg(rp.permission_key ORDER BY rp.permission_key) FILTER (WHERE rp.permission_key IS NOT NULL), '{}')
FROM role_bindings b JOIN roles r ON r.id = b.role_id LEFT JOIN role_permissions rp ON rp.role_id = r.id
WHERE b.user_id = $1::uuid AND b.tenant_id = $2::uuid AND b.state = 'active' AND (b.expires_at IS NULL OR b.expires_at > now())
  AND ((r.scope = 'tenant' AND b.workspace_id IS NULL) OR (r.scope = 'workspace' AND b.workspace_id = $3::uuid))
GROUP BY r.key, r.scope ORDER BY r.key`, userID, f.TenantID, nullable(workspaceID))
	if err != nil {
		return f, err
	}
	for rows.Next() {
		var br BoundRole
		if err := rows.Scan(&br.Key, &br.Scope, &br.Permissions); err != nil {
			rows.Close()
			return f, err
		}
		if br.Scope == "tenant" {
			f.TenantRoles = append(f.TenantRoles, br)
		} else {
			f.WorkspaceRoles = append(f.WorkspaceRoles, br)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return f, err
	}
	if workspaceID != "" {
		rows, err = q.Query(ctx, `SELECT permission_key, effect FROM entitlement_overrides
WHERE workspace_id = $1::uuid AND user_id = $2::uuid AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, workspaceID, userID)
		if err != nil {
			return f, err
		}
		defer rows.Close()
		for rows.Next() {
			var o Override
			if err := rows.Scan(&o.Permission, &o.Effect); err != nil {
				return f, err
			}
			f.Overrides = append(f.Overrides, o)
		}
		return f, rows.Err()
	}
	return f, nil
}

// LoadApp returns registry facts for an app key (nil when unknown).
func (e *Engine) LoadApp(ctx context.Context, q postgres.Querier, appKey string) (*App, error) {
	if q == nil {
		q = e.Pool
	}
	var a App
	var manifest struct {
		Required  []string `json:"required_entitlements"`
		Allowlist []string `json:"workspace_allowlist"`
	}
	var hasPublished bool
	err := q.QueryRow(ctx, `SELECT a.app_key, a.status, a.published_version_id IS NOT NULL, COALESCE(v.manifest, '{}'::jsonb)
FROM mini_apps a LEFT JOIN mini_app_versions v ON v.id = a.published_version_id WHERE a.app_key = $1`, appKey).
		Scan(&a.Key, &a.Status, &hasPublished, &manifest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.HasPublishedVersion = hasPublished
	a.RequiredEntitlements, a.WorkspaceAllowlist = manifest.Required, manifest.Allowlist
	return &a, nil
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) App(ctx context.Context, userID, workspaceID, appKey, correlationID string) (Decision, error) {
	f, err := e.Facts(ctx, nil, userID, workspaceID)
	if err != nil {
		return Decision{}, err
	}
	app, err := e.LoadApp(ctx, nil, appKey)
	if err != nil {
		return Decision{}, err
	}
	d := DecideApp(f, app, correlationID, e.now())
	if app == nil {
		d.ResourceID = appKey
	}
	return d, nil
}

func (e *Engine) Tenant(ctx context.Context, q postgres.Querier, userID, perm, correlationID string) (Decision, error) {
	f, err := e.Facts(ctx, q, userID, "")
	if err != nil {
		return Decision{}, err
	}
	return DecideTenantPermission(f, perm, correlationID, e.now()), nil
}

func (e *Engine) Workspace(ctx context.Context, q postgres.Querier, userID, workspaceID, perm, correlationID string) (Decision, error) {
	f, err := e.Facts(ctx, q, userID, workspaceID)
	if err != nil {
		return Decision{}, err
	}
	return DecideWorkspacePermission(f, perm, correlationID, e.now()), nil
}

func (e *Engine) Switch(ctx context.Context, userID, workspaceID, correlationID string) (Decision, error) {
	f, err := e.Facts(ctx, nil, userID, workspaceID)
	if err != nil {
		return Decision{}, err
	}
	return DecideWorkspaceSwitch(f, correlationID, e.now()), nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
