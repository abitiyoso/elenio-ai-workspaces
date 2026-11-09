package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elenio/agent-studio/internal/ports"
)

const (
	testCPIssuer   = "http://100.126.146.52:5200"
	testCPUser     = "0b7e2a4c-91d3-4f5e-8a6b-1c2d3e4f5a6b"
	testCPOther    = "6f1d2c3b-4a59-4e8d-9c7b-a1b2c3d4e5f6"
	testCPUnmapped = "11111111-2222-4333-8444-555555555555"
)

type jwksServer struct {
	*httptest.Server
	mu   sync.Mutex
	keys []map[string]any
	hits int
}

func newJWKSServer(t *testing.T, keys ...map[string]any) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": s.keys})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) setKeys(keys ...map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = keys
}

func (s *jwksServer) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func pad32(n *big.Int) []byte {
	out := make([]byte, 32)
	n.FillBytes(out)
	return out
}

// ecThumbprint mirrors the Control Plane's RFC 7638 kid.
func ecThumbprint(pub *ecdsa.PublicKey) string {
	j := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, b64url(pad32(pub.X)), b64url(pad32(pub.Y)))
	sum := sha256.Sum256([]byte(j))
	return b64url(sum[:])
}

func ecJWK(pub *ecdsa.PublicKey, kid string) map[string]any {
	return map[string]any{"kty": "EC", "crv": "P-256", "x": b64url(pad32(pub.X)), "y": b64url(pad32(pub.Y)), "kid": kid, "use": "sig", "alg": "ES256"}
}

func newECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func signingInput(t *testing.T, header, claims map[string]any) string {
	t.Helper()
	h, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	c, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return b64url(h) + "." + b64url(c)
}

func signES256(t *testing.T, key *ecdsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	input := signingInput(t, map[string]any{"alg": "ES256", "typ": "JWT", "kid": kid}, claims)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + b64url(append(pad32(r), pad32(s)...))
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss": testCPIssuer, "aud": "agent-studio", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(300 * time.Second).Unix(),
		"jti": "jti-1", "sub": testCPUser, "tenant_id": "acme-demo", "workspace_id": "default", "principal_type": "human",
		"scope": "apps:open", "idp_iss": "https://idp.example", "idp_sub": "idp-user-1", "sid": "sid-1",
	}
}

type cpFixture struct {
	key      *ecdsa.PrivateKey
	kid      string
	server   *jwksServer
	verifier *ControlPlaneJWT
	now      time.Time
	clock    *time.Time
}

func newCPFixture(t *testing.T) *cpFixture {
	t.Helper()
	key := newECKey(t)
	kid := ecThumbprint(&key.PublicKey)
	server := newJWKSServer(t, ecJWK(&key.PublicKey, kid))
	now := time.Unix(1_800_000_000, 0)
	clock := now
	verifier, err := NewControlPlaneJWT(ControlPlaneConfig{
		Issuer: testCPIssuer, JWKSURL: server.URL + "/.well-known/jwks.json", TenantKey: "acme-demo", HTTPClient: server.Client(),
		Now: func() time.Time { return clock },
	}, []AccessGrant{
		{Login: testCPUser, TenantID: testTenantA, Permissions: []string{"agent.read", "tool.read"}},
		{Login: strings.ToUpper(testCPOther), TenantID: testTenantB, Permissions: []string{"agent.read", "agent.create"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &cpFixture{key: key, kid: kid, server: server, verifier: verifier, now: now, clock: &clock}
}

func bearerRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestControlPlaneJWTAcceptsValidToken(t *testing.T) {
	f := newCPFixture(t)
	r := bearerRequest(signES256(t, f.key, f.kid, validClaims(f.now)))
	r.Header.Set("Tailscale-User-Login", "operator@example.com")
	r.Header.Set("X-Tenant-ID", testTenantB)
	p, err := f.verifier.VerifyRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	want := ports.Principal{Issuer: testCPIssuer, Subject: testCPUser, TenantID: testTenantA, Kind: "user"}
	if p != want {
		t.Fatalf("principal = %+v, want %+v", p, want)
	}

	// Through the composite, the JWS is routed to the human adapter, never to service tokens.
	c := Composite{Human: f.verifier, Service: testServiceTokens(t, f.now)}
	cp, err := c.VerifyRequest(context.Background(), bearerRequest(signES256(t, f.key, f.kid, validClaims(f.now))))
	if err != nil || cp != want {
		t.Fatalf("composite principal = %+v err=%v", cp, err)
	}
	if c.IdentityMode() != "control-plane" || c.IsDevelopment() {
		t.Fatalf("composite mode = %q", c.IdentityMode())
	}
	if perms := c.PermissionsFor(cp); len(perms) != 2 || perms[0] != "agent.read" {
		t.Fatalf("permissions = %v", perms)
	}

	// An audience array that contains agent-studio and a fractional time are accepted.
	claims := validClaims(f.now)
	claims["aud"] = []string{"other-app", "agent-studio"}
	claims["iat"] = float64(f.now.Unix()) + 0.5
	if _, err := f.verifier.VerifyRequest(context.Background(), bearerRequest(signES256(t, f.key, f.kid, claims))); err != nil {
		t.Fatalf("audience array rejected: %v", err)
	}
	// Within the 60s clock skew.
	claims = validClaims(f.now.Add(30 * time.Second))
	if _, err := f.verifier.VerifyRequest(context.Background(), bearerRequest(signES256(t, f.key, f.kid, claims))); err != nil {
		t.Fatalf("token within skew rejected: %v", err)
	}
}

func TestControlPlaneJWTAcceptsRS256(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := newCPFixture(t)
	f.server.setKeys(map[string]any{"kty": "RSA", "kid": "rsa-1", "use": "sig", "alg": "RS256",
		"n": b64url(rsaKey.N.Bytes()), "e": b64url(big.NewInt(int64(rsaKey.E)).Bytes())})
	input := signingInput(t, map[string]any{"alg": "RS256", "kid": "rsa-1"}, validClaims(f.now))
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.verifier.VerifyRequest(context.Background(), bearerRequest(input+"."+b64url(sig))); err != nil {
		t.Fatalf("RS256 token rejected: %v", err)
	}
	// An ES256 header must not be verified against the RSA key.
	if _, err := f.verifier.VerifyRequest(context.Background(), bearerRequest(signES256(t, f.key, "rsa-1", validClaims(f.now)))); err == nil {
		t.Fatal("ES256 token was accepted against an RSA key")
	}
}

func TestControlPlaneJWTRejectsInvalidTokens(t *testing.T) {
	f := newCPFixture(t)
	foreign := newECKey(t)
	mutate := func(change func(map[string]any)) string {
		claims := validClaims(f.now)
		change(claims)
		return signES256(t, f.key, f.kid, claims)
	}
	noneInput := signingInput(t, map[string]any{"alg": "none", "kid": f.kid}, validClaims(f.now))
	hsInput := signingInput(t, map[string]any{"alg": "HS256", "kid": f.kid}, validClaims(f.now))
	mac := hmac.New(sha256.New, []byte(ecJWK(&f.key.PublicKey, f.kid)["x"].(string)))
	mac.Write([]byte(hsInput))
	critInput := signingInput(t, map[string]any{"alg": "ES256", "kid": f.kid, "crit": []string{"exp"}}, validClaims(f.now))
	critDigest := sha256.Sum256([]byte(critInput))
	cr, cs, _ := ecdsa.Sign(rand.Reader, f.key, critDigest[:])

	cases := map[string]string{
		"wrong aud":          mutate(func(c map[string]any) { c["aud"] = "flowcase" }),
		"wrong aud array":    mutate(func(c map[string]any) { c["aud"] = []string{"flowcase", "kms"} }),
		"wrong iss":          mutate(func(c map[string]any) { c["iss"] = "http://100.126.146.52:5201" }),
		"iss trailing slash": mutate(func(c map[string]any) { c["iss"] = testCPIssuer + "/" }),
		"expired": mutate(func(c map[string]any) {
			c["iat"], c["nbf"], c["exp"] = f.now.Add(-400*time.Second).Unix(), f.now.Add(-400*time.Second).Unix(), f.now.Add(-61*time.Second).Unix()
		}),
		"nbf in future": mutate(func(c map[string]any) { c["nbf"] = f.now.Add(2 * time.Minute).Unix() }),
		"iat in future": mutate(func(c map[string]any) {
			c["iat"], c["nbf"], c["exp"] = f.now.Add(2*time.Minute).Unix(), f.now.Unix(), f.now.Add(5*time.Minute).Unix()
		}),
		"ttl too long":    mutate(func(c map[string]any) { c["exp"] = f.now.Add(16 * time.Minute).Unix() }),
		"exp before iat":  mutate(func(c map[string]any) { c["exp"] = f.now.Add(-1 * time.Second).Unix(); c["iat"] = f.now.Unix() }),
		"missing exp":     mutate(func(c map[string]any) { delete(c, "exp") }),
		"missing iat":     mutate(func(c map[string]any) { delete(c, "iat") }),
		"service type":    mutate(func(c map[string]any) { c["principal_type"] = "service" }),
		"missing type":    mutate(func(c map[string]any) { delete(c, "principal_type") }),
		"other tenant":    mutate(func(c map[string]any) { c["tenant_id"] = "other-tenant" }),
		"missing tenant":  mutate(func(c map[string]any) { delete(c, "tenant_id") }),
		"unmapped sub":    mutate(func(c map[string]any) { c["sub"] = testCPUnmapped }),
		"empty sub":       mutate(func(c map[string]any) { c["sub"] = "" }),
		"non-uuid sub":    mutate(func(c map[string]any) { c["sub"] = "operator@example.com" }),
		"numeric iss":     mutate(func(c map[string]any) { c["iss"] = 42 }),
		"alg none":        noneInput + ".",
		"alg none signed": noneInput + "." + b64url([]byte("sig")),
		"HS256":           hsInput + "." + b64url(mac.Sum(nil)),
		"crit header":     critInput + "." + b64url(append(pad32(cr), pad32(cs)...)),
		"unknown kid":     signES256(t, f.key, "not-a-published-kid", validClaims(f.now)),
		"foreign key":     signES256(t, foreign, f.kid, validClaims(f.now)),
		"missing kid":     signES256(t, f.key, "", validClaims(f.now)),
		"tampered claims": func() string {
			parts := strings.Split(signES256(t, f.key, f.kid, validClaims(f.now)), ".")
			c := validClaims(f.now)
			c["sub"] = testCPOther
			b, _ := json.Marshal(c)
			return parts[0] + "." + b64url(b) + "." + parts[2]
		}(),
		"not a jws":  "a.b",
		"oversized":  signES256(t, f.key, f.kid, validClaims(f.now)) + strings.Repeat("A", maxBearerTokenBytes),
		"empty part": "a..c",
	}
	for name, token := range cases {
		if p, err := f.verifier.VerifyRequest(context.Background(), bearerRequest(token)); err == nil {
			t.Errorf("%s: token accepted as %+v", name, p)
		} else {
			t.Logf("%s: %v", name, err)
		}
	}
}

func TestControlPlaneJWTRequiresExactlyOneBearerHeader(t *testing.T) {
	f := newCPFixture(t)
	token := signES256(t, f.key, f.kid, validClaims(f.now))
	c := Composite{Human: f.verifier, Service: testServiceTokens(t, f.now)}
	ctx := context.Background()

	missing := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	double := bearerRequest(token)
	double.Header.Add("Authorization", "Bearer "+token)
	basic := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	basic.Header.Set("Authorization", "Basic "+token)
	forged := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	forged.Header.Set("Tailscale-User-Login", "operator@example.com")
	forged.Header.Set("Tailscale-User-Name", "Operator")
	forged.Header.Set("X-User-ID", testCPUser)
	forged.Header.Set("X-Forwarded-User", testCPUser)

	for name, r := range map[string]*http.Request{"missing": missing, "double": double, "basic scheme": basic, "forged tailscale header": forged} {
		if p, err := f.verifier.VerifyRequest(ctx, r); err == nil {
			t.Errorf("%s: verifier accepted %+v", name, p)
		}
		if p, err := c.VerifyRequest(ctx, r); err == nil {
			t.Errorf("%s: composite accepted %+v", name, p)
		}
	}

	// Opaque service credentials still reach ServiceTokens in this mode.
	svc := bearerRequest("flowcase-token-0123456789abcdef0123")
	p, err := c.VerifyRequest(ctx, svc)
	if err != nil || p.Kind != "service" {
		t.Fatalf("service token not routed to service credentials: %+v %v", p, err)
	}
	if err := c.Authorize(ctx, p, "run.invoke", "tenant:"+testTenantA); err != nil {
		t.Fatalf("service action denied: %v", err)
	}
}

func TestControlPlaneJWKSRefetchIsRateLimited(t *testing.T) {
	f := newCPFixture(t)
	ctx := context.Background()
	if _, err := f.verifier.VerifyRequest(ctx, bearerRequest(signES256(t, f.key, f.kid, validClaims(f.now)))); err != nil {
		t.Fatal(err)
	}
	if f.server.hitCount() != 1 {
		t.Fatalf("initial JWKS fetches = %d, want 1", f.server.hitCount())
	}
	// Rotation: a new key appears in the JWKS.
	rotated := newECKey(t)
	rotatedKid := ecThumbprint(&rotated.PublicKey)
	f.server.setKeys(ecJWK(&f.key.PublicKey, f.kid), ecJWK(&rotated.PublicKey, rotatedKid))

	*f.clock = f.now.Add(10 * time.Second)
	for i := 0; i < 3; i++ {
		if _, err := f.verifier.VerifyRequest(ctx, bearerRequest(signES256(t, rotated, rotatedKid, validClaims(*f.clock)))); err == nil {
			t.Fatal("unknown kid accepted before the refresh interval elapsed")
		}
	}
	if f.server.hitCount() != 1 {
		t.Fatalf("unknown kid refetched within 30s: %d fetches", f.server.hitCount())
	}
	*f.clock = f.now.Add(31 * time.Second)
	if _, err := f.verifier.VerifyRequest(ctx, bearerRequest(signES256(t, rotated, rotatedKid, validClaims(*f.clock)))); err != nil {
		t.Fatalf("rotated key rejected after refresh: %v", err)
	}
	if _, err := f.verifier.VerifyRequest(ctx, bearerRequest(signES256(t, f.key, "still-unknown", validClaims(*f.clock)))); err == nil {
		t.Fatal("unknown kid accepted")
	}
	if f.server.hitCount() != 2 {
		t.Fatalf("JWKS fetches = %d, want 2", f.server.hitCount())
	}
	// After the cache TTL the keys are refreshed; a removed key stops working.
	f.server.setKeys(ecJWK(&rotated.PublicKey, rotatedKid))
	*f.clock = f.now.Add(11 * time.Minute)
	if _, err := f.verifier.VerifyRequest(ctx, bearerRequest(signES256(t, f.key, f.kid, validClaims(*f.clock)))); err == nil {
		t.Fatal("key removed from JWKS was still accepted after cache expiry")
	}
}

func TestControlPlaneJWKSDuplicateKidIsUntrusted(t *testing.T) {
	f := newCPFixture(t)
	other := newECKey(t)
	f.server.setKeys(ecJWK(&f.key.PublicKey, f.kid), ecJWK(&other.PublicKey, f.kid))
	if _, err := f.verifier.VerifyRequest(context.Background(), bearerRequest(signES256(t, f.key, f.kid, validClaims(f.now)))); err == nil {
		t.Fatal("token accepted although two JWKS keys share its kid")
	}
}

func TestControlPlaneJWTAuthorize(t *testing.T) {
	f := newCPFixture(t)
	ctx := context.Background()
	p, err := f.verifier.VerifyRequest(ctx, bearerRequest(signES256(t, f.key, f.kid, validClaims(f.now))))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.verifier.Authorize(ctx, p, "agent.read", "tenant:"+testTenantA); err != nil {
		t.Fatalf("granted action denied: %v", err)
	}
	if err := f.verifier.Authorize(ctx, p, "agent.create", "tenant:"+testTenantA); err == nil {
		t.Fatal("ungranted action was authorized")
	}
	if err := f.verifier.Authorize(ctx, p, "agent.read", "tenant:"+testTenantB); err == nil {
		t.Fatal("other tenant was authorized")
	}
	if err := f.verifier.Authorize(ctx, p, "tool.read", "global:tool-catalog"); err != nil {
		t.Fatalf("global catalog read denied: %v", err)
	}
	if err := f.verifier.Authorize(ctx, p, "agent.read", "global:tool-catalog"); err == nil {
		t.Fatal("non-read action on global catalog was authorized")
	}
	forged := p
	forged.TenantID = testTenantB
	if err := f.verifier.Authorize(ctx, forged, "agent.read", "tenant:"+testTenantB); err == nil {
		t.Fatal("principal with forged tenant was authorized")
	}

	// Issuer binding in both directions: the same login string under the
	// other issuer is not accepted.
	tsPrincipal := ports.Principal{Issuer: "tailscale-serve", Subject: testCPUser, TenantID: testTenantA, Kind: "user"}
	if err := f.verifier.Authorize(ctx, tsPrincipal, "agent.read", "tenant:"+testTenantA); err == nil {
		t.Fatal("Tailscale-issued principal was authorized by the Control Plane adapter")
	}
	ts, err := NewTailscaleServe([]AccessGrant{{Login: testCPUser, TenantID: testTenantA, Permissions: []string{"agent.read"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.Authorize(ctx, p, "agent.read", "tenant:"+testTenantA); err == nil {
		t.Fatal("Control Plane principal was authorized by the Tailscale adapter")
	}
	if err := ts.Authorize(ctx, tsPrincipal, "agent.read", "tenant:"+testTenantA); err != nil {
		t.Fatalf("Tailscale path regressed: %v", err)
	}
}

func TestControlPlaneConfig(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(k string) string { return values[k] }
	}
	base := map[string]string{"AGENT_STUDIO_CP_ISSUER": testCPIssuer, "AGENT_STUDIO_CP_JWKS_URL": "http://100.126.146.52:5200/.well-known/jwks.json", "AGENT_STUDIO_CP_TENANT_KEY": "acme-demo"}
	if _, err := ControlPlaneConfigFromEnv(env(base), EnvironmentProduction); err == nil {
		t.Fatal("http JWKS URL accepted outside development")
	}
	cfg, err := ControlPlaneConfigFromEnv(env(base), EnvironmentDevelopment)
	if err != nil {
		t.Fatalf("http JWKS URL rejected in development: %v", err)
	}
	if cfg.Audience != "agent-studio" || cfg.MaxTokenTTL != 15*time.Minute {
		t.Fatalf("defaults = %+v", cfg)
	}
	https := map[string]string{"AGENT_STUDIO_CP_ISSUER": testCPIssuer, "AGENT_STUDIO_CP_JWKS_URL": "https://cp.example/.well-known/jwks.json", "AGENT_STUDIO_CP_TENANT_KEY": "acme-demo",
		"AGENT_STUDIO_CP_AUDIENCE": "agent-studio-cp", "AGENT_STUDIO_CP_MAX_TOKEN_TTL": "10m"}
	cfg, err = ControlPlaneConfigFromEnv(env(https), EnvironmentProduction)
	if err != nil || cfg.Audience != "agent-studio-cp" || cfg.MaxTokenTTL != 10*time.Minute {
		t.Fatalf("https config = %+v err=%v", cfg, err)
	}
	for name, override := range map[string]map[string]string{
		"missing issuer": {"AGENT_STUDIO_CP_ISSUER": ""},
		"missing tenant": {"AGENT_STUDIO_CP_TENANT_KEY": ""},
		"missing jwks":   {"AGENT_STUDIO_CP_JWKS_URL": ""},
		"relative jwks":  {"AGENT_STUDIO_CP_JWKS_URL": "/.well-known/jwks.json"},
		"ftp issuer":     {"AGENT_STUDIO_CP_ISSUER": "ftp://cp.example"},
		"bad ttl":        {"AGENT_STUDIO_CP_MAX_TOKEN_TTL": "forever"},
		"huge ttl":       {"AGENT_STUDIO_CP_MAX_TOKEN_TTL": "48h"},
		"negative ttl":   {"AGENT_STUDIO_CP_MAX_TOKEN_TTL": "-1m"},
	} {
		values := map[string]string{}
		for k, v := range https {
			values[k] = v
		}
		for k, v := range override {
			values[k] = v
		}
		if _, err := ControlPlaneConfigFromEnv(env(values), EnvironmentProduction); err == nil {
			t.Errorf("%s: invalid config accepted", name)
		}
	}
	if _, err := HumanAuthMode("oidc"); err == nil {
		t.Fatal("unknown human auth mode accepted")
	}
	if mode, err := HumanAuthMode(""); err != nil || mode != HumanAuthTailscale {
		t.Fatalf("default human auth mode = %q err=%v", mode, err)
	}
	// In this mode the access-map login must be a Control Plane user id.
	if _, err := NewControlPlaneJWT(cfg, []AccessGrant{{Login: "operator@example.com", TenantID: testTenantA, Permissions: []string{"agent.read"}}}); err == nil {
		t.Fatal("non-UUID login accepted in Control Plane mode")
	}
}
