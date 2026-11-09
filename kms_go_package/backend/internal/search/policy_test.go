package search

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

func TestReaderAndContributorEvidenceEligibility(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		caseRole   string
		caseScoped bool
		lifecycle  string
		review     string
		trust      string
		parser     string
		index      string
		published  bool
		want       bool
	}{
		{name: "reader cannot discover draft unverified source", role: "reader", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", want: false},
		{name: "contributor can discover unverified source", role: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", want: true},
		{name: "steward can discover unverified source", role: "steward", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", want: true},
		{name: "reader can discover current published approved evidence", role: "reader", lifecycle: "published", review: "approved", trust: "approved", parser: "success", index: "indexed", published: true, want: true},
		{name: "reader cannot discover V1 after V2 becomes published", role: "reader", lifecycle: "published", review: "approved", trust: "approved", parser: "success", index: "indexed", published: false, want: false},
		{name: "reader cannot discover unapproved trust source", role: "reader", lifecycle: "published", review: "approved", trust: "unverified", parser: "success", index: "indexed", want: false},
		{name: "case contributor can discover scoped draft", role: "reader", caseRole: "contributor", caseScoped: true, lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", want: true},
		{name: "collection contributor with case reader cannot discover case draft", role: "contributor", caseRole: "reader", caseScoped: true, lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", want: false},
		{name: "case reader can discover current approved published evidence", role: "contributor", caseRole: "reader", caseScoped: true, lifecycle: "published", review: "approved", trust: "approved", parser: "success", index: "indexed", published: true, want: true},
		{name: "case reader can discover customer source in case", role: "contributor", caseRole: "reader", caseScoped: true, lifecycle: "ready_for_review", review: "draft", trust: "customer_source", parser: "success", index: "indexed", want: true},
		{name: "customer source is not discoverable outside case scope", role: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "customer_source", parser: "success", index: "indexed", want: false},
		{name: "failed parser output is never discoverable", role: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "partial", index: "indexed", want: false},
		{name: "stale unindexed chunk is never discoverable", role: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "pending", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := authorizedChunk(test.caseScoped, test.role, test.caseRole, test.lifecycle, test.review, test.trust, test.parser, test.index, test.published)
			if got != test.want {
				t.Fatalf("authorizedChunk()=%v, want %v", got, test.want)
			}
		})
	}
}

func TestSplitCollectionsKeepsReaderAndEditorPoliciesSeparate(t *testing.T) {
	groups := splitCollections(map[string]searchPolicy{"reader-collection": approvedEvidencePolicy, "case-reader-collection": caseCustomerSourcePolicy, "editor-collection": editorDiscoveryPolicy})
	if len(groups[approvedEvidencePolicy]) != 1 || groups[approvedEvidencePolicy][0] != "reader-collection" || len(groups[caseCustomerSourcePolicy]) != 1 || groups[caseCustomerSourcePolicy][0] != "case-reader-collection" || len(groups[editorDiscoveryPolicy]) != 1 || groups[editorDiscoveryPolicy][0] != "editor-collection" {
		t.Fatalf("unexpected collection policy groups: %#v", groups)
	}
}

func TestCaseScopePolicyIgnoresBroaderCollectionEditorRole(t *testing.T) {
	if got := policyForRoles(true, "contributor", "reader"); got != caseCustomerSourcePolicy {
		t.Fatalf("case policy=%v, want case customer-source reader policy", got)
	}
	if got := policyForRoles(true, "reader", "contributor"); got != editorDiscoveryPolicy {
		t.Fatalf("case contributor policy=%v, want editor discovery", got)
	}
	if got := policyForRoles(false, "contributor", ""); got != editorDiscoveryPolicy {
		t.Fatalf("collection contributor policy=%v, want editor discovery", got)
	}
}

func TestAuthorizeHitsRechecksCurrentPostgresEvidenceAndMembership(t *testing.T) {
	tests := []struct {
		name           string
		collectionRole string
		caseRole       string
		lifecycle      string
		review         string
		trust          string
		parser         string
		index          string
		published      bool
		caseID         string
		want           int
	}{
		{name: "reader rejected stale unapproved search hit", collectionRole: "reader", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", want: 0},
		{name: "contributor can discover current unverified hit", collectionRole: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", want: 1},
		{name: "reader sees current approved published hit", collectionRole: "reader", lifecycle: "published", review: "approved", trust: "approved", parser: "success", index: "indexed", published: true, want: 1},
		{name: "reader V1 hidden after V2 publication", collectionRole: "reader", lifecycle: "published", review: "approved", trust: "approved", parser: "success", index: "indexed", published: false, want: 0},
		{name: "case reader with collection contributor rejected from case draft", collectionRole: "contributor", caseRole: "reader", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", caseID: "00000000-0000-4000-8000-000000000099", want: 0},
		{name: "case reader with collection contributor sees current approved evidence", collectionRole: "contributor", caseRole: "reader", lifecycle: "published", review: "approved", trust: "approved", parser: "success", index: "indexed", published: true, caseID: "00000000-0000-4000-8000-000000000099", want: 1},
		{name: "case reader searches customer source in case", collectionRole: "contributor", caseRole: "reader", lifecycle: "ready_for_review", review: "draft", trust: "customer_source", parser: "success", index: "indexed", caseID: "00000000-0000-4000-8000-000000000099", want: 1},
		{name: "stale partial parser hit rejected", collectionRole: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "partial", index: "indexed", want: 0},
		{name: "stale non-indexed hit rejected", collectionRole: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "pending", want: 0},
		{name: "case contributor can discover scoped hit", collectionRole: "reader", caseRole: "contributor", lifecycle: "ready_for_review", review: "draft", trust: "unverified", parser: "success", index: "indexed", caseID: "00000000-0000-4000-8000-000000000099", want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := []driver.Value{
				"chunk", "doc", "version", "Title", "authorized snippet", []byte("[]"), nil, nil, "", test.caseID,
				test.lifecycle, test.review, test.trust, test.parser, test.index, test.collectionRole, test.caseRole, test.published,
			}
			db := sql.OpenDB(searchConnector{row: row})
			defer db.Close()
			service := NewService(db, nil, nil)
			result, err := service.authorizeHits(context.Background(), auth.Identity{TenantID: "tenant", UserID: "user"}, test.caseID, []rankedHit{{hit: platform.SearchHit{ChunkID: "chunk"}, score: 0.5, reason: "keyword"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != test.want {
				t.Fatalf("authorized results=%d, want %d", len(result), test.want)
			}
			if len(result) > 0 && (result[0].TrustStatus != test.trust || result[0].PublicationStatus != test.lifecycle) {
				t.Fatalf("returned trust/publication labels = %q/%q", result[0].TrustStatus, result[0].PublicationStatus)
			}
		})
	}
}

type searchConnector struct{ row []driver.Value }

func (c searchConnector) Connect(context.Context) (driver.Conn, error) {
	return searchConn{row: c.row}, nil
}
func (c searchConnector) Driver() driver.Driver { return searchDriver{row: c.row} }

type searchDriver struct{ row []driver.Value }

func (d searchDriver) Open(string) (driver.Conn, error) { return searchConn{row: d.row}, nil }

type searchConn struct{ row []driver.Value }

func (searchConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (searchConn) Close() error                        { return nil }
func (searchConn) Begin() (driver.Tx, error)           { return nil, errors.New("not implemented") }
func (searchConn) CheckNamedValue(value *driver.NamedValue) error {
	if _, ok := value.Value.([]string); ok {
		return nil
	}
	return driver.ErrSkip
}
func (c searchConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &searchRows{row: c.row}, nil
}

type searchRows struct {
	row  []driver.Value
	done bool
}

func (r *searchRows) Columns() []string {
	return []string{"chunk_id", "document_id", "version_id", "title", "content", "heading_path", "page_start", "page_end", "source_anchor", "case_id", "lifecycle_status", "review_status", "trust_status", "parser_status", "index_status", "collection_role", "case_role", "current_published"}
}
func (*searchRows) Close() error { return nil }
func (r *searchRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.row)
	r.done = true
	return nil
}
