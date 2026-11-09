// Package ops holds operational queries shared by the API status endpoint, metrics and the kms-admin CLI.
package ops

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

// CollectionPrincipalsQuery lists principals allowed to see a collection scope, or a case scope when $3 is set.
const CollectionPrincipalsQuery = `SELECT m.principal_id::text FROM collection_memberships m
	WHERE m.tenant_id=$1 AND m.collection_id=$2 AND (NULLIF($3,'') IS NULL OR EXISTS(
		SELECT 1 FROM case_memberships cm WHERE cm.tenant_id=m.tenant_id AND cm.case_id=NULLIF($3,'')::uuid AND cm.principal_id=m.principal_id))`

// indexableChunks is the canonical definition of what Elasticsearch should contain: indexed chunks of live,
// unrevoked, unexpired versions. Revocation, expiry and deletion remove chunks through the outbox.
const indexableChunks = `FROM document_chunks dc
	JOIN documents d ON d.tenant_id=dc.tenant_id AND d.id=dc.document_id
	JOIN document_versions v ON v.tenant_id=dc.tenant_id AND v.id=dc.version_id
	JOIN collections c ON c.tenant_id=d.tenant_id AND c.id=d.collection_id
	WHERE dc.indexed_at IS NOT NULL AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL
	AND v.publication_status NOT IN ('expired','revoked') AND v.index_status='indexed'
	AND (NULLIF($1::text,'') IS NULL OR dc.tenant_id=NULLIF($1::text,'')::uuid)`

func ScopePrincipals(ctx context.Context, db *sql.DB, tenantID, collectionID, caseID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, CollectionPrincipalsQuery, tenantID, collectionID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []string{}
	for rows.Next() {
		var member string
		if err := rows.Scan(&member); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// ExpectedIndexedChunks counts chunks that should be in Elasticsearch; an empty tenantID counts all tenants.
func ExpectedIndexedChunks(ctx context.Context, db *sql.DB, tenantID string) (int64, error) {
	var count int64
	err := db.QueryRowContext(ctx, `SELECT count(*) `+indexableChunks, tenantID).Scan(&count)
	return count, err
}

// ForEachIndexableChunk streams canonical chunk records with current trust, publication and ACL metadata,
// so an index can be rebuilt entirely from PostgreSQL. Vectors are not stored in PostgreSQL and must be re-embedded.
func ForEachIndexableChunk(ctx context.Context, db *sql.DB, tenantID string, fn func(platform.IndexedChunk) error) error {
	rows, err := db.QueryContext(ctx, `SELECT dc.chunk_id,dc.tenant_id::text,dc.document_id::text,dc.version_id::text,d.collection_id::text,COALESCE(d.case_id::text,''),
	c.classification,v.trust_status,v.publication_status,v.parser_status,d.title,to_json(dc.heading_path),dc.content,dc.page_start,dc.page_end,COALESCE(dc.source_anchor,'') `+
		indexableChunks+` ORDER BY dc.tenant_id,dc.version_id,dc.chunk_number`, tenantID)
	if err != nil {
		return err
	}
	type scope struct{ tenant, collection, caseID string }
	type record struct {
		chunk   platform.IndexedChunk
		scopeID scope
	}
	// Collect first: principals are looked up per scope, and database/sql cannot nest queries on a busy row set.
	var records []record
	for rows.Next() {
		var item record
		var headings []byte
		var pageStart, pageEnd sql.NullInt64
		c := &item.chunk
		if err := rows.Scan(&c.ChunkID, &c.TenantID, &c.DocumentID, &c.VersionID, &c.CollectionID, &c.CaseID, &c.Classification, &c.TrustStatus,
			&c.PublicationStatus, &c.ParserStatus, &c.Title, &headings, &c.Content, &pageStart, &pageEnd, &c.SourceAnchor); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(headings, &c.HeadingPath); err != nil {
			rows.Close()
			return fmt.Errorf("decode heading path for chunk %s: %w", c.ChunkID, err)
		}
		if c.HeadingPath == nil {
			c.HeadingPath = []string{}
		}
		if pageStart.Valid {
			value := int(pageStart.Int64)
			c.PageStart = &value
		}
		if pageEnd.Valid {
			value := int(pageEnd.Int64)
			c.PageEnd = &value
		}
		c.IndexStatus = "indexed"
		c.Scope = map[bool]string{true: "case", false: "collection"}[c.CaseID != ""]
		item.scopeID = scope{c.TenantID, c.CollectionID, c.CaseID}
		records = append(records, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	principals := map[scope][]string{}
	for _, item := range records {
		members, ok := principals[item.scopeID]
		if !ok {
			members, err = ScopePrincipals(ctx, db, item.scopeID.tenant, item.scopeID.collection, item.scopeID.caseID)
			if err != nil {
				return err
			}
			principals[item.scopeID] = members
		}
		item.chunk.ACLPrincipals = members
		if err := fn(item.chunk); err != nil {
			return err
		}
	}
	return nil
}

type QueueCounts struct {
	Queued               int64   `json:"queued"`
	Processing           int64   `json:"processing"`
	Failed               int64   `json:"failed"`
	Completed            int64   `json:"completed"`
	OldestPendingSeconds float64 `json:"oldest_pending_seconds"`
}

type Status struct {
	Ingestion      QueueCounts `json:"ingestion"`
	Outbox         QueueCounts `json:"outbox"`
	PendingReviews int64       `json:"pending_reviews"`
	SchemaVersion  int64       `json:"schema_version"`
}

// LoadStatus summarizes durable queues; an empty tenantID reports all tenants (metrics), otherwise one tenant.
func LoadStatus(ctx context.Context, db *sql.DB, tenantID string) (Status, error) {
	var status Status
	err := db.QueryRowContext(ctx, `SELECT
		count(*) FILTER (WHERE status='queued'), count(*) FILTER (WHERE status='processing'),
		count(*) FILTER (WHERE status='failed'), count(*) FILTER (WHERE status='indexed'),
		COALESCE(EXTRACT(EPOCH FROM now()-min(created_at) FILTER (WHERE status IN ('queued','processing'))),0)::float8
		FROM ingestion_jobs WHERE NULLIF($1::text,'') IS NULL OR tenant_id=NULLIF($1::text,'')::uuid`, tenantID).
		Scan(&status.Ingestion.Queued, &status.Ingestion.Processing, &status.Ingestion.Failed, &status.Ingestion.Completed, &status.Ingestion.OldestPendingSeconds)
	if err != nil {
		return status, fmt.Errorf("load ingestion queue status: %w", err)
	}
	err = db.QueryRowContext(ctx, `SELECT
		count(*) FILTER (WHERE status='queued'), count(*) FILTER (WHERE status='processing'),
		count(*) FILTER (WHERE status='failed'), count(*) FILTER (WHERE status='done'),
		COALESCE(EXTRACT(EPOCH FROM now()-min(created_at) FILTER (WHERE status IN ('queued','processing'))),0)::float8
		FROM outbox_events WHERE NULLIF($1::text,'') IS NULL OR tenant_id=NULLIF($1::text,'')::uuid`, tenantID).
		Scan(&status.Outbox.Queued, &status.Outbox.Processing, &status.Outbox.Failed, &status.Outbox.Completed, &status.Outbox.OldestPendingSeconds)
	if err != nil {
		return status, fmt.Errorf("load outbox status: %w", err)
	}
	// Same liveness rules as the review queue: reviews of deleted, revoked or expired versions are not actionable.
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM document_reviews r
	JOIN documents d ON d.tenant_id=r.tenant_id AND d.id=r.document_id
	JOIN document_versions v ON v.tenant_id=r.tenant_id AND v.id=r.version_id
	WHERE r.status='pending' AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())
	AND (NULLIF($1::text,'') IS NULL OR r.tenant_id=NULLIF($1::text,'')::uuid)`, tenantID).Scan(&status.PendingReviews)
	if err != nil {
		return status, fmt.Errorf("load review status: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&status.SchemaVersion); err != nil {
		return status, fmt.Errorf("load schema version: %w", err)
	}
	return status, nil
}

// RequeueFailedOutbox gives failed cleanup/index events a fresh retry budget after the cause is fixed.
func RequeueFailedOutbox(ctx context.Context, db *sql.DB, tenantID string) (int64, error) {
	result, err := db.ExecContext(ctx, `UPDATE outbox_events SET status='queued',attempts=0,available_at=now(),lease_expires_at=NULL,updated_at=now()
	WHERE status='failed' AND (NULLIF($1::text,'') IS NULL OR tenant_id=NULLIF($1::text,'')::uuid)`, tenantID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
