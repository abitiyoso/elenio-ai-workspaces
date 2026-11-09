package auth

import (
	"context"
	"net/http/httptest"
	"testing"
)

const (
	testTenantA = "482cbf9a-1707-4be1-8e22-6b71c3c94071"
	testTenantB = "5d0b6a8e-3f1c-4c2a-9b7e-0a1b2c3d4e5f"
)

func testAccess(t *testing.T) *TailscaleServe {
	t.Helper()
	access, err := NewTailscaleServe([]AccessGrant{
		{Login: "Operator@Example.com", TenantID: testTenantA, Permissions: []string{"agent.read", "tool.read"}},
		{Login: "other@example.com", TenantID: testTenantB, Permissions: []string{"agent.read", "agent.create"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func TestTailscaleServeRejectsMissingOrUnmappedIdentity(t *testing.T) {
	access := testAccess(t)
	r := httptest.NewRequest("GET", "/api/v1/agents", nil)
	if _, err := access.VerifyRequest(context.Background(), r); err == nil {
		t.Fatal("request without Serve header was accepted")
	}
	r.Header.Set("Tailscale-User-Login", "stranger@example.com")
	if _, err := access.VerifyRequest(context.Background(), r); err == nil {
		t.Fatal("unmapped tailnet login was accepted")
	}
	r.Header.Set("Tailscale-User-Login", " operator@EXAMPLE.com ")
	principal, err := access.VerifyRequest(context.Background(), r)
	if err != nil || principal.TenantID != testTenantA || principal.Subject != "operator@example.com" {
		t.Fatalf("mapped login was not resolved: %+v err=%v", principal, err)
	}
}

func TestTailscaleServeIgnoresClientTenantHeaders(t *testing.T) {
	access := testAccess(t)
	r := httptest.NewRequest("GET", "/api/v1/agents", nil)
	r.Header.Set("Tailscale-User-Login", "operator@example.com")
	r.Header.Set("X-Tenant-ID", testTenantB)
	principal, err := access.VerifyRequest(context.Background(), r)
	if err != nil || principal.TenantID != testTenantA {
		t.Fatalf("client header influenced tenant: %+v err=%v", principal, err)
	}
}

func TestTailscaleServeAuthorizeDeniesCrossTenantAndUngrantedActions(t *testing.T) {
	access := testAccess(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Tailscale-User-Login", "operator@example.com")
	principal, err := access.VerifyRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := access.Authorize(ctx, principal, "agent.read", "tenant:"+testTenantA); err != nil {
		t.Fatalf("granted action denied: %v", err)
	}
	if err := access.Authorize(ctx, principal, "agent.read", "tenant:"+testTenantB); err == nil {
		t.Fatal("cross-tenant resource was authorized")
	}
	if err := access.Authorize(ctx, principal, "agent.create", "tenant:"+testTenantA); err == nil {
		t.Fatal("ungranted action was authorized")
	}
	if err := access.Authorize(ctx, principal, "agent.version.publish", "tenant:"+testTenantA); err == nil {
		t.Fatal("withheld publish action was authorized")
	}
	if err := access.Authorize(ctx, principal, "tool.read", "global:tool-catalog"); err != nil {
		t.Fatalf("global catalog read denied: %v", err)
	}
	if err := access.Authorize(ctx, principal, "agent.read", "global:tool-catalog"); err == nil {
		t.Fatal("non-read action on global catalog was authorized")
	}
	forged := principal
	forged.TenantID = testTenantB
	if err := access.Authorize(ctx, forged, "agent.read", "tenant:"+testTenantB); err == nil {
		t.Fatal("principal with forged tenant was authorized")
	}
}

func TestAccessMapRejectsInvalidEntries(t *testing.T) {
	cases := map[string][]AccessGrant{
		"empty":           nil,
		"bad tenant":      {{Login: "a@example.com", TenantID: "tenant-a", Permissions: []string{"agent.read"}}},
		"no permissions":  {{Login: "a@example.com", TenantID: testTenantA}},
		"duplicate login": {{Login: "a@example.com", TenantID: testTenantA, Permissions: []string{"agent.read"}}, {Login: "A@example.com", TenantID: testTenantB, Permissions: []string{"agent.read"}}},
		"bad permission":  {{Login: "a@example.com", TenantID: testTenantA, Permissions: []string{"Agent Read"}}},
	}
	for name, entries := range cases {
		if _, err := NewTailscaleServe(entries); err == nil {
			t.Errorf("%s: invalid access map was accepted", name)
		}
	}
}

func TestDevIdentityRequiresDevelopmentEnvironment(t *testing.T) {
	access := testAccess(t)
	for _, env := range []string{"", EnvironmentProduction, "staging"} {
		if _, err := NewDevIdentity(env, access, "operator@example.com"); err == nil {
			t.Fatalf("dev identity was enabled for environment %q", env)
		}
	}
	if _, err := NewDevIdentity(EnvironmentDevelopment, access, "stranger@example.com"); err == nil {
		t.Fatal("dev identity accepted an unmapped login")
	}
	if _, err := Environment("staging"); err == nil {
		t.Fatal("unknown environment was accepted")
	}
	if env, err := Environment(""); err != nil || env != EnvironmentProduction {
		t.Fatalf("empty environment must default to production: %q err=%v", env, err)
	}
}

func TestDevIdentityOverridesClientSuppliedLogin(t *testing.T) {
	dev, err := NewDevIdentity(EnvironmentDevelopment, testAccess(t), "operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Tailscale-User-Login", "other@example.com")
	principal, err := dev.VerifyRequest(context.Background(), r)
	if err != nil || principal.Subject != "operator@example.com" || principal.TenantID != testTenantA {
		t.Fatalf("dev identity honored client login: %+v err=%v", principal, err)
	}
	if r.Header.Get("Tailscale-User-Login") != "other@example.com" {
		t.Fatal("dev identity mutated the caller's request")
	}
}
