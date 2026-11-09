// Package store is UCW's PostgreSQL access: embedded migrations, the immutable pack registry rows,
// case bindings and audit events. No other service's database is ever touched.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/migrations"
)

type Store struct {
	DB *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid UCW_DATABASE_URL")
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{DB: pool}, nil
}

func (s *Store) Close() { s.DB.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.DB.Ping(ctx) }

// Migrate applies embedded *.up.sql files in order under an advisory lock; each file runs in one
// transaction and is recorded in schema_migrations.
func (s *Store) Migrate(ctx context.Context) error {
	names, err := fs.Glob(migrations.FS, "*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(84800001)`); err != nil {
		return err
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(84800001)`) }()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, name text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, name := range names {
		v, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad version prefix", name)
		}
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, v).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("migration %s: %w", name, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, v, name)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// --- pack registry rows -----------------------------------------------------------------------------

func (s *Store) ListPackVersions(ctx context.Context) ([]packs.StoredVersion, error) {
	rows, err := s.DB.Query(ctx, `SELECT pack_key, version, checksum, manifest, status, source_file FROM workspace_pack_versions`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (packs.StoredVersion, error) {
		var v packs.StoredVersion
		var manifest []byte
		err := r.Scan(&v.PackKey, &v.Version, &v.Checksum, &manifest, &v.Status, &v.SourceFile)
		v.Manifest = manifest
		return v, err
	})
}

// InsertPackVersion stores a new version; an existing row is never touched (the table is insert-only).
func (s *Store) InsertPackVersion(ctx context.Context, v packs.StoredVersion) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO workspace_pack_versions (pack_key, version, checksum, manifest, status, source_file)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (pack_key, version) DO NOTHING`,
		v.PackKey, v.Version, v.Checksum, []byte(v.Manifest), v.Status, v.SourceFile)
	return err
}

// --- case bindings ----------------------------------------------------------------------------------

type Binding struct {
	CaseID      string    `json:"case_id"`
	PackKey     string    `json:"pack_key"`
	PackVersion string    `json:"pack_version"`
	Binding     string    `json:"binding"`
	BoundAt     time.Time `json:"bound_at"`
	BoundBy     string    `json:"bound_by"`
}

const (
	PinnedAtCreation  = "pinned_at_creation"
	PinnedOnFirstView = "pinned_on_first_view"
)

func (s *Store) GetBinding(ctx context.Context, tenant, caseID string) (*Binding, error) {
	var b Binding
	err := s.DB.QueryRow(ctx, `SELECT case_id, pack_key, pack_version, binding, bound_at, bound_by FROM workspace_case_bindings
		WHERE tenant_key = $1 AND case_id = $2`, tenant, caseID).Scan(&b.CaseID, &b.PackKey, &b.PackVersion, &b.Binding, &b.BoundAt, &b.BoundBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) GetBindings(ctx context.Context, tenant string, caseIDs []string) (map[string]Binding, error) {
	out := map[string]Binding{}
	if len(caseIDs) == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT case_id, pack_key, pack_version, binding, bound_at, bound_by FROM workspace_case_bindings
		WHERE tenant_key = $1 AND case_id = ANY($2)`, tenant, caseIDs)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Binding, error) {
		var b Binding
		return b, r.Scan(&b.CaseID, &b.PackKey, &b.PackVersion, &b.Binding, &b.BoundAt, &b.BoundBy)
	})
	for _, b := range list {
		out[b.CaseID] = b
	}
	return out, err
}

// Bind pins a case once. If a binding already exists it is returned unchanged (never re-pinned).
func (s *Store) Bind(ctx context.Context, tenant string, b Binding) (*Binding, error) {
	if _, err := s.DB.Exec(ctx, `INSERT INTO workspace_case_bindings (tenant_key, case_id, pack_key, pack_version, binding, bound_by)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (tenant_key, case_id) DO NOTHING`,
		tenant, b.CaseID, b.PackKey, b.PackVersion, b.Binding, b.BoundBy); err != nil {
		return nil, err
	}
	return s.GetBinding(ctx, tenant, b.CaseID)
}

// --- audit ------------------------------------------------------------------------------------------

type AuditEvent struct {
	TenantKey     string
	ActorRef      string
	Action        string
	ResourceRef   string
	CorrelationID string
	Metadata      map[string]any
}

func (s *Store) Audit(ctx context.Context, e AuditEvent) error {
	meta, _ := json.Marshal(e.Metadata)
	if e.Metadata == nil {
		meta = []byte(`{}`)
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO workspace_audit_events (tenant_key, actor_ref, action, resource_ref, correlation_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)`, e.TenantKey, e.ActorRef, e.Action, e.ResourceRef, e.CorrelationID, meta)
	return err
}
