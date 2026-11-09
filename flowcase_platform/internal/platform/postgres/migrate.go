package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLockID is an arbitrary constant for pg_advisory_lock so concurrent deploys serialize.
const migrationLockID int64 = 0x666c6f77636173 // "flowcas"

var migrationName = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.(up|down)\.sql$`)

type Migration struct {
	Version  int
	Name     string
	Up       string
	Down     string
	Checksum string
}

// LoadMigrations reads paired up/down files from fsys and orders them by version.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	byVersion := map[int]*Migration{}
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		version, _ := strconv.Atoi(m[1])
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		mig := byVersion[version]
		if mig == nil {
			mig = &Migration{Version: version, Name: m[2]}
			byVersion[version] = mig
		} else if mig.Name != m[2] {
			return nil, fmt.Errorf("migration %06d has conflicting names %q and %q", version, mig.Name, m[2])
		}
		if m[3] == "up" {
			mig.Up = string(body)
		} else {
			mig.Down = string(body)
		}
	}
	out := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.Up == "" || m.Down == "" {
			return nil, fmt.Errorf("migration %06d_%s needs both up and down files", m.Version, m.Name)
		}
		sum := sha256.Sum256([]byte(m.Up))
		m.Checksum = hex.EncodeToString(sum[:])
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 1; found %06d at position %d", m.Version, i+1)
		}
	}
	return out, nil
}

type Migrator struct {
	Pool       *pgxpool.Pool
	Migrations []Migration
}

const ensureTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version    integer PRIMARY KEY,
    name       text NOT NULL,
    checksum   text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`

// Latest is the schema version this binary expects.
func (m Migrator) Latest() int { return len(m.Migrations) }

// Up applies all pending migrations, each in its own transaction, and rejects edited migrations.
func (m Migrator) Up(ctx context.Context) (applied []int, err error) {
	err = m.withLock(ctx, func(conn *pgxpool.Conn) error {
		done, err := appliedChecksums(ctx, conn)
		if err != nil {
			return err
		}
		for _, mig := range m.Migrations {
			if sum, ok := done[mig.Version]; ok {
				if sum != mig.Checksum {
					return fmt.Errorf("migration %06d_%s was modified after being applied", mig.Version, mig.Name)
				}
				continue
			}
			if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, mig.Up); err != nil {
					return fmt.Errorf("apply %06d_%s: %w", mig.Version, mig.Name, err)
				}
				_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`, mig.Version, mig.Name, mig.Checksum)
				return err
			}); err != nil {
				return err
			}
			applied = append(applied, mig.Version)
		}
		return nil
	})
	return applied, err
}

// Down reverts the most recent `steps` migrations.
func (m Migrator) Down(ctx context.Context, steps int) (reverted []int, err error) {
	err = m.withLock(ctx, func(conn *pgxpool.Conn) error {
		done, err := appliedChecksums(ctx, conn)
		if err != nil {
			return err
		}
		for i := len(m.Migrations) - 1; i >= 0 && len(reverted) < steps; i-- {
			mig := m.Migrations[i]
			if _, ok := done[mig.Version]; !ok {
				continue
			}
			if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, mig.Down); err != nil {
					return fmt.Errorf("revert %06d_%s: %w", mig.Version, mig.Name, err)
				}
				_, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, mig.Version)
				return err
			}); err != nil {
				return err
			}
			reverted = append(reverted, mig.Version)
		}
		return nil
	})
	return reverted, err
}

var ErrSchemaBehind = errors.New("database schema is not at the expected version")

// Check verifies the database has exactly the migrations this binary ships (used by /readyz).
func (m Migrator) Check(ctx context.Context) error {
	conn, err := m.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrSchemaBehind
	}
	done, err := appliedChecksums(ctx, conn)
	if err != nil {
		return err
	}
	if len(done) != len(m.Migrations) {
		return ErrSchemaBehind
	}
	for _, mig := range m.Migrations {
		if done[mig.Version] != mig.Checksum {
			return ErrSchemaBehind
		}
	}
	return nil
}

func (m Migrator) withLock(ctx context.Context, fn func(*pgxpool.Conn) error) error {
	conn, err := m.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return err
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID) }()
	if _, err := conn.Exec(ctx, ensureTableSQL); err != nil {
		return err
	}
	return fn(conn)
}

func appliedChecksums(ctx context.Context, conn *pgxpool.Conn) (map[int]string, error) {
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	out := map[int]string{}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return nil, err
		}
		out[v] = sum
	}
	rows.Close()
	return out, rows.Err()
}
