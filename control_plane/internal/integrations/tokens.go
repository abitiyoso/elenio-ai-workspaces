// Package integrations holds the trusted downstream configuration: audience-bound service tokens, the
// JWKS that mini-app AuthAdapters verify, and route-health probes. Upstream targets come only from
// deployment configuration (TRUSTED_APP_UPSTREAMS), never from registry manifests or requests.
package integrations

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/elenio/control-plane/internal/platform/secrets"
)

// ContextVersion matches contracts/auth_context.schema.json.
const ContextVersion = "workspace.authctx.v1"

type Issuer struct {
	issuer   string
	ttl      time.Duration
	key      *ecdsa.PrivateKey
	kid      string
	previous []JWK
	now      func() time.Time
}

type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
}

// NewIssuer loads an ES256 private key (PEM, PKCS#8 or SEC1). With no key it generates an ephemeral one,
// which config only permits in development.
func NewIssuer(issuer string, ttl time.Duration, pemKey, previousJWKS []byte) (*Issuer, error) {
	var key *ecdsa.PrivateKey
	if len(pemKey) == 0 {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		key = k
	} else {
		block, _ := pem.Decode(pemKey)
		if block == nil {
			return nil, errors.New("service token signing key is not PEM")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			if parsed, err = x509.ParseECPrivateKey(block.Bytes); err != nil {
				return nil, errors.New("service token signing key must be an EC P-256 private key")
			}
		}
		k, ok := parsed.(*ecdsa.PrivateKey)
		if !ok || k.Curve != elliptic.P256() {
			return nil, errors.New("service token signing key must be an EC P-256 private key")
		}
		key = k
	}
	is := &Issuer{issuer: issuer, ttl: ttl, key: key, now: time.Now}
	is.kid = thumbprint(&key.PublicKey)
	if len(previousJWKS) > 0 {
		var set struct {
			Keys []JWK `json:"keys"`
		}
		if err := json.Unmarshal(previousJWKS, &set); err != nil {
			return nil, fmt.Errorf("previous JWKS: %w", err)
		}
		is.previous = set.Keys
	}
	return is, nil
}

func b64(n *big.Int) string {
	b := n.Bytes()
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return base64.RawURLEncoding.EncodeToString(padded)
}

// thumbprint is the RFC 7638 JWK thumbprint, used as kid.
func thumbprint(pub *ecdsa.PublicKey) string {
	j := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, b64(pub.X), b64(pub.Y))
	sum := sha256.Sum256([]byte(j))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (is *Issuer) JWKS() map[string]any {
	keys := []JWK{{Kty: "EC", Crv: "P-256", X: b64(is.key.PublicKey.X), Y: b64(is.key.PublicKey.Y), Kid: is.kid, Use: "sig", Alg: "ES256"}}
	keys = append(keys, is.previous...)
	return map[string]any{"keys": keys}
}

func (is *Issuer) KeyIDs() []string {
	ids := []string{is.kid}
	for _, k := range is.previous {
		ids = append(ids, k.Kid)
	}
	return ids
}

func (is *Issuer) IssuerURL() string           { return is.issuer }
func (is *Issuer) TTL() time.Duration          { return is.ttl }
func (is *Issuer) PublicKey() *ecdsa.PublicKey { return &is.key.PublicKey }
func (is *Issuer) KeyID() string               { return is.kid }

// Subject is the verified context placed in a downstream token.
type Subject struct {
	UserID        string
	TenantKey     string
	WorkspaceKey  string
	IdPIssuer     string
	IdPSubject    string
	PrincipalType string
	SessionID     string
	Scopes        []string
	PolicyVersion string
	CorrelationID string
	AuthTime      time.Time
	// Actor, when set, adds an RFC 8693 actor claim {"act":{"sub":Actor}} (delegated token exchange). Tokens
	// carrying act are never accepted as exchange subject tokens (no chaining).
	Actor string
	// MaxTTL optionally shortens the token lifetime below the issuer TTL; it can never extend it.
	MaxTTL time.Duration
}

// Mint issues a short-lived, audience-restricted ES256 token. It carries identity and platform scopes
// only; downstream services still apply their own domain authorization.
func (is *Issuer) Mint(audience string, s Subject) (string, time.Time, error) {
	if audience == "" {
		return "", time.Time{}, errors.New("audience is required")
	}
	now := is.now()
	ttl := is.ttl
	if s.MaxTTL > 0 && s.MaxTTL < ttl {
		ttl = s.MaxTTL
	}
	exp := now.Add(ttl)
	claims := jwt.MapClaims{
		"iss":            is.issuer,
		"sub":            s.UserID,
		"aud":            audience,
		"iat":            now.Unix(),
		"nbf":            now.Unix(),
		"exp":            exp.Unix(),
		"jti":            secrets.RandomToken(16),
		"ctx_ver":        ContextVersion,
		"principal_type": s.PrincipalType,
		"tenant_id":      s.TenantKey,
		"workspace_id":   s.WorkspaceKey,
		"scope":          joinScopes(s.Scopes),
		"idp_iss":        s.IdPIssuer,
		"idp_sub":        s.IdPSubject,
		"sid":            SessionClaim(is.kid, s.SessionID), // stable per session, not the session id
		"policy_version": s.PolicyVersion,
		"correlation_id": s.CorrelationID,
		"auth_time":      s.AuthTime.Unix(),
	}
	if s.Actor != "" {
		claims["act"] = map[string]any{"sub": s.Actor}
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	t.Header["kid"] = is.kid
	t.Header["typ"] = "JWT"
	signed, err := t.SignedString(is.key)
	return signed, exp, err
}

// SessionClaim is the sid value placed in tokens signed under kid: an HMAC of the internal session id, so the
// session row id is never disclosed downstream but the Control Plane can match a token back to its session.
func SessionClaim(kid, sessionID string) string { return secrets.HMAC([]byte(kid), sessionID) }

// MaxSubjectTokenLifetime matches what downstream verifiers accept (exp - iat).
const MaxSubjectTokenLifetime = 15 * time.Minute

var ErrTokenInvalid = errors.New("token is not a valid Control Plane token")

// Verified is a token this Control Plane issued, after signature and registered-claim checks.
type Verified struct {
	Claims jwt.MapClaims
	KeyID  string // kid that verified the signature (current or a previous rotation key)
}

// verificationKey returns the public key for kid: the current signing key or a previous JWKS entry.
func (is *Issuer) verificationKey(kid string) (*ecdsa.PublicKey, bool) {
	if kid == "" {
		return nil, false
	}
	if kid == is.kid {
		return &is.key.PublicKey, true
	}
	for _, k := range is.previous {
		if k.Kid != kid || k.Kty != "EC" || k.Crv != "P-256" || (k.Alg != "" && k.Alg != "ES256") {
			continue
		}
		x, errX := base64.RawURLEncoding.DecodeString(k.X)
		y, errY := base64.RawURLEncoding.DecodeString(k.Y)
		if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
			return nil, false
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return nil, false
		}
		return pub, true
	}
	return nil, false
}

// Verify checks a token minted by this issuer: ES256 only, kid of the current or a previous key, exact iss,
// exp/iat required, nbf honoured, lifetime <= MaxSubjectTokenLifetime. Audience and the custom claims are the
// caller's policy (see the token-exchange handler).
func (is *Issuer) Verify(raw string) (*Verified, error) {
	if raw == "" || len(raw) > 8192 {
		return nil, ErrTokenInvalid
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"ES256"}), jwt.WithIssuer(is.issuer), jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(), jwt.WithLeeway(30*time.Second), jwt.WithTimeFunc(is.now))
	claims := jwt.MapClaims{}
	var kid string
	_, err := parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ = t.Header["kid"].(string)
		pub, ok := is.verificationKey(kid)
		if !ok {
			return nil, ErrTokenInvalid
		}
		return pub, nil
	})
	if err != nil {
		return nil, ErrTokenInvalid
	}
	iat, _ := claims.GetIssuedAt()
	exp, _ := claims.GetExpirationTime()
	if iat == nil || exp == nil || exp.Sub(iat.Time) > MaxSubjectTokenLifetime || exp.Before(iat.Time) {
		return nil, ErrTokenInvalid
	}
	return &Verified{Claims: claims, KeyID: kid}, nil
}

func joinScopes(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}
