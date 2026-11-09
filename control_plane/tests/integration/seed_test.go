package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/elenio/control-plane/internal/devseed"
)

// An existing database keeps its registry rows; re-applying a seed with a greater version adds and (when marked)
// publishes it, exactly once. This is how the VM's draft case-workspace row becomes the published 0.2.0.
func TestDevSeedUpgradesExistingDraftAppIdempotently(t *testing.T) {
	e := newEnv(t, envOptions{})
	ctx := context.Background()
	write := func(version, status string) string {
		m := manifest("seed-demo", version)
		m["status"], m["required_entitlements"] = status, []string{"case-workspace:open"}
		raw, _ := json.Marshal(map[string]any{"apps": []any{m}})
		p := filepath.Join(t.TempDir(), "seed.json")
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	apply := func(path string) {
		t.Helper()
		if err := devseed.Apply(ctx, e.pool, path, e.tenantID, e.idp.srv.URL, e.cfg.RegistryEnv); err != nil {
			t.Fatal(err)
		}
	}
	state := func() (status, published string, revision int) {
		t.Helper()
		if err := e.pool.QueryRow(ctx, `SELECT a.status, COALESCE(v.version, ''), a.revision FROM mini_apps a
LEFT JOIN mini_app_versions v ON v.id = a.published_version_id WHERE a.app_key = 'seed-demo'`).Scan(&status, &published, &revision); err != nil {
			t.Fatal(err)
		}
		return
	}
	apply(write("0.1.0", "draft"))
	if st, pub, _ := state(); st != "draft" || pub != "" {
		t.Fatalf("initial seed: %s %q", st, pub)
	}
	upgrade := write("0.2.0", "published")
	apply(upgrade)
	st, pub, rev := state()
	if st != "published" || pub != "0.2.0" {
		t.Fatalf("upgrade not published: %s %q", st, pub)
	}
	apply(upgrade) // idempotent
	if _, _, rev2 := state(); rev2 != rev {
		t.Fatalf("re-apply changed revision %d -> %d", rev, rev2)
	}
	apply(write("0.1.5", "published")) // never downgrades
	if _, pub, _ := state(); pub != "0.2.0" {
		t.Fatalf("downgrade applied: %q", pub)
	}
	var routes, audits int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM mini_app_routes WHERE registered_path = '/apps/seed-demo/' AND enabled`).Scan(&routes)
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM platform_audit_events WHERE action = 'app.published' AND target_id = 'seed-demo'`).Scan(&audits)
	mustEqual(t, routes, 1, "enabled routes")
	mustEqual(t, audits, 1, "seed publish audit rows")

	// A disabled app is never re-enabled by the seed.
	if _, err := e.pool.Exec(ctx, `UPDATE mini_apps SET status = 'disabled' WHERE app_key = 'seed-demo'`); err != nil {
		t.Fatal(err)
	}
	apply(write("0.3.0", "published"))
	if st, pub, _ := state(); st != "disabled" || pub != "0.2.0" {
		t.Fatalf("disabled app changed by seed: %s %q", st, pub)
	}

	// The shipped seed publishes case-workspace 0.2.0.
	var cw string
	_ = e.pool.QueryRow(ctx, `SELECT v.version FROM mini_apps a JOIN mini_app_versions v ON v.id = a.published_version_id WHERE a.app_key = 'case-workspace'`).Scan(&cw)
	mustEqual(t, cw, "0.2.0", "case-workspace published version")
}
