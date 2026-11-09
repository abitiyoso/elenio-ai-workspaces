// Package testkit holds test-only helpers: a fake Control Plane signing key, JWKS and token minting
// with the same claims the Control Plane issuer (internal/integrations.Issuer.Mint) puts in tokens.
// It is imported only by tests.
package testkit

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"time"
)

const (
	Issuer   = "http://cp.test"
	Tenant   = "acme-demo"
	Audience = "case-workspace"
)

// Signer is a fake Control Plane ES256 key.
type Signer struct {
	Key *ecdsa.PrivateKey
	Kid string
}

func NewSigner() *Signer {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return &Signer{Key: k, Kid: "kid-" + fmt.Sprint(time.Now().UnixNano())}
}

func b64(n *big.Int) string {
	b := n.Bytes()
	p := make([]byte, 32)
	copy(p[32-len(b):], b)
	return base64.RawURLEncoding.EncodeToString(p)
}

// JWKS is the public key set document.
func (s *Signer) JWKS() []byte {
	b, _ := json.Marshal(map[string]any{"keys": []map[string]string{{"kty": "EC", "crv": "P-256", "x": b64(s.Key.PublicKey.X),
		"y": b64(s.Key.PublicKey.Y), "kid": s.Kid, "use": "sig", "alg": "ES256"}}})
	return b
}

// JWKSServer serves the JWKS at any path.
func (s *Signer) JWKSServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.JWKS())
	}))
}

// Claims returns the claims a CP-minted human token carries for sub (5 minute lifetime).
func Claims(sub string) map[string]any {
	now := time.Now()
	return map[string]any{"iss": Issuer, "sub": sub, "aud": Audience, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"jti": fmt.Sprint(now.UnixNano()), "ctx_ver": "workspace.authctx.v1", "principal_type": "human", "tenant_id": Tenant, "workspace_id": "sim-b2b",
		"scope": "app:open case_worker", "sid": "sid-" + sub[:8], "policy_version": "test", "correlation_id": "test", "auth_time": now.Unix()}
}

func enc(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Sign signs claims with ES256 under the signer's kid.
func (s *Signer) Sign(claims map[string]any) string {
	return s.SignWith(claims, s.Key, s.Kid)
}

// SignWith signs with an arbitrary key and kid (foreign-key tests).
func (s *Signer) SignWith(claims map[string]any, key *ecdsa.PrivateKey, kid string) string {
	input := enc(map[string]string{"alg": "ES256", "typ": "JWT", "kid": kid}) + "." + enc(claims)
	d := sha256.Sum256([]byte(input))
	r, sv, err := ecdsa.Sign(rand.Reader, key, d[:])
	if err != nil {
		panic(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// None builds an alg=none token.
func (s *Signer) None(claims map[string]any) string {
	return enc(map[string]string{"alg": "none", "typ": "JWT", "kid": s.Kid}) + "." + enc(claims) + "."
}

// HS256 builds an HMAC token keyed with secret (algorithm confusion tests).
func (s *Signer) HS256(claims map[string]any, secret []byte) string {
	input := enc(map[string]string{"alg": "HS256", "typ": "JWT", "kid": s.Kid}) + "." + enc(claims)
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Token is a valid token for sub.
func (s *Signer) Token(sub string) string { return s.Sign(Claims(sub)) }
