package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"
	"time"
)

func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func testServiceTokens(t *testing.T, now time.Time) *ServiceTokens {
	t.Helper()
	tokens, err := NewServiceTokens([]ServiceGrant{{
		ClientID: "flowcase", TokenSHA256: digest("flowcase-token-0123456789abcdef0123"), TenantID: testTenantA,
		Permissions: []string{"run.invoke", "run.read"}, ExpiresAt: now.Add(time.Hour),
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func TestServiceTokensRejectNonRunPermissions(t *testing.T) {
	_, err := NewServiceTokens([]ServiceGrant{{ClientID: "flowcase", TokenSHA256: digest("x"), TenantID: testTenantA,
		Permissions: []string{"release.approve"}, ExpiresAt: time.Now().Add(time.Hour)}}, time.Now)
	if err == nil {
		t.Fatal("service grant with release authority was accepted")
	}
}

func TestCompositeRoutesBearerToServiceWithoutFallback(t *testing.T) {
	now := time.Now()
	dev, err := NewDevIdentity(EnvironmentDevelopment, testAccess(t), "operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	c := Composite{Human: dev, Service: testServiceTokens(t, now)}
	ctx := context.Background()

	r := httptest.NewRequest("POST", "/api/v1/runs", nil)
	r.Header.Set("Authorization", "Bearer wrong-token-0123456789abcdef0123456789")
	if _, err := c.VerifyRequest(ctx, r); err == nil {
		t.Fatal("bad bearer token fell back to the development identity")
	}

	r.Header.Set("Authorization", "Bearer flowcase-token-0123456789abcdef0123")
	p, err := c.VerifyRequest(ctx, r)
	if err != nil || p.Kind != "service" || p.Subject != "service:flowcase" || p.TenantID != testTenantA {
		t.Fatalf("service token not verified: %+v %v", p, err)
	}
	if err := c.Authorize(ctx, p, "run.invoke", "tenant:"+testTenantA); err != nil {
		t.Fatalf("granted service action denied: %v", err)
	}
	for _, action := range []string{"run.cancel", "agent.update", "release.approve"} {
		if err := c.Authorize(ctx, p, action, "tenant:"+testTenantA); err == nil {
			t.Errorf("service action %s was authorized", action)
		}
	}
	if err := c.Authorize(ctx, p, "run.invoke", "tenant:"+testTenantB); err == nil {
		t.Fatal("service token authorized another tenant")
	}

	expired := Composite{Human: dev, Service: testServiceTokens(t, now.Add(-2*time.Hour))}
	expired.Service.now = func() time.Time { return now }
	if _, err := expired.VerifyRequest(ctx, r); err == nil {
		t.Fatal("expired service token was accepted")
	}

	human := httptest.NewRequest("GET", "/api/v1/runs", nil)
	hp, err := c.VerifyRequest(ctx, human)
	if err != nil || hp.Kind == "service" {
		t.Fatalf("human request was not routed to the human adapter: %+v %v", hp, err)
	}
	if !c.IsDevelopment() || len(c.PermissionsFor(hp)) != 2 || len(c.PermissionsFor(p)) != 0 {
		t.Fatal("composite did not report development identity or permissions")
	}
}
