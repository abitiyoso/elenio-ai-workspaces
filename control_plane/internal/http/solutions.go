package http

import (
	"net/http"
	"strconv"

	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/solutions"
)

func (s *Server) solutionService() *solutions.Service {
	return &solutions.Service{Pool: s.pool, Engine: s.engine}
}
func (s *Server) handleSolutions(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	out, err := s.solutionService().List(r.Context(), p, r.URL.Query().Get("cursor"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleSolution(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	out, err := s.solutionService().Get(r.Context(), p, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(out.Revision)))
	writeJSON(w, 200, out)
}
func (s *Server) handleCreateSolution(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	var in solutions.Definition
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, created, err := s.solutionService().Create(r.Context(), p, in, r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(out.Revision)))
	writeJSON(w, status, out)
}
func (s *Server) handleUpdateSolution(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	quoted := r.Header.Get("If-Match")
	value, err := strconv.Unquote(quoted)
	revision, parseErr := strconv.Atoi(value)
	if err != nil || parseErr != nil || revision < 1 || strconv.Quote(strconv.Itoa(revision)) != quoted {
		writeError(w, r, 428, "PRECONDITION_REQUIRED", "Supply the current strong revision ETag in If-Match")
		return
	}
	var in solutions.Definition
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.solutionService().Update(r.Context(), p, r.PathValue("id"), revision, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(out.Revision)))
	writeJSON(w, 200, out)
}
