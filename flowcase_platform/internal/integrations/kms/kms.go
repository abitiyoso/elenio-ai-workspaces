// Package kms is Flowcase's port to the external Knowledge Management System. KMS owns parsing,
// indexing, embeddings and source governance; Flowcase only stores references and calls this port.
// Retrieved text is untrusted evidence and must never be treated as instructions.
package kms

import (
	"context"
	"errors"
	"time"
)

var (
	ErrDisabled  = errors.New("knowledge provider is disabled")
	ErrForbidden = errors.New("knowledge source is not accessible to this user")
	ErrNotFound  = errors.New("knowledge source not found")
)

// Scope is the end-user context forwarded to KMS so KMS can apply its own ACL.
type Scope struct {
	TenantID string
	UserID   string
	CaseID   string
	TraceID  string
}

type SourceRef struct {
	SourceID  string `json:"source_id"`
	VersionID string `json:"version_id"`
	Anchor    string `json:"anchor,omitempty"`
}

type SourceStatus string

const (
	SourcePublished SourceStatus = "published"
	SourceWithdrawn SourceStatus = "withdrawn"
	SourceExpired   SourceStatus = "expired"
)

type Source struct {
	Ref            SourceRef    `json:"ref"`
	TenantID       string       `json:"tenant_id"`
	Title          string       `json:"title"`
	Classification string       `json:"classification"`
	Status         SourceStatus `json:"status"`
	ExpiresAt      *time.Time   `json:"expires_at,omitempty"`
}

type SearchRequest struct {
	Query       string   `json:"query"`
	Collections []string `json:"collections,omitempty"`
	Limit       int      `json:"limit"`
}

type Citation struct {
	Source  Source  `json:"source"`
	Snippet string  `json:"snippet"` // untrusted evidence text
	Score   float64 `json:"score"`
}

type SearchResult struct {
	Citations []Citation `json:"citations"`
}

// Health is safe to expose to the UI: it never contains secrets or endpoints with credentials.
type Health struct {
	Mode    string `json:"mode"`
	Status  string `json:"status"` // disabled | stub | ok | unavailable
	Message string `json:"message"`
}

type KnowledgeProvider interface {
	Search(ctx context.Context, scope Scope, req SearchRequest) (SearchResult, error)
	GetSource(ctx context.Context, scope Scope, ref SourceRef) (Source, error)
	Health(ctx context.Context) Health
}

// CheckTenant rejects provider responses that belong to another tenant, as a defence in depth
// beneath KMS's own ACL (UAT-06).
func CheckTenant(scope Scope, s Source) error {
	if s.TenantID != scope.TenantID {
		return ErrForbidden
	}
	return nil
}

type disabled struct{}

func NewDisabled() KnowledgeProvider { return disabled{} }

func (disabled) Search(context.Context, Scope, SearchRequest) (SearchResult, error) {
	return SearchResult{}, ErrDisabled
}
func (disabled) GetSource(context.Context, Scope, SourceRef) (Source, error) {
	return Source{}, ErrDisabled
}
func (disabled) Health(context.Context) Health {
	return Health{Mode: "disabled", Status: "disabled", Message: "No KMS retrieval contract is configured yet."}
}

// stub is a development-only, in-memory provider with fictional sources scoped to the caller's tenant.
type stub struct{}

func NewDevelopmentStub() KnowledgeProvider { return stub{} }

func (stub) Search(_ context.Context, scope Scope, req SearchRequest) (SearchResult, error) {
	if req.Limit <= 0 || req.Limit > 20 {
		req.Limit = 5
	}
	src := Source{
		Ref:      SourceRef{SourceID: "stub-source-1", VersionID: "v1"},
		TenantID: scope.TenantID, Title: "Development stub source (fictional)",
		Classification: "internal", Status: SourcePublished,
	}
	return SearchResult{Citations: []Citation{{Source: src, Snippet: "Stub evidence for local development only.", Score: 0}}}, nil
}

func (stub) GetSource(_ context.Context, scope Scope, ref SourceRef) (Source, error) {
	if ref.SourceID != "stub-source-1" {
		return Source{}, ErrNotFound
	}
	ref.VersionID = "v1"
	return Source{Ref: ref, TenantID: scope.TenantID, Title: "Development stub source (fictional)", Classification: "internal", Status: SourcePublished}, nil
}

func (stub) Health(context.Context) Health {
	return Health{Mode: "stub", Status: "stub", Message: "Development stub: fictional evidence, no real KMS connection."}
}
