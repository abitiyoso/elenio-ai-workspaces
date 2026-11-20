package solutions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/jackc/pgx/v5"
)

type ExecutionInput struct {
	WorkflowKey string          `json:"workflow_key"`
	Title       string          `json:"title"`
	Variables   json.RawMessage `json:"variables"`
}
type Execution struct {
	ID              string    `json:"id"`
	SolutionID      string    `json:"solution_id"`
	PlanRevision    int       `json:"plan_revision"`
	WorkflowKey     string    `json:"workflow_key"`
	WorkflowVersion int       `json:"workflow_version"`
	CaseID          string    `json:"case_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

const executionColumns = `id::text,solution_id::text,plan_revision,workflow_key,workflow_version,COALESCE(case_id::text,''),created_at`

func scanExecution(row pgx.Row) (Execution, error) {
	var item Execution
	err := row.Scan(&item.ID, &item.SolutionID, &item.PlanRevision, &item.WorkflowKey, &item.WorkflowVersion, &item.CaseID, &item.CreatedAt)
	return item, err
}

// Reserve before the remote call so a lost response can be retried after the solution advances.
func (s *Service) BeginExecution(ctx context.Context, p principal.Principal, id string, revision int, key string, in ExecutionInput) (Execution, error) {
	if !uuidPattern.MatchString(id) || revision < 1 || !requestPattern.MatchString(key) || !workflowKeyPattern.MatchString(in.WorkflowKey) || len([]rune(strings.TrimSpace(in.Title))) < 1 || len([]rune(in.Title)) > 300 || len(in.Variables) > 256<<10 {
		return Execution{}, apperr.BadRequest("INVALID_EXECUTION", "Supply the saved solution revision, pinned workflow key, title and stable Idempotency-Key")
	}
	if len(in.Variables) == 0 {
		in.Variables = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(in.Variables, &object) != nil || object == nil {
		return Execution{}, apperr.BadRequest("INVALID_INPUT", "Variables must be a JSON object")
	}
	raw, err := json.Marshal(struct {
		Solution string
		Revision int
		Input    ExecutionInput
	}{id, revision, in})
	if err != nil {
		return Execution{}, err
	}
	fingerprint, err := CanonicalSnapshotHash(raw)
	if err != nil {
		return Execution{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Execution{}, err
	}
	defer tx.Rollback(ctx)
	if err = s.require(ctx, tx, p, "solution:author"); err != nil {
		return Execution{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, p.WorkspaceID+":"+p.UserID+":execution:"+key); err != nil {
		return Execution{}, err
	}
	var storedHash string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM solution_executions WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND created_by=$3::uuid AND request_key=$4`, p.TenantID, p.WorkspaceID, p.UserID, key).Scan(&storedHash)
	if err == nil {
		if storedHash != fingerprint {
			return Execution{}, apperr.New(409, "IDEMPOTENCY_CONFLICT", "Retry key was already used for different execution input")
		}
		item, err := scanExecution(tx.QueryRow(ctx, `SELECT `+executionColumns+` FROM solution_executions WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND created_by=$3::uuid AND request_key=$4`, p.TenantID, p.WorkspaceID, p.UserID, key))
		return item, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, err
	}
	plan, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM solution_projects WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND id=$3::uuid FOR UPDATE`, p.TenantID, p.WorkspaceID, id))
	if err != nil {
		return Execution{}, missing(err)
	}
	if plan.Revision != revision {
		return Execution{}, apperr.New(412, "REVISION_CONFLICT", "Solution changed; reload before starting a new execution")
	}
	version := 0
	for _, ref := range plan.Definition.Workflows {
		if ref.Key == in.WorkflowKey {
			version = ref.Version
			break
		}
	}
	if version == 0 {
		return Execution{}, apperr.BadRequest("WORKFLOW_NOT_LINKED", "The workflow is not pinned in this solution revision")
	}
	item, err := scanExecution(tx.QueryRow(ctx, `INSERT INTO solution_executions(tenant_id,workspace_id,solution_id,plan_revision,workflow_key,workflow_version,created_by,request_key,request_hash) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid,$8,$9) RETURNING `+executionColumns, p.TenantID, p.WorkspaceID, id, revision, in.WorkflowKey, version, p.UserID, key, fingerprint))
	if err != nil {
		return Execution{}, err
	}
	if err = audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Action: "solution.execution_requested", TargetType: "solution", TargetID: id, Decision: "allow", Metadata: map[string]any{"execution_id": item.ID, "plan_revision": revision, "workflow_key": in.WorkflowKey, "workflow_version": version}}); err != nil {
		return Execution{}, err
	}
	return item, tx.Commit(ctx)
}
func (s *Service) FinishExecution(ctx context.Context, p principal.Principal, item Execution, caseID string) error {
	if !uuidPattern.MatchString(caseID) {
		return apperr.BadRequest("INVALID_CASE", "Invalid case identifier")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.require(ctx, tx, p, "solution:author"); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE solution_executions SET case_id=$5::uuid WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND created_by=$3::uuid AND id=$4::uuid AND case_id IS NULL`, p.TenantID, p.WorkspaceID, p.UserID, item.ID, caseID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var existing string
		if err = tx.QueryRow(ctx, `SELECT case_id::text FROM solution_executions WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND created_by=$3::uuid AND id=$4::uuid`, p.TenantID, p.WorkspaceID, p.UserID, item.ID).Scan(&existing); err != nil {
			return missing(err)
		}
		if existing != caseID {
			return apperr.New(409, "CASE_CONFLICT", "Execution already points to another case")
		}
	} else if err = audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Action: "solution.execution_started", TargetType: "solution", TargetID: item.SolutionID, Decision: "allow", Metadata: map[string]any{"execution_id": item.ID, "case_id": caseID, "plan_revision": item.PlanRevision}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) ListExecutions(ctx context.Context, p principal.Principal, id, cursor string) ([]Execution, string, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, "", err
	}
	if cursor == "" {
		cursor = "00000000-0000-0000-0000-000000000000"
	}
	if !uuidPattern.MatchString(cursor) {
		return nil, "", apperr.BadRequest("INVALID_CURSOR", "Invalid execution cursor")
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+executionColumns+` FROM solution_executions WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND solution_id=$3::uuid AND created_by=$4::uuid AND id>$5::uuid ORDER BY id LIMIT 21`, p.TenantID, p.WorkspaceID, id, p.UserID, cursor)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []Execution{}
	for rows.Next() {
		item, err := scanExecution(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	next := ""
	if len(items) > 20 {
		items = items[:20]
		next = items[19].ID
	}
	return items, next, rows.Err()
}
