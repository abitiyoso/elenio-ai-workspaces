package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/elenio/control-plane/internal/identity/principal"
)

func solutionRevision(w http.ResponseWriter, r *http.Request) (int, bool) {
	quoted := r.Header.Get("If-Match")
	value, err := strconv.Unquote(quoted)
	revision, parseErr := strconv.Atoi(value)
	if err != nil || parseErr != nil || revision < 1 || strconv.Quote(strconv.Itoa(revision)) != quoted {
		writeError(w, r, 428, "PRECONDITION_REQUIRED", "Supply the current strong revision ETag in If-Match")
		return 0, false
	}
	return revision, true
}
func (s *Server) handleCreateCandidate(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	revision, ok := solutionRevision(w, r)
	if !ok {
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := decode(r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	service := s.solutionService()
	id := r.PathValue("id")
	requestKey := r.Header.Get("Idempotency-Key")
	replay, found, err := service.ReplayCandidate(ctx, p, id, revision, input.Version, requestKey)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if found {
		writeJSON(w, 200, replay)
		return
	}
	report, err := s.collectSolutionReadiness(ctx, p, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	evidence, err := json.Marshal(report)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	item, created, err := service.CreateCandidate(ctx, p, id, revision, input.Version, requestKey, evidence)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status := 200
	if created {
		status = 201
	}
	writeJSON(w, status, item)
}
func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	page, err := s.solutionService().ListCandidates(r.Context(), p, r.PathValue("id"), r.URL.Query().Get("cursor"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, page)
}
func (s *Server) handleCandidate(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	item, err := s.solutionService().GetCandidate(r.Context(), p, r.PathValue("id"), r.PathValue("candidate_id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(item.ContentHash))
	writeJSON(w, 200, item)
}
