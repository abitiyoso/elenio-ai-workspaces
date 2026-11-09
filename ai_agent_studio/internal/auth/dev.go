package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/elenio/agent-studio/internal/ports"
)

const (
	EnvironmentProduction  = "production"
	EnvironmentDevelopment = "development"
)

// DevIdentity is a local development stand-in for Tailscale Serve. It
// substitutes one configured login for the missing Serve header, then defers
// tenant and action checks to the same explicit access map. It is constructed
// only in the development environment so production fails closed.
type DevIdentity struct {
	access *TailscaleServe
	login  string
}

func NewDevIdentity(environment string, access *TailscaleServe, login string) (*DevIdentity, error) {
	if environment != EnvironmentDevelopment {
		return nil, errors.New("development identity requires AGENT_STUDIO_ENV=development")
	}
	login = normalizeLogin(login)
	if access == nil || login == "" || strings.ContainsAny(login, "\r\n") {
		return nil, errors.New("development identity requires an access map and AGENT_STUDIO_DEV_LOGIN")
	}
	if _, ok := access.grants[login]; !ok {
		return nil, errors.New("AGENT_STUDIO_DEV_LOGIN is not mapped in the access map")
	}
	return &DevIdentity{access: access, login: login}, nil
}

func (d *DevIdentity) VerifyRequest(ctx context.Context, r *http.Request) (ports.Principal, error) {
	if d == nil || r == nil {
		return ports.Principal{}, errors.New("verified development request is required")
	}
	// Clone so a client-supplied header can never select another mapped login.
	verified := r.Clone(ctx)
	verified.Header.Set("Tailscale-User-Login", d.login)
	return d.access.VerifyRequest(ctx, verified)
}

func (d *DevIdentity) Authorize(ctx context.Context, principal ports.Principal, action, resource string) error {
	if d == nil {
		return errors.New("verified development identity is required")
	}
	return d.access.Authorize(ctx, principal, action, resource)
}

// Environment validates AGENT_STUDIO_ENV; an empty value means production.
func Environment(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", EnvironmentProduction:
		return EnvironmentProduction, nil
	case EnvironmentDevelopment:
		return EnvironmentDevelopment, nil
	default:
		return "", errors.New("AGENT_STUDIO_ENV must be production or development")
	}
}

func (d *DevIdentity) PermissionsFor(principal ports.Principal) []string {
	return d.access.PermissionsFor(principal)
}
