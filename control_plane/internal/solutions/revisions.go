package solutions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/jackc/pgx/v5"
)

type Revision struct {
	Plan        Project   `json:"plan"`
	ContentHash string    `json:"content_hash"`
	Origin      string    `json:"origin"`
	RecordedBy  string    `json:"recorded_by,omitempty"`
	RecordedAt  time.Time `json:"recorded_at"`
}
type RevisionPage struct {
	Items      []Revision `json:"items"`
	NextBefore int        `json:"next_before,omitempty"`
}

const revisionColumns = `snapshot,origin,COALESCE(recorded_by::text,''),recorded_at`

func scanRevision(row pgx.Row) (Revision, error) {
	var item Revision
	if err := row.Scan(&item.Plan, &item.Origin, &item.RecordedBy, &item.RecordedAt); err != nil {
		return item, missing(err)
	}
	raw, err := json.Marshal(item.Plan)
	if err != nil {
		return item, err
	}
	item.ContentHash, err = CanonicalSnapshotHash(raw)
	return item, err
}
func (s *Service) ListRevisions(ctx context.Context, p principal.Principal, id string, before int) (RevisionPage, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return RevisionPage{}, err
	}
	if before < 0 {
		return RevisionPage{}, apperr.BadRequest("INVALID_CURSOR", "Use a positive revision cursor")
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+revisionColumns+` FROM solution_project_revisions WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND solution_id=$3::uuid AND ($4=0 OR revision<$4) ORDER BY revision DESC LIMIT 21`, p.TenantID, p.WorkspaceID, id, before)
	if err != nil {
		return RevisionPage{}, err
	}
	defer rows.Close()
	out := RevisionPage{Items: []Revision{}}
	for rows.Next() {
		item, err := scanRevision(rows)
		if err != nil {
			return RevisionPage{}, err
		}
		out.Items = append(out.Items, item)
	}
	if len(out.Items) > 20 {
		out.Items = out.Items[:20]
		out.NextBefore = out.Items[19].Plan.Revision
	}
	return out, rows.Err()
}
func (s *Service) GetRevision(ctx context.Context, p principal.Principal, id string, revision int) (Revision, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return Revision{}, err
	}
	if revision < 1 {
		return Revision{}, apperr.BadRequest("INVALID_REVISION", "Use a positive saved revision")
	}
	return scanRevision(s.Pool.QueryRow(ctx, `SELECT `+revisionColumns+` FROM solution_project_revisions WHERE tenant_id=$1::uuid AND workspace_id=$2::uuid AND solution_id=$3::uuid AND revision=$4`, p.TenantID, p.WorkspaceID, id, revision))
}
