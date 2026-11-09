package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DevIssuer is the issuer recorded for the fixed development identity; no real token has it.
const DevIssuer = "flowcase-dev-mock"

type DevSeed struct {
	TenantID    string
	UserID      string
	DisplayName string
	Roles       []string
	// DomainRoles are pack assignee roles granted to the default user (e.g. poc-owner).
	DomainRoles []string
	Personas    []DevSeedPersona
	// Policies are delegated to every persona holding domain_approver. Development convenience only;
	// production delegations come from the organisation's DoA, never from sample data.
	Policies []string
}

type DevSeedPersona struct {
	UserID      string
	DisplayName string
	Roles       []string
}

// SeedDevelopment upserts the fixed local tenant/user/membership/roles used by AUTH_MODE=dev_mock.
// Callers must only invoke it after config validation confirmed APP_ENV=development.
func SeedDevelopment(ctx context.Context, pool *pgxpool.Pool, s DevSeed) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, external_ref, name) VALUES ($1, 'dev-tenant', 'Local Development Tenant')
			  ON CONFLICT (id) DO NOTHING`, []any{s.TenantID}},
			{`INSERT INTO users (id, issuer, subject, display_name) VALUES ($1, $2, $3, $4)
			  ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`, []any{s.UserID, DevIssuer, s.UserID, s.DisplayName}},
			{`INSERT INTO memberships (tenant_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, []any{s.TenantID, s.UserID}},
			{`INSERT INTO workspaces (tenant_id, key, name) VALUES ($1, 'solution-workspace', 'Solution Workspace')
			  ON CONFLICT (tenant_id, key) DO NOTHING`, []any{s.TenantID}},
		}
		for _, st := range stmts {
			if _, err := tx.Exec(ctx, st.sql, st.args...); err != nil {
				return fmt.Errorf("seed development identity: %w", err)
			}
		}
		if err := grantRoles(ctx, tx, s.TenantID, s.UserID, s.Roles); err != nil {
			return err
		}
		for _, role := range s.DomainRoles {
			if _, err := tx.Exec(ctx, `INSERT INTO domain_role_assignments (tenant_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				s.TenantID, s.UserID, role); err != nil {
				return fmt.Errorf("seed domain role %q: %w", role, err)
			}
		}
		for _, p := range s.Personas {
			if _, err := tx.Exec(ctx, `INSERT INTO users (id, issuer, subject, display_name) VALUES ($1, $2, $3, $4)
				ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`, p.UserID, DevIssuer, p.UserID, p.DisplayName); err != nil {
				return fmt.Errorf("seed persona %s: %w", p.DisplayName, err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO memberships (tenant_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, s.TenantID, p.UserID); err != nil {
				return err
			}
			if err := grantRoles(ctx, tx, s.TenantID, p.UserID, p.Roles); err != nil {
				return err
			}
			if !slices.Contains(p.Roles, "domain_approver") {
				continue
			}
			for _, policy := range s.Policies {
				if _, err := tx.Exec(ctx, `INSERT INTO policy_delegations (tenant_id, policy_ref, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
					s.TenantID, policy, p.UserID); err != nil {
					return fmt.Errorf("seed delegation %s: %w", policy, err)
				}
			}
		}
		return nil
	})
}

func grantRoles(ctx context.Context, tx pgx.Tx, tenantID, userID string, roles []string) error {
	for _, role := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO role_assignments (tenant_id, user_id, role) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, tenantID, userID, role); err != nil {
			return fmt.Errorf("seed development role %q: %w", role, err)
		}
	}
	return nil
}
