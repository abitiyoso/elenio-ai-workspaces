package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

func (s *server) registerCaseRoutes(mux *http.ServeMux) {
	route := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.authenticate(h)) }
	route("GET /api/v1/runtime-capabilities", s.runtimeCapabilities)
	route("GET /api/v1/cases", s.listCases)
	route("POST /api/v1/cases", s.createCase)
	route("GET /api/v1/cases/{id}", s.getCase)
	route("POST /api/v1/cases/{id}/pause", s.pauseCase(true))
	route("POST /api/v1/cases/{id}/resume", s.pauseCase(false))
	route("POST /api/v1/cases/{id}/events", s.caseEvent)
	route("GET /api/v1/tasks", s.listTasks)
	route("GET /api/v1/tasks/{id}", s.getTask)
	route("POST /api/v1/tasks/{id}/complete", s.completeTask)
	route("POST /api/v1/tasks/{id}/revision", s.requestRevision)
	route("POST /api/v1/tasks/{id}/comments", s.addComment)
	route("GET /api/v1/approvals", s.listApprovals)
	route("GET /api/v1/approvals/{id}", s.getApproval)
	route("POST /api/v1/approvals/{id}/decision", s.decide)
	route("GET /api/v1/drafts", s.listDrafts)
	route("GET /api/v1/drafts/{id}", s.getDraft)
	route("POST /api/v1/drafts/{id}/submit", s.submitDraft)
	route("POST /api/v1/drafts/{id}/revision", s.reviseDraft)
	route("GET /api/v1/cases/{id}/artifacts", s.listArtifacts)
	route("POST /api/v1/cases/{id}/artifacts/{artifact}/versions", s.reviseArtifact)
	// Agent runtimes authenticate by signature (HMAC, timestamp, audience), not by a user session.
	mux.HandleFunc("POST /api/v1/agent-callbacks/{provider}", s.agentCallback)
}

func (s *server) agentCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "Callback body exceeds 1 MiB.", nil)
		return
	}
	outcome, err := s.deps.Cases.HandleAgentCallback(r.Context(), r.PathValue("provider"),
		agent.CallbackEnvelope{Provider: r.PathValue("provider"), Headers: r.Header, Body: body})
	switch {
	case errors.Is(err, cases.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Unknown agent provider.", nil)
	case errors.Is(err, cases.ErrInvalidCallback):
		s.deps.Logger.WarnContext(r.Context(), "agent callback rejected", "provider", r.PathValue("provider"), "error", err.Error())
		writeError(w, r, http.StatusUnauthorized, "invalid_callback", "Callback failed verification.", nil)
	case err != nil:
		s.casesError(w, r, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"outcome": string(outcome)})
	}
}

func (s *server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Cases.ListArtifacts(r.Context(), identity(r), id)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) reviseArtifact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	artifactID := r.PathValue("artifact")
	if !uuidPath(artifactID) {
		writeError(w, r, http.StatusNotFound, "not_found", "Not found.", nil)
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in struct {
		Content    json.RawMessage `json:"content"`
		ChangeNote string          `json:"change_note"`
	}
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if err := s.deps.Cases.ReviseArtifact(r.Context(), identity(r), id, artifactID, expected, in.Content, in.ChangeNote); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeCase(w, r, http.StatusCreated, id)
}

var uuidPath = func(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4 && strings.Trim(s, "0123456789abcdef-") == ""
}

// pathID returns the {id} path value or writes 404 (malformed ids cannot exist).
func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPath(id) {
		writeError(w, r, http.StatusNotFound, "not_found", "Not found.", nil)
		return "", false
	}
	return id, true
}

func (s *server) casesError(w http.ResponseWriter, r *http.Request, err error) {
	var denied *cases.DeniedError
	var conflict *cases.ConflictError
	var input *cases.InputError
	var verr *cases.ValidationError
	switch {
	case errors.Is(err, cases.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "Your role does not allow this action.", nil)
	case errors.As(err, &denied):
		writeError(w, r, http.StatusForbidden, denied.Reason, denied.Message, nil)
	case errors.Is(err, cases.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Not found.", nil)
	case errors.Is(err, cases.ErrPreconditionFailed):
		writeError(w, r, http.StatusPreconditionFailed, "version_mismatch", "This item changed since you loaded it. Reload and try again.", nil)
	case errors.Is(err, cases.ErrIdempotencyReuse):
		writeError(w, r, http.StatusUnprocessableEntity, "idempotency_key_reused", "This Idempotency-Key was already used for a different request.", nil)
	case errors.As(err, &conflict):
		writeError(w, r, http.StatusConflict, conflict.Code, conflict.Message, nil)
	case errors.As(err, &input):
		writeError(w, r, http.StatusBadRequest, input.Code, input.Message, []ErrorDetail{{Code: input.Code, Message: input.Message, Path: input.Path}})
	case errors.As(err, &verr):
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "The request does not satisfy the workflow's schema or runtime.", diagDetails(verr.Diagnostics))
	default:
		s.deps.Logger.ErrorContext(r.Context(), "case runtime error", "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, "internal", "Unexpected server error.", nil)
	}
}

func diagDetails(diags []definition.Diagnostic) []ErrorDetail {
	out := make([]ErrorDetail, 0, len(diags))
	for _, d := range diags {
		out = append(out, ErrorDetail{Code: d.Code, Message: d.Message, Path: d.Path})
	}
	return out
}

func (s *server) runtimeCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"supported_node_types": cases.SupportedNodeTypes})
}

func (s *server) listCases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, next, err := s.deps.Cases.ListCases(r.Context(), identity(r), cases.ListFilter{
		Status: q.Get("status"), Query: q.Get("q"), Cursor: q.Get("cursor"), Limit: intQuery(r, "limit", 50)})
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": cursor})
}

func (s *server) writeCase(w http.ResponseWriter, r *http.Request, status int, id string) {
	d, err := s.deps.Cases.GetCase(r.Context(), identity(r), id)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(d.VersionNum))
	writeJSON(w, status, d)
}

func (s *server) createCase(w http.ResponseWriter, r *http.Request) {
	var in cases.CreateInput
	body, ok := decodeBody(w, r, &in)
	if !ok {
		return
	}
	id, err := s.deps.Cases.CreateCase(r.Context(), identity(r), cases.Idempotency(idempotency(r, body)), in)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/cases/"+id)
	s.writeCase(w, r, http.StatusCreated, id)
}

func (s *server) getCase(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		s.writeCase(w, r, http.StatusOK, id)
	}
}

func (s *server) pauseCase(pause bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		expected, ok := ifMatch(w, r)
		if !ok {
			return
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if _, ok := decodeBody(w, r, &in); !ok {
			return
		}
		if err := s.deps.Cases.SetPaused(r.Context(), identity(r), id, expected, pause, in.Reason); err != nil {
			s.casesError(w, r, err)
			return
		}
		s.writeCase(w, r, http.StatusOK, id)
	}
}

func (s *server) caseEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Type string `json:"type"`
		Body string `json:"body"`
	}
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if in.Type != "note" {
		writeError(w, r, http.StatusBadRequest, "unsupported_event", "Only \"note\" events are accepted in this phase.", []ErrorDetail{{Code: "unsupported_event", Message: "type must be note", Path: "type"}})
		return
	}
	if err := s.deps.Cases.AddNote(r.Context(), identity(r), id, in.Body); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeCase(w, r, http.StatusCreated, id)
}

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	items, err := s.deps.Cases.ListTasks(r.Context(), identity(r), status)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) writeTask(w http.ResponseWriter, r *http.Request, id string) {
	t, err := s.deps.Cases.GetTask(r.Context(), identity(r), id)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(t.VersionNum))
	writeJSON(w, http.StatusOK, t)
}

func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		s.writeTask(w, r, id)
	}
}

func (s *server) completeTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in cases.CompleteTaskInput
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if err := s.deps.Cases.CompleteTask(r.Context(), identity(r), id, expected, in); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeTask(w, r, id)
}

func (s *server) requestRevision(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in struct {
		Rationale string `json:"rationale"`
	}
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if err := s.deps.Cases.RequestRevision(r.Context(), identity(r), id, expected, in.Rationale); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeTask(w, r, id)
}

func (s *server) addComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if err := s.deps.Cases.AddComment(r.Context(), identity(r), id, in.Body); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeTask(w, r, id)
}

func (s *server) listApprovals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	items, err := s.deps.Cases.ListApprovals(r.Context(), identity(r), status)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) writeApproval(w http.ResponseWriter, r *http.Request, id string) {
	a, err := s.deps.Cases.GetApproval(r.Context(), identity(r), id)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(a.VersionNum))
	writeJSON(w, http.StatusOK, a)
}

func (s *server) getApproval(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		s.writeApproval(w, r, id)
	}
}

func (s *server) decide(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in cases.DecisionInput
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if err := s.deps.Cases.Decide(r.Context(), identity(r), id, expected, in); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeApproval(w, r, id)
}

func (s *server) listDrafts(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Cases.ListDrafts(r.Context(), identity(r), r.URL.Query().Get("status"))
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) writeDraft(w http.ResponseWriter, r *http.Request, id string) {
	d, err := s.deps.Cases.GetDraft(r.Context(), identity(r), id)
	if err != nil {
		s.casesError(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(d.VersionNum))
	writeJSON(w, http.StatusOK, d)
}

func (s *server) getDraft(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		s.writeDraft(w, r, id)
	}
}

func (s *server) submitDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if err := s.deps.Cases.SubmitDraft(r.Context(), identity(r), id, expected, in.Note); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeDraft(w, r, id)
}

func (s *server) reviseDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in struct {
		Feedback string `json:"feedback"`
	}
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if err := s.deps.Cases.ReviseDraft(r.Context(), identity(r), id, expected, in.Feedback); err != nil {
		s.casesError(w, r, err)
		return
	}
	s.writeDraft(w, r, id)
}
