package solutions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var candidateVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type CandidateSnapshot struct {
	SchemaVersion string          `json:"schema_version"`
	Plan          Project         `json:"plan"`
	Readiness     json.RawMessage `json:"readiness"`
	Limitations   []string        `json:"limitations"`
}
type Candidate struct {
	ID           string            `json:"id"`
	SolutionID   string            `json:"solution_id"`
	Version      string            `json:"version"`
	PlanRevision int               `json:"plan_revision"`
	Status       string            `json:"status"`
	Snapshot     CandidateSnapshot `json:"snapshot"`
	ContentHash  string            `json:"content_hash"`
	CreatedAt    time.Time         `json:"created_at"`
}
type CandidatePage struct {
	Items      []Candidate `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

const candidateColumns = `id::text,solution_id::text,version,plan_revision,snapshot,content_hash,created_at`

func scanCandidate(row pgx.Row) (Candidate, error) {
	var item Candidate
	err := row.Scan(&item.ID, &item.SolutionID, &item.Version, &item.PlanRevision, &item.Snapshot, &item.ContentHash, &item.CreatedAt)
	item.Status = "candidate"
	return item, err
}
func candidateInput(id string, revision int, version, requestKey string) error {
	if !uuidPattern.MatchString(id) || revision < 1 || !candidateVersion.MatchString(version) || len(version) > 32 || !requestPattern.MatchString(requestKey) {
		return apperr.BadRequest("INVALID_CANDIDATE", "Supply a solution ID, current revision, numeric version (e.g. 1.0.0) and a stable Idempotency-Key")
	}
	return nil
}

// CanonicalSnapshotHash survives JSONB key ordering and preserves numeric precision.
// It is a checksum, not a signature or approval.
func CanonicalSnapshotHash(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", apperr.BadRequest("INVALID_SNAPSHOT", "Expected one snapshot object")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// Replay is checked before collecting another dependency snapshot, including after the plan advances.
func (s *Service) ReplayCandidate(ctx context.Context, p principal.Principal, id string, revision int, version, requestKey string) (Candidate, bool, error) {
	if err := candidateInput(id, revision, version, requestKey); err != nil {
		return Candidate{}, false, err
	}
	if err := s.require(ctx, nil, p, "solution:author"); err != nil {
		return Candidate{}, false, err
	}
	item, err := scanCandidate(s.Pool.QueryRow(ctx, `SELECT `+candidateColumns+` FROM solution_candidates WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND creation_key=$3`, p.TenantID, p.WorkspaceID, requestKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, false, nil
	}
	if err != nil {
		return Candidate{}, false, err
	}
	if item.SolutionID != id || item.Version != version || item.PlanRevision != revision {
		return Candidate{}, false, apperr.New(409, "IDEMPOTENCY_CONFLICT", "Retry key was already used for another candidate request")
	}
	return item, true, nil
}
func (s *Service) CreateCandidate(ctx context.Context, p principal.Principal, id string, revision int, version, requestKey string, readiness json.RawMessage) (Candidate, bool, error) {
	if err := candidateInput(id, revision, version, requestKey); err != nil {
		return Candidate{}, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Candidate{}, false, err
	}
	defer tx.Rollback(ctx)
	if err = s.require(ctx, tx, p, "solution:author"); err != nil {
		return Candidate{}, false, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, p.WorkspaceID+":candidate:"+requestKey); err != nil {
		return Candidate{}, false, err
	}
	replay, err := scanCandidate(tx.QueryRow(ctx, `SELECT `+candidateColumns+` FROM solution_candidates WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND creation_key=$3`, p.TenantID, p.WorkspaceID, requestKey))
	if err == nil {
		if replay.SolutionID != id || replay.Version != version || replay.PlanRevision != revision {
			return Candidate{}, false, apperr.New(409, "IDEMPOTENCY_CONFLICT", "Retry key was already used for another candidate request")
		}
		return replay, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, false, err
	}
	plan, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM solution_projects WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND id=$3::uuid FOR UPDATE`, p.TenantID, p.WorkspaceID, id))
	if err != nil {
		return Candidate{}, false, missing(err)
	}
	if plan.Revision != revision {
		return Candidate{}, false, apperr.New(412, "REVISION_CONFLICT", "Solution changed while dependencies were checked; reload and review before creating a candidate")
	}
	var evidence struct {
		SolutionID   string    `json:"solution_id"`
		Revision     int       `json:"revision"`
		ReleaseReady bool      `json:"release_ready"`
		CheckedAt    time.Time `json:"checked_at"`
	}
	if len(readiness) > 192<<10 || json.Unmarshal(readiness, &evidence) != nil || evidence.SolutionID != id || evidence.Revision != revision || evidence.ReleaseReady || evidence.CheckedAt.IsZero() || time.Since(evidence.CheckedAt) > time.Minute || evidence.CheckedAt.After(time.Now().Add(time.Second)) {
		return Candidate{}, false, apperr.BadRequest("INVALID_EVIDENCE", "A current server-generated partial readiness snapshot for this exact revision is required")
	}
	snapshot := CandidateSnapshot{SchemaVersion: "elenio.solution-candidate.v1", Plan: plan, Readiness: readiness, Limitations: []string{"Candidate only: not a solution release, authorization grant, installation package or deployment.", "Dependency observations are time-scoped and must be revalidated before any release.", "No customer documents, runtime results or credentials are included."}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return Candidate{}, false, err
	}
	hash, err := CanonicalSnapshotHash(raw)
	if err != nil {
		return Candidate{}, false, err
	}
	item, err := scanCandidate(tx.QueryRow(ctx, `INSERT INTO solution_candidates(tenant_id,workspace_id,solution_id,version,plan_revision,snapshot,content_hash,creation_key,created_by) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9::uuid) RETURNING `+candidateColumns, p.TenantID, p.WorkspaceID, id, version, revision, raw, hash, requestKey, p.UserID))
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return Candidate{}, false, apperr.New(409, "CANDIDATE_EXISTS", "Candidate version already exists; use a new version, existing snapshots are immutable")
	}
	if err != nil {
		return Candidate{}, false, err
	}
	if err = audit.Record(ctx, tx, audit.Event{ActorUserID: p.UserID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Action: "solution.candidate.created", TargetType: "solution_candidate", TargetID: item.ID, Decision: "allow", Metadata: map[string]any{"solution_id": id, "version": version, "plan_revision": revision, "content_hash": hash}}); err != nil {
		return Candidate{}, false, err
	}
	return item, true, tx.Commit(ctx)
}
func (s *Service) ListCandidates(ctx context.Context, p principal.Principal, id, cursor string) (CandidatePage, error) {
	out := CandidatePage{Items: []Candidate{}}
	if _, err := s.Get(ctx, p, id); err != nil {
		return out, err
	}
	if cursor != "" && !uuidPattern.MatchString(cursor) {
		return out, apperr.BadRequest("INVALID_CURSOR", "Invalid candidate cursor")
	}
	if cursor == "" {
		cursor = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+candidateColumns+` FROM solution_candidates WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND solution_id=$3::uuid AND id>$4::uuid ORDER BY id LIMIT 51`, p.TenantID, p.WorkspaceID, id, cursor)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanCandidate(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.NextCursor = out.Items[49].ID
	}
	return out, rows.Err()
}
func (s *Service) GetCandidate(ctx context.Context, p principal.Principal, id, candidateID string) (Candidate, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return Candidate{}, err
	}
	if !uuidPattern.MatchString(candidateID) {
		return Candidate{}, apperr.BadRequest("INVALID_ID", "Invalid candidate ID")
	}
	item, err := scanCandidate(s.Pool.QueryRow(ctx, `SELECT `+candidateColumns+` FROM solution_candidates WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND solution_id=$3::uuid AND id=$4::uuid`, p.TenantID, p.WorkspaceID, id, candidateID))
	return item, missing(err)
}
