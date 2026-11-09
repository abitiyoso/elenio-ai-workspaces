package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/testutil/pgtest"
)

const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type fixture struct {
	pool *pgxpool.Pool
	ctx  context.Context
}

func (f fixture) exec(t *testing.T, sql string, args ...any) error {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, sql, args...)
	return err
}

func (f fixture) mustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := f.exec(t, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", strings.Fields(sql)[0], err)
	}
}

func (f fixture) id(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&id); err != nil {
		t.Fatalf("query id: %v", err)
	}
	return id
}

// tenantWithMember creates a tenant, a human user and an active membership.
func (f fixture) tenantWithMember(t *testing.T, ref string) (tenantID, userID string) {
	t.Helper()
	tenantID = f.id(t, `INSERT INTO tenants (external_ref, name) VALUES ($1, $1) RETURNING id::text`, ref)
	userID = f.id(t, `INSERT INTO users (issuer, subject, display_name) VALUES ('https://host.example', $1, $1) RETURNING id::text`, ref+"-user")
	f.mustExec(t, `INSERT INTO memberships (tenant_id, user_id) VALUES ($1, $2)`, tenantID, userID)
	return tenantID, userID
}

func (f fixture) definition(t *testing.T, tenantID, userID, key string) string {
	t.Helper()
	return f.id(t, `INSERT INTO workflow_definitions (tenant_id, key, name, created_by) VALUES ($1, $2, 'Test workflow', $3) RETURNING id::text`, tenantID, key, userID)
}

func (f fixture) draftVersion(t *testing.T, tenantID, defID, userID string, version int) string {
	t.Helper()
	return f.id(t, `INSERT INTO workflow_versions (tenant_id, definition_id, version, schema_version, status, definition, created_by)
		VALUES ($1, $2, $3, '1.0', 'draft', '{"nodes":[]}', $4) RETURNING id::text`, tenantID, defID, version, userID)
}

func (f fixture) publish(t *testing.T, versionID, userID, hash string) {
	t.Helper()
	f.mustExec(t, `UPDATE workflow_versions SET status = 'published', definition_hash = $2, published_by = $3, published_at = now() WHERE id = $1`, versionID, hash, userID)
}

func newFixture(t *testing.T) fixture {
	pool, _ := pgtest.Migrated(t)
	return fixture{pool: pool, ctx: context.Background()}
}

func expectCode(t *testing.T, err error, code, what string) {
	t.Helper()
	expectAnyCode(t, err, what, code)
}

func expectAnyCode(t *testing.T, err error, what string, codes ...string) {
	t.Helper()
	for _, code := range codes {
		if postgres.HasCode(err, code) {
			return
		}
	}
	t.Fatalf("%s: expected SQLSTATE in %v, got %v", what, codes, err)
}

func TestMigrationsUpDownAndChecksum(t *testing.T) {
	pool := pgtest.Raw(t)
	ctx := context.Background()
	migs, err := postgres.LoadMigrations(migrationsFS())
	if err != nil {
		t.Fatal(err)
	}
	m := postgres.Migrator{Pool: pool, Migrations: migs}

	if err := m.Check(ctx); !errors.Is(err, postgres.ErrSchemaBehind) {
		t.Fatalf("empty schema should be behind, got %v", err)
	}
	applied, err := m.Up(ctx)
	if err != nil || len(applied) != m.Latest() {
		t.Fatalf("first up: applied=%v err=%v", applied, err)
	}
	if again, err := m.Up(ctx); err != nil || len(again) != 0 {
		t.Fatalf("second up must be a no-op: %v %v", again, err)
	}
	if err := m.Check(ctx); err != nil {
		t.Fatalf("schema should be current: %v", err)
	}
	reverted, err := m.Down(ctx, m.Latest())
	if err != nil || len(reverted) != m.Latest() {
		t.Fatalf("down all: %v %v", reverted, err)
	}
	var tables int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename <> 'schema_migrations'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("down should remove every table, %d left (%v)", tables, err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("re-up after down: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET checksum = $1 WHERE version = 1`, hashA); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("edited migration must be rejected, got %v", err)
	}
	if err := m.Check(ctx); !errors.Is(err, postgres.ErrSchemaBehind) {
		t.Fatalf("readiness must fail on checksum drift, got %v", err)
	}
}

func TestPublishedWorkflowVersionIsImmutable(t *testing.T) {
	f := newFixture(t)
	tenant, user := f.tenantWithMember(t, "acme")
	def := f.definition(t, tenant, user, "poc-request")
	v1 := f.draftVersion(t, tenant, def, user, 1)

	f.mustExec(t, `UPDATE workflow_versions SET definition = '{"nodes":[{"id":"start"}]}' WHERE id = $1`, v1)
	if err := f.exec(t, `UPDATE workflow_versions SET status = 'published' WHERE id = $1`, v1); err == nil {
		t.Fatal("publishing without hash and publisher must violate the check constraint")
	}
	f.publish(t, v1, user, hashA)

	for name, sql := range map[string]string{
		"content":       `UPDATE workflow_versions SET definition = '{"nodes":[]}' WHERE id = $1`,
		"hash":          `UPDATE workflow_versions SET definition_hash = '` + hashB + `' WHERE id = $1`,
		"version":       `UPDATE workflow_versions SET version = 9 WHERE id = $1`,
		"back to draft": `UPDATE workflow_versions SET status = 'draft' WHERE id = $1`,
		"delete":        `DELETE FROM workflow_versions WHERE id = $1`,
	} {
		expectCode(t, f.exec(t, sql, v1), postgres.CodeImmutable, name)
	}
	expectCode(t, f.exec(t, `TRUNCATE workflow_versions CASCADE`), postgres.CodeImmutable, "truncate")

	f.mustExec(t, `UPDATE workflow_versions SET status = 'deprecated', deprecated_by = $2, deprecated_at = now() WHERE id = $1`, v1, user)
	expectCode(t, f.exec(t, `UPDATE workflow_versions SET definition = '{}' WHERE id = $1`, v1), postgres.CodeImmutable, "deprecated content")

	v2 := f.draftVersion(t, tenant, def, user, 2)
	f.mustExec(t, `DELETE FROM workflow_versions WHERE id = $1`, v2) // drafts remain editable/deletable
}

func TestCasesArePinnedToPublishedVersions(t *testing.T) {
	f := newFixture(t)
	tenant, user := f.tenantWithMember(t, "acme")
	def := f.definition(t, tenant, user, "sim-tender")
	v1 := f.draftVersion(t, tenant, def, user, 1)

	insertCase := func(ref, version string, n int, hash string) error {
		return f.exec(t, `INSERT INTO cases (tenant_id, reference, title, workflow_version_id, workflow_key, workflow_version, definition_hash, owner_id, created_by)
			VALUES ($1, $2, 'Case', $3, 'sim-tender', $4, $5, $6, $6)`, tenant, ref, version, n, hash, user)
	}
	expectCode(t, insertCase("C-1", v1, 1, hashA), postgres.CodePinInvalid, "case from draft")
	f.publish(t, v1, user, hashA)
	expectCode(t, insertCase("C-1", v1, 1, hashB), postgres.CodePinInvalid, "mismatched hash")
	expectCode(t, insertCase("C-1", v1, 2, hashA), postgres.CodePinInvalid, "mismatched version number")
	if err := insertCase("C-1", v1, 1, hashA); err != nil {
		t.Fatalf("case from published v1: %v", err)
	}

	v2 := f.draftVersion(t, tenant, def, user, 2)
	f.publish(t, v2, user, hashB)
	expectCode(t, f.exec(t, `UPDATE cases SET workflow_version_id = $1, workflow_version = 2, definition_hash = $2 WHERE reference = 'C-1'`, v2, hashB),
		postgres.CodeImmutable, "re-pin after v2 publish")
	f.mustExec(t, `UPDATE cases SET status = 'active', version_num = version_num + 1 WHERE reference = 'C-1'`)

	var pinned int
	if err := f.pool.QueryRow(f.ctx, `SELECT workflow_version FROM cases WHERE reference = 'C-1'`).Scan(&pinned); err != nil || pinned != 1 {
		t.Fatalf("case must stay on v1, got %d (%v)", pinned, err)
	}

	f.mustExec(t, `UPDATE workflow_versions SET status = 'deprecated', deprecated_by = $2, deprecated_at = now() WHERE id = $1`, v1, user)
	expectCode(t, insertCase("C-2", v1, 1, hashA), postgres.CodePinInvalid, "case from deprecated version")
}

func TestTenantIsolationByCompositeForeignKeys(t *testing.T) {
	f := newFixture(t)
	tenantA, userA := f.tenantWithMember(t, "tenant-a")
	tenantB, userB := f.tenantWithMember(t, "tenant-b")
	defA := f.definition(t, tenantA, userA, "poc-request")
	vA := f.draftVersion(t, tenantA, defA, userA, 1)
	f.publish(t, vA, userA, hashA)

	const fkViolation = "23503"
	// The pin trigger looks the version up within the case's tenant, so it rejects before the FK does.
	expectAnyCode(t, f.exec(t, `INSERT INTO cases (tenant_id, reference, title, workflow_version_id, workflow_key, workflow_version, definition_hash, owner_id, created_by)
		VALUES ($1, 'X-1', 'Cross tenant', $2, 'poc-request', 1, $3, $4, $4)`, tenantB, vA, hashA, userB), "tenant B case on tenant A version", postgres.CodePinInvalid, fkViolation)
	expectCode(t, f.exec(t, `INSERT INTO cases (tenant_id, reference, title, workflow_version_id, workflow_key, workflow_version, definition_hash, owner_id, created_by)
		VALUES ($1, 'X-2', 'Foreign owner', $2, 'poc-request', 1, $3, $4, $4)`, tenantA, vA, hashA, userB), fkViolation, "tenant A case owned by tenant B user")
	expectCode(t, f.exec(t, `INSERT INTO workflow_definitions (tenant_id, key, name, created_by) VALUES ($1, 'foreign', 'Foreign author', $2)`, tenantA, userB),
		fkViolation, "definition authored by non-member")
	expectCode(t, f.exec(t, `INSERT INTO workflow_versions (tenant_id, definition_id, version, schema_version, status, definition, created_by)
		VALUES ($1, $2, 2, '1.0', 'draft', '{}', $3)`, tenantB, defA, userB), fkViolation, "tenant B version of tenant A definition")
	expectCode(t, f.exec(t, `INSERT INTO role_assignments (tenant_id, user_id, role) VALUES ($1, $2, 'tenant_admin')`, tenantA, userB),
		fkViolation, "role in tenant without membership")
}

func TestAuditEventsAreAppendOnly(t *testing.T) {
	f := newFixture(t)
	tenant, user := f.tenantWithMember(t, "acme")
	id := f.id(t, `INSERT INTO audit_events (tenant_id, actor_type, actor_id, action, subject_type, subject_id, trace_id)
		VALUES ($1, 'user', $2, 'workflow.published', 'workflow_version', 'v1', 'trace-1') RETURNING id::text`, tenant, user)
	expectCode(t, f.exec(t, `UPDATE audit_events SET action = 'workflow.deleted' WHERE id = $1`, id), postgres.CodeImmutable, "update audit")
	expectCode(t, f.exec(t, `DELETE FROM audit_events WHERE id = $1`, id), postgres.CodeImmutable, "delete audit")
	expectCode(t, f.exec(t, `TRUNCATE audit_events`), postgres.CodeImmutable, "truncate audit")
	if err := f.exec(t, `INSERT INTO audit_events (tenant_id, actor_type, action, subject_type, subject_id) VALUES ($1, 'user', 'case.created', 'case', 'c')`, tenant); err == nil {
		t.Fatal("user audit events must name the actor")
	}
}

func TestExecutionAndInboxDedupConstraints(t *testing.T) {
	f := newFixture(t)
	tenant, user := f.tenantWithMember(t, "acme")
	def := f.definition(t, tenant, user, "poc-request")
	v := f.draftVersion(t, tenant, def, user, 1)
	f.publish(t, v, user, hashA)
	caseID := f.id(t, `INSERT INTO cases (tenant_id, reference, title, workflow_version_id, workflow_key, workflow_version, definition_hash, owner_id, created_by)
		VALUES ($1, 'C-1', 'Case', $2, 'poc-request', 1, $3, $4, $4) RETURNING id::text`, tenant, v, hashA, user)
	token := f.id(t, `INSERT INTO execution_tokens (tenant_id, case_id) VALUES ($1, $2) RETURNING id::text`, tenant, caseID)

	insertExec := func(key string) error {
		return f.exec(t, `INSERT INTO node_executions (tenant_id, case_id, token_id, node_id, node_type, idempotency_key) VALUES ($1, $2, $3, 'scope', 'human_task', $4)`, tenant, caseID, token, key)
	}
	const uniqueViolation = "23505"
	if err := insertExec("k-1"); err != nil {
		t.Fatal(err)
	}
	expectCode(t, insertExec("k-2"), uniqueViolation, "same case+node+iteration+token")
	expectCode(t, f.exec(t, `INSERT INTO node_executions (tenant_id, case_id, token_id, node_id, node_type, iteration, idempotency_key) VALUES ($1, $2, $3, 'scope', 'human_task', 2, 'k-1')`, tenant, caseID, token),
		uniqueViolation, "reused idempotency key")
	expectCode(t, f.exec(t, `INSERT INTO node_executions (tenant_id, case_id, token_id, node_id, node_type, idempotency_key) VALUES ($1, $2, $3, 'x', 'javascript', 'k-3')`, tenant, caseID, token),
		"23514", "unknown node type")

	f.mustExec(t, `INSERT INTO inbox_dedup (source, message_id, tenant_id, payload_hash) VALUES ('agent:stub', 'm-1', $1, $2)`, tenant, hashA)
	expectCode(t, f.exec(t, `INSERT INTO inbox_dedup (source, message_id, tenant_id, payload_hash) VALUES ('agent:stub', 'm-1', $1, $2)`, tenant, hashB), uniqueViolation, "duplicate callback")
}

func TestPostgresDirectoryResolvesOnlyActiveMembers(t *testing.T) {
	f := newFixture(t)
	tenant, user := f.tenantWithMember(t, "acme")
	f.tenantWithMember(t, "other")
	f.mustExec(t, `INSERT INTO role_assignments (tenant_id, user_id, role) VALUES ($1, $2, 'workflow_designer')`, tenant, user)
	f.mustExec(t, `INSERT INTO role_assignments (tenant_id, user_id, role, valid_from, valid_until) VALUES ($1, $2, 'tenant_admin', now() - interval '2 days', now() - interval '1 day')`, tenant, user)
	f.mustExec(t, `INSERT INTO role_assignments (tenant_id, user_id, role, revoked_at) VALUES ($1, $2, 'domain_approver', now())`, tenant, user)

	dir := auth.PostgresDirectory{DB: f.pool}
	claims := auth.Claims{Issuer: "https://host.example", Subject: "acme-user", TenantRef: "acme", ExpiresAt: time.Now().Add(time.Minute)}
	id, err := dir.Resolve(f.ctx, claims)
	if err != nil {
		t.Fatalf("active member should resolve: %v", err)
	}
	if id.TenantID != tenant || id.UserID != user || len(id.Roles) != 1 || id.Roles[0] != auth.RoleWorkflowDesigner {
		t.Fatalf("unexpected identity %+v", id)
	}

	reject := map[string]auth.Claims{
		"other tenant":   {Issuer: claims.Issuer, Subject: "acme-user", TenantRef: "other"},
		"unknown tenant": {Issuer: claims.Issuer, Subject: "acme-user", TenantRef: "nope"},
		"other issuer":   {Issuer: "https://evil.example", Subject: "acme-user", TenantRef: "acme"},
	}
	for name, c := range reject {
		if _, err := dir.Resolve(f.ctx, c); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("%s: expected ErrUnauthenticated, got %v", name, err)
		}
	}
	f.mustExec(t, `UPDATE memberships SET status = 'suspended' WHERE tenant_id = $1 AND user_id = $2`, tenant, user)
	if _, err := dir.Resolve(f.ctx, claims); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("suspended membership must be rejected, got %v", err)
	}
	f.mustExec(t, `UPDATE memberships SET status = 'active' WHERE tenant_id = $1 AND user_id = $2`, tenant, user)
	f.mustExec(t, `UPDATE tenants SET status = 'suspended' WHERE id = $1`, tenant)
	if _, err := dir.Resolve(f.ctx, claims); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("suspended tenant must be rejected, got %v", err)
	}
}

func TestSeedDevelopmentIsIdempotent(t *testing.T) {
	f := newFixture(t)
	seed := postgres.DevSeed{TenantID: "00000000-0000-4000-8000-000000000001", UserID: "00000000-0000-4000-8000-000000000101", DisplayName: "Local Developer", Roles: []string{"workspace_member", "workflow_designer"}}
	for range 2 {
		if err := postgres.SeedDevelopment(f.ctx, f.pool, seed); err != nil {
			t.Fatal(err)
		}
	}
	var roles int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM role_assignments WHERE user_id = $1`, seed.UserID).Scan(&roles); err != nil || roles != 2 {
		t.Fatalf("expected 2 roles after repeated seeding, got %d (%v)", roles, err)
	}
}
