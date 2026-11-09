// Package http is the transport layer: routing, middleware (correlation, security headers, rate limits,
// session resolution, CSRF/origin checks), JSON handlers, the authorised mini-app route and the SPA.
package http

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
	Issues  any    `json:"issues,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	writeJSON(w, status, apiError{Code: code, Message: msg, TraceID: observability.RequestIDFrom(r.Context())})
}

// fail maps service errors to structured responses; unexpected errors are logged and hidden.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	if e, ok := apperr.As(err); ok {
		writeJSON(w, e.Status, apiError{Code: e.Code, Message: e.Message, TraceID: observability.RequestIDFrom(r.Context()), Issues: e.Issues})
		return
	}
	s.log.Error("request_failed", "request_id", observability.RequestIDFrom(r.Context()), "path", r.URL.Path, "error", err.Error())
	writeError(w, r, http.StatusInternalServerError, "INTERNAL", "unexpected error")
}

// decode reads a strict JSON body (application/json, size-limited, unknown fields rejected).
func decode(r *http.Request, v any) error {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/json" {
		return apperr.New(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 256<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperr.BadRequest("INVALID_BODY", "request body is not valid JSON for this endpoint")
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.BadRequest("INVALID_BODY", "request body must contain one JSON object")
	}
	return nil
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · AI Workspace</title>
<style>
:root{--navy:#101B33;--cobalt:#2864EC;--canvas:#F4F6FB;--ink:#1B2B43;--muted:#5F6F87;--edge:#E4EAF4}
body{margin:0;font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;background:var(--canvas);color:var(--ink);display:grid;place-items:center;min-height:100vh;padding:16px;box-sizing:border-box}
main{background:#fff;border:1px solid var(--edge);border-radius:16px;max-width:520px;width:100%;padding:28px;box-shadow:0 10px 30px rgba(16,27,51,.08)}
h1{font-size:20px;margin:0 0 8px;color:var(--navy)} p{margin:8px 0;color:var(--muted)} code{background:var(--canvas);padding:2px 6px;border-radius:6px;color:var(--ink)}
a.btn{display:inline-block;margin-top:14px;background:var(--cobalt);color:#fff;text-decoration:none;padding:9px 16px;border-radius:10px;font-weight:600}
</style></head><body><main><h1>{{.Title}}</h1>{{range .Lines}}<p>{{.}}</p>{{end}}
{{if .Code}}<p>Reason: <code>{{.Code}}</code> · Trace: <code>{{.Trace}}</code></p>{{end}}
<a class="btn" href="{{.Href}}">{{.Action}}</a></main></body></html>`))

type page struct {
	Title, Code, Trace, Href, Action string
	Lines                            []string
}

func writePage(w http.ResponseWriter, r *http.Request, status int, p page) {
	p.Trace = observability.RequestIDFrom(r.Context())
	if p.Href == "" {
		p.Href, p.Action = "/", "Back to workspace"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := pageTmpl.Execute(w, p); err != nil {
		slog.Default().Error("page_render_failed", "error", err.Error())
	}
}
