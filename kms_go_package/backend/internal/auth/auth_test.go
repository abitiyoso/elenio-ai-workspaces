package auth

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestDevelopmentMockIgnoresBrowserIdentity(t *testing.T) {
	r := httptest.NewRequest("GET", "/session", nil)
	r.Header.Set("X-Tenant-ID", "forged")
	r.Header.Set("X-User-ID", "forged")
	got, err := NewDevelopmentMock("dev-user", "dev-tenant").Authenticate(context.Background(), r)
	if err != nil || got.UserID != "dev-user" || got.TenantID != "dev-tenant" {
		t.Fatalf("identity = %#v, err = %v", got, err)
	}
}

func TestTrustedAdapterFailsClosedWithoutVerifier(t *testing.T) {
	if _, err := NewTrustedSessionAdapter(nil).Authenticate(context.Background(), httptest.NewRequest("GET", "/session", nil)); err != ErrVerifierNotConfigured {
		t.Fatalf("err = %v", err)
	}
}
