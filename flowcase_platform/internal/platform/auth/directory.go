package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PostgresDirectory requires an active user, an active tenant (matched by its external reference
// from the host token) and an active membership. Roles come only from Flowcase role assignments.
type PostgresDirectory struct{ DB Querier }

const resolveMemberSQL = `
SELECT u.id::text, t.id::text, u.display_name, u.kind
FROM tenants t
JOIN memberships m ON m.tenant_id = t.id AND m.status = 'active'
JOIN users u ON u.id = m.user_id AND u.status = 'active'
WHERE t.external_ref = $1 AND t.status = 'active' AND u.issuer = $2 AND u.subject = $3`

const tenantRolesSQL = `
SELECT DISTINCT role FROM role_assignments
WHERE tenant_id = $1 AND user_id = $2 AND scope_type = 'tenant'
  AND revoked_at IS NULL AND valid_from <= now() AND (valid_until IS NULL OR valid_until > now())
ORDER BY role`

func (d PostgresDirectory) Resolve(ctx context.Context, c Claims) (Identity, error) {
	var id Identity
	err := d.DB.QueryRow(ctx, resolveMemberSQL, c.TenantRef, c.Issuer, c.Subject).Scan(&id.UserID, &id.TenantID, &id.DisplayName, &id.Kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, unauthenticated("not_a_member")
	}
	if err != nil {
		return Identity{}, misconfigured("directory_unavailable")
	}
	rows, err := d.DB.Query(ctx, tenantRolesSQL, id.TenantID, id.UserID)
	if err != nil {
		return Identity{}, misconfigured("directory_unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return Identity{}, misconfigured("directory_unavailable")
		}
		if role, ok := ParseRole(name); ok {
			id.Roles = append(id.Roles, role)
		}
	}
	if rows.Err() != nil {
		return Identity{}, misconfigured("directory_unavailable")
	}
	return id, nil
}
