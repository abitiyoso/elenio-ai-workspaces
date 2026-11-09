package httpapi

import (
	"context"
	"net/http"

	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
	"github.com/elenio/agent-studio/internal/simpack"
)

type SIMPackStore interface {
	ApplySIMPack(context.Context, string, string, simpack.Pack) (postgres.SIMPackResult, error)
}

// SIMPackHandler applies the embedded SIM domain pack to unconfigured drafts.
type SIMPackHandler struct {
	Store      SIMPackStore
	Auth       ports.AuthAdapter
	Authorizer ports.Authorizer
}

func (h SIMPackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, err := h.Auth.VerifyRequest(r.Context(), r)
	if err != nil || principal.TenantID == "" || principal.Kind == "service" {
		writeError(w, http.StatusUnauthorized, "verified tenant identity is required")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := h.Authorizer.Authorize(r.Context(), principal, "agent.bootstrap", "tenant:"+principal.TenantID); err != nil {
		writeError(w, http.StatusForbidden, "action is not authorized")
		return
	}
	pack, err := simpack.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SIM pack is unavailable")
		return
	}
	result, err := h.Store.ApplySIMPack(r.Context(), principal.TenantID, principal.Subject, pack)
	if err != nil {
		writeError(w, http.StatusConflict, "SIM pack could not be applied")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
