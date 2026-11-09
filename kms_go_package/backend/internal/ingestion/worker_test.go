package ingestion

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/chunking"
)

func TestRetryDelayUsesBoundedExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 15 * time.Second},
		{attempt: 1, want: 15 * time.Second},
		{attempt: 2, want: 30 * time.Second},
		{attempt: 3, want: 60 * time.Second},
		{attempt: 4, want: 120 * time.Second},
		{attempt: 5, want: 240 * time.Second},
		{attempt: 20, want: 300 * time.Second},
	}
	for _, test := range tests {
		if got := retryDelay(test.attempt); got != test.want {
			t.Errorf("retryDelay(%d)=%s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestCollectionPrincipalQueryCastsCaseIDToUUID(t *testing.T) {
	if !strings.Contains(collectionPrincipalsQuery, "NULLIF($3,'')::uuid") {
		t.Fatalf("case membership comparison must cast the optional text case ID to UUID: %s", collectionPrincipalsQuery)
	}
	if strings.Contains(collectionPrincipalsQuery, "case_id=$3") {
		t.Fatalf("case membership query compares UUID column to uncast parameter: %s", collectionPrincipalsQuery)
	}
}

func TestHeadingPathForDatabaseInsertIsNeverNil(t *testing.T) {
	path := nonNilHeadingPath(nil)
	if path == nil {
		t.Fatal("nil heading path would be encoded as SQL NULL")
	}
	if len(path) != 0 {
		t.Fatalf("heading path=%v, want empty", path)
	}
}

func TestFailReturnsFailureStatePersistenceError(t *testing.T) {
	db := sql.OpenDB(scriptedConnector{script: &execScript{failAt: 2}})
	defer db.Close()
	worker := &Worker{db: db}
	err := worker.fail(context.Background(), Job{ID: "job", TenantID: "tenant", VersionID: "version", Attempts: 2, MaxAttempts: 5}, errors.New("Docling unavailable"))
	if err == nil {
		t.Fatal("expected version-state persistence failure to be returned")
	}
}

func TestFailPersistsRetryStateWithBackoff(t *testing.T) {
	script := &execScript{}
	db := sql.OpenDB(scriptedConnector{script: script})
	defer db.Close()
	worker := &Worker{db: db}
	err := worker.fail(context.Background(), Job{ID: "job", TenantID: "tenant", VersionID: "version", Attempts: 3, MaxAttempts: 5}, errors.New("Docling unavailable"))
	if err != nil {
		t.Fatal(err)
	}
	if !script.committed {
		t.Fatal("failure-state transaction was not committed")
	}
	if got := script.firstArgs[1].Value; got != int64(60) {
		t.Fatalf("retry delay argument=%v, want 60 seconds", got)
	}
	if got := script.secondArgs[2].Value; got != "pending" {
		t.Fatalf("retry parser status=%v, want pending", got)
	}
}

func TestIndexedChunkWritesStatusesAndCaseTrustForES(t *testing.T) {
	document := indexedChunk(Job{ID: "job", TenantID: "tenant", DocumentID: "doc", VersionID: "version", CollectionID: "collection"}, source{Title: "title", Classification: "internal", TrustStatus: "customer_source", CaseID: "case", Members: []string{"principal"}}, chunking.Chunk{ID: "chunk", Content: "content"}, nil, "success", nil)
	if document.ParserStatus != "success" || document.IndexStatus != "indexed" {
		t.Fatalf("indexed status fields = parser:%q index:%q", document.ParserStatus, document.IndexStatus)
	}
	if document.TrustStatus != "customer_source" || document.CaseID != "case" {
		t.Fatalf("indexed case source fields = trust:%q case:%q", document.TrustStatus, document.CaseID)
	}
}

type execScript struct {
	mu         sync.Mutex
	count      int
	failAt     int
	firstArgs  []driver.NamedValue
	secondArgs []driver.NamedValue
	committed  bool
}

type scriptedConnector struct{ script *execScript }

func (c scriptedConnector) Connect(context.Context) (driver.Conn, error) {
	return scriptedConn{script: c.script}, nil
}
func (c scriptedConnector) Driver() driver.Driver { return scriptedDriver{script: c.script} }

type scriptedDriver struct{ script *execScript }

func (d scriptedDriver) Open(string) (driver.Conn, error) { return scriptedConn{script: d.script}, nil }

type scriptedConn struct{ script *execScript }

func (c scriptedConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c scriptedConn) Close() error                        { return nil }
func (c scriptedConn) Begin() (driver.Tx, error)           { return scriptedTx{script: c.script}, nil }
func (c scriptedConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return scriptedTx{script: c.script}, nil
}
func (c scriptedConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.script.mu.Lock()
	defer c.script.mu.Unlock()
	c.script.count++
	if c.script.count == 1 {
		c.script.firstArgs = append([]driver.NamedValue(nil), args...)
	} else {
		c.script.secondArgs = append([]driver.NamedValue(nil), args...)
	}
	if c.script.failAt == c.script.count {
		return nil, fmt.Errorf("scripted database failure at query %d", c.script.count)
	}
	return driver.RowsAffected(1), nil
}

type scriptedTx struct{ script *execScript }

func (tx scriptedTx) Commit() error {
	tx.script.mu.Lock()
	tx.script.committed = true
	tx.script.mu.Unlock()
	return nil
}
func (scriptedTx) Rollback() error { return nil }
