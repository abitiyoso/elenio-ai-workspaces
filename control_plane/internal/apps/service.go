package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/platform/postgres"
	"github.com/elenio/control-plane/internal/workspaces"
)

type Service struct {
	Pool        *pgxpool.Pool
	Engine      *authorization.Engine
	Upstreams   *integrations.Upstreams
	Environment string // manifest environment for this deployment: dev | staging | production
}

type Summary struct {
	AppKey      string              `json:"app_key"`
	DisplayName string              `json:"display_name"`
	Description string              `json:"description"`
	Category    string              `json:"category"`
	Icon        string              `json:"icon"`
	OwnerTeam   string              `json:"owner_team"`
	RouteBase   string              `json:"route_base"`
	Version     string              `json:"version"`
	Status      string              `json:"status"`
	Integration string              `json:"integration"`
	Health      integrations.Health `json:"health"`
}

type Version struct {
	ID           string     `json:"id"`
	Version      string     `json:"version"`
	ReleaseState string     `json:"release_state"`
	Checksum     string     `json:"checksum"`
	Manifest     Manifest   `json:"manifest"`
	CreatedBy    *string    `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	PublishedBy  *string    `json:"published_by"`
	PublishedAt  *time.Time `json:"published_at"`
}

type RegistryApp struct {
	AppKey             string              `json:"app_key"`
	DisplayName        string              `json:"display_name"`
	OwnerTeam          string              `json:"owner_team"`
	Status             string              `json:"status"`
	Revision           int                 `json:"revision"`
	RouteBase          string              `json:"route_base"`
	PublishedVersion   *string             `json:"published_version"`
	LatestVersion      string              `json:"latest_version"`
	DisabledReason     *string             `json:"disabled_reason"`
	Integration        string              `json:"integration"`
	UpstreamConfigured bool                `json:"upstream_configured"`
	Audience           *string             `json:"audience"`
	Health             integrations.Health `json:"health"`
	Versions           []Version           `json:"versions"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

func cid(ctx context.Context) string { return observability.RequestIDFrom(ctx) }

// ---------- catalog ----------

// Catalog returns published apps the principal may open in the active workspace (same decision as the
// route middleware, so the tile list and direct access never disagree).
func (s *Service) Catalog(ctx context.Context, p principal.Principal) ([]Summary, error) {
	out := []Summary{}
	if p.WorkspaceID == "" {
		return out, nil
	}
	facts, err := s.Engine.Facts(ctx, nil, p.UserID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT a.app_key, a.status, v.manifest FROM mini_apps a
JOIN mini_app_versions v ON v.id = a.published_version_id WHERE a.status = 'published' ORDER BY a.app_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, status string
		var m Manifest
		if err := rows.Scan(&key, &status, &m); err != nil {
			return nil, err
		}
		d := authorization.DecideApp(facts, &authorization.App{Key: key, Status: status, HasPublishedVersion: true,
			RequiredEntitlements: m.RequiredEntitlements, WorkspaceAllowlist: m.WorkspaceAllowlist}, cid(ctx), time.Now())
		if d.Allowed() {
			out = append(out, s.summary(key, status, m))
		}
	}
	return out, rows.Err()
}

func (s *Service) summary(key, status string, m Manifest) Summary {
	return Summary{AppKey: key, DisplayName: m.DisplayName, Description: m.Description, Category: m.Category, Icon: m.Icon,
		OwnerTeam: m.OwnerTeam, RouteBase: m.RouteBase, Version: m.Version, Status: status,
		Integration: s.Upstreams.Integration(key), Health: s.Upstreams.Health(key)}
}

type LaunchResult struct {
	AppKey      string `json:"app_key"`
	RouteBase   string `json:"route_base"`
	Integration string `json:"integration"`
}

// Launch authorises app:open and returns the registered relative route (never a caller-supplied URL).
func (s *Service) Launch(ctx context.Context, p principal.Principal, appKey string) (*LaunchResult, *authorization.Decision, error) {
	d, err := s.Engine.App(ctx, p.UserID, p.WorkspaceID, appKey, cid(ctx))
	if err != nil {
		return nil, nil, err
	}
	if p.WorkspaceID == "" {
		d.Effect, d.ReasonCode = "deny", authorization.ReasonNotMember
	}
	ev := audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, TargetType: "mini_app",
		TargetID: appKey, Decision: d.Effect, ReasonCode: d.ReasonCode, PolicyVersion: d.PolicyVersion}
	if !d.Allowed() {
		ev.Action = "authz.denied"
		ev.Metadata = map[string]any{"action": "app:open", "missing_permissions": d.MissingPermissions}
		_ = audit.Record(ctx, s.Pool, ev)
		return nil, &d, apperr.Forbidden(d.ReasonCode, "app is not available in this workspace")
	}
	ev.Action = "app.launched"
	_ = audit.Record(ctx, s.Pool, ev)
	return &LaunchResult{AppKey: appKey, RouteBase: "/apps/" + appKey + "/", Integration: s.Upstreams.Integration(appKey)}, &d, nil
}

// ---------- registry admin ----------

func (s *Service) requireRegistry(ctx context.Context, p principal.Principal) error {
	d, err := s.Engine.Tenant(ctx, nil, p.UserID, "registry:manage", cid(ctx))
	if err != nil {
		return err
	}
	if !d.Allowed() {
		_ = audit.Record(ctx, s.Pool, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "authz.denied",
			TargetType: "platform_admin", TargetID: "registry:manage", Decision: "deny", ReasonCode: d.ReasonCode, PolicyVersion: d.PolicyVersion})
		return apperr.Forbidden(d.ReasonCode, "not permitted: registry:manage")
	}
	return nil
}

func (s *Service) List(ctx context.Context, p principal.Principal) ([]RegistryApp, error) {
	if err := s.requireRegistry(ctx, p); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT app_key FROM mini_apps ORDER BY app_key`)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	out := []RegistryApp{}
	for _, k := range keys {
		a, err := s.load(ctx, s.Pool, k)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, p principal.Principal, key string) (*RegistryApp, error) {
	if err := s.requireRegistry(ctx, p); err != nil {
		return nil, err
	}
	return s.load(ctx, s.Pool, key)
}

func (s *Service) load(ctx context.Context, q postgres.Querier, key string) (*RegistryApp, error) {
	var a RegistryApp
	var id string
	err := q.QueryRow(ctx, `SELECT a.id::text, a.app_key, a.owner_team, a.status, a.revision, a.route_base, pv.version, a.disabled_reason, a.updated_at
FROM mini_apps a LEFT JOIN mini_app_versions pv ON pv.id = a.published_version_id WHERE a.app_key = $1`, key).
		Scan(&id, &a.AppKey, &a.OwnerTeam, &a.Status, &a.Revision, &a.RouteBase, &a.PublishedVersion, &a.DisabledReason, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("app not found")
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT id::text, version, release_state, checksum, manifest, created_by::text, created_at, published_by::text, published_at
FROM mini_app_versions WHERE mini_app_id = $1::uuid ORDER BY created_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	a.Versions = []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.Version, &v.ReleaseState, &v.Checksum, &v.Manifest, &v.CreatedBy, &v.CreatedAt, &v.PublishedBy, &v.PublishedAt); err != nil {
			return nil, err
		}
		a.Versions = append(a.Versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(a.Versions) > 0 {
		a.LatestVersion = a.Versions[0].Version
		a.DisplayName = a.Versions[0].Manifest.DisplayName
		for _, v := range a.Versions {
			if v.ReleaseState == "published" {
				a.DisplayName = v.Manifest.DisplayName
			}
		}
	}
	up, ok := s.Upstreams.Get(a.AppKey)
	a.UpstreamConfigured = ok && up.Mode == "proxy" && up.URL != ""
	if ok && up.Audience != "" {
		a.Audience = &up.Audience
	}
	a.Integration = s.Upstreams.Integration(a.AppKey)
	a.Health = s.Upstreams.Health(a.AppKey)
	return &a, nil
}

type DraftInput struct {
	Manifest json.RawMessage `json:"manifest"`
	Reason   string          `json:"reason"`
}

func invalid(issues []Issue) error {
	e := apperr.Invalid("MANIFEST_INVALID", "manifest failed validation")
	e.Issues = issues
	return e
}

func (s *Service) parse(in DraftInput) (Manifest, []byte, string, error) {
	m, issues := ParseManifest(in.Manifest)
	if len(issues) == 0 {
		issues = Validate(m, s.Environment)
	}
	if len(issues) > 0 {
		return m, nil, "", invalid(issues)
	}
	canon, sum, err := Canonical(m)
	return m, canon, sum, err
}

// Create registers a new app with its first immutable draft version.
func (s *Service) Create(ctx context.Context, p principal.Principal, in DraftInput) (*RegistryApp, error) {
	if err := s.requireRegistry(ctx, p); err != nil {
		return nil, err
	}
	if err := workspaces.CheckReason(in.Reason); err != nil {
		return nil, err
	}
	m, canon, sum, err := s.parse(in)
	if err != nil {
		return nil, err
	}
	err = postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var appID string
		err := tx.QueryRow(ctx, `INSERT INTO mini_apps (app_key, route_base, owner_team) VALUES ($1, $2, $3) RETURNING id::text`,
			m.AppKey, m.RouteBase, m.OwnerTeam).Scan(&appID)
		if isUnique(err) {
			return apperr.Conflict("APP_CONFLICT", "app_key or route_base is already registered")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mini_app_versions (mini_app_id, version, manifest, checksum, created_by) VALUES ($1::uuid, $2, $3, $4, $5::uuid)`,
			appID, m.Version, canon, sum, p.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "app.registered", TargetType: "mini_app",
			TargetID: m.AppKey, Rationale: in.Reason, Metadata: map[string]any{"version": m.Version, "checksum": sum, "route_base": m.RouteBase}})
	})
	if err != nil {
		return nil, err
	}
	return s.load(ctx, s.Pool, m.AppKey)
}

// AddVersion adds a new immutable draft. The route base can never change for an existing app key.
func (s *Service) AddVersion(ctx context.Context, p principal.Principal, key string, in DraftInput) (*RegistryApp, error) {
	if err := s.requireRegistry(ctx, p); err != nil {
		return nil, err
	}
	if err := workspaces.CheckReason(in.Reason); err != nil {
		return nil, err
	}
	m, canon, sum, err := s.parse(in)
	if err != nil {
		return nil, err
	}
	if m.AppKey != key {
		return nil, invalid([]Issue{{"app_key", "KEY_MISMATCH", "manifest app_key must match the app being versioned"}})
	}
	err = postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var appID, route string
		err := tx.QueryRow(ctx, `SELECT id::text, route_base FROM mini_apps WHERE app_key = $1 FOR UPDATE`, key).Scan(&appID, &route)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("app not found")
		}
		if err != nil {
			return err
		}
		if route != m.RouteBase {
			return invalid([]Issue{{"route_base", "ROUTE_IMMUTABLE", "route_base of a registered app cannot change"}})
		}
		var latest string
		if err := tx.QueryRow(ctx, `SELECT version FROM mini_app_versions WHERE mini_app_id = $1::uuid ORDER BY created_at DESC LIMIT 1`, appID).Scan(&latest); err != nil {
			return err
		}
		if !semverGreater(m.Version, latest) {
			return invalid([]Issue{{"version", "VERSION_NOT_INCREASED", "version must be greater than " + latest}})
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mini_app_versions (mini_app_id, version, manifest, checksum, created_by) VALUES ($1::uuid, $2, $3, $4, $5::uuid)`,
			appID, m.Version, canon, sum, p.UserID); err != nil {
			if isUnique(err) {
				return apperr.Conflict("VERSION_EXISTS", "version already exists")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mini_apps SET revision = revision + 1, updated_at = now() WHERE id = $1::uuid`, appID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "app.version_drafted", TargetType: "mini_app",
			TargetID: key, Rationale: in.Reason, Metadata: map[string]any{"version": m.Version, "checksum": sum}})
	})
	if err != nil {
		return nil, err
	}
	return s.load(ctx, s.Pool, key)
}

// SemverGreater reports whether version a (MAJOR.MINOR.PATCH) is greater than b.
func SemverGreater(a, b string) bool { return semverGreater(a, b) }

func semverGreater(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	return false
}

// ParseIfMatch accepts `"<revision>"` or `<revision>`; a missing header is a 428-style precondition error.
func ParseIfMatch(h string) (int, error) {
	h = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(h), "W/"))
	h = strings.Trim(h, `"`)
	n, err := strconv.Atoi(h)
	if err != nil || n < 1 {
		return 0, apperr.New(http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", `If-Match: "<revision>" is required`)
	}
	return n, nil
}

type lockedApp struct {
	id, status  string
	revision    int
	publishedID *string
}

func lockApp(ctx context.Context, tx pgx.Tx, key string, revision int) (*lockedApp, error) {
	var a lockedApp
	err := tx.QueryRow(ctx, `SELECT id::text, status, revision, published_version_id::text FROM mini_apps WHERE app_key = $1 FOR UPDATE`, key).
		Scan(&a.id, &a.status, &a.revision, &a.publishedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("app not found")
	}
	if err != nil {
		return nil, err
	}
	if a.revision != revision {
		return nil, apperr.New(http.StatusPreconditionFailed, "REVISION_MISMATCH", "the app changed since you loaded it; reload and retry")
	}
	return &a, nil
}

type PublishInput struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

// Publish makes one draft version the live manifest (previous published version becomes superseded) and
// registers its route for this environment.
func (s *Service) Publish(ctx context.Context, p principal.Principal, key string, revision int, in PublishInput) (*RegistryApp, error) {
	if err := s.requireRegistry(ctx, p); err != nil {
		return nil, err
	}
	if err := workspaces.CheckReason(in.Reason); err != nil {
		return nil, err
	}
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		a, err := lockApp(ctx, tx, key, revision)
		if err != nil {
			return err
		}
		if a.status == "retired" {
			return apperr.Conflict("APP_RETIRED", "retired apps cannot be published")
		}
		var vid, state, checksum string
		var m Manifest
		err = tx.QueryRow(ctx, `SELECT id::text, release_state, checksum, manifest FROM mini_app_versions WHERE mini_app_id = $1::uuid AND version = $2`, a.id, in.Version).
			Scan(&vid, &state, &checksum, &m)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("version not found")
		}
		if err != nil {
			return err
		}
		if state != "draft" {
			return apperr.Conflict("VERSION_NOT_DRAFT", "only draft versions can be published")
		}
		// Re-validate at publish time (environment and rules may have tightened since drafting).
		if issues := Validate(m, s.Environment); len(issues) > 0 {
			return invalid(issues)
		}
		if a.publishedID != nil {
			if _, err := tx.Exec(ctx, `UPDATE mini_app_versions SET release_state = 'superseded' WHERE id = $1::uuid`, *a.publishedID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE mini_app_versions SET release_state = 'published', published_by = $2::uuid, published_at = now() WHERE id = $1::uuid`, vid, p.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mini_app_routes SET enabled = false WHERE registered_path = $1 AND environment = $2 AND enabled`, m.RouteBase, m.Environment); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mini_app_routes (app_version_id, environment, registered_path, upstream_config_ref) VALUES ($1::uuid, $2, $3, $4)`,
			vid, m.Environment, m.RouteBase, key); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mini_apps SET status = 'published', published_version_id = $2::uuid, disabled_reason = NULL,
revision = revision + 1, updated_at = now() WHERE id = $1::uuid`, a.id, vid); err != nil {
			return err
		}
		if err := audit.Enqueue(ctx, tx, "app.published", "app_published:"+key+":"+in.Version, map[string]any{"app_key": key, "version": in.Version, "checksum": checksum}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: "app.published", TargetType: "mini_app",
			TargetID: key, Rationale: in.Reason, Metadata: map[string]any{"version": in.Version, "checksum": checksum}})
	})
	if err != nil {
		return nil, err
	}
	return s.load(ctx, s.Pool, key)
}

type ReasonInput struct {
	Reason string `json:"reason"`
}

// SetEnabled disables (or re-enables) an app. Disable takes effect on the next request everywhere because
// no positive decision is cached; route rows are disabled in the same transaction.
func (s *Service) SetEnabled(ctx context.Context, p principal.Principal, key string, revision int, enable bool, reason string) (*RegistryApp, error) {
	if err := s.requireRegistry(ctx, p); err != nil {
		return nil, err
	}
	if err := workspaces.CheckReason(reason); err != nil {
		return nil, err
	}
	err := postgres.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		a, err := lockApp(ctx, tx, key, revision)
		if err != nil {
			return err
		}
		action := "app.disabled"
		if enable {
			action = "app.enabled"
			if a.status != "disabled" || a.publishedID == nil {
				return apperr.Conflict("INVALID_STATE", "only a disabled app with a published version can be enabled")
			}
			if _, err := tx.Exec(ctx, `UPDATE mini_apps SET status = 'published', disabled_reason = NULL, revision = revision + 1, updated_at = now() WHERE id = $1::uuid`, a.id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE mini_app_routes SET enabled = true WHERE app_version_id = $1::uuid
AND NOT EXISTS (SELECT 1 FROM mini_app_routes r2 WHERE r2.registered_path = mini_app_routes.registered_path AND r2.environment = mini_app_routes.environment AND r2.enabled)`, *a.publishedID); err != nil {
				return err
			}
		} else {
			if a.status != "published" {
				return apperr.Conflict("INVALID_STATE", "only a published app can be disabled")
			}
			if _, err := tx.Exec(ctx, `UPDATE mini_apps SET status = 'disabled', disabled_reason = $2, revision = revision + 1, updated_at = now() WHERE id = $1::uuid`, a.id, reason); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE mini_app_routes r SET enabled = false FROM mini_app_versions v WHERE v.id = r.app_version_id AND v.mini_app_id = $1::uuid AND r.enabled`, a.id); err != nil {
				return err
			}
		}
		if err := audit.Enqueue(ctx, tx, action, action+":"+key+":"+strconv.Itoa(revision), map[string]any{"app_key": key}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, Action: action, TargetType: "mini_app", TargetID: key, Rationale: reason})
	})
	if err != nil {
		return nil, err
	}
	return s.load(ctx, s.Pool, key)
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
