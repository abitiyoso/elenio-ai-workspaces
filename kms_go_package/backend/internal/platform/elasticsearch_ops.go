package platform

import (
	"bytes"
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

// WithIndex returns a client that writes to another physical index, used to build a replacement index.
func (s *SearchClient) WithIndex(index string) (*SearchClient, error) {
	if strings.TrimSpace(index) == "" {
		return nil, fmt.Errorf("Elasticsearch index is required")
	}
	return &SearchClient{baseURL: s.baseURL, client: &http.Client{Timeout: 60 * time.Second}, index: index, dimensions: s.dimensions, ensured: &atomic.Bool{}}, nil
}

func (s *SearchClient) Dimensions() int { return s.dimensions }

// BulkIndexChunks upserts chunks by deterministic chunk ID, so a retried batch never duplicates documents.
func (s *SearchClient) BulkIndexChunks(ctx context.Context, chunks []IndexedChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, chunk := range chunks {
		if err := s.validateChunk(chunk); err != nil {
			return err
		}
		action, err := json.Marshal(map[string]any{"index": map[string]string{"_index": s.index, "_id": chunk.ChunkID}})
		if err != nil {
			return err
		}
		doc, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		body.Write(action)
		body.WriteByte('\n')
		body.Write(doc)
		body.WriteByte('\n')
	}
	if err := s.EnsureIndex(ctx); err != nil {
		return err
	}
	respBody, err := s.do(ctx, http.MethodPost, "/_bulk", "application/x-ndjson", &body, 8<<20)
	if err != nil {
		return fmt.Errorf("Elasticsearch bulk index: %w", err)
	}
	var result struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			ID    string          `json:"_id"`
			Error json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode Elasticsearch bulk response: %w", err)
	}
	if result.Errors {
		for _, item := range result.Items {
			for _, outcome := range item {
				if len(outcome.Error) > 0 && string(outcome.Error) != "null" {
					return fmt.Errorf("Elasticsearch bulk item %s failed: %s", outcome.ID, truncate(string(outcome.Error), 500))
				}
			}
		}
		return fmt.Errorf("Elasticsearch bulk request reported errors")
	}
	return nil
}

func (s *SearchClient) validateChunk(chunk IndexedChunk) error {
	if chunk.ChunkID == "" || chunk.TenantID == "" || chunk.DocumentID == "" || chunk.VersionID == "" || chunk.CollectionID == "" || chunk.Content == "" {
		return fmt.Errorf("chunk identity and content are required")
	}
	if s.dimensions > 0 && len(chunk.Embedding) != s.dimensions {
		return fmt.Errorf("chunk vector has %d dimensions; configured dimensions are %d", len(chunk.Embedding), s.dimensions)
	}
	if s.dimensions == 0 && len(chunk.Embedding) > 0 {
		return fmt.Errorf("embedding dimensions are not configured")
	}
	return nil
}

// UpdateScopePrincipals replaces the ACL projection for every chunk in one collection or case scope.
// An empty caseID targets collection-level chunks only.
func (s *SearchClient) UpdateScopePrincipals(ctx context.Context, tenantID, collectionID, caseID string, principals []string) error {
	if principals == nil {
		principals = []string{}
	}
	filters := []any{
		map[string]any{"term": map[string]any{"tenant_id": tenantID}},
		map[string]any{"term": map[string]any{"collection_id": collectionID}},
	}
	if caseID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"case_id": caseID}})
	} else {
		filters = append(filters, map[string]any{"bool": map[string]any{"must_not": map[string]any{"exists": map[string]any{"field": "case_id"}}}})
	}
	body, err := json.Marshal(map[string]any{
		"query":  map[string]any{"bool": map[string]any{"filter": filters}},
		"script": map[string]any{"lang": "painless", "source": "ctx._source.acl_principals=params.principals", "params": map[string]any{"principals": principals}},
	})
	if err != nil {
		return err
	}
	if err := s.EnsureIndex(ctx); err != nil {
		return err
	}
	if _, err := s.do(ctx, http.MethodPost, "/"+url.PathEscape(s.index)+"/_update_by_query?conflicts=proceed&refresh=true", "application/json", bytes.NewReader(body), 64<<10); err != nil {
		return fmt.Errorf("Elasticsearch ACL projection update: %w", err)
	}
	return nil
}

// Refresh makes recent writes visible to search and count requests.
func (s *SearchClient) Refresh(ctx context.Context) error {
	if _, err := s.do(ctx, http.MethodPost, "/"+url.PathEscape(s.index)+"/_refresh", "", nil, 64<<10); err != nil {
		return fmt.Errorf("Elasticsearch refresh: %w", err)
	}
	return nil
}

// CountChunks returns the number of indexed chunks, optionally limited to one tenant.
func (s *SearchClient) CountChunks(ctx context.Context, tenantID string) (int64, error) {
	query := map[string]any{"query": map[string]any{"match_all": map[string]any{}}}
	if tenantID != "" {
		query = map[string]any{"query": map[string]any{"term": map[string]any{"tenant_id": tenantID}}}
	}
	body, err := json.Marshal(query)
	if err != nil {
		return 0, err
	}
	respBody, err := s.do(ctx, http.MethodPost, "/"+url.PathEscape(s.index)+"/_count", "application/json", bytes.NewReader(body), 64<<10)
	if err != nil {
		return 0, fmt.Errorf("Elasticsearch count: %w", err)
	}
	var result struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	return result.Count, nil
}

// AliasTargets lists the physical indices behind an alias; it is empty when the name is a concrete index or missing.
func (s *SearchClient) AliasTargets(ctx context.Context, alias string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/_alias/"+url.PathEscape(alias), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Elasticsearch alias lookup returned %s: %s", resp.Status, truncate(string(body), 500))
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(result))
	for index := range result {
		targets = append(targets, index)
	}
	return targets, nil
}

// SwapAlias atomically points alias at target and removes it from every other index.
func (s *SearchClient) SwapAlias(ctx context.Context, alias, target string) error {
	current, err := s.AliasTargets(ctx, alias)
	if err != nil {
		return err
	}
	actions := []any{}
	for _, index := range current {
		if index != target {
			actions = append(actions, map[string]any{"remove": map[string]string{"index": index, "alias": alias}})
		}
	}
	actions = append(actions, map[string]any{"add": map[string]any{"index": target, "alias": alias, "is_write_index": true}})
	body, err := json.Marshal(map[string]any{"actions": actions})
	if err != nil {
		return err
	}
	if _, err := s.do(ctx, http.MethodPost, "/_aliases", "application/json", bytes.NewReader(body), 64<<10); err != nil {
		return fmt.Errorf("Elasticsearch alias swap: %w", err)
	}
	return nil
}

func (s *SearchClient) do(ctx context.Context, method, path, contentType string, body io.Reader, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("returned %s: %s", resp.Status, truncate(strings.TrimSpace(string(respBody)), 500))
	}
	return respBody, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
