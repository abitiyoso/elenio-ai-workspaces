// Package runtime executes pinned agent versions with Eino ADK. Agents only
// produce drafts: every tool is read-only or deterministic, all evidence is
// untrusted data, and outputs are validated before anyone sees them.
package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var (
	ErrEvidenceUnavailable    = errors.New("evidence source is not available")
	ErrKMSDelegationRequired  = errors.New("KMS retrieval requires a delegated user session")
	ErrKMSScopeDenied         = errors.New("KMS denied the requested retrieval scope")
	errKMSServiceUnauthorized = errors.New("KMS rejected the retrieval service credential")
)

// Evidence is one retrievable source passage. Snippet text is data and is
// never interpreted as an instruction.
type Evidence struct {
	SourceID          string `json:"source_id"`
	SourceVersion     string `json:"source_version"`
	Title             string `json:"title"`
	Snippet           string `json:"snippet"`
	Citation          string `json:"citation"`
	Anchor            string `json:"anchor"`
	TrustStatus       string `json:"trust_status"`
	PublicationStatus string `json:"publication_status"`
}

// SearchScope is the tool binding's allowed retrieval scope plus the run's case.
type SearchScope struct {
	TenantID      string
	CaseID        string
	CollectionIDs []string
}

type EvidenceSource interface {
	Origin() string
	Search(ctx context.Context, scope SearchScope, query string, topK int) ([]Evidence, error)
}

// FixtureEvidence serves sandbox and evaluation fixtures. It is labelled
// "fixture" everywhere so test passages are never mistaken for KMS sources.
type FixtureEvidence struct {
	Items []Evidence
}

func (FixtureEvidence) Origin() string { return "fixture" }

func (f FixtureEvidence) Search(_ context.Context, _ SearchScope, query string, topK int) ([]Evidence, error) {
	terms := strings.Fields(strings.ToLower(query))
	type scored struct {
		item  Evidence
		score int
	}
	ranked := make([]scored, 0, len(f.Items))
	for _, item := range f.Items {
		text := strings.ToLower(item.Title + " " + item.Snippet)
		score := 0
		for _, term := range terms {
			if len(term) > 2 && strings.Contains(text, term) {
				score++
			}
		}
		ranked = append(ranked, scored{item, score})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if topK <= 0 || topK > len(ranked) {
		topK = len(ranked)
	}
	out := make([]Evidence, 0, topK)
	for _, r := range ranked[:topK] {
		out = append(out, r.item)
	}
	return out, nil
}

// ParseFixtures reads sandbox/evaluation fixtures, defaulting unknown trust to unverified.
func ParseFixtures(raw json.RawMessage) ([]Evidence, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []Evidence
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := items[:0]
	for _, item := range items {
		if strings.TrimSpace(item.SourceID) == "" {
			continue
		}
		switch item.TrustStatus {
		case "approved", "unverified", "customer_source", "expired", "conflicted":
		default:
			item.TrustStatus = "unverified"
		}
		if item.Citation == "" {
			item.Citation = item.Title
		}
		out = append(out, item)
	}
	return out, nil
}

// UnavailableEvidence fails closed when no evidence source is configured.
type UnavailableEvidence struct{}

func (UnavailableEvidence) Origin() string { return "kms" }
func (UnavailableEvidence) Search(context.Context, SearchScope, string, int) ([]Evidence, error) {
	return nil, ErrEvidenceUnavailable
}

type delegatedSessionKey struct{}

// WithDelegatedSession attaches the end user KMS retrieval is delegated to. For
// production runs this is the run's delegated_user (a Control Plane user UUID
// supplied by an authenticated service client), sent in KMSRetrieval.UserHeader.
func WithDelegatedSession(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, delegatedSessionKey{}, token)
}

func delegatedSession(ctx context.Context) string {
	token, _ := ctx.Value(delegatedSessionKey{}).(string)
	return token
}

// KMSRetrieval calls KMS POST /api/v1/retrieval/query with an audience-bound
// service credential. KMS also requires a delegated user in the same tenant;
// without one the adapter fails closed instead of widening scope.
//
// The run's case reference is a Flowcase case id, not a KMS case id, so it is
// only sent as case_id when ForwardCaseID is set.
type KMSRetrieval struct {
	BaseURL       string
	ServiceToken  string
	UserHeader    string
	ForwardCaseID bool
	Client        *http.Client
}

func (KMSRetrieval) Origin() string { return "kms" }

func (k KMSRetrieval) Search(ctx context.Context, scope SearchScope, query string, topK int) ([]Evidence, error) {
	if k.BaseURL == "" || k.ServiceToken == "" || k.UserHeader == "" || http.CanonicalHeaderKey(k.UserHeader) == "Authorization" {
		return nil, ErrEvidenceUnavailable
	}
	session := delegatedSession(ctx)
	if session == "" {
		return nil, ErrKMSDelegationRequired
	}
	endpoint, err := url.JoinPath(k.BaseURL, "/api/v1/retrieval/query")
	if err != nil {
		return nil, ErrEvidenceUnavailable
	}
	if topK <= 0 || topK > 10 {
		topK = 5
	}
	body := map[string]any{"query": query, "mode": "keyword", "top_k": topK}
	if len(scope.CollectionIDs) > 0 {
		body["collection_ids"] = scope.CollectionIDs
	}
	if k.ForwardCaseID && scope.CaseID != "" {
		body["case_id"] = scope.CaseID
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.ServiceToken)
	req.Header.Set(k.UserHeader, session)
	client := k.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport", ErrEvidenceUnavailable)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errKMSServiceUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		return nil, ErrKMSScopeDenied
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrEvidenceUnavailable, resp.StatusCode)
	}
	var decoded struct {
		Results []struct {
			DocumentID        string `json:"document_id"`
			VersionID         string `json:"version_id"`
			Title             string `json:"title"`
			Snippet           string `json:"snippet"`
			Citation          string `json:"citation"`
			SourceAnchor      string `json:"source_anchor"`
			PageStart         int    `json:"page_start"`
			TrustStatus       string `json:"trust_status"`
			PublicationStatus string `json:"publication_status"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: response", ErrEvidenceUnavailable)
	}
	out := make([]Evidence, 0, len(decoded.Results))
	for _, r := range decoded.Results {
		trust := r.TrustStatus
		switch r.PublicationStatus {
		case "expired", "revoked", "archived":
			trust = "expired"
		case "published", "":
		default:
			trust = "unverified"
		}
		anchor := r.SourceAnchor
		if anchor == "" && r.PageStart > 0 {
			anchor = fmt.Sprintf("p.%d", r.PageStart)
		}
		out = append(out, Evidence{SourceID: r.DocumentID, SourceVersion: r.VersionID, Title: r.Title, Snippet: r.Snippet,
			Citation: r.Citation, Anchor: anchor, TrustStatus: trust, PublicationStatus: r.PublicationStatus})
	}
	return out, nil
}
