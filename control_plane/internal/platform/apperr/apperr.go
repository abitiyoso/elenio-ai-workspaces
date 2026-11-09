// Package apperr carries a stable machine code and HTTP status from services to the transport layer.
package apperr

import (
	"errors"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Issues  any // optional structured details (e.g. manifest validation issues)
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func Forbidden(code, message string) *Error  { return New(http.StatusForbidden, code, message) }
func NotFound(message string) *Error         { return New(http.StatusNotFound, "NOT_FOUND", message) }
func Invalid(code, message string) *Error    { return New(http.StatusUnprocessableEntity, code, message) }
func Conflict(code, message string) *Error   { return New(http.StatusConflict, code, message) }
func BadRequest(code, message string) *Error { return New(http.StatusBadRequest, code, message) }
func Unauthenticated(message string) *Error {
	return New(http.StatusUnauthorized, "UNAUTHENTICATED", message)
}
func Unavailable(code, message string) *Error {
	return New(http.StatusServiceUnavailable, code, message)
}

func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
