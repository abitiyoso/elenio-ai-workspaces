// Package solutions owns workspace-scoped solution plans. Resource references are not grants,
// verified releases, deployment evidence or copies of domain runtime state.
package solutions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var workflowKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var requestPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

type WorkflowRef struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
}
type Definition struct {
	Key          string        `json:"key"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	DeliveryMode string        `json:"delivery_mode"` // internal | solution | enterprise
	Output       string        `json:"output"`        // business_ui | api | both
	Workflows    []WorkflowRef `json:"workflows"`
}
type Project struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	WorkspaceID string     `json:"workspace_id"`
	Revision    int        `json:"revision"`
	Definition  Definition `json:"definition"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
type Page struct {
	Items      []Project `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}
type Service struct {
	Pool   *pgxpool.Pool
	Engine *authorization.Engine
}

func Validate(d Definition) error {
	if !keyPattern.MatchString(d.Key) || strings.TrimSpace(d.Name) != d.Name || len([]rune(d.Name)) < 2 || len([]rune(d.Name)) > 120 || len(d.Description) > 12000 {
		return apperr.BadRequest("INVALID_SOLUTION", "Use a stable solution key, a 2–120 character name and a description of at most 12000 bytes")
	}
	switch d.DeliveryMode {
	case "internal", "solution", "enterprise":
	default:
		return apperr.BadRequest("INVALID_MODE", "Unknown delivery mode")
	}
	switch d.Output {
	case "business_ui", "api", "both":
	default:
		return apperr.BadRequest("INVALID_OUTPUT", "Unknown application output")
	}
	if len(d.Workflows) > 100 {
		return apperr.BadRequest("INVALID_REFERENCES", "At most 100 pinned workflow references are supported")
	}
	seen := map[string]bool{}
	for _, ref := range d.Workflows {
		if !workflowKeyPattern.MatchString(ref.Key) || ref.Version < 1 || seen[ref.Key] {
			return apperr.BadRequest("INVALID_REFERENCES", "Workflow keys must be unique and pinned to positive versions")
		}
		seen[ref.Key] = true
	}
	return nil
}

func (s *Service) require(ctx context.Context, q postgres.Querier, p principal.Principal, perm string) error {
	if !uuidPattern.MatchString(p.WorkspaceID) {
		return apperr.Forbidden("WORKSPACE_REQUIRED", "Select an authorized workspace")
	}
	facts, err := s.Engine.Facts(ctx, q, p.UserID, p.WorkspaceID)
	if err != nil {
		return err
	}
	if facts.TenantID != p.TenantID {
		return apperr.Forbidden("TENANT_MISMATCH", "Verified tenant does not match")
	}
	decision := authorization.DecideWorkspacePermission(facts, perm, observability.RequestIDFrom(ctx), time.Now())
	if !decision.Allowed() {
		return apperr.Forbidden(decision.ReasonCode, "Solution access is not allowed in this workspace")
	}
	return nil
}

const columns = `id::text,tenant_id::text,workspace_id::text,revision,definition,created_at,updated_at`

func scan(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.TenantID, &p.WorkspaceID, &p.Revision, &p.Definition, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
func missing(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.New(http.StatusNotFound, "NOT_FOUND", "Solution not found in the active workspace")
	}
	return err
}

func (s *Service) List(ctx context.Context, p principal.Principal, cursor string) (Page, error) {
	out := Page{Items: []Project{}}
	if err := s.require(ctx, nil, p, "solution:view"); err != nil {
		return out, err
	}
	if cursor != "" && !uuidPattern.MatchString(cursor) {
		return out, apperr.BadRequest("INVALID_CURSOR", "Invalid solution cursor")
	}
	if cursor == "" {
		cursor = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+columns+` FROM solution_projects WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND id>$3::uuid ORDER BY id LIMIT 51`, p.TenantID, p.WorkspaceID, cursor)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.NextCursor = out.Items[49].ID
	}
	return out, rows.Err()
}
func (s *Service) Get(ctx context.Context, p principal.Principal, id string) (Project, error) {
	if err := s.require(ctx, nil, p, "solution:view"); err != nil {
		return Project{}, err
	}
	if !uuidPattern.MatchString(id) {
		return Project{}, apperr.BadRequest("INVALID_ID", "Invalid solution ID")
	}
	item, err := scan(s.Pool.QueryRow(ctx, `SELECT `+columns+` FROM solution_projects WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND id=$3::uuid`, p.TenantID, p.WorkspaceID, id))
	return item, missing(err)
}
func (s *Service) Create(ctx context.Context, p principal.Principal, d Definition, requestKey string) (Project, bool, error) {
	if err := Validate(d); err != nil {
		return Project{}, false, err
	}
	if !requestPattern.MatchString(requestKey) {
		return Project{}, false, apperr.BadRequest("IDEMPOTENCY_REQUIRED", "Supply an 8–128 character Idempotency-Key")
	}
	if d.Workflows == nil {
		d.Workflows = []WorkflowRef{}
	}
	raw, _ := json.Marshal(d)
	hash := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(hash[:])
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Project{}, false, err
	}
	defer tx.Rollback(ctx)
	if err = s.require(ctx, tx, p, "solution:author"); err != nil {
		return Project{}, false, err
	}
	// One request key is serialized within its verified workspace. No tenant comes from the body.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, p.WorkspaceID+":"+requestKey); err != nil {
		return Project{}, false, err
	}
	var storedHash string
	err = tx.QueryRow(ctx, `SELECT creation_hash FROM solution_projects WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND creation_key=$3`, p.TenantID, p.WorkspaceID, requestKey).Scan(&storedHash)
	if err == nil {
		if storedHash != fingerprint {
			return Project{}, false, apperr.New(409, "IDEMPOTENCY_CONFLICT", "Request key already used for a different solution")
		}
		item, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM solution_projects WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND creation_key=$3`, p.TenantID, p.WorkspaceID, requestKey))
		return item, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Project{}, false, err
	}
	item, err := scan(tx.QueryRow(ctx, `INSERT INTO solution_projects(tenant_id,workspace_id,key,definition,creation_key,creation_hash,created_by) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7::uuid) RETURNING `+columns, p.TenantID, p.WorkspaceID, d.Key, raw, requestKey, fingerprint, p.UserID))
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return Project{}, false, apperr.New(409, "SOLUTION_EXISTS", "Solution key already exists in this workspace")
	}
	if err != nil {
		return Project{}, false, err
	}
	if err = s.record(ctx, tx, p, item, "solution.created"); err != nil {
		return Project{}, false, err
	}
	return item, true, tx.Commit(ctx)
}
func (s *Service) Update(ctx context.Context, p principal.Principal, id string, revision int, d Definition) (Project, error) {
	if err := Validate(d); err != nil {
		return Project{}, err
	}
	if !uuidPattern.MatchString(id) || revision < 1 {
		return Project{}, apperr.BadRequest("INVALID_VERSION", "Supply a valid solution ID and positive revision")
	}
	if d.Workflows == nil {
		d.Workflows = []WorkflowRef{}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback(ctx)
	if err = s.require(ctx, tx, p, "solution:author"); err != nil {
		return Project{}, err
	}
	item, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM solution_projects WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND id=$3::uuid FOR UPDATE`, p.TenantID, p.WorkspaceID, id))
	if err != nil {
		return Project{}, missing(err)
	}
	if item.Revision != revision {
		return Project{}, apperr.New(412, "REVISION_CONFLICT", "Solution changed; reload and review before saving")
	}
	if item.Definition.Key != d.Key {
		return Project{}, apperr.BadRequest("IMMUTABLE_KEY", "The solution key cannot change")
	}
	raw, _ := json.Marshal(d)
	item, err = scan(tx.QueryRow(ctx, `UPDATE solution_projects SET definition=$4,revision=revision+1,updated_at=now() WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND id=$3::uuid RETURNING `+columns, p.TenantID, p.WorkspaceID, id, raw))
	if err != nil {
		return Project{}, err
	}
	if err = s.record(ctx, tx, p, item, "solution.updated"); err != nil {
		return Project{}, err
	}
	return item, tx.Commit(ctx)
}
func (s *Service) record(ctx context.Context, tx pgx.Tx, p principal.Principal, item Project, action string) error {
	return audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Action: action, TargetType: "solution", TargetID: item.ID, Decision: "allow", Metadata: map[string]any{"key": item.Definition.Key, "revision": item.Revision, "workflow_count": len(item.Definition.Workflows)}})
}
