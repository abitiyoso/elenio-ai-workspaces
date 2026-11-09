// Package auth verifies the Control Plane delegation token on every BFF request. Identity, tenant and
// workspace come only from a verified ES256 token (aud=case-workspace); identity headers, query strings
// and request bodies are never consulted.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnauthenticated is any token problem (missing, malformed, bad signature, wrong claims): 401.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrKeysUnavailable means the signing keys could not be fetched: 503, the token may be fine.
	ErrKeysUnavailable = errors.New("control plane signing keys unavailable")
)

const (
	maxBearerBytes  = 8 << 10
	maxJWKSBytes    = 1 << 20
	jwksFetchTime   = 5 * time.Second
	defaultSkew     = 30 * time.Second
	defaultMaxTTL   = 15 * time.Minute
	defaultCacheTTL = 10 * time.Minute
	defaultRefresh  = 30 * time.Second
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Identity is the verified caller. Token is the raw incoming token; it is only ever sent to the Control
// Plane token exchange and never logged or returned.
type Identity struct {
	UserID       string // Control Plane user id (token sub, lower-case UUID)
	SessionRef   string // token sid: a stable per-session reference (not the session id itself)
	TenantKey    string
	WorkspaceKey string
	Scopes       []string
	ExpiresAt    time.Time
	Token        string
}

type ctxKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// Config is operator configuration for the verifier.
type Config struct {
	Issuer     string
	JWKSURL    string
	Audience   string
	TenantKey  string
	MaxTTL     time.Duration
	ClockSkew  time.Duration
	CacheTTL   time.Duration
	MinRefresh time.Duration
	HTTPClient *http.Client
	Now        func() time.Time
}

// Verifier checks Control Plane tokens: ES256 only, kid lookup in the CP JWKS, exact issuer, audience,
// tenant, principal_type human, exp/iat required, nbf honoured, lifetime <= MaxTTL.
type Verifier struct {
	issuer, audience, tenant string
	maxTTL, skew             time.Duration
	now                      func() time.Time
	keys                     *jwksCache
}

func NewVerifier(cfg Config) (*Verifier, error) {
	if cfg.Issuer == "" || cfg.JWKSURL == "" || cfg.Audience == "" || cfg.TenantKey == "" {
		return nil, errors.New("auth: issuer, JWKS URL, audience and tenant key are required")
	}
	if cfg.MaxTTL <= 0 {
		cfg.MaxTTL = defaultMaxTTL
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = defaultSkew
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = defaultCacheTTL
	}
	if cfg.MinRefresh <= 0 {
		cfg.MinRefresh = defaultRefresh
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: jwksFetchTime, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Verifier{issuer: cfg.Issuer, audience: cfg.Audience, tenant: cfg.TenantKey, maxTTL: cfg.MaxTTL, skew: cfg.ClockSkew, now: cfg.Now,
		keys: &jwksCache{url: cfg.JWKSURL, client: client, ttl: cfg.CacheTTL, minRefresh: cfg.MinRefresh, now: cfg.Now}}, nil
}

// Ready reports whether the signing keys can be loaded (readiness probe).
func (v *Verifier) Ready(ctx context.Context) error { return v.keys.ensure(ctx) }

// VerifyRequest reads exactly one "Authorization: Bearer <JWS>" header and verifies it.
func (v *Verifier) VerifyRequest(r *http.Request) (Identity, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return Identity{}, fmt.Errorf("%w: exactly one Authorization header is required", ErrUnauthenticated)
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Identity{}, fmt.Errorf("%w: bearer token required", ErrUnauthenticated)
	}
	return v.Verify(r.Context(), parts[1])
}

type jwsHeader struct {
	Alg  string          `json:"alg"`
	Kid  string          `json:"kid"`
	Crit json.RawMessage `json:"crit"`
}

type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return errors.New("aud must be a string or an array of strings")
	}
	*a = many
	return nil
}

type claims struct {
	Issuer        string          `json:"iss"`
	Subject       string          `json:"sub"`
	Audience      audience        `json:"aud"`
	ExpiresAt     *json.Number    `json:"exp"`
	IssuedAt      *json.Number    `json:"iat"`
	NotBefore     *json.Number    `json:"nbf"`
	PrincipalType string          `json:"principal_type"`
	TenantID      string          `json:"tenant_id"`
	WorkspaceID   string          `json:"workspace_id"`
	Scope         string          `json:"scope"`
	SID           string          `json:"sid"`
	Act           json.RawMessage `json:"act"`
}

func fail(reason string) error { return fmt.Errorf("%w: %s", ErrUnauthenticated, reason) }

// Verify checks one compact JWS.
func (v *Verifier) Verify(ctx context.Context, token string) (Identity, error) {
	if token == "" || len(token) > maxBearerBytes {
		return Identity{}, fail("token missing or too large")
	}
	seg := strings.Split(token, ".")
	if len(seg) != 3 || seg[0] == "" || seg[1] == "" || seg[2] == "" {
		return Identity{}, fail("not a compact JWS")
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(seg[0])
	if err != nil {
		return Identity{}, fail("malformed header")
	}
	var h jwsHeader
	if err := json.Unmarshal(rawHeader, &h); err != nil {
		return Identity{}, fail("malformed header")
	}
	if h.Alg != "ES256" {
		return Identity{}, fail("algorithm not allowed")
	}
	if h.Crit != nil {
		return Identity{}, fail("critical headers not supported")
	}
	if h.Kid == "" || len(h.Kid) > 256 {
		return Identity{}, fail("kid missing")
	}
	sig, err := base64.RawURLEncoding.DecodeString(seg[2])
	if err != nil || len(sig) != 64 {
		return Identity{}, fail("malformed signature")
	}
	key, err := v.keys.lookup(ctx, h.Kid)
	if err != nil {
		return Identity{}, err
	}
	digest := sha256.Sum256([]byte(seg[0] + "." + seg[1]))
	if !ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return Identity{}, fail("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(seg[1])
	if err != nil {
		return Identity{}, fail("malformed payload")
	}
	var c claims
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	if err := dec.Decode(&c); err != nil {
		return Identity{}, fail("malformed claims")
	}
	if c.Issuer != v.issuer {
		return Identity{}, fail("issuer not trusted")
	}
	audOK := false
	for _, a := range c.Audience {
		if a == v.audience {
			audOK = true
		}
	}
	if !audOK {
		return Identity{}, fail("wrong audience")
	}
	if c.ExpiresAt == nil || c.IssuedAt == nil {
		return Identity{}, fail("exp and iat required")
	}
	exp, err1 := numericDate(*c.ExpiresAt)
	iat, err2 := numericDate(*c.IssuedAt)
	if err1 != nil || err2 != nil {
		return Identity{}, fail("invalid time claim")
	}
	now := v.now()
	if !now.Before(exp.Add(v.skew)) {
		return Identity{}, fail("expired")
	}
	if iat.After(now.Add(v.skew)) {
		return Identity{}, fail("issued in the future")
	}
	if c.NotBefore != nil {
		nbf, err := numericDate(*c.NotBefore)
		if err != nil || nbf.After(now.Add(v.skew)) {
			return Identity{}, fail("not yet valid")
		}
	}
	if !exp.After(iat) || exp.Sub(iat) > v.maxTTL {
		return Identity{}, fail("lifetime exceeds maximum")
	}
	if c.PrincipalType != "human" {
		return Identity{}, fail("not a human principal")
	}
	if c.TenantID != v.tenant {
		return Identity{}, fail("tenant not served")
	}
	if len(c.Act) > 0 && string(c.Act) != "null" {
		return Identity{}, fail("delegated (act) tokens are not accepted")
	}
	sub := strings.ToLower(strings.TrimSpace(c.Subject))
	if !uuidPattern.MatchString(sub) {
		return Identity{}, fail("subject must be a user id")
	}
	return Identity{UserID: sub, SessionRef: c.SID, TenantKey: c.TenantID, WorkspaceKey: c.WorkspaceID, Scopes: strings.Fields(c.Scope),
		ExpiresAt: exp, Token: token}, nil
}

func numericDate(n json.Number) (time.Time, error) {
	f, err := n.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1<<40 {
		return time.Time{}, errors.New("invalid time claim")
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)), nil
}

// jwksCache holds the CP EC P-256 keys by kid. Unknown kids trigger a refetch at most once per
// minRefresh so forged kids cannot hammer the Control Plane.
type jwksCache struct {
	url        string
	client     *http.Client
	ttl        time.Duration
	minRefresh time.Duration
	now        func() time.Time

	mu          sync.Mutex
	keys        map[string]*ecdsa.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

func (c *jwksCache) ensure(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && c.now().Sub(c.fetchedAt) < c.ttl {
		return nil
	}
	keys, err := c.fetch(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeysUnavailable, err)
	}
	c.keys, c.fetchedAt, c.lastAttempt = keys, c.now(), c.now()
	return nil
}

func (c *jwksCache) lookup(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	fresh := !c.fetchedAt.IsZero() && now.Sub(c.fetchedAt) < c.ttl
	if k, ok := c.keys[kid]; ok && fresh {
		return k, nil
	}
	var fetchErr error
	if c.lastAttempt.IsZero() || now.Sub(c.lastAttempt) >= c.minRefresh {
		c.lastAttempt = now
		keys, err := c.fetch(ctx)
		if err == nil {
			c.keys, c.fetchedAt, fresh = keys, now, true
		}
		fetchErr = err
	}
	if k, ok := c.keys[kid]; ok && fresh {
		return k, nil
	}
	if fetchErr != nil && c.keys == nil {
		return nil, fmt.Errorf("%w: %v", ErrKeysUnavailable, fetchErr)
	}
	return nil, fail("unknown signing key")
}

type rawJWK struct {
	Kty, Kid, Use, Alg, Crv, X, Y string
}

func (c *jwksCache) fetch(ctx context.Context) (map[string]*ecdsa.PublicKey, error) {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jwksFetchTime)
	defer cancel()
	req, err := http.NewRequestWithContext(fctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxJWKSBytes {
		return nil, errors.New("JWKS too large")
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, errors.New("JWKS malformed")
	}
	keys := map[string]*ecdsa.PublicKey{}
	ambiguous := map[string]bool{}
	for _, k := range set.Keys {
		pub, ok := parseEC(rawJWK{Kty: k.Kty, Kid: k.Kid, Use: k.Use, Alg: k.Alg, Crv: k.Crv, X: k.X, Y: k.Y})
		if !ok {
			continue
		}
		if _, dup := keys[k.Kid]; dup || ambiguous[k.Kid] {
			delete(keys, k.Kid)
			ambiguous[k.Kid] = true
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

func parseEC(k rawJWK) (*ecdsa.PublicKey, bool) {
	if k.Kid == "" || k.Kty != "EC" || k.Crv != "P-256" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "ES256") {
		return nil, false
	}
	x, errX := base64.RawURLEncoding.DecodeString(k.X)
	y, errY := base64.RawURLEncoding.DecodeString(k.Y)
	if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
		return nil, false
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
	if err != nil {
		return nil, false
	}
	return pub, true
}
