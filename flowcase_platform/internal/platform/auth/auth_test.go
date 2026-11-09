package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "https://host.example"
	testAudience = "flowcase"
)

var fixedNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

type keyFixture struct {
	rsaKey *rsa.PrivateKey
	ecKey  *ecdsa.PrivateKey
	server *httptest.Server
	hits   atomic.Int32
}

func newKeyFixture(t *testing.T) *keyFixture {
	t.Helper()
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ek, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &keyFixture{rsaKey: rk, ecKey: ek}
	enc := base64.RawURLEncoding.EncodeToString
	pad := func(b *big.Int) string { out := make([]byte, 32); b.FillBytes(out); return enc(out) }
	jwks := map[string]any{"keys": []map[string]string{
		{"kty": "RSA", "kid": "rsa-1", "use": "sig", "alg": "RS256", "n": enc(rk.N.Bytes()), "e": enc(big.NewInt(int64(rk.E)).Bytes())},
		{"kty": "EC", "kid": "ec-1", "use": "sig", "crv": "P-256", "x": pad(ek.X), "y": pad(ek.Y)},
		{"kty": "oct", "kid": "sym", "k": enc([]byte("shared-secret"))},
	}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.hits.Add(1)
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func baseClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss": testIssuer, "aud": testAudience, "sub": "user-123", "tenant_id": "acme",
		"iat": fixedNow.Add(-time.Minute).Unix(), "exp": fixedNow.Add(4 * time.Minute).Unix(),
	}
}

func (f *keyFixture) sign(t *testing.T, method jwt.SigningMethod, kid string, claims jwt.MapClaims, key any) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *keyFixture) verifier() *JWTVerifier {
	cache := NewJWKSCache(f.server.URL, 10*time.Minute)
	cache.Now = func() time.Time { return fixedNow }
	return NewJWTVerifier(JWTVerifierConfig{
		Issuer: testIssuer, Audience: testAudience, TenantClaim: "tenant_id",
		ClockSkew: 30 * time.Second, MaxTokenTTL: 15 * time.Minute, Keys: cache,
		Now: func() time.Time { return fixedNow },
	})
}

type fakeDirectory struct{ members map[string]Identity }

func (d fakeDirectory) Resolve(_ context.Context, c Claims) (Identity, error) {
	id, ok := d.members[c.TenantRef+"/"+c.Subject]
	if !ok {
		return Identity{}, unauthenticated("not_a_member")
	}
	return id, nil
}

func memberDirectory() fakeDirectory {
	return fakeDirectory{members: map[string]Identity{
		"acme/user-123": {UserID: "u-1", TenantID: "t-1", DisplayName: "Ada", Roles: []Role{RoleWorkspaceMember}},
	}}
}

func request(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestTrustedAdapterAcceptsValidTokens(t *testing.T) {
	f := newKeyFixture(t)
	adapter := NewTrustedAdapter(f.verifier(), memberDirectory())
	for name, tok := range map[string]string{
		"RS256": f.sign(t, jwt.SigningMethodRS256, "rsa-1", baseClaims(), f.rsaKey),
		"ES256": f.sign(t, jwt.SigningMethodES256, "ec-1", baseClaims(), f.ecKey),
	} {
		t.Run(name, func(t *testing.T) {
			id, err := adapter.Authenticate(context.Background(), request(tok))
			if err != nil {
				t.Fatalf("expected success, got %v (%s)", err, Reason(err))
			}
			if id.UserID != "u-1" || id.TenantID != "t-1" || id.AuthMode != "trusted_jwt" || id.Subject != "user-123" {
				t.Fatalf("unexpected identity %+v", id)
			}
		})
	}
}

func TestTrustedAdapterMarksOnlyListedHumansSoDExempt(t *testing.T) {
	f := newKeyFixture(t)
	tok := f.sign(t, jwt.SigningMethodES256, "ec-1", baseClaims(), f.ecKey)
	human := fakeDirectory{members: map[string]Identity{"acme/user-123": {UserID: "u-1", TenantID: "t-1", Kind: KindHuman}}}
	service := fakeDirectory{members: map[string]Identity{"acme/user-123": {UserID: "u-1", TenantID: "t-1", Kind: KindService}}}
	for name, c := range map[string]struct {
		dir    Directory
		exempt []string
		want   bool
	}{
		"listed human":   {human, []string{"user-123"}, true},
		"unlisted human": {human, []string{"someone-else"}, false},
		"no list":        {human, nil, false},
		"listed service": {service, []string{"user-123"}, false},
	} {
		id, err := NewTrustedAdapter(f.verifier(), c.dir, c.exempt...).Authenticate(context.Background(), request(tok))
		if err != nil || id.SoDExempt != c.want {
			t.Errorf("%s: SoDExempt = %v, want %v (%v)", name, id.SoDExempt, c.want, err)
		}
	}
}

func TestTrustedAdapterRejectsInvalidTokens(t *testing.T) {
	f := newKeyFixture(t)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	with := func(k string, v any) jwt.MapClaims { c := baseClaims(); c[k] = v; return c }
	without := func(k string) jwt.MapClaims { c := baseClaims(); delete(c, k); return c }

	cases := map[string]struct {
		token  string
		reason string
	}{
		"expired":             {f.sign(t, jwt.SigningMethodRS256, "rsa-1", with("exp", fixedNow.Add(-2*time.Minute).Unix()), f.rsaKey), "token_expired"},
		"not yet valid":       {f.sign(t, jwt.SigningMethodRS256, "rsa-1", with("nbf", fixedNow.Add(5*time.Minute).Unix()), f.rsaKey), "token_not_yet_valid"},
		"issued in future":    {f.sign(t, jwt.SigningMethodRS256, "rsa-1", with("iat", fixedNow.Add(5*time.Minute).Unix()), f.rsaKey), "token_not_yet_valid"},
		"wrong audience":      {f.sign(t, jwt.SigningMethodRS256, "rsa-1", with("aud", "kms"), f.rsaKey), "invalid_audience"},
		"wrong issuer":        {f.sign(t, jwt.SigningMethodRS256, "rsa-1", with("iss", "https://evil.example"), f.rsaKey), "invalid_issuer"},
		"missing exp":         {f.sign(t, jwt.SigningMethodRS256, "rsa-1", without("exp"), f.rsaKey), "missing_required_claim"},
		"missing iat":         {f.sign(t, jwt.SigningMethodRS256, "rsa-1", without("iat"), f.rsaKey), "missing_lifetime_claims"},
		"lifetime too long":   {f.sign(t, jwt.SigningMethodRS256, "rsa-1", with("exp", fixedNow.Add(2*time.Hour).Unix()), f.rsaKey), "token_lifetime_too_long"},
		"missing tenant":      {f.sign(t, jwt.SigningMethodRS256, "rsa-1", without("tenant_id"), f.rsaKey), "invalid_tenant_claim"},
		"missing subject":     {f.sign(t, jwt.SigningMethodRS256, "rsa-1", without("sub"), f.rsaKey), "invalid_subject"},
		"forged signature":    {f.sign(t, jwt.SigningMethodRS256, "rsa-1", baseClaims(), otherKey), "invalid_signature"},
		"unknown kid":         {f.sign(t, jwt.SigningMethodRS256, "rsa-9", baseClaims(), f.rsaKey), "unverifiable_token"},
		"missing kid":         {f.sign(t, jwt.SigningMethodRS256, "", baseClaims(), f.rsaKey), "unverifiable_token"},
		"alg none":            {f.sign(t, jwt.SigningMethodNone, "rsa-1", baseClaims(), jwt.UnsafeAllowNoneSignatureType), "malformed_token"},
		"HS256 alg confusion": {f.sign(t, jwt.SigningMethodHS256, "sym", baseClaims(), []byte("shared-secret")), "invalid_signature"},
		"not a member":        {f.sign(t, jwt.SigningMethodRS256, "rsa-1", with("tenant_id", "other-tenant"), f.rsaKey), "not_a_member"},
		"malformed":           {"not-a-jwt", "malformed_token"},
	}
	adapter := NewTrustedAdapter(f.verifier(), memberDirectory())
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := adapter.Authenticate(context.Background(), request(tc.token))
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("expected ErrUnauthenticated, got %v", err)
			}
			if Reason(err) != tc.reason {
				t.Fatalf("expected reason %q, got %q", tc.reason, Reason(err))
			}
		})
	}
}

func TestBearerTokenOnlyFromSingleAuthorizationHeader(t *testing.T) {
	f := newKeyFixture(t)
	valid := f.sign(t, jwt.SigningMethodRS256, "rsa-1", baseClaims(), f.rsaKey)
	adapter := NewTrustedAdapter(f.verifier(), memberDirectory())

	inQuery := httptest.NewRequest(http.MethodGet, "/api/v1/session?access_token="+valid, nil)
	inCookie := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	inCookie.AddCookie(&http.Cookie{Name: "access_token", Value: valid})
	twoHeaders := request(valid)
	twoHeaders.Header.Add("Authorization", "Bearer "+valid)
	basic := httptest.NewRequest(http.MethodGet, "/", nil)
	basic.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	forgedHeaders := httptest.NewRequest(http.MethodGet, "/", nil)
	forgedHeaders.Header.Set("X-User-ID", "u-1")
	forgedHeaders.Header.Set("X-Tenant-ID", "t-1")

	for name, r := range map[string]*http.Request{"query": inQuery, "cookie": inCookie, "two headers": twoHeaders, "basic": basic, "forged identity headers": forgedHeaders} {
		t.Run(name, func(t *testing.T) {
			if _, err := adapter.Authenticate(context.Background(), r); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("expected rejection, got %v", err)
			}
		})
	}
}

func TestUnreachableJWKSFailsClosedAsMisconfigured(t *testing.T) {
	f := newKeyFixture(t)
	tok := f.sign(t, jwt.SigningMethodRS256, "rsa-1", baseClaims(), f.rsaKey)
	cache := NewJWKSCache("http://127.0.0.1:1/jwks", time.Minute)
	v := NewJWTVerifier(JWTVerifierConfig{Issuer: testIssuer, Audience: testAudience, TenantClaim: "tenant_id", MaxTokenTTL: 15 * time.Minute, Keys: cache, Now: func() time.Time { return fixedNow }})
	_, err := NewTrustedAdapter(v, memberDirectory()).Authenticate(context.Background(), request(tok))
	if !errors.Is(err, ErrMisconfigured) {
		t.Fatalf("expected ErrMisconfigured, got %v", err)
	}
}

func TestUnknownKidRefreshIsRateLimited(t *testing.T) {
	f := newKeyFixture(t)
	v := f.verifier()
	tok := f.sign(t, jwt.SigningMethodRS256, "rsa-404", baseClaims(), f.rsaKey)
	for range 5 {
		_, _ = v.Verify(context.Background(), tok)
	}
	if hits := f.hits.Load(); hits != 1 {
		t.Fatalf("expected a single JWKS fetch within the refresh interval, got %d", hits)
	}
}

func TestParseJWKSRejectsWeakAndSymmetricKeys(t *testing.T) {
	weak, _ := rsa.GenerateKey(rand.Reader, 1024)
	enc := base64.RawURLEncoding.EncodeToString
	data, _ := json.Marshal(map[string]any{"keys": []map[string]string{
		{"kty": "RSA", "kid": "weak", "n": enc(weak.N.Bytes()), "e": enc(big.NewInt(int64(weak.E)).Bytes())},
		{"kty": "oct", "kid": "sym", "k": enc([]byte("x"))},
		{"kty": "EC", "kid": "off-curve", "crv": "P-256", "x": enc(make([]byte, 32)), "y": enc(make([]byte, 32))},
	}})
	keys, err := ParseJWKS(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no usable keys, got %v", keys)
	}
}

func TestDevMockIgnoresRequestSuppliedIdentity(t *testing.T) {
	adapter := NewDevMockAdapter("u-dev", "t-dev", "Local Developer", []Role{RoleWorkspaceMember})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-User-ID", "attacker")
	r.Header.Set("X-Tenant-ID", "victim-tenant")
	r.Header.Set("X-Roles", "tenant_admin")
	id, err := adapter.Authenticate(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if id.UserID != "u-dev" || id.TenantID != "t-dev" || id.HasRole(RoleTenantAdmin) || id.AuthMode != "dev_mock" {
		t.Fatalf("dev mock must return its fixed identity, got %+v", id)
	}
	id.Roles[0] = RoleTenantAdmin
	again, _ := adapter.Authenticate(context.Background(), r)
	if again.HasRole(RoleTenantAdmin) {
		t.Fatal("identity roles must not be shared mutable state")
	}
}

func TestDevPersonasComeFromServerConfigOnly(t *testing.T) {
	adapter := NewDevMockAdapter("u-dev", "t-dev", "Dev", []Role{RoleCaseOwner},
		DevPersona{Key: "approver", UserID: "u-appr", DisplayName: "Approver", Roles: []Role{RoleDomainApprover}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(DevPersonaHeader, "approver")
	r.Header.Set("X-Roles", "tenant_admin")
	id, err := adapter.Authenticate(context.Background(), r)
	if err != nil || id.UserID != "u-appr" || id.TenantID != "t-dev" || !id.HasRole(RoleDomainApprover) || id.HasRole(RoleCaseOwner) || id.Persona != "approver" {
		t.Fatalf("persona must map to its configured identity: %+v %v", id, err)
	}
	r.Header.Set(DevPersonaHeader, "tenant-admin")
	if _, err := adapter.Authenticate(context.Background(), r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unknown persona must be rejected, got %v", err)
	}
	if got := adapter.(interface{ Personas() []DevPersona }).Personas(); len(got) != 2 || got[0].Key != "default" {
		t.Fatalf("persona list: %+v", got)
	}
}
