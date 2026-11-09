package transport

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

func newUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type failingDirectory struct{}

func (failingDirectory) LookupPrincipal(context.Context, string, string) (auth.PrincipalRecord, error) {
	return auth.PrincipalRecord{}, errors.New("directory connection refused")
}

// delegationFixture is one tenant with two collections (A, B), one published chunk in each, and principals:
// memberA (active, member of A only), memberB (active, member of B only), disabled (member of A, disabled),
// plus foreign (active in a second tenant, member of nothing here).
type delegationFixture struct {
	db                                            *sql.DB
	tenant, otherTenant, collectionA, collectionB string
	chunkA, chunkB                                string
	memberA, memberB, disabled, foreign           string
	delegateToken, delegateClient                 string
	queryOnlyToken, queryOnlyClient               string
}

func seedDelegationFixture(t *testing.T, db *sql.DB) delegationFixture {
	t.Helper()
	ctx := context.Background()
	f := delegationFixture{db: db, tenant: newUUID(t), otherTenant: newUUID(t), collectionA: newUUID(t), collectionB: newUUID(t),
		memberA: newUUID(t), memberB: newUUID(t), disabled: newUUID(t), foreign: newUUID(t)}
	f.chunkA, f.chunkB = "chunk-a-"+f.collectionA, "chunk-b-"+f.collectionB
	admin := newUUID(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", strings.Fields(query)[0:3], err)
		}
	}
	exec(`INSERT INTO tenants(id,name) VALUES($1,'delegation test'),($2,'delegation other tenant')`, f.tenant, f.otherTenant)
	t.Cleanup(func() { cleanupDelegationFixture(db, f) })
	for _, c := range []struct{ id, chunk, name string }{{f.collectionA, f.chunkA, "A"}, {f.collectionB, f.chunkB, "B"}} {
		doc, version := newUUID(t), newUUID(t)
		exec(`INSERT INTO collections(id,tenant_id,name) VALUES($1,$2,$3)`, c.id, f.tenant, "delegation collection "+c.name)
		exec(`INSERT INTO documents(id,tenant_id,collection_id,title,lifecycle_status,created_by) VALUES($1,$2,$3,$4,'published',$5)`, doc, f.tenant, c.id, "Policy "+c.name, admin)
		exec(`INSERT INTO document_versions(id,tenant_id,document_id,version_number,checksum_sha256,original_object_key,media_type,parser_status,index_status,review_status,trust_status,publication_status,created_by)
			VALUES($1,$2,$3,1,repeat('a',64),'k','application/pdf','success','indexed','approved','approved','published',$4)`, version, f.tenant, doc, admin)
		exec(`UPDATE documents SET current_version_id=$1,published_version_id=$1 WHERE id=$2`, version, doc)
		exec(`INSERT INTO document_chunks(chunk_id,tenant_id,collection_id,document_id,version_id,chunk_number,content,indexed_at) VALUES($1,$2,$3,$4,$5,0,$6,now())`, c.chunk, f.tenant, c.id, doc, version, "refund policy "+c.name)
	}
	exec(`INSERT INTO collection_memberships(tenant_id,collection_id,principal_id,membership_role,granted_by) VALUES($1,$2,$3,'reader',$6),($1,$4,$5,'reader',$6),($1,$2,$7,'reader',$6)`,
		f.tenant, f.collectionA, f.memberA, f.collectionB, f.memberB, admin, f.disabled)
	exec(`INSERT INTO tenant_principals(tenant_id,principal_id,roles) VALUES($1,$2,ARRAY['knowledge_steward']),($1,$3,'{}')`, f.tenant, f.memberA, f.memberB)
	exec(`INSERT INTO tenant_principals(tenant_id,principal_id,status) VALUES($1,$2,'disabled')`, f.tenant, f.disabled)
	exec(`INSERT INTO tenant_principals(tenant_id,principal_id) VALUES($1,$2)`, f.otherTenant, f.foreign)
	grants := []auth.ResourceGrant{{CollectionID: f.collectionA}, {CollectionID: f.collectionB}}
	var err error
	f.delegateToken, f.delegateClient, err = auth.ProvisionServiceClient(ctx, db, f.tenant, admin, "agent-studio-delegate", time.Now().Add(time.Hour), grants, auth.ServiceClientScopes(true))
	if err != nil {
		t.Fatalf("provision delegate client: %v", err)
	}
	f.queryOnlyToken, f.queryOnlyClient, err = auth.ProvisionServiceClient(ctx, db, f.tenant, admin, "agent-studio-query", time.Now().Add(time.Hour), grants, auth.ServiceClientScopes(false))
	if err != nil {
		t.Fatalf("provision query-only client: %v", err)
	}
	if _, _, err := auth.ProvisionServiceClient(ctx, db, f.tenant, admin, "bad", time.Now().Add(time.Hour), grants, []string{"retrieval:admin"}); err == nil {
		t.Fatal("unknown scope was provisioned")
	}
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT array_to_string(scopes,',') FROM service_clients WHERE id=$1`, f.delegateClient).Scan(&stored); err != nil || stored != "retrieval:query,retrieval:delegate" {
		t.Fatalf("stored scopes=%q err=%v", stored, err)
	}
	return f
}

// cleanupDelegationFixture removes what it can; audit_events is append-only by design, so its rows stay.
func cleanupDelegationFixture(db *sql.DB, f delegationFixture) {
	ctx := context.Background()
	for _, query := range []string{
		`DELETE FROM service_client_grants WHERE tenant_id=$1`,
		`DELETE FROM service_clients WHERE tenant_id=$1`,
		`DELETE FROM document_chunks WHERE tenant_id=$1`,
		`UPDATE documents SET current_version_id=NULL,published_version_id=NULL WHERE tenant_id=$1`,
		`DELETE FROM document_versions WHERE tenant_id=$1`,
		`DELETE FROM documents WHERE tenant_id=$1`,
		`DELETE FROM collection_memberships WHERE tenant_id=$1`,
		`DELETE FROM collections WHERE tenant_id=$1`,
		`DELETE FROM tenant_principals WHERE tenant_id=$1 OR tenant_id=$2`,
		`DELETE FROM tenants WHERE id=$1 OR id=$2`,
	} {
		args := []any{f.tenant}
		if strings.Contains(query, "$2") {
			args = append(args, f.otherTenant)
		}
		_, _ = db.ExecContext(ctx, query, args...)
	}
}

// fakeElasticsearch ignores filters and returns both chunks, so only the PostgreSQL recheck
// (service grant + delegated principal's membership) decides what reaches the response.
func fakeElasticsearch(t *testing.T, chunks ...string) *platform.SearchClient {
	t.Helper()
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			_, _ = io.WriteString(w, `{"acknowledged":true}`)
			return
		}
		hits := make([]map[string]any, 0, len(chunks))
		for i, chunk := range chunks {
			hits = append(hits, map[string]any{"_id": chunk, "_score": float64(len(chunks) - i), "_source": map[string]any{"chunk_id": chunk}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": map[string]any{"hits": hits}})
	}))
	t.Cleanup(es.Close)
	client, err := platform.NewSearchClient(es.URL, "kms_chunks_test", 0)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type lastAudit struct {
	Outcome   string
	UserID    string
	ErrorCode string
	Reason    string
	Delegated bool
	Results   int
}

func latestRetrievalAudit(t *testing.T, db *sql.DB, tenantID, clientID string) lastAudit {
	t.Helper()
	var a lastAudit
	var userID, code, reason sql.NullString
	var delegated sql.NullBool
	err := db.QueryRowContext(context.Background(), `SELECT outcome,details->>'user_id',details->>'error_code',details->>'reason',(details->>'delegated')::boolean,(details->>'result_count')::int
		FROM audit_events WHERE tenant_id=$1 AND actor_type='service' AND actor_id=$2 AND action='retrieval.query' ORDER BY created_at DESC, id DESC LIMIT 1`, tenantID, clientID).
		Scan(&a.Outcome, &userID, &code, &reason, &delegated, &a.Results)
	if err != nil {
		t.Fatalf("read retrieval audit: %v", err)
	}
	a.UserID, a.ErrorCode, a.Reason, a.Delegated = userID.String, code.String, reason.String, delegated.Bool
	return a
}

// TestDelegatedRetrievalAgainstPostgres needs a migrated, disposable KMS database (it inserts two tenants and
// leaves append-only audit rows behind). Run with: KMS_TEST_DATABASE_URL=postgres://... go test ./internal/transport/
func TestDelegatedRetrievalAgainstPostgres(t *testing.T) {
	url := os.Getenv("KMS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KMS_TEST_DATABASE_URL is not set; point it at a migrated, disposable KMS database")
	}
	database, err := platform.OpenDatabase(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	f := seedDelegationFixture(t, db)
	search := fakeElasticsearch(t, f.chunkA, f.chunkB)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The human adapter behaves like control_plane_jwt facing a kms_svc_ bearer: it rejects the request. The
	// delegated path must not depend on it.
	rejectingHuman := auth.NewTrustedSessionAdapter(auth.TrustedSessionVerifierFunc(func(context.Context, *http.Request) (auth.Identity, error) {
		return auth.Identity{}, &auth.TokenRejection{Reason: "token_not_compact_jws"}
	}))
	server := NewServer(Dependencies{Logger: logger, Database: database, Search: search, Auth: rejectingHuman})

	type response struct {
		Results []struct {
			ChunkID string `json:"chunk_id"`
		} `json:"results"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	call := func(t *testing.T, handler http.Handler, token string, body string, delegated ...string) (int, response) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieval/query", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		for _, value := range delegated {
			req.Header.Add(auth.DelegatedUserHeader, value)
		}
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		var out response
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
		}
		return resp.Code, out
	}
	onlyA := fmt.Sprintf(`{"query":"refund","collection_ids":[%q]}`, f.collectionA)

	t.Run("delegated principal gets only its own membership", func(t *testing.T) {
		status, out := call(t, server, f.delegateToken, onlyA, strings.ToUpper(f.memberA))
		if status != http.StatusOK || len(out.Results) != 1 || out.Results[0].ChunkID != f.chunkA {
			t.Fatalf("status=%d results=%+v error=%s", status, out.Results, out.Error.Code)
		}
		a := latestRetrievalAudit(t, db, f.tenant, f.delegateClient)
		if a.Outcome != "allowed" || a.UserID != f.memberA || !a.Delegated || a.Results != 1 || a.ErrorCode != "" {
			t.Fatalf("audit=%+v", a)
		}
	})
	t.Run("delegated principal without membership is forbidden", func(t *testing.T) {
		status, out := call(t, server, f.delegateToken, onlyA, f.memberB)
		if status != http.StatusForbidden || out.Error.Code != "forbidden" {
			t.Fatalf("status=%d error=%s", status, out.Error.Code)
		}
		if a := latestRetrievalAudit(t, db, f.tenant, f.delegateClient); a.Outcome != "denied" || a.UserID != f.memberB || !a.Delegated {
			t.Fatalf("audit=%+v", a)
		}
	})
	t.Run("all granted collections require membership in all of them", func(t *testing.T) {
		status, _ := call(t, server, f.delegateToken, `{"query":"refund"}`, f.memberA)
		if status != http.StatusForbidden {
			t.Fatalf("status=%d", status)
		}
	})

	rejections := map[string]struct {
		token, client string
		values        []string
		reason        string
	}{
		"header without delegate scope": {f.queryOnlyToken, f.queryOnlyClient, []string{f.memberA}, "delegation_not_permitted"},
		"unknown principal":             {f.delegateToken, f.delegateClient, []string{newUUID(t)}, "principal_not_provisioned"},
		"disabled principal":            {f.delegateToken, f.delegateClient, []string{f.disabled}, "principal_disabled"},
		"principal of another tenant":   {f.delegateToken, f.delegateClient, []string{f.foreign}, "principal_not_provisioned"},
		"malformed uuid":                {f.delegateToken, f.delegateClient, []string{"not-a-uuid"}, "delegated_user_not_uuid"},
		"two header values":             {f.delegateToken, f.delegateClient, []string{f.memberA, f.memberA}, "delegated_user_repeated"},
	}
	for name, tc := range rejections {
		t.Run(name, func(t *testing.T) {
			status, out := call(t, server, tc.token, onlyA, tc.values...)
			if status != http.StatusUnauthorized || out.Error.Code != "delegated_user_required" || len(out.Results) != 0 {
				t.Fatalf("status=%d error=%s results=%d", status, out.Error.Code, len(out.Results))
			}
			a := latestRetrievalAudit(t, db, f.tenant, tc.client)
			if a.Outcome != "denied" || a.ErrorCode != "delegated_user_required" || a.Reason != tc.reason || !a.Delegated || a.UserID != "" {
				t.Fatalf("audit=%+v, want reason %s", a, tc.reason)
			}
		})
	}

	t.Run("directory outage is 503", func(t *testing.T) {
		outage := NewServer(Dependencies{Logger: logger, Database: database, Search: search, Auth: rejectingHuman, Principals: failingDirectory{}})
		status, out := call(t, outage, f.delegateToken, onlyA, f.memberA)
		if status != http.StatusServiceUnavailable || out.Error.Code != "delegated_user_unavailable" {
			t.Fatalf("status=%d error=%s", status, out.Error.Code)
		}
		if a := latestRetrievalAudit(t, db, f.tenant, f.delegateClient); a.Outcome != "failed" || a.ErrorCode != "delegated_user_unavailable" || !a.Delegated {
			t.Fatalf("audit=%+v", a)
		}
	})

	t.Run("no header keeps the human session path", func(t *testing.T) {
		status, out := call(t, server, f.delegateToken, onlyA)
		if status != http.StatusUnauthorized || out.Error.Code != "delegated_user_required" {
			t.Fatalf("status=%d error=%s", status, out.Error.Code)
		}
		if a := latestRetrievalAudit(t, db, f.tenant, f.delegateClient); a.Outcome != "denied" || a.Delegated || a.Reason != "" {
			t.Fatalf("audit=%+v", a)
		}
		// Mock mode: the header-less request uses the adapter's identity exactly as before.
		mock := NewServer(Dependencies{Logger: logger, Database: database, Search: search, Auth: auth.NewDevelopmentMock(f.memberA, f.tenant)})
		status, out = call(t, mock, f.queryOnlyToken, onlyA)
		if status != http.StatusOK || len(out.Results) != 1 || out.Results[0].ChunkID != f.chunkA {
			t.Fatalf("mock status=%d results=%+v error=%s", status, out.Results, out.Error.Code)
		}
		if a := latestRetrievalAudit(t, db, f.tenant, f.queryOnlyClient); a.Outcome != "allowed" || a.Delegated || a.UserID != f.memberA {
			t.Fatalf("audit=%+v", a)
		}
		// Mock mode still honours the header for a delegating client, and the header wins over the mock identity.
		status, out = call(t, mock, f.delegateToken, fmt.Sprintf(`{"query":"refund","collection_ids":[%q]}`, f.collectionB), f.memberB)
		if status != http.StatusOK || len(out.Results) != 1 || out.Results[0].ChunkID != f.chunkB {
			t.Fatalf("mock delegated status=%d results=%+v error=%s", status, out.Results, out.Error.Code)
		}
	})
}
