package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/elenio/agent-studio/internal/ports"
)

var (
	clientIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,62}$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// Service clients may only invoke and observe runs; never author, review or release.
	serviceActions = map[string]bool{"run.invoke": true, "run.read": true, "run.cancel": true}
)

// ServiceGrant binds one opaque bearer credential (stored only as SHA-256) to
// a tenant, an expiry and run-only actions.
type ServiceGrant struct {
	ClientID    string    `json:"client_id"`
	TokenSHA256 string    `json:"token_sha256"`
	TenantID    string    `json:"tenant_id"`
	Permissions []string  `json:"permissions"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type serviceGrant struct {
	clientID    string
	digest      []byte
	tenantID    string
	permissions map[string]bool
	expiresAt   time.Time
}

type ServiceTokens struct {
	grants []serviceGrant
	now    func() time.Time
}

func LoadServiceTokens(path string) (*ServiceTokens, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read service token file: %w", err)
	}
	var entries []ServiceGrant
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode service token file: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("service token file contains multiple JSON values")
	}
	return NewServiceTokens(entries, time.Now)
}

func NewServiceTokens(entries []ServiceGrant, now func() time.Time) (*ServiceTokens, error) {
	tokens := &ServiceTokens{now: now}
	seen := map[string]bool{}
	for i, e := range entries {
		tenantID := strings.ToLower(strings.TrimSpace(e.TenantID))
		digest := strings.ToLower(strings.TrimSpace(e.TokenSHA256))
		if !clientIDPattern.MatchString(e.ClientID) || !tenantIDPattern.MatchString(tenantID) || !sha256Pattern.MatchString(digest) || e.ExpiresAt.IsZero() {
			return nil, fmt.Errorf("service grant %d requires client_id, tenant UUID, token_sha256 and expires_at", i)
		}
		if seen[e.ClientID] || seen[digest] {
			return nil, fmt.Errorf("service grant %d duplicates a client or token", i)
		}
		seen[e.ClientID], seen[digest] = true, true
		perms := map[string]bool{}
		for _, p := range e.Permissions {
			if !serviceActions[p] {
				return nil, fmt.Errorf("service grant %d requests non-run permission %q", i, p)
			}
			perms[p] = true
		}
		raw, _ := hex.DecodeString(digest)
		tokens.grants = append(tokens.grants, serviceGrant{clientID: e.ClientID, digest: raw, tenantID: tenantID, permissions: perms, expiresAt: e.ExpiresAt})
	}
	return tokens, nil
}

func (s *ServiceTokens) verify(r *http.Request) (ports.Principal, error) {
	if s == nil {
		return ports.Principal{}, errors.New("service credentials are not configured")
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) < 32 {
		return ports.Principal{}, errors.New("bearer service credential is required")
	}
	sum := sha256.Sum256([]byte(parts[1]))
	for _, g := range s.grants {
		if subtle.ConstantTimeCompare(sum[:], g.digest) == 1 {
			if !s.now().Before(g.expiresAt) {
				return ports.Principal{}, errors.New("service credential expired")
			}
			return ports.Principal{Issuer: "service-token", Subject: "service:" + g.clientID, TenantID: g.tenantID, Kind: "service"}, nil
		}
	}
	return ports.Principal{}, errors.New("service credential is not recognized")
}

func (s *ServiceTokens) authorize(p ports.Principal, action, resource string) error {
	if s == nil {
		return errors.New("service credentials are not configured")
	}
	for _, g := range s.grants {
		if "service:"+g.clientID == p.Subject && g.tenantID == p.TenantID {
			if !s.now().Before(g.expiresAt) || resource != "tenant:"+g.tenantID || !g.permissions[action] {
				return errors.New("service action is not authorized")
			}
			return nil
		}
	}
	return errors.New("service action is not authorized")
}

// HumanIdentity is the adapter that verifies people (Tailscale Serve, the
// development stand-in, or Control Plane delegation tokens).
type HumanIdentity interface {
	ports.AuthAdapter
	ports.Authorizer
}

// Composite routes Authorization: Bearer requests to service credentials and
// everything else to the human adapter. A bearer request never falls back to
// the human path, so the development identity cannot be borrowed by a caller
// presenting a bad token. A human adapter that is itself bearer-based (the
// Control Plane JWT mode) claims its own token shape first; the claimed
// request never falls back to service credentials either.
type Composite struct {
	Human   HumanIdentity
	Service *ServiceTokens
}

// bearerHuman is implemented by human adapters that authenticate with an
// Authorization header.
type bearerHuman interface {
	handlesBearer(*http.Request) bool
}

func (c Composite) VerifyRequest(ctx context.Context, r *http.Request) (ports.Principal, error) {
	if bh, ok := c.Human.(bearerHuman); ok && bh.handlesBearer(r) {
		return c.Human.VerifyRequest(ctx, r)
	}
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return c.Service.verify(r)
	}
	return c.Human.VerifyRequest(ctx, r)
}

func (c Composite) Authorize(ctx context.Context, p ports.Principal, action, resource string) error {
	if p.Kind == "service" {
		return c.Service.authorize(p, action, resource)
	}
	return c.Human.Authorize(ctx, p, action, resource)
}

// IsDevelopment reports whether the human adapter is the development stand-in.
func (c Composite) IsDevelopment() bool {
	_, ok := c.Human.(*DevIdentity)
	return ok
}

// IdentityMode names the human identity mode for the session endpoint.
func (c Composite) IdentityMode() string {
	switch c.Human.(type) {
	case *DevIdentity:
		return "development"
	case *ControlPlaneJWT:
		return "control-plane"
	default:
		return "tailscale-serve"
	}
}

func (c Composite) PermissionsFor(p ports.Principal) []string {
	if lister, ok := c.Human.(interface {
		PermissionsFor(ports.Principal) []string
	}); ok && p.Kind != "service" {
		return lister.PermissionsFor(p)
	}
	return []string{}
}

// ClientCount reports how many service clients are configured.
func (s *ServiceTokens) ClientCount() int {
	if s == nil {
		return 0
	}
	return len(s.grants)
}
