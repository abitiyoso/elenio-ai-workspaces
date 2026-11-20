package http

import (
	"net/http"
	"strconv"

	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/platform/apperr"
)

func (s *Server) handleSolutionRevisions(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	before := 0
	if value := r.URL.Query().Get("before"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || strconv.Itoa(parsed) != value {
			s.fail(w, r, apperr.BadRequest("INVALID_CURSOR", "Use a positive revision cursor"))
			return
		}
		before = parsed
	}
	page, err := s.solutionService().ListRevisions(r.Context(), p, r.PathValue("id"), before)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, page)
}
func (s *Server) handleSolutionRevision(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	value := r.PathValue("revision")
	revision, err := strconv.Atoi(value)
	if err != nil || revision < 1 || strconv.Itoa(revision) != value {
		s.fail(w, r, apperr.BadRequest("INVALID_REVISION", "Use a positive saved revision"))
		return
	}
	item, err := s.solutionService().GetRevision(r.Context(), p, r.PathValue("id"), revision)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(item.ContentHash))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, item)
}
