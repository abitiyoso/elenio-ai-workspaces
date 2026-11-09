package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/testutil/pgtest"
	"github.com/elenio/flowcase/internal/workflows/definition"
	"github.com/elenio/flowcase/internal/workflows/registry"
	"github.com/elenio/flowcase/internal/workflows/validator"
)

type env struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	svc  *registry.Service
	ps   []packs.Pack
}

func newEnv(t *testing.T) *env {
	pool, _ := pgtest.Migrated(t)
	ps, err := packs.Load()
	if err != nil {
		t.Fatal(err)
	}
	cat, err := packs.Catalog(ps)
	if err != nil {
		t.Fatal(err)
	}
	tpls, err := packs.Templates(ps)
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, ctx: context.Background(), pool: pool, ps: ps, svc: &registry.Service{DB: pool, Catalog: cat, Templates: tpls}}
}

// member creates (or reuses) a tenant and adds a user with the given roles, returning the identity
// the directory would resolve.
func (e *env) member(tenantRef, subject, kind string, roles ...auth.Role) auth.Identity {
	e.t.Helper()
	var tenantID, userID string
	if err := e.pool.QueryRow(e.ctx, `INSERT INTO tenants (external_ref, name) VALUES ($1, $1)
		ON CONFLICT (external_ref) DO UPDATE SET name = EXCLUDED.name RETURNING id::text`, tenantRef).Scan(&tenantID); err != nil {
		e.t.Fatal(err)
	}
	if err := e.pool.QueryRow(e.ctx, `INSERT INTO users (issuer, subject, display_name, kind) VALUES ('https://host', $1, $1, $2) RETURNING id::text`,
		subject, kind).Scan(&userID); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO memberships (tenant_id, user_id) VALUES ($1, $2)`, tenantID, userID); err != nil {
		e.t.Fatal(err)
	}
	for _, r := range roles {
		if _, err := e.pool.Exec(e.ctx, `INSERT INTO role_assignments (tenant_id, user_id, role) VALUES ($1, $2, $3)`, tenantID, userID, r); err != nil {
			e.t.Fatal(err)
		}
	}
	return auth.Identity{UserID: userID, TenantID: tenantID, DisplayName: subject, Kind: kind, Roles: roles}
}

func (e *env) admin() auth.Identity {
	return e.member("acme", "ada", auth.KindHuman, auth.RoleWorkspaceMember, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)
}

func sampleBytes(t *testing.T, name string) []byte {
	raw, err := os.ReadFile("../../../samples/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func idem(key string, body ...[]byte) registry.Idempotency {
	return registry.Idempotency{Key: key, Hash: registry.RequestHash(body...)}
}

func TestSeedImportsBothPacks(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	log, err := e.svc.SeedPacks(e.ctx, a, e.ps)
	if err != nil {
		t.Fatalf("seed: %v (%v)", err, log)
	}
	sim, err := e.svc.GetDefinition(e.ctx, a, "sim-tender")
	if err != nil {
		t.Fatal(err)
	}
	if sim.LatestPublished == nil || *sim.LatestPublished != 4 || sim.OpenDraft == nil || *sim.OpenDraft != 5 {
		t.Fatalf("sim-tender should have published v1, v2 (case brief), v3 (author draft review), v4 (optional hidden IDs) and draft v5: %+v", sim.DefinitionSummary)
	}
	v2, err := e.svc.GetVersion(e.ctx, a, "sim-tender", 2)
	res2 := validator.Validate(sampleBytes(t, "sim_tender_v2.json"), e.svc.Catalog)
	want2, _ := definition.Hash(*res2.Definition)
	if err != nil || v2.Status != registry.StatusPublished || v2.DefinitionHash == nil || *v2.DefinitionHash != want2 {
		t.Fatalf("v2 must be published with the sample's content: %+v %v", v2, err)
	}
	v3, err := e.svc.GetVersion(e.ctx, a, "sim-tender", 3)
	res3 := validator.Validate(sampleBytes(t, "sim_tender_v3.json"), e.svc.Catalog)
	want3, _ := definition.Hash(*res3.Definition)
	if err != nil || v3.Status != registry.StatusPublished || v3.DefinitionHash == nil || *v3.DefinitionHash != want3 {
		t.Fatalf("v3 must be published with the sample's content: %+v %v", v3, err)
	}
	poc, err := e.svc.GetDefinition(e.ctx, a, "internal-poc-request")
	if err != nil || poc.LatestPublished == nil {
		t.Fatalf("poc pack should be published: %+v %v", poc, err)
	}

	// The stored hash equals the hash of the sample file's content.
	res := validator.Validate(sampleBytes(t, "sim_tender_v1.json"), e.svc.Catalog)
	want, _ := definition.Hash(*res.Definition)
	v1, err := e.svc.GetVersion(e.ctx, a, "sim-tender", 1)
	if err != nil || v1.DefinitionHash == nil || *v1.DefinitionHash != want {
		t.Fatalf("published hash %v != sample hash %s (%v)", v1.DefinitionHash, want, err)
	}

	if again, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil || len(again) == 0 {
		t.Fatalf("re-seeding must be a no-op: %v %v", again, err)
	}
	list, _, err := e.svc.ListDefinitions(e.ctx, a, registry.ListFilter{})
	if err != nil || len(list) != 2 {
		t.Fatalf("expected exactly two workflows after re-seed, got %d (%v)", len(list), err)
	}
}

// TestUpgradeSeededDatabaseToCaseBriefVersion replays the documented upgrade of a database seeded
// before v2 (case brief) existed: v1 published and the old illustrative draft open as v2.
func TestUpgradeSeededDatabaseToCaseBriefVersion(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	res, err := e.svc.CreateDefinition(e.ctx, a, idem("old-seed"), registry.CreateInput{Key: "sim-tender", Template: "sim-tender"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Publish(e.ctx, a, "sim-tender", 1, res.Version.VersionNum); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateDraftVersion(e.ctx, a, idem("old-draft"), "sim-tender", 0); err != nil {
		t.Fatal(err)
	}
	// Re-running the seed cannot touch an open draft.
	if _, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil {
		t.Fatal(err)
	}
	if v2, _ := e.svc.GetVersion(e.ctx, a, "sim-tender", 2); v2.Status != registry.StatusDraft {
		t.Fatalf("seed must leave the open v2 draft alone, got %s", v2.Status)
	}
	// PATCH v2 with samples/sim_tender_v2.json, validate, publish (designer, then publisher).
	v2, _ := e.svc.GetVersion(e.ctx, a, "sim-tender", 2)
	note := "SIM case brief inputs"
	if _, err := e.svc.UpdateDraft(e.ctx, a, "sim-tender", 2, v2.VersionNum, registry.UpdateInput{Definition: sampleBytes(t, "sim_tender_v2.json"), ChangeNote: &note}); err != nil {
		t.Fatal(err)
	}
	validated, err := e.svc.ValidateVersion(e.ctx, a, "sim-tender", 2)
	if err != nil || validated.Version.Status != registry.StatusValidated {
		t.Fatalf("validate: %v %+v", err, validated.Report.Errors())
	}
	published, err := e.svc.Publish(e.ctx, a, "sim-tender", 2, validated.Version.VersionNum)
	if err != nil {
		t.Fatal(err)
	}
	want := validator.Validate(sampleBytes(t, "sim_tender_v2.json"), e.svc.Catalog)
	hash, _ := definition.Hash(*want.Definition)
	if *published.Version.DefinitionHash != hash {
		t.Fatal("upgraded v2 must equal a freshly seeded v2")
	}
	// A later seed run publishes v3 (author draft review) and v4 (optional hidden IDs) and restores the illustrative security draft as v5.
	if _, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil {
		t.Fatal(err)
	}
	if v3, err := e.svc.GetVersion(e.ctx, a, "sim-tender", 3); err != nil || v3.Status != registry.StatusPublished {
		t.Fatalf("seed should publish v3: %+v %v", v3, err)
	}
	if v4, err := e.svc.GetVersion(e.ctx, a, "sim-tender", 4); err != nil || v4.Status != registry.StatusPublished {
		t.Fatalf("seed should publish v4: %+v %v", v4, err)
	}
	if v5, err := e.svc.GetVersion(e.ctx, a, "sim-tender", 5); err != nil || v5.Status != registry.StatusDraft {
		t.Fatalf("seed should add the v5 draft: %+v %v", v5, err)
	}
}

// TestCasePinnedToV1AfterV2Publish covers UAT-02 / the Definition of Done pinning requirement at the
// registry layer: publishing a later version (v5) neither mutates v1 nor re-pins a case started on v1.
func TestCasePinnedToV1AfterV2Publish(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	if _, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil {
		t.Fatal(err)
	}
	v1, _ := e.svc.GetVersion(e.ctx, a, "sim-tender", 1)
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO cases (tenant_id, reference, title, workflow_version_id, workflow_key, workflow_version, definition_hash, owner_id, created_by)
		VALUES ($1, 'SIM-1', 'Tender', $2, 'sim-tender', 1, $3, $4, $4)`, a.TenantID, v1.ID, *v1.DefinitionHash, a.UserID); err != nil {
		t.Fatal(err)
	}

	validated, err := e.svc.ValidateVersion(e.ctx, a, "sim-tender", 5)
	if err != nil || validated.Version.Status != registry.StatusValidated {
		t.Fatalf("v5 should validate: %+v %v", validated.Report.Errors(), err)
	}
	published, err := e.svc.Publish(e.ctx, a, "sim-tender", 5, validated.Version.VersionNum)
	if err != nil {
		t.Fatalf("publish v5: %v", err)
	}
	if *published.Version.DefinitionHash == *v1.DefinitionHash || published.Version.DerivedFromVersion == nil || *published.Version.DerivedFromVersion != 4 {
		t.Fatalf("v5 must be a distinct version derived from v4: %+v", published.Version)
	}

	var pinnedVersion int
	var pinnedHash string
	if err := e.pool.QueryRow(e.ctx, `SELECT workflow_version, definition_hash FROM cases WHERE reference = 'SIM-1'`).Scan(&pinnedVersion, &pinnedHash); err != nil {
		t.Fatal(err)
	}
	if pinnedVersion != 1 || pinnedHash != *v1.DefinitionHash {
		t.Fatalf("case moved off v1: v%d %s", pinnedVersion, pinnedHash)
	}
	after, _ := e.svc.GetVersion(e.ctx, a, "sim-tender", 1)
	if string(after.Definition) != string(v1.Definition) || *after.DefinitionHash != *v1.DefinitionHash || after.Status != registry.StatusPublished {
		t.Fatal("publishing v5 changed v1")
	}
}

func TestPublishedVersionsCannotBeEdited(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	if _, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil {
		t.Fatal(err)
	}
	v1, _ := e.svc.GetVersion(e.ctx, a, "internal-poc-request", 1)
	if _, err := e.svc.UpdateDraft(e.ctx, a, "internal-poc-request", 1, v1.VersionNum, registry.UpdateInput{Definition: v1.Definition}); !errors.Is(err, registry.ErrImmutable) {
		t.Fatalf("editing a published version must fail with ErrImmutable, got %v", err)
	}
	if _, err := e.svc.Publish(e.ctx, a, "internal-poc-request", 1, v1.VersionNum); !errors.Is(err, registry.ErrImmutable) {
		t.Fatalf("re-publishing must fail, got %v", err)
	}
	if _, err := e.pool.Exec(e.ctx, `UPDATE workflow_versions SET definition = '{}' WHERE id = $1`, v1.ID); !postgres.HasCode(err, postgres.CodeImmutable) {
		t.Fatalf("database must also refuse, got %v", err)
	}
}

func TestDraftLifecycleWithConcurrencyAndValidation(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	created, err := e.svc.CreateDefinition(e.ctx, a, idem("create-poc-copy-1", []byte("x")), registry.CreateInput{Key: "poc-copy", Template: "internal-poc-request", Name: "PoC copy"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version.Status != registry.StatusDraft || !created.Report.Valid {
		t.Fatalf("template draft should be valid: %+v", created.Report.Errors())
	}

	// Make the draft invalid: an approval bound to an unregistered policy (UAT-08).
	var doc map[string]any
	_ = json.Unmarshal(created.Version.Definition, &doc)
	for _, n := range doc["nodes"].([]any) {
		if nm := n.(map[string]any); nm["id"] == "budget_approval" {
			nm["config"].(map[string]any)["policy_ref"] = "poc.unknown"
		}
	}
	doc["policy_refs"] = []any{"poc.unknown"}
	bad, _ := json.Marshal(doc)
	saved, err := e.svc.UpdateDraft(e.ctx, a, "poc-copy", 1, created.Version.VersionNum, registry.UpdateInput{Definition: bad})
	if err != nil {
		t.Fatalf("drafts may be saved with semantic errors: %v", err)
	}
	if saved.Report.Valid {
		t.Fatal("saved report should show the unknown policy")
	}

	// A second writer holding the old ETag loses.
	if _, err := e.svc.UpdateDraft(e.ctx, a, "poc-copy", 1, created.Version.VersionNum, registry.UpdateInput{Definition: bad}); !errors.Is(err, registry.ErrPreconditionFailed) {
		t.Fatalf("stale If-Match must fail, got %v", err)
	}

	var verr *registry.ValidationError
	_, err = e.svc.Publish(e.ctx, a, "poc-copy", 1, saved.Version.VersionNum)
	if !errors.As(err, &verr) || !slices.ContainsFunc(verr.Report.Errors(), func(d definition.Diagnostic) bool { return d.Code == "unknown_policy_ref" }) {
		t.Fatalf("publish must be rejected with unknown_policy_ref, got %v", err)
	}

	// Structurally broken content is never stored.
	if _, err := e.svc.UpdateDraft(e.ctx, a, "poc-copy", 1, saved.Version.VersionNum, registry.UpdateInput{Definition: []byte(`{"nodes":"nope"}`)}); !errors.As(err, &verr) {
		t.Fatalf("structural errors must be rejected, got %v", err)
	}
	// The body cannot retarget another workflow or version.
	doc["key"] = "sim-tender"
	other, _ := json.Marshal(doc)
	if _, err := e.svc.UpdateDraft(e.ctx, a, "poc-copy", 1, saved.Version.VersionNum, registry.UpdateInput{Definition: other}); !errors.As(err, &verr) {
		t.Fatalf("key mismatch must be rejected, got %v", err)
	}

	// Fix, validate, publish.
	fixed, err := e.svc.UpdateDraft(e.ctx, a, "poc-copy", 1, saved.Version.VersionNum, registry.UpdateInput{Definition: created.Version.Definition})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := e.svc.Publish(e.ctx, a, "poc-copy", 1, fixed.Version.VersionNum)
	if err != nil || pub.Version.Status != registry.StatusPublished || pub.Version.PublishedByName == nil {
		t.Fatalf("publish after fix: %+v %v", pub.Version, err)
	}

	// Next version: only one open draft at a time.
	if _, err := e.svc.CreateDraftVersion(e.ctx, a, idem("fork-poc-copy-2"), "poc-copy", 0); err != nil {
		t.Fatal(err)
	}
	var conflict *registry.ConflictError
	if _, err := e.svc.CreateDraftVersion(e.ctx, a, idem("fork-poc-copy-3"), "poc-copy", 0); !errors.As(err, &conflict) || conflict.Code != "draft_exists" {
		t.Fatalf("second open draft must conflict, got %v", err)
	}

	// Deprecation keeps content and hash.
	dep, err := e.svc.Deprecate(e.ctx, a, "poc-copy", 1, pub.Version.VersionNum)
	if err != nil || dep.Version.Status != registry.StatusDeprecated || *dep.Version.DefinitionHash != *pub.Version.DefinitionHash {
		t.Fatalf("deprecate: %+v %v", dep.Version, err)
	}
}

func TestAuthorization(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	if _, err := e.svc.SeedPacks(e.ctx, admin, e.ps); err != nil {
		t.Fatal(err)
	}
	designer := e.member("acme", "dan", auth.KindHuman, auth.RoleWorkspaceMember, auth.RoleWorkflowDesigner)
	member := e.member("acme", "mia", auth.KindHuman, auth.RoleWorkspaceMember)
	agent := e.member("acme", "agent-runner", auth.KindService, auth.RoleWorkflowPublisher, auth.RoleWorkflowDesigner)
	outsider := e.member("globex", "olaf", auth.KindHuman, auth.RoleWorkspaceMember, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)

	v3, _ := e.svc.GetVersion(e.ctx, admin, "sim-tender", 3)
	for name, id := range map[string]auth.Identity{"designer": designer, "service principal": agent, "member": member} {
		if _, err := e.svc.Publish(e.ctx, id, "sim-tender", 3, v3.VersionNum); !errors.Is(err, registry.ErrForbidden) {
			t.Errorf("%s publish: expected ErrForbidden, got %v", name, err)
		}
	}
	if _, err := e.svc.CreateDefinition(e.ctx, member, idem("member-create-1"), registry.CreateInput{Key: "x-flow", Name: "X flow"}); !errors.Is(err, registry.ErrForbidden) {
		t.Errorf("member create: expected ErrForbidden, got %v", err)
	}
	if _, err := e.svc.UpdateDraft(e.ctx, member, "sim-tender", 3, v3.VersionNum, registry.UpdateInput{Definition: v3.Definition}); !errors.Is(err, registry.ErrForbidden) {
		t.Errorf("member edit: expected ErrForbidden, got %v", err)
	}
	if _, err := e.svc.GetVersion(e.ctx, member, "sim-tender", 1); err != nil {
		t.Errorf("members can read published workflows: %v", err)
	}
	if _, _, err := e.svc.ListAudit(e.ctx, member, auditFilter()); !errors.Is(err, registry.ErrForbidden) {
		t.Errorf("member audit: expected ErrForbidden, got %v", err)
	}

	// Another tenant sees nothing and cannot act, even with every role.
	if list, _, err := e.svc.ListDefinitions(e.ctx, outsider, registry.ListFilter{}); err != nil || len(list) != 0 {
		t.Errorf("outsider list leaked %d workflows (%v)", len(list), err)
	}
	if _, err := e.svc.GetVersion(e.ctx, outsider, "sim-tender", 1); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("outsider read: expected ErrNotFound, got %v", err)
	}
	if _, err := e.svc.Publish(e.ctx, outsider, "sim-tender", 3, v3.VersionNum); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("outsider publish: expected ErrNotFound, got %v", err)
	}
	// Same key in another tenant is independent.
	if _, err := e.svc.CreateDefinition(e.ctx, outsider, idem("globex-sim-1"), registry.CreateInput{Key: "sim-tender", Template: "sim-tender"}); err != nil {
		t.Errorf("tenants own their key namespace: %v", err)
	}
}

func TestIdempotentCreateAndDuplicate(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	body := []byte(`{"key":"travel","name":"Travel request"}`)
	in := registry.CreateInput{Key: "travel", Name: "Travel request"}
	first, err := e.svc.CreateDefinition(e.ctx, a, idem("create-travel-1", body), in)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.svc.CreateDefinition(e.ctx, a, idem("create-travel-1", body), in)
	if err != nil || replay.Version.ID != first.Version.ID {
		t.Fatalf("replay must return the original version: %v %v", replay.Version.ID, err)
	}
	if _, err := e.svc.CreateDefinition(e.ctx, a, idem("create-travel-1", []byte("different")), registry.CreateInput{Key: "travel-2", Name: "Other"}); !errors.Is(err, registry.ErrIdempotencyReuse) {
		t.Fatalf("reusing a key for another body must fail, got %v", err)
	}
	var input *registry.InputError
	if _, err := e.svc.CreateDefinition(e.ctx, a, registry.Idempotency{}, registry.CreateInput{Key: "travel-3", Name: "Travel 3"}); !errors.As(err, &input) {
		t.Fatalf("missing idempotency key must be an input error, got %v", err)
	}
	var conflict *registry.ConflictError
	if _, err := e.svc.CreateDefinition(e.ctx, a, idem("create-travel-dup"), in); !errors.As(err, &conflict) || conflict.Code != "key_exists" {
		t.Fatalf("duplicate key must conflict, got %v", err)
	}

	if _, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil {
		t.Fatal(err)
	}
	dup, err := e.svc.Duplicate(e.ctx, a, idem("dup-sim-1"), "sim-tender", registry.DuplicateInput{Key: "sim-tender-apac", Name: "SIM Tender APAC"})
	if err != nil || dup.Version.Version != 1 || dup.Version.Status != registry.StatusDraft || !dup.Report.Valid {
		t.Fatalf("duplicate: %+v %v %v", dup.Version, dup.Report.Errors(), err)
	}
	var d definition.Definition
	_ = json.Unmarshal(dup.Version.Definition, &d)
	if d.Key != "sim-tender-apac" || d.Name != "SIM Tender APAC" || len(d.Nodes) != 18 {
		t.Fatalf("duplicate content wrong: key=%s name=%s nodes=%d", d.Key, d.Name, len(d.Nodes))
	}
}

func TestPublishWritesAuditAndOutbox(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	if _, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil {
		t.Fatal(err)
	}
	entries, _, err := e.svc.ListAudit(e.ctx, a, auditFilter())
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, en := range entries {
		actions = append(actions, en.Action)
		if en.Action == "workflow.version.published" && (en.ActorName == nil || *en.ActorName != "ada") {
			t.Fatalf("publish audit must name the human actor: %+v", en)
		}
	}
	for _, want := range []string{"workflow.created", "workflow.version.published", "workflow.version.created", "workflow.version.updated"} {
		if !slices.Contains(actions, want) {
			t.Fatalf("missing audit action %s in %v", want, actions)
		}
	}
	var outbox int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM outbox_events WHERE topic = 'workflow.version.published' AND tenant_id = $1`, a.TenantID).Scan(&outbox); err != nil || outbox != 5 {
		t.Fatalf("expected 5 publish outbox events (sim v1, sim v2, sim v3, sim v4, poc v1), got %d (%v)", outbox, err)
	}
}
