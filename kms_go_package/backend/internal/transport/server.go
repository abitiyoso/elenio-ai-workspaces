package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/governance"
	"github.com/elenio/knowledge-workspace/backend/internal/observability"
	"github.com/elenio/knowledge-workspace/backend/internal/ops"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
	searchsvc "github.com/elenio/knowledge-workspace/backend/internal/search"
	"github.com/elenio/knowledge-workspace/backend/internal/workspace"
)

type Dependencies struct {
	Logger         *slog.Logger
	Database       *platform.Database
	Search         *platform.SearchClient
	ObjectStore    platform.ObjectStore
	Embedding      platform.EmbeddingProvider
	MaxUploadBytes int64
	Auth           auth.AuthAdapter
	// Principals resolves X-KMS-Delegated-User on the retrieval endpoint; nil uses tenant_principals in Database.
	Principals auth.PrincipalDirectory
	Metrics    *observability.Metrics
	Operations OperationsConfig
	// SelfApprovalExempt is config.SelfApprovalExempt (development only).
	SelfApprovalExempt []string
}

// OperationsConfig is non-secret deployment information reported by the status endpoint.
type OperationsConfig struct {
	Environment    string
	AuthMode       string
	DoclingBaseURL string
	// MetricsToken protects /metrics outside development; without it the endpoint is disabled there.
	MetricsToken string
}
type contextKey string

const identityKey contextKey = "identity"

func NewServer(d Dependencies) http.Handler {
	mux := http.NewServeMux()
	workspaces := workspace.NewService(d.Database.SQL(), d.ObjectStore, d.MaxUploadBytes)
	searchService := searchsvc.NewService(d.Database.SQL(), d.Search, d.Embedding)
	governanceService := governance.NewService(d.Database.SQL()).WithSelfApprovalExempt(d.SelfApprovalExempt)
	serviceAuth := auth.NewServiceAuthenticator(d.Database.SQL())
	principals := d.Principals
	if principals == nil {
		principals = auth.NewSQLPrincipalDirectory(d.Database.SQL())
	}
	metrics := d.Metrics
	if metrics == nil {
		metrics = observability.NewMetrics()
	}
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		if !metricsAllowed(d.Operations, r) {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var gauges []observability.Gauge
		if db := d.Database.SQL(); db != nil {
			status, err := ops.LoadStatus(ctx, db, "")
			if err != nil {
				logInternal(r, "metrics status query failed", err)
			}
			gauges = statusGauges(status, err == nil)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = metrics.WritePrometheus(w, gauges)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]string{}
		status := http.StatusOK
		if d.Database.Ping(ctx) != nil {
			checks["postgres"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else {
			checks["postgres"] = "ok"
		}
		if d.Search.Ping(ctx) != nil {
			checks["elasticsearch"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else {
			checks["elasticsearch"] = "ok"
		}
		if d.ObjectStore == nil {
			checks["object_storage"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else if d.ObjectStore.Ping(ctx) != nil {
			checks["object_storage"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else {
			checks["object_storage"] = "ok"
		}
		checks["embeddings"] = platform.EmbeddingStatus(d.Embedding)
		writeJSON(w, status, map[string]any{"status": map[bool]string{true: "ready", false: "not_ready"}[status == http.StatusOK], "checks": checks})
	})
	mux.Handle("GET /api/v1/ops/status", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOperationsStatus(w, r, d, identityFromRequest(r))
	})))
	mux.Handle("GET /api/v1/session", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, _ := r.Context().Value(identityKey).(auth.Identity)
		writeJSON(w, http.StatusOK, map[string]any{"user_id": identity.UserID, "tenant_id": identity.TenantID, "roles": identity.Roles})
	})))
	mux.Handle("GET /api/v1/collections", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := identityFromRequest(r)
		items, err := workspaces.ListCollections(r.Context(), identity)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"collections": items})
	})))
	mux.Handle("POST /api/v1/collections", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name           string `json:"name"`
			Description    string `json:"description"`
			Classification string `json:"classification"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		item, err := workspaces.CreateCollection(r.Context(), identityFromRequest(r), input.Name, input.Description, input.Classification)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"collection": item})
	})))
	mux.Handle("POST /api/v1/collections/{collectionID}/members", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PrincipalID string `json:"principal_id"`
			Role        string `json:"role"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		if err := workspaces.AddCollectionMember(r.Context(), identityFromRequest(r), r.PathValue("collectionID"), input.PrincipalID, input.Role); err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "granted"})
	})))
	mux.Handle("POST /api/v1/collections/{collectionID}/cases", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		item, err := workspaces.CreateCase(r.Context(), identityFromRequest(r), r.PathValue("collectionID"), input.Name)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"case": item})
	})))
	mux.Handle("POST /api/v1/cases/{caseID}/members", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PrincipalID string `json:"principal_id"`
			Role        string `json:"role"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		if err := workspaces.AddCaseMember(r.Context(), identityFromRequest(r), r.PathValue("caseID"), input.PrincipalID, input.Role); err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "granted"})
	})))
	mux.Handle("GET /api/v1/documents", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items, err := workspaces.ListDocuments(r.Context(), identityFromRequest(r))
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"documents": items})
	})))
	mux.Handle("GET /api/v1/documents/{documentID}/versions", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items, err := governanceService.Versions(r.Context(), identityFromRequest(r), r.PathValue("documentID"))
		if err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"versions": items})
	})))
	mux.Handle("POST /api/v1/documents/{documentID}/versions", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maxUpload := d.MaxUploadBytes
		if maxUpload <= 0 {
			maxUpload = 50 << 20
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_upload", "Upload must contain a PDF file.")
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_upload", "Upload must contain a PDF file.")
			return
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_upload", "Unable to read the uploaded file.")
			return
		}
		if int64(len(content)) > maxUpload {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "PDF exceeds the configured upload limit.")
			return
		}
		item, job, err := workspaces.UploadVersion(r.Context(), identityFromRequest(r), r.PathValue("documentID"), header.Filename, content)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"document": item, "job": job})
	})))
	mux.Handle("GET /api/v1/reviews", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items, err := governanceService.ListReviews(r.Context(), identityFromRequest(r))
		if err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"reviews": items})
	})))
	for _, decision := range []string{"approved", "rejected"} {
		action, status := decision, decision
		if action == "approved" {
			action = "approve"
		}
		if action == "rejected" {
			action = "reject"
		}
		mux.Handle("POST /api/v1/reviews/{reviewID}/"+action, authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Rationale string `json:"rationale"`
			}
			if err := decodeJSON(w, r, &input, 64<<10); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
				return
			}
			if err := governanceService.DecideReview(r.Context(), identityFromRequest(r), r.PathValue("reviewID"), status, input.Rationale, correlationID(r)); err != nil {
				writeGovernanceError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": status})
		})))
	}
	mux.Handle("POST /api/v1/documents/{documentID}/submit-review", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			VersionID string `json:"version_id"`
			Note      string `json:"note"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		item, err := governanceService.SubmitReview(r.Context(), identityFromRequest(r), r.PathValue("documentID"), input.VersionID, input.Note, correlationID(r))
		if err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"review": item, "status": "submitted"})
	})))
	mux.Handle("POST /api/v1/documents/{documentID}/versions/{versionID}/publish", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Rationale string `json:"rationale"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		if err := governanceService.PublishVersion(r.Context(), identityFromRequest(r), r.PathValue("documentID"), r.PathValue("versionID"), input.Rationale, correlationID(r)); err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
	})))
	mux.Handle("POST /api/v1/documents/{documentID}/versions/{versionID}/revoke", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Rationale string `json:"rationale"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		if err := governanceService.RevokeVersion(r.Context(), identityFromRequest(r), r.PathValue("documentID"), r.PathValue("versionID"), input.Rationale, correlationID(r)); err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
	})))
	mux.Handle("PATCH /api/v1/documents/{documentID}/versions/{versionID}/validity", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ExpiresAt *time.Time `json:"expires_at"`
			Rationale string     `json:"rationale"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		if err := governanceService.SetExpiry(r.Context(), identityFromRequest(r), r.PathValue("documentID"), r.PathValue("versionID"), input.ExpiresAt, input.Rationale, correlationID(r)); err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	})))
	mux.Handle("DELETE /api/v1/documents/{documentID}", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Rationale string `json:"rationale"`
		}
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		if err := governanceService.DeleteDocument(r.Context(), identityFromRequest(r), r.PathValue("documentID"), input.Rationale, correlationID(r)); err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "cleanup_queued"})
	})))
	mux.Handle("GET /api/v1/activity", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items, err := governanceService.Activity(r.Context(), identityFromRequest(r), r.URL.Query().Get("collection_id"), r.URL.Query().Get("case_id"), 100)
		if err != nil {
			writeGovernanceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"activity": items})
	})))
	mux.Handle("POST /api/v1/documents", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maxUpload := d.MaxUploadBytes
		if maxUpload <= 0 {
			maxUpload = 50 << 20
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_upload", "Upload must contain a PDF file and required form fields.")
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_upload", "Upload must contain a PDF file.")
			return
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_upload", "Unable to read the uploaded file.")
			return
		}
		if int64(len(content)) > maxUpload {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "PDF exceeds the configured upload limit.")
			return
		}
		item, job, err := workspaces.UploadPDF(r.Context(), identityFromRequest(r), r.FormValue("collection_id"), r.FormValue("case_id"), r.FormValue("title"), header.Filename, content)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"document": item, "job": job})
	})))
	mux.Handle("GET /api/v1/ingestion/jobs/{jobID}", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item, err := workspaces.GetJob(r.Context(), identityFromRequest(r), r.PathValue("jobID"))
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": item})
	})))
	mux.Handle("POST /api/v1/ingestion/jobs/{jobID}/retry", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := workspaces.RetryJob(r.Context(), identityFromRequest(r), r.PathValue("jobID")); err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	})))
	mux.Handle("GET /api/v1/documents/{documentID}/versions/{versionID}/preview", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, err := workspaces.OriginalPDF(r.Context(), identityFromRequest(r), r.PathValue("documentID"), r.PathValue("versionID"))
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	})))
	mux.Handle("POST /api/v1/search", authMiddleware(d.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input searchsvc.Request
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		results, err := searchService.Search(r.Context(), identityFromRequest(r), input)
		if err != nil {
			if errors.Is(err, searchsvc.ErrEmbeddingUnavailable) || platform.IsEmbeddingUnavailable(err) {
				writeError(w, http.StatusServiceUnavailable, "embedding_unavailable", "Semantic search is unavailable until an embedding provider is configured.")
				return
			}
			if errors.Is(err, searchsvc.ErrForbidden) {
				writeError(w, http.StatusForbidden, "forbidden", "Search scope is not authorized.")
				return
			}
			if errors.Is(err, searchsvc.ErrInvalidRequest) {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			logInternal(r, "search request failed", err)
			writeError(w, http.StatusBadGateway, "search_unavailable", "Search service request failed.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	})))
	mux.HandleFunc("POST /api/v1/retrieval/query", func(w http.ResponseWriter, r *http.Request) {
		serviceIdentity, err := serviceAuth.Authenticate(r.Context(), r, auth.RetrievalAudience)
		if err != nil {
			if errors.Is(err, auth.ErrServiceUnauthenticated) {
				writeError(w, http.StatusUnauthorized, "invalid_service_credential", "A valid, active retrieval service credential is required.")
			} else {
				logInternal(r, "service credential lookup failed", err)
				writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			}
			return
		}
		db := d.Database.SQL()
		audit := retrievalAudit{TenantID: serviceIdentity.TenantID, ClientID: serviceIdentity.ClientID}
		// X-KMS-Delegated-User replaces the forwarded user session for clients holding retrieval:delegate, so a
		// runtime without a live user token can act for a tenant principal. Without the header nothing changes.
		humanIdentity, delegated, err := auth.ResolveDelegatedUser(r.Context(), r, serviceIdentity, principals)
		audit.Delegated = delegated
		if err != nil {
			var rejection *auth.DelegationRejection
			if errors.As(err, &rejection) {
				audit.Outcome, audit.Code, audit.Reason = "denied", "delegated_user_required", rejection.Reason
				if !retrievalAuditOrFail(w, r.Context(), db, audit, "the request was not processed") {
					return
				}
				writeError(w, http.StatusUnauthorized, "delegated_user_required", "A delegated user that is active in the service tenant is required.")
				return
			}
			logInternal(r, "delegated user lookup failed", err)
			audit.Outcome, audit.Code, audit.Reason = "failed", "delegated_user_unavailable", "verifier_unavailable"
			if !retrievalAuditOrFail(w, r.Context(), db, audit, "the request was not processed") {
				return
			}
			writeError(w, http.StatusServiceUnavailable, "delegated_user_unavailable", "The delegated user could not be verified right now.")
			return
		}
		if !delegated {
			humanIdentity, err = d.Auth.Authenticate(r.Context(), r)
			if err != nil || humanIdentity.TenantID != serviceIdentity.TenantID {
				audit.Outcome, audit.Code = "denied", "delegated_user_required"
				if !retrievalAuditOrFail(w, r.Context(), db, audit, "the request was not processed") {
					return
				}
				writeError(w, http.StatusUnauthorized, "delegated_user_required", "A verified user session in the service tenant is required.")
				return
			}
		}
		audit.UserID = humanIdentity.UserID
		var input searchsvc.Request
		if err := decodeJSON(w, r, &input, 64<<10); err != nil {
			audit.Outcome, audit.Code = "failed", "invalid_request"
			if !retrievalAuditOrFail(w, r.Context(), db, audit, "the request was not processed") {
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
			return
		}
		results, err := searchService.SearchService(r.Context(), serviceIdentity, humanIdentity, input)
		if err != nil {
			audit.Outcome, audit.Code = "failed", "search_failed"
			if errors.Is(err, searchsvc.ErrForbidden) {
				audit.Outcome, audit.Code = "denied", "forbidden"
			}
			if errors.Is(err, searchsvc.ErrInvalidRequest) {
				audit.Code = "invalid_request"
			}
			if errors.Is(err, searchsvc.ErrEmbeddingUnavailable) {
				audit.Code = "embedding_unavailable"
			}
			if !retrievalAuditOrFail(w, r.Context(), db, audit, "the request was not completed") {
				return
			}
			if errors.Is(err, searchsvc.ErrForbidden) {
				writeError(w, http.StatusForbidden, "forbidden", "The requested retrieval scope is not authorized.")
			} else if errors.Is(err, searchsvc.ErrInvalidRequest) {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			} else if errors.Is(err, searchsvc.ErrEmbeddingUnavailable) {
				writeError(w, http.StatusServiceUnavailable, "embedding_unavailable", "Semantic search is unavailable until an embedding provider is configured.")
			} else {
				logInternal(r, "retrieval search failed", err)
				writeError(w, http.StatusBadGateway, "search_unavailable", "Search service request failed.")
			}
			return
		}
		audit.Outcome, audit.ResultCount = "allowed", len(results)
		if !retrievalAuditOrFail(w, r.Context(), db, audit, "the results were not returned") {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	})
	return instrument(d.Logger, metrics, mux)
}

type auditExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// retrievalAudit is one retrieval.query audit row. UserID is the verified (or delegated) human; Delegated marks
// requests that named the human with X-KMS-Delegated-User; Reason is the stable delegation refusal code.
type retrievalAudit struct {
	TenantID, ClientID, UserID string
	Outcome, Code, Reason      string
	ResultCount                int
	Delegated                  bool
}

func persistRetrievalAudit(ctx context.Context, db auditExecer, entry retrievalAudit) error {
	_, err := db.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,outcome,details)
	VALUES($1,'service',$2,'retrieval.query','search','retrieval',$3,jsonb_build_object('error_code',NULLIF($4,''),'user_id',NULLIF($5,''),'result_count',$6::int,'delegated',$7::boolean,'reason',NULLIF($8,'')))`,
		entry.TenantID, entry.ClientID, entry.Outcome, entry.Code, entry.UserID, entry.ResultCount, entry.Delegated, entry.Reason)
	return err
}

func retrievalAuditOrFail(w http.ResponseWriter, ctx context.Context, db auditExecer, entry retrievalAudit, consequence string) bool {
	if err := persistRetrievalAudit(ctx, db, entry); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Retrieval audit storage is unavailable; "+consequence+".")
		return false
	}
	return true
}

func identityFromRequest(r *http.Request) auth.Identity {
	identity, _ := r.Context().Value(identityKey).(auth.Identity)
	return identity
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "The requested resource is not authorized.")
	case errors.Is(err, workspace.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "The requested resource was not found.")
	case errors.Is(err, workspace.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "A collection with this name already exists.")
	case errors.Is(err, workspace.ErrStorageMissing):
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "Object storage is not configured.")
	case strings.Contains(err.Error(), "exceeds the configured maximum"):
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "PDF exceeds the configured upload limit.")
	case strings.Contains(err.Error(), "must contain") || strings.Contains(err.Error(), "must be a PDF") || strings.Contains(err.Error(), "must be internal") || strings.Contains(err.Error(), "must use UUID") || strings.Contains(err.Error(), "must be a UUID") || strings.Contains(err.Error(), "classification must"):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case strings.Contains(err.Error(), "membership role"):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		logInternal(r, "workspace request failed", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func writeGovernanceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, governance.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "The requested governance action is not authorized.")
	case errors.Is(err, governance.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "The requested resource was not found or is no longer active.")
	case strings.Contains(err.Error(), "rationale") || strings.Contains(err.Error(), "only draft") || strings.Contains(err.Error(), "only approved") || strings.Contains(err.Error(), "decision must") || strings.Contains(err.Error(), "cannot be published"):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		logInternal(r, "governance request failed", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "retryable": status >= 500}})
}

func authMiddleware(adapter auth.AuthAdapter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := adapter.Authenticate(r.Context(), r)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, auth.ErrVerifierNotConfigured) || errors.Is(err, auth.ErrVerifierUnavailable) {
				status = http.StatusServiceUnavailable
			}
			writeJSON(w, status, map[string]any{"error": map[string]string{"code": "unauthenticated", "message": "A verified host session is required."}})
			return
		}
		if info := observability.RequestInfoFrom(r.Context()); info != nil {
			info.TenantID = identity.TenantID
		}
		r = r.WithContext(context.WithValue(r.Context(), identityKey, identity))
		next.ServeHTTP(w, r)
	})
}

// instrument assigns a request ID, logs one structured line per request (matched route, tenant, status,
// latency; never bodies or query text) and records HTTP metrics.
func instrument(logger *slog.Logger, metrics *observability.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := observability.RequestID(r.Header.Get("X-Request-ID"))
		info := &observability.RequestInfo{ID: requestID, Logger: logger.With("request_id", requestID)}
		w.Header().Set("X-Request-ID", requestID)
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		routed := r.WithContext(observability.WithRequestInfo(r.Context(), info))
		next.ServeHTTP(wrapped, routed)
		route := routed.Pattern
		if route == "" {
			route = "unmatched"
		}
		elapsed := time.Since(started)
		metrics.ObserveHTTP(r.Method, route, wrapped.status, elapsed)
		level := slog.LevelInfo
		if route == "GET /healthz" || route == "GET /readyz" || route == "GET /metrics" {
			level = slog.LevelDebug
		}
		attrs := []any{"method", r.Method, "route", route, "status", wrapped.status, "duration_ms", elapsed.Milliseconds()}
		if info.TenantID != "" {
			attrs = append(attrs, "tenant_id", info.TenantID)
		}
		info.Logger.Log(r.Context(), level, "http request", attrs...)
	})
}

// logInternal records the cause of a 5xx that the client only sees as a generic error.
func logInternal(r *http.Request, message string, err error) {
	if info := observability.RequestInfoFrom(r.Context()); info != nil && info.Logger != nil {
		info.Logger.Error(message, "error", err, "tenant_id", info.TenantID)
	}
}

// correlationID ties audit rows to logs: the caller's X-Correlation-ID when present, else the request ID.
func correlationID(r *http.Request) string {
	if supplied := strings.TrimSpace(r.Header.Get("X-Correlation-ID")); supplied != "" && len(supplied) <= 128 {
		return supplied
	}
	if info := observability.RequestInfoFrom(r.Context()); info != nil {
		return info.ID
	}
	return ""
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
