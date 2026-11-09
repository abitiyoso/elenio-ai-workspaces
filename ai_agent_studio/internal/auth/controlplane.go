package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/elenio/agent-studio/internal/ports"
)

// Human identity modes selected by AGENT_STUDIO_HUMAN_AUTH.
const (
	HumanAuthTailscale       = "tailscale"
	HumanAuthControlPlaneJWT = "control_plane_jwt"
)

const (
	defaultControlPlaneAudience = "agent-studio"
	defaultMaxTokenTTL          = 15 * time.Minute
	maxAllowedTokenTTL          = time.Hour
	defaultClockSkew            = 60 * time.Second
	defaultJWKSCacheTTL         = 10 * time.Minute
	defaultJWKSRefreshInterval  = 30 * time.Second
	maxBearerTokenBytes         = 8 << 10
	maxJWKSBytes                = 1 << 20
	jwksFetchTimeout            = 5 * time.Second
	minRSAKeyBits               = 2048
)

var subjectUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// HumanAuthMode validates AGENT_STUDIO_HUMAN_AUTH; empty means tailscale.
func HumanAuthMode(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", HumanAuthTailscale:
		return HumanAuthTailscale, nil
	case HumanAuthControlPlaneJWT:
		return HumanAuthControlPlaneJWT, nil
	default:
		return "", errors.New("AGENT_STUDIO_HUMAN_AUTH must be tailscale or control_plane_jwt")
	}
}

// ControlPlaneConfig configures verification of AI Workspace Control Plane
// delegation tokens. Every value is operator configuration.
type ControlPlaneConfig struct {
	Issuer   string
	JWKSURL  string
	Audience string
	// TenantKey is the Control Plane tenant key (token claim tenant_id) this instance serves. Tokens minted
	// for another tenant are rejected even when the same user id is mapped in the access map.
	TenantKey string
	// AllowInsecureJWKS permits an http:// JWKS URL (development only).
	AllowInsecureJWKS   bool
	MaxTokenTTL         time.Duration
	ClockSkew           time.Duration
	JWKSCacheTTL        time.Duration
	JWKSRefreshInterval time.Duration
	HTTPClient          *http.Client
	Now                 func() time.Time
}

// ControlPlaneConfigFromEnv reads AGENT_STUDIO_CP_* settings.
func ControlPlaneConfigFromEnv(getenv func(string) string, environment string) (ControlPlaneConfig, error) {
	cfg := ControlPlaneConfig{
		Issuer:            strings.TrimSpace(getenv("AGENT_STUDIO_CP_ISSUER")),
		JWKSURL:           strings.TrimSpace(getenv("AGENT_STUDIO_CP_JWKS_URL")),
		Audience:          strings.TrimSpace(getenv("AGENT_STUDIO_CP_AUDIENCE")),
		TenantKey:         strings.TrimSpace(getenv("AGENT_STUDIO_CP_TENANT_KEY")),
		AllowInsecureJWKS: environment == EnvironmentDevelopment,
		MaxTokenTTL:       defaultMaxTokenTTL,
	}
	if cfg.Issuer == "" {
		return cfg, errors.New("AGENT_STUDIO_CP_ISSUER is required with AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt")
	}
	if cfg.JWKSURL == "" {
		return cfg, errors.New("AGENT_STUDIO_CP_JWKS_URL is required with AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt")
	}
	if cfg.Audience == "" {
		cfg.Audience = defaultControlPlaneAudience
	}
	if cfg.TenantKey == "" {
		return cfg, errors.New("AGENT_STUDIO_CP_TENANT_KEY is required with AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt")
	}
	if raw := strings.TrimSpace(getenv("AGENT_STUDIO_CP_MAX_TOKEN_TTL")); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil || ttl <= 0 || ttl > maxAllowedTokenTTL {
			return cfg, fmt.Errorf("AGENT_STUDIO_CP_MAX_TOKEN_TTL must be a duration between 1s and %s", maxAllowedTokenTTL)
		}
		cfg.MaxTokenTTL = ttl
	}
	return cfg, cfg.validate()
}

func (cfg ControlPlaneConfig) validate() error {
	issuer, err := url.Parse(cfg.Issuer)
	if err != nil || (issuer.Scheme != "https" && issuer.Scheme != "http") || issuer.Host == "" ||
		issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return errors.New("AGENT_STUDIO_CP_ISSUER must be an absolute http(s) origin without credentials, query or fragment")
	}
	jwks, err := url.Parse(cfg.JWKSURL)
	if err != nil || jwks.Host == "" || jwks.User != nil || jwks.Fragment != "" {
		return errors.New("AGENT_STUDIO_CP_JWKS_URL must be an absolute URL without credentials or fragment")
	}
	switch {
	case jwks.Scheme == "https":
	case jwks.Scheme == "http" && cfg.AllowInsecureJWKS:
	default:
		return errors.New("AGENT_STUDIO_CP_JWKS_URL must use https unless AGENT_STUDIO_ENV=development")
	}
	if cfg.Audience == "" || strings.ContainsAny(cfg.Audience, " \t\r\n") {
		return errors.New("AGENT_STUDIO_CP_AUDIENCE must be a non-empty token")
	}
	if cfg.TenantKey == "" || len(cfg.TenantKey) > 256 || strings.ContainsAny(cfg.TenantKey, " \t\r\n") {
		return errors.New("AGENT_STUDIO_CP_TENANT_KEY must be a non-empty token")
	}
	if cfg.MaxTokenTTL <= 0 || cfg.MaxTokenTTL > maxAllowedTokenTTL {
		return errors.New("control plane max token TTL is out of range")
	}
	return nil
}

// ControlPlaneJWT verifies the per-request delegation token the Control Plane
// BFF injects (Authorization: Bearer <JWS>) and resolves the token subject (a
// Control Plane user id) through the same explicit access map used by the
// Tailscale mode. Tailscale and other identity headers are never consulted.
type ControlPlaneJWT struct {
	issuer    string
	audience  string
	tenantKey string
	maxTTL    time.Duration
	skew      time.Duration
	now       func() time.Time
	grants    map[string]grant
	keys      *jwksCache
}

// LoadControlPlaneJWT reads the access map file and builds the verifier.
func LoadControlPlaneJWT(cfg ControlPlaneConfig, accessMapPath string) (*ControlPlaneJWT, error) {
	entries, err := ReadAccessMapFile(accessMapPath)
	if err != nil {
		return nil, err
	}
	return NewControlPlaneJWT(cfg, entries)
}

// NewControlPlaneJWT builds the verifier. In this mode each access-map
// "login" must be a Control Plane user id (UUID).
func NewControlPlaneJWT(cfg ControlPlaneConfig, entries []AccessGrant) (*ControlPlaneJWT, error) {
	if cfg.Audience == "" {
		cfg.Audience = defaultControlPlaneAudience
	}
	if cfg.MaxTokenTTL == 0 {
		cfg.MaxTokenTTL = defaultMaxTokenTTL
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	grants, err := buildGrants(entries, subjectUUIDPattern.MatchString, "a Control Plane user id (UUID) as login")
	if err != nil {
		return nil, err
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = defaultClockSkew
	}
	if cfg.JWKSCacheTTL <= 0 {
		cfg.JWKSCacheTTL = defaultJWKSCacheTTL
	}
	if cfg.JWKSRefreshInterval <= 0 {
		cfg.JWKSRefreshInterval = defaultJWKSRefreshInterval
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: jwksFetchTimeout,
			// A redirect could move key discovery to an unvetted origin.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return &ControlPlaneJWT{
		issuer: cfg.Issuer, audience: cfg.Audience, tenantKey: cfg.TenantKey, maxTTL: cfg.MaxTokenTTL, skew: cfg.ClockSkew, now: cfg.Now, grants: grants,
		keys: &jwksCache{url: cfg.JWKSURL, client: client, ttl: cfg.JWKSCacheTTL, minRefresh: cfg.JWKSRefreshInterval, now: cfg.Now},
	}, nil
}

// Issuer reports the configured Control Plane issuer.
func (a *ControlPlaneJWT) Issuer() string { return a.issuer }

func (a *ControlPlaneJWT) VerifyRequest(ctx context.Context, r *http.Request) (ports.Principal, error) {
	if a == nil || r == nil {
		return ports.Principal{}, errors.New("verified Control Plane request is required")
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return ports.Principal{}, errors.New("exactly one Authorization header is required")
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ports.Principal{}, errors.New("bearer Control Plane token is required")
	}
	if len(parts[1]) > maxBearerTokenBytes {
		return ports.Principal{}, errors.New("Control Plane token is too large")
	}
	subject, err := a.verifyToken(ctx, parts[1])
	if err != nil {
		return ports.Principal{}, err
	}
	access, ok := a.grants[subject]
	if !ok {
		return ports.Principal{}, errors.New("Control Plane user is not mapped to an Agent Studio tenant")
	}
	return ports.Principal{Issuer: a.issuer, Subject: subject, TenantID: access.tenantID, Kind: "user"}, nil
}

func (a *ControlPlaneJWT) Authorize(_ context.Context, principal ports.Principal, action, resource string) error {
	if a == nil || principal.Issuer != a.issuer || principal.Subject == "" || principal.Kind != "user" {
		return errors.New("verified Control Plane identity is required")
	}
	return authorizeGrant(a.grants, principal, action, resource)
}

// PermissionsFor lists the mapped actions for a verified principal so the UI
// can hide controls; the API still authorizes every request.
func (a *ControlPlaneJWT) PermissionsFor(principal ports.Principal) []string {
	if a == nil || principal.Issuer != a.issuer {
		return []string{}
	}
	return permissionsFor(a.grants, principal)
}

// handlesBearer claims every request whose Authorization header is a compact
// JWS (or that carries several Authorization headers, which VerifyRequest
// rejects). Opaque service credentials keep going to ServiceTokens.
func (a *ControlPlaneJWT) handlesBearer(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) > 1 {
		return true
	}
	if len(values) == 0 {
		return false
	}
	parts := strings.Fields(values[0])
	return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && strings.Count(parts[1], ".") == 2
}

type jwsHeader struct {
	Alg  string          `json:"alg"`
	Kid  string          `json:"kid"`
	Crit json.RawMessage `json:"crit"`
}

type audienceClaim []string

func (a *audienceClaim) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = audienceClaim{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return errors.New("aud must be a string or an array of strings")
	}
	*a = many
	return nil
}

type controlPlaneClaims struct {
	Issuer        string        `json:"iss"`
	Subject       string        `json:"sub"`
	Audience      audienceClaim `json:"aud"`
	ExpiresAt     *json.Number  `json:"exp"`
	IssuedAt      *json.Number  `json:"iat"`
	NotBefore     *json.Number  `json:"nbf"`
	PrincipalType string        `json:"principal_type"`
	TenantID      string        `json:"tenant_id"`
}

// verifyToken checks the signature and registered claims and returns the
// normalized subject.
func (a *ControlPlaneJWT) verifyToken(ctx context.Context, token string) (string, error) {
	segments := strings.Split(token, ".")
	if len(segments) != 3 || segments[0] == "" || segments[1] == "" || segments[2] == "" {
		return "", errors.New("Control Plane token is not a compact JWS")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		return "", errors.New("Control Plane token header is malformed")
	}
	var header jwsHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return "", errors.New("Control Plane token header is malformed")
	}
	if header.Alg != "ES256" && header.Alg != "RS256" {
		return "", errors.New("Control Plane token algorithm is not allowed")
	}
	if header.Crit != nil {
		return "", errors.New("Control Plane token has unsupported critical headers")
	}
	if header.Kid == "" || len(header.Kid) > 256 {
		return "", errors.New("Control Plane token key id is missing")
	}
	signature, err := base64.RawURLEncoding.DecodeString(segments[2])
	if err != nil {
		return "", errors.New("Control Plane token signature is malformed")
	}
	key, err := a.keys.lookup(ctx, header.Kid)
	if err != nil {
		return "", err
	}
	if err := key.verify(header.Alg, []byte(segments[0]+"."+segments[1]), signature); err != nil {
		return "", err
	}

	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return "", errors.New("Control Plane token payload is malformed")
	}
	var claims controlPlaneClaims
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&claims); err != nil {
		return "", errors.New("Control Plane token claims are malformed")
	}
	if claims.Issuer != a.issuer {
		return "", errors.New("Control Plane token issuer is not trusted")
	}
	audienceOK := false
	for _, aud := range claims.Audience {
		if aud == a.audience {
			audienceOK = true
			break
		}
	}
	if !audienceOK {
		return "", errors.New("Control Plane token audience is not Agent Studio")
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil {
		return "", errors.New("Control Plane token requires exp and iat")
	}
	exp, err := numericDate(*claims.ExpiresAt)
	if err != nil {
		return "", err
	}
	iat, err := numericDate(*claims.IssuedAt)
	if err != nil {
		return "", err
	}
	now := a.now()
	if !now.Before(exp.Add(a.skew)) {
		return "", errors.New("Control Plane token has expired")
	}
	if iat.After(now.Add(a.skew)) {
		return "", errors.New("Control Plane token was issued in the future")
	}
	if claims.NotBefore != nil {
		nbf, err := numericDate(*claims.NotBefore)
		if err != nil {
			return "", err
		}
		if nbf.After(now.Add(a.skew)) {
			return "", errors.New("Control Plane token is not yet valid")
		}
	}
	if !exp.After(iat) || exp.Sub(iat) > a.maxTTL {
		return "", errors.New("Control Plane token lifetime exceeds the allowed maximum")
	}
	if claims.PrincipalType != "human" {
		return "", errors.New("Control Plane token is not a human delegation token")
	}
	if claims.TenantID != a.tenantKey {
		return "", errors.New("Control Plane token tenant is not served by this Agent Studio instance")
	}
	subject := normalizeLogin(claims.Subject)
	if !subjectUUIDPattern.MatchString(subject) {
		return "", errors.New("Control Plane token subject must be a user id")
	}
	return subject, nil
}

func numericDate(n json.Number) (time.Time, error) {
	f, err := n.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1<<40 {
		return time.Time{}, errors.New("Control Plane token has an invalid time claim")
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)), nil
}

// verificationKey is one signing key from the Control Plane JWKS.
type verificationKey struct {
	alg string // "ES256" or "RS256"
	ec  *ecdsa.PublicKey
	rsa *rsa.PublicKey
}

func (k verificationKey) verify(alg string, signingInput, signature []byte) error {
	if alg != k.alg {
		return errors.New("Control Plane token algorithm does not match its key")
	}
	digest := sha256.Sum256(signingInput)
	switch alg {
	case "ES256":
		if k.ec == nil || len(signature) != 64 {
			return errors.New("Control Plane token signature is invalid")
		}
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		if !ecdsa.Verify(k.ec, digest[:], r, s) {
			return errors.New("Control Plane token signature is invalid")
		}
		return nil
	case "RS256":
		if k.rsa == nil || rsa.VerifyPKCS1v15(k.rsa, crypto.SHA256, digest[:], signature) != nil {
			return errors.New("Control Plane token signature is invalid")
		}
		return nil
	default:
		return errors.New("Control Plane token algorithm is not allowed")
	}
}

// jwksCache holds the Control Plane signing keys keyed by kid. Keys are
// refetched when the cache is older than ttl or a token names an unknown kid,
// but never more than once per minRefresh, so unknown kids cannot be used to
// hammer the Control Plane.
type jwksCache struct {
	url        string
	client     *http.Client
	ttl        time.Duration
	minRefresh time.Duration
	now        func() time.Time

	mu          sync.Mutex
	keys        map[string]verificationKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

func (c *jwksCache) lookup(ctx context.Context, kid string) (verificationKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	fresh := !c.fetchedAt.IsZero() && now.Sub(c.fetchedAt) < c.ttl
	if key, ok := c.keys[kid]; ok && fresh {
		return key, nil
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
	if key, ok := c.keys[kid]; ok && fresh {
		return key, nil
	}
	if fetchErr != nil {
		return verificationKey{}, fmt.Errorf("Control Plane signing keys are unavailable: %w", fetchErr)
	}
	return verificationKey{}, errors.New("Control Plane token signing key is unknown")
}

type rawJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (c *jwksCache) fetch(ctx context.Context) (map[string]verificationKey, error) {
	// Detach from the caller's cancellation: one aborted request must not
	// burn the refresh window for everyone.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jwksFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, c.url, nil)
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
		return nil, errors.New("JWKS document is too large")
	}
	var set struct {
		Keys []rawJWK `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, errors.New("JWKS document is malformed")
	}
	keys := map[string]verificationKey{}
	ambiguous := map[string]bool{}
	for _, raw := range set.Keys {
		key, ok := parseJWK(raw)
		if !ok {
			continue
		}
		if _, dup := keys[raw.Kid]; dup || ambiguous[raw.Kid] {
			// Two keys under one kid: trust neither.
			delete(keys, raw.Kid)
			ambiguous[raw.Kid] = true
			continue
		}
		keys[raw.Kid] = key
	}
	return keys, nil
}

// parseJWK accepts EC P-256 (ES256) and RSA >= 2048 bit (RS256) signing keys
// and skips anything else.
func parseJWK(raw rawJWK) (verificationKey, bool) {
	if raw.Kid == "" || (raw.Use != "" && raw.Use != "sig") {
		return verificationKey{}, false
	}
	switch raw.Kty {
	case "EC":
		if raw.Crv != "P-256" || (raw.Alg != "" && raw.Alg != "ES256") {
			return verificationKey{}, false
		}
		x, errX := base64.RawURLEncoding.DecodeString(raw.X)
		y, errY := base64.RawURLEncoding.DecodeString(raw.Y)
		if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
			return verificationKey{}, false
		}
		point := append(append([]byte{4}, x...), y...)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
		if err != nil {
			return verificationKey{}, false
		}
		return verificationKey{alg: "ES256", ec: pub}, true
	case "RSA":
		if raw.Alg != "" && raw.Alg != "RS256" {
			return verificationKey{}, false
		}
		n, errN := base64.RawURLEncoding.DecodeString(raw.N)
		e, errE := base64.RawURLEncoding.DecodeString(raw.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			return verificationKey{}, false
		}
		modulus := new(big.Int).SetBytes(n)
		exponent := new(big.Int).SetBytes(e)
		if modulus.BitLen() < minRSAKeyBits || exponent.Int64() < 3 || exponent.Int64() > math.MaxInt32 || exponent.Bit(0) == 0 {
			return verificationKey{}, false
		}
		return verificationKey{alg: "RS256", rsa: &rsa.PublicKey{N: modulus, E: int(exponent.Int64())}}, true
	default:
		return verificationKey{}, false
	}
}
