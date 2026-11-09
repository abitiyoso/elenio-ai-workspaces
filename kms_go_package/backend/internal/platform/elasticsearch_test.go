package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestKeywordMappingOmitsVectorUntilDimensionsAreConfigured(t *testing.T) {
	client, err := NewSearchClient("http://localhost:9200", "kms_chunks_v1", 0)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := client.MappingJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mapping, `"embedding"`) {
		t.Fatalf("keyword-only mapping unexpectedly declares a vector: %s", mapping)
	}
}

func TestCreateIndexAppliesConfiguredMapping(t *testing.T) {
	client, err := NewSearchClient("http://localhost:9200", "kms_chunks_v1", 512)
	if err != nil {
		t.Fatal(err)
	}
	client.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut || r.URL.Path != "/kms_chunks_v1" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"dims":512`) || !strings.Contains(string(body), `"dense_vector"`) {
			t.Fatalf("mapping body=%s", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"acknowledged":true}`)), Request: r}, nil
	})}
	if err := client.CreateIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMappingUsesElasticsearchDenseVector(t *testing.T) {
	client, err := NewSearchClient("http://localhost:9200", "kms_chunks_v1", 768)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := client.MappingJSON()
	if err != nil || !strings.Contains(mapping, `"dims":768`) || !strings.Contains(mapping, `"dense_vector"`) {
		t.Fatalf("mapping=%s err=%v", mapping, err)
	}
	for _, field := range []string{`"parser_status":{"type":"keyword"}`, `"index_status":{"type":"keyword"}`} {
		if !strings.Contains(mapping, field) {
			t.Fatalf("mapping is missing status field %s: %s", field, mapping)
		}
	}
}

func TestReaderSearchFilterRequiresApprovedPublished(t *testing.T) {
	filters, err := json.Marshal(accessFilters("tenant", "principal", []string{"collection"}, "", true, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"trust_status":"approved"`, `"publication_status":"published"`} {
		if !strings.Contains(string(filters), required) {
			t.Fatalf("reader filter is missing %s: %s", required, filters)
		}
	}
}

func TestContributorSearchFilterAllowsDiscoveryStatuses(t *testing.T) {
	filters, err := json.Marshal(accessFilters("tenant", "principal", []string{"collection"}, "", false, false))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(filters), "trust_status") || strings.Contains(string(filters), "publication_status") {
		t.Fatalf("contributor discovery unexpectedly filters status: %s", filters)
	}
}

func TestSearchKeywordEmitsStatusFiltersForMappedFields(t *testing.T) {
	var query map[string]any
	client, err := NewSearchClient("http://elasticsearch.test", "kms_chunks_v1", 0)
	if err != nil {
		t.Fatal(err)
	}
	client.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPut {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"acknowledged":true}`)), Request: r}, nil
		}
		if r.Method != http.MethodPost || r.URL.Path != "/kms_chunks_v1/_search" {
			t.Fatalf("unexpected Elasticsearch request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"hits":{"hits":[]}}`)), Request: r}, nil
	})}
	if _, err := client.SearchKeyword(context.Background(), "query", "tenant", "principal", []string{"collection"}, "", 10, true, false); err != nil {
		t.Fatal(err)
	}
	serialized, err := json.Marshal(query["query"].(map[string]any)["bool"].(map[string]any)["filter"])
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"parser_status":"success"`, `"index_status":"indexed"`, `"trust_status":"approved"`, `"publication_status":"published"`} {
		if !strings.Contains(string(serialized), required) {
			t.Fatalf("search request missing filter %s: %s", required, serialized)
		}
	}
}

func TestCaseReaderSearchFilterAllowsOnlyApprovedAndCustomerSources(t *testing.T) {
	serialized, err := json.Marshal(accessFilters("tenant", "principal", []string{"collection"}, "case-id", true, true))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"case_id":"case-id"`, `"trust_status":"approved"`, `"trust_status":"customer_source"`, `"publication_status":"published"`, `"draft"`, `"ready_for_review"`} {
		if !strings.Contains(string(serialized), required) {
			t.Fatalf("case reader prefilter missing %s: %s", required, serialized)
		}
	}
	if strings.Contains(string(serialized), `"trust_status":"unverified"`) {
		t.Fatalf("case reader prefilter allows generic unverified documents: %s", serialized)
	}
}
