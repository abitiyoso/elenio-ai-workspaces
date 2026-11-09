package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func randomUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// TestSQLPrincipalDirectoryAgainstPostgres needs a migrated, disposable KMS database (it inserts and then
// deletes one tenant and its principals). Run with: KMS_TEST_DATABASE_URL=postgres://... go test ./internal/auth/
func TestSQLPrincipalDirectoryAgainstPostgres(t *testing.T) {
	url := os.Getenv("KMS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KMS_TEST_DATABASE_URL is not set; point it at a migrated, disposable KMS database")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tenantID, activeID, disabledID := randomUUID(t), randomUUID(t), randomUUID(t)
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,name) VALUES($1,'verifier test tenant')`, tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM tenant_principals WHERE tenant_id=$1`, tenantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM tenants WHERE id=$1`, tenantID)
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_principals(tenant_id,principal_id,display_name,roles) VALUES($1,$2,'Active Steward',ARRAY['knowledge_steward'])`, tenantID, activeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_principals(tenant_id,principal_id,status,roles) VALUES($1,$2,'disabled',ARRAY['platform_admin'])`, tenantID, disabledID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_principals(tenant_id,principal_id,roles) VALUES($1,$2,ARRAY['workspace_admin'])`, tenantID, randomUUID(t)); err == nil {
		t.Fatal("roles CHECK constraint accepted an unknown role")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_principals(tenant_id,principal_id) VALUES($1,$2)`, randomUUID(t), randomUUID(t)); err == nil {
		t.Fatal("tenant foreign key accepted an unknown tenant")
	}

	directory := NewSQLPrincipalDirectory(db)
	record, err := directory.LookupPrincipal(ctx, tenantID, activeID)
	if err != nil || record.Status != "active" || !slices.Equal(record.Roles, []string{"knowledge_steward"}) {
		t.Fatalf("record = %+v, err = %v", record, err)
	}
	if _, err := directory.LookupPrincipal(ctx, tenantID, randomUUID(t)); err != ErrPrincipalNotProvisioned {
		t.Fatalf("missing principal err = %v", err)
	}

	// End to end: token verification + SQL directory.
	h := newHarness(t)
	verifier, err := NewControlPlaneVerifier(ControlPlaneConfig{
		Issuer: testIssuer, Audience: "kms", JWKSURL: h.jwks.URL + "/.well-known/jwks.json",
		TenantMap: map[string]string{"acme-demo": tenantID}, MaxTokenTTL: h.verifier.maxTTL, ClockSkew: h.verifier.skew,
		JWKSCacheTTL: h.verifier.keys.ttl, Directory: directory, Now: h.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	claims := h.claims()
	claims["sub"] = activeID
	claims["roles"] = []string{"platform_admin"}
	identity, err := verifier.Verify(ctx, bearerRequest(h.sign(claims)))
	if err != nil || identity.UserID != activeID || identity.TenantID != tenantID || !slices.Equal(identity.Roles, []string{"knowledge_steward"}) {
		t.Fatalf("identity = %+v, err = %v", identity, err)
	}
	claims["sub"] = disabledID
	_, err = verifier.Verify(ctx, bearerRequest(h.sign(claims)))
	requireRejected(t, err, "principal_disabled")
	claims["sub"] = randomUUID(t)
	_, err = verifier.Verify(ctx, bearerRequest(h.sign(claims)))
	requireRejected(t, err, "principal_not_provisioned")
}
