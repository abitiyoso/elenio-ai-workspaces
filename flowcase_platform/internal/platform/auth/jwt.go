package auth

import (
	"context"
	"crypto"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// KeySource resolves a verification key by key id (normally a JWKS cache).
type KeySource interface {
	Key(ctx context.Context, kid string) (crypto.PublicKey, error)
}

type JWTVerifierConfig struct {
	Issuer      string
	Audience    string
	TenantClaim string
	ClockSkew   time.Duration
	MaxTokenTTL time.Duration
	Keys        KeySource
	Now         func() time.Time
}

// JWTVerifier validates short-lived, audience-restricted host tokens signed with RS256 or ES256.
type JWTVerifier struct {
	cfg    JWTVerifierConfig
	parser *jwt.Parser
}

func NewJWTVerifier(cfg JWTVerifierConfig) *JWTVerifier {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg(), jwt.SigningMethodES256.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithAudience(cfg.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(cfg.ClockSkew),
		jwt.WithTimeFunc(cfg.Now),
	)
	return &JWTVerifier{cfg: cfg, parser: parser}
}

func (v *JWTVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	if v.cfg.Keys == nil || v.cfg.Issuer == "" || v.cfg.Audience == "" {
		return Claims{}, misconfigured("verifier_incomplete")
	}
	var keyErr error
	claims := jwt.MapClaims{}
	_, err := v.parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, unauthenticated("missing_kid")
		}
		key, err := v.cfg.Keys.Key(ctx, kid)
		keyErr = err
		return key, err
	})
	if err != nil {
		if keyErr != nil && errors.Is(keyErr, ErrMisconfigured) {
			return Claims{}, keyErr
		}
		return Claims{}, unauthenticated(jwtReason(err))
	}

	iat, _ := claims.GetIssuedAt()
	exp, _ := claims.GetExpirationTime()
	if iat == nil || exp == nil {
		return Claims{}, unauthenticated("missing_lifetime_claims")
	}
	if exp.Sub(iat.Time) > v.cfg.MaxTokenTTL {
		return Claims{}, unauthenticated("token_lifetime_too_long")
	}
	sub, _ := claims.GetSubject()
	iss, _ := claims.GetIssuer()
	tenant, _ := claims[v.cfg.TenantClaim].(string)
	if sub == "" || len(sub) > 256 {
		return Claims{}, unauthenticated("invalid_subject")
	}
	if tenant == "" || len(tenant) > 256 {
		return Claims{}, unauthenticated("invalid_tenant_claim")
	}
	return Claims{Issuer: iss, Subject: sub, TenantRef: tenant, IssuedAt: iat.Time, ExpiresAt: exp.Time}, nil
}

func jwtReason(err error) string {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return "token_expired"
	case errors.Is(err, jwt.ErrTokenNotValidYet), errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
		return "token_not_yet_valid"
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return "invalid_audience"
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return "invalid_issuer"
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "invalid_signature"
	case errors.Is(err, jwt.ErrTokenUnverifiable):
		return "unverifiable_token"
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return "missing_required_claim"
	case errors.Is(err, jwt.ErrTokenMalformed):
		return "malformed_token"
	}
	return "invalid_token"
}
