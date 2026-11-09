package governance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
)

func TestSelfApprovalIsDeniedForSubmitterAndVersionAuthor(t *testing.T) {
	if !selfApprovalDenied("author", "author", "author") {
		t.Fatal("submitter self-approval must be denied")
	}
	if !selfApprovalDenied("author", "other", "author") {
		t.Fatal("version author approval must be denied")
	}
	if selfApprovalDenied("independent-reviewer", "submitter", "author") {
		t.Fatal("independent reviewer must not be treated as self-approval")
	}
}

func TestVersionCapabilitiesKeepCaseAndCollectionRolesScoped(t *testing.T) {
	draft := Version{ID: "v2", CreatedBy: "author", ReviewStatus: "draft", ParserStatus: "success", IndexStatus: "indexed", TrustStatus: "unverified", PublicationStatus: "draft"}
	if !contains(versionCapabilities("contributor", false, true, "author", "", draft, false), "upload_version") {
		t.Fatal("collection contributor should be able to upload a new version")
	}
	if !contains(versionCapabilities("contributor", true, true, "author", "", draft, false), "upload_version") {
		t.Fatal("case contributor should be able to upload a new version")
	}
	if contains(versionCapabilities("reader", true, true, "reader", "", draft, false), "upload_version") {
		t.Fatal("case reader must not inherit collection contributor upload rights")
	}
	if contains(versionCapabilities("reader", false, true, "reader", "", draft, false), "upload_version") {
		t.Fatal("collection reader must not upload versions")
	}
}

func TestCasePrivateCustomerSourceNeverGetsPublishCapability(t *testing.T) {
	version := Version{CreatedBy: "author", ReviewStatus: "approved", ParserStatus: "success", IndexStatus: "indexed", TrustStatus: "approved", PublicationStatus: "ready_for_review"}
	if contains(versionCapabilities("steward", true, true, "steward", "submitter", version, false), "publish") {
		t.Fatal("case scoped content must never be publishable globally")
	}
	if !contains(versionCapabilities("steward", false, true, "steward", "submitter", version, false), "publish") {
		t.Fatal("eligible collection evidence should expose publish capability")
	}
	if contains(versionCapabilities("steward", false, false, "steward", "submitter", version, false), "publish") {
		t.Fatal("a non-current version must not expose publish capability")
	}
}

func TestDeniedReviewAuditInsertFailureIsPropagated(t *testing.T) {
	wantErr := errors.New("audit insert failed")
	script := &reviewDenialScript{execErr: wantErr}
	db := sql.OpenDB(reviewDenialConnector{script: script})
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = recordDeniedReview(context.Background(), tx, auth.Identity{TenantID: "tenant", UserID: "reviewer"}, target{VersionID: "version", CollectionID: "collection"}, "approved", "rationale", "", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want audit failure %v", err, wantErr)
	}
	if script.commits != 0 {
		t.Fatalf("transaction commits=%d, want no commit after audit insert failure", script.commits)
	}
}

func TestDeniedReviewAuditCommitFailureIsPropagated(t *testing.T) {
	wantErr := errors.New("audit transaction commit failed")
	script := &reviewDenialScript{commitErr: wantErr}
	db := sql.OpenDB(reviewDenialConnector{script: script})
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = recordDeniedReview(context.Background(), tx, auth.Identity{TenantID: "tenant", UserID: "reviewer"}, target{VersionID: "version", CollectionID: "collection"}, "approved", "rationale", "", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want commit failure %v", err, wantErr)
	}
	if script.execs != 1 || script.commits != 1 {
		t.Fatalf("audit execs=%d commits=%d, want one each", script.execs, script.commits)
	}
}

func TestDeniedReviewReturnsForbiddenOnlyAfterAuditCommit(t *testing.T) {
	script := &reviewDenialScript{}
	db := sql.OpenDB(reviewDenialConnector{script: script})
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = recordDeniedReview(context.Background(), tx, auth.Identity{TenantID: "tenant", UserID: "reviewer"}, target{VersionID: "version", CollectionID: "collection"}, "approved", "rationale", "", nil)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error=%v, want forbidden after successful audit", err)
	}
	if script.execs != 1 || script.commits != 1 {
		t.Fatalf("audit execs=%d commits=%d, want one each", script.execs, script.commits)
	}
}

func TestReviewMembershipInfrastructureErrorIsNotConvertedToForbidden(t *testing.T) {
	wantErr := errors.New("membership database unavailable")
	allowed, err := reviewMembershipAllowed("", wantErr)
	if allowed || !errors.Is(err, wantErr) {
		t.Fatalf("allowed=%v err=%v, want original infrastructure error", allowed, err)
	}
	allowed, err = reviewMembershipAllowed("", ErrForbidden)
	if allowed || err != nil {
		t.Fatalf("missing scoped membership should be a denial, allowed=%v err=%v", allowed, err)
	}
}

type reviewDenialScript struct {
	execErr, commitErr error
	execs, commits     int
}
type reviewDenialConnector struct{ script *reviewDenialScript }

func (c reviewDenialConnector) Connect(context.Context) (driver.Conn, error) {
	return reviewDenialConn{script: c.script}, nil
}
func (c reviewDenialConnector) Driver() driver.Driver { return reviewDenialDriver{script: c.script} }

type reviewDenialDriver struct{ script *reviewDenialScript }

func (d reviewDenialDriver) Open(string) (driver.Conn, error) {
	return reviewDenialConn{script: d.script}, nil
}

type reviewDenialConn struct{ script *reviewDenialScript }

func (reviewDenialConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (reviewDenialConn) Close() error                { return nil }
func (c reviewDenialConn) Begin() (driver.Tx, error) { return reviewDenialTx{script: c.script}, nil }
func (c reviewDenialConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return reviewDenialTx{script: c.script}, nil
}
func (c reviewDenialConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.script.execs++
	if c.script.execErr != nil {
		return nil, c.script.execErr
	}
	return driver.RowsAffected(1), nil
}

type reviewDenialTx struct{ script *reviewDenialScript }

func (tx reviewDenialTx) Commit() error { tx.script.commits++; return tx.script.commitErr }
func (reviewDenialTx) Rollback() error  { return nil }

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestSelfApprovalExemptionOnlyWidensTheReviewCapability(t *testing.T) {
	pending := Version{CreatedBy: "author", ReviewStatus: "pending", ParserStatus: "success", IndexStatus: "indexed", TrustStatus: "unverified", PublicationStatus: "ready_for_review"}
	if contains(versionCapabilities("steward", false, true, "author", "author", pending, false), "review") {
		t.Fatal("authors must not review their own version")
	}
	if !contains(versionCapabilities("steward", false, true, "author", "author", pending, true), "review") {
		t.Fatal("an exempt author should be offered the review")
	}
	if contains(versionCapabilities("reader", false, true, "author", "author", pending, true), "review") {
		t.Fatal("the exemption must not replace the reviewer role")
	}
	if d := reviewAuditDetails("r1", true); d["self_approval_exempt"] != true {
		t.Fatalf("exempt decision not marked: %v", d)
	}
	if _, marked := reviewAuditDetails("r1", false)["self_approval_exempt"]; marked {
		t.Fatal("ordinary decisions must not carry the exemption marker")
	}
}
