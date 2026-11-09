// Package pgtest gives integration tests an isolated, migrated PostgreSQL schema.
//
// Tests are skipped unless FLOWCASE_TEST_DATABASE_URL is set. With FLOWCASE_REQUIRE_DB=1 (CI and
// `make test-integration`) a missing database fails the run instead, so skips cannot pass silently.
package pgtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/platform/observability"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/migrations"
)

// Raw returns a pool on a fresh empty schema (no migrations applied).
func Raw(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("FLOWCASE_TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("FLOWCASE_REQUIRE_DB") == "1" {
			t.Fatal("FLOWCASE_TEST_DATABASE_URL is required for integration tests")
		}
		t.Skip("set FLOWCASE_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := postgres.Open(ctx, base, 2)
	if err != nil {
		t.Fatal(err)
	}
	schema := "t_" + observability.NewID()[:16]
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, err := postgres.Open(ctx, u.String(), 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_, _ = admin.Exec(cctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
		admin.Close()
	})
	return pool
}

// Migrated returns a pool on a fresh schema with all migrations applied.
func Migrated(t *testing.T) (*pgxpool.Pool, postgres.Migrator) {
	t.Helper()
	pool := Raw(t)
	migs, err := postgres.LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	m := postgres.Migrator{Pool: pool, Migrations: migs}
	if _, err := m.Up(context.Background()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return pool, m
}
