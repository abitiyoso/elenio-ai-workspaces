package auth

import (
	"strings"
	"testing"
	"time"
)

func TestServiceRecordRequiresAudienceExpiryScopeAndGrant(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	base := ServiceIdentity{Audience: RetrievalAudience, Scopes: []string{"retrieval:query"}, Grants: []ResourceGrant{{CollectionID: "c"}}, ExpiresAt: now.Add(time.Hour)}
	if !serviceRecordValid(base, RetrievalAudience, false, now) {
		t.Fatal("valid scoped service client was rejected")
	}
	if serviceRecordValid(base, "other-audience", false, now) {
		t.Fatal("wrong audience was accepted")
	}
	if serviceRecordValid(base, RetrievalAudience, true, now) {
		t.Fatal("revoked credential was accepted")
	}
	base.ExpiresAt = now
	if serviceRecordValid(base, RetrievalAudience, false, now) {
		t.Fatal("expired credential was accepted")
	}
	base.ExpiresAt = now.Add(time.Hour)
	base.Scopes = []string{"unrelated:scope"}
	if serviceRecordValid(base, RetrievalAudience, false, now) {
		t.Fatal("credential without retrieval scope was accepted")
	}
	base.Scopes = []string{"retrieval:query"}
	base.Grants = nil
	if serviceRecordValid(base, RetrievalAudience, false, now) {
		t.Fatal("credential without a resource grant was accepted")
	}
}

func TestDecodeServiceRecordAcceptsStoredScopesAndGrants(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	record := ServiceIdentity{ClientID: "client", TenantID: "tenant", Audience: RetrievalAudience, ExpiresAt: now.Add(time.Hour)}
	scopes := []byte(`["retrieval:query"]`)
	grants := []byte(`[{"collection_id":"collection-a","case_id":null}]`)
	identity, err := decodeServiceRecord(record, scopes, grants, RetrievalAudience, false, now)
	if err != nil {
		t.Fatalf("active credential stored in the database was rejected: %v", err)
	}
	if len(identity.Grants) != 1 || identity.Grants[0].CollectionID != "collection-a" || identity.Grants[0].CaseID != "" {
		t.Fatalf("unexpected decoded grants %+v", identity.Grants)
	}
	if _, err := decodeServiceRecord(record, scopes, []byte(`[]`), RetrievalAudience, false, now); err != ErrServiceUnauthenticated {
		t.Fatalf("credential without grants: got %v", err)
	}
	if _, err := decodeServiceRecord(record, scopes, grants, RetrievalAudience, true, now); err != ErrServiceUnauthenticated {
		t.Fatalf("revoked credential: got %v", err)
	}
}

func TestServiceGrantDoesNotBroadenCollectionScopeIntoCase(t *testing.T) {
	identity := ServiceIdentity{Grants: []ResourceGrant{{CollectionID: "collection-a"}, {CollectionID: "collection-b", CaseID: "case-1"}}}
	if !AllowsGrant(identity, "collection-a", "") {
		t.Fatal("collection grant should allow that collection scope")
	}
	if AllowsGrant(identity, "collection-a", "case-1") {
		t.Fatal("collection-only grant must not include case-private scope")
	}
	if !AllowsGrant(identity, "collection-b", "case-1") {
		t.Fatal("explicit case grant should allow the matching case")
	}
	if AllowsGrant(identity, "collection-b", "case-2") {
		t.Fatal("case grant must not extend to another case")
	}
}

func TestCredentialHashStoresDigestNotSecret(t *testing.T) {
	secret := "kms_svc_secret-value-never-persist"
	hash := CredentialHash(secret)
	if hash == secret || strings.Contains(hash, secret) || len(hash) != 64 {
		t.Fatalf("unexpected credential hash %q", hash)
	}
	if hash != CredentialHash(secret) {
		t.Fatal("credential hashing is not deterministic")
	}
}
