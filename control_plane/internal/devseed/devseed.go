// Package devseed loads illustrative demo data (workspaces, directory bindings, role bindings, sample
// registry entries) for explicit development deployments only. Config refuses DEV_SEED in production.
package devseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/control-plane/internal/apps"
	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/platform/postgres"
)

type File struct {
	Disclaimer string `json:"disclaimer"`
	Workspaces []struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"workspaces"`
	Users []struct {
		Subject     string              `json:"subject"`
		Name        string              `json:"name"`
		Email       string              `json:"email"`
		TenantRoles []string            `json:"tenant_roles"`
		Memberships map[string][]string `json:"memberships"`
	} `json:"users"`
	Apps []json.RawMessage `json:"apps"`
}

// Apply is idempotent: existing rows are left untouched.
func Apply(ctx context.Context, pool *pgxpool.Pool, path, tenantID, issuer, registryEnv string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read seed file: %w", err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("parse seed file: %w", err)
	}
	return postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		ws := map[string]string{}
		for _, w := range f.Workspaces {
			if _, err := tx.Exec(ctx, `INSERT INTO workspaces (tenant_id, key, name, description) VALUES ($1::uuid, $2, $3, $4) ON CONFLICT (tenant_id, key) DO NOTHING`,
				tenantID, w.Key, w.Name, w.Description); err != nil {
				return err
			}
			var id string
			if err := tx.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE tenant_id = $1::uuid AND key = $2`, tenantID, w.Key).Scan(&id); err != nil {
				return err
			}
			ws[w.Key] = id
		}
		for _, u := range f.Users {
			if _, err := tx.Exec(ctx, `INSERT INTO users (tenant_id, idp_issuer, idp_subject, display_name, email) VALUES ($1::uuid, $2, $3, $4, $5)
ON CONFLICT (idp_issuer, idp_subject) DO NOTHING`, tenantID, issuer, u.Subject, u.Name, u.Email); err != nil {
				return err
			}
			var uid string
			if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE idp_issuer = $1 AND idp_subject = $2`, issuer, u.Subject).Scan(&uid); err != nil {
				return err
			}
			for _, role := range u.TenantRoles {
				if err := bind(ctx, tx, tenantID, "", uid, role); err != nil {
					return err
				}
			}
			for wsKey, roles := range u.Memberships {
				wid, ok := ws[wsKey]
				if !ok {
					return fmt.Errorf("unknown workspace %q for %s", wsKey, u.Name)
				}
				if _, err := tx.Exec(ctx, `INSERT INTO workspace_memberships (tenant_id, workspace_id, user_id, reason)
SELECT $1::uuid, $2::uuid, $3::uuid, 'development seed'
WHERE NOT EXISTS (SELECT 1 FROM workspace_memberships WHERE workspace_id = $2::uuid AND user_id = $3::uuid AND state <> 'revoked')`, tenantID, wid, uid); err != nil {
					return err
				}
				for _, role := range roles {
					if err := bind(ctx, tx, tenantID, wid, uid, role); err != nil {
						return err
					}
				}
			}
		}
		for _, rawApp := range f.Apps {
			if err := seedApp(ctx, tx, rawApp, tenantID, registryEnv); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "system", TenantID: tenantID, Action: "devseed.applied", TargetType: "tenant",
			TargetID: tenantID, Rationale: "explicit development seed (DEV_SEED=true)", Metadata: map[string]any{"users": len(f.Users), "apps": len(f.Apps)}})
	})
}

func bind(ctx context.Context, tx pgx.Tx, tenantID, workspaceID, userID, role string) error {
	var ws any
	if workspaceID != "" {
		ws = workspaceID
	}
	_, err := tx.Exec(ctx, `INSERT INTO role_bindings (tenant_id, workspace_id, user_id, role_id, reason)
SELECT $1::uuid, $2::uuid, $3::uuid, r.id, 'development seed' FROM roles r WHERE r.key = $4
  AND ((r.scope = 'tenant') = ($2::uuid IS NULL))
  AND NOT EXISTS (SELECT 1 FROM role_bindings b WHERE b.user_id = $3::uuid AND b.role_id = r.id AND b.state = 'active'
                  AND b.workspace_id IS NOT DISTINCT FROM $2::uuid)`, tenantID, ws, userID, role)
	return err
}

// seedApp registers a sample manifest; entries marked "published" are published, others stay draft. For an
// app that already exists, a seed entry with a strictly greater version is added as a new version (and
// published when marked so, superseding the previous release) — the same transitions as the admin
// AddVersion/Publish API, attributed to the system. Equal or lower versions are left untouched, and disabled or
// retired apps are never re-enabled, so re-applying the seed is idempotent.
func seedApp(ctx context.Context, tx pgx.Tx, raw json.RawMessage, tenantID, registryEnv string) error {
	var sample apps.Manifest
	if err := json.Unmarshal(raw, &sample); err != nil {
		return err
	}
	publish := sample.Status == "published"
	sample.Status, sample.Environment = "draft", registryEnv
	if issues := apps.Validate(sample, registryEnv); len(issues) > 0 {
		return fmt.Errorf("seed manifest %s invalid: %v", sample.AppKey, issues)
	}
	canon, sum, err := apps.Canonical(sample)
	if err != nil {
		return err
	}
	var appID, status, route string
	var publishedID *string
	existing := false
	err = tx.QueryRow(ctx, `SELECT id::text, status, route_base, published_version_id::text FROM mini_apps WHERE app_key = $1 FOR UPDATE`, sample.AppKey).
		Scan(&appID, &status, &route, &publishedID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `INSERT INTO mini_apps (app_key, route_base, owner_team) VALUES ($1, $2, $3) RETURNING id::text`,
			sample.AppKey, sample.RouteBase, sample.OwnerTeam).Scan(&appID); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		existing = true
		var latest string
		if err := tx.QueryRow(ctx, `SELECT version FROM mini_app_versions WHERE mini_app_id = $1::uuid ORDER BY created_at DESC LIMIT 1`, appID).Scan(&latest); err != nil {
			return err
		}
		if !apps.SemverGreater(sample.Version, latest) || route != sample.RouteBase || status == "disabled" || status == "retired" {
			return nil
		}
	}
	var vid string
	if err := tx.QueryRow(ctx, `INSERT INTO mini_app_versions (mini_app_id, version, manifest, checksum) VALUES ($1::uuid, $2, $3, $4) RETURNING id::text`,
		appID, sample.Version, canon, sum).Scan(&vid); err != nil {
		return err
	}
	if existing { // admin UIs hold If-Match revisions; a seeded upgrade must invalidate them
		if _, err := tx.Exec(ctx, `UPDATE mini_apps SET revision = revision + 1, updated_at = now() WHERE id = $1::uuid`, appID); err != nil {
			return err
		}
	}
	if !publish {
		return nil
	}
	if publishedID != nil {
		if _, err := tx.Exec(ctx, `UPDATE mini_app_versions SET release_state = 'superseded' WHERE id = $1::uuid`, *publishedID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE mini_app_versions SET release_state = 'published', published_at = now() WHERE id = $1::uuid`, vid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE mini_app_routes SET enabled = false WHERE registered_path = $1 AND environment = $2 AND enabled`, sample.RouteBase, registryEnv); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO mini_app_routes (app_version_id, environment, registered_path, upstream_config_ref) VALUES ($1::uuid, $2, $3, $4)`,
		vid, registryEnv, sample.RouteBase, sample.AppKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE mini_apps SET status = 'published', published_version_id = $2::uuid, disabled_reason = NULL, updated_at = now() WHERE id = $1::uuid`,
		appID, vid); err != nil {
		return err
	}
	if !existing {
		return nil // first seed of a fresh database: covered by the devseed.applied event
	}
	return audit.Record(ctx, tx, audit.Event{ActorType: "system", TenantID: tenantID, Action: "app.published", TargetType: "mini_app", TargetID: sample.AppKey,
		Rationale: "development seed (DEV_SEED=true)", Metadata: map[string]any{"version": sample.Version, "checksum": sum, "source": "devseed"}})
}

// BootstrapAdmin creates the first platform admin for a fresh tenant (break-glass, audited). It refuses when
// any active platform admin already exists, so it cannot be used to add admins later.
func BootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, tenantID, issuer, subject, name, email string) error {
	return postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM role_bindings b JOIN roles r ON r.id = b.role_id
WHERE r.key = 'platform_admin' AND b.tenant_id = $1::uuid AND b.state = 'active'`, tenantID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("a platform admin already exists; use the dual-control grant flow")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO users (tenant_id, idp_issuer, idp_subject, display_name, email) VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''))
ON CONFLICT (idp_issuer, idp_subject) DO NOTHING`, tenantID, issuer, subject, name, email); err != nil {
			return err
		}
		var uid string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE idp_issuer = $1 AND idp_subject = $2 AND tenant_id = $3::uuid`, issuer, subject, tenantID).Scan(&uid); err != nil {
			return err
		}
		if err := bind(ctx, tx, tenantID, "", uid, "platform_admin"); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "system", TenantID: tenantID, Action: "role_binding.bootstrap_admin",
			TargetType: "user", TargetID: uid, Rationale: "first platform admin bootstrap (CLI)", Metadata: map[string]any{"role": "platform_admin"}})
	})
}
