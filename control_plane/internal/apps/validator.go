// Package apps implements the mini-app registry: manifest validation, immutable versions, lifecycle,
// entitlement-filtered catalog and the authorised same-origin route.
package apps

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Manifest mirrors contracts/mini_app_manifest.schema.json (workspace.app.v1).
type Manifest struct {
	SchemaVersion               string   `json:"schema_version"`
	AppKey                      string   `json:"app_key"`
	DisplayName                 string   `json:"display_name"`
	Description                 string   `json:"description"`
	Category                    string   `json:"category"`
	Icon                        string   `json:"icon"`
	OwnerTeam                   string   `json:"owner_team"`
	DeploymentMode              string   `json:"deployment_mode"`
	RouteBase                   string   `json:"route_base"`
	Environment                 string   `json:"environment"`
	Version                     string   `json:"version"`
	Status                      string   `json:"status"`
	RequiredEntitlements        []string `json:"required_entitlements"`
	WorkspaceAllowlist          []string `json:"workspace_allowlist,omitempty"`
	HealthRoute                 string   `json:"health_route,omitempty"`
	RequiresDomainAuthorization bool     `json:"requires_domain_authorization"`
	Notes                       string   `json:"notes,omitempty"`
}

type Issue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

var (
	appKeyRe      = regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`)
	iconRe        = regexp.MustCompile(`^[a-z][a-z0-9-]{1,35}$`)
	routeRe       = regexp.MustCompile(`^/apps/[a-z][a-z0-9-]{2,63}/$`)
	versionRe     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	entitlementRe = regexp.MustCompile(`^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$`)
	wsKeyRe       = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	scriptish     = regexp.MustCompile(`(?i)(<\s*/?\s*script|javascript:|data:|vbscript:|on[a-z]+\s*=|<\s*iframe|<\s*img)`)
)

// knownFields are the only accepted top-level keys (additionalProperties: false).
var knownFields = []string{"schema_version", "app_key", "display_name", "description", "category", "icon", "owner_team",
	"deployment_mode", "route_base", "environment", "version", "status", "required_entitlements", "workspace_allowlist",
	"health_route", "requires_domain_authorization", "notes"}

// ParseManifest decodes strictly: unknown fields (e.g. "upstream_url", "target") are rejected, so a manifest
// can never carry a proxy destination.
func ParseManifest(raw json.RawMessage) (Manifest, []Issue) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return Manifest{}, []Issue{{"manifest", "INVALID_JSON", "manifest must be a JSON object"}}
	}
	var issues []Issue
	for k := range generic {
		if !slices.Contains(knownFields, k) {
			issues = append(issues, Issue{k, "UNKNOWN_FIELD", "field is not allowed in workspace.app.v1 (upstream targets come from deployment config only)"})
		}
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&m); err != nil {
		issues = append(issues, Issue{"manifest", "INVALID_TYPE", "manifest fields have invalid types"})
	}
	return m, issues
}

// Validate applies the schema plus the PRD's mandatory route rules (no traversal, encoded traversal,
// protocol-relative or absolute URLs, scripts, or route/key mismatch).
func Validate(m Manifest, environment string) []Issue {
	var out []Issue
	add := func(field, code, msg string) { out = append(out, Issue{field, code, msg}) }
	if m.SchemaVersion != "workspace.app.v1" {
		add("schema_version", "UNSUPPORTED_SCHEMA", "schema_version must be workspace.app.v1")
	}
	if !appKeyRe.MatchString(m.AppKey) {
		add("app_key", "INVALID_APP_KEY", "app_key must match ^[a-z][a-z0-9-]{2,63}$")
	}
	textField := func(field, v string, min, max int) {
		n := len([]rune(v))
		if n < min || n > max {
			add(field, "INVALID_LENGTH", fmt.Sprintf("%s must be %d-%d characters", field, min, max))
		}
		if scriptish.MatchString(v) || strings.IndexFunc(v, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 {
			add(field, "UNSAFE_TEXT", field+" contains markup, script URLs or control characters")
		}
	}
	textField("display_name", m.DisplayName, 3, 80)
	textField("description", m.Description, 0, 350)
	textField("owner_team", m.OwnerTeam, 2, 100)
	textField("notes", m.Notes, 0, 400)
	if !slices.Contains([]string{"platform", "experience", "business", "admin"}, m.Category) {
		add("category", "INVALID_ENUM", "category must be platform, experience, business or admin")
	}
	if !iconRe.MatchString(m.Icon) {
		add("icon", "INVALID_ICON", "icon must be an icon name, not a URL")
	}
	if m.DeploymentMode != "same_origin_route" {
		add("deployment_mode", "UNSUPPORTED_MODE", "only same_origin_route is enabled in P0 (no iframe or external origins)")
	}
	out = append(out, ValidateRoute(m.RouteBase, m.AppKey)...)
	if m.Environment != environment {
		add("environment", "ENVIRONMENT_MISMATCH", "environment must be "+environment+" for this deployment")
	}
	if !versionRe.MatchString(m.Version) {
		add("version", "INVALID_VERSION", "version must be semantic MAJOR.MINOR.PATCH")
	}
	if m.Status != "draft" {
		add("status", "INVALID_STATUS", "new manifest versions are submitted as draft; publish is a separate step")
	}
	if len(m.RequiredEntitlements) == 0 {
		add("required_entitlements", "REQUIRED", "at least one required entitlement")
	}
	seen := map[string]bool{}
	for _, e := range m.RequiredEntitlements {
		if !entitlementRe.MatchString(e) || seen[e] {
			add("required_entitlements", "INVALID_ENTITLEMENT", "entitlements must be unique resource:action keys")
			break
		}
		seen[e] = true
	}
	seen = map[string]bool{}
	for _, w := range m.WorkspaceAllowlist {
		if !wsKeyRe.MatchString(w) || seen[w] {
			add("workspace_allowlist", "INVALID_WORKSPACE", "workspace_allowlist must contain unique workspace keys")
			break
		}
		seen[w] = true
	}
	if m.HealthRoute != "" && m.HealthRoute != "/healthz" && m.HealthRoute != "/readyz" {
		add("health_route", "INVALID_HEALTH_ROUTE", "health_route must be /healthz or /readyz")
	}
	if !m.RequiresDomainAuthorization {
		add("requires_domain_authorization", "REQUIRED_TRUE", "mini-apps must enforce their own domain authorization")
	}
	return out
}

// ValidateRoute checks a registered route base. It is defensive beyond the regex so that any future
// relaxation of the pattern still rejects traversal and redirect tricks.
func ValidateRoute(route, appKey string) []Issue {
	bad := func(code, msg string) []Issue { return []Issue{{"route_base", code, msg}} }
	lower := strings.ToLower(route)
	switch {
	case route == "":
		return bad("REQUIRED", "route_base is required")
	case strings.Contains(route, "\\") || strings.ContainsAny(route, "\r\n\t\x00 "):
		return bad("UNSAFE_ROUTE", "route_base contains forbidden characters")
	case strings.HasPrefix(route, "//") || strings.Contains(lower, "://") || strings.HasPrefix(lower, "javascript:"):
		return bad("EXTERNAL_ROUTE", "route_base must be a relative same-origin path, not a URL")
	case strings.Contains(route, "%"):
		return bad("ENCODED_ROUTE", "percent-encoded route segments are not allowed")
	case strings.Contains(route, "/../") || strings.Contains(route, "/./") || strings.HasSuffix(route, "/..") || strings.Contains(route, "//"):
		return bad("PATH_TRAVERSAL", "route_base must not contain dot segments or empty segments")
	}
	if u, err := url.Parse(route); err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
		return bad("UNSAFE_ROUTE", "route_base must be a plain path")
	}
	if !routeRe.MatchString(route) {
		return bad("INVALID_ROUTE", "route_base must match /apps/<app-key>/")
	}
	if route != "/apps/"+appKey+"/" {
		return bad("ROUTE_KEY_MISMATCH", "route_base must be /apps/<app_key>/")
	}
	return nil
}

// Canonical returns the canonical JSON (sorted keys via map round-trip) and its SHA-256 checksum.
func Canonical(m Manifest) ([]byte, string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, "", err
	}
	var generic map[string]any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil, "", err
	}
	canon, err := json.Marshal(generic) // encoding/json sorts map keys
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canon)
	return canon, hex.EncodeToString(sum[:]), nil
}
