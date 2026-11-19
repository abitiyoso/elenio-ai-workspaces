package intake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/case-workspace/internal/apierr"
	"github.com/elenio/case-workspace/internal/auth"
	"github.com/elenio/case-workspace/internal/docling"
)

// Uploaded documents (a KAK, RFP or brief) are converted to text by Docling (OCR for scanned pages). The text of
// every ready document fills the pack's documents.target_field and the pack's prompt is sent as the user's
// message, so the intake agent reads the document in the draft it already receives.

const (
	// MaxDocumentBytes bounds one upload.
	MaxDocumentBytes = 15 << 20
	maxFilename      = 200

	docProcessing = "processing"
	docReady      = "ready"
	docFailed     = "failed"
)

const (
	msgDocUnreadable  = "This document could not be read. Upload a PDF, DOCX, TXT or MD file that is not password-protected."
	msgDocEmpty       = "No text could be read from this document."
	msgDocTimeout     = "Reading this document took too long. Remove it and upload it again."
	msgDocLost        = "This document's conversion was lost. Remove it and upload it again."
	msgDocUnavailable = "Document reading is temporarily unavailable. Please try again in a moment."
)

// Accepted lists the upload extensions shown to the client.
var Accepted = []string{".pdf", ".docx", ".txt", ".md"}

const docxType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// DocumentView is an uploaded document as the client renders it (never its text).
type DocumentView struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	MediaType string    `json:"media_type"`
	SizeBytes int       `json:"size_bytes"`
	Status    string    `json:"status"`
	Chars     *int      `json:"chars"`
	Error     *string   `json:"error"`
	CreatedAt time.Time `json:"created_at"`

	content *string
	taskID  *string
	sha256  string
}

// UploadInfo tells the client whether and what it may upload (nil when the pack has no documents block).
type UploadInfo struct {
	MaxFiles  int      `json:"max_files"`
	MaxBytes  int      `json:"max_bytes"`
	Accept    []string `json:"accept"`
	Truncated bool     `json:"truncated"`
}

// MessageDocument marks a user message that was sent for an uploaded document.
type MessageDocument struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
}

func (s *Service) docTimeout() time.Duration {
	if s.DocumentTimeout > 0 {
		return s.DocumentTimeout
	}
	return 10 * time.Minute
}

type rowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadDocuments(ctx context.Context, q rowsQuerier, sessionID string) ([]DocumentView, error) {
	rows, err := q.Query(ctx, `SELECT id::text, filename, media_type, size_bytes, status, chars, error, created_at, content, task_id, sha256
		FROM workspace_intake_documents WHERE session_id = $1 ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentView{}
	for rows.Next() {
		var d DocumentView
		if err := rows.Scan(&d.ID, &d.Filename, &d.MediaType, &d.SizeBytes, &d.Status, &d.Chars, &d.Error, &d.CreatedAt, &d.content, &d.taskID, &d.sha256); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func processing(docs []DocumentView) bool {
	for _, d := range docs {
		if d.Status == docProcessing {
			return true
		}
	}
	return false
}

func readyDocs(docs []DocumentView) bool {
	for _, d := range docs {
		if d.Status == docReady {
			return true
		}
	}
	return false
}

// documentText joins the ready documents' text under their file names, cut to max runes.
func documentText(docs []DocumentView, max int) (string, bool) {
	var parts []string
	for _, d := range docs {
		if d.Status == docReady && d.content != nil {
			parts = append(parts, "# "+d.Filename+"\n\n"+*d.content)
		}
	}
	text := strings.Join(parts, "\n\n")
	if utf8.RuneCountInString(text) <= max {
		return text, false
	}
	return truncateRunes(text, max), true
}

var (
	imageComment = regexp.MustCompile(`(?m)^\s*<!--\s*image\s*-->\s*$`)
	blankRuns    = regexp.MustCompile(`\n{3,}`)
)

// CleanExtracted normalises Docling Markdown (or a text file) for the draft: entities decoded, image
// placeholders and control characters dropped, blank runs collapsed.
func CleanExtracted(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = imageComment.ReplaceAllString(md, "")
	md = html.UnescapeString(md)
	md = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, md)
	return strings.TrimSpace(blankRuns.ReplaceAllString(md, "\n\n"))
}

// SanitizeFilename keeps the base name without control or path characters, at most 200 characters.
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	// Both browser/Windows and POSIX paths were normalized above. Use slash
	// semantics so a root slash never becomes a Windows backslash filename.
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "." || name == "" {
		return ""
	}
	if utf8.RuneCountInString(name) > maxFilename {
		ext := filepath.Ext(name)
		if utf8.RuneCountInString(ext) > 10 {
			ext = ""
		}
		name = string([]rune(name)[:maxFilename-utf8.RuneCountInString(ext)]) + ext
	}
	return name
}

// DetectDocument decides the media type from content (magic bytes) and extension; ok=false when unsupported.
func DetectDocument(name string, b []byte) (mediaType string, ok bool) {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case bytes.HasPrefix(b, []byte("%PDF-")):
		return "application/pdf", true
	case ext == ".docx" && bytes.HasPrefix(b, []byte("PK\x03\x04")):
		return docxType, true
	case (ext == ".txt" || ext == ".md") && utf8.Valid(b) && !bytes.ContainsRune(b, 0):
		if ext == ".md" {
			return "text/markdown", true
		}
		return "text/plain", true
	}
	return "", false
}

func docError(status int, code, msg string) *apierr.Error {
	return &apierr.Error{Status: status, Code: code, Message: msg, Details: []apierr.Detail{{Code: code, Message: msg, Path: "file"}}}
}

// Upload adds a document to the caller's open session. Text files are ready at once; PDF and DOCX are
// submitted to Docling and finish on a later poll (Get), which then starts the agent turn.
func (s *Service) Upload(ctx context.Context, a auth.Identity, id, filename string, content []byte) (*Session, error) {
	filename = SanitizeFilename(filename)
	switch {
	case filename == "":
		return nil, docError(http.StatusUnprocessableEntity, "invalid_document", "The file needs a name.")
	case len(content) == 0:
		return nil, docError(http.StatusUnprocessableEntity, "invalid_document", "The file is empty.")
	case len(content) > MaxDocumentBytes:
		return nil, docError(http.StatusRequestEntityTooLarge, "document_too_large", fmt.Sprintf("Documents may be at most %d MB.", MaxDocumentBytes>>20))
	}
	mediaType, ok := DetectDocument(filename, content)
	if !ok {
		return nil, docError(http.StatusUnsupportedMediaType, "unsupported_document", "Upload a PDF, DOCX, TXT or MD file.")
	}
	text := strings.HasPrefix(mediaType, "text/")
	if !text && s.Docling == nil {
		return nil, docError(http.StatusServiceUnavailable, "document_conversion_unavailable", msgDocUnavailable)
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	docID := newUUID()
	var sessionID string
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		ses, err := load(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		sessionID = ses.ID
		cfg := ses.intake.Documents
		ready := 0
		for _, d := range ses.Documents {
			if d.Status != docFailed {
				ready++
			}
		}
		switch {
		case cfg == nil:
			return conflict("uploads_disabled", "This intake does not accept documents.")
		case ses.Status != statusOpen:
			return conflict("session_closed", "This conversation already created a case. Start a new conversation.")
		case ses.pending.active():
			return conflict("turn_in_progress", "The assistant is still answering the previous message.")
		case processing(ses.Documents):
			return conflict("document_processing", "Another document is still being read.")
		case ready >= cfg.MaxFiles:
			return conflict("too_many_documents", fmt.Sprintf("This conversation already has %d documents. Remove one to upload another.", cfg.MaxFiles))
		case userTurns(ses.Messages) >= maxUserTurns:
			return conflict("too_many_turns", fmt.Sprintf("This conversation reached %d messages. Create the case or start a new conversation.", maxUserTurns))
		}
		for _, d := range ses.Documents {
			if d.sha256 == digest && d.Status != docFailed {
				return conflict("duplicate_document", "This document is already in the conversation.")
			}
		}
		// A failed upload of the same file is replaced by the new attempt.
		if _, err := tx.Exec(ctx, `DELETE FROM workspace_intake_documents WHERE session_id = $1 AND sha256 = $2 AND status = 'failed'`, ses.ID, digest); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO workspace_intake_documents (id, session_id, tenant_key, filename, media_type, size_bytes, sha256, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'processing')`, docID, ses.ID, a.TenantKey, filename, mediaType, len(content), digest)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE workspace_intake_sessions SET version_num = version_num + 1, updated_at = now() WHERE id = $1`, ses.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, a.UserID, "intake.document_uploaded", "intake_session:"+sessionID, map[string]any{"document_id": docID,
		"media_type": mediaType, "size_bytes": len(content), "sha256": digest})
	if text {
		if err := s.finishDocument(ctx, a, sessionID, docID, CleanExtracted(string(content))); err != nil {
			return nil, err
		}
		return load(ctx, s.DB, a, sessionID, false)
	}
	name := filename
	if mediaType == "application/pdf" && !strings.EqualFold(filepath.Ext(name), ".pdf") {
		name += ".pdf"
	}
	task, err := s.Docling.Submit(ctx, name, content)
	if err != nil {
		s.log().WarnContext(ctx, "document submit failed", "intake_session", sessionID, "document_id", docID, "error", err.Error())
		if ferr := s.failDocument(ctx, a, sessionID, docID, msgDocUnavailable); ferr != nil {
			return nil, ferr
		}
		return nil, docError(http.StatusServiceUnavailable, "document_conversion_unavailable", msgDocUnavailable)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE workspace_intake_documents SET task_id = $3, updated_at = now() WHERE session_id = $1 AND id = $2 AND status = 'processing'`,
		sessionID, docID, task); err != nil {
		return nil, err
	}
	return load(ctx, s.DB, a, sessionID, false)
}

// RemoveDocument deletes a document and rebuilds the document field from those left.
func (s *Service) RemoveDocument(ctx context.Context, a auth.Identity, id, docID string) (*Session, error) {
	if !isUUID(docID) {
		return nil, apierr.NotFound
	}
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		ses, err := load(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		var doc *DocumentView
		for i := range ses.Documents {
			if ses.Documents[i].ID == strings.ToLower(docID) {
				doc = &ses.Documents[i]
			}
		}
		switch {
		case doc == nil:
			return apierr.NotFound
		case ses.Status != statusOpen:
			return conflict("session_closed", "This conversation already created a case.")
		case ses.pending.active() && doc.Status == docReady:
			return conflict("turn_in_progress", "The assistant is still answering. Remove the document after it replies.")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM workspace_intake_documents WHERE session_id = $1 AND id = $2`, ses.ID, doc.ID); err != nil {
			return err
		}
		if doc.Status == docReady {
			left := make([]DocumentView, 0, len(ses.Documents))
			for _, d := range ses.Documents {
				if d.ID != doc.ID {
					left = append(left, d)
				}
			}
			s.setDocumentField(ses, left)
		}
		_, err = tx.Exec(ctx, `UPDATE workspace_intake_sessions SET draft = $3, missing = $4, ready = $5, version_num = version_num + 1, updated_at = now()
			WHERE tenant_key = $1 AND id = $2`, a.TenantKey, ses.ID, jsonb(ses.Draft), jsonb(Missing(ses.intake.Fields, ses.Draft)),
			ses.Ready && len(Missing(ses.intake.Fields, ses.Draft)) == 0)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, a.UserID, "intake.document_removed", "intake_session:"+strings.ToLower(id), map[string]any{"document_id": strings.ToLower(docID)})
	return load(ctx, s.DB, a, id, false)
}

// setDocumentField rewrites the pack's document field from the ready documents (removed when none is left).
func (s *Service) setDocumentField(ses *Session, docs []DocumentView) {
	cfg := ses.intake.Documents
	if cfg == nil {
		return
	}
	f, ok := field(ses.intake.Fields, cfg.TargetField)
	if !ok {
		return
	}
	text, _ := documentText(docs, limit(f))
	if text == "" {
		delete(ses.Draft, cfg.TargetField)
		return
	}
	ses.Draft[cfg.TargetField] = text
}

func (s *Service) failDocument(ctx context.Context, a auth.Identity, sessionID, docID, msg string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE workspace_intake_documents SET status = 'failed', error = $3, updated_at = now()
		WHERE session_id = $1 AND id = $2 AND status = 'processing'`, sessionID, docID, msg)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE workspace_intake_sessions SET version_num = version_num + 1, updated_at = now() WHERE id = $1`, sessionID)
	s.Audit.Record(ctx, a.UserID, "intake.document_failed", "intake_session:"+sessionID, map[string]any{"document_id": docID})
	return err
}

// settleDocuments checks each processing document once: a finished conversion is applied, a failed, lost or
// overdue one is marked failed, a running one is left alone.
func (s *Service) settleDocuments(ctx context.Context, a auth.Identity, ses *Session) error {
	for _, d := range ses.Documents {
		if d.Status != docProcessing {
			continue
		}
		log := s.log().With("intake_session", ses.ID, "document_id", d.ID)
		age := s.now().Sub(d.CreatedAt)
		if d.taskID == nil || s.Docling == nil {
			if age > startGrace {
				log.WarnContext(ctx, "document conversion never started")
				if err := s.failDocument(ctx, a, ses.ID, d.ID, msgDocLost); err != nil {
					return err
				}
			}
			continue
		}
		state, err := s.Docling.Status(ctx, *d.taskID)
		switch {
		case errors.Is(err, docling.ErrUnknownTask):
			log.WarnContext(ctx, "document task not found")
			err = s.failDocument(ctx, a, ses.ID, d.ID, msgDocLost)
		case err != nil && age > s.docTimeout():
			log.WarnContext(ctx, "document task overdue and unreachable", "error", err.Error())
			err = s.failDocument(ctx, a, ses.ID, d.ID, msgDocTimeout)
		case err != nil:
			log.WarnContext(ctx, "document status poll failed; will retry", "error", err.Error())
			err = nil
		case state == docling.StateFailure:
			log.WarnContext(ctx, "document conversion failed")
			err = s.failDocument(ctx, a, ses.ID, d.ID, msgDocUnreadable)
		case state == docling.StatePending && age > s.docTimeout():
			err = s.failDocument(ctx, a, ses.ID, d.ID, msgDocTimeout)
		case state == docling.StateSuccess:
			var md string
			if md, err = s.Docling.Markdown(ctx, *d.taskID); err != nil {
				log.WarnContext(ctx, "document result fetch failed", "error", err.Error())
				err = s.failDocument(ctx, a, ses.ID, d.ID, msgDocUnreadable)
			} else {
				err = s.finishDocument(ctx, a, ses.ID, d.ID, CleanExtracted(md))
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// finishDocument stores a document's text, rebuilds the document field and, when the session can take a
// turn, sends the pack's document prompt as the user's message and starts the agent run.
func (s *Service) finishDocument(ctx context.Context, a auth.Identity, sessionID, docID, text string) error {
	if text == "" {
		return s.failDocument(ctx, a, sessionID, docID, msgDocEmpty)
	}
	var start *turnStart
	applied := false
	chars := utf8.RuneCountInString(text)
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		ses, err := load(ctx, tx, a, sessionID, true)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE workspace_intake_documents SET status = 'ready', content = $3, chars = $4, updated_at = now()
			WHERE session_id = $1 AND id = $2 AND status = 'processing'`, ses.ID, docID, text, chars)
		if err != nil || tag.RowsAffected() == 0 {
			return err // already applied by a concurrent poll (or removed)
		}
		applied = true
		docs, err := loadDocuments(ctx, tx, ses.ID)
		if err != nil {
			return err
		}
		ses.Documents = docs
		s.setDocumentField(ses, docs)
		missing := Missing(ses.intake.Fields, ses.Draft)
		if _, err := tx.Exec(ctx, `UPDATE workspace_intake_sessions SET draft = $3, missing = $4, ready = $5, version_num = version_num + 1, updated_at = now()
			WHERE tenant_key = $1 AND id = $2`, a.TenantKey, ses.ID, jsonb(ses.Draft), jsonb(missing), ses.Ready && len(missing) == 0); err != nil {
			return err
		}
		cfg := ses.intake.Documents
		if cfg == nil || ses.Status != statusOpen || ses.pending.active() || userTurns(ses.Messages) >= maxUserTurns {
			return nil
		}
		var filename string
		for _, d := range docs {
			if d.ID == docID {
				filename = d.Filename
			}
		}
		msg := Message{Role: RoleUser, Content: strings.ReplaceAll(cfg.Prompt, "{filename}", filename), At: s.now().UTC(),
			Document: &MessageDocument{ID: docID, Filename: filename}}
		start, err = s.prepareTurn(ctx, tx, a, ses, append(ses.Messages, msg))
		return err
	})
	if err != nil || !applied {
		return err
	}
	s.Audit.Record(ctx, a.UserID, "intake.document_ready", "intake_session:"+sessionID, map[string]any{"document_id": docID, "chars": chars})
	if start != nil {
		// A failed start is recorded on the session as last_error; the client offers Retry.
		if _, err := s.startTurn(ctx, a, sessionID, *start); err != nil {
			s.log().WarnContext(ctx, "document turn not started", "intake_session", sessionID, "error", err.Error())
		}
	}
	return nil
}
