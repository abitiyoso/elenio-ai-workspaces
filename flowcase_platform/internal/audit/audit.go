// Package audit writes and reads the append-only audit trail. Writes happen inside the same
// transaction as the state change they describe, so an action is never committed without its record.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/flowcase/internal/platform/observability"
)

type Event struct {
	TenantID       string
	ActorType      string // user | service | system
	ActorID        string
	Action         string // e.g. workflow.version.published
	SubjectType    string
	SubjectID      string
	SubjectVersion string
	CaseID         string
	TraceID        string
	Data           map[string]any
}

// Record appends one event. Data keys that look sensitive are redacted before storage.
func Record(ctx context.Context, tx pgx.Tx, e Event) error {
	data := map[string]any{}
	for k, v := range e.Data {
		if observability.IsSensitiveKey(k) {
			v = observability.Redacted
		}
		data[k] = v
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("audit data: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
		(tenant_id, actor_type, actor_id, action, subject_type, subject_id, subject_version, case_id, trace_id, data)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, NULLIF($7, ''), NULLIF($8, '')::uuid, NULLIF($9, ''), $10)`,
		e.TenantID, e.ActorType, e.ActorID, e.Action, e.SubjectType, e.SubjectID, e.SubjectVersion, e.CaseID, e.TraceID, raw)
	if err != nil {
		return fmt.Errorf("record audit event %s: %w", e.Action, err)
	}
	return nil
}

type Entry struct {
	ID             string          `json:"id"`
	Seq            int64           `json:"seq"`
	OccurredAt     time.Time       `json:"occurred_at"`
	ActorType      string          `json:"actor_type"`
	ActorID        *string         `json:"actor_id"`
	ActorName      *string         `json:"actor_name"`
	Action         string          `json:"action"`
	SubjectType    string          `json:"subject_type"`
	SubjectID      string          `json:"subject_id"`
	SubjectVersion *string         `json:"subject_version"`
	TraceID        *string         `json:"trace_id"`
	Data           json.RawMessage `json:"data"`
}

type Filter struct {
	SubjectType string
	SubjectID   string
	BeforeSeq   int64 // cursor: return events with seq < BeforeSeq; 0 = newest
	Limit       int
}

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// List returns tenant events newest first, plus the cursor for the next page (0 when done).
func List(ctx context.Context, q Querier, tenantID string, f Filter) ([]Entry, int64, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := q.Query(ctx, `
		SELECT a.id::text, a.seq, a.occurred_at, a.actor_type, a.actor_id::text, u.display_name, a.action,
		       a.subject_type, a.subject_id, a.subject_version, a.trace_id, a.data
		FROM audit_events a LEFT JOIN users u ON u.id = a.actor_id
		WHERE a.tenant_id = $1
		  AND ($2 = '' OR a.subject_type = $2)
		  AND ($3 = '' OR a.subject_id = $3)
		  AND ($4 = 0 OR a.seq < $4)
		ORDER BY a.seq DESC
		LIMIT $5`, tenantID, f.SubjectType, f.SubjectID, f.BeforeSeq, f.Limit+1)
	if err != nil {
		return nil, 0, err
	}
	entries, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := r.Scan(&e.ID, &e.Seq, &e.OccurredAt, &e.ActorType, &e.ActorID, &e.ActorName, &e.Action,
			&e.SubjectType, &e.SubjectID, &e.SubjectVersion, &e.TraceID, &e.Data)
		return e, err
	})
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(entries) > f.Limit {
		entries = entries[:f.Limit]
		next = entries[len(entries)-1].Seq
	}
	if entries == nil {
		entries = []Entry{}
	}
	return entries, next, nil
}

// TraceID is a convenience for callers recording events from a request context.
func TraceID(ctx context.Context) string { return observability.TraceID(ctx) }
