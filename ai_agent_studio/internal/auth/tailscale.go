// Package auth maps a verified human identity (Tailscale Serve headers or a
// Control Plane delegation token) to an explicit tenant grant.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/elenio/agent-studio/internal/ports"
)

const tailscaleIssuer = "tailscale-serve"

var (
	tenantIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	actionPattern   = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
)

// AccessGrant binds one verified Tailscale login to one tenant and an explicit
// set of application actions. Network access to the host never grants these
// permissions by itself.
type AccessGrant struct {
	Login       string   `json:"login"`
	TenantID    string   `json:"tenant_id"`
	Permissions []string `json:"permissions"`
}

type grant struct {
	tenantID    string
	permissions map[string]struct{}
}

// TailscaleServe verifies identity only from headers provided by Tailscale
// Serve and then resolves tenant and action permission through an explicit
// allowlist. Deployments must keep the upstream listener on loopback and
// expose it only through Tailscale Serve; local host processes are in the
// trusted boundary because Serve's identity headers are not signed tokens.
type TailscaleServe struct {
	grants map[string]grant
}

func LoadAccessMap(path string) (*TailscaleServe, error) {
	entries, err := ReadAccessMapFile(path)
	if err != nil {
		return nil, err
	}
	return NewTailscaleServe(entries)
}

// ReadAccessMapFile decodes the access-map file shared by every human identity
// mode. The file format is identical across modes; only the meaning of
// "login" differs (a Tailscale login, or a Control Plane user id).
func ReadAccessMapFile(path string) ([]AccessGrant, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("AGENT_STUDIO_ACCESS_MAP_FILE is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read access map: %w", err)
	}
	var entries []AccessGrant
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode access map: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("access map contains multiple JSON values")
	}
	return entries, nil
}

func NewTailscaleServe(entries []AccessGrant) (*TailscaleServe, error) {
	grants, err := buildGrants(entries, func(login string) bool {
		return login != "" && !strings.ContainsAny(login, "\r\n")
	}, "a valid Tailscale login")
	if err != nil {
		return nil, err
	}
	return &TailscaleServe{grants: grants}, nil
}

// buildGrants validates access-map entries. validLogin checks the normalized
// login for the active identity mode; loginLabel names it in errors.
func buildGrants(entries []AccessGrant, validLogin func(string) bool, loginLabel string) (map[string]grant, error) {
	if len(entries) == 0 {
		return nil, errors.New("access map must contain at least one explicit grant")
	}
	grants := make(map[string]grant, len(entries))
	for i, entry := range entries {
		login := normalizeLogin(entry.Login)
		tenantID := strings.ToLower(strings.TrimSpace(entry.TenantID))
		if !validLogin(login) || !tenantIDPattern.MatchString(tenantID) {
			return nil, fmt.Errorf("access map entry %d requires %s and tenant UUID", i, loginLabel)
		}
		if len(entry.Permissions) == 0 {
			return nil, fmt.Errorf("access map entry %d requires explicit permissions", i)
		}
		if _, exists := grants[login]; exists {
			return nil, fmt.Errorf("access map contains duplicate login %q", login)
		}
		permissions := make(map[string]struct{}, len(entry.Permissions))
		for _, permission := range entry.Permissions {
			permission = strings.TrimSpace(permission)
			if !actionPattern.MatchString(permission) {
				return nil, fmt.Errorf("access map entry %d has invalid permission", i)
			}
			if _, exists := permissions[permission]; exists {
				return nil, fmt.Errorf("access map entry %d contains duplicate permission %q", i, permission)
			}
			permissions[permission] = struct{}{}
		}
		grants[login] = grant{tenantID: tenantID, permissions: permissions}
	}
	return grants, nil
}

func (a *TailscaleServe) VerifyRequest(_ context.Context, r *http.Request) (ports.Principal, error) {
	if a == nil || r == nil {
		return ports.Principal{}, errors.New("verified Tailscale request is required")
	}
	rawLogin := strings.TrimSpace(r.Header.Get("Tailscale-User-Login"))
	if rawLogin == "" {
		return ports.Principal{}, errors.New("Tailscale Serve identity header is missing")
	}
	login, err := new(mime.WordDecoder).DecodeHeader(rawLogin)
	if err != nil {
		return ports.Principal{}, errors.New("Tailscale Serve identity header is invalid")
	}
	login = normalizeLogin(login)
	access, ok := a.grants[login]
	if !ok {
		return ports.Principal{}, errors.New("Tailscale identity is not mapped to an Agent Studio tenant")
	}
	return ports.Principal{
		Issuer:   tailscaleIssuer,
		Subject:  login,
		TenantID: access.tenantID,
		Kind:     "user",
	}, nil
}

func (a *TailscaleServe) Authorize(_ context.Context, principal ports.Principal, action, resource string) error {
	if a == nil || principal.Issuer != tailscaleIssuer || principal.Subject == "" {
		return errors.New("verified Tailscale identity is required")
	}
	return authorizeGrant(a.grants, principal, action, resource)
}

// authorizeGrant applies the access-map rules once the caller has bound the
// principal to its own issuer: the subject's mapped tenant must match, the
// resource must be that tenant (or read-only on the global tool catalog) and
// the action must be explicitly granted.
func authorizeGrant(grants map[string]grant, principal ports.Principal, action, resource string) error {
	access, ok := grants[normalizeLogin(principal.Subject)]
	if !ok || access.tenantID != principal.TenantID {
		return errors.New("Agent Studio tenant access is denied")
	}
	if resource != "tenant:"+access.tenantID && resource != "global:tool-catalog" {
		return errors.New("Agent Studio resource access is denied")
	}
	if resource == "global:tool-catalog" && action != "tool.read" {
		return errors.New("Agent Studio global resource access is denied")
	}
	if _, ok := access.permissions[action]; !ok {
		return errors.New("Agent Studio action is not authorized")
	}
	return nil
}

func normalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

// PermissionsFor lists the mapped actions for a verified principal so the UI
// can hide controls; the API still authorizes every request.
func (a *TailscaleServe) PermissionsFor(principal ports.Principal) []string {
	return permissionsFor(a.grants, principal)
}

func permissionsFor(grants map[string]grant, principal ports.Principal) []string {
	access, ok := grants[normalizeLogin(principal.Subject)]
	if !ok || access.tenantID != principal.TenantID {
		return []string{}
	}
	out := make([]string, 0, len(access.permissions))
	for p := range access.permissions {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
