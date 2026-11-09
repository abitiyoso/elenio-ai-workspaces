package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elenio/case-workspace/internal/auth"
	"github.com/elenio/case-workspace/internal/testkit"
)

const member = "3f1c2b4a-1111-4a2b-9c3d-000000000003"

func setup(t *testing.T) (*testkit.Signer, *auth.Verifier) {
	t.Helper()
	s := testkit.NewSigner()
	srv := s.JWKSServer()
	t.Cleanup(srv.Close)
	v, err := auth.NewVerifier(auth.Config{Issuer: testkit.Issuer, JWKSURL: srv.URL, Audience: testkit.Audience, TenantKey: testkit.Tenant})
	if err != nil {
		t.Fatal(err)
	}
	return s, v
}

func request(token string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/workspace/me/context", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestValidToken(t *testing.T) {
	s, v := setup(t)
	id, err := v.VerifyRequest(request(s.Token(member), nil))
	if err != nil {
		t.Fatal(err)
	}
	if id.UserID != member || id.TenantKey != testkit.Tenant || id.WorkspaceKey != "sim-b2b" || id.SessionRef == "" || len(id.Scopes) != 2 {
		t.Fatalf("identity = %+v", id)
	}
}

// TestNegativeSuite mirrors control_plane/tests/contract/tokencheck: every case must be rejected as unauthenticated.
func TestNegativeSuite(t *testing.T) {
	s, v := setup(t)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	mut := func(f func(c map[string]any)) string {
		c := testkit.Claims(member)
		f(c)
		return s.Sign(c)
	}
	cases := []struct {
		name    string
		token   string
		headers map[string]string
	}{
		{name: "no token"},
		{name: "forged identity headers, no token", headers: map[string]string{"X-User-ID": member, "X-Tenant-ID": testkit.Tenant,
			"X-Role": "platform_admin", "Tailscale-User-Login": "admin@example.invalid", "X-Flowcase-Dev-Persona": "approver"}},
		{name: "wrong audience flowcase", token: mut(func(c map[string]any) { c["aud"] = "flowcase" })},
		{name: "wrong audience kms", token: mut(func(c map[string]any) { c["aud"] = "kms" })},
		{name: "wrong audience agent-studio", token: mut(func(c map[string]any) { c["aud"] = "agent-studio" })},
		{name: "wrong issuer", token: mut(func(c map[string]any) { c["iss"] = "https://evil.example" })},
		{name: "expired", token: mut(func(c map[string]any) {
			p := time.Now().Add(-20 * time.Minute)
			c["iat"], c["nbf"], c["exp"] = p.Unix(), p.Unix(), p.Add(10*time.Minute).Unix()
		})},
		{name: "nbf in future", token: mut(func(c map[string]any) { c["nbf"] = time.Now().Add(10 * time.Minute).Unix() })},
		{name: "lifetime > 15m", token: mut(func(c map[string]any) { c["exp"] = time.Now().Add(2 * time.Hour).Unix() })},
		{name: "missing exp", token: mut(func(c map[string]any) { delete(c, "exp") })},
		{name: "foreign key same kid", token: s.SignWith(testkit.Claims(member), other, s.Kid)},
		{name: "unknown kid", token: s.SignWith(testkit.Claims(member), s.Key, "nope")},
		{name: "alg none", token: s.None(testkit.Claims(member))},
		{name: "HS256 confusion", token: s.HS256(testkit.Claims(member), []byte(s.Kid))},
		{name: "other tenant claim", token: mut(func(c map[string]any) { c["tenant_id"] = "other-tenant" })},
		{name: "service principal type", token: mut(func(c map[string]any) { c["principal_type"] = "service" })},
		{name: "subject not a uuid", token: mut(func(c map[string]any) { c["sub"] = "admin" })},
		{name: "delegated act token", token: mut(func(c map[string]any) { c["act"] = map[string]any{"sub": "case-workspace"} })},
		{name: "garbage", token: "a.b.c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.VerifyRequest(request(tc.token, tc.headers))
			if !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("want ErrUnauthenticated, got %v", err)
			}
		})
	}
	t.Run("two authorization headers", func(t *testing.T) {
		r := request(s.Token(member), nil)
		r.Header.Add("Authorization", "Bearer "+s.Token(member))
		if _, err := v.VerifyRequest(r); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestKeysUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	v, _ := auth.NewVerifier(auth.Config{Issuer: testkit.Issuer, JWKSURL: srv.URL, Audience: testkit.Audience, TenantKey: testkit.Tenant})
	s := testkit.NewSigner()
	if _, err := v.Verify(context.Background(), s.Token(member)); !errors.Is(err, auth.ErrKeysUnavailable) {
		t.Fatalf("got %v", err)
	}
}
