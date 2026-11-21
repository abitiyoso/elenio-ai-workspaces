package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/elenio/flowcase/internal/audit"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/registry"
)

const maxBodyBytes = 512 << 10

// Registry wires the workflow registry (Phase 1). All handlers read identity from the context only.
type Registry struct {
	Service   *registry.Service
	Catalog   catalog.Catalog
	Templates []packs.Template
}

func (s *server) registerWorkflowRoutes(mux *http.ServeMux) {
	route := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.authenticate(h)) }
	route("GET /api/v1/workflows", s.listWorkflows)
	route("POST /api/v1/workflows", s.createWorkflow)
	route("GET /api/v1/workflows/{key}", s.getWorkflow)
	route("POST /api/v1/workflows/{key}/duplicate", s.duplicateWorkflow)
	route("GET /api/v1/workflows/{key}/versions", s.listVersions)
	route("POST /api/v1/workflows/{key}/versions", s.createVersion)
	route("GET /api/v1/workflows/{key}/versions/{version}", s.getVersion)
	route("GET /api/v1/workflows/{key}/versions/{version}/dependencies", s.getVersionDependencies)
	route("PATCH /api/v1/workflows/{key}/versions/{version}", s.updateVersion)
	route("POST /api/v1/workflows/{key}/versions/{version}/validate", s.validateVersion)
	route("POST /api/v1/workflows/{key}/versions/{version}/publish", s.publishVersion)
	route("POST /api/v1/workflows/{key}/versions/{version}/deprecate", s.deprecateVersion)
	route("POST /api/v1/workflow-validations", s.validateDocument)
	route("GET /api/v1/workflow-templates", s.listTemplates)
	route("GET /api/v1/workflow-catalog", s.getCatalog)
	route("GET /api/v1/audit-events", s.listAuditEvents)
}

func identity(r *http.Request) auth.Identity {
	id, _ := auth.FromContext(r.Context())
	return id
}

// --- request helpers ----------------------------------------------------------------------------

var etagPattern = regexp.MustCompile(`^"([0-9]{1,9})"$`)

func etag(versionNum int) string { return fmt.Sprintf(`"%d"`, versionNum) }

// ifMatch requires a strong ETag naming the version_num the client edited.
func ifMatch(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		writeError(w, r, http.StatusPreconditionRequired, "precondition_required", "Send If-Match with the ETag of the version you edited.", nil)
		return 0, false
	}
	m := etagPattern.FindStringSubmatch(raw)
	if m == nil {
		writeError(w, r, http.StatusBadRequest, "invalid_if_match", "If-Match must be a strong ETag such as \"3\".", nil)
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.ContentLength != 0 || r.Header.Get("Content-Type") != "" {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Request bodies must be application/json.", nil)
			return nil, false
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("Request body exceeds %d KiB.", maxBodyBytes>>10), nil)
		return nil, false
	}
	return body, true
}

// decodeBody strictly decodes a command body; an empty body decodes to the zero value.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) ([]byte, bool) {
	body, ok := readBody(w, r)
	if !ok {
		return nil, false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return body, true
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Request body is not valid for this endpoint: "+err.Error(), nil)
		return nil, false
	}
	return body, true
}

func idempotency(r *http.Request, body []byte) registry.Idempotency {
	return registry.Idempotency{Key: r.Header.Get("Idempotency-Key"), Hash: registry.RequestHash([]byte(r.Method), []byte(r.URL.Path), body)}
}

func versionParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	v, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || v < 1 {
		writeError(w, r, http.StatusBadRequest, "invalid_version", "Version must be a positive integer.", nil)
		return 0, false
	}
	return v, true
}

func intQuery(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil {
		return v
	}
	return def
}

// registryError maps service errors to the documented HTTP contract.
func (s *server) registryError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *registry.ValidationError
	var conflict *registry.ConflictError
	var input *registry.InputError
	switch {
	case errors.Is(err, registry.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "Your role does not allow this action.", nil)
	case errors.Is(err, registry.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Workflow or version not found.", nil)
	case errors.Is(err, registry.ErrImmutable):
		writeError(w, r, http.StatusConflict, "immutable_version", "Published versions are immutable; create a new draft version instead.", nil)
	case errors.Is(err, registry.ErrPreconditionFailed):
		writeError(w, r, http.StatusPreconditionFailed, "version_mismatch", "This version changed since you loaded it. Reload and re-apply your edits.", nil)
	case errors.Is(err, registry.ErrIdempotencyReuse):
		writeError(w, r, http.StatusUnprocessableEntity, "idempotency_key_reused", "This Idempotency-Key was already used for a different request.", nil)
	case errors.As(err, &conflict):
		writeError(w, r, http.StatusConflict, conflict.Code, conflict.Message, nil)
	case errors.As(err, &input):
		writeError(w, r, http.StatusBadRequest, input.Code, input.Message, []ErrorDetail{{Code: input.Code, Message: input.Message, Path: input.Path}})
	case errors.As(err, &verr):
		var details []ErrorDetail
		for _, d := range verr.Report.Errors() {
			details = append(details, ErrorDetail{Code: d.Code, Message: d.Message, Path: d.Path})
		}
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_definition", "The workflow definition has validation errors.", details)
	default:
		s.deps.Logger.ErrorContext(r.Context(), "registry error", "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, "internal", "Unexpected server error.", nil)
	}
}

func writeSave(w http.ResponseWriter, status int, res registry.SaveResult) {
	w.Header().Set("ETag", etag(res.Version.VersionNum))
	writeJSON(w, status, res)
}

// --- handlers -----------------------------------------------------------------------------------

func (s *server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after := ""
	if c := q.Get("cursor"); c != "" {
		b, err := base64.RawURLEncoding.DecodeString(c)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_cursor", "Cursor is not valid.", nil)
			return
		}
		after = string(b)
	}
	items, next, err := s.deps.Registry.Service.ListDefinitions(r.Context(), identity(r), registry.ListFilter{
		Status: q.Get("status"), Query: q.Get("q"), After: after, Limit: intQuery(r, "limit", 50)})
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	var cursor *string
	if next != "" {
		c := base64.RawURLEncoding.EncodeToString([]byte(next))
		cursor = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": cursor})
}

func (s *server) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var in registry.CreateInput
	body, ok := decodeBody(w, r, &in)
	if !ok {
		return
	}
	res, err := s.deps.Registry.Service.CreateDefinition(r.Context(), identity(r), idempotency(r, body), in)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/workflows/%s/versions/%d", res.Version.WorkflowKey, res.Version.Version))
	writeSave(w, http.StatusCreated, res)
}

func (s *server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	d, err := s.deps.Registry.Service.GetDefinition(r.Context(), identity(r), r.PathValue("key"))
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) listVersions(w http.ResponseWriter, r *http.Request) {
	d, err := s.deps.Registry.Service.GetDefinition(r.Context(), identity(r), r.PathValue("key"))
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Versions})
}

func (s *server) duplicateWorkflow(w http.ResponseWriter, r *http.Request) {
	var in registry.DuplicateInput
	body, ok := decodeBody(w, r, &in)
	if !ok {
		return
	}
	res, err := s.deps.Registry.Service.Duplicate(r.Context(), identity(r), idempotency(r, body), r.PathValue("key"), in)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/workflows/%s/versions/1", res.Version.WorkflowKey))
	writeSave(w, http.StatusCreated, res)
}

func (s *server) createVersion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FromVersion int `json:"from_version"`
	}
	body, ok := decodeBody(w, r, &in)
	if !ok {
		return
	}
	res, err := s.deps.Registry.Service.CreateDraftVersion(r.Context(), identity(r), idempotency(r, body), r.PathValue("key"), in.FromVersion)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/workflows/%s/versions/%d", res.Version.WorkflowKey, res.Version.Version))
	writeSave(w, http.StatusCreated, res)
}

func (s *server) getVersion(w http.ResponseWriter, r *http.Request) {
	v, ok := versionParam(w, r)
	if !ok {
		return
	}
	ver, err := s.deps.Registry.Service.GetVersion(r.Context(), identity(r), r.PathValue("key"), v)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(ver.VersionNum))
	writeJSON(w, http.StatusOK, ver)
}

func (s *server) getVersionDependencies(w http.ResponseWriter, r *http.Request) {
	v, ok := versionParam(w, r)
	if !ok {
		return
	}
	snapshot, err := s.deps.Registry.Service.ObserveBindings(r.Context(), identity(r), r.PathValue("key"), v)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", strconv.Quote(snapshot.ContentHash))
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) updateVersion(w http.ResponseWriter, r *http.Request) {
	v, ok := versionParam(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var in registry.UpdateInput
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	res, err := s.deps.Registry.Service.UpdateDraft(r.Context(), identity(r), r.PathValue("key"), v, expected, in)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	writeSave(w, http.StatusOK, res)
}

func (s *server) validateVersion(w http.ResponseWriter, r *http.Request) {
	v, ok := versionParam(w, r)
	if !ok {
		return
	}
	res, err := s.deps.Registry.Service.ValidateVersion(r.Context(), identity(r), r.PathValue("key"), v)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	writeSave(w, http.StatusOK, res)
}

func (s *server) publishVersion(w http.ResponseWriter, r *http.Request) {
	v, ok := versionParam(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	res, err := s.deps.Registry.Service.Publish(r.Context(), identity(r), r.PathValue("key"), v, expected)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	writeSave(w, http.StatusOK, res)
}

func (s *server) deprecateVersion(w http.ResponseWriter, r *http.Request) {
	v, ok := versionParam(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatch(w, r)
	if !ok {
		return
	}
	res, err := s.deps.Registry.Service.Deprecate(r.Context(), identity(r), r.PathValue("key"), v, expected)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	writeSave(w, http.StatusOK, res)
}

func (s *server) validateDocument(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Definition json.RawMessage `json:"definition"`
	}
	if _, ok := decodeBody(w, r, &in); !ok {
		return
	}
	if len(in.Definition) == 0 {
		writeError(w, r, http.StatusBadRequest, "required", "definition is required.", []ErrorDetail{{Code: "required", Message: "definition is required", Path: "definition"}})
		return
	}
	report, err := s.deps.Registry.Service.ValidateDocument(r.Context(), identity(r), in.Definition)
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type templateView struct {
	Pack        string `json:"pack"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *server) listTemplates(w http.ResponseWriter, r *http.Request) {
	if len(identity(r).Roles) == 0 {
		s.registryError(w, r, registry.ErrForbidden)
		return
	}
	out := []templateView{}
	for _, t := range s.deps.Registry.Templates {
		out = append(out, templateView{Pack: t.Pack, Key: t.Key, Name: t.Name, Description: t.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *server) getCatalog(w http.ResponseWriter, r *http.Request) {
	if len(identity(r).Roles) == 0 {
		s.registryError(w, r, registry.ErrForbidden)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, catalog.ForTenant(s.deps.Registry.Catalog, identity(r).TenantID).Snapshot())
}

func (s *server) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, err := strconv.ParseInt(q.Get("cursor"), 10, 64)
	if q.Get("cursor") != "" && (err != nil || before < 1) {
		writeError(w, r, http.StatusBadRequest, "invalid_cursor", "Cursor is not valid.", nil)
		return
	}
	entries, next, err := s.deps.Registry.Service.ListAudit(r.Context(), identity(r), audit.Filter{
		SubjectType: q.Get("subject_type"), SubjectID: q.Get("subject_id"), BeforeSeq: before, Limit: intQuery(r, "limit", 50)})
	if err != nil {
		s.registryError(w, r, err)
		return
	}
	var cursor *string
	if next != 0 {
		c := strconv.FormatInt(next, 10)
		cursor = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries, "next_cursor": cursor})
}
