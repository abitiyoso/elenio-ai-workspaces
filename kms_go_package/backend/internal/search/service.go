package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

var (
	ErrEmbeddingUnavailable = platform.ErrEmbeddingUnavailable
	ErrForbidden            = errors.New("search scope is not authorized")
	ErrInvalidRequest       = errors.New("invalid search request")
	searchUUIDPattern       = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type Service struct {
	db        *sql.DB
	search    *platform.SearchClient
	embedding platform.EmbeddingProvider
}

// SearchService applies the service credential's exact collection/case grants,
// then rechecks current PostgreSQL lifecycle state before returning any content.
func (s *Service) SearchService(ctx context.Context, identity auth.ServiceIdentity, human auth.Identity, request Request) ([]Result, error) {
	if identity.TenantID != human.TenantID || !searchUUIDPattern.MatchString(human.UserID) || !searchUUIDPattern.MatchString(human.TenantID) {
		return nil, ErrForbidden
	}
	request.Query = strings.TrimSpace(request.Query)
	if request.Query == "" || len(request.Query) > 2000 {
		return nil, fmt.Errorf("%w: query must contain 1 to 2000 characters", ErrInvalidRequest)
	}
	if request.Mode == "" {
		request.Mode = "keyword"
	}
	if request.Mode != "keyword" && request.Mode != "semantic" && request.Mode != "hybrid" {
		return nil, fmt.Errorf("%w: invalid search mode", ErrInvalidRequest)
	}
	if request.TopK == 0 {
		request.TopK = 10
	}
	if request.TopK < 1 || request.TopK > 50 {
		return nil, fmt.Errorf("%w: top_k must be between 1 and 50", ErrInvalidRequest)
	}
	if request.CaseID != "" && !searchUUIDPattern.MatchString(request.CaseID) {
		return nil, fmt.Errorf("%w: case_id must be a UUID", ErrInvalidRequest)
	}
	selected := map[string]string{}
	for _, grant := range identity.Grants {
		if request.CaseID == "" && grant.CaseID == "" || request.CaseID != "" && grant.CaseID == request.CaseID {
			selected[grant.CollectionID] = grant.CaseID
		}
	}
	if request.CaseID != "" && len(selected) == 0 {
		return nil, ErrForbidden
	}
	if len(request.CollectionIDs) > 0 {
		allowed := map[string]string{}
		for _, id := range request.CollectionIDs {
			if !searchUUIDPattern.MatchString(id) {
				return nil, ErrInvalidRequest
			}
			if scope, ok := selected[id]; ok {
				allowed[id] = scope
			} else {
				return nil, ErrForbidden
			}
		}
		selected = allowed
	}
	if len(selected) == 0 {
		return []Result{}, nil
	}
	for collectionID, grantCaseID := range selected {
		var allowed bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM service_client_grants g JOIN collection_memberships cm ON cm.tenant_id=g.tenant_id AND cm.collection_id=g.collection_id AND cm.principal_id=$5
			WHERE g.tenant_id=$1 AND g.client_id=$2 AND g.collection_id=$3 AND g.case_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid
			AND (NULLIF($4,'') IS NULL OR EXISTS(SELECT 1 FROM case_memberships casem WHERE casem.tenant_id=g.tenant_id AND casem.case_id=g.case_id AND casem.principal_id=$5))
		)`, identity.TenantID, identity.ClientID, collectionID, grantCaseID, human.UserID).Scan(&allowed); err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrForbidden
		}
	}
	collections := make([]string, 0, len(selected))
	for id := range selected {
		collections = append(collections, id)
	}
	var lexical, semantic []platform.SearchHit
	if request.Mode == "keyword" || request.Mode == "hybrid" {
		hits, err := s.search.SearchKeyword(ctx, request.Query, identity.TenantID, "", collections, request.CaseID, request.TopK, true, false)
		if err != nil {
			return nil, err
		}
		lexical = hits
	}
	if request.Mode == "semantic" || request.Mode == "hybrid" {
		if s.embedding == nil {
			return nil, ErrEmbeddingUnavailable
		}
		vectors, err := s.embedding.Embed(ctx, []string{request.Query})
		if err != nil {
			return nil, err
		}
		if len(vectors) != 1 || len(vectors[0]) != s.embedding.Dimensions() {
			return nil, errors.New("embedding provider returned an invalid vector")
		}
		hits, err := s.search.SearchVector(ctx, vectors[0], identity.TenantID, "", collections, request.CaseID, request.TopK, true, false)
		if err != nil {
			return nil, err
		}
		semantic = hits
	}
	ranked := fuse(lexical, semantic, request.Mode, request.TopK)
	return s.authorizeServiceHits(ctx, identity, human, request.CaseID, ranked)
}

func (s *Service) authorizeServiceHits(ctx context.Context, identity auth.ServiceIdentity, human auth.Identity, caseID string, ranked []rankedHit) ([]Result, error) {
	ids := make([]string, 0, len(ranked))
	ranks := map[string]rankedHit{}
	for _, hit := range ranked {
		ids = append(ids, hit.hit.ChunkID)
		ranks[hit.hit.ChunkID] = hit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT dc.chunk_id,dc.document_id::text,dc.version_id::text,d.title,dc.content,to_json(dc.heading_path),dc.page_start,dc.page_end,COALESCE(dc.source_anchor,''),v.trust_status,v.publication_status
	FROM document_chunks dc JOIN documents d ON d.tenant_id=dc.tenant_id AND d.id=dc.document_id JOIN document_versions v ON v.tenant_id=dc.tenant_id AND v.id=dc.version_id AND v.document_id=d.id
	WHERE dc.tenant_id=$1 AND dc.chunk_id=ANY($2::text[]) AND dc.indexed_at IS NOT NULL AND d.case_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
	AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND d.published_version_id=v.id AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())
	AND v.review_status='approved' AND v.trust_status='approved' AND v.publication_status='published' AND v.parser_status='success' AND v.index_status='indexed'
	AND EXISTS(SELECT 1 FROM service_clients sc WHERE sc.tenant_id=d.tenant_id AND sc.id=$4 AND sc.revoked_at IS NULL AND sc.expires_at>now() AND 'retrieval:query'=ANY(sc.scopes))
	AND EXISTS(SELECT 1 FROM service_client_grants g WHERE g.tenant_id=d.tenant_id AND g.client_id=$4 AND g.collection_id=d.collection_id AND g.case_id IS NOT DISTINCT FROM d.case_id)
	AND EXISTS(SELECT 1 FROM collection_memberships cm WHERE cm.tenant_id=d.tenant_id AND cm.collection_id=d.collection_id AND cm.principal_id=$5)
	AND (d.case_id IS NULL OR EXISTS(SELECT 1 FROM case_memberships casem WHERE casem.tenant_id=d.tenant_id AND casem.case_id=d.case_id AND casem.principal_id=$5))`, identity.TenantID, ids, caseID, identity.ClientID, human.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]Result{}
	for rows.Next() {
		var item Result
		var heading []byte
		var p1, p2 sql.NullInt64
		if err := rows.Scan(&item.ChunkID, &item.DocumentID, &item.VersionID, &item.Title, &item.Snippet, &heading, &p1, &p2, &item.SourceAnchor, &item.TrustStatus, &item.PublicationStatus); err != nil {
			return nil, err
		}
		if len(heading) > 0 {
			if err := json.Unmarshal(heading, &item.HeadingPath); err != nil {
				return nil, err
			}
		}
		if p1.Valid {
			v := int(p1.Int64)
			item.PageStart = &v
		}
		if p2.Valid {
			v := int(p2.Int64)
			item.PageEnd = &v
		}
		hit := ranks[item.ChunkID]
		item.RelevanceScore = hit.score
		item.MatchedReason = hit.reason
		item.Citation = citation(item)
		byID[item.ChunkID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Result{}
	for _, hit := range ranked {
		if item, ok := byID[hit.hit.ChunkID]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

type Request struct {
	Query         string   `json:"query"`
	Mode          string   `json:"mode"`
	CollectionIDs []string `json:"collection_ids,omitempty"`
	CaseID        string   `json:"case_id,omitempty"`
	TopK          int      `json:"top_k,omitempty"`
}

type Result struct {
	ChunkID           string   `json:"chunk_id"`
	DocumentID        string   `json:"document_id"`
	VersionID         string   `json:"version_id"`
	Title             string   `json:"title"`
	Snippet           string   `json:"snippet"`
	HeadingPath       []string `json:"heading_path"`
	PageStart         *int     `json:"page_start,omitempty"`
	PageEnd           *int     `json:"page_end,omitempty"`
	SourceAnchor      string   `json:"source_anchor,omitempty"`
	Citation          string   `json:"citation"`
	TrustStatus       string   `json:"trust_status"`
	PublicationStatus string   `json:"publication_status"`
	RelevanceScore    float64  `json:"relevance_score"`
	MatchedReason     string   `json:"matched_reason"`
}

type searchPolicy uint8

const (
	approvedEvidencePolicy searchPolicy = iota
	caseCustomerSourcePolicy
	editorDiscoveryPolicy
)

func NewService(db *sql.DB, searchClient *platform.SearchClient, embedding platform.EmbeddingProvider) *Service {
	return &Service{db: db, search: searchClient, embedding: embedding}
}

func (s *Service) Search(ctx context.Context, identity auth.Identity, request Request) ([]Result, error) {
	request.Query = strings.TrimSpace(request.Query)
	if request.Query == "" || len(request.Query) > 2000 {
		return nil, fmt.Errorf("%w: query must contain 1 to 2000 characters", ErrInvalidRequest)
	}
	if request.Mode == "" {
		request.Mode = "keyword"
	}
	if request.Mode != "keyword" && request.Mode != "semantic" && request.Mode != "hybrid" {
		return nil, fmt.Errorf("%w: mode must be keyword, semantic, or hybrid", ErrInvalidRequest)
	}
	if request.TopK == 0 {
		request.TopK = 10
	}
	if request.TopK < 1 || request.TopK > 50 {
		return nil, fmt.Errorf("%w: top_k must be between 1 and 50", ErrInvalidRequest)
	}
	if !searchUUIDPattern.MatchString(identity.TenantID) || !searchUUIDPattern.MatchString(identity.UserID) {
		return nil, fmt.Errorf("%w: verified identity is invalid", ErrInvalidRequest)
	}
	if request.CaseID != "" && !searchUUIDPattern.MatchString(request.CaseID) {
		return nil, fmt.Errorf("%w: case_id must be a UUID", ErrInvalidRequest)
	}
	collectionAccess, err := s.accessibleCollections(ctx, identity, request.CaseID)
	if err != nil {
		return nil, err
	}
	if len(collectionAccess) == 0 {
		// Explicitly requested scopes are refused the same way whether or not the user has other memberships.
		if request.CaseID != "" || len(request.CollectionIDs) > 0 {
			return nil, ErrForbidden
		}
		return []Result{}, nil
	}
	collectionSet := make(map[string]bool, len(collectionAccess))
	for id := range collectionAccess {
		collectionSet[id] = true
	}
	if len(request.CollectionIDs) > 0 {
		selected := make(map[string]searchPolicy, len(request.CollectionIDs))
		seen := make(map[string]bool, len(request.CollectionIDs))
		for _, id := range request.CollectionIDs {
			if !searchUUIDPattern.MatchString(id) || !collectionSet[id] {
				return nil, ErrForbidden
			}
			if !seen[id] {
				selected[id] = collectionAccess[id]
				seen[id] = true
			}
		}
		collectionAccess = selected
	}
	if len(collectionAccess) == 0 {
		return []Result{}, nil
	}

	var lexical, semantic []platform.SearchHit
	for policy, collections := range splitCollections(collectionAccess) {
		if len(collections) == 0 {
			continue
		}
		approvedOnly := policy != editorDiscoveryPolicy
		allowCustomerSource := policy == caseCustomerSourcePolicy
		if request.Mode == "keyword" || request.Mode == "hybrid" {
			hits, searchErr := s.search.SearchKeyword(ctx, request.Query, identity.TenantID, identity.UserID, collections, request.CaseID, request.TopK, approvedOnly, allowCustomerSource)
			if searchErr != nil {
				return nil, fmt.Errorf("keyword search: %w", searchErr)
			}
			lexical = append(lexical, hits...)
		}
		if request.Mode == "semantic" || request.Mode == "hybrid" {
			if s.embedding == nil {
				return nil, ErrEmbeddingUnavailable
			}
			vectors, embedErr := s.embedding.Embed(ctx, []string{request.Query})
			if embedErr != nil {
				return nil, fmt.Errorf("embed search query: %w", embedErr)
			}
			if len(vectors) != 1 || len(vectors[0]) != s.embedding.Dimensions() {
				return nil, errors.New("embedding provider returned a vector with an invalid count or dimension")
			}
			hits, searchErr := s.search.SearchVector(ctx, vectors[0], identity.TenantID, identity.UserID, collections, request.CaseID, request.TopK, approvedOnly, allowCustomerSource)
			if searchErr != nil {
				return nil, fmt.Errorf("semantic search: %w", searchErr)
			}
			semantic = append(semantic, hits...)
		}
	}
	ranked := fuse(lexical, semantic, request.Mode, request.TopK)
	if len(ranked) == 0 {
		return []Result{}, nil
	}
	return s.authorizeHits(ctx, identity, request.CaseID, ranked)
}

func (s *Service) accessibleCollections(ctx context.Context, identity auth.Identity, caseID string) (map[string]searchPolicy, error) {
	var rows *sql.Rows
	var err error
	if caseID != "" {
		rows, err = s.db.QueryContext(ctx, `SELECT c.collection_id::text,m.membership_role,cm.membership_role FROM case_scopes c
		JOIN case_memberships cm ON cm.tenant_id=c.tenant_id AND cm.case_id=c.case_id
		JOIN collection_memberships m ON m.tenant_id=c.tenant_id AND m.collection_id=c.collection_id AND m.principal_id=cm.principal_id
		WHERE c.tenant_id=$1 AND c.case_id=$2 AND cm.principal_id=$3`, identity.TenantID, caseID, identity.UserID)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT collection_id::text,membership_role FROM collection_memberships WHERE tenant_id=$1 AND principal_id=$2`, identity.TenantID, identity.UserID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	access := map[string]searchPolicy{}
	for rows.Next() {
		var id string
		var collectionRole string
		if caseID != "" {
			var caseRole string
			if err := rows.Scan(&id, &collectionRole, &caseRole); err != nil {
				return nil, err
			}
			access[id] = policyForRoles(true, collectionRole, caseRole)
		} else {
			if err := rows.Scan(&id, &collectionRole); err != nil {
				return nil, err
			}
			access[id] = policyForRoles(false, collectionRole, "")
		}
	}
	return access, rows.Err()
}

func canDiscoverDrafts(role string) bool {
	return role == "owner" || role == "steward" || role == "contributor"
}

func policyForRoles(caseScoped bool, collectionRole, caseRole string) searchPolicy {
	if caseScoped {
		if canDiscoverDrafts(caseRole) {
			return editorDiscoveryPolicy
		}
		return caseCustomerSourcePolicy
	}
	if canDiscoverDrafts(collectionRole) {
		return editorDiscoveryPolicy
	}
	return approvedEvidencePolicy
}

func splitCollections(access map[string]searchPolicy) map[searchPolicy][]string {
	groups := map[searchPolicy][]string{}
	for id, policy := range access {
		groups[policy] = append(groups[policy], id)
	}
	return groups
}

func authorizedChunk(caseScoped bool, collectionRole, caseRole, lifecycle, review, trust, parser, index string, currentPublished bool) bool {
	if parser != "success" || index != "indexed" {
		return false
	}
	if trust == "customer_source" {
		return caseScoped && caseRole != ""
	}
	if caseScoped && canDiscoverDrafts(caseRole) || !caseScoped && canDiscoverDrafts(collectionRole) {
		return true
	}
	return currentPublished && lifecycle == "published" && review == "approved" && trust == "approved"
}

type rankedHit struct {
	hit    platform.SearchHit
	score  float64
	reason string
}

func fuse(lexical, semantic []platform.SearchHit, mode string, limit int) []rankedHit {
	results := map[string]*rankedHit{}
	add := func(hits []platform.SearchHit, reason string) {
		for rank, hit := range hits {
			entry := results[hit.ChunkID]
			if entry == nil {
				entry = &rankedHit{hit: hit}
				results[hit.ChunkID] = entry
			}
			entry.score += 1 / float64(60+rank+1)
			if entry.reason == "" {
				entry.reason = reason
			} else if entry.reason != reason && !strings.Contains(entry.reason, "+") {
				entry.reason += "+" + reason
			}
		}
	}
	if mode == "keyword" {
		add(lexical, "keyword")
	} else if mode == "semantic" {
		add(semantic, "semantic")
	} else {
		add(lexical, "keyword")
		add(semantic, "semantic")
	}
	out := make([]rankedHit, 0, len(results))
	for _, result := range results {
		out = append(out, *result)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) authorizeHits(ctx context.Context, identity auth.Identity, caseID string, ranked []rankedHit) ([]Result, error) {
	ids := make([]string, 0, len(ranked))
	rankMap := make(map[string]rankedHit, len(ranked))
	for _, result := range ranked {
		ids = append(ids, result.hit.ChunkID)
		rankMap[result.hit.ChunkID] = result
	}
	rows, err := s.db.QueryContext(ctx, `SELECT dc.chunk_id,dc.document_id::text,dc.version_id::text,d.title,dc.content,to_json(dc.heading_path),
	dc.page_start,dc.page_end,COALESCE(dc.source_anchor,''),COALESCE(d.case_id::text,''),v.publication_status,v.review_status,
	v.trust_status,v.parser_status,v.index_status,cm.membership_role,COALESCE(casem.membership_role,''),(d.published_version_id=v.id AND v.publication_status='published')
	FROM document_chunks dc
	JOIN documents d ON d.tenant_id=dc.tenant_id AND d.id=dc.document_id
	JOIN collection_memberships cm ON cm.tenant_id=d.tenant_id AND cm.collection_id=d.collection_id AND cm.principal_id=$3
	JOIN document_versions v ON v.tenant_id=dc.tenant_id AND v.id=dc.version_id AND v.document_id=d.id
	LEFT JOIN knowledge_sources ks ON ks.tenant_id=d.tenant_id AND ks.document_id=d.id
	LEFT JOIN case_memberships casem ON casem.tenant_id=d.tenant_id AND casem.case_id=d.case_id AND casem.principal_id=$3
	WHERE dc.tenant_id=$1 AND dc.chunk_id = ANY($2::text[]) AND dc.indexed_at IS NOT NULL
	AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())
	AND ((($4='') AND d.case_id IS NULL) OR (($4<>'') AND d.case_id=$4::uuid AND casem.principal_id=$3))`, identity.TenantID, ids, identity.UserID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	authorized := map[string]Result{}
	for rows.Next() {
		var result Result
		var headingJSON []byte
		var pageStart, pageEnd sql.NullInt64
		var hitCaseID string
		var lifecycle, review, trust, parser, index, collectionRole, caseRole string
		var currentPublished bool
		if err := rows.Scan(&result.ChunkID, &result.DocumentID, &result.VersionID, &result.Title, &result.Snippet, &headingJSON, &pageStart, &pageEnd, &result.SourceAnchor, &hitCaseID, &lifecycle, &review, &trust, &parser, &index, &collectionRole, &caseRole, &currentPublished); err != nil {
			return nil, err
		}
		if len(headingJSON) > 0 {
			if err := json.Unmarshal(headingJSON, &result.HeadingPath); err != nil {
				return nil, fmt.Errorf("decode authorized chunk heading path: %w", err)
			}
		}
		if !authorizedChunk(hitCaseID != "", collectionRole, caseRole, lifecycle, review, trust, parser, index, currentPublished) {
			continue
		}
		if pageStart.Valid {
			value := int(pageStart.Int64)
			result.PageStart = &value
		}
		if pageEnd.Valid {
			value := int(pageEnd.Int64)
			result.PageEnd = &value
		}
		result.TrustStatus = trust
		result.PublicationStatus = lifecycle
		if hit, ok := rankMap[result.ChunkID]; ok {
			result.RelevanceScore = hit.score
			result.MatchedReason = hit.reason
		}
		result.Citation = citation(result)
		authorized[result.ChunkID] = result
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(authorized))
	for _, rankedResult := range ranked {
		if result, ok := authorized[rankedResult.hit.ChunkID]; ok {
			results = append(results, result)
		}
	}
	return results, nil
}

func citation(result Result) string {
	parts := []string{result.Title}
	if len(result.HeadingPath) > 0 {
		parts = append(parts, strings.Join(result.HeadingPath, " / "))
	}
	if result.PageStart != nil && result.PageEnd != nil {
		if *result.PageStart == *result.PageEnd {
			parts = append(parts, fmt.Sprintf("page %d", *result.PageStart))
		} else {
			parts = append(parts, fmt.Sprintf("pages %d–%d", *result.PageStart, *result.PageEnd))
		}
	}
	return strings.Join(parts, " · ")
}
