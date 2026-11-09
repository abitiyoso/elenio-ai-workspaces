// Package apierr is the client-facing error of the BFF: {"error": {code, message, trace_id, details}}.
package apierr

import (
	"errors"
	"net/http"

	"github.com/elenio/case-workspace/internal/controlplane"
	"github.com/elenio/case-workspace/internal/flowcase"
)

type Detail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type Error struct {
	Status  int
	Code    string
	Message string
	Details []Detail
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

var (
	SessionInactive     = New(http.StatusUnauthorized, "session_inactive", "Your Control Plane session is no longer active. Sign in again.")
	FlowcaseUnavailable = New(http.StatusServiceUnavailable, "flowcase_unavailable", "Flowcase is temporarily unavailable.")
	FlowcaseDenied      = New(http.StatusForbidden, "flowcase_access_denied", "Flowcase did not accept your identity. Ask an administrator to provision you in Flowcase.")
	NotFound            = New(http.StatusNotFound, "not_found", "Not found.")
)

// Upstream maps token-exchange and Flowcase errors to client errors; it returns nil for anything else.
// Flowcase 4xx answers pass through with their status, code, message and details.
func Upstream(err error) *Error {
	var fe *flowcase.Error
	var ae *Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ae):
		return ae
	case errors.Is(err, controlplane.ErrSessionInactive):
		return SessionInactive
	case errors.Is(err, controlplane.ErrUnavailable), errors.Is(err, flowcase.ErrUnavailable):
		return FlowcaseUnavailable
	case errors.As(err, &fe):
		if fe.Status == http.StatusUnauthorized {
			return FlowcaseDenied
		}
		out := &Error{Status: fe.Status, Code: fe.Code, Message: fe.Message}
		for _, d := range fe.Details {
			out.Details = append(out.Details, Detail{Code: d.Code, Message: d.Message, Path: d.Path})
		}
		return out
	}
	return nil
}
