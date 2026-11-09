package auth

import (
	"context"
	"errors"
	"net/http"
)

var (
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrVerifierNotConfigured = errors.New("trusted session verifier is not configured")
	// ErrVerifierUnavailable means the credential could not be checked (e.g. JWKS or directory outage),
	// as opposed to being invalid. The trusted adapter passes it through so callers can answer 503.
	ErrVerifierUnavailable = errors.New("trusted session verifier is temporarily unavailable")
)

type Identity struct {
	UserID   string
	TenantID string
	Roles    []string
}

// AuthAdapter resolves identity only from a trusted host session or a local development adapter.
type AuthAdapter interface {
	Authenticate(context.Context, *http.Request) (Identity, error)
}

// TrustedSessionVerifier is implemented by the host-specific signed-token or session integration.
// Implementations must verify issuer, audience, expiry, signature/key rotation and tenant membership.
type TrustedSessionVerifier interface {
	Verify(context.Context, *http.Request) (Identity, error)
}

type TrustedSessionVerifierFunc func(context.Context, *http.Request) (Identity, error)

func (f TrustedSessionVerifierFunc) Verify(ctx context.Context, r *http.Request) (Identity, error) {
	return f(ctx, r)
}

type trustedSessionAdapter struct{ verifier TrustedSessionVerifier }

func NewTrustedSessionAdapter(verifier TrustedSessionVerifier) AuthAdapter {
	return trustedSessionAdapter{verifier: verifier}
}

func (a trustedSessionAdapter) Authenticate(ctx context.Context, r *http.Request) (Identity, error) {
	if a.verifier == nil {
		return Identity{}, ErrVerifierNotConfigured
	}
	identity, err := a.verifier.Verify(ctx, r)
	if err != nil {
		if errors.Is(err, ErrVerifierUnavailable) {
			return Identity{}, ErrVerifierUnavailable
		}
		return Identity{}, ErrUnauthenticated
	}
	if identity.UserID == "" || identity.TenantID == "" {
		return Identity{}, ErrUnauthenticated
	}
	return identity, nil
}

// DevelopmentMock returns one fixed local identity. It deliberately ignores browser-supplied identity headers.
type developmentMock struct{ userID, tenantID string }

func NewDevelopmentMock(userID, tenantID string) AuthAdapter {
	return developmentMock{userID: userID, tenantID: tenantID}
}

func (a developmentMock) Authenticate(context.Context, *http.Request) (Identity, error) {
	if a.userID == "" || a.tenantID == "" {
		return Identity{}, ErrUnauthenticated
	}
	return Identity{UserID: a.userID, TenantID: a.tenantID, Roles: []string{"platform_admin"}}, nil
}
