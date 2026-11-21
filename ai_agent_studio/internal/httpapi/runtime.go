package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
)

const maxRunBody = 256 << 10

var (
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
	refPattern         = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	keyPattern         = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,62}$`)
	uuidPattern        = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	caseClasses        = map[string]bool{"positive": true, "negative": true, "adversarial": true, "evidence_conflict": true, "missing_evidence": true}
	feedbackClasses    = map[string]bool{"factual_error": true, "missing_evidence": true, "format": true, "policy": true, "style": true, "other": true}
)

type RuntimeStore interface {
	CreateRun(context.Context, string, postgres.RunCreate) (postgres.Run, bool, error)
	GetRun(context.Context, string, string) (postgres.Run, error)
	ListRuns(context.Context, string, postgres.RunFilter) ([]postgres.Run, int, error)
	ListRunEvents(context.Context, string, string) ([]postgres.RunEvent, error)
	ListRunEvidence(context.Context, string, string) ([]postgres.EvidenceRef, error)
	RequestCancel(context.Context, string, string, string) (postgres.Run, error)
	CreateEvalSuite(context.Context, string, string, string, string, string) (postgres.EvalSuite, error)
	ListEvalSuites(context.Context, string, string) ([]postgres.EvalSuite, error)
	CreateEvalCase(context.Context, string, string, postgres.EvalCaseInput) (postgres.EvalCase, error)
	ListEvalCases(context.Context, string, string) ([]postgres.EvalCase, error)
	DeleteEvalCase(context.Context, string, string, string) error
	StartEvalRun(context.Context, string, string, string, string, bool) (postgres.EvalRun, error)
	GetEvalRun(context.Context, string, string) (postgres.EvalRun, error)
	ListEvalRuns(context.Context, string, string, int, int) ([]postgres.EvalRun, int, error)
	CreateReleaseRequest(context.Context, string, string, string, string, string, string) (postgres.ReleaseRequest, error)
	ReviewRelease(context.Context, string, string, string, bool, string) (postgres.ReleaseRequest, error)
	DecideRelease(context.Context, string, string, string, bool, string) (postgres.ReleaseRequest, error)
	WithdrawReleaseRequest(context.Context, string, string, string) (postgres.ReleaseRequest, error)
	RollbackRelease(context.Context, string, string, string, string) (postgres.Release, error)
	ListReleaseRequests(context.Context, string, string) ([]postgres.ReleaseRequest, error)
	ListReleases(context.Context, string, string) ([]postgres.Release, error)
	ObserveRelease(context.Context, string, string, string, string) (postgres.ReleaseEvidence, error)
	CreateFeedback(context.Context, string, string, string, string, string, bool, string) (postgres.FeedbackItem, error)
	ListFeedback(context.Context, string, string) ([]postgres.FeedbackItem, error)
	RunMetrics(context.Context, string, time.Duration) (postgres.RunMetrics, error)
	ListAuditEvents(context.Context, string, int, int) ([]postgres.AuditEvent, int, error)
	QueueDepth(context.Context, string) (map[string]int, error)
}

// IntegrationStatus is static deployment configuration reported to operators.
type IntegrationStatus struct {
	WorkerEnabled         bool   `json:"worker_enabled"`
	DevMockModel          bool   `json:"dev_mock_model"`
	KMSMode               string `json:"kms_mode"`
	FlowcaseCallback      bool   `json:"flowcase_callback_configured"`
	ServiceClients        int    `json:"service_clients"`
	ReleaseEnvironment    string `json:"release_environment"`
	TraceRetentionDays    int    `json:"trace_retention_days"`
	EinoVersion           string `json:"eino_version"`
	ProviderAdapterFamily string `json:"provider_adapter"`
}

type RuntimeHandler struct {
	Store              RuntimeStore
	Auth               ports.AuthAdapter
	Authorizer         ports.Authorizer
	AllowDevMock       bool
	ReleaseEnvironment string
	Status             IntegrationStatus
	Limiter            *RateLimiter
}

func RegisterRuntimeRoutes(mux *http.ServeMux, h RuntimeHandler) {
	if h.Limiter == nil {
		h.Limiter = NewRateLimiter(60, time.Minute)
	}
	if h.ReleaseEnvironment == "" {
		h.ReleaseEnvironment = "production"
	}
	for _, prefix := range []string{"/api/v1/runs", "/api/v1/sandbox/runs", "/api/v1/evaluations", "/api/v1/releases",
		"/api/v1/feedback", "/api/v1/metrics", "/api/v1/integrations/status", "/api/v1/audit-events"} {
		mux.Handle(prefix, h)
		mux.Handle(prefix+"/", h)
	}
}

func (h RuntimeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, err := h.Auth.VerifyRequest(r.Context(), r)
	if err != nil || principal.TenantID == "" || principal.Subject == "" {
		writeError(w, http.StatusUnauthorized, "verified tenant identity is required")
		return
	}
	resource := "tenant:" + principal.TenantID
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/"), "/")
	service := principal.Kind == "service"
	// Service clients reach run endpoints only.
	if service && parts[0] != "runs" {
		writeError(w, http.StatusForbidden, "service clients may only invoke and observe runs")
		return
	}
	c := call{w: w, r: r, p: principal, resource: resource, h: h}
	switch {
	case parts[0] == "runs" && len(parts) == 1:
		if r.Method == http.MethodPost {
			c.createRun()
		} else {
			c.listRuns()
		}
	case parts[0] == "runs" && len(parts) == 2:
		c.getRun(parts[1])
	case parts[0] == "runs" && len(parts) == 3 && parts[2] == "events":
		c.runEvents(parts[1])
	case parts[0] == "runs" && len(parts) == 3 && parts[2] == "cancel":
		c.cancelRun(parts[1])
	case parts[0] == "sandbox" && len(parts) == 2 && parts[1] == "runs":
		c.sandboxRun()
	case parts[0] == "evaluations":
		c.evaluations(parts[1:])
	case parts[0] == "releases":
		c.releases(parts[1:])
	case parts[0] == "feedback" && len(parts) == 1:
		c.feedback()
	case parts[0] == "metrics" && len(parts) == 1:
		c.metrics()
	case parts[0] == "integrations" && len(parts) == 2:
		c.integrations()
	case parts[0] == "audit-events" && len(parts) == 1:
		c.audit()
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

type call struct {
	w        http.ResponseWriter
	r        *http.Request
	p        ports.Principal
	resource string
	h        RuntimeHandler
}

func (c call) allow(action string) bool {
	if err := c.h.Authorizer.Authorize(c.r.Context(), c.p, action, c.resource); err != nil {
		writeError(c.w, http.StatusForbidden, "action is not authorized")
		return false
	}
	return true
}

func (c call) method(methods ...string) bool {
	for _, m := range methods {
		if c.r.Method == m {
			return true
		}
	}
	methodNotAllowed(c.w, methods...)
	return false
}

func (c call) rateLimited() bool {
	if !c.h.Limiter.Allow(c.p.TenantID) {
		c.w.Header().Set("Retry-After", "10")
		writeError(c.w, http.StatusTooManyRequests, "run rate limit exceeded for this tenant")
		return true
	}
	return false
}

func (c call) queryInt(name string, fallback int) int {
	if v, err := strconv.Atoi(c.r.URL.Query().Get(name)); err == nil {
		return v
	}
	return fallback
}

// runError maps repository errors to stable API responses without leaking internals.
func (c call) runError(err error) {
	switch {
	case errors.Is(err, postgres.ErrAgentNotFound), errors.Is(err, postgres.ErrRunNotFound):
		writeError(c.w, http.StatusNotFound, "not found")
	case errors.Is(err, postgres.ErrNoActiveRelease):
		writeError(c.w, http.StatusConflict, "agent has no active release in this environment")
	case errors.Is(err, postgres.ErrVersionMismatch):
		writeError(c.w, http.StatusConflict, "requested version is not the active release")
	case errors.Is(err, postgres.ErrIdempotencyConflict):
		writeError(c.w, http.StatusConflict, "idempotency key was already used with a different request")
	case errors.Is(err, postgres.ErrBackpressure):
		writeError(c.w, http.StatusTooManyRequests, "too many queued or running runs for this tenant")
	case errors.Is(err, postgres.ErrModelNotApproved):
		writeError(c.w, http.StatusConflict, "no model deployment is approved for this agent version and environment")
	case errors.Is(err, postgres.ErrSelfApproval):
		writeError(c.w, http.StatusForbidden, "separation of duties: requester and reviewer cannot decide their own release")
	case errors.Is(err, postgres.ErrEvaluationStale):
		writeError(c.w, http.StatusConflict, "agent version changed after its evaluation; re-run the evaluation")
	case errors.Is(err, postgres.ErrEvaluationBlocked):
		writeError(c.w, http.StatusConflict, "evaluation gate did not pass for this version")
	case errors.Is(err, postgres.ErrNoRollbackTarget):
		writeError(c.w, http.StatusConflict, "there is no previous release to roll back to")
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(c.w, http.StatusConflict, "operation is not allowed in the current state")
	default:
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "violates") {
			writeError(c.w, http.StatusConflict, "request conflicts with existing records")
			return
		}
		writeError(c.w, http.StatusInternalServerError, "request could not be completed")
	}
}

// flowcaseRunRequest matches Flowcase's agent.StartRunRequest plus the case
// reference and correlation id its invocation contract carries.
// DelegatedUser is the Control Plane user UUID (the case owner, which is also
// the KMS principal id) that KMS retrieval for this run is delegated to. Only
// service callers may set it; it is sent to KMS in the configured user header.
type flowcaseRunRequest struct {
	IdempotencyKey  string          `json:"idempotency_key"`
	AgentID         string          `json:"agent_id"`
	SkillVersion    string          `json:"skill_version"`
	Input           json.RawMessage `json:"input"`
	Evidence        json.RawMessage `json:"evidence"`
	CaseID          string          `json:"case_id"`
	CorrelationID   string          `json:"correlation_id"`
	DelegatedUser   string          `json:"delegated_user"`
	DeadlineSeconds int             `json:"deadline_seconds"`
}

func (c call) createRun() {
	action := "run.create"
	if c.p.Kind == "service" {
		action = "run.invoke"
	}
	if !c.allow(action) || c.rateLimited() {
		return
	}
	var body flowcaseRunRequest
	if !decodeBoundedJSON(c.w, c.r, maxRunBody, &body) {
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = c.r.Header.Get("Idempotency-Key")
	}
	if !idempotencyPattern.MatchString(body.IdempotencyKey) || !keyPattern.MatchString(body.AgentID) || !isJSONObject(body.Input) {
		writeError(c.w, http.StatusBadRequest, "idempotency_key, agent_id and an input object are required")
		return
	}
	if (body.CaseID != "" && !refPattern.MatchString(body.CaseID)) || (body.CorrelationID != "" && !refPattern.MatchString(body.CorrelationID)) {
		writeError(c.w, http.StatusBadRequest, "case_id and correlation_id must be short reference strings")
		return
	}
	if body.DelegatedUser != "" {
		// Human runs never delegate: a person's own session is the only
		// identity their runs may act with.
		if c.p.Kind != "service" {
			writeError(c.w, http.StatusBadRequest, "delegated_user is accepted from service clients only")
			return
		}
		body.DelegatedUser = strings.ToLower(body.DelegatedUser)
		if !uuidPattern.MatchString(body.DelegatedUser) {
			writeError(c.w, http.StatusBadRequest, "delegated_user must be a user UUID")
			return
		}
	}
	if len(body.Evidence) == 0 || string(body.Evidence) == "null" {
		body.Evidence = json.RawMessage(`[]`)
	}
	if !isJSONArray(body.Evidence) {
		writeError(c.w, http.StatusBadRequest, "evidence must be an array of references")
		return
	}
	run, created, err := c.h.Store.CreateRun(c.r.Context(), c.p.TenantID, postgres.RunCreate{
		AgentKey: body.AgentID, Kind: "production", CallerKind: callerKind(c.p), CallerSubject: c.p.Subject,
		IdempotencyKey: body.IdempotencyKey, ExternalCorrelationID: body.CorrelationID, CaseRef: body.CaseID, DelegatedUser: body.DelegatedUser,
		PinVersion: body.SkillVersion, Input: body.Input, Evidence: body.Evidence,
		Deadline: time.Duration(body.DeadlineSeconds) * time.Second, ReleaseEnvironment: c.h.ReleaseEnvironment,
	})
	if err != nil {
		c.runError(err)
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	c.writeRun(status, run)
}

func callerKind(p ports.Principal) string {
	if p.Kind == "service" {
		return "service"
	}
	return "user"
}

func (c call) writeRun(status int, run postgres.Run) {
	if c.p.Kind != "service" {
		writeJSON(c.w, status, run)
		return
	}
	result := postgres.FlowcaseRunResult{RunID: run.ID, Status: postgres.FlowcaseStatus(run.Status), AgentID: run.AgentKey, SkillVersion: run.AgentSemver, TraceIDs: []string{run.ID}, UsageObservation: postgres.ObserveRunUsage(run)}
	if run.ErrorCode != nil {
		result.Error = *run.ErrorCode
	}
	if run.Status == "succeeded" || run.Status == "needs_review" {
		evidence, err := c.h.Store.ListRunEvidence(c.r.Context(), c.p.TenantID, run.ID)
		if err != nil {
			// Retryable for the caller: never return a settled run without its source manifest.
			writeError(c.w, http.StatusServiceUnavailable, "run evidence could not be loaded")
			return
		}
		if output, err := postgres.FlowcaseOutput(run, evidence); err == nil {
			result.Output = output
			result.OutputChecksum, _ = postgres.OutputChecksum(output)
		}
	}
	writeJSON(c.w, status, result)
}

// serviceOwns keeps a service client to the runs it started itself.
func (c call) serviceOwns(run postgres.Run) bool {
	return c.p.Kind != "service" || run.CallerSubject == c.p.Subject
}

func (c call) getRun(id string) {
	if !c.method(http.MethodGet) || !validCatalogID(id) && notFound(c.w) || !c.allow("run.read") {
		return
	}
	run, err := c.h.Store.GetRun(c.r.Context(), c.p.TenantID, id)
	if err != nil || !c.serviceOwns(run) {
		writeError(c.w, http.StatusNotFound, "not found")
		return
	}
	if c.p.Kind == "service" {
		c.writeRun(http.StatusOK, run)
		return
	}
	evidence, err := c.h.Store.ListRunEvidence(c.r.Context(), c.p.TenantID, id)
	if err != nil {
		c.runError(err)
		return
	}
	writeJSON(c.w, http.StatusOK, map[string]any{"run": run, "evidence": evidence})
}

func notFound(w http.ResponseWriter) bool {
	writeError(w, http.StatusNotFound, "not found")
	return true
}

func (c call) listRuns() {
	if !c.method(http.MethodGet) || !c.allow("run.read") {
		return
	}
	if c.p.Kind == "service" {
		writeError(c.w, http.StatusForbidden, "service clients read runs by id")
		return
	}
	q := c.r.URL.Query()
	items, total, err := c.h.Store.ListRuns(c.r.Context(), c.p.TenantID, postgres.RunFilter{
		AgentKey: q.Get("agent_key"), Kind: q.Get("kind"), Status: q.Get("status"), Limit: c.queryInt("limit", 25), Offset: c.queryInt("offset", 0),
	})
	if err != nil {
		c.runError(err)
		return
	}
	writeJSON(c.w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (c call) runEvents(id string) {
	if !c.method(http.MethodGet) || !validCatalogID(id) && notFound(c.w) || !c.allow("run.read") {
		return
	}
	if c.p.Kind == "service" {
		writeError(c.w, http.StatusForbidden, "trace is available to authorized users only")
		return
	}
	events, err := c.h.Store.ListRunEvents(c.r.Context(), c.p.TenantID, id)
	if err != nil {
		c.runError(err)
		return
	}
	writeJSON(c.w, http.StatusOK, map[string]any{"items": events})
}

func (c call) cancelRun(id string) {
	if !c.method(http.MethodPost) || !validCatalogID(id) && notFound(c.w) || !c.allow("run.cancel") {
		return
	}
	if c.p.Kind == "service" {
		run, err := c.h.Store.GetRun(c.r.Context(), c.p.TenantID, id)
		if err != nil || !c.serviceOwns(run) {
			writeError(c.w, http.StatusNotFound, "not found")
			return
		}
	}
	run, err := c.h.Store.RequestCancel(c.r.Context(), c.p.TenantID, id, c.p.Subject)
	if err != nil {
		c.runError(err)
		return
	}
	c.writeRun(http.StatusAccepted, run)
}

func (c call) sandboxRun() {
	if !c.method(http.MethodPost) || !c.allow("sandbox.run") || c.rateLimited() {
		return
	}
	var body struct {
		AgentKey        string          `json:"agent_key"`
		AgentVersionID  string          `json:"agent_version_id"`
		Input           json.RawMessage `json:"input"`
		EvidenceFixture json.RawMessage `json:"evidence_fixture"`
		IdempotencyKey  string          `json:"idempotency_key"`
	}
	if !decodeBoundedJSON(c.w, c.r, maxRunBody, &body) {
		return
	}
	if !keyPattern.MatchString(body.AgentKey) || !validCatalogID(body.AgentVersionID) || !isJSONObject(body.Input) {
		writeError(c.w, http.StatusBadRequest, "agent_key, agent_version_id and an input object are required")
		return
	}
	if len(body.EvidenceFixture) == 0 || string(body.EvidenceFixture) == "null" {
		body.EvidenceFixture = json.RawMessage(`[]`)
	}
	if !isJSONArray(body.EvidenceFixture) {
		writeError(c.w, http.StatusBadRequest, "evidence_fixture must be an array")
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = "sandbox:" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if !idempotencyPattern.MatchString(body.IdempotencyKey) {
		writeError(c.w, http.StatusBadRequest, "idempotency_key is invalid")
		return
	}
	run, created, err := c.h.Store.CreateRun(c.r.Context(), c.p.TenantID, postgres.RunCreate{
		AgentKey: body.AgentKey, AgentVersionID: body.AgentVersionID, Kind: "sandbox", CallerKind: "user", CallerSubject: c.p.Subject,
		IdempotencyKey: body.IdempotencyKey, Input: body.Input, Evidence: body.EvidenceFixture, AllowDevMock: c.h.AllowDevMock,
	})
	if err != nil {
		c.runError(err)
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	writeJSON(c.w, status, run)
}

func (c call) evaluations(parts []string) {
	switch {
	case len(parts) == 1 && parts[0] == "suites":
		if c.r.Method == http.MethodPost {
			if !c.allow("eval.write") {
				return
			}
			var body struct {
				AgentKey    string `json:"agent_key"`
				Key         string `json:"key"`
				Description string `json:"description"`
			}
			if !decodeBoundedJSON(c.w, c.r, 16<<10, &body) {
				return
			}
			if !keyPattern.MatchString(body.AgentKey) || !keyPattern.MatchString(body.Key) || len(body.Description) > 2000 {
				writeError(c.w, http.StatusBadRequest, "agent_key and key are required")
				return
			}
			item, err := c.h.Store.CreateEvalSuite(c.r.Context(), c.p.TenantID, body.AgentKey, body.Key, body.Description, c.p.Subject)
			if err != nil {
				c.runError(err)
				return
			}
			writeJSON(c.w, http.StatusCreated, item)
			return
		}
		if !c.method(http.MethodGet) || !c.allow("eval.read") {
			return
		}
		items, err := c.h.Store.ListEvalSuites(c.r.Context(), c.p.TenantID, c.r.URL.Query().Get("agent_key"))
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, map[string]any{"items": items})
	case len(parts) == 3 && parts[0] == "suites" && parts[2] == "cases" && validCatalogID(parts[1]):
		if c.r.Method == http.MethodPost {
			if !c.allow("eval.write") {
				return
			}
			var body struct {
				Key             string          `json:"key"`
				Classification  string          `json:"classification"`
				Critical        bool            `json:"critical"`
				Input           json.RawMessage `json:"input"`
				EvidenceFixture json.RawMessage `json:"evidence_fixture"`
				Expectations    json.RawMessage `json:"expectations"`
			}
			if !decodeBoundedJSON(c.w, c.r, maxRunBody, &body) {
				return
			}
			if !keyPattern.MatchString(body.Key) || !caseClasses[body.Classification] || !isJSONObject(body.Input) || (len(body.EvidenceFixture) > 0 && !isJSONArray(body.EvidenceFixture)) || (len(body.Expectations) > 0 && !isJSONObject(body.Expectations)) {
				writeError(c.w, http.StatusBadRequest, "key, input object, evidence_fixture array and expectations object are required")
				return
			}
			item, err := c.h.Store.CreateEvalCase(c.r.Context(), c.p.TenantID, parts[1], postgres.EvalCaseInput{
				Key: body.Key, Classification: body.Classification, Critical: body.Critical, Input: body.Input,
				Evidence: body.EvidenceFixture, Expectations: body.Expectations, CreatedBy: c.p.Subject,
			})
			if err != nil {
				c.runError(err)
				return
			}
			writeJSON(c.w, http.StatusCreated, item)
			return
		}
		if !c.method(http.MethodGet) || !c.allow("eval.read") {
			return
		}
		items, err := c.h.Store.ListEvalCases(c.r.Context(), c.p.TenantID, parts[1])
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, map[string]any{"items": items})
	case len(parts) == 2 && parts[0] == "cases" && validCatalogID(parts[1]):
		if !c.method(http.MethodDelete) || !c.allow("eval.write") {
			return
		}
		if err := c.h.Store.DeleteEvalCase(c.r.Context(), c.p.TenantID, parts[1], c.p.Subject); err != nil {
			c.runError(err)
			return
		}
		c.w.WriteHeader(http.StatusNoContent)
	case len(parts) == 1 && parts[0] == "runs":
		if c.r.Method == http.MethodPost {
			if !c.allow("eval.run") || c.rateLimited() {
				return
			}
			var body struct {
				SuiteID        string `json:"suite_id"`
				AgentVersionID string `json:"agent_version_id"`
			}
			if !decodeBoundedJSON(c.w, c.r, 4<<10, &body) {
				return
			}
			if !validCatalogID(body.SuiteID) || !validCatalogID(body.AgentVersionID) {
				writeError(c.w, http.StatusBadRequest, "suite_id and agent_version_id are required")
				return
			}
			item, err := c.h.Store.StartEvalRun(c.r.Context(), c.p.TenantID, body.SuiteID, body.AgentVersionID, c.p.Subject, c.h.AllowDevMock)
			if err != nil {
				c.runError(err)
				return
			}
			writeJSON(c.w, http.StatusAccepted, item)
			return
		}
		if !c.method(http.MethodGet) || !c.allow("eval.read") {
			return
		}
		items, total, err := c.h.Store.ListEvalRuns(c.r.Context(), c.p.TenantID, c.r.URL.Query().Get("agent_key"), c.queryInt("limit", 25), c.queryInt("offset", 0))
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, map[string]any{"items": items, "total": total})
	case len(parts) == 2 && parts[0] == "runs" && validCatalogID(parts[1]):
		if !c.method(http.MethodGet) || !c.allow("eval.read") {
			return
		}
		item, err := c.h.Store.GetEvalRun(c.r.Context(), c.p.TenantID, parts[1])
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, item)
	default:
		writeError(c.w, http.StatusNotFound, "route not found")
	}
}

type decisionBody struct {
	Approve bool   `json:"approve"`
	Reason  string `json:"reason"`
}

func (c call) decision() (decisionBody, bool) {
	var body decisionBody
	if !decodeBoundedJSON(c.w, c.r, 8<<10, &body) {
		return body, false
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > 2000 {
		writeError(c.w, http.StatusBadRequest, "a reason of at most 2000 characters is required")
		return body, false
	}
	return body, true
}

func (c call) releases(parts []string) {
	switch {
	case len(parts) == 1 && parts[0] == "evidence":
		if !c.method(http.MethodGet) || !c.allow("release.read") {
			return
		}
		query := c.r.URL.Query()
		for key, values := range query {
			if (key != "agent_key" && key != "skill_version") || len(values) != 1 {
				writeError(c.w, http.StatusBadRequest, "supply only one agent_key and optional skill_version")
				return
			}
		}
		key, pin := query.Get("agent_key"), query.Get("skill_version")
		if !keyPattern.MatchString(key) || (pin != "" && !refPattern.MatchString(pin)) {
			writeError(c.w, http.StatusBadRequest, "agent_key and optional skill_version must be valid references")
			return
		}
		evidence, err := c.h.Store.ObserveRelease(c.r.Context(), c.p.TenantID, key, pin, c.h.ReleaseEnvironment)
		if err != nil {
			c.runError(err)
			return
		}
		c.w.Header().Set("Cache-Control", "no-store")
		c.w.Header().Set("ETag", strconv.Quote(evidence.ContentHash))
		writeJSON(c.w, http.StatusOK, evidence)
	case len(parts) == 0:
		if !c.method(http.MethodGet) || !c.allow("release.read") {
			return
		}
		items, err := c.h.Store.ListReleases(c.r.Context(), c.p.TenantID, c.r.URL.Query().Get("agent_key"))
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, map[string]any{"items": items})
	case len(parts) == 1 && parts[0] == "requests":
		if c.r.Method == http.MethodPost {
			if !c.allow("release.request") {
				return
			}
			var body struct {
				AgentVersionID string `json:"agent_version_id"`
				EvalRunID      string `json:"eval_run_id"`
				Environment    string `json:"environment"`
				Note           string `json:"note"`
			}
			if !decodeBoundedJSON(c.w, c.r, 8<<10, &body) {
				return
			}
			if !validCatalogID(body.AgentVersionID) || !validCatalogID(body.EvalRunID) || (body.Environment != "staging" && body.Environment != "production") || len(body.Note) > 2000 {
				writeError(c.w, http.StatusBadRequest, "agent_version_id, eval_run_id and environment (staging or production) are required")
				return
			}
			item, err := c.h.Store.CreateReleaseRequest(c.r.Context(), c.p.TenantID, body.AgentVersionID, body.EvalRunID, body.Environment, body.Note, c.p.Subject)
			if err != nil {
				c.runError(err)
				return
			}
			writeJSON(c.w, http.StatusCreated, item)
			return
		}
		if !c.method(http.MethodGet) || !c.allow("release.read") {
			return
		}
		items, err := c.h.Store.ListReleaseRequests(c.r.Context(), c.p.TenantID, c.r.URL.Query().Get("status"))
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, map[string]any{"items": items})
	case len(parts) == 3 && parts[0] == "requests" && validCatalogID(parts[1]):
		if !c.method(http.MethodPost) {
			return
		}
		var item postgres.ReleaseRequest
		var err error
		switch parts[2] {
		case "domain-review":
			if !c.allow("release.review") {
				return
			}
			body, ok := c.decision()
			if !ok {
				return
			}
			item, err = c.h.Store.ReviewRelease(c.r.Context(), c.p.TenantID, parts[1], c.p.Subject, body.Approve, body.Reason)
		case "ops-decision":
			if !c.allow("release.approve") {
				return
			}
			body, ok := c.decision()
			if !ok {
				return
			}
			item, err = c.h.Store.DecideRelease(c.r.Context(), c.p.TenantID, parts[1], c.p.Subject, body.Approve, body.Reason)
		case "withdraw":
			if !c.allow("release.request") {
				return
			}
			item, err = c.h.Store.WithdrawReleaseRequest(c.r.Context(), c.p.TenantID, parts[1], c.p.Subject)
		default:
			writeError(c.w, http.StatusNotFound, "route not found")
			return
		}
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, item)
	case len(parts) == 2 && validCatalogID(parts[0]) && parts[1] == "rollback":
		if !c.method(http.MethodPost) || !c.allow("release.rollback") {
			return
		}
		body, ok := c.decision()
		if !ok {
			return
		}
		item, err := c.h.Store.RollbackRelease(c.r.Context(), c.p.TenantID, parts[0], c.p.Subject, body.Reason)
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusOK, item)
	default:
		writeError(c.w, http.StatusNotFound, "route not found")
	}
}

func (c call) feedback() {
	if c.r.Method == http.MethodPost {
		if !c.allow("feedback.create") {
			return
		}
		var body struct {
			AgentKey          string `json:"agent_key"`
			RunID             string `json:"run_id"`
			Classification    string `json:"classification"`
			Note              string `json:"note"`
			ReusableCandidate bool   `json:"reusable_candidate"`
		}
		if !decodeBoundedJSON(c.w, c.r, 16<<10, &body) {
			return
		}
		if !keyPattern.MatchString(body.AgentKey) || !feedbackClasses[body.Classification] || (body.RunID != "" && !validCatalogID(body.RunID)) || strings.TrimSpace(body.Note) == "" || len(body.Note) > 4000 {
			writeError(c.w, http.StatusBadRequest, "agent_key, classification and a note are required")
			return
		}
		item, err := c.h.Store.CreateFeedback(c.r.Context(), c.p.TenantID, body.AgentKey, body.RunID, body.Classification, body.Note, body.ReusableCandidate, c.p.Subject)
		if err != nil {
			c.runError(err)
			return
		}
		writeJSON(c.w, http.StatusCreated, item)
		return
	}
	if !c.method(http.MethodGet) || !c.allow("feedback.read") {
		return
	}
	items, err := c.h.Store.ListFeedback(c.r.Context(), c.p.TenantID, c.r.URL.Query().Get("agent_key"))
	if err != nil {
		c.runError(err)
		return
	}
	writeJSON(c.w, http.StatusOK, map[string]any{"items": items})
}

func (c call) metrics() {
	if !c.method(http.MethodGet) || !c.allow("metrics.read") {
		return
	}
	hours := c.queryInt("window_hours", 24)
	if hours < 1 || hours > 24*30 {
		hours = 24
	}
	m, err := c.h.Store.RunMetrics(c.r.Context(), c.p.TenantID, time.Duration(hours)*time.Hour)
	if err != nil {
		c.runError(err)
		return
	}
	writeJSON(c.w, http.StatusOK, m)
}

func (c call) integrations() {
	if !c.method(http.MethodGet) || !c.allow("metrics.read") {
		return
	}
	depth, err := c.h.Store.QueueDepth(c.r.Context(), c.p.TenantID)
	if err != nil {
		c.runError(err)
		return
	}
	writeJSON(c.w, http.StatusOK, map[string]any{"configuration": c.h.Status, "queue_depth": depth})
}

func (c call) audit() {
	if !c.method(http.MethodGet) || !c.allow("audit.read") {
		return
	}
	items, total, err := c.h.Store.ListAuditEvents(c.r.Context(), c.p.TenantID, c.queryInt("limit", 50), c.queryInt("offset", 0))
	if err != nil {
		c.runError(err)
		return
	}
	writeJSON(c.w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func isJSONArray(raw json.RawMessage) bool {
	var v []json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &v) == nil && v != nil
}

// RateLimiter is a per-tenant fixed-window limiter for run creation.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	counts map[string]*windowCount
	now    func() time.Time
}

type windowCount struct {
	start time.Time
	n     int
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, counts: map[string]*windowCount{}, now: time.Now}
}

func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c := l.counts[key]
	if c == nil || now.Sub(c.start) >= l.window {
		l.counts[key] = &windowCount{start: now, n: 1}
		return true
	}
	if c.n >= l.limit {
		return false
	}
	c.n++
	return true
}
