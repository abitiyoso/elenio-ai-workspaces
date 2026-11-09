package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitSQLStatementsHonorsQuotesAndComments(t *testing.T) {
	source := `-- comment ;
CREATE TABLE sample (value TEXT DEFAULT 'one;two');
/* block; comment */
INSERT INTO sample VALUES ($tag$three;four$tag$);`
	statements := splitSQLStatements(source)
	if len(statements) != 2 {
		t.Fatalf("statements=%q", statements)
	}
	if !strings.Contains(statements[0], "one;two") || !strings.Contains(statements[1], "three;four") {
		t.Fatalf("quoted content was changed: %q", statements)
	}
}

func TestGovernanceMigrationBaselinesExistingVersionsWithoutDroppingData(t *testing.T) {
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000003_governance.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(up))
	for _, required := range []string{"update document_versions", "update documents", "current_version_id", "published_version_id", "outbox_events", "audit_events_append_only"} {
		if !strings.Contains(source, required) {
			t.Errorf("governance migration is missing baseline or durability clause %q", required)
		}
	}
	for _, forbidden := range []string{"drop table", "truncate ", "delete from"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("up migration contains data-destructive clause %q", forbidden)
		}
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000003_governance.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(down)), "refusing to remove phase 3 governance data") {
		t.Fatal("down migration must refuse rollback while Phase 3 data exists")
	}
}

func TestTrustedPrincipalsMigrationConstrainsRolesAndStatus(t *testing.T) {
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000004_trusted_principals.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(up))
	for _, required := range []string{"create table tenant_principals", "references tenants(id)", "primary key (tenant_id, principal_id)", "status in ('active','disabled')", "roles <@ array['platform_admin','knowledge_steward']::text[]"} {
		if !strings.Contains(source, required) {
			t.Errorf("trusted principals migration is missing %q", required)
		}
	}
	if statements := splitSQLStatements(string(up)); len(statements) != 1 {
		t.Fatalf("expected one statement, got %d", len(statements))
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000004_trusted_principals.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(down)), "refusing to remove provisioned tenant principals") {
		t.Fatal("down migration must refuse rollback while principals are provisioned")
	}
}
