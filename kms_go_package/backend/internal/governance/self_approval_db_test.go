package governance

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

func testUUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// TestSelfApprovalExemptionAgainstPostgres needs a migrated, disposable KMS database (append-only audit rows stay).
// Run with: KMS_TEST_DATABASE_URL=postgres://... go test ./internal/governance/
func TestSelfApprovalExemptionAgainstPostgres(t *testing.T) {
	url := os.Getenv("KMS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KMS_TEST_DATABASE_URL is not set; point it at a migrated, disposable KMS database")
	}
	ctx := context.Background()
	database, err := platform.OpenDatabase(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	tenant, collection, exempt, regular := testUUID(t), testUUID(t), testUUID(t), testUUID(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec(`INSERT INTO tenants(id,name) VALUES($1,'self approval test')`, tenant)
	exec(`INSERT INTO collections(id,tenant_id,name) VALUES($1,$2,'self approval')`, collection, tenant)
	exec(`INSERT INTO collection_memberships(tenant_id,collection_id,principal_id,membership_role,granted_by) VALUES($1,$2,$3,'steward',$3),($1,$2,$4,'steward',$3)`,
		tenant, collection, exempt, regular)
	draft := func(author string) (string, string) {
		t.Helper()
		doc, version := testUUID(t), testUUID(t)
		exec(`INSERT INTO documents(id,tenant_id,collection_id,title,lifecycle_status,created_by) VALUES($1,$2,$3,'Doc','draft',$4)`, doc, tenant, collection, author)
		exec(`INSERT INTO document_versions(id,tenant_id,document_id,version_number,checksum_sha256,original_object_key,media_type,parser_status,index_status,review_status,trust_status,publication_status,created_by)
			VALUES($1,$2,$3,1,repeat('a',64),'k','application/pdf','success','indexed','draft','unverified','draft',$4)`, version, tenant, doc, author)
		exec(`UPDATE documents SET current_version_id=$1 WHERE id=$2`, version, doc)
		return doc, version
	}
	s := NewService(db).WithSelfApprovalExempt([]string{exempt})
	queued := func(id auth.Identity, reviewID string) bool {
		reviews, err := s.ListReviews(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range reviews {
			if r.ID == reviewID {
				return true
			}
		}
		return false
	}

	t.Run("exempt principal reviews its own submission", func(t *testing.T) {
		id := auth.Identity{UserID: exempt, TenantID: tenant}
		doc, version := draft(exempt)
		review, err := s.SubmitReview(ctx, id, doc, version, "", "corr-exempt")
		if err != nil {
			t.Fatal(err)
		}
		if !queued(id, review.ID) {
			t.Fatal("own submission must appear in the exempt reviewer's queue")
		}
		if err := s.DecideReview(ctx, id, review.ID, "approved", "walkthrough", "corr-exempt"); err != nil {
			t.Fatalf("exempt self-approval refused: %v", err)
		}
		var flagged bool
		if err := db.QueryRowContext(ctx, `SELECT self_approval_exempt FROM review_decisions WHERE review_id=$1`, review.ID).Scan(&flagged); err != nil || !flagged {
			t.Fatalf("decision row not flagged: %v %v", flagged, err)
		}
		var marker sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT details->>'self_approval_exempt' FROM audit_events WHERE tenant_id=$1 AND action='review.approved' AND outcome='allowed' AND actor_id=$2`, tenant, exempt).Scan(&marker); err != nil || marker.String != "true" {
			t.Fatalf("audit marker = %q, %v", marker.String, err)
		}
	})

	t.Run("everyone else is still refused", func(t *testing.T) {
		id := auth.Identity{UserID: regular, TenantID: tenant}
		doc, version := draft(regular)
		review, err := s.SubmitReview(ctx, id, doc, version, "", "corr-regular")
		if err != nil {
			t.Fatal(err)
		}
		if queued(id, review.ID) {
			t.Fatal("own submission must stay out of a regular reviewer's queue")
		}
		if err := s.DecideReview(ctx, id, review.ID, "approved", "self", "corr-regular"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("regular self-approval must be forbidden, got %v", err)
		}
		// The exempt principal reviewing someone else's work is an ordinary independent review.
		if err := s.DecideReview(ctx, auth.Identity{UserID: exempt, TenantID: tenant}, review.ID, "approved", "independent", "corr-regular"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("database refuses an unflagged self-review", func(t *testing.T) {
		doc, version := draft(regular)
		review := testUUID(t)
		exec(`INSERT INTO document_reviews(id,tenant_id,document_id,version_id,submitted_by) VALUES($1,$2,$3,$4,$5)`, review, tenant, doc, version, regular)
		_, err := db.ExecContext(ctx, `INSERT INTO review_decisions(tenant_id,review_id,document_id,version_id,submitter_id,reviewer_id,decision,rationale) VALUES($1,$2,$3,$4,$5,$5,'approved','x')`,
			tenant, review, doc, version, regular)
		if err == nil {
			t.Fatal("reviewer = submitter without the exemption flag must violate the check constraint")
		}
	})
}
