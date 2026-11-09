package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

const (
	delegTenant     = "11111111-1111-4111-8111-111111111111"
	delegOther      = "22222222-2222-4222-8222-222222222222"
	delegActive     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	delegDisabled   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	delegOtherTenUs = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

type delegationDirectory struct {
	rows  map[string]PrincipalRecord // key: tenant + "/" + principal
	err   error
	calls int
}

func (d *delegationDirectory) LookupPrincipal(_ context.Context, tenantID, principalID string) (PrincipalRecord, error) {
	d.calls++
	if d.err != nil {
		return PrincipalRecord{}, d.err
	}
	record, ok := d.rows[tenantID+"/"+principalID]
	if !ok {
		return PrincipalRecord{}, ErrPrincipalNotProvisioned
	}
	return record, nil
}

func newDelegationDirectory() *delegationDirectory {
	return &delegationDirectory{rows: map[string]PrincipalRecord{
		delegTenant + "/" + delegActive:    {Status: "active", Roles: []string{"knowledge_steward"}},
		delegTenant + "/" + delegDisabled:  {Status: "disabled", Roles: []string{"platform_admin"}},
		delegOther + "/" + delegOtherTenUs: {Status: "active", Roles: []string{"platform_admin"}},
	}}
}

func delegatingClient() ServiceIdentity {
	return ServiceIdentity{ClientID: "client", TenantID: delegTenant, Scopes: []string{ScopeRetrievalQuery, ScopeRetrievalDelegate}}
}

func delegatedRequest(values ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/retrieval/query", nil)
	r.Header.Set("Authorization", "Bearer kms_svc_0123456789012345678901234567890123456789")
	for _, value := range values {
		r.Header.Add(DelegatedUserHeader, value)
	}
	return r
}

func TestResolveDelegatedUserAcceptsActivePrincipalWithRolesFromDirectory(t *testing.T) {
	directory := newDelegationDirectory()
	identity, delegated, err := ResolveDelegatedUser(context.Background(), delegatedRequest("AAAAAAAA-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), delegatingClient(), directory)
	if err != nil || !delegated {
		t.Fatalf("delegated=%v err=%v", delegated, err)
	}
	if identity.UserID != delegActive || identity.TenantID != delegTenant || !slices.Equal(identity.Roles, []string{"knowledge_steward"}) {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestResolveDelegatedUserWithoutHeaderIsNotDelegated(t *testing.T) {
	directory := newDelegationDirectory()
	identity, delegated, err := ResolveDelegatedUser(context.Background(), delegatedRequest(), ServiceIdentity{TenantID: delegTenant, Scopes: []string{ScopeRetrievalQuery}}, directory)
	if err != nil || delegated || identity.UserID != "" || directory.calls != 0 {
		t.Fatalf("identity=%+v delegated=%v err=%v calls=%d", identity, delegated, err, directory.calls)
	}
}

func TestResolveDelegatedUserRejections(t *testing.T) {
	for name, tc := range map[string]struct {
		client ServiceIdentity
		values []string
		reason string
	}{
		"header without delegate scope": {ServiceIdentity{TenantID: delegTenant, Scopes: []string{ScopeRetrievalQuery}}, []string{delegActive}, "delegation_not_permitted"},
		"two header values":             {delegatingClient(), []string{delegActive, delegActive}, "delegated_user_repeated"},
		"malformed uuid":                {delegatingClient(), []string{"not-a-uuid"}, "delegated_user_not_uuid"},
		"empty header":                  {delegatingClient(), []string{""}, "delegated_user_not_uuid"},
		"comma joined values":           {delegatingClient(), []string{delegActive + "," + delegActive}, "delegated_user_not_uuid"},
		"unknown principal":             {delegatingClient(), []string{"dddddddd-dddd-4ddd-8ddd-dddddddddddd"}, "principal_not_provisioned"},
		"disabled principal":            {delegatingClient(), []string{delegDisabled}, "principal_disabled"},
		"principal of another tenant":   {delegatingClient(), []string{delegOtherTenUs}, "principal_not_provisioned"},
	} {
		t.Run(name, func(t *testing.T) {
			_, delegated, err := ResolveDelegatedUser(context.Background(), delegatedRequest(tc.values...), tc.client, newDelegationDirectory())
			var rejection *DelegationRejection
			if !delegated || !errors.As(err, &rejection) || rejection.Reason != tc.reason {
				t.Fatalf("delegated=%v err=%v, want rejection %q", delegated, err, tc.reason)
			}
			if errors.Is(err, ErrVerifierUnavailable) {
				t.Fatal("a definitive rejection must not look like an outage")
			}
		})
	}
}

func TestResolveDelegatedUserReportsDirectoryOutageAsUnavailable(t *testing.T) {
	directory := &delegationDirectory{err: errors.New("connection refused")}
	_, delegated, err := ResolveDelegatedUser(context.Background(), delegatedRequest(delegActive), delegatingClient(), directory)
	var rejection *DelegationRejection
	if !delegated || !errors.Is(err, ErrVerifierUnavailable) || errors.As(err, &rejection) {
		t.Fatalf("delegated=%v err=%v, want ErrVerifierUnavailable", delegated, err)
	}
	_, _, err = ResolveDelegatedUser(context.Background(), delegatedRequest(delegActive), delegatingClient(), nil)
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("nil directory err=%v", err)
	}
}

func TestServiceClientScopesAndValidation(t *testing.T) {
	if got := ServiceClientScopes(false); !slices.Equal(got, []string{ScopeRetrievalQuery}) {
		t.Fatalf("default scopes %v", got)
	}
	if got := ServiceClientScopes(true); !slices.Equal(got, []string{ScopeRetrievalQuery, ScopeRetrievalDelegate}) {
		t.Fatalf("delegate scopes %v", got)
	}
	for _, scopes := range [][]string{nil, {ScopeRetrievalDelegate}, {ScopeRetrievalQuery, "admin:all"}} {
		if validServiceClientScopes(scopes) {
			t.Fatalf("scopes %v were accepted", scopes)
		}
	}
	if !validServiceClientScopes(ServiceClientScopes(true)) {
		t.Fatal("delegate scopes rejected")
	}
}

func TestDelegateScopeDoesNotReplaceQueryScope(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	record := ServiceIdentity{Audience: RetrievalAudience, Scopes: []string{ScopeRetrievalDelegate}, Grants: []ResourceGrant{{CollectionID: "c"}}, ExpiresAt: now.Add(time.Hour)}
	if serviceRecordValid(record, RetrievalAudience, false, now) {
		t.Fatal("a client with only retrieval:delegate must not authenticate for retrieval")
	}
}
