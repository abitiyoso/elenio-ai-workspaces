package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type SearchClient struct {
	baseURL    string
	client     *http.Client
	index      string
	dimensions int
	// ensured caches a successful index check so searches and writes do not re-create the index each call.
	ensured *atomic.Bool
}

type IndexedChunk struct {
	ChunkID           string    `json:"chunk_id"`
	TenantID          string    `json:"tenant_id"`
	DocumentID        string    `json:"document_id"`
	VersionID         string    `json:"version_id"`
	CollectionID      string    `json:"collection_id"`
	CaseID            string    `json:"case_id,omitempty"`
	Scope             string    `json:"scope"`
	Classification    string    `json:"classification"`
	TrustStatus       string    `json:"trust_status"`
	PublicationStatus string    `json:"publication_status"`
	ParserStatus      string    `json:"parser_status"`
	IndexStatus       string    `json:"index_status"`
	Title             string    `json:"title"`
	HeadingPath       []string  `json:"heading_path"`
	Content           string    `json:"content"`
	PageStart         *int      `json:"page_start,omitempty"`
	PageEnd           *int      `json:"page_end,omitempty"`
	SourceAnchor      string    `json:"source_anchor,omitempty"`
	EmbeddingModelID  string    `json:"embedding_model_id,omitempty"`
	Embedding         []float32 `json:"embedding,omitempty"`
	ACLPrincipals     []string  `json:"acl_principals"`
}

type SearchHit struct {
	ChunkID      string   `json:"chunk_id"`
	Score        float64  `json:"score"`
	DocumentID   string   `json:"document_id"`
	VersionID    string   `json:"version_id"`
	CollectionID string   `json:"collection_id"`
	Content      string   `json:"content"`
	HeadingPath  []string `json:"heading_path"`
	PageStart    *int     `json:"page_start,omitempty"`
	PageEnd      *int     `json:"page_end,omitempty"`
	SourceAnchor string   `json:"source_anchor,omitempty"`
}

func NewSearchClient(baseURL, index string, dimensions int) (*SearchClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid Elasticsearch URL")
	}
	if strings.TrimSpace(index) == "" {
		return nil, fmt.Errorf("Elasticsearch index is required")
	}
	return &SearchClient{baseURL: strings.TrimRight(baseURL, "/"), index: index, dimensions: dimensions, client: &http.Client{Timeout: 4 * time.Second}, ensured: &atomic.Bool{}}, nil
}

func (s *SearchClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Elasticsearch returned %s", resp.Status)
	}
	return nil
}

func (s *SearchClient) IndexName() string { return s.index }

func (s *SearchClient) CreateIndex(ctx context.Context) error {
	mapping, err := s.MappingJSON()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.baseURL+"/"+url.PathEscape(s.index), strings.NewReader(mapping))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		// An existing index, or an alias with this name (blue/green reindex), keeps its mapping.
		if strings.Contains(string(body), "resource_already_exists_exception") || strings.Contains(string(body), "already exists as alias") {
			return s.validateExistingMapping(ctx)
		}
		return fmt.Errorf("Elasticsearch index creation returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("Elasticsearch index creation returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *SearchClient) EnsureIndex(ctx context.Context) error {
	if s.ensured.Load() {
		return nil
	}
	if err := s.CreateIndex(ctx); err != nil {
		return err
	}
	s.ensured.Store(true)
	return nil
}

func (s *SearchClient) validateExistingMapping(ctx context.Context) error {
	if s.dimensions == 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/"+url.PathEscape(s.index)+"/_mapping", nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Elasticsearch mapping read returned %s", resp.Status)
	}
	var mapping map[string]struct {
		Mappings struct {
			Properties map[string]struct {
				Type string `json:"type"`
				Dims int    `json:"dims"`
			} `json:"properties"`
		} `json:"mappings"`
	}
	if err := json.Unmarshal(body, &mapping); err != nil {
		return fmt.Errorf("decode Elasticsearch mapping: %w", err)
	}
	for _, index := range mapping {
		vector, ok := index.Mappings.Properties["embedding"]
		if !ok || vector.Type != "dense_vector" || (vector.Dims != 0 && vector.Dims != s.dimensions) {
			return fmt.Errorf("Elasticsearch index mapping is incompatible with configured embedding dimensions; select a new versioned index and reindex")
		}
	}
	return nil
}

// MappingJSON omits the vector field until dimensions are known. A later model/dimension change requires a new index.
func (s *SearchClient) MappingJSON() (string, error) {
	properties := map[string]any{
		"chunk_id": map[string]string{"type": "keyword"}, "tenant_id": map[string]string{"type": "keyword"},
		"document_id": map[string]string{"type": "keyword"}, "version_id": map[string]string{"type": "keyword"},
		"collection_id": map[string]string{"type": "keyword"}, "case_id": map[string]string{"type": "keyword"}, "scope": map[string]string{"type": "keyword"},
		"classification": map[string]string{"type": "keyword"}, "trust_status": map[string]string{"type": "keyword"},
		"publication_status": map[string]string{"type": "keyword"}, "parser_status": map[string]string{"type": "keyword"}, "index_status": map[string]string{"type": "keyword"}, "title": map[string]string{"type": "text"},
		"heading_path": map[string]string{"type": "text"}, "content": map[string]string{"type": "text"},
		"page_start": map[string]string{"type": "integer"}, "page_end": map[string]string{"type": "integer"},
		"source_anchor": map[string]string{"type": "keyword"}, "embedding_model_id": map[string]string{"type": "keyword"},
		"acl_principals": map[string]string{"type": "keyword"},
	}
	if s.dimensions > 0 {
		properties["embedding"] = map[string]any{"type": "dense_vector", "dims": s.dimensions, "index": true, "similarity": "cosine"}
	}
	data, err := json.Marshal(map[string]any{"mappings": map[string]any{"properties": properties}})
	return string(data), err
}

func (s *SearchClient) IndexChunk(ctx context.Context, chunk IndexedChunk) error {
	if err := s.validateChunk(chunk); err != nil {
		return err
	}
	if err := s.EnsureIndex(ctx); err != nil {
		return err
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.baseURL+"/"+url.PathEscape(s.index)+"/_doc/"+url.PathEscape(chunk.ChunkID), strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("Elasticsearch chunk index returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *SearchClient) SearchKeyword(ctx context.Context, query, tenantID, principalID string, collectionIDs []string, caseID string, topK int, approvedOnly, allowCustomerSource bool) ([]SearchHit, error) {
	filters := accessFilters(tenantID, principalID, collectionIDs, caseID, approvedOnly, allowCustomerSource)
	body := map[string]any{"size": topK, "query": map[string]any{"bool": map[string]any{
		"must":   []any{map[string]any{"multi_match": map[string]any{"query": query, "fields": []string{"title^3", "heading_path^2", "content"}}}},
		"filter": filters,
	}}}
	return s.search(ctx, body)
}

func (s *SearchClient) SearchVector(ctx context.Context, vector []float32, tenantID, principalID string, collectionIDs []string, caseID string, topK int, approvedOnly, allowCustomerSource bool) ([]SearchHit, error) {
	if s.dimensions <= 0 || len(vector) != s.dimensions {
		return nil, fmt.Errorf("semantic search requires a configured embedding dimension")
	}
	body := map[string]any{"size": topK, "knn": map[string]any{
		"field": "embedding", "query_vector": vector, "k": topK, "num_candidates": max(topK*10, 100), "filter": accessFilters(tenantID, principalID, collectionIDs, caseID, approvedOnly, allowCustomerSource),
	}}
	return s.search(ctx, body)
}

func accessFilters(tenantID, principalID string, collectionIDs []string, caseID string, approvedOnly, allowCustomerSource bool) []any {
	filters := []any{
		map[string]any{"term": map[string]any{"tenant_id": tenantID}},
		map[string]any{"term": map[string]any{"parser_status": "success"}},
		map[string]any{"term": map[string]any{"index_status": "indexed"}},
	}
	if principalID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"acl_principals": principalID}})
	}
	if len(collectionIDs) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"collection_id": collectionIDs}})
	}
	if caseID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"case_id": caseID}})
	} else {
		filters = append(filters, map[string]any{"bool": map[string]any{"must_not": map[string]any{"exists": map[string]any{"field": "case_id"}}}})
	}
	if approvedOnly {
		if allowCustomerSource {
			filters = append(filters, map[string]any{"bool": map[string]any{"should": []any{
				map[string]any{"bool": map[string]any{"filter": []any{
					map[string]any{"term": map[string]any{"trust_status": "approved"}},
					map[string]any{"term": map[string]any{"publication_status": "published"}},
				}}},
				map[string]any{"bool": map[string]any{"filter": []any{
					map[string]any{"term": map[string]any{"trust_status": "customer_source"}},
					map[string]any{"terms": map[string]any{"publication_status": []string{"draft", "ready_for_review"}}},
				}}},
			}, "minimum_should_match": 1}})
		} else {
			filters = append(filters,
				map[string]any{"term": map[string]any{"trust_status": "approved"}},
				map[string]any{"term": map[string]any{"publication_status": "published"}},
			)
		}
	}
	return filters
}

func (s *SearchClient) search(ctx context.Context, query any) ([]SearchHit, error) {
	if err := s.EnsureIndex(ctx); err != nil {
		return nil, err
	}
	data, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/"+url.PathEscape(s.index)+"/_search", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Elasticsearch search returned %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}
	var result struct {
		Hits struct {
			Hits []struct {
				ID     string       `json:"_id"`
				Score  float64      `json:"_score"`
				Source IndexedChunk `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		hits = append(hits, SearchHit{
			ChunkID: hit.ID, Score: hit.Score, DocumentID: hit.Source.DocumentID, VersionID: hit.Source.VersionID,
			CollectionID: hit.Source.CollectionID, Content: hit.Source.Content, HeadingPath: hit.Source.HeadingPath,
			PageStart: hit.Source.PageStart, PageEnd: hit.Source.PageEnd, SourceAnchor: hit.Source.SourceAnchor,
		})
	}
	return hits, nil
}

func (s *SearchClient) DeleteVersion(ctx context.Context, tenantID, versionID string) error {
	query, _ := json.Marshal(map[string]any{"query": map[string]any{"bool": map[string]any{"filter": []any{
		map[string]any{"term": map[string]any{"tenant_id": tenantID}},
		map[string]any{"term": map[string]any{"version_id": versionID}},
	}}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/"+url.PathEscape(s.index)+"/_delete_by_query?conflicts=proceed&refresh=true", strings.NewReader(string(query)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("Elasticsearch delete returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *SearchClient) UpdateVersionMetadata(ctx context.Context, tenantID, versionID, trustStatus, publicationStatus string) error {
	if trustStatus != "approved" && trustStatus != "unverified" && trustStatus != "customer_source" {
		return fmt.Errorf("invalid trust status")
	}
	switch publicationStatus {
	case "draft", "ready_for_review", "published", "expired", "archived", "revoked":
	default:
		return fmt.Errorf("invalid publication status")
	}
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			map[string]any{"term": map[string]any{"tenant_id": tenantID}},
			map[string]any{"term": map[string]any{"version_id": versionID}},
		}}},
		"script": map[string]any{"lang": "painless", "source": "ctx._source.trust_status=params.trust;ctx._source.publication_status=params.publication", "params": map[string]any{"trust": trustStatus, "publication": publicationStatus}},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/"+url.PathEscape(s.index)+"/_update_by_query?conflicts=proceed&refresh=true", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		response, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("Elasticsearch version metadata update returned %s: %s", resp.Status, strings.TrimSpace(string(response)))
	}
	return nil
}
