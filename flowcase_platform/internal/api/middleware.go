package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/metrics"
	"github.com/elenio/flowcase/internal/platform/observability"
)

const traceHeader = "X-Request-ID"

// ErrorDetail is one structured validation failure (PRD §10 mandatory request rules).
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type errorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	TraceID string        `json:"trace_id"`
	Details []ErrorDetail `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details []ErrorDetail) {
	writeJSON(w, status, map[string]errorBody{"error": {Code: code, Message: message, TraceID: observability.TraceID(r.Context()), Details: details}})
}

func traceIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := observability.AcceptTraceID(r.Header.Get(traceHeader))
		w.Header().Set(traceHeader, id)
		next.ServeHTTP(w, r.WithContext(observability.WithTraceID(r.Context(), id)))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func recoverer(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.ErrorContext(r.Context(), "panic", "trace_id", observability.TraceID(r.Context()), "panic", v, "stack", string(debug.Stack()))
				writeError(w, r, http.StatusInternalServerError, "internal", "Unexpected server error.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

// requestLogger records route pattern, never raw query strings, headers or bodies.
func requestLogger(logger *slog.Logger, m *metrics.Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		elapsed := time.Since(started)
		logger.InfoContext(r.Context(), "http request",
			"method", r.Method, "route", route, "status", sw.status,
			"duration_ms", elapsed.Milliseconds(), "trace_id", observability.TraceID(r.Context()))
		code := fmt.Sprintf("%dxx", sw.status/100)
		m.Add("flowcase_http_requests_total", "HTTP requests by route pattern and status class.", metrics.Labels{"route": route, "code": code}, 1)
		m.Observe("flowcase_http_request_duration_seconds", "HTTP request latency by route pattern.", metrics.HTTPBuckets, metrics.Labels{"route": route}, elapsed.Seconds())
	})
}

func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.deps.Auth.Authenticate(r.Context(), r)
		if err != nil {
			level, status, code, msg := slog.LevelInfo, http.StatusUnauthorized, "unauthenticated", "A verified host session is required."
			if errors.Is(err, auth.ErrMisconfigured) || !errors.Is(err, auth.ErrUnauthenticated) {
				level, status, code, msg = slog.LevelError, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable."
			}
			s.deps.Logger.Log(r.Context(), level, "authentication rejected", "reason", auth.Reason(err), "trace_id", observability.TraceID(r.Context()))
			if status == http.StatusUnauthorized {
				w.Header().Set("WWW-Authenticate", `Bearer realm="flowcase"`)
			}
			writeError(w, r, status, code, msg, nil)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
	})
}
