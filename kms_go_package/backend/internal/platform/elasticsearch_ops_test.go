package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func respond(r *http.Request, status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func testChunk(id string) IndexedChunk {
	return IndexedChunk{ChunkID: id, TenantID: "tenant", DocumentID: "doc", VersionID: "version", CollectionID: "collection", Content: "text", ACLPrincipals: []string{"user"}}
}

func TestBulkIndexUpsertsByChunkIDSoRetriesDoNotDuplicate(t *testing.T) {
	client, _ := NewSearchClient("http://elasticsearch.test", "kms_chunks_v2", 0)
	var bulkBody string
	client.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPut {
			return respond(r, http.StatusOK, `{"acknowledged":true}`)
		}
		if r.URL.Path != "/_bulk" || r.Header.Get("Content-Type") != "application/x-ndjson" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		bulkBody = string(data)
		return respond(r, http.StatusOK, `{"errors":false,"items":[]}`)
	})}
	if err := client.BulkIndexChunks(context.Background(), []IndexedChunk{testChunk("c-1"), testChunk("c-2")}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(bulkBody), "\n")
	if len(lines) != 4 || !strings.HasSuffix(bulkBody, "\n") {
		t.Fatalf("bulk body must be newline-terminated action/document pairs: %q", bulkBody)
	}
	for i, id := range []string{"c-1", "c-2"} {
		var action map[string]map[string]string
		if err := json.Unmarshal([]byte(lines[i*2]), &action); err != nil {
			t.Fatal(err)
		}
		if action["index"]["_id"] != id || action["index"]["_index"] != "kms_chunks_v2" {
			t.Fatalf("action %d=%v; want deterministic index upsert of %s", i, action, id)
		}
	}
}

func TestBulkIndexSurfacesItemFailures(t *testing.T) {
	client, _ := NewSearchClient("http://elasticsearch.test", "kms_chunks_v1", 0)
	client.ensured.Store(true)
	client.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, `{"errors":true,"items":[{"index":{"_id":"c-1","status":201}},{"index":{"_id":"c-2","status":400,"error":{"type":"mapper_parsing_exception"}}}]}`)
	})}
	err := client.BulkIndexChunks(context.Background(), []IndexedChunk{testChunk("c-1"), testChunk("c-2")})
	if err == nil || !strings.Contains(err.Error(), "c-2") || !strings.Contains(err.Error(), "mapper_parsing_exception") {
		t.Fatalf("err=%v; partial bulk failure must fail the batch", err)
	}
}

func TestUpdateScopePrincipalsTargetsOnlyThatScope(t *testing.T) {
	for _, tc := range []struct{ caseID, want, reject string }{
		{"", `"must_not":{"exists":{"field":"case_id"}}`, `"case_id":"`},
		{"case-1", `"case_id":"case-1"`, `must_not`},
	} {
		client, _ := NewSearchClient("http://elasticsearch.test", "kms_chunks_v1", 0)
		client.ensured.Store(true)
		var body string
		client.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/kms_chunks_v1/_update_by_query" {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			data, _ := io.ReadAll(r.Body)
			body = string(data)
			return respond(r, http.StatusOK, `{"updated":2}`)
		})}
		if err := client.UpdateScopePrincipals(context.Background(), "tenant", "collection", tc.caseID, []string{"user-a", "user-b"}); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"tenant_id":"tenant"`, `"collection_id":"collection"`, tc.want, `"principals":["user-a","user-b"]`} {
			if !strings.Contains(body, want) {
				t.Fatalf("case %q body missing %s: %s", tc.caseID, want, body)
			}
		}
		if strings.Contains(body, tc.reject) {
			t.Fatalf("case %q body leaks into another scope (%s): %s", tc.caseID, tc.reject, body)
		}
	}
}

func TestSwapAliasMovesAliasAtomically(t *testing.T) {
	client, _ := NewSearchClient("http://elasticsearch.test", "kms_chunks_current", 0)
	var actions string
	client.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/_alias/kms_chunks_current":
			return respond(r, http.StatusOK, `{"kms_chunks_v1":{"aliases":{"kms_chunks_current":{}}}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/_aliases":
			data, _ := io.ReadAll(r.Body)
			actions = string(data)
			return respond(r, http.StatusOK, `{"acknowledged":true}`)
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		return nil, nil
	})}
	if err := client.SwapAlias(context.Background(), "kms_chunks_current", "kms_chunks_v2"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(actions, `{"remove":{"alias":"kms_chunks_current","index":"kms_chunks_v1"}}`) || !strings.Contains(actions, `"add":{"alias":"kms_chunks_current","index":"kms_chunks_v2","is_write_index":true}`) {
		t.Fatalf("alias actions must remove old and add new in one request: %s", actions)
	}
}

func TestCreateIndexAcceptsExistingAliasName(t *testing.T) {
	client, _ := NewSearchClient("http://elasticsearch.test", "kms_chunks_current", 0)
	client.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusBadRequest, `{"error":{"type":"invalid_index_name_exception","reason":"Invalid index name [kms_chunks_current], already exists as alias"}}`)
	})}
	if err := client.EnsureIndex(context.Background()); err != nil {
		t.Fatalf("alias-backed index must be treated as existing: %v", err)
	}
}
