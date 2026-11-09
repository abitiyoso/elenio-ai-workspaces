package auth

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/elenio/knowledge-workspace/backend/internal/observability"
)

const (
	maxBearerTokenBytes   = 8 << 10
	maxJWKSBytes          = 1 << 20
	unknownKidRefetchGap  = 30 * time.Second
	jwksFailureBackoff    = 2 * time.Second
	jwksFetchTimeout      = 5 * time.Second
	minRSAKeyBits         = 2048
	principalTypeHuman    = "human"
	principalStatusActive = "active"
)

var (
	// ErrPrincipalNotProvisioned is returned by a PrincipalDirectory when no row exists for the principal.
	ErrPrincipalNotProvisioned = errors.New("principal is not provisioned in this tenant")
	errUnknownKeyID            = errors.New("unknown key id")
)

// TokenRejection is a definitive "this credential is not acceptable" outcome. Reason is a stable internal code
// that is safe to log; it never contains token contents.
type TokenRejection struct{ Reason string }

func (e *TokenRejection) Error() string { return "control plane token rejected: " + e.Reason }

// PrincipalRecord is the KMS-side directory entry for a Control Plane principal.
type PrincipalRecord struct {
	Status string
	Roles  []string
}

// PrincipalDirectory resolves a verified (tenant, principal) pair to its KMS status and tenant roles.
type PrincipalDirectory interface {
	LookupPrincipal(ctx context.Context, tenantID, principalID string) (PrincipalRecord, error)
}

// SQLPrincipalDirectory reads tenant_principals (migration 000004).
type SQLPrincipalDirectory struct{ db *sql.DB }

func NewSQLPrincipalDirectory(db *sql.DB) *SQLPrincipalDirectory {
	return &SQLPrincipalDirectory{db: db}
}

func (d *SQLPrincipalDirectory) LookupPrincipal(ctx context.Context, tenantID, principalID string) (PrincipalRecord, error) {
	if d == nil || d.db == nil {
		return PrincipalRecord{}, sql.ErrConnDone
	}
	var record PrincipalRecord
	var rawRoles []byte
	err := d.db.QueryRowContext(ctx, `SELECT status,array_to_json(roles) FROM tenant_principals WHERE tenant_id=$1 AND principal_id=$2`, tenantID, principalID).Scan(&record.Status, &rawRoles)
	if errors.Is(err, sql.ErrNoRows) {
		return PrincipalRecord{}, ErrPrincipalNotProvisioned
	}
	if err != nil {
		return PrincipalRecord{}, err
	}
	if err := json.Unmarshal(rawRoles, &record.Roles); err != nil {
		return PrincipalRecord{}, fmt.Errorf("decode principal roles: %w", err)
	}
	return record, nil
}

// ControlPlaneConfig configures ControlPlaneVerifier. TenantMap maps the token's tenant key to a KMS tenant UUID.
type ControlPlaneConfig struct {
	Issuer       string
	Audience     string
	JWKSURL      string
	TenantMap    map[string]string
	MaxTokenTTL  time.Duration
	ClockSkew    time.Duration
	JWKSCacheTTL time.Duration
	Directory    PrincipalDirectory
	HTTPClient   *http.Client
	Logger       *slog.Logger
	Now          func() time.Time
}

// ControlPlaneVerifier verifies the per-request bearer token injected by the AI Workspace Control Plane BFF
// and admits only principals provisioned in tenant_principals. It implements TrustedSessionVerifier.
type ControlPlaneVerifier struct {
	issuer, audience string
	tenantMap        map[string]string
	maxTTL, skew     time.Duration
	directory        PrincipalDirectory
	keys             *jwksCache
	logger           *slog.Logger
	now              func() time.Time
}

type controlPlaneClaims struct {
	jwt.RegisteredClaims
	PrincipalType string `json:"principal_type"`
	TenantKey     string `json:"tenant_id"`
}

func NewControlPlaneVerifier(cfg ControlPlaneConfig) (*ControlPlaneVerifier, error) {
	if cfg.Issuer == "" || cfg.Audience == "" || cfg.JWKSURL == "" || len(cfg.TenantMap) == 0 || cfg.Directory == nil || cfg.MaxTokenTTL <= 0 || cfg.JWKSCacheTTL <= 0 || cfg.ClockSkew < 0 {
		return nil, errors.New("control plane verifier requires issuer, audience, JWKS URL, tenant map, principal directory, max token TTL and JWKS cache TTL")
	}
	tenantMap := make(map[string]string, len(cfg.TenantMap))
	for key, tenantID := range cfg.TenantMap {
		tenantID = strings.ToLower(tenantID)
		if key == "" || !isUUID(tenantID) {
			return nil, errors.New("control plane tenant map entries must map a key to a UUID")
		}
		tenantMap[key] = tenantID
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: jwksFetchTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &ControlPlaneVerifier{
		issuer: cfg.Issuer, audience: cfg.Audience, tenantMap: tenantMap, maxTTL: cfg.MaxTokenTTL, skew: cfg.ClockSkew,
		directory: cfg.Directory, logger: logger, now: now,
		keys: &jwksCache{url: cfg.JWKSURL, client: client, ttl: cfg.JWKSCacheTTL, now: now},
	}, nil
}

// Verify consults exactly one Authorization: Bearer header. Cookies, query strings and X-* headers are never read.
// A *TokenRejection means the credential is not acceptable; ErrVerifierUnavailable means it could not be checked.
func (v *ControlPlaneVerifier) Verify(ctx context.Context, r *http.Request) (Identity, error) {
	identity, err := v.verify(ctx, r)
	if err != nil {
		v.logOutcome(ctx, err)
	}
	return identity, err
}

func (v *ControlPlaneVerifier) verify(ctx context.Context, r *http.Request) (Identity, error) {
	raw, reason := bearerToken(r)
	if reason != "" {
		return Identity{}, &TokenRejection{Reason: reason}
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg(), jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(v.skew),
		jwt.WithTimeFunc(v.now),
	)
	claims := &controlPlaneClaims{}
	if _, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) { return v.keyFor(ctx, token) }); err != nil {
		if errors.Is(err, ErrVerifierUnavailable) {
			return Identity{}, err
		}
		return Identity{}, &TokenRejection{Reason: parseFailureReason(err)}
	}
	// Defence in depth on top of the parser options.
	if claims.Issuer != v.issuer {
		return Identity{}, &TokenRejection{Reason: "issuer_mismatch"}
	}
	if claims.ExpiresAt == nil {
		return Identity{}, &TokenRejection{Reason: "exp_missing"}
	}
	if claims.IssuedAt == nil {
		return Identity{}, &TokenRejection{Reason: "iat_missing"}
	}
	if lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time); lifetime <= 0 || lifetime > v.maxTTL {
		return Identity{}, &TokenRejection{Reason: "lifetime_exceeds_max"}
	}
	if claims.PrincipalType != principalTypeHuman {
		return Identity{}, &TokenRejection{Reason: "principal_type_not_human"}
	}
	if !isUUID(claims.Subject) {
		return Identity{}, &TokenRejection{Reason: "subject_not_uuid"}
	}
	tenantID, ok := v.tenantMap[claims.TenantKey]
	if !ok || claims.TenantKey == "" {
		return Identity{}, &TokenRejection{Reason: "tenant_not_mapped"}
	}
	userID := strings.ToLower(claims.Subject)
	record, err := v.directory.LookupPrincipal(ctx, tenantID, userID)
	if errors.Is(err, ErrPrincipalNotProvisioned) {
		return Identity{}, &TokenRejection{Reason: "principal_not_provisioned"}
	}
	if err != nil {
		return Identity{}, fmt.Errorf("%w: principal directory lookup failed: %v", ErrVerifierUnavailable, err)
	}
	if record.Status != principalStatusActive {
		return Identity{}, &TokenRejection{Reason: "principal_disabled"}
	}
	roles := make([]string, 0, len(record.Roles))
	roles = append(roles, record.Roles...)
	return Identity{UserID: userID, TenantID: tenantID, Roles: roles}, nil
}

func (v *ControlPlaneVerifier) keyFor(ctx context.Context, token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, errUnknownKeyID
	}
	key, err := v.keys.lookup(ctx, kid)
	if err != nil {
		return nil, err
	}
	alg := token.Method.Alg()
	if key.alg != "" && key.alg != alg {
		return nil, errors.New("key algorithm mismatch")
	}
	switch k := key.public.(type) {
	case *ecdsa.PublicKey:
		if alg != jwt.SigningMethodES256.Alg() {
			return nil, errors.New("key type mismatch")
		}
		return k, nil
	case *rsa.PublicKey:
		if alg != jwt.SigningMethodRS256.Alg() {
			return nil, errors.New("key type mismatch")
		}
		return k, nil
	}
	return nil, errors.New("unsupported key")
}

func (v *ControlPlaneVerifier) logOutcome(ctx context.Context, err error) {
	logger := v.logger
	if info := observability.RequestInfoFrom(ctx); info != nil && info.Logger != nil {
		logger = info.Logger
	}
	var rejection *TokenRejection
	if errors.As(err, &rejection) {
		logger.InfoContext(ctx, "control plane token rejected", "reason", rejection.Reason)
		return
	}
	// Unavailability errors are built only from JWKS/database failures, never from token contents.
	logger.WarnContext(ctx, "control plane token could not be verified", "reason", "verifier_unavailable", "error", err)
}

// bearerToken returns the compact JWS from exactly one Authorization: Bearer header, or a rejection reason.
func bearerToken(r *http.Request) (string, string) {
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		return "", "authorization_missing"
	}
	if len(values) != 1 {
		return "", "authorization_repeated"
	}
	if len(values[0]) > maxBearerTokenBytes+len("Bearer ") {
		return "", "token_too_large"
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t") {
		return "", "authorization_not_bearer"
	}
	if len(token) > maxBearerTokenBytes {
		return "", "token_too_large"
	}
	if strings.Count(token, ".") != 2 {
		return "", "token_not_compact_jws"
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return "", "token_not_compact_jws"
		}
	}
	return token, ""
}

func parseFailureReason(err error) string {
	switch {
	case errors.Is(err, errUnknownKeyID):
		return "unknown_kid"
	case errors.Is(err, jwt.ErrTokenMalformed):
		return "malformed"
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "signature_invalid"
	case errors.Is(err, jwt.ErrTokenExpired):
		return "expired"
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return "not_yet_valid"
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
		return "issued_in_future"
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return "issuer_mismatch"
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return "audience_mismatch"
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return "required_claim_missing"
	case errors.Is(err, jwt.ErrTokenUnverifiable):
		return "unverifiable"
	}
	return "invalid"
}

type verificationKey struct {
	alg    string
	public any
}

// jwksCache holds the issuer's signing keys. Keys are refreshed when the cache TTL lapses; an unknown kid
// triggers at most one refetch per unknownKidRefetchGap. A fetch failure is reported as ErrVerifierUnavailable,
// never as a bad token, and expired keys are not served after a failed refresh.
type jwksCache struct {
	url    string
	client *http.Client
	ttl    time.Duration
	now    func() time.Time

	mu          sync.Mutex
	keys        map[string]verificationKey
	fetchedAt   time.Time
	lastAttempt time.Time
	lastErr     error
}

func (c *jwksCache) lookup(ctx context.Context, kid string) (verificationKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	fresh := c.keys != nil && now.Sub(c.fetchedAt) < c.ttl
	if fresh {
		if key, ok := c.keys[kid]; ok {
			return key, nil
		}
		if now.Sub(c.lastAttempt) < unknownKidRefetchGap {
			return verificationKey{}, errUnknownKeyID
		}
	} else if c.lastErr != nil && now.Sub(c.lastAttempt) < jwksFailureBackoff {
		return verificationKey{}, fmt.Errorf("%w: JWKS refresh backing off", ErrVerifierUnavailable)
	}
	c.lastAttempt = now
	keys, err := c.fetch(ctx)
	if err != nil {
		c.lastErr = err
		if !fresh {
			c.keys = nil
		}
		// When the cache is still fresh, known keys stay valid until their TTL; only this kid is unresolved.
		return verificationKey{}, fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	c.keys, c.fetchedAt, c.lastErr = keys, now, nil
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return verificationKey{}, errUnknownKeyID
}

type jsonWebKey struct {
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
	// Detach from the caller's cancellation so one aborted request does not poison the shared cache.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jwksFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build JWKS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch JWKS: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read JWKS: %w", err)
	}
	if len(body) > maxJWKSBytes {
		return nil, errors.New("JWKS document is too large")
	}
	var set struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}
	keys := map[string]verificationKey{}
	duplicate := map[string]bool{}
	for _, jwk := range set.Keys {
		if jwk.Kid == "" || (jwk.Use != "" && jwk.Use != "sig") {
			continue
		}
		public, alg, ok := parseJWK(jwk)
		if !ok {
			continue
		}
		if _, seen := keys[jwk.Kid]; seen || duplicate[jwk.Kid] {
			// An ambiguous kid cannot be trusted to select one key.
			delete(keys, jwk.Kid)
			duplicate[jwk.Kid] = true
			continue
		}
		keys[jwk.Kid] = verificationKey{alg: alg, public: public}
	}
	if len(keys) == 0 {
		return nil, errors.New("JWKS contains no usable ES256 or RS256 signing keys")
	}
	return keys, nil
}

func parseJWK(jwk jsonWebKey) (any, string, bool) {
	switch jwk.Kty {
	case "EC":
		if jwk.Crv != "P-256" || (jwk.Alg != "" && jwk.Alg != "ES256") {
			return nil, "", false
		}
		x, errX := base64.RawURLEncoding.DecodeString(jwk.X)
		y, errY := base64.RawURLEncoding.DecodeString(jwk.Y)
		if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
			return nil, "", false
		}
		// crypto/ecdh rejects points that are not on the curve.
		if _, err := ecdh.P256().NewPublicKey(append(append([]byte{4}, x...), y...)); err != nil {
			return nil, "", false
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, "ES256", true
	case "RSA":
		if jwk.Alg != "" && jwk.Alg != "RS256" {
			return nil, "", false
		}
		n, errN := base64.RawURLEncoding.DecodeString(jwk.N)
		e, errE := base64.RawURLEncoding.DecodeString(jwk.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			return nil, "", false
		}
		modulus := new(big.Int).SetBytes(n)
		exponent := int(new(big.Int).SetBytes(e).Int64())
		if modulus.BitLen() < minRSAKeyBits || exponent < 3 || exponent%2 == 0 {
			return nil, "", false
		}
		return &rsa.PublicKey{N: modulus, E: exponent}, "RS256", true
	}
	return nil, "", false
}
