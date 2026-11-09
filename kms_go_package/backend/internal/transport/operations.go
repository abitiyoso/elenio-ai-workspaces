package transport

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/observability"
	"github.com/elenio/knowledge-workspace/backend/internal/ops"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

func metricsAllowed(cfg OperationsConfig, r *http.Request) bool {
	if cfg.MetricsToken != "" {
		parts := strings.Fields(r.Header.Get("Authorization"))
		return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && subtle.ConstantTimeCompare([]byte(parts[1]), []byte(cfg.MetricsToken)) == 1
	}
	return cfg.Environment == "development"
}

func statusGauges(status ops.Status, ok bool) []observability.Gauge {
	up := 0.0
	if ok {
		up = 1
	}
	gauges := []observability.Gauge{{Name: "kms_status_query_up", Help: "Whether queue status could be read from PostgreSQL.", Value: up}}
	if !ok {
		return gauges
	}
	queue := func(name, help string, counts ops.QueueCounts) {
		for state, value := range map[string]int64{"queued": counts.Queued, "processing": counts.Processing, "failed": counts.Failed, "completed": counts.Completed} {
			gauges = append(gauges, observability.Gauge{Name: name, Help: help, Labels: map[string]string{"status": state}, Value: float64(value)})
		}
	}
	queue("kms_ingestion_jobs", "Ingestion jobs by status.", status.Ingestion)
	queue("kms_outbox_events", "Index and storage cleanup outbox events by status.", status.Outbox)
	return append(gauges,
		observability.Gauge{Name: "kms_ingestion_oldest_pending_seconds", Help: "Age of the oldest queued or processing ingestion job.", Value: status.Ingestion.OldestPendingSeconds},
		observability.Gauge{Name: "kms_outbox_oldest_pending_seconds", Help: "Age of the oldest queued or processing outbox event (index backlog).", Value: status.Outbox.OldestPendingSeconds},
		observability.Gauge{Name: "kms_pending_reviews", Help: "Version reviews awaiting a decision.", Value: float64(status.PendingReviews)},
		observability.Gauge{Name: "kms_schema_version", Help: "Highest applied database migration.", Value: float64(status.SchemaVersion)},
	)
}

type dependencyStatus struct {
	Postgres      string `json:"postgres"`
	Elasticsearch string `json:"elasticsearch"`
	ObjectStorage string `json:"object_storage"`
	Docling       string `json:"docling"`
	Embeddings    string `json:"embeddings"`
}

type searchIndexStatus struct {
	Name          string   `json:"name"`
	AliasTargets  []string `json:"alias_targets"`
	IndexedChunks *int64   `json:"indexed_chunks"`
	ExpectedChunk int64    `json:"expected_chunks"`
	InSync        *bool    `json:"in_sync"`
}

type embeddingStatus struct {
	Enabled    bool   `json:"enabled"`
	ModelID    string `json:"model_id,omitempty"`
	Dimensions int    `json:"dimensions,omitempty"`
}

// writeOperationsStatus reports tenant-scoped health for the Settings screen. It exposes no secrets,
// endpoints or document content: only states and counts.
func writeOperationsStatus(w http.ResponseWriter, r *http.Request, d Dependencies, identity auth.Identity) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	db := d.Database.SQL()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, "status_unavailable", "Operational status is unavailable.")
		return
	}
	status, err := ops.LoadStatus(ctx, db, identity.TenantID)
	if err != nil {
		logInternal(r, "operations status query failed", err)
		writeError(w, http.StatusServiceUnavailable, "status_unavailable", "Operational status is unavailable.")
		return
	}
	deps := dependencyStatus{Postgres: "ok", Elasticsearch: "ok", ObjectStorage: "ok", Docling: "not_configured", Embeddings: platform.EmbeddingStatus(d.Embedding)}
	index := searchIndexStatus{AliasTargets: []string{}}
	if d.Search == nil || d.Search.Ping(ctx) != nil {
		deps.Elasticsearch = "unavailable"
	} else {
		index.Name = d.Search.IndexName()
		if targets, err := d.Search.AliasTargets(ctx, index.Name); err == nil && targets != nil {
			index.AliasTargets = targets
		}
		if count, err := d.Search.CountChunks(ctx, identity.TenantID); err == nil {
			index.IndexedChunks = &count
		} else {
			logInternal(r, "search index count failed", err)
		}
	}
	if d.ObjectStore == nil || d.ObjectStore.Ping(ctx) != nil {
		deps.ObjectStorage = "unavailable"
	}
	if d.Operations.DoclingBaseURL != "" {
		deps.Docling = probe(ctx, d.Operations.DoclingBaseURL+"/health")
	}
	if expected, err := ops.ExpectedIndexedChunks(ctx, db, identity.TenantID); err == nil {
		index.ExpectedChunk = expected
		if index.IndexedChunks != nil {
			inSync := *index.IndexedChunks == expected
			index.InSync = &inSync
		}
	} else {
		logInternal(r, "expected chunk count failed", err)
	}
	embedding := embeddingStatus{Enabled: d.Embedding != nil}
	if d.Embedding != nil {
		embedding.ModelID, embedding.Dimensions = d.Embedding.ModelID(), d.Embedding.Dimensions()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"environment": d.Operations.Environment, "auth_mode": d.Operations.AuthMode, "schema_version": status.SchemaVersion,
		"dependencies": deps, "search_index": index, "embedding": embedding,
		"ingestion": status.Ingestion, "outbox": status.Outbox, "pending_reviews": status.PendingReviews,
		"checked_at": time.Now().UTC(),
	})
}

var probeClient = &http.Client{Timeout: 2 * time.Second}

func probe(ctx context.Context, target string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "unavailable"
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		return "unavailable"
	}
	resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "ok"
	}
	return "unavailable"
}
