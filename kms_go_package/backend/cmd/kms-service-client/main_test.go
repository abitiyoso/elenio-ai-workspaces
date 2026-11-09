package main

import (
	"slices"
	"testing"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
)

func TestCreateArgsDelegateFlagAddsDelegateScope(t *testing.T) {
	base := []string{"--tenant-id", "00000000-0000-4000-8000-000000000002", "--actor-id", "00000000-0000-4000-8000-000000000001",
		"--name", "agent-studio", "--expires-at", "2027-01-01T00:00:00Z", "--grant", "00000000-0000-4000-8000-000000000003"}
	opts, err := parseCreateArgs(base)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(opts.Scopes, []string{auth.ScopeRetrievalQuery}) {
		t.Fatalf("default scopes = %v, want only %s", opts.Scopes, auth.ScopeRetrievalQuery)
	}
	opts, err = parseCreateArgs(append(slices.Clone(base), "--delegate"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(opts.Scopes, []string{auth.ScopeRetrievalQuery, auth.ScopeRetrievalDelegate}) {
		t.Fatalf("--delegate scopes = %v", opts.Scopes)
	}
	if len(opts.Grants) != 1 || opts.Grants[0].CollectionID != "00000000-0000-4000-8000-000000000003" || opts.Name != "agent-studio" {
		t.Fatalf("unexpected options %+v", opts)
	}
}

func TestCreateArgsRejectsBadExpiryAndGrant(t *testing.T) {
	if _, err := parseCreateArgs([]string{"--expires-at", "tomorrow"}); err == nil {
		t.Fatal("non-RFC3339 expiry was accepted")
	}
	if _, err := parseCreateArgs([]string{"--expires-at", "2027-01-01T00:00:00Z", "--grant", "a:b:c"}); err == nil {
		t.Fatal("malformed grant was accepted")
	}
	if _, err := parseCreateArgs([]string{"--expires-at", "2027-01-01T00:00:00Z", "--delegate=maybe"}); err == nil {
		t.Fatal("non-boolean --delegate value was accepted")
	}
}
