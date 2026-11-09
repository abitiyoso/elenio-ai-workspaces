package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/chunking"
	"github.com/elenio/knowledge-workspace/backend/internal/docling"
	"github.com/elenio/knowledge-workspace/backend/internal/ops"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

const embeddingBatchSize = 32

const collectionPrincipalsQuery = ops.CollectionPrincipalsQuery

type Worker struct {
	db          *sql.DB
	store       platform.ObjectStore
	search      *platform.SearchClient
	embedding   platform.EmbeddingProvider
	docling     *docling.Client
	logger      *slog.Logger
	pollEvery   time.Duration
	lease       time.Duration
	chunkLength int
}

type Job struct {
	ID           string
	TenantID     string
	CollectionID string
	DocumentID   string
	VersionID    string
	Attempts     int
	MaxAttempts  int
}

type source struct {
	Key            string
	Filename       string
	Title          string
	Classification string
	CollectionID   string
	CaseID         string
	TrustStatus    string
	Members        []string
}

func NewWorker(db *sql.DB, store platform.ObjectStore, search *platform.SearchClient, embedding platform.EmbeddingProvider, converter *docling.Client, logger *slog.Logger) *Worker {
	return &Worker{db: db, store: store, search: search, embedding: embedding, docling: converter, logger: logger, pollEvery: 2 * time.Second, lease: 45 * time.Minute, chunkLength: 1200}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.db == nil || w.store == nil || w.search == nil || w.docling == nil {
		return errors.New("ingestion worker requires PostgreSQL, S3 storage, Elasticsearch and Docling")
	}
	ticker := time.NewTicker(w.pollEvery)
	defer ticker.Stop()
	lastExpirySweep := time.Time{}
	for {
		if time.Since(lastExpirySweep) >= time.Minute {
			if err := w.expireDueVersions(ctx); err != nil {
				return fmt.Errorf("expire due versions: %w", err)
			}
			lastExpirySweep = time.Now()
		}
		outbox, found, err := w.claimOutbox(ctx)
		if err != nil {
			return err
		}
		if found {
			if err := w.processOutbox(ctx, outbox); err != nil {
				w.logger.Error("cleanup outbox event failed", "event_id", outbox.ID, "tenant_id", outbox.TenantID, "error", err)
				if persistErr := w.failOutbox(ctx, outbox, err); persistErr != nil {
					w.logger.Error("failed to persist outbox retry state", "event_id", outbox.ID, "error", persistErr)
				}
			} else if err := w.completeOutbox(ctx, outbox.ID); err != nil {
				w.logger.Error("failed to complete outbox event", "event_id", outbox.ID, "error", err)
			}
			continue
		}
		job, found, err := w.claim(ctx)
		if err != nil {
			return err
		}
		if found {
			if err := w.process(ctx, job); err != nil {
				w.logger.Error("ingestion job failed", "job_id", job.ID, "tenant_id", job.TenantID, "error", err)
				if persistErr := w.fail(ctx, job, err); persistErr != nil {
					w.logger.Error("failed to persist ingestion failure state", "job_id", job.ID, "tenant_id", job.TenantID, "error", persistErr)
				}
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) expireDueVersions(ctx context.Context) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `WITH expired AS (
		UPDATE document_versions v SET publication_status='expired'
		FROM documents d
		WHERE d.tenant_id=v.tenant_id AND d.id=v.document_id AND v.expires_at<=now()
		AND v.publication_status NOT IN ('expired','revoked') AND d.deleted_at IS NULL
		RETURNING v.tenant_id,v.document_id,v.id,d.collection_id,d.case_id
	), cleared AS (
		UPDATE documents d SET published_version_id=NULL,lifecycle_status='expired',updated_at=now()
		FROM expired e WHERE d.tenant_id=e.tenant_id AND d.id=e.document_id AND d.published_version_id=e.id
		RETURNING d.tenant_id,d.id
	), publication AS (
		INSERT INTO publication_events(tenant_id,document_id,version_id,actor_id,event_type,rationale)
		SELECT tenant_id,document_id,id,'00000000-0000-4000-8000-000000000000'::uuid,'expired','automatic expiry'
		FROM expired RETURNING id
	), audit AS (
		INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,collection_id,case_id,version_id,outcome,rationale,details)
		SELECT tenant_id,'system','expiry-worker','document.expiry','document_version',id::text,collection_id,case_id,id,'allowed','automatic expiry',jsonb_build_object('source','expiry_worker')
		FROM expired RETURNING id
	), queued AS (
		INSERT INTO outbox_events(tenant_id,event_type,aggregate_id,payload)
		SELECT tenant_id,'version.delete_index',document_id,jsonb_build_object('version_id',id::text) FROM expired RETURNING id
	)
	SELECT count(*) FROM expired`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type outboxEvent struct {
	ID, TenantID, EventType string
	Payload                 []byte
	Attempts, MaxAttempts   int
}

func (w *Worker) claimOutbox(ctx context.Context) (outboxEvent, bool, error) {
	var event outboxEvent
	err := w.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id FROM outbox_events WHERE (status='queued' OR (status='processing' AND lease_expires_at < now()))
		AND available_at <= now() AND attempts < max_attempts ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE outbox_events e SET status='processing',attempts=e.attempts+1,lease_expires_at=now()+interval '60 seconds',updated_at=now()
	FROM candidate WHERE e.id=candidate.id RETURNING e.id::text,e.tenant_id::text,e.event_type,e.payload,e.attempts,e.max_attempts`).
		Scan(&event.ID, &event.TenantID, &event.EventType, &event.Payload, &event.Attempts, &event.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return outboxEvent{}, false, nil
	}
	if err != nil {
		return outboxEvent{}, false, fmt.Errorf("claim cleanup outbox: %w", err)
	}
	return event, true, nil
}

func (w *Worker) processOutbox(ctx context.Context, event outboxEvent) error {
	var payload struct {
		VersionID         string   `json:"version_id"`
		VersionIDs        []string `json:"version_ids"`
		ObjectKeys        []string `json:"object_keys"`
		TrustStatus       string   `json:"trust_status"`
		PublicationStatus string   `json:"publication_status"`
		CollectionID      string   `json:"collection_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode outbox payload: %w", err)
	}
	switch event.EventType {
	case "version.publish":
		return w.search.UpdateVersionMetadata(ctx, event.TenantID, payload.VersionID, payload.TrustStatus, payload.PublicationStatus)
	case "version.delete_index":
		return w.search.DeleteVersion(ctx, event.TenantID, payload.VersionID)
	case "scope.acl_refresh":
		return w.refreshScopePrincipals(ctx, event.TenantID, payload.CollectionID)
	case "document.delete":
		for _, versionID := range payload.VersionIDs {
			if err := w.search.DeleteVersion(ctx, event.TenantID, versionID); err != nil {
				return err
			}
		}
		for _, key := range payload.ObjectKeys {
			if err := w.store.Delete(ctx, key); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported outbox event type %q", event.EventType)
	}
	return nil
}

func (w *Worker) failOutbox(ctx context.Context, event outboxEvent, cause error) error {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	delay := retryDelay(event.Attempts)
	_, err := w.db.ExecContext(ctx, `UPDATE outbox_events SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'queued' END,
	available_at=now()+($2 * interval '1 second'),lease_expires_at=NULL,last_error=$3,updated_at=now() WHERE id=$1 AND status='processing'`, event.ID, int(delay.Seconds()), message)
	return err
}

func (w *Worker) completeOutbox(ctx context.Context, eventID string) error {
	_, err := w.db.ExecContext(ctx, `UPDATE outbox_events SET status='done',lease_expires_at=NULL,last_error=NULL,updated_at=now() WHERE id=$1 AND status='processing'`, eventID)
	return err
}

func (w *Worker) claim(ctx context.Context) (Job, bool, error) {
	var job Job
	err := w.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id FROM ingestion_jobs
		WHERE (status='queued' OR (status='processing' AND lease_expires_at < now()))
		AND available_at <= now() AND attempts < max_attempts
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1
	)
	UPDATE ingestion_jobs j SET status='processing',stage='loading',attempts=j.attempts+1,
	lease_expires_at=now()+($1 * interval '1 second'),updated_at=now()
	FROM candidate WHERE j.id=candidate.id
	RETURNING j.id::text,j.tenant_id::text,j.collection_id::text,j.document_id::text,j.version_id::text,j.attempts,j.max_attempts`, int(w.lease.Seconds())).Scan(&job.ID, &job.TenantID, &job.CollectionID, &job.DocumentID, &job.VersionID, &job.Attempts, &job.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("claim ingestion job: %w", err)
	}
	return job, true, nil
}

func (w *Worker) process(ctx context.Context, job Job) error {
	jobCtx, cancel := context.WithTimeout(ctx, 35*time.Minute)
	defer cancel()
	input, err := w.loadSource(jobCtx, job)
	if err != nil {
		return err
	}
	content, err := w.store.Get(jobCtx, input.Key)
	if err != nil {
		return fmt.Errorf("load original object: %w", err)
	}
	if err := w.setStage(jobCtx, job.ID, "parsing"); err != nil {
		return err
	}
	parseStarted := time.Now()
	result, err := w.docling.Convert(jobCtx, input.Filename, "application/pdf", content, func(taskID string) error {
		_, updateErr := w.db.ExecContext(jobCtx, `UPDATE ingestion_jobs SET docling_task_id=$2,stage='parsing',lease_expires_at=now()+($3 * interval '1 second'),updated_at=now() WHERE id=$1`, job.ID, taskID, int(w.lease.Seconds()))
		return updateErr
	})
	if err != nil {
		return err
	}
	parseDuration := time.Since(parseStarted)
	markdownKey := fmt.Sprintf("%s/documents/%s/versions/%s/parsed.md", job.TenantID, job.DocumentID, job.VersionID)
	jsonKey := fmt.Sprintf("%s/documents/%s/versions/%s/docling.json", job.TenantID, job.DocumentID, job.VersionID)
	if err := w.store.Put(jobCtx, markdownKey, "text/markdown; charset=utf-8", []byte(result.Markdown)); err != nil {
		return fmt.Errorf("store parsed Markdown: %w", err)
	}
	if err := w.store.Put(jobCtx, jsonKey, "application/json", result.JSON); err != nil {
		return fmt.Errorf("store structured Docling JSON: %w", err)
	}
	chunks, err := chunking.Normalize(result.JSON, result.Markdown, job.VersionID, w.chunkLength)
	if err != nil {
		return fmt.Errorf("normalize Docling result: %w", err)
	}
	if err := w.persistParsed(jobCtx, job, input, result, markdownKey, jsonKey, chunks); err != nil {
		_ = w.store.Delete(jobCtx, markdownKey)
		_ = w.store.Delete(jobCtx, jsonKey)
		return err
	}
	if err := w.setStage(jobCtx, job.ID, "indexing"); err != nil {
		return err
	}
	active, err := w.jobStillActive(jobCtx, job)
	if err != nil {
		return err
	}
	if !active {
		return errors.New("document version was revoked, deleted, or expired during ingestion")
	}
	parserStatus := "success"
	if result.Status == "partial_success" {
		parserStatus = "partial"
	}
	timings, err := w.indexChunks(jobCtx, job, input, chunks, parserStatus)
	if err != nil {
		return err
	}
	active, err = w.jobStillActive(jobCtx, job)
	if err != nil {
		return err
	}
	if !active {
		return errors.New("document version was revoked, deleted, or expired during indexing")
	}
	versionUpdate, err := w.db.ExecContext(jobCtx, `UPDATE document_versions SET index_status='indexed' WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, job.TenantID, job.VersionID)
	if err != nil {
		return err
	}
	if count, countErr := versionUpdate.RowsAffected(); countErr != nil {
		return countErr
	} else if count != 1 {
		_ = w.search.DeleteVersion(jobCtx, job.TenantID, job.VersionID)
		return errors.New("document version became inactive before index completion")
	}
	_, err = w.db.ExecContext(jobCtx, `UPDATE documents SET lifecycle_status='ready_for_review',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND lifecycle_status='processing'`, job.TenantID, job.DocumentID)
	if err != nil {
		return err
	}
	completed, err := w.db.ExecContext(jobCtx, `UPDATE ingestion_jobs j SET status='indexed',stage='indexed',last_error=NULL,lease_expires_at=NULL,updated_at=now()
	WHERE j.id=$1 AND j.status='processing' AND EXISTS(SELECT 1 FROM documents d JOIN document_versions v ON v.tenant_id=d.tenant_id AND v.document_id=d.id
	WHERE d.tenant_id=j.tenant_id AND d.id=j.document_id AND v.id=j.version_id AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now()))`, job.ID)
	if err != nil {
		return err
	}
	if count, countErr := completed.RowsAffected(); countErr != nil {
		return countErr
	} else if count != 1 {
		_ = w.search.DeleteVersion(jobCtx, job.TenantID, job.VersionID)
		return errors.New("ingestion job became inactive before completion")
	}
	w.logger.Info("ingestion job indexed", "job_id", job.ID, "tenant_id", job.TenantID, "document_id", job.DocumentID, "chunk_count", len(chunks), "embedding_model_id", modelID(w.embedding),
		"parser_status", parserStatus, "docling_ms", parseDuration.Milliseconds(), "embedding_ms", timings.embedding.Milliseconds(), "index_ms", timings.indexing.Milliseconds(), "attempt", job.Attempts)
	return nil
}

func (w *Worker) loadSource(ctx context.Context, job Job) (source, error) {
	var input source
	rows, err := w.db.QueryContext(ctx, `SELECT v.original_object_key,v.original_filename,d.title,d.collection_id::text,c.classification,COALESCE(d.case_id::text,''),v.trust_status
	FROM document_versions v JOIN documents d ON d.tenant_id=v.tenant_id AND d.id=v.document_id
	JOIN collections c ON c.tenant_id=d.tenant_id AND c.id=d.collection_id
	JOIN knowledge_sources ks ON ks.tenant_id=d.tenant_id AND ks.document_id=d.id
	WHERE v.tenant_id=$1 AND v.document_id=$2 AND v.id=$3 AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())`, job.TenantID, job.DocumentID, job.VersionID)
	if err != nil {
		return input, err
	}
	if !rows.Next() {
		rows.Close()
		return input, errors.New("ingestion source was not found")
	}
	if err := rows.Scan(&input.Key, &input.Filename, &input.Title, &input.CollectionID, &input.Classification, &input.CaseID, &input.TrustStatus); err != nil {
		rows.Close()
		return input, err
	}
	if err := rows.Close(); err != nil {
		return input, err
	}
	input.Members, err = ops.ScopePrincipals(ctx, w.db, job.TenantID, job.CollectionID, input.CaseID)
	return input, err
}

// refreshScopePrincipals rewrites the Elasticsearch ACL projection of a collection and each of its cases
// from current PostgreSQL memberships.
func (w *Worker) refreshScopePrincipals(ctx context.Context, tenantID, collectionID string) error {
	if collectionID == "" {
		return errors.New("ACL refresh event has no collection ID")
	}
	rows, err := w.db.QueryContext(ctx, `SELECT case_id::text FROM case_scopes WHERE tenant_id=$1 AND collection_id=$2`, tenantID, collectionID)
	if err != nil {
		return err
	}
	scopes := []string{""}
	for rows.Next() {
		var caseID string
		if err := rows.Scan(&caseID); err != nil {
			rows.Close()
			return err
		}
		scopes = append(scopes, caseID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, caseID := range scopes {
		members, err := ops.ScopePrincipals(ctx, w.db, tenantID, collectionID, caseID)
		if err != nil {
			return err
		}
		if err := w.search.UpdateScopePrincipals(ctx, tenantID, collectionID, caseID, members); err != nil {
			return err
		}
	}
	w.logger.Info("search ACL projection refreshed", "tenant_id", tenantID, "collection_id", collectionID, "scopes", len(scopes))
	return nil
}

func (w *Worker) persistParsed(ctx context.Context, job Job, input source, result docling.Result, markdownKey, jsonKey string, chunks []chunking.Chunk) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())
	FROM documents d JOIN document_versions v ON v.tenant_id=d.tenant_id AND v.document_id=d.id
	WHERE d.tenant_id=$1 AND d.id=$2 AND v.id=$3 FOR UPDATE OF d,v`, job.TenantID, job.DocumentID, job.VersionID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return errors.New("document version became inactive during parsing")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_chunks WHERE tenant_id=$1 AND version_id=$2`, job.TenantID, job.VersionID); err != nil {
		return err
	}
	for _, artifact := range []struct{ kind, key, mediaType string }{{"markdown", markdownKey, "text/markdown; charset=utf-8"}, {"docling_json", jsonKey, "application/json"}} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO parser_artifacts(tenant_id,version_id,artifact_type,object_key,media_type) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(tenant_id,version_id,artifact_type) DO UPDATE SET object_key=EXCLUDED.object_key,media_type=EXCLUDED.media_type,created_at=now()`, job.TenantID, job.VersionID, artifact.kind, artifact.key, artifact.mediaType); err != nil {
			return err
		}
	}
	for _, chunk := range chunks {
		headingPath := nonNilHeadingPath(chunk.HeadingPath)
		if _, err := tx.ExecContext(ctx, `INSERT INTO document_chunks(chunk_id,tenant_id,collection_id,case_id,document_id,version_id,chunk_number,content,heading_path,page_start,page_end,source_anchor)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''))`, chunk.ID, job.TenantID, job.CollectionID, input.CaseID, job.DocumentID, job.VersionID, chunk.Number, chunk.Content, headingPath, chunk.PageStart, chunk.PageEnd, chunk.SourceAnchor); err != nil {
			return fmt.Errorf("persist chunk %d: %w", chunk.Number, err)
		}
	}
	parserStatus := "success"
	if result.Status == "partial_success" {
		parserStatus = "partial"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET parser_status=$3,index_status='pending' WHERE tenant_id=$1 AND id=$2`, job.TenantID, job.VersionID, parserStatus); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ingestion_jobs SET stage='chunking',updated_at=now(),lease_expires_at=now()+($2 * interval '1 second') WHERE id=$1`, job.ID, int(w.lease.Seconds())); err != nil {
		return err
	}
	return tx.Commit()
}

func nonNilHeadingPath(path []string) []string {
	if path == nil {
		return []string{}
	}
	return path
}

type stageTimings struct{ embedding, indexing time.Duration }

func (w *Worker) indexChunks(ctx context.Context, job Job, input source, chunks []chunking.Chunk, parserStatus string) (stageTimings, error) {
	var timings stageTimings
	if err := w.search.EnsureIndex(ctx); err != nil {
		return timings, fmt.Errorf("ensure search index: %w", err)
	}
	for start := 0; start < len(chunks); start += embeddingBatchSize {
		batch := chunks[start:min(start+embeddingBatchSize, len(chunks))]
		vectors, err := w.embedBatch(ctx, batch, &timings)
		if err != nil {
			return timings, err
		}
		indexed := make([]platform.IndexedChunk, 0, len(batch))
		ids := make([]string, 0, len(batch))
		for offset, item := range batch {
			var vector []float32
			if vectors != nil {
				vector = vectors[offset]
			}
			indexed = append(indexed, indexedChunk(job, input, item, vector, parserStatus, w.embedding))
			ids = append(ids, item.ID)
		}
		started := time.Now()
		if err := w.search.BulkIndexChunks(ctx, indexed); err != nil {
			return timings, fmt.Errorf("index chunks: %w", err)
		}
		timings.indexing += time.Since(started)
		if _, err := w.db.ExecContext(ctx, `UPDATE document_chunks SET indexed_at=now(),embedding_model_id=NULLIF($3::text,'') WHERE tenant_id=$1 AND chunk_id=ANY($2::text[])`, job.TenantID, ids, modelID(w.embedding)); err != nil {
			return timings, err
		}
		if err := w.touchLease(ctx, job.ID); err != nil {
			return timings, err
		}
	}
	// The job is reported as indexed next; refresh so that status means "searchable now".
	started := time.Now()
	if err := w.search.Refresh(ctx); err != nil {
		return timings, err
	}
	timings.indexing += time.Since(started)
	return timings, nil
}

// embedBatch returns nil vectors when no embedding provider is configured (keyword-only index).
func (w *Worker) embedBatch(ctx context.Context, batch []chunking.Chunk, timings *stageTimings) ([][]float32, error) {
	if w.embedding == nil {
		return nil, nil
	}
	texts := make([]string, 0, len(batch))
	for _, item := range batch {
		texts = append(texts, item.Content)
	}
	started := time.Now()
	vectors, err := w.embedding.Embed(ctx, texts)
	timings.embedding += time.Since(started)
	if err != nil {
		return nil, fmt.Errorf("embed chunks: %w", err)
	}
	if len(vectors) != len(texts) {
		return nil, errors.New("embedding provider returned a mismatched vector count")
	}
	for _, vector := range vectors {
		if len(vector) != w.embedding.Dimensions() {
			return nil, errors.New("embedding provider returned a vector with the wrong dimension")
		}
	}
	return vectors, nil
}

func indexedChunk(job Job, input source, item chunking.Chunk, vector []float32, parserStatus string, provider platform.EmbeddingProvider) platform.IndexedChunk {
	chunk := platform.IndexedChunk{
		ChunkID: item.ID, TenantID: job.TenantID, DocumentID: job.DocumentID, VersionID: job.VersionID,
		CollectionID: job.CollectionID, CaseID: input.CaseID, Scope: map[bool]string{true: "case", false: "collection"}[input.CaseID != ""], Classification: input.Classification,
		TrustStatus: input.TrustStatus, PublicationStatus: "draft", ParserStatus: parserStatus, IndexStatus: "indexed", Title: input.Title, HeadingPath: item.HeadingPath,
		Content: item.Content, PageStart: item.PageStart, PageEnd: item.PageEnd, SourceAnchor: item.SourceAnchor,
		ACLPrincipals: input.Members,
	}
	if provider != nil {
		chunk.EmbeddingModelID = provider.ModelID()
		chunk.Embedding = vector
	}
	return chunk
}

func (w *Worker) setStage(ctx context.Context, jobID, stage string) error {
	_, err := w.db.ExecContext(ctx, `UPDATE ingestion_jobs SET stage=$2,updated_at=now(),lease_expires_at=now()+($3 * interval '1 second') WHERE id=$1`, jobID, stage, int(w.lease.Seconds()))
	return err
}

func (w *Worker) jobStillActive(ctx context.Context, job Job) (bool, error) {
	var active bool
	err := w.db.QueryRowContext(ctx, `SELECT d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL
	AND (v.expires_at IS NULL OR v.expires_at>now()) FROM documents d JOIN document_versions v
	ON v.tenant_id=d.tenant_id AND v.document_id=d.id WHERE d.tenant_id=$1 AND d.id=$2 AND v.id=$3`,
		job.TenantID, job.DocumentID, job.VersionID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return active, err
}

func (w *Worker) touchLease(ctx context.Context, jobID string) error {
	_, err := w.db.ExecContext(ctx, `UPDATE ingestion_jobs SET lease_expires_at=now()+($2 * interval '1 second'),updated_at=now() WHERE id=$1`, jobID, int(w.lease.Seconds()))
	return err
}

func (w *Worker) fail(ctx context.Context, job Job, cause error) error {
	lastError := strings.TrimSpace(cause.Error())
	if len(lastError) > 500 {
		lastError = lastError[:500]
	}
	delay := retryDelay(job.Attempts)
	terminal := job.Attempts >= job.MaxAttempts
	parserStatus, indexStatus := "pending", "pending"
	if terminal {
		parserStatus, indexStatus = "failed", "failed"
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failure state transaction: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE ingestion_jobs SET status=CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'queued' END,
	stage=CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'retry_wait' END,
	available_at=now()+($2 * interval '1 second'),lease_expires_at=NULL,last_error=$3,updated_at=now() WHERE id=$1 AND status='processing'`, job.ID, int(delay.Seconds()), lastError)
	if err != nil {
		return fmt.Errorf("update failed ingestion job: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check failed ingestion job update: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("failed ingestion job %s was not in processing state", job.ID)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET parser_status=$3,index_status=$4 WHERE tenant_id=$1 AND id=$2`, job.TenantID, job.VersionID, parserStatus, indexStatus); err != nil {
		return fmt.Errorf("update failed document version state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ingestion failure state: %w", err)
	}
	return nil
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 15 * time.Second
	for current := 1; current < attempt && delay < 300*time.Second; current++ {
		delay *= 2
		if delay > 300*time.Second {
			delay = 300 * time.Second
		}
	}
	return delay
}

func modelID(provider platform.EmbeddingProvider) string {
	if provider == nil {
		return ""
	}
	return provider.ModelID()
}
