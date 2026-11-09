// Package auth resolves the caller identity from a host-issued token (or an opt-in development mock)
// and the Flowcase tenant directory. Identity, tenant and roles are never taken from request headers
// or bodies supplied by the browser.
package auth

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	// ErrUnauthenticated means the caller is unknown, forged, expired or not a member. Maps to 401.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrMisconfigured means the adapter cannot verify anyone (e.g. keys unavailable). Maps to 503.
	ErrMisconfigured = errors.New("authentication is not available")
)

// authError carries a machine-readable reason for logs; callers only see ErrUnauthenticated.
type authError struct {
	reason string
	base   error
}

func (e *authError) Error() string        { return e.base.Error() + ": " + e.reason }
func (e *authError) Is(target error) bool { return target == e.base }

func unauthenticated(reason string) error {
	return &authError{reason: reason, base: ErrUnauthenticated}
}
func misconfigured(reason string) error { return &authError{reason: reason, base: ErrMisconfigured} }

// Reason returns the internal reason code of an auth failure, safe to log (never contains token data).
func Reason(err error) string {
	var ae *authError
	if errors.As(err, &ae) {
		return ae.reason
	}
	return "unknown"
}

type Role string

// Roles from PRD §3. Each is scoped to a tenant (and later workspace/case) by role_assignments.
const (
	RoleWorkspaceMember   Role = "workspace_member"
	RoleCaseOwner         Role = "case_owner"
	RoleWorkflowDesigner  Role = "workflow_designer"
	RoleWorkflowPublisher Role = "workflow_publisher"
	RoleSMEContributor    Role = "sme_contributor"
	RoleDomainApprover    Role = "domain_approver"
	RoleAIOps             Role = "ai_ops"
	RoleTenantAdmin       Role = "tenant_admin"
)

var knownRoles = []Role{RoleWorkspaceMember, RoleCaseOwner, RoleWorkflowDesigner, RoleWorkflowPublisher, RoleSMEContributor, RoleDomainApprover, RoleAIOps, RoleTenantAdmin}

func ParseRole(s string) (Role, bool) {
	r := Role(s)
	return r, slices.Contains(knownRoles, r)
}

// Principal kinds. Service principals (agents, connectors) never hold approval or publish authority.
const (
	KindHuman   = "human"
	KindService = "service"
)

type Identity struct {
	UserID      string
	TenantID    string
	DisplayName string
	Kind        string
	Roles       []Role
	Issuer      string
	Subject     string
	AuthMode    string
	ExpiresAt   time.Time
	Persona     string // development personas only
	// SoDExempt marks a human whose host subject is listed in SELF_APPROVAL_EXEMPT_SUBJECTS
	// (development only): segregation-of-duties rules do not stop them approving their own work.
	SoDExempt bool
}

func (i Identity) HasRole(r Role) bool { return slices.Contains(i.Roles, r) }

// IsHuman is false for service principals and for identities whose kind was not resolved.
func (i Identity) IsHuman() bool { return i.Kind == KindHuman }

// Claims are the verified token facts, before tenant membership is resolved.
type Claims struct {
	Issuer    string
	Subject   string
	TenantRef string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// AuthAdapter authenticates a request coming through the host wrapper.
type AuthAdapter interface {
	Authenticate(ctx context.Context, r *http.Request) (Identity, error)
	Mode() string
}

// TokenVerifier validates a raw bearer token (signature, issuer, audience, lifetime).
type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (Claims, error)
}

// Directory maps verified claims to an active Flowcase tenant membership and its roles.
type Directory interface {
	Resolve(ctx context.Context, c Claims) (Identity, error)
}

type trustedAdapter struct {
	verifier  TokenVerifier
	directory Directory
	sodExempt []string
}

// NewTrustedAdapter verifies host tokens. sodExemptSubjects are host subjects exempt from
// segregation-of-duties rules (config allows them only in development).
func NewTrustedAdapter(v TokenVerifier, d Directory, sodExemptSubjects ...string) AuthAdapter {
	return trustedAdapter{verifier: v, directory: d, sodExempt: sodExemptSubjects}
}

func (trustedAdapter) Mode() string { return "trusted_jwt" }

func (a trustedAdapter) Authenticate(ctx context.Context, r *http.Request) (Identity, error) {
	if a.verifier == nil || a.directory == nil {
		return Identity{}, misconfigured("adapter_incomplete")
	}
	raw, err := BearerToken(r)
	if err != nil {
		return Identity{}, err
	}
	claims, err := a.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, err
	}
	identity, err := a.directory.Resolve(ctx, claims)
	if err != nil {
		return Identity{}, err
	}
	if identity.UserID == "" || identity.TenantID == "" {
		return Identity{}, unauthenticated("directory_incomplete")
	}
	identity.AuthMode = a.Mode()
	identity.Issuer, identity.Subject, identity.ExpiresAt = claims.Issuer, claims.Subject, claims.ExpiresAt
	identity.SoDExempt = identity.IsHuman() && slices.Contains(a.sodExempt, claims.Subject)
	return identity, nil
}

var compactJWS = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

const maxTokenLength = 8192

// BearerToken extracts the token from exactly one Authorization header. Query strings and cookies
// are deliberately not consulted.
func BearerToken(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	switch len(values) {
	case 0:
		return "", unauthenticated("missing_token")
	case 1:
	default:
		return "", unauthenticated("ambiguous_authorization")
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(values[0]), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", unauthenticated("unsupported_scheme")
	}
	if len(token) > maxTokenLength || !compactJWS.MatchString(token) {
		return "", unauthenticated("malformed_token")
	}
	return token, nil
}

// DevPersonaHeader selects one of the server-configured development personas. It is honoured only
// by the development mock adapter; trusted mode never reads it.
const DevPersonaHeader = "X-Flowcase-Dev-Persona"

// DevPersona is an additional fixed development identity in the same tenant (e.g. an approver),
// so segregation-of-duties rules can be exercised locally.
type DevPersona struct {
	Key         string `json:"key"`
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Roles       []Role `json:"roles"`
}

// devMockAdapter returns fixed identities from server configuration. Browsers can only choose among
// the configured personas by key; they can never supply a user, tenant or role. Construction is gated
// by config (development + acknowledgement).
type devMockAdapter struct {
	identity Identity
	personas map[string]DevPersona
	order    []DevPersona
}

func NewDevMockAdapter(userID, tenantID, displayName string, roles []Role, personas ...DevPersona) AuthAdapter {
	a := devMockAdapter{identity: Identity{
		UserID: userID, TenantID: tenantID, DisplayName: displayName, Kind: KindHuman,
		Roles: slices.Clone(roles), Issuer: "flowcase-dev-mock", Subject: userID, AuthMode: "dev_mock", Persona: "default",
	}, personas: map[string]DevPersona{}}
	a.order = append(a.order, DevPersona{Key: "default", UserID: userID, DisplayName: displayName, Roles: slices.Clone(roles)})
	for _, p := range personas {
		a.personas[p.Key] = p
		a.order = append(a.order, p)
	}
	return a
}

func (devMockAdapter) Mode() string { return "dev_mock" }

// Personas lists the selectable development identities (for the dev banner).
func (a devMockAdapter) Personas() []DevPersona { return slices.Clone(a.order) }

func (a devMockAdapter) Authenticate(_ context.Context, r *http.Request) (Identity, error) {
	id := a.identity
	id.Roles = slices.Clone(id.Roles)
	key := r.Header.Get(DevPersonaHeader)
	if key == "" || key == "default" {
		return id, nil
	}
	p, ok := a.personas[key]
	if !ok {
		return Identity{}, unauthenticated("unknown_dev_persona")
	}
	id.UserID, id.Subject, id.DisplayName, id.Roles, id.Persona = p.UserID, p.UserID, p.DisplayName, slices.Clone(p.Roles), p.Key
	return id, nil
}

type identityKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}
