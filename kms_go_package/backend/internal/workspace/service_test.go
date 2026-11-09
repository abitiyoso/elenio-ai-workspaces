package workspace

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

func TestUploadPermissionUsesCaseContributorGrant(t *testing.T) {
	if canUpload("reader", "contributor") != true {
		t.Fatal("case contributor should be allowed to upload within the granted case")
	}
	if canUpload("reader", "reader") {
		t.Fatal("reader must not be allowed to upload")
	}
	if canUpload("contributor", "reader") {
		t.Fatal("collection contributor must not elevate an explicit case reader grant")
	}
	if canUpload("reader", "contributor") != true {
		t.Fatal("case contributor should be allowed to upload even when collection membership is reader")
	}
	if canUpload("reader", "") {
		t.Fatal("collection reader must not upload outside a case grant")
	}
	if canUpload("contributor", "") != true {
		t.Fatal("collection contributor should be allowed to upload a collection document")
	}
}

func TestCanContributeRecognizesOnlyEditorRoles(t *testing.T) {
	for role, want := range map[string]bool{"owner": true, "steward": true, "contributor": true, "reader": false, "": false} {
		if got := canContribute(role); got != want {
			t.Errorf("canContribute(%q)=%v, want %v", role, got, want)
		}
	}
}

func TestOriginalPDFUsesVersionAndScopeAuthorizationBeforeStorageRead(t *testing.T) {
	tests := []struct {
		name      string
		row       []driver.Value
		wantError bool
		wantGets  int
	}{
		{name: "reader cannot preview draft", row: previewRow("", "reader", "", false, "draft", "unverified", "draft", "success", "indexed"), wantError: true},
		{name: "reader can preview current published evidence", row: previewRow("", "reader", "", true, "approved", "approved", "published", "success", "indexed"), wantGets: 1},
		{name: "reader cannot preview V1 after V2 publish", row: previewRow("", "reader", "", false, "approved", "approved", "published", "success", "indexed"), wantError: true},
		{name: "case reader can preview case customer source", row: previewRow("case-1", "reader", "reader", false, "draft", "customer_source", "draft", "success", "indexed"), wantGets: 1},
		{name: "collection editor cannot preview case content without case grant", row: previewRow("case-1", "contributor", "", false, "draft", "customer_source", "draft", "success", "indexed"), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := sql.OpenDB(previewConnector{row: test.row})
			defer db.Close()
			store := &previewStore{}
			service := NewService(db, store, 1<<20)
			_, err := service.OriginalPDF(context.Background(), auth.Identity{TenantID: "tenant", UserID: "user"}, "document", "version")
			if (err != nil) != test.wantError {
				t.Fatalf("OriginalPDF error=%v, wantError=%v", err, test.wantError)
			}
			if store.gets != test.wantGets {
				t.Fatalf("object-store reads=%d, want %d", store.gets, test.wantGets)
			}
		})
	}
}

func previewRow(caseID, collectionRole, caseRole string, values ...any) []driver.Value {
	return []driver.Value{"object-key", caseID, collectionRole, caseRole, values[0], values[1], values[2], values[3], values[4], values[5]}
}

type previewConnector struct{ row []driver.Value }

func (c previewConnector) Connect(context.Context) (driver.Conn, error) {
	return previewConn{row: c.row}, nil
}
func (c previewConnector) Driver() driver.Driver { return previewDriver{row: c.row} }

type previewDriver struct{ row []driver.Value }

func (d previewDriver) Open(string) (driver.Conn, error) { return previewConn{row: d.row}, nil }

type previewConn struct{ row []driver.Value }

func (previewConn) Prepare(string) (driver.Stmt, error)      { return nil, errors.New("not implemented") }
func (previewConn) Close() error                             { return nil }
func (previewConn) Begin() (driver.Tx, error)                { return nil, errors.New("not implemented") }
func (previewConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func (c previewConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &previewRows{row: c.row}, nil
}

type previewRows struct {
	row  []driver.Value
	done bool
}

func (*previewRows) Columns() []string {
	return []string{"object_key", "case_id", "collection_role", "case_role", "current_published", "review_status", "trust_status", "publication_status", "parser_status", "index_status"}
}
func (*previewRows) Close() error { return nil }
func (r *previewRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.row)
	r.done = true
	return nil
}

type previewStore struct{ gets int }

func (*previewStore) Ping(context.Context) error                        { return nil }
func (*previewStore) Put(context.Context, string, string, []byte) error { return nil }
func (s *previewStore) Get(context.Context, string) ([]byte, error) {
	s.gets++
	return []byte("%PDF-"), nil
}
func (*previewStore) Delete(context.Context, string) error { return nil }

var _ platform.ObjectStore = (*previewStore)(nil)
