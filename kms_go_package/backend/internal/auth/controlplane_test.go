package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "http://100.126.146.52:5200"
	testTenantID = "0d6f1c2e-1111-4222-8333-444455556666"
	testUserID   = "7b0c5d9e-2f3a-4b1c-8d6e-9a0b1c2d3e4f"
)

type fakeDirectory struct {
	mu      sync.Mutex
	records map[string]PrincipalRecord
	err     error
}

func (d *fakeDirectory) LookupPrincipal(_ context.Context, tenantID, principalID string) (PrincipalRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return PrincipalRecord{}, d.err
	}
	record, ok := d.records[tenantID+"/"+principalID]
	if !ok {
		return PrincipalRecord{}, ErrPrincipalNotProvisioned
	}
	return record, nil
}

type jwksServer struct {
	*httptest.Server
	hits   atomic.Int32
	failed atomic.Bool
	mu     sync.Mutex
	keys   []map[string]string
}

func newJWKSServer(t *testing.T, keys ...map[string]string) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if r.URL.Path != "/.well-known/jwks.json" || s.failed.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": s.keys})
	}))
	t.Cleanup(s.Close)
	return s
}

func b64Coord(n *big.Int) string {
	padded := make([]byte, 32)
	n.FillBytes(padded)
	return base64.RawURLEncoding.EncodeToString(padded)
}

func ecJWK(pub *ecdsa.PublicKey, kid string) map[string]string {
	return map[string]string{"kty": "EC", "crv": "P-256", "x": b64Coord(pub.X), "y": b64Coord(pub.Y), "kid": kid, "use": "sig", "alg": "ES256"}
}

// thumbprintKid mirrors control_plane/internal/integrations/tokens.go (RFC 7638).
func thumbprintKid(pub *ecdsa.PublicKey) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, b64Coord(pub.X), b64Coord(pub.Y))))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type harness struct {
	t         *testing.T
	key       *ecdsa.PrivateKey
	kid       string
	jwks      *jwksServer
	directory *fakeDirectory
	verifier  *ControlPlaneVerifier
	now       time.Time
	clockMu   sync.Mutex
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, key: key, kid: thumbprintKid(&key.PublicKey), now: time.Unix(1_790_000_000, 0)}
	h.jwks = newJWKSServer(t, ecJWK(&key.PublicKey, h.kid))
	h.directory = &fakeDirectory{records: map[string]PrincipalRecord{
		testTenantID + "/" + testUserID: {Status: "active", Roles: []string{"knowledge_steward"}},
	}}
	h.verifier, err = NewControlPlaneVerifier(ControlPlaneConfig{
		Issuer: testIssuer, Audience: "kms", JWKSURL: h.jwks.URL + "/.well-known/jwks.json",
		TenantMap:   map[string]string{"acme-demo": testTenantID},
		MaxTokenTTL: 15 * time.Minute, ClockSkew: 60 * time.Second, JWKSCacheTTL: 10 * time.Minute,
		Directory: h.directory, Now: h.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) clock() time.Time {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	return h.now
}

func (h *harness) advance(d time.Duration) {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	h.now = h.now.Add(d)
}

// claims matches the shape Issuer.Mint produces in the Control Plane.
func (h *harness) claims() jwt.MapClaims {
	now := h.clock()
	return jwt.MapClaims{
		"iss": testIssuer, "aud": "kms", "sub": testUserID, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(300 * time.Second).Unix(),
		"jti": "j-1", "ctx_ver": "workspace.authctx.v1", "principal_type": "human", "tenant_id": "acme-demo", "workspace_id": "acme-main",
		"scope": "apps:open", "idp_iss": "https://idp.example.test", "idp_sub": "idp-user", "sid": "s", "policy_version": "rbac-2026.10.1",
	}
}

func (h *harness) sign(claims jwt.MapClaims) string {
	return signES256(h.t, h.key, h.kid, claims)
}

func signES256(t *testing.T, key *ecdsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func bearerRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func (h *harness) verify(r *http.Request) (Identity, error) {
	return h.verifier.Verify(context.Background(), r)
}

func requireRejected(t *testing.T, err error, reason string) {
	t.Helper()
	var rejection *TokenRejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected token rejection %q, got %v", reason, err)
	}
	if errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("rejection must not be reported as unavailability: %v", err)
	}
	if reason != "" && rejection.Reason != reason {
		t.Fatalf("rejection reason = %q, want %q", rejection.Reason, reason)
	}
}

func TestControlPlaneVerifierAcceptsValidToken(t *testing.T) {
	h := newHarness(t)
	identity, err := h.verify(bearerRequest(h.sign(h.claims())))
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if identity.UserID != testUserID || identity.TenantID != testTenantID || !slices.Equal(identity.Roles, []string{"knowledge_steward"}) {
		t.Fatalf("identity = %+v", identity)
	}
	adapted, err := NewTrustedSessionAdapter(h.verifier).Authenticate(context.Background(), bearerRequest(h.sign(h.claims())))
	if err != nil || adapted.UserID != testUserID {
		t.Fatalf("adapter identity = %+v, err = %v", adapted, err)
	}
}

func TestControlPlaneVerifierAcceptsAudienceArrayAndUppercaseSubject(t *testing.T) {
	h := newHarness(t)
	claims := h.claims()
	claims["aud"] = []string{"other-app", "kms"}
	claims["sub"] = strings.ToUpper(testUserID)
	identity, err := h.verify(bearerRequest(h.sign(claims)))
	if err != nil || identity.UserID != testUserID {
		t.Fatalf("identity = %+v, err = %v", identity, err)
	}
}

func TestControlPlaneVerifierRolesComeFromDirectoryNotToken(t *testing.T) {
	h := newHarness(t)
	claims := h.claims()
	claims["roles"] = []string{"platform_admin"}
	claims["role"] = "platform_admin"
	identity, err := h.verify(bearerRequest(h.sign(claims)))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(identity.Roles, []string{"knowledge_steward"}) {
		t.Fatalf("roles = %v, want only the directory roles", identity.Roles)
	}
	h.directory.records[testTenantID+"/"+testUserID] = PrincipalRecord{Status: "active"}
	identity, err = h.verify(bearerRequest(h.sign(claims)))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Roles == nil || len(identity.Roles) != 0 {
		t.Fatalf("roles = %#v, want an empty non-nil slice", identity.Roles)
	}
}

func TestControlPlaneVerifierRejectsInvalidClaims(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(h *harness, c jwt.MapClaims)
		reason string
	}{
		{"wrong audience", func(_ *harness, c jwt.MapClaims) { c["aud"] = "flowcase" }, "audience_mismatch"},
		{"missing audience", func(_ *harness, c jwt.MapClaims) { delete(c, "aud") }, "required_claim_missing"},
		{"wrong issuer", func(_ *harness, c jwt.MapClaims) { c["iss"] = "http://100.126.146.52:5201" }, "issuer_mismatch"},
		{"issuer with trailing slash", func(_ *harness, c jwt.MapClaims) { c["iss"] = testIssuer + "/" }, "issuer_mismatch"},
		{"expired", func(h *harness, c jwt.MapClaims) {
			c["iat"], c["nbf"], c["exp"] = h.clock().Add(-10*time.Minute).Unix(), h.clock().Add(-10*time.Minute).Unix(), h.clock().Add(-61*time.Second).Unix()
		}, "expired"},
		{"nbf in future", func(h *harness, c jwt.MapClaims) { c["nbf"] = h.clock().Add(2 * time.Minute).Unix() }, "not_yet_valid"},
		{"iat in future", func(h *harness, c jwt.MapClaims) {
			c["iat"], c["exp"] = h.clock().Add(2*time.Minute).Unix(), h.clock().Add(5*time.Minute).Unix()
		}, "issued_in_future"},
		{"ttl too long", func(h *harness, c jwt.MapClaims) { c["exp"] = h.clock().Add(16 * time.Minute).Unix() }, "lifetime_exceeds_max"},
		{"exp before iat", func(h *harness, c jwt.MapClaims) {
			c["iat"], c["exp"] = h.clock().Unix(), h.clock().Add(-1*time.Second).Unix()
		}, ""},
		{"missing exp", func(_ *harness, c jwt.MapClaims) { delete(c, "exp") }, "required_claim_missing"},
		{"missing iat", func(_ *harness, c jwt.MapClaims) { delete(c, "iat") }, "iat_missing"},
		{"non-uuid sub", func(_ *harness, c jwt.MapClaims) { c["sub"] = "alice@example.com" }, "subject_not_uuid"},
		{"service principal", func(_ *harness, c jwt.MapClaims) { c["principal_type"] = "service" }, "principal_type_not_human"},
		{"missing principal type", func(_ *harness, c jwt.MapClaims) { delete(c, "principal_type") }, "principal_type_not_human"},
		{"unmapped tenant", func(_ *harness, c jwt.MapClaims) { c["tenant_id"] = "other-tenant" }, "tenant_not_mapped"},
		{"tenant uuid instead of key", func(_ *harness, c jwt.MapClaims) { c["tenant_id"] = testTenantID }, "tenant_not_mapped"},
		{"principal not provisioned", func(_ *harness, c jwt.MapClaims) { c["sub"] = "11111111-2222-4333-8444-555555555555" }, "principal_not_provisioned"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			claims := h.claims()
			tc.mutate(h, claims)
			_, err := h.verify(bearerRequest(h.sign(claims)))
			requireRejected(t, err, tc.reason)
			if _, err := NewTrustedSessionAdapter(h.verifier).Authenticate(context.Background(), bearerRequest(h.sign(claims))); err != ErrUnauthenticated {
				t.Fatalf("adapter err = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestControlPlaneVerifierHonoursClockSkew(t *testing.T) {
	h := newHarness(t)
	claims := h.claims()
	claims["nbf"] = h.clock().Add(30 * time.Second).Unix()
	claims["exp"] = h.clock().Add(-30 * time.Second).Unix()
	claims["iat"] = h.clock().Add(-4 * time.Minute).Unix()
	if _, err := h.verify(bearerRequest(h.sign(claims))); err != nil {
		t.Fatalf("token within 60s skew rejected: %v", err)
	}
}

func TestControlPlaneVerifierRejectsDisabledPrincipal(t *testing.T) {
	h := newHarness(t)
	h.directory.records[testTenantID+"/"+testUserID] = PrincipalRecord{Status: "disabled", Roles: []string{"platform_admin"}}
	_, err := h.verify(bearerRequest(h.sign(h.claims())))
	requireRejected(t, err, "principal_disabled")
}

func TestControlPlaneVerifierRejectsAlgNone(t *testing.T) {
	h := newHarness(t)
	header := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"alg":"none","typ":"JWT","kid":%q}`, h.kid)))
	payload, _ := json.Marshal(h.claims())
	token := header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
	_, err := h.verify(bearerRequest(token))
	requireRejected(t, err, "")
	unsafe, err := jwt.NewWithClaims(jwt.SigningMethodNone, h.claims()).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.verify(bearerRequest(unsafe))
	requireRejected(t, err, "")
}

func TestControlPlaneVerifierRejectsHS256(t *testing.T) {
	h := newHarness(t)
	publicDER, err := x509.MarshalPKIXPublicKey(&h.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string][]byte{
		"random secret":                   []byte("an-attacker-chosen-shared-secret-value"),
		"public key bytes (confusion)":    publicDER,
		"jwk x coordinate (confusion)":    []byte(b64Coord(h.key.PublicKey.X)),
		"empty-ish secret with valid kid": []byte("x"),
	} {
		t.Run(name, func(t *testing.T) {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, h.claims())
			token.Header["kid"] = h.kid
			signed, err := token.SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.verify(bearerRequest(signed))
			requireRejected(t, err, "")
		})
	}
}

func TestControlPlaneVerifierRejectsUnknownKid(t *testing.T) {
	h := newHarness(t)
	_, err := h.verify(bearerRequest(signES256(t, h.key, "not-a-published-kid", h.claims())))
	requireRejected(t, err, "unknown_kid")
	token := jwt.NewWithClaims(jwt.SigningMethodES256, h.claims())
	signed, _ := token.SignedString(h.key)
	_, err = h.verify(bearerRequest(signed))
	requireRejected(t, err, "unknown_kid")
}

func TestControlPlaneVerifierRejectsForeignKeyWithSameKid(t *testing.T) {
	h := newHarness(t)
	foreign, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.verify(bearerRequest(signES256(t, foreign, h.kid, h.claims())))
	requireRejected(t, err, "signature_invalid")
}

func TestControlPlaneVerifierRequiresExactlyOneBearerHeader(t *testing.T) {
	h := newHarness(t)
	valid := h.sign(h.claims())

	missing := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	_, err := h.verify(missing)
	requireRejected(t, err, "authorization_missing")

	double := bearerRequest(valid)
	double.Header.Add("Authorization", "Bearer "+valid)
	_, err = h.verify(double)
	requireRejected(t, err, "authorization_repeated")

	basic := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	basic.Header.Set("Authorization", "Basic "+valid)
	_, err = h.verify(basic)
	requireRejected(t, err, "authorization_not_bearer")

	extra := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	extra.Header.Set("Authorization", "Bearer "+valid+" "+valid)
	_, err = h.verify(extra)
	requireRejected(t, err, "authorization_not_bearer")

	serviceCredential := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	serviceCredential.Header.Set("Authorization", "Bearer kms_svc_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG")
	_, err = h.verify(serviceCredential)
	requireRejected(t, err, "token_not_compact_jws")

	oversized := h.claims()
	oversized["padding"] = strings.Repeat("a", maxBearerTokenBytes)
	_, err = h.verify(bearerRequest(h.sign(oversized)))
	requireRejected(t, err, "token_too_large")
}

func TestControlPlaneVerifierIgnoresForgedHeadersCookiesAndQuery(t *testing.T) {
	h := newHarness(t)
	valid := h.sign(h.claims())
	r := httptest.NewRequest(http.MethodGet, "/api/v1/session?access_token="+valid+"&token="+valid, nil)
	r.Header.Set("X-User-ID", testUserID)
	r.Header.Set("X-Tenant-ID", testTenantID)
	r.Header.Set("X-Roles", "platform_admin")
	r.Header.Set("X-Forwarded-Authorization", "Bearer "+valid)
	r.AddCookie(&http.Cookie{Name: "token", Value: valid})
	r.AddCookie(&http.Cookie{Name: "Authorization", Value: "Bearer " + valid})
	_, err := h.verify(r)
	requireRejected(t, err, "authorization_missing")
	if _, err := NewTrustedSessionAdapter(h.verifier).Authenticate(context.Background(), r); err != ErrUnauthenticated {
		t.Fatalf("adapter err = %v", err)
	}
}

func TestControlPlaneVerifierReportsJWKSFailureAsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.jwks.failed.Store(true)
	_, err := h.verify(bearerRequest(h.sign(h.claims())))
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("err = %v, want ErrVerifierUnavailable", err)
	}
	var rejection *TokenRejection
	if errors.As(err, &rejection) {
		t.Fatalf("JWKS outage must not be reported as a bad token: %v", err)
	}
	if _, err := NewTrustedSessionAdapter(h.verifier).Authenticate(context.Background(), bearerRequest(h.sign(h.claims()))); err != ErrVerifierUnavailable {
		t.Fatalf("adapter err = %v, want ErrVerifierUnavailable", err)
	}
	// Recovery after the failure backoff.
	h.jwks.failed.Store(false)
	h.advance(jwksFailureBackoff)
	if _, err := h.verify(bearerRequest(h.sign(h.claims()))); err != nil {
		t.Fatalf("verifier did not recover after JWKS came back: %v", err)
	}
}

func TestControlPlaneVerifierDoesNotServeExpiredKeysWhenRefreshFails(t *testing.T) {
	h := newHarness(t)
	if _, err := h.verify(bearerRequest(h.sign(h.claims()))); err != nil {
		t.Fatal(err)
	}
	h.jwks.failed.Store(true)
	h.advance(10*time.Minute + time.Second)
	if _, err := h.verify(bearerRequest(h.sign(h.claims()))); !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("err = %v, want ErrVerifierUnavailable", err)
	}
}

func TestControlPlaneVerifierReportsDirectoryFailureAsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.directory.err = errors.New("connection refused")
	_, err := h.verify(bearerRequest(h.sign(h.claims())))
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("err = %v, want ErrVerifierUnavailable", err)
	}
}

func TestControlPlaneVerifierRateLimitsUnknownKidRefetch(t *testing.T) {
	h := newHarness(t)
	if _, err := h.verify(bearerRequest(h.sign(h.claims()))); err != nil {
		t.Fatal(err)
	}
	if hits := h.jwks.hits.Load(); hits != 1 {
		t.Fatalf("initial JWKS hits = %d", hits)
	}
	unknown := bearerRequest(signES256(t, h.key, "rotated-kid", h.claims()))
	for i := 0; i < 5; i++ {
		_, err := h.verify(unknown)
		requireRejected(t, err, "unknown_kid")
	}
	// The first fetch happened at the same instant, so the 30s gap suppresses every refetch.
	if hits := h.jwks.hits.Load(); hits != 1 {
		t.Fatalf("JWKS hits within 30s = %d, want 1", hits)
	}
	h.advance(unknownKidRefetchGap)
	_, err := h.verify(unknown)
	requireRejected(t, err, "unknown_kid")
	_, _ = h.verify(unknown)
	if hits := h.jwks.hits.Load(); hits != 2 {
		t.Fatalf("JWKS hits after 30s = %d, want 2", hits)
	}
}

func TestControlPlaneVerifierPicksUpRotatedKey(t *testing.T) {
	h := newHarness(t)
	if _, err := h.verify(bearerRequest(h.sign(h.claims()))); err != nil {
		t.Fatal(err)
	}
	next, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nextKid := thumbprintKid(&next.PublicKey)
	h.jwks.mu.Lock()
	h.jwks.keys = append(h.jwks.keys, ecJWK(&next.PublicKey, nextKid))
	h.jwks.mu.Unlock()
	h.advance(unknownKidRefetchGap)
	if _, err := h.verify(bearerRequest(signES256(t, next, nextKid, h.claims()))); err != nil {
		t.Fatalf("rotated key rejected: %v", err)
	}
}

func TestControlPlaneVerifierAcceptsRS256AndRejectsAlgKeyMismatch(t *testing.T) {
	h := newHarness(t)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	h.jwks.mu.Lock()
	h.jwks.keys = append(h.jwks.keys, map[string]string{
		"kty": "RSA", "kid": "rsa-1", "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(rsaKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaKey.E)).Bytes()),
	})
	h.jwks.mu.Unlock()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, h.claims())
	token.Header["kid"] = "rsa-1"
	signed, err := token.SignedString(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.verify(bearerRequest(signed)); err != nil {
		t.Fatalf("RS256 token rejected: %v", err)
	}
	// An ES256 token pointing at the RSA kid must not verify.
	_, err = h.verify(bearerRequest(signES256(t, h.key, "rsa-1", h.claims())))
	requireRejected(t, err, "")
	// RS512 is outside the allow-list.
	token = jwt.NewWithClaims(jwt.SigningMethodRS512, h.claims())
	token.Header["kid"] = "rsa-1"
	signed, _ = token.SignedString(rsaKey)
	_, err = h.verify(bearerRequest(signed))
	requireRejected(t, err, "")
}

func TestParseJWKRejectsOffCurveAndWeakKeys(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	good := ecJWK(&key.PublicKey, "k")
	jwk := jsonWebKey{Kty: "EC", Crv: "P-256", X: good["x"], Y: good["y"], Kid: "k"}
	if _, _, ok := parseJWK(jwk); !ok {
		t.Fatal("valid EC key rejected")
	}
	jwk.Y = b64Coord(new(big.Int).Add(key.PublicKey.Y, big.NewInt(1)))
	if _, _, ok := parseJWK(jwk); ok {
		t.Fatal("off-curve EC key accepted")
	}
	weak, _ := rsa.GenerateKey(rand.Reader, 1024)
	if _, _, ok := parseJWK(jsonWebKey{Kty: "RSA", Kid: "w", N: base64.RawURLEncoding.EncodeToString(weak.N.Bytes()), E: "AQAB"}); ok {
		t.Fatal("1024-bit RSA key accepted")
	}
}

func TestBearerTokenNeverReadsTokenContentsIntoReason(t *testing.T) {
	h := newHarness(t)
	claims := h.claims()
	claims["sub"] = "secret-subject-value"
	_, err := h.verify(bearerRequest(h.sign(claims)))
	if err == nil || strings.Contains(err.Error(), "secret-subject-value") {
		t.Fatalf("error leaks token contents: %v", err)
	}
}
