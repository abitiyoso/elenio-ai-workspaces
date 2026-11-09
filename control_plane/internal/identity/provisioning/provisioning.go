// Package provisioning binds verified IdP subjects (iss, sub) to directory users at login and keeps
// tenant, IdP and service-identity metadata in sync with deployment configuration.
package provisioning

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/control-plane/internal/identity/oidc"
	"github.com/elenio/control-plane/internal/platform/postgres"
)

var (
	ErrSuspended      = errors.New("user is suspended")
	ErrNotProvisioned = errors.New("user is not provisioned")
	ErrWrongTenant    = errors.New("user belongs to another tenant")
)

type User struct {
	ID          string
	DisplayName string
	Status      string
	Created     bool
}

// BindLogin resolves the directory user for a verified identity. Email is display data and never used to
// match an existing user. Suspended users are refused.
func BindLogin(ctx context.Context, tx pgx.Tx, tenantID string, id oidc.Identity, autoProvision bool) (User, error) {
	var u User
	var userTenant string
	err := tx.QueryRow(ctx, `SELECT id::text, tenant_id::text, display_name, status FROM users WHERE idp_issuer = $1 AND idp_subject = $2 FOR UPDATE`,
		id.Issuer, id.Subject).Scan(&u.ID, &userTenant, &u.DisplayName, &u.Status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if !autoProvision {
			return User{}, ErrNotProvisioned
		}
		u = User{DisplayName: clip(id.DisplayName, 200), Status: "active", Created: true}
		err = tx.QueryRow(ctx, `INSERT INTO users (tenant_id, idp_issuer, idp_subject, display_name, email, last_login_at)
VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''), now()) RETURNING id::text`, tenantID, id.Issuer, id.Subject, u.DisplayName, clip(id.Email, 254)).Scan(&u.ID)
		return u, err
	case err != nil:
		return User{}, err
	}
	if userTenant != tenantID {
		return User{}, ErrWrongTenant
	}
	if u.Status != "active" {
		return u, ErrSuspended
	}
	if id.DisplayName != "" {
		u.DisplayName = clip(id.DisplayName, 200)
	}
	_, err = tx.Exec(ctx, `UPDATE users SET display_name = $2, email = COALESCE(NULLIF($3, ''), email), last_login_at = now(), updated_at = now() WHERE id = $1::uuid`,
		u.ID, u.DisplayName, clip(id.Email, 254))
	return u, err
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > n {
		return string([]rune(s)[:n])
	}
	return s
}

// EnsureTenant creates the configured tenant on first start and returns its id.
func EnsureTenant(ctx context.Context, q postgres.Querier, key, name string) (string, error) {
	if _, err := q.Exec(ctx, `INSERT INTO tenants (key, name) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING`, key, name); err != nil {
		return "", err
	}
	var id string
	err := q.QueryRow(ctx, `SELECT id::text FROM tenants WHERE key = $1`, key).Scan(&id)
	return id, err
}

// SyncIdentityProvider records IdP metadata (secret reference only, never the secret).
func SyncIdentityProvider(ctx context.Context, q postgres.Querier, tenantID, issuer, clientID, secretRef string) error {
	_, err := q.Exec(ctx, `INSERT INTO identity_providers (tenant_id, issuer_url, client_id, secret_ref) VALUES ($1::uuid, $2, $3, $4)
ON CONFLICT (issuer_url) DO UPDATE SET client_id = EXCLUDED.client_id, secret_ref = EXCLUDED.secret_ref`, tenantID, issuer, clientID, secretRef)
	return err
}

func UpdateIdentityProviderStatus(ctx context.Context, q postgres.Querier, issuer, status, detail, hash string) error {
	if status != "ok" && status != "unavailable" {
		status = "unknown"
	}
	_, err := q.Exec(ctx, `UPDATE identity_providers SET status = $2, last_error = NULLIF($3, ''), discovery_hash = COALESCE(NULLIF($4, ''), discovery_hash),
last_checked_at = now() WHERE issuer_url = $1`, issuer, status, detail, hash)
	return err
}

// SyncServiceIdentity records the audience-bound identity the BFF uses towards one mini-app.
func SyncServiceIdentity(ctx context.Context, q postgres.Querier, clientID, audience string, scopes []string, description string) error {
	if scopes == nil {
		scopes = []string{}
	}
	_, err := q.Exec(ctx, `INSERT INTO service_identities (client_id, audience, scopes, description) VALUES ($1, $2, to_jsonb($3::text[]), $4)
ON CONFLICT (client_id) DO UPDATE SET audience = EXCLUDED.audience, scopes = EXCLUDED.scopes, description = EXCLUDED.description`,
		clientID, audience, scopes, description)
	return err
}
