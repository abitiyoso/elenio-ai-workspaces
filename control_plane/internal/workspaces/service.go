// Package workspaces implements the directory, workspace, membership, role-binding and override services.
// Every method re-authorizes the verified actor with the decision engine; handlers and UI guards are not
// the authority.
package workspaces

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/identity/session"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/platform/postgres"
)

const grantRequestTTL = 72 * time.Hour

type Service struct {
	Pool     *pgxpool.Pool
	Engine   *authorization.Engine
	Sessions *session.Store
}

// ---------- shared types ----------

type Workspace struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	MemberCount *int   `json:"member_count,omitempty"`
}

type RoleBinding struct {
	ID          string     `json:"id"`
	RoleKey     string     `json:"role_key"`
	WorkspaceID *string    `json:"workspace_id"`
	State       string     `json:"state"`
	ExpiresAt   *time.Time `json:"expires_at"`
	GrantedBy   *string    `json:"granted_by"`
	Reason      string     `json:"reason"`
	CreatedAt   time.Time  `json:"created_at"`
}

type MemberUser struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email"`
	Status      string  `json:"status"`
}

type Membership struct {
	ID            string        `json:"id"`
	WorkspaceID   string        `json:"workspace_id"`
	WorkspaceKey  string        `json:"workspace_key"`
	WorkspaceName string        `json:"workspace_name"`
	UserID        string        `json:"user_id"`
	State         string        `json:"state"`
	JoinedAt      time.Time     `json:"joined_at"`
	Roles         []RoleBinding `json:"roles"`
	User          *MemberUser   `json:"user,omitempty"`
}

type DirectoryUser struct {
	ID            string        `json:"id"`
	DisplayName   string        `json:"display_name"`
	Email         *string       `json:"email"`
	IdPIssuer     string        `json:"idp_issuer"`
	IdPSubject    string        `json:"idp_subject"`
	PrincipalType string        `json:"principal_type"`
	Status        string        `json:"status"`
	LastLoginAt   *time.Time    `json:"last_login_at"`
	TenantRoles   []RoleBinding `json:"tenant_roles"`
	Memberships   []Membership  `json:"memberships"`
}

type GrantRequest struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	UserName        string     `json:"user_name"`
	WorkspaceID     *string    `json:"workspace_id"`
	WorkspaceName   *string    `json:"workspace_name"`
	RoleKey         string     `json:"role_key"`
	RequestedBy     string     `json:"requested_by"`
	RequestedByName string     `json:"requested_by_name"`
	Reason          string     `json:"reason"`
	State           string     `json:"state"`
	DecidedBy       *string    `json:"decided_by"`
	DecisionReason  *string    `json:"decision_reason"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	BindingExpires  *time.Time `json:"-"`
	TenantID        string     `json:"-"`
}

type GrantResult struct {
	Outcome string        `json:"outcome"`
	Binding *RoleBinding  `json:"binding,omitempty"`
	Request *GrantRequest `json:"request,omitempty"`
}

type Override struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	UserID        string     `json:"user_id"`
	UserName      string     `json:"user_name"`
	PermissionKey string     `json:"permission_key"`
	Effect        string     `json:"effect"`
	Reason        string     `json:"reason"`
	ExpiresAt     *time.Time `json:"expires_at"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Role struct {
	Key         string   `json:"key"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Scope       string   `json:"scope"`
	Sensitive   bool     `json:"sensitive"`
	Permissions []string `json:"permissions"`
}

type Permission struct {
	Key          string `json:"key"`
	ResourceType string `json:"resource_type"`
	Action       string `json:"action"`
	Description  string `json:"description"`
	Sensitive    bool   `json:"sensitive"`
}

// ---------- authorization helpers ----------

func cid(ctx context.Context) string { return observability.RequestIDFrom(ctx) }

// requireTenant enforces a tenant permission and audits denials.
func (s *Service) requireTenant(ctx context.Context, p principal.Principal, perm string) error {
	d, err := s.Engine.Tenant(ctx, nil, p.UserID, perm, cid(ctx))
	if err != nil {
		return err
	}
	if !d.Allowed() {
		s.auditDeny(ctx, p, "", d)
		return apperr.Forbidden(d.ReasonCode, "not permitted: "+perm)
	}
	return nil
}

func (s *Service) requireWorkspace(ctx context.Context, p principal.Principal, workspaceID, perm string) error {
	d, err := s.Engine.Workspace(ctx, nil, p.UserID, workspaceID, perm, cid(ctx))
	if err != nil {
		return err
	}
	if !d.Allowed() {
		s.auditDeny(ctx, p, workspaceID, d)
		// Unknown/foreign workspaces and missing permission look the same to the caller.
		return apperr.Forbidden(d.ReasonCode, "not permitted: "+perm)
	}
	return nil
}

func (s *Service) auditDeny(ctx context.Context, p principal.Principal, workspaceID string, d authorization.Decision) {
	_ = audit.Record(ctx, s.Pool, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: validUUIDOrEmpty(workspaceID),
		Action: "authz.denied", TargetType: d.ResourceType, TargetID: d.ResourceID, Decision: "deny", ReasonCode: d.ReasonCode,
		PolicyVersion: d.PolicyVersion, Metadata: map[string]any{"action": d.Action, "missing_permissions": d.MissingPermissions}})
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidUUID(s string) bool { return uuidPattern.MatchString(s) }

func validUUIDOrEmpty(s string) string {
	if ValidUUID(s) {
		return s
	}
	return ""
}

// ManagedWorkspaces lists workspace ids where the actor holds memberships:manage.
func (s *Service) ManagedWorkspaces(ctx context.Context, p principal.Principal) ([]string, error) {
	d, err := s.Engine.Tenant(ctx, nil, p.UserID, "memberships:manage", cid(ctx))
	if err != nil {
		return nil, err
	}
	var rows pgx.Rows
	if d.Allowed() {
		rows, err = s.Pool.Query(ctx, `SELECT id::text FROM workspaces WHERE tenant_id = $1::uuid AND status = 'active' ORDER BY name`, p.TenantID)
	} else {
		rows, err = s.Pool.Query(ctx, `
SELECT DISTINCT w.id::text, w.name FROM workspaces w
JOIN workspace_memberships m ON m.workspace_id = w.id AND m.user_id = $2::uuid AND m.state = 'active'
JOIN role_bindings b ON b.workspace_id = w.id AND b.user_id = $2::uuid AND b.state = 'active' AND (b.expires_at IS NULL OR b.expires_at > now())
JOIN role_permissions rp ON rp.role_id = b.role_id AND rp.permission_key = 'memberships:manage'
JOIN users u ON u.id = $2::uuid AND u.status = 'active'
WHERE w.tenant_id = $1::uuid AND w.status = 'active'
  AND NOT EXISTS (SELECT 1 FROM entitlement_overrides o WHERE o.workspace_id = w.id AND o.user_id = $2::uuid
                  AND o.permission_key = 'memberships:manage' AND o.effect = 'deny' AND o.revoked_at IS NULL
                  AND (o.expires_at IS NULL OR o.expires_at > now()))
ORDER BY w.name`, p.TenantID, p.UserID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		var name *string
		if d.Allowed() {
			err = rows.Scan(&id)
		} else {
			err = rows.Scan(&id, &name)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---------- session-facing ----------

// UserWorkspaces lists active workspaces where the user has an active membership.
func (s *Service) UserWorkspaces(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := s.Pool.Query(ctx, `SELECT w.id::text, w.key, w.name, w.description, w.status FROM workspaces w
JOIN workspace_memberships m ON m.workspace_id = w.id AND m.user_id = $1::uuid AND m.state = 'active'
JOIN users u ON u.id = m.user_id AND u.status = 'active'
WHERE w.status = 'active' ORDER BY w.name`, userID)
	if err != nil {
		return nil, err
	}
	return collectWorkspaces(rows)
}

func collectWorkspaces(rows pgx.Rows) ([]Workspace, error) {
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Key, &w.Name, &w.Description, &w.Status); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SwitchWorkspace verifies membership server-side, stores the choice on the session and audits it.
func (s *Service) SwitchWorkspace(ctx context.Context, p principal.Principal, workspaceID string) error {
	if !ValidUUID(workspaceID) {
		return apperr.Forbidden(authorization.ReasonNotMember, "workspace not available")
	}
	d, err := s.Engine.Switch(ctx, p.UserID, workspaceID, cid(ctx))
	if err != nil {
		return err
	}
	if !d.Allowed() {
		s.auditDeny(ctx, p, workspaceID, d)
		return apperr.Forbidden(d.ReasonCode, "workspace not available")
	}
	return postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Sessions.SetWorkspace(ctx, tx, p.SessionID, workspaceID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: workspaceID,
			Action: "session.workspace_switched", TargetType: "workspace", TargetID: workspaceID, Decision: "allow",
			ReasonCode: d.ReasonCode, PolicyVersion: d.PolicyVersion, Metadata: map[string]any{"from_workspace_id": p.WorkspaceID}})
	})
}

// ---------- directory ----------

const userSelect = `SELECT u.id::text, u.display_name, u.email, u.idp_issuer, u.idp_subject, u.principal_type, u.status, u.last_login_at FROM users u`

func (s *Service) ListUsers(ctx context.Context, p principal.Principal, q string) ([]DirectoryUser, error) {
	all, err := s.Engine.Tenant(ctx, nil, p.UserID, "users:manage", cid(ctx))
	if err != nil {
		return nil, err
	}
	var scope []string
	if !all.Allowed() {
		scope, err = s.ManagedWorkspaces(ctx, p)
		if err != nil {
			return nil, err
		}
		if len(scope) == 0 {
			s.auditDeny(ctx, p, "", all)
			return nil, apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted: users:read")
		}
	}
	q = strings.TrimSpace(q)
	if len(q) > 100 {
		q = q[:100]
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
	rows, err := s.Pool.Query(ctx, userSelect+`
WHERE u.tenant_id = $1::uuid AND (lower(u.display_name) LIKE $2 OR lower(COALESCE(u.email, '')) LIKE $2)
  AND ($3::uuid[] IS NULL OR EXISTS (SELECT 1 FROM workspace_memberships m WHERE m.user_id = u.id AND m.state <> 'revoked' AND m.workspace_id = ANY($3::uuid[])))
ORDER BY u.display_name LIMIT 200`, p.TenantID, pattern, nullableSlice(scope, all.Allowed()))
	if err != nil {
		return nil, err
	}
	users, err := scanUsers(rows)
	if err != nil {
		return nil, err
	}
	for i := range users {
		if err := s.fillUser(ctx, &users[i], scope, all.Allowed()); err != nil {
			return nil, err
		}
	}
	return users, nil
}

func nullableSlice(s []string, all bool) any {
	if all {
		return nil
	}
	return s
}

func scanUsers(rows pgx.Rows) ([]DirectoryUser, error) {
	defer rows.Close()
	out := []DirectoryUser{}
	for rows.Next() {
		var u DirectoryUser
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Email, &u.IdPIssuer, &u.IdPSubject, &u.PrincipalType, &u.Status, &u.LastLoginAt); err != nil {
			return nil, err
		}
		u.TenantRoles, u.Memberships = []RoleBinding{}, []Membership{}
		out = append(out, u)
	}
	return out, rows.Err()
}

const bindingSelect = `SELECT b.id::text, r.key, b.workspace_id::text, b.state, b.expires_at, b.granted_by::text, b.reason, b.created_at
FROM role_bindings b JOIN roles r ON r.id = b.role_id`

func scanBindings(rows pgx.Rows) ([]RoleBinding, error) {
	defer rows.Close()
	out := []RoleBinding{}
	for rows.Next() {
		var b RoleBinding
		if err := rows.Scan(&b.ID, &b.RoleKey, &b.WorkspaceID, &b.State, &b.ExpiresAt, &b.GrantedBy, &b.Reason, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// fillUser attaches tenant roles and memberships (restricted to scope for workspace admins).
func (s *Service) fillUser(ctx context.Context, u *DirectoryUser, scope []string, all bool) error {
	rows, err := s.Pool.Query(ctx, bindingSelect+` WHERE b.user_id = $1::uuid AND b.workspace_id IS NULL AND b.state = 'active' ORDER BY r.key`, u.ID)
	if err != nil {
		return err
	}
	if u.TenantRoles, err = scanBindings(rows); err != nil {
		return err
	}
	rows, err = s.Pool.Query(ctx, `SELECT m.id::text, w.id::text, w.key, w.name, m.user_id::text, m.state, m.joined_at
FROM workspace_memberships m JOIN workspaces w ON w.id = m.workspace_id
WHERE m.user_id = $1::uuid AND m.state <> 'revoked' AND ($2::uuid[] IS NULL OR w.id = ANY($2::uuid[])) ORDER BY w.name`, u.ID, nullableSlice(scope, all))
	if err != nil {
		return err
	}
	ms, err := scanMemberships(rows)
	if err != nil {
		return err
	}
	for i := range ms {
		if err := s.fillMembershipRoles(ctx, &ms[i]); err != nil {
			return err
		}
	}
	u.Memberships = ms
	return nil
}

func scanMemberships(rows pgx.Rows) ([]Membership, error) {
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.WorkspaceKey, &m.WorkspaceName, &m.UserID, &m.State, &m.JoinedAt); err != nil {
			return nil, err
		}
		m.Roles = []RoleBinding{}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) fillMembershipRoles(ctx context.Context, m *Membership) error {
	rows, err := s.Pool.Query(ctx, bindingSelect+` WHERE b.user_id = $1::uuid AND b.workspace_id = $2::uuid AND b.state = 'active' ORDER BY r.key`, m.UserID, m.WorkspaceID)
	if err != nil {
		return err
	}
	m.Roles, err = scanBindings(rows)
	return err
}

func (s *Service) getUser(ctx context.Context, tenantID, id string) (*DirectoryUser, error) {
	if !ValidUUID(id) {
		return nil, apperr.NotFound("user not found")
	}
	rows, err := s.Pool.Query(ctx, userSelect+` WHERE u.id = $1::uuid AND u.tenant_id = $2::uuid`, id, tenantID)
	if err != nil {
		return nil, err
	}
	users, err := scanUsers(rows)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, apperr.NotFound("user not found")
	}
	if err := s.fillUser(ctx, &users[0], nil, true); err != nil {
		return nil, err
	}
	return &users[0], nil
}

type CreateUserInput struct {
	IdPIssuer   string `json:"idp_issuer"`
	IdPSubject  string `json:"idp_subject"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Reason      string `json:"reason"`
}

// CreateUser pre-provisions a directory binding (iss, sub). No credentials exist in the Control Plane.
func (s *Service) CreateUser(ctx context.Context, p principal.Principal, in CreateUserInput, allowedIssuer string) (*DirectoryUser, error) {
	if err := s.requireTenant(ctx, p, "users:manage"); err != nil {
		return nil, err
	}
	if err := CheckReason(in.Reason); err != nil {
		return nil, err
	}
	in.IdPSubject, in.DisplayName, in.Email = strings.TrimSpace(in.IdPSubject), strings.TrimSpace(in.DisplayName), strings.TrimSpace(in.Email)
	if in.IdPIssuer != allowedIssuer {
		return nil, apperr.Invalid("ISSUER_NOT_TRUSTED", "idp_issuer must be the configured identity provider issuer")
	}
	if in.IdPSubject == "" || len(in.IdPSubject) > 255 || strings.ContainsAny(in.IdPSubject, "\r\n\x00") {
		return nil, apperr.Invalid("INVALID_SUBJECT", "idp_subject is required (max 255 chars)")
	}
	if len(in.DisplayName) < 1 || len(in.DisplayName) > 200 {
		return nil, apperr.Invalid("INVALID_NAME", "display_name is required (max 200 chars)")
	}
	if in.Email != "" && (len(in.Email) > 254 || !strings.Contains(in.Email, "@")) {
		return nil, apperr.Invalid("INVALID_EMAIL", "email is invalid")
	}
	var id string
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO users (tenant_id, idp_issuer, idp_subject, display_name, email) VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''))
RETURNING id::text`, p.TenantID, in.IdPIssuer, in.IdPSubject, in.DisplayName, in.Email).Scan(&id)
		if isUnique(err) {
			return apperr.Conflict("USER_EXISTS", "a directory entry for this issuer and subject already exists")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "user.provisioned",
			TargetType: "user", TargetID: id, Rationale: in.Reason, Metadata: map[string]any{"display_name": in.DisplayName}})
	})
	if err != nil {
		return nil, err
	}
	return s.getUser(ctx, p.TenantID, id)
}

// SetUserStatus suspends or reactivates a user. Suspension revokes all sessions in the same transaction.
func (s *Service) SetUserStatus(ctx context.Context, p principal.Principal, userID, status, reason string) (*DirectoryUser, error) {
	if err := s.requireTenant(ctx, p, "users:manage"); err != nil {
		return nil, err
	}
	if err := CheckReason(reason); err != nil {
		return nil, err
	}
	if userID == p.UserID {
		return nil, apperr.Forbidden("SELF_ACTION_FORBIDDEN", "you cannot change your own account status")
	}
	if _, err := s.getUser(ctx, p.TenantID, userID); err != nil {
		return nil, err
	}
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET status = $2, status_reason = $3, updated_at = now() WHERE id = $1::uuid AND status <> $2`, userID, status, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.Conflict("NO_CHANGE", "user already "+status)
		}
		meta := map[string]any{"status": status}
		if status == "suspended" {
			n, err := s.Sessions.RevokeUser(ctx, tx, userID, "user_suspended")
			if err != nil {
				return err
			}
			meta["sessions_revoked"] = n
			if err := audit.Enqueue(ctx, tx, "identity.user_suspended", "user_suspended:"+userID+":"+cid(ctx),
				map[string]any{"user_id": userID, "tenant_id": p.TenantID}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "user." + status,
			TargetType: "user", TargetID: userID, Rationale: reason, Metadata: meta})
	})
	if err != nil {
		return nil, err
	}
	return s.getUser(ctx, p.TenantID, userID)
}

func (s *Service) RevokeUserSessions(ctx context.Context, p principal.Principal, userID, reason string) (int64, error) {
	if err := s.requireTenant(ctx, p, "users:manage"); err != nil {
		return 0, err
	}
	if err := CheckReason(reason); err != nil {
		return 0, err
	}
	if _, err := s.getUser(ctx, p.TenantID, userID); err != nil {
		return 0, err
	}
	var n int64
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var err error
		if n, err = s.Sessions.RevokeUser(ctx, tx, userID, "admin_revoked"); err != nil {
			return err
		}
		if err := audit.Enqueue(ctx, tx, "identity.sessions_revoked", "sessions_revoked:"+userID+":"+cid(ctx),
			map[string]any{"user_id": userID, "tenant_id": p.TenantID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "session.revoked_by_admin",
			TargetType: "user", TargetID: userID, Rationale: reason, Metadata: map[string]any{"sessions_revoked": n}})
	})
	return n, err
}

// ---------- workspaces ----------

func (s *Service) ListWorkspaces(ctx context.Context, p principal.Principal) ([]Workspace, error) {
	scope, err := s.ManagedWorkspaces(ctx, p)
	if err != nil {
		return nil, err
	}
	canCreate, err := s.Engine.Tenant(ctx, nil, p.UserID, "workspaces:create", cid(ctx))
	if err != nil {
		return nil, err
	}
	if len(scope) == 0 && !canCreate.Allowed() {
		s.auditDeny(ctx, p, "", canCreate)
		return nil, apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted: workspaces:read")
	}
	rows, err := s.Pool.Query(ctx, `SELECT w.id::text, w.key, w.name, w.description, w.status,
  (SELECT count(*) FROM workspace_memberships m WHERE m.workspace_id = w.id AND m.state = 'active')::int
FROM workspaces w WHERE w.tenant_id = $1::uuid AND ($2::boolean OR w.id = ANY($3::uuid[])) ORDER BY w.name`,
		p.TenantID, canCreate.Allowed(), scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		var n int
		if err := rows.Scan(&w.ID, &w.Key, &w.Name, &w.Description, &w.Status, &n); err != nil {
			return nil, err
		}
		w.MemberCount = &n
		out = append(out, w)
	}
	return out, rows.Err()
}

var workspaceKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

type CreateWorkspaceInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Reason      string `json:"reason"`
}

func (s *Service) CreateWorkspace(ctx context.Context, p principal.Principal, in CreateWorkspaceInput) (*Workspace, error) {
	if err := s.requireTenant(ctx, p, "workspaces:create"); err != nil {
		return nil, err
	}
	if err := CheckReason(in.Reason); err != nil {
		return nil, err
	}
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if !workspaceKeyPattern.MatchString(in.Key) {
		return nil, apperr.Invalid("INVALID_KEY", "key must be lowercase letters, digits and dashes (2-63 chars)")
	}
	if len(in.Name) < 2 || len(in.Name) > 120 || len(in.Description) > 500 {
		return nil, apperr.Invalid("INVALID_NAME", "name must be 2-120 chars, description at most 500")
	}
	w := Workspace{Key: in.Key, Name: in.Name, Description: in.Description, Status: "active"}
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO workspaces (tenant_id, key, name, description, owner_user_id) VALUES ($1::uuid, $2, $3, $4, $5::uuid) RETURNING id::text`,
			p.TenantID, in.Key, in.Name, in.Description, p.UserID).Scan(&w.ID)
		if isUnique(err) {
			return apperr.Conflict("WORKSPACE_EXISTS", "workspace key already exists")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: w.ID, Action: "workspace.created",
			TargetType: "workspace", TargetID: w.ID, Rationale: in.Reason, Metadata: map[string]any{"key": in.Key}})
	})
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Service) ListMembers(ctx context.Context, p principal.Principal, workspaceID string) ([]Membership, error) {
	if !ValidUUID(workspaceID) {
		return nil, apperr.Forbidden(authorization.ReasonNotMember, "not permitted")
	}
	if err := s.requireWorkspace(ctx, p, workspaceID, "memberships:manage"); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT m.id::text, w.id::text, w.key, w.name, m.user_id::text, m.state, m.joined_at, u.display_name, u.email, u.status
FROM workspace_memberships m JOIN workspaces w ON w.id = m.workspace_id JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = $1::uuid AND w.tenant_id = $2::uuid AND m.state <> 'revoked' ORDER BY u.display_name`, workspaceID, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		m := Membership{User: &MemberUser{}}
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.WorkspaceKey, &m.WorkspaceName, &m.UserID, &m.State, &m.JoinedAt,
			&m.User.DisplayName, &m.User.Email, &m.User.Status); err != nil {
			return nil, err
		}
		m.User.ID = m.UserID
		out = append(out, m)
	}
	rows.Close()
	for i := range out {
		if err := s.fillMembershipRoles(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) AddMember(ctx context.Context, p principal.Principal, workspaceID, userID, reason string) (*Membership, error) {
	if !ValidUUID(workspaceID) {
		return nil, apperr.Forbidden(authorization.ReasonNotMember, "not permitted")
	}
	if err := s.requireWorkspace(ctx, p, workspaceID, "memberships:manage"); err != nil {
		return nil, err
	}
	if err := CheckReason(reason); err != nil {
		return nil, err
	}
	if _, err := s.getUser(ctx, p.TenantID, userID); err != nil {
		return nil, err
	}
	var id string
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO workspace_memberships (tenant_id, workspace_id, user_id, invited_by, reason)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5) RETURNING id::text`, p.TenantID, workspaceID, userID, p.UserID, reason).Scan(&id)
		if isUnique(err) {
			return apperr.Conflict("ALREADY_MEMBER", "user already has a live membership in this workspace")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: workspaceID, Action: "membership.created",
			TargetType: "membership", TargetID: id, Rationale: reason, Metadata: map[string]any{"user_id": userID}})
	})
	if err != nil {
		return nil, err
	}
	return s.getMembership(ctx, p.TenantID, id)
}

func (s *Service) getMembership(ctx context.Context, tenantID, id string) (*Membership, error) {
	if !ValidUUID(id) {
		return nil, apperr.NotFound("membership not found")
	}
	rows, err := s.Pool.Query(ctx, `SELECT m.id::text, w.id::text, w.key, w.name, m.user_id::text, m.state, m.joined_at
FROM workspace_memberships m JOIN workspaces w ON w.id = m.workspace_id WHERE m.id = $1::uuid AND m.tenant_id = $2::uuid`, id, tenantID)
	if err != nil {
		return nil, err
	}
	ms, err := scanMemberships(rows)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, apperr.NotFound("membership not found")
	}
	if err := s.fillMembershipRoles(ctx, &ms[0]); err != nil {
		return nil, err
	}
	return &ms[0], nil
}

// SetMembershipState suspends, reinstates or revokes a membership. Revocation also revokes the workspace's
// role bindings; both suspend and revoke drop the workspace from the user's live sessions.
func (s *Service) SetMembershipState(ctx context.Context, p principal.Principal, membershipID, action, reason string) (*Membership, error) {
	m, err := s.getMembership(ctx, p.TenantID, membershipID)
	if err != nil {
		return nil, apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted")
	}
	if err := s.requireWorkspace(ctx, p, m.WorkspaceID, "memberships:manage"); err != nil {
		return nil, err
	}
	if err := CheckReason(reason); err != nil {
		return nil, err
	}
	from, to := map[string][2]string{"suspend": {"active", "suspended"}, "reinstate": {"suspended", "active"}, "revoke": {"", "revoked"}}[action][0],
		map[string]string{"suspend": "suspended", "reinstate": "active", "revoke": "revoked"}[action]
	if to == "" {
		return nil, apperr.BadRequest("INVALID_ACTION", "unknown membership action")
	}
	if from != "" && m.State != from {
		return nil, apperr.Conflict("INVALID_STATE", fmt.Sprintf("membership is %s", m.State))
	}
	err = postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE workspace_memberships SET state = $2, updated_at = now(), revoked_at = CASE WHEN $2 = 'revoked' THEN now() END
WHERE id = $1::uuid`, membershipID, to); err != nil {
			return err
		}
		meta := map[string]any{"user_id": m.UserID, "from": m.State, "to": to}
		if to == "revoked" {
			tag, err := tx.Exec(ctx, `UPDATE role_bindings SET state = 'revoked', revoked_at = now(), revoked_by = $3::uuid
WHERE user_id = $1::uuid AND workspace_id = $2::uuid AND state = 'active'`, m.UserID, m.WorkspaceID, p.UserID)
			if err != nil {
				return err
			}
			meta["bindings_revoked"] = tag.RowsAffected()
		}
		if to != "active" {
			if err := s.Sessions.ClearWorkspace(ctx, tx, m.UserID, m.WorkspaceID); err != nil {
				return err
			}
			if err := audit.Enqueue(ctx, tx, "membership."+to, "membership:"+membershipID+":"+to+":"+cid(ctx),
				map[string]any{"user_id": m.UserID, "workspace_id": m.WorkspaceID}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: m.WorkspaceID,
			Action: "membership." + to, TargetType: "membership", TargetID: membershipID, Rationale: reason, Metadata: meta})
	})
	if err != nil {
		return nil, err
	}
	return s.getMembership(ctx, p.TenantID, membershipID)
}

// ---------- roles and grants ----------

func (s *Service) Roles(ctx context.Context, p principal.Principal) ([]Role, error) {
	if err := s.requireAnyAdmin(ctx, p); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT r.key, r.display_name, r.description, r.scope, r.sensitive,
  COALESCE(array_agg(rp.permission_key ORDER BY rp.permission_key) FILTER (WHERE rp.permission_key IS NOT NULL), '{}')
FROM roles r LEFT JOIN role_permissions rp ON rp.role_id = r.id GROUP BY r.id ORDER BY r.scope, r.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.Key, &r.DisplayName, &r.Description, &r.Scope, &r.Sensitive, &r.Permissions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) Permissions(ctx context.Context, p principal.Principal) ([]Permission, error) {
	if err := s.requireAnyAdmin(ctx, p); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT key, resource_type, action, description, sensitive FROM permissions ORDER BY resource_type, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Permission{}
	for rows.Next() {
		var x Permission
		if err := rows.Scan(&x.Key, &x.ResourceType, &x.Action, &x.Description, &x.Sensitive); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// requireAnyAdmin: platform admin or a workspace admin of at least one workspace.
func (s *Service) requireAnyAdmin(ctx context.Context, p principal.Principal) error {
	scope, err := s.ManagedWorkspaces(ctx, p)
	if err != nil {
		return err
	}
	if len(scope) > 0 {
		return nil
	}
	return s.requireTenant(ctx, p, "platform:admin")
}

type GrantInput struct {
	RoleKey   string     `json:"role_key"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type roleRow struct {
	id, key, scope string
	sensitive      bool
}

func (s *Service) role(ctx context.Context, key string) (*roleRow, error) {
	var r roleRow
	err := s.Pool.QueryRow(ctx, `SELECT id::text, key, scope, sensitive FROM roles WHERE key = $1`, key).Scan(&r.id, &r.key, &r.scope, &r.sensitive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.Invalid("UNKNOWN_ROLE", "unknown role")
	}
	return &r, err
}

func checkExpiry(t *time.Time) error {
	if t != nil && (t.Before(time.Now().Add(time.Minute)) || t.After(time.Now().AddDate(2, 0, 0))) {
		return apperr.Invalid("INVALID_EXPIRY", "expires_at must be in the future and within two years")
	}
	return nil
}

// GrantWorkspaceRole grants a workspace-scoped role on an active membership. Self-grants are refused;
// sensitive roles become a pending request that a different authorised approver must decide.
func (s *Service) GrantWorkspaceRole(ctx context.Context, p principal.Principal, membershipID string, in GrantInput) (*GrantResult, error) {
	m, err := s.getMembership(ctx, p.TenantID, membershipID)
	if err != nil {
		return nil, apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted")
	}
	if err := s.requireWorkspace(ctx, p, m.WorkspaceID, "memberships:manage"); err != nil {
		return nil, err
	}
	if err := CheckReason(in.Reason); err != nil {
		return nil, err
	}
	if err := checkExpiry(in.ExpiresAt); err != nil {
		return nil, err
	}
	r, err := s.role(ctx, in.RoleKey)
	if err != nil {
		return nil, err
	}
	if r.scope != "workspace" {
		return nil, apperr.Invalid("ROLE_SCOPE_MISMATCH", "tenant roles are granted on the user, not on a membership")
	}
	if m.UserID == p.UserID {
		s.auditSoD(ctx, p, m.WorkspaceID, m.UserID, r.key, "SELF_GRANT_FORBIDDEN")
		return nil, apperr.Forbidden("SELF_GRANT_FORBIDDEN", "you cannot grant roles to yourself")
	}
	if m.State != "active" {
		return nil, apperr.Conflict("MEMBERSHIP_NOT_ACTIVE", "roles can only be granted on an active membership")
	}
	if r.sensitive {
		req, err := s.createGrantRequest(ctx, p, m.UserID, m.WorkspaceID, r, in)
		if err != nil {
			return nil, err
		}
		return &GrantResult{Outcome: "pending_approval", Request: req}, nil
	}
	b, err := s.insertBinding(ctx, nil, p, m.UserID, m.WorkspaceID, r, in.Reason, in.ExpiresAt, "")
	if err != nil {
		return nil, err
	}
	return &GrantResult{Outcome: "granted", Binding: b}, nil
}

// GrantTenantRole requests a tenant-scoped role. All tenant roles are sensitive and need dual control.
func (s *Service) GrantTenantRole(ctx context.Context, p principal.Principal, userID string, in GrantInput) (*GrantResult, error) {
	if err := s.requireTenant(ctx, p, "platform:admin"); err != nil {
		return nil, err
	}
	if err := CheckReason(in.Reason); err != nil {
		return nil, err
	}
	if err := checkExpiry(in.ExpiresAt); err != nil {
		return nil, err
	}
	u, err := s.getUser(ctx, p.TenantID, userID)
	if err != nil {
		return nil, err
	}
	r, err := s.role(ctx, in.RoleKey)
	if err != nil {
		return nil, err
	}
	if r.scope != "tenant" {
		return nil, apperr.Invalid("ROLE_SCOPE_MISMATCH", "workspace roles are granted on a membership")
	}
	if u.ID == p.UserID {
		s.auditSoD(ctx, p, "", u.ID, r.key, "SELF_GRANT_FORBIDDEN")
		return nil, apperr.Forbidden("SELF_GRANT_FORBIDDEN", "you cannot grant roles to yourself")
	}
	if u.Status != "active" || u.PrincipalType != "human" {
		return nil, apperr.Conflict("SUBJECT_NOT_ELIGIBLE", "tenant roles require an active human user")
	}
	req, err := s.createGrantRequest(ctx, p, u.ID, "", r, in)
	if err != nil {
		return nil, err
	}
	return &GrantResult{Outcome: "pending_approval", Request: req}, nil
}

func (s *Service) auditSoD(ctx context.Context, p principal.Principal, workspaceID, target, role, code string) {
	_ = audit.Record(ctx, s.Pool, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: workspaceID,
		Action: "role_binding.denied", TargetType: "user", TargetID: target, Decision: "deny", ReasonCode: code,
		PolicyVersion: s.Engine.PolicyVersion, Metadata: map[string]any{"role": role}})
}

func (s *Service) createGrantRequest(ctx context.Context, p principal.Principal, userID, workspaceID string, r *roleRow, in GrantInput) (*GrantRequest, error) {
	var id string
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role_grant_requests WHERE user_id = $1::uuid AND role_id = $2::uuid
AND workspace_id IS NOT DISTINCT FROM $3::uuid AND state = 'pending' AND expires_at > now())`, userID, r.id, nullable(workspaceID)).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return apperr.Conflict("REQUEST_PENDING", "an identical grant request is already pending")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO role_grant_requests (tenant_id, workspace_id, user_id, role_id, requested_by, reason, binding_expires_at, expires_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8) RETURNING id::text`,
			p.TenantID, nullable(workspaceID), userID, r.id, p.UserID, in.Reason, in.ExpiresAt, time.Now().Add(grantRequestTTL)).Scan(&id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: workspaceID,
			Action: "role_grant.requested", TargetType: "grant_request", TargetID: id, Rationale: in.Reason,
			PolicyVersion: s.Engine.PolicyVersion, Metadata: map[string]any{"user_id": userID, "role": r.key}})
	})
	if err != nil {
		return nil, err
	}
	return s.getGrantRequest(ctx, p.TenantID, id)
}

func (s *Service) insertBinding(ctx context.Context, q postgres.Querier, p principal.Principal, userID, workspaceID string, r *roleRow, reason string, expires *time.Time, requestID string) (*RoleBinding, error) {
	var b RoleBinding
	run := func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO role_bindings (tenant_id, workspace_id, user_id, role_id, expires_at, granted_by, grant_request_id, reason)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6::uuid, $7::uuid, $8) RETURNING id::text`,
			p.TenantID, nullable(workspaceID), userID, r.id, expires, p.UserID, nullable(requestID), reason).Scan(&id)
		if isUnique(err) {
			return apperr.Conflict("ALREADY_GRANTED", "the role is already granted")
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, bindingSelect+` WHERE b.id = $1::uuid`, id)
		if err != nil {
			return err
		}
		bs, err := scanBindings(rows)
		if err != nil || len(bs) == 0 {
			return errors.Join(err, errors.New("binding not readable"))
		}
		b = bs[0]
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: workspaceID,
			Action: "role_binding.granted", TargetType: "role_binding", TargetID: id, Decision: "allow", Rationale: reason,
			PolicyVersion: s.Engine.PolicyVersion, Metadata: map[string]any{"user_id": userID, "role": r.key, "grant_request_id": requestID}})
	}
	var err error
	if tx, ok := q.(pgx.Tx); ok {
		err = run(tx)
	} else {
		err = postgres.WithTx(ctx, s.Pool, run)
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// RevokeBinding revokes a role binding. Removing the last platform admin is refused (lockout guard).
func (s *Service) RevokeBinding(ctx context.Context, p principal.Principal, bindingID, reason string) error {
	if !ValidUUID(bindingID) {
		return apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted")
	}
	var userID, roleKey, state string
	var workspaceID *string
	err := s.Pool.QueryRow(ctx, `SELECT b.user_id::text, b.workspace_id::text, r.key, b.state FROM role_bindings b JOIN roles r ON r.id = b.role_id
WHERE b.id = $1::uuid AND b.tenant_id = $2::uuid`, bindingID, p.TenantID).Scan(&userID, &workspaceID, &roleKey, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted")
	}
	if err != nil {
		return err
	}
	ws := ""
	if workspaceID != nil {
		ws = *workspaceID
		err = s.requireWorkspace(ctx, p, ws, "memberships:manage")
	} else {
		err = s.requireTenant(ctx, p, "platform:admin")
	}
	if err != nil {
		return err
	}
	if err := CheckReason(reason); err != nil {
		return err
	}
	if state != "active" {
		return apperr.Conflict("INVALID_STATE", "binding is not active")
	}
	return postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if roleKey == "platform_admin" {
			var remaining int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM role_bindings b JOIN roles r ON r.id = b.role_id JOIN users u ON u.id = b.user_id
WHERE r.key = 'platform_admin' AND b.tenant_id = $1::uuid AND b.state = 'active' AND u.status = 'active' AND b.id <> $2::uuid
  AND (b.expires_at IS NULL OR b.expires_at > now())`, p.TenantID, bindingID).Scan(&remaining); err != nil {
				return err
			}
			if remaining == 0 {
				return apperr.Conflict("LAST_PLATFORM_ADMIN", "cannot revoke the last active platform admin")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE role_bindings SET state = 'revoked', revoked_at = now(), revoked_by = $2::uuid WHERE id = $1::uuid`, bindingID, p.UserID); err != nil {
			return err
		}
		if err := audit.Enqueue(ctx, tx, "role_binding.revoked", "binding_revoked:"+bindingID, map[string]any{"user_id": userID, "role": roleKey, "workspace_id": ws}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: ws, Action: "role_binding.revoked",
			TargetType: "role_binding", TargetID: bindingID, Rationale: reason, PolicyVersion: s.Engine.PolicyVersion,
			Metadata: map[string]any{"user_id": userID, "role": roleKey}})
	})
}

const grantRequestSelect = `SELECT g.id::text, g.user_id::text, u.display_name, g.workspace_id::text, w.name, r.key, g.requested_by::text,
  rq.display_name, g.reason, CASE WHEN g.state = 'pending' AND g.expires_at <= now() THEN 'expired' ELSE g.state END,
  g.decided_by::text, g.decision_reason, g.created_at, g.expires_at, g.binding_expires_at, g.tenant_id::text
FROM role_grant_requests g JOIN users u ON u.id = g.user_id JOIN users rq ON rq.id = g.requested_by JOIN roles r ON r.id = g.role_id
LEFT JOIN workspaces w ON w.id = g.workspace_id`

func scanGrantRequests(rows pgx.Rows) ([]GrantRequest, error) {
	defer rows.Close()
	out := []GrantRequest{}
	for rows.Next() {
		var g GrantRequest
		if err := rows.Scan(&g.ID, &g.UserID, &g.UserName, &g.WorkspaceID, &g.WorkspaceName, &g.RoleKey, &g.RequestedBy, &g.RequestedByName,
			&g.Reason, &g.State, &g.DecidedBy, &g.DecisionReason, &g.CreatedAt, &g.ExpiresAt, &g.BindingExpires, &g.TenantID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Service) getGrantRequest(ctx context.Context, tenantID, id string) (*GrantRequest, error) {
	if !ValidUUID(id) {
		return nil, apperr.NotFound("request not found")
	}
	rows, err := s.Pool.Query(ctx, grantRequestSelect+` WHERE g.id = $1::uuid AND g.tenant_id = $2::uuid`, id, tenantID)
	if err != nil {
		return nil, err
	}
	gs, err := scanGrantRequests(rows)
	if err != nil {
		return nil, err
	}
	if len(gs) == 0 {
		return nil, apperr.NotFound("request not found")
	}
	return &gs[0], nil
}

func (s *Service) ListGrantRequests(ctx context.Context, p principal.Principal, state string) ([]GrantRequest, error) {
	approver, err := s.Engine.Tenant(ctx, nil, p.UserID, "grants:approve", cid(ctx))
	if err != nil {
		return nil, err
	}
	scope := []string{}
	if !approver.Allowed() {
		if scope, err = s.ManagedWorkspaces(ctx, p); err != nil {
			return nil, err
		}
		if len(scope) == 0 {
			s.auditDeny(ctx, p, "", approver)
			return nil, apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted: grants:read")
		}
	}
	if state != "" && state != "pending" && state != "approved" && state != "rejected" {
		return nil, apperr.BadRequest("INVALID_STATE", "state must be pending, approved or rejected")
	}
	// Non-approvers see only requests in their workspaces that they raised.
	rows, err := s.Pool.Query(ctx, grantRequestSelect+` WHERE g.tenant_id = $1::uuid
  AND ($2 = '' OR ($2 = 'pending' AND g.state = 'pending' AND g.expires_at > now()) OR ($2 <> 'pending' AND g.state = $2))
  AND ($3::boolean OR (g.workspace_id = ANY($4::uuid[]) AND g.requested_by = $5::uuid))
ORDER BY g.created_at DESC LIMIT 200`, p.TenantID, state, approver.Allowed(), scope, p.UserID)
	if err != nil {
		return nil, err
	}
	return scanGrantRequests(rows)
}

// DecideGrantRequest approves or rejects a pending sensitive grant. The approver must hold grants:approve
// and be neither the requester nor the target (also enforced by a database CHECK).
func (s *Service) DecideGrantRequest(ctx context.Context, p principal.Principal, id string, approve bool, reason string) (*GrantRequest, error) {
	if err := s.requireTenant(ctx, p, "grants:approve"); err != nil {
		return nil, err
	}
	if err := CheckReason(reason); err != nil {
		return nil, err
	}
	g, err := s.getGrantRequest(ctx, p.TenantID, id)
	if err != nil {
		return nil, err
	}
	if g.State != "pending" {
		return nil, apperr.Conflict("REQUEST_NOT_PENDING", "request is "+g.State)
	}
	if g.RequestedBy == p.UserID || g.UserID == p.UserID {
		s.auditSoD(ctx, p, deref(g.WorkspaceID), g.UserID, g.RoleKey, "SELF_APPROVAL_FORBIDDEN")
		return nil, apperr.Forbidden("SELF_APPROVAL_FORBIDDEN", "an independent approver must decide this request")
	}
	r, err := s.role(ctx, g.RoleKey)
	if err != nil {
		return nil, err
	}
	state := "rejected"
	if approve {
		state = "approved"
	}
	err = postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE role_grant_requests SET state = $2, decided_by = $3::uuid, decided_at = now(), decision_reason = $4
WHERE id = $1::uuid AND state = 'pending' AND expires_at > now()`, id, state, p.UserID, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.Conflict("REQUEST_NOT_PENDING", "request is no longer pending")
		}
		if approve {
			ws := deref(g.WorkspaceID)
			if ws != "" {
				var st string
				err := tx.QueryRow(ctx, `SELECT state FROM workspace_memberships WHERE user_id = $1::uuid AND workspace_id = $2::uuid AND state <> 'revoked'`, g.UserID, ws).Scan(&st)
				if errors.Is(err, pgx.ErrNoRows) || st != "active" {
					return apperr.Conflict("MEMBERSHIP_NOT_ACTIVE", "target membership is no longer active")
				}
				if err != nil {
					return err
				}
			}
			if _, err := s.insertBinding(ctx, tx, p, g.UserID, ws, r, g.Reason, g.BindingExpires, id); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: deref(g.WorkspaceID),
			Action: "role_grant." + state, TargetType: "grant_request", TargetID: id, Rationale: reason, PolicyVersion: s.Engine.PolicyVersion,
			Metadata: map[string]any{"user_id": g.UserID, "role": g.RoleKey, "requested_by": g.RequestedBy}})
	})
	if err != nil {
		return nil, err
	}
	return s.getGrantRequest(ctx, p.TenantID, id)
}

// ---------- overrides ----------

func (s *Service) ListOverrides(ctx context.Context, p principal.Principal, workspaceID string) ([]Override, error) {
	if !ValidUUID(workspaceID) {
		return nil, apperr.Forbidden(authorization.ReasonNotMember, "not permitted")
	}
	if err := s.requireWorkspace(ctx, p, workspaceID, "memberships:manage"); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT o.id::text, o.workspace_id::text, o.user_id::text, u.display_name, o.permission_key, o.effect, o.reason,
  o.expires_at, o.created_by::text, o.created_at
FROM entitlement_overrides o JOIN users u ON u.id = o.user_id
WHERE o.workspace_id = $1::uuid AND o.tenant_id = $2::uuid AND o.revoked_at IS NULL ORDER BY o.created_at DESC`, workspaceID, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Override{}
	for rows.Next() {
		var o Override
		if err := rows.Scan(&o.ID, &o.WorkspaceID, &o.UserID, &o.UserName, &o.PermissionKey, &o.Effect, &o.Reason, &o.ExpiresAt, &o.CreatedBy, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type OverrideInput struct {
	UserID        string     `json:"user_id"`
	PermissionKey string     `json:"permission_key"`
	Effect        string     `json:"effect"`
	Reason        string     `json:"reason"`
	ExpiresAt     *time.Time `json:"expires_at"`
}

func (s *Service) CreateOverride(ctx context.Context, p principal.Principal, workspaceID string, in OverrideInput) (*Override, error) {
	if !ValidUUID(workspaceID) {
		return nil, apperr.Forbidden(authorization.ReasonNotMember, "not permitted")
	}
	if err := s.requireWorkspace(ctx, p, workspaceID, "memberships:manage"); err != nil {
		return nil, err
	}
	if err := CheckReason(in.Reason); err != nil {
		return nil, err
	}
	if err := checkExpiry(in.ExpiresAt); err != nil {
		return nil, err
	}
	if in.Effect != "allow" && in.Effect != "deny" {
		return nil, apperr.Invalid("INVALID_EFFECT", "effect must be allow or deny")
	}
	if _, err := s.getUser(ctx, p.TenantID, in.UserID); err != nil {
		return nil, err
	}
	var sensitive bool
	if err := s.Pool.QueryRow(ctx, `SELECT sensitive FROM permissions WHERE key = $1`, in.PermissionKey).Scan(&sensitive); err != nil {
		return nil, apperr.Invalid("UNKNOWN_PERMISSION", "unknown permission")
	}
	if in.Effect == "allow" {
		if sensitive {
			return nil, apperr.Forbidden("SENSITIVE_ALLOW_FORBIDDEN", "sensitive permissions can only be granted through reviewed roles")
		}
		if in.UserID == p.UserID {
			return nil, apperr.Forbidden("SELF_GRANT_FORBIDDEN", "you cannot grant yourself access")
		}
	}
	var id string
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO entitlement_overrides (tenant_id, workspace_id, user_id, permission_key, effect, reason, expires_at, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8::uuid) RETURNING id::text`, p.TenantID, workspaceID, in.UserID, in.PermissionKey, in.Effect,
			in.Reason, in.ExpiresAt, p.UserID).Scan(&id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: workspaceID, Action: "override.created",
			TargetType: "override", TargetID: id, Decision: in.Effect, Rationale: in.Reason, PolicyVersion: s.Engine.PolicyVersion,
			Metadata: map[string]any{"user_id": in.UserID, "permission": in.PermissionKey}})
	})
	if err != nil {
		return nil, err
	}
	list, err := s.ListOverrides(ctx, p, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, errors.New("override not readable")
}

func (s *Service) DeleteOverride(ctx context.Context, p principal.Principal, id, reason string) error {
	if !ValidUUID(id) {
		return apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted")
	}
	var workspaceID string
	err := s.Pool.QueryRow(ctx, `SELECT workspace_id::text FROM entitlement_overrides WHERE id = $1::uuid AND tenant_id = $2::uuid AND revoked_at IS NULL`, id, p.TenantID).Scan(&workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted")
	}
	if err != nil {
		return err
	}
	if err := s.requireWorkspace(ctx, p, workspaceID, "memberships:manage"); err != nil {
		return err
	}
	if err := CheckReason(reason); err != nil {
		return err
	}
	return postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE entitlement_overrides SET revoked_at = now(), revoked_by = $2::uuid WHERE id = $1::uuid`, id, p.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: workspaceID, Action: "override.revoked",
			TargetType: "override", TargetID: id, Rationale: reason})
	})
}

// AccessPreview explains the app:open decision for another user, for admins of that workspace.
func (s *Service) AccessPreview(ctx context.Context, p principal.Principal, userID, workspaceID, appKey string) (*authorization.Decision, error) {
	if !ValidUUID(workspaceID) || !ValidUUID(userID) {
		return nil, apperr.Forbidden(authorization.ReasonMissingPermission, "not permitted")
	}
	if err := s.requireWorkspace(ctx, p, workspaceID, "memberships:manage"); err != nil {
		return nil, err
	}
	if _, err := s.getUser(ctx, p.TenantID, userID); err != nil {
		return nil, err
	}
	d, err := s.Engine.App(ctx, userID, workspaceID, appKey, cid(ctx))
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ---------- helpers ----------

func CheckReason(reason string) error {
	r := strings.TrimSpace(reason)
	if len(r) < 3 || len(r) > 500 {
		return apperr.New(http.StatusUnprocessableEntity, "REASON_REQUIRED", "a reason of 3-500 characters is required")
	}
	return nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
