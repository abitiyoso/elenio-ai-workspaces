// Package httpapi is the BFF transport: /api/v1/workspace/* (every request authenticated with a Control
// Plane token, aud=case-workspace), /healthz, /readyz and the SPA. Identity is read only from the
// verified token in the request context. No CORS headers are ever sent: the browser reaches the BFF
// same-origin through the Control Plane proxy.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/elenio/case-workspace/internal/apierr"
	"github.com/elenio/case-workspace/internal/auth"
	"github.com/elenio/case-workspace/internal/intake"
	"github.com/elenio/case-workspace/internal/reqctx"
	"github.com/elenio/case-workspace/internal/workspace"
)

const (
	requestIDHeader = "X-Request-ID"
	maxBody         = 256 << 10
)

// Check is a named readiness probe; its error text is logged, never returned.
type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

type Deps struct {
	Log       *slog.Logger
	Verifier  *auth.Verifier
	Workspace *workspace.Service
	Intake    *intake.Service
	Readiness []Check
	Web       http.Handler // nil: API only
}

type server struct{ d Deps }

func New(d Deps) http.Handler {
	s := &server{d: d}
	api := http.NewServeMux()
	const p = "/api/v1/workspace"
	api.HandleFunc("GET "+p+"/me/context", s.meContext)
	api.HandleFunc("GET "+p+"/packs", s.listPacks)
	api.HandleFunc("GET "+p+"/packs/{key}/versions/{version}", s.getPack)
	api.HandleFunc("GET "+p+"/cases", s.listCases)
	api.HandleFunc("GET "+p+"/cases/{id}", s.getCase)
	api.HandleFunc("POST "+p+"/cases/{id}/drafts/{draft}/submit", s.reviewDraft(true))
	api.HandleFunc("POST "+p+"/cases/{id}/drafts/{draft}/revision", s.reviewDraft(false))
	api.HandleFunc("POST "+p+"/intake-sessions", s.createSession)
	api.HandleFunc("GET "+p+"/intake-sessions/{id}", s.getSession)
	api.HandleFunc("POST "+p+"/intake-sessions/{id}/messages", s.sendMessage)
	api.HandleFunc("POST "+p+"/intake-sessions/{id}/documents", s.uploadDocument)
	api.HandleFunc("DELETE "+p+"/intake-sessions/{id}/documents/{doc}", s.removeDocument)
	api.HandleFunc("PUT "+p+"/intake-sessions/{id}/draft", s.updateDraft)
	api.HandleFunc("POST "+p+"/intake-sessions/{id}/case", s.createCase)
	api.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "route_not_found", "No such API route.", nil)
	})

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.HandleFunc("GET /readyz", s.readyz)
	// Every /api/ path (known or not, any method) is authenticated first so anonymous callers cannot probe routes.
	root.Handle("/api/", apiHeaders(s.authenticate(api)))
	if d.Web != nil {
		root.Handle("/", d.Web)
	} else {
		root.Handle("/", apiHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusNotFound, "route_not_found", "No such route.", nil)
		})))
	}
	return requestIDs(s.logRequests(s.recoverer(root)))
}

// --- middleware -------------------------------------------------------------------------------------

func requestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := reqctx.Accept(r.Header.Get(requestIDHeader))
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(reqctx.With(r.Context(), id)))
	})
}

func apiHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

// logRequests logs method, route pattern, status and duration; never headers, query strings or bodies.
func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}
		s.d.Log.InfoContext(r.Context(), "http request", "method", r.Method, "route", route, "status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(), "request_id", reqctx.ID(r.Context()))
	})
}

func (s *server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.d.Log.ErrorContext(r.Context(), "panic", "request_id", reqctx.ID(r.Context()), "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				writeError(w, r, http.StatusInternalServerError, "internal", "Unexpected server error.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.d.Verifier.VerifyRequest(r)
		if err != nil {
			if errors.Is(err, auth.ErrKeysUnavailable) {
				s.d.Log.ErrorContext(r.Context(), "authentication unavailable", "error", err.Error(), "request_id", reqctx.ID(r.Context()))
				writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.", nil)
				return
			}
			s.d.Log.InfoContext(r.Context(), "authentication rejected", "reason", err.Error(), "request_id", reqctx.ID(r.Context()))
			w.Header().Set("WWW-Authenticate", `Bearer realm="case-workspace"`)
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A verified Control Plane session is required.", nil)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
	})
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks, ready := map[string]string{}, true
	for _, c := range s.d.Readiness {
		if err := c.Run(ctx); err != nil {
			ready = false
			checks[c.Name] = "unavailable"
			s.d.Log.WarnContext(ctx, "readiness check failed", "check", c.Name, "error", err.Error())
			continue
		}
		checks[c.Name] = "ok"
	}
	status, label := http.StatusOK, "ready"
	if !ready {
		status, label = http.StatusServiceUnavailable, "not_ready"
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{"status": label, "checks": checks})
}

// --- helpers ----------------------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	TraceID string          `json:"trace_id"`
	Details []apierr.Detail `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string, details []apierr.Detail) {
	writeJSON(w, status, map[string]errBody{"error": {Code: code, Message: msg, TraceID: reqctx.ID(r.Context()), Details: details}})
}

func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apierr.Error
	if errors.As(err, &ae) {
		if ae.Status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="case-workspace"`)
		}
		writeError(w, r, ae.Status, ae.Code, ae.Message, ae.Details)
		return
	}
	s.d.Log.ErrorContext(r.Context(), "request failed", "error", err.Error(), "request_id", reqctx.ID(r.Context()))
	writeError(w, r, http.StatusInternalServerError, "internal", "Unexpected server error.", nil)
}

func identity(r *http.Request) auth.Identity {
	id, _ := auth.FromContext(r.Context())
	return id
}

// decode reads a bounded JSON body strictly (unknown fields rejected). An empty body leaves dst as is.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" || r.ContentLength > 0 {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || mt != "application/json" {
			writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Request bodies must be application/json.", nil)
			return false
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("Request body exceeds %d KiB.", maxBody>>10), nil)
		return false
	}
	if strings.TrimSpace(string(body)) == "" {
		return true
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Request body is not valid for this endpoint.", nil)
		return false
	}
	return true
}

var etagPattern = regexp.MustCompile(`^"([0-9]{1,9})"$`)

func ifMatch(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		writeError(w, r, http.StatusPreconditionRequired, "precondition_required", "Send If-Match with the ETag of the version you edited.", nil)
		return 0, false
	}
	m := etagPattern.FindStringSubmatch(raw)
	if m == nil {
		writeError(w, r, http.StatusBadRequest, "invalid_if_match", `If-Match must be a strong ETag such as "3".`, nil)
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

func etag(n int) string { return `"` + strconv.Itoa(n) + `"` }

// --- handlers ---------------------------------------------------------------------------------------

func (s *server) meContext(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.d.Workspace.MeContext(r.Context(), identity(r)))
}

func (s *server) listPacks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.d.Workspace.ListPacks(r.Context(), identity(r)))
}

func (s *server) getPack(w http.ResponseWriter, r *http.Request) {
	d, err := s.d.Workspace.GetPack(r.Context(), identity(r), r.PathValue("key"), r.PathValue("version"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) listCases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.d.Workspace.ListCases(r.Context(), identity(r), q.Get("q"), q.Get("status"), q.Get("cursor"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) writeCase(w http.ResponseWriter, c *workspace.CaseContext) {
	w.Header().Set("ETag", etag(c.VersionNum))
	writeJSON(w, http.StatusOK, c)
}

func (s *server) getCase(w http.ResponseWriter, r *http.Request) {
	c, err := s.d.Workspace.GetCase(r.Context(), identity(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeCase(w, c)
}

// reviewDraft forwards submit/revision. An Idempotency-Key header is accepted and ignored: Flowcase
// draft decisions are made idempotent by If-Match on the review's version_num.
func (s *server) reviewDraft(submit bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected, ok := ifMatch(w, r)
		if !ok {
			return
		}
		var in struct {
			Note     string `json:"note"`
			Feedback string `json:"feedback"`
		}
		if !decode(w, r, &in) {
			return
		}
		text := in.Feedback
		if submit {
			if in.Feedback != "" {
				writeError(w, r, http.StatusBadRequest, "invalid_json", "submit takes {\"note\"}.", nil)
				return
			}
			if len([]rune(in.Note)) > 1000 {
				writeError(w, r, http.StatusUnprocessableEntity, "invalid_note", "note may have at most 1000 characters", []apierr.Detail{{Code: "invalid_note", Message: "too long", Path: "note"}})
				return
			}
			text = in.Note
		} else if in.Note != "" {
			writeError(w, r, http.StatusBadRequest, "invalid_json", "revision takes {\"feedback\"}.", nil)
			return
		}
		c, err := s.d.Workspace.ReviewDraft(r.Context(), identity(r), r.PathValue("id"), r.PathValue("draft"), expected, submit, text)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.writeCase(w, c)
	}
}

func writeSession(w http.ResponseWriter, status int, ses *intake.Session) {
	w.Header().Set("ETag", etag(ses.VersionNum))
	writeJSON(w, status, ses)
}

func (s *server) createSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PackKey string `json:"pack_key"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.PackKey) == "" {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "pack_key is required.", []apierr.Detail{{Code: "required", Message: "pack_key is required", Path: "pack_key"}})
		return
	}
	ses, err := s.d.Intake.Create(r.Context(), identity(r), in.PackKey)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/workspace/intake-sessions/"+ses.ID)
	writeSession(w, http.StatusCreated, ses)
}

func (s *server) getSession(w http.ResponseWriter, r *http.Request) {
	ses, err := s.d.Intake.Get(r.Context(), identity(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeSession(w, http.StatusOK, ses)
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &in) {
		return
	}
	ses, err := s.d.Intake.Send(r.Context(), identity(r), r.PathValue("id"), in.Content)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeSession(w, http.StatusAccepted, ses)
}

// uploadDocument reads one multipart part named "file" (at most intake.MaxDocumentBytes).
func (s *server) uploadDocument(w http.ResponseWriter, r *http.Request) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Upload the document as multipart/form-data with a file field.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, intake.MaxDocumentBytes+64<<10)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_upload", "The upload could not be read.", nil)
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, r, http.StatusRequestEntityTooLarge, "document_too_large", fmt.Sprintf("Documents may be at most %d MB.", intake.MaxDocumentBytes>>20), nil)
				return
			}
			writeError(w, r, http.StatusBadRequest, "invalid_upload", "Send the document in a form field named file.", nil)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		content, err := io.ReadAll(io.LimitReader(part, intake.MaxDocumentBytes+1))
		_ = part.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, r, http.StatusRequestEntityTooLarge, "document_too_large", fmt.Sprintf("Documents may be at most %d MB.", intake.MaxDocumentBytes>>20), nil)
				return
			}
			writeError(w, r, http.StatusBadRequest, "invalid_upload", "The upload could not be read.", nil)
			return
		}
		ses, err := s.d.Intake.Upload(r.Context(), identity(r), r.PathValue("id"), part.FileName(), content)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeSession(w, http.StatusAccepted, ses)
		return
	}
}

func (s *server) removeDocument(w http.ResponseWriter, r *http.Request) {
	ses, err := s.d.Intake.RemoveDocument(r.Context(), identity(r), r.PathValue("id"), r.PathValue("doc"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeSession(w, http.StatusOK, ses)
}

func (s *server) updateDraft(w http.ResponseWriter, r *http.Request) {
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in struct {
		Draft map[string]any `json:"draft"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Draft == nil {
		in.Draft = map[string]any{}
	}
	ses, err := s.d.Intake.UpdateDraft(r.Context(), identity(r), r.PathValue("id"), expected, in.Draft)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeSession(w, http.StatusOK, ses)
}

func (s *server) createCase(w http.ResponseWriter, r *http.Request) {
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in struct{}
	if !decode(w, r, &in) {
		return
	}
	created, err := s.d.Intake.CreateCase(r.Context(), identity(r), r.PathValue("id"), expected, r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}
