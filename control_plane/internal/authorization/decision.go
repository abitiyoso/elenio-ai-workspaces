// Package authorization is the default-deny platform entitlement engine. It answers only "may this verified
// principal discover/open app X or administer platform resource Y in workspace Z"; domain authorization
// (documents, cases, approvals, agent releases) stays inside each mini-app.
package authorization

import (
	"slices"
	"sort"
	"time"

	"github.com/elenio/control-plane/internal/platform/secrets"
)

// Reason codes (contracts/authorization_decision.schema.json plus WORKSPACE_NOT_ALLOWED / OVERRIDE_ALLOW).
const (
	ReasonRoleGrant           = "ROLE_GRANT"
	ReasonOverrideAllow       = "OVERRIDE_ALLOW"
	ReasonExplicitDeny        = "EXPLICIT_DENY"
	ReasonNotMember           = "NOT_MEMBER"
	ReasonMembershipSuspended = "MEMBERSHIP_SUSPENDED"
	ReasonAppDisabled         = "APP_DISABLED"
	ReasonAppUnpublished      = "APP_UNPUBLISHED"
	ReasonMissingPermission   = "MISSING_PERMISSION"
	ReasonSubjectDisabled     = "SUBJECT_DISABLED"
	ReasonWrongAudience       = "WRONG_AUDIENCE"
	ReasonPolicyError         = "POLICY_ERROR"
	ReasonWorkspaceNotAllowed = "WORKSPACE_NOT_ALLOWED"
)

const (
	ResourceMiniApp       = "mini_app"
	ResourceWorkspace     = "workspace"
	ResourcePlatformAdmin = "platform_admin"
	ActionAppOpen         = "app:open"
	ActionWorkspaceSwitch = "workspace:switch"
)

type TraceStep struct {
	Step    string `json:"step"`
	Outcome string `json:"outcome"` // pass | fail | info
	Detail  string `json:"detail"`
}

type Decision struct {
	DecisionID         string      `json:"decision_id"`
	Effect             string      `json:"effect"`
	ReasonCode         string      `json:"reason_code"`
	SubjectID          string      `json:"subject_id"`
	TenantID           string      `json:"tenant_id"`
	WorkspaceID        string      `json:"workspace_id"`
	ResourceType       string      `json:"resource_type"`
	ResourceID         string      `json:"resource_id"`
	Action             string      `json:"action"`
	PolicyVersion      string      `json:"policy_version"`
	EffectiveRoles     []string    `json:"effective_roles"`
	MissingPermissions []string    `json:"missing_permissions"`
	EvaluatedAt        time.Time   `json:"evaluated_at"`
	CorrelationID      string      `json:"correlation_id"`
	Trace              []TraceStep `json:"trace,omitempty"`
}

func (d Decision) Allowed() bool { return d.Effect == "allow" }

type BoundRole struct {
	Key         string
	Scope       string // tenant | workspace
	Permissions []string
}

type Override struct {
	Permission string
	Effect     string // allow | deny
}

// Facts are everything the engine needs, loaded fresh from PostgreSQL per decision (no positive caching,
// so revocations apply on the next request).
type Facts struct {
	UserID          string
	TenantID        string
	UserStatus      string
	PrincipalType   string
	WorkspaceID     string
	WorkspaceKey    string
	WorkspaceStatus string // "" when the workspace does not exist in this tenant
	MembershipState string // "" when there is no live membership
	TenantRoles     []BoundRole
	WorkspaceRoles  []BoundRole
	Overrides       []Override
	PolicyVersion   string
	SensitivePerms  map[string]bool
}

type App struct {
	Key                  string
	Status               string // draft | published | disabled | retired
	HasPublishedVersion  bool
	RequiredEntitlements []string
	WorkspaceAllowlist   []string
}

type builder struct {
	d     Decision
	trace []TraceStep
}

func newDecision(f Facts, resourceType, resourceID, action, correlationID string, now time.Time) *builder {
	return &builder{d: Decision{
		DecisionID: "dec_" + secrets.RandomToken(12), SubjectID: f.UserID, TenantID: f.TenantID, WorkspaceID: f.WorkspaceID,
		ResourceType: resourceType, ResourceID: resourceID, Action: action, PolicyVersion: f.PolicyVersion,
		EffectiveRoles: []string{}, MissingPermissions: []string{}, EvaluatedAt: now.UTC(), CorrelationID: correlationID,
	}}
}

func (b *builder) step(step, outcome, detail string) {
	b.trace = append(b.trace, TraceStep{step, outcome, detail})
}

func (b *builder) finish(effect, reason string) Decision {
	b.d.Effect, b.d.ReasonCode, b.d.Trace = effect, reason, b.trace
	return b.d
}

// roleKeys lists the roles that contribute in this context.
func roleKeys(roles ...[]BoundRole) []string {
	var out []string
	for _, rs := range roles {
		for _, r := range rs {
			if !slices.Contains(out, r.Key) {
				out = append(out, r.Key)
			}
		}
	}
	sort.Strings(out)
	return out
}

func permSet(roles ...[]BoundRole) map[string]bool {
	out := map[string]bool{}
	for _, rs := range roles {
		for _, r := range rs {
			for _, p := range r.Permissions {
				out[p] = true
			}
		}
	}
	return out
}

// subjectAndMembership runs the shared checks: active subject, existing workspace, live active membership.
func subjectAndMembership(b *builder, f Facts) (string, bool) {
	if f.UserStatus != "active" {
		b.step("subject", "fail", "user is "+orUnknown(f.UserStatus))
		return ReasonSubjectDisabled, false
	}
	b.step("subject", "pass", "user is active")
	if f.WorkspaceStatus != "active" {
		b.step("membership", "fail", "workspace not found or not active")
		return ReasonNotMember, false
	}
	switch f.MembershipState {
	case "active":
		b.step("membership", "pass", "active membership in "+f.WorkspaceKey)
		return "", true
	case "suspended":
		b.step("membership", "fail", "membership in "+f.WorkspaceKey+" is suspended")
		return ReasonMembershipSuspended, false
	default:
		b.step("membership", "fail", "no active membership in "+f.WorkspaceKey)
		return ReasonNotMember, false
	}
}

// EffectivePermissions returns workspace permissions after role union and explicit overrides (deny wins).
func EffectivePermissions(f Facts) []string {
	perms := permSet(f.TenantRoles, f.WorkspaceRoles)
	for _, o := range f.Overrides {
		if o.Effect == "allow" && !f.SensitivePerms[o.Permission] {
			perms[o.Permission] = true
		}
	}
	for _, o := range f.Overrides {
		if o.Effect == "deny" {
			delete(perms, o.Permission)
		}
	}
	out := make([]string, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// DecideApp evaluates app:open for a mini-app in the facts' workspace.
func DecideApp(f Facts, app *App, correlationID string, now time.Time) Decision {
	key := ""
	if app != nil {
		key = app.Key
	}
	b := newDecision(f, ResourceMiniApp, key, ActionAppOpen, correlationID, now)
	if reason, ok := subjectAndMembership(b, f); !ok {
		return b.finish("deny", reason)
	}
	if app == nil {
		b.step("app", "fail", "app is not registered") // reported like unpublished: no registry enumeration
		return b.finish("deny", ReasonAppUnpublished)
	}
	switch {
	case app.Status == "disabled" || app.Status == "retired":
		b.step("app", "fail", "app is "+app.Status)
		return b.finish("deny", ReasonAppDisabled)
	case app.Status != "published" || !app.HasPublishedVersion:
		b.step("app", "fail", "app has no published version")
		return b.finish("deny", ReasonAppUnpublished)
	}
	b.step("app", "pass", "app is published")
	if len(app.WorkspaceAllowlist) > 0 && !slices.Contains(app.WorkspaceAllowlist, f.WorkspaceKey) {
		b.step("workspace_allowlist", "fail", "workspace "+f.WorkspaceKey+" is not in the app allowlist")
		return b.finish("deny", ReasonWorkspaceNotAllowed)
	}
	b.d.EffectiveRoles = roleKeys(f.TenantRoles, f.WorkspaceRoles)
	fromRoles := permSet(f.TenantRoles, f.WorkspaceRoles)
	b.step("roles", "info", "effective roles: "+joinOrNone(b.d.EffectiveRoles))
	for _, o := range f.Overrides {
		if o.Effect == "deny" && slices.Contains(app.RequiredEntitlements, o.Permission) {
			b.step("overrides", "fail", "explicit deny on "+o.Permission)
			b.d.MissingPermissions = []string{o.Permission}
			return b.finish("deny", ReasonExplicitDeny)
		}
	}
	effective := EffectivePermissions(f)
	usedOverride := false
	for _, req := range app.RequiredEntitlements {
		if !slices.Contains(effective, req) {
			b.d.MissingPermissions = append(b.d.MissingPermissions, req)
		} else if !fromRoles[req] {
			usedOverride = true
		}
	}
	if len(b.d.MissingPermissions) > 0 {
		b.step("entitlements", "fail", "missing "+joinOrNone(b.d.MissingPermissions))
		return b.finish("deny", ReasonMissingPermission)
	}
	if usedOverride {
		b.step("entitlements", "pass", "required entitlements satisfied with an allow override")
		return b.finish("allow", ReasonOverrideAllow)
	}
	b.step("entitlements", "pass", "required entitlements granted by roles")
	return b.finish("allow", ReasonRoleGrant)
}

// DecideWorkspaceSwitch requires an active subject and an active membership.
func DecideWorkspaceSwitch(f Facts, correlationID string, now time.Time) Decision {
	b := newDecision(f, ResourceWorkspace, f.WorkspaceID, ActionWorkspaceSwitch, correlationID, now)
	if reason, ok := subjectAndMembership(b, f); !ok {
		return b.finish("deny", reason)
	}
	b.d.EffectiveRoles = roleKeys(f.TenantRoles, f.WorkspaceRoles)
	return b.finish("allow", ReasonRoleGrant)
}

// DecideTenantPermission evaluates a tenant-scoped platform permission (registry, audit, users...).
func DecideTenantPermission(f Facts, perm, correlationID string, now time.Time) Decision {
	b := newDecision(f, ResourcePlatformAdmin, perm, perm, correlationID, now)
	b.d.WorkspaceID = ""
	if f.UserStatus != "active" {
		b.step("subject", "fail", "user is "+orUnknown(f.UserStatus))
		return b.finish("deny", ReasonSubjectDisabled)
	}
	if f.PrincipalType != "human" {
		b.step("subject", "fail", "service principals cannot hold platform administration")
		return b.finish("deny", ReasonMissingPermission)
	}
	b.d.EffectiveRoles = roleKeys(f.TenantRoles)
	if permSet(f.TenantRoles)[perm] {
		return b.finish("allow", ReasonRoleGrant)
	}
	b.d.MissingPermissions = []string{perm}
	return b.finish("deny", ReasonMissingPermission)
}

// DecideWorkspacePermission evaluates an administrative permission in one workspace: granted by a tenant
// role, or by a workspace role held through an active membership. Explicit denies apply.
func DecideWorkspacePermission(f Facts, perm, correlationID string, now time.Time) Decision {
	b := newDecision(f, ResourceWorkspace, f.WorkspaceID, perm, correlationID, now)
	if f.UserStatus != "active" {
		b.step("subject", "fail", "user is "+orUnknown(f.UserStatus))
		return b.finish("deny", ReasonSubjectDisabled)
	}
	if f.PrincipalType != "human" {
		return b.finish("deny", ReasonMissingPermission)
	}
	if f.WorkspaceStatus == "" {
		b.step("workspace", "fail", "workspace not found in tenant")
		return b.finish("deny", ReasonNotMember)
	}
	for _, o := range f.Overrides {
		if o.Effect == "deny" && o.Permission == perm {
			b.d.MissingPermissions = []string{perm}
			return b.finish("deny", ReasonExplicitDeny)
		}
	}
	if permSet(f.TenantRoles)[perm] {
		b.d.EffectiveRoles = roleKeys(f.TenantRoles)
		return b.finish("allow", ReasonRoleGrant)
	}
	if f.MembershipState == "active" && permSet(f.WorkspaceRoles)[perm] {
		b.d.EffectiveRoles = roleKeys(f.WorkspaceRoles)
		return b.finish("allow", ReasonRoleGrant)
	}
	b.d.EffectiveRoles = roleKeys(f.TenantRoles, f.WorkspaceRoles)
	b.d.MissingPermissions = []string{perm}
	if f.MembershipState == "suspended" {
		return b.finish("deny", ReasonMembershipSuspended)
	}
	return b.finish("deny", ReasonMissingPermission)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	out := s[0]
	for _, x := range s[1:] {
		out += ", " + x
	}
	return out
}
