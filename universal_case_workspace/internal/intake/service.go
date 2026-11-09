package intake

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/case-workspace/internal/agentstudio"
	"github.com/elenio/case-workspace/internal/apierr"
	"github.com/elenio/case-workspace/internal/audit"
	"github.com/elenio/case-workspace/internal/auth"
	"github.com/elenio/case-workspace/internal/docling"
	"github.com/elenio/case-workspace/internal/flowcase"
	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/internal/store"
)

const (
	maxUserTurns = 30
	maxMessage   = 4000
	// startGrace bounds a turn whose run was never recorded as started (e.g. a crash mid-request).
	startGrace = time.Minute
	statusOpen = "open"
)

// Friendly, user-facing failure texts. Details go to the log, never to the chat.
const (
	msgFailed      = "The assistant could not answer this message. Please try again."
	msgTimeout     = "The assistant took too long to answer. Please try again."
	msgLost        = "The assistant's answer could not be retrieved. Please try again."
	msgUnavailable = "The assistant is temporarily unavailable. Your message is saved; please try again in a moment."
	msgRejected    = "The assistant could not accept this message. Please rephrase it or try again."
)

var clientKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// Gate decides whether the caller may open an intake for a pack (pack live/startable and the user can
// create Flowcase cases). It returns a client error.
type Gate func(ctx context.Context, id auth.Identity, p *packs.Pack) error

// FlowcaseAPI is the part of Flowcase the intake uses (as the user).
type FlowcaseAPI interface {
	CreateCase(ctx context.Context, token, idempotencyKey string, in flowcase.CreateInput) (*flowcase.CaseDetail, error)
	AddNote(ctx context.Context, token, caseID, body string) error
}

// Binder pins a case to a pack version once.
type Binder interface {
	Bind(ctx context.Context, tenant string, b store.Binding) (*store.Binding, error)
}

type Service struct {
	DB       *pgxpool.Pool
	Runner   agentstudio.Runner
	Packs    *packs.Registry
	Gate     Gate
	Flowcase FlowcaseAPI
	// Token returns the caller's Flowcase-audience token (Control Plane token exchange).
	Token  func(ctx context.Context, id auth.Identity) (string, error)
	Binder Binder
	Audit  *audit.Recorder
	Log    *slog.Logger
	// Docling converts uploaded PDF/DOCX documents (nil: only text files can be uploaded).
	Docling docling.Converter
	// PendingTimeout bounds one turn (default 3 minutes), DocumentTimeout one document conversion (default
	// 10 minutes); Now is injectable for tests.
	PendingTimeout  time.Duration
	DocumentTimeout time.Duration
	Now             func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) timeout() time.Duration {
	if s.PendingTimeout > 0 {
		return s.PendingTimeout
	}
	return 3 * time.Minute
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func conflict(code, msg string) *apierr.Error { return apierr.New(http.StatusConflict, code, msg) }

// FieldView is one brief field as the client renders it.
type FieldView struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Widget    string `json:"widget"`
	Required  bool   `json:"required"`
	Help      string `json:"help,omitempty"`
	MaxLength int    `json:"max_length"`
}

// Session is the client view of an intake session (ETag = version_num).
type Session struct {
	ID            string         `json:"id"`
	PackKey       string         `json:"pack_key"`
	PackVersion   string         `json:"pack_version"`
	WorkflowKey   string         `json:"workflow_key"`
	Status        string         `json:"status"`
	Fields        []FieldView    `json:"fields"`
	Greeting      string         `json:"greeting"`
	Starters      []string       `json:"starters"`
	Draft         Draft          `json:"draft"`
	Messages      []Message      `json:"messages"`
	KBStatus      string         `json:"kb_status"`
	KBItems       []KBItem       `json:"kb_items"`
	Sources       []Source       `json:"sources"`
	Documents     []DocumentView `json:"documents"`
	Uploads       *UploadInfo    `json:"uploads"`
	Ready         bool           `json:"ready"`
	Missing       []string       `json:"missing"`
	Pending       bool           `json:"pending"`
	LastError     *string        `json:"last_error"`
	CaseID        *string        `json:"case_id"`
	CaseReference *string        `json:"case_reference"`
	Turn          int            `json:"turn"`
	VersionNum    int            `json:"version_num"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`

	intake  packs.Intake
	pending pendingState
}

type pendingState struct {
	Key, RunID, Agent *string
	Since             *time.Time
}

func (p pendingState) active() bool { return p.Key != nil }

// Created identifies the case an intake session created.
type Created struct {
	CaseID    string `json:"case_id"`
	Reference string `json:"reference"`
}

const sessionColumns = `id::text, pack_key, pack_version, workflow_key, intake, status, draft, messages, kb_status, kb_items, sources,
	ready, missing, pending_key, pending_run_id, pending_agent, pending_since, turn, last_error, case_id, case_reference,
	created_at, updated_at, version_num`

func isUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4 && strings.Trim(strings.ToLower(s), "0123456789abcdef-") == ""
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	rowsQuerier
}

// load reads a session the caller owns; anyone else (other users, other tenants) gets 404.
func load(ctx context.Context, q querier, a auth.Identity, id string, forUpdate bool) (*Session, error) {
	if !isUUID(id) {
		return nil, apierr.NotFound
	}
	sql := `SELECT ` + sessionColumns + ` FROM workspace_intake_sessions WHERE id = $1 AND tenant_key = $2 AND user_id = $3`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var ses Session
	var intakeRaw, draft, messages, kbItems, sources, missing []byte
	err := q.QueryRow(ctx, sql, strings.ToLower(id), a.TenantKey, a.UserID).Scan(&ses.ID, &ses.PackKey, &ses.PackVersion, &ses.WorkflowKey, &intakeRaw,
		&ses.Status, &draft, &messages, &ses.KBStatus, &kbItems, &sources, &ses.Ready, &missing, &ses.pending.Key, &ses.pending.RunID,
		&ses.pending.Agent, &ses.pending.Since, &ses.Turn, &ses.LastError, &ses.CaseID, &ses.CaseReference, &ses.CreatedAt, &ses.UpdatedAt, &ses.VersionNum)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.NotFound
	}
	if err != nil {
		return nil, err
	}
	for _, p := range []struct {
		raw []byte
		dst any
	}{{intakeRaw, &ses.intake}, {draft, &ses.Draft}, {messages, &ses.Messages}, {kbItems, &ses.KBItems}, {sources, &ses.Sources}, {missing, &ses.Missing}} {
		if len(p.raw) > 0 {
			if err := json.NewDecoder(bytes.NewReader(p.raw)).Decode(p.dst); err != nil {
				return nil, fmt.Errorf("intake session %s: %w", id, err)
			}
		}
	}
	ses.Pending = ses.pending.active()
	ses.Greeting, ses.Starters = ses.intake.Greeting, ses.intake.Starters
	for _, f := range ses.intake.Fields {
		ses.Fields = append(ses.Fields, FieldView{Key: f.Key, Label: f.Label, Widget: f.Widget, Required: f.Required, Help: f.Help, MaxLength: limit(f)})
	}
	for _, p := range []*[]string{&ses.Starters, &ses.Missing} {
		if *p == nil {
			*p = []string{}
		}
	}
	if ses.Fields == nil {
		ses.Fields = []FieldView{}
	}
	if ses.Draft == nil {
		ses.Draft = Draft{}
	}
	if ses.Messages == nil {
		ses.Messages = []Message{}
	}
	if ses.KBItems == nil {
		ses.KBItems = []KBItem{}
	}
	if ses.Sources == nil {
		ses.Sources = []Source{}
	}
	if ses.Documents, err = loadDocuments(ctx, q, ses.ID); err != nil {
		return nil, err
	}
	if cfg := ses.intake.Documents; cfg != nil {
		ses.Uploads = &UploadInfo{MaxFiles: cfg.MaxFiles, MaxBytes: MaxDocumentBytes, Accept: Accepted}
		if f, ok := field(ses.intake.Fields, cfg.TargetField); ok {
			_, ses.Uploads.Truncated = documentText(ses.Documents, limit(f))
		}
	}
	return &ses, nil
}

func jsonb(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (s *Service) packTitle(ses *Session) string {
	if p := s.Packs.Get(ses.PackKey, ses.PackVersion); p != nil && p.Manifest != nil {
		return p.Manifest.Title
	}
	return ""
}

// Create opens a session on the newest valid published version of the pack, with its fixed greeting.
func (s *Service) Create(ctx context.Context, a auth.Identity, packKey string) (*Session, error) {
	p := s.Packs.LatestPublished(packKey)
	if p == nil {
		for _, q := range s.Packs.All() {
			if q.Key == packKey {
				return nil, conflict("pack_not_startable", "This domain pack has no valid published version.")
			}
		}
		return nil, apierr.New(http.StatusNotFound, "pack_not_found", "No such domain pack.")
	}
	if p.Manifest.Intake == nil {
		return nil, conflict("pack_not_startable", "This domain pack has no intake chat.")
	}
	if s.Gate != nil {
		if err := s.Gate(ctx, a, p); err != nil {
			return nil, err
		}
	}
	in := p.Manifest.Intake
	id := newUUID()
	msgs := []Message{{Role: RoleAssistant, Content: in.Greeting, At: s.now().UTC()}}
	if _, err := s.DB.Exec(ctx, `INSERT INTO workspace_intake_sessions (id, tenant_key, user_id, pack_key, pack_version, workflow_key, intake, messages, missing)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, id, a.TenantKey, a.UserID, p.Key, p.Version, p.Manifest.Bindings.FlowcaseWorkflowKey,
		jsonb(in), jsonb(msgs), jsonb(Missing(in.Fields, Draft{}))); err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, a.UserID, "intake.session_opened", "intake_session:"+id, map[string]any{"pack_key": p.Key, "pack_version": p.Version})
	return load(ctx, s.DB, a, id, false)
}

// Get returns the caller's session. While a turn is pending it polls the run once and applies a
// finished result, so clients only need to poll this.
func (s *Service) Get(ctx context.Context, a auth.Identity, id string) (*Session, error) {
	ses, err := load(ctx, s.DB, a, id, false)
	if err != nil {
		return nil, err
	}
	busy := processing(ses.Documents)
	if busy {
		if err := s.settleDocuments(ctx, a, ses); err != nil {
			return nil, err
		}
		if ses, err = load(ctx, s.DB, a, id, false); err != nil {
			return nil, err
		}
	}
	if !ses.pending.active() {
		return ses, nil
	}
	if err := s.settle(ctx, a, ses); err != nil {
		return nil, err
	}
	return load(ctx, s.DB, a, id, false)
}

// fail ends the pending turn identified by key with a friendly error; nothing is appended.
func (s *Service) fail(ctx context.Context, a auth.Identity, id, key, message string) error {
	_, err := s.DB.Exec(ctx, `UPDATE workspace_intake_sessions SET pending_key = NULL, pending_run_id = NULL, pending_agent = NULL, pending_since = NULL,
		last_error = $4, version_num = version_num + 1, updated_at = now() WHERE tenant_key = $1 AND id = $2 AND pending_key = $3`,
		a.TenantKey, id, key, message)
	return err
}

// settle checks the pending turn once: a finished run is applied, a failed, lost or overdue one ends the
// turn with a friendly error, a running one is left alone.
func (s *Service) settle(ctx context.Context, a auth.Identity, ses *Session) error {
	p := ses.pending
	key := *p.Key
	overdue := p.Since != nil && s.now().Sub(*p.Since) > s.timeout()
	log := s.log().With("intake_session", ses.ID, "turn", ses.Turn)
	if p.RunID == nil {
		if p.Since != nil && s.now().Sub(*p.Since) > startGrace {
			log.WarnContext(ctx, "intake turn never started")
			return s.fail(ctx, a, ses.ID, key, msgFailed)
		}
		return nil
	}
	runID := *p.RunID
	run, err := s.Runner.GetRun(ctx, runID)
	switch {
	case errors.Is(err, agentstudio.ErrUnknownRun):
		log.WarnContext(ctx, "intake run not found", "run_id", runID)
		return s.fail(ctx, a, ses.ID, key, msgLost)
	case err != nil && overdue:
		log.WarnContext(ctx, "intake run overdue and unreachable", "run_id", runID, "error", err.Error())
		return s.fail(ctx, a, ses.ID, key, msgTimeout)
	case err != nil:
		log.WarnContext(ctx, "intake run poll failed; will retry", "run_id", runID, "error", err.Error())
		return nil
	case !run.Terminal():
		if !overdue {
			return nil
		}
		if err := s.Runner.CancelRun(ctx, runID); err != nil {
			log.WarnContext(ctx, "cancel overdue intake run failed", "run_id", runID, "error", err.Error())
		}
		return s.fail(ctx, a, ses.ID, key, msgTimeout)
	case !run.Succeeded():
		log.WarnContext(ctx, "intake run did not succeed", "run_id", runID, "status", run.Status, "error", run.Error)
		return s.fail(ctx, a, ses.ID, key, msgFailed)
	}
	turn, err := Reconcile(run, deref(p.Agent))
	if err != nil {
		log.WarnContext(ctx, "intake run result rejected", "run_id", runID, "agent_id", run.AgentID, "error", err.Error())
		s.Audit.Record(ctx, a.UserID, "intake.turn_rejected", "intake_session:"+ses.ID, map[string]any{"turn": ses.Turn, "run_id": runID, "reason": err.Error()})
		return s.fail(ctx, a, ses.ID, key, msgFailed)
	}
	applied := false
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		cur, err := load(ctx, tx, a, ses.ID, true)
		if err != nil {
			return err
		}
		if cur.pending.RunID == nil || *cur.pending.RunID != runID {
			return nil // already applied (or superseded) by a concurrent poll
		}
		fields := cur.intake.Fields
		var locked []string // the document field holds the uploaded text; the agent never rewrites it
		if cfg := cur.intake.Documents; cfg != nil && readyDocs(cur.Documents) {
			locked = append(locked, cfg.TargetField)
		}
		changed := cur.Draft.Merge(fields, turn.CaseUpdate, locked...)
		missing := Missing(fields, cur.Draft)
		kbItems := cur.KBItems
		if len(turn.KBItems) > 0 || turn.KBStatus == "not_covered" {
			kbItems = turn.KBItems
		}
		if kbItems == nil {
			kbItems = []KBItem{}
		}
		msgs := append(cur.Messages, Message{Role: RoleAssistant, Content: turn.Reply, At: s.now().UTC(), Sources: turn.Sources,
			UpdatedFields: changed, KBItems: turn.KBItems, QuickReplies: turn.QuickReplies, Unresolved: turn.Unresolved})
		_, err = tx.Exec(ctx, `UPDATE workspace_intake_sessions SET draft = $3, messages = $4, kb_status = $5, kb_items = $6, sources = $7, ready = $8,
			missing = $9, pending_key = NULL, pending_run_id = NULL, pending_agent = NULL, pending_since = NULL, last_error = NULL,
			version_num = version_num + 1, updated_at = now() WHERE tenant_key = $1 AND id = $2`,
			a.TenantKey, ses.ID, jsonb(cur.Draft), jsonb(msgs), turn.KBStatus, jsonb(kbItems), jsonb(mergeSources(cur.Sources, turn.Sources)),
			Ready(turn.ModelReady, missing, turn.KBStatus), jsonb(missing))
		applied = err == nil
		return err
	})
	if applied {
		s.Audit.Record(ctx, a.UserID, "intake.turn_applied", "intake_session:"+ses.ID, map[string]any{"turn": ses.Turn, "run_id": runID,
			"kb_status": turn.KBStatus, "sources": len(turn.Sources)})
	}
	return err
}

// Reconcile verifies a succeeded run before it is applied: the agent is the one this turn started, the
// output matches its checksum and is at most 256 KiB, and it parses as an intake turn.
func Reconcile(run agentstudio.Run, expectedAgent string) (Turn, error) {
	if expectedAgent == "" || run.AgentID != expectedAgent {
		return Turn{}, errors.New("agent mismatch")
	}
	if len(run.Output) == 0 || len(run.Output) > agentstudio.MaxOutput {
		return Turn{}, errors.New("output missing or too large")
	}
	sum, err := agentstudio.OutputChecksum(run.Output)
	if err != nil || sum != run.OutputChecksum {
		return Turn{}, errors.New("output checksum mismatch")
	}
	return ParseTurn(run.Output)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func userTurns(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == RoleUser {
			n++
		}
	}
	return n
}

// IdempotencyKey is the Agent Studio key of one turn.
func IdempotencyKey(sessionID string, turn int) string {
	return fmt.Sprintf("ucw-intake-%s:%d", sessionID, turn)
}

// turnStart is a turn recorded as pending whose agent run is still to be started.
type turnStart struct {
	key, agentID string
	input        json.RawMessage
	turn         int
}

// prepareTurn saves msgs (ending with the user's message) and marks the next turn pending, inside tx.
func (s *Service) prepareTurn(ctx context.Context, tx pgx.Tx, a auth.Identity, ses *Session, msgs []Message) (*turnStart, error) {
	fields := ses.intake.Fields
	input, err := BuildInput(s.packTitle(ses), ses.WorkflowKey, fields, ses.Draft, Missing(fields, ses.Draft), msgs)
	if err != nil {
		return nil, err
	}
	t := &turnStart{agentID: ses.intake.AgentKey, input: input, turn: ses.Turn + 1}
	t.key = IdempotencyKey(ses.ID, t.turn)
	_, err = tx.Exec(ctx, `UPDATE workspace_intake_sessions SET messages = $3, turn = $4, pending_key = $5, pending_agent = $6, pending_since = $7,
		pending_run_id = NULL, last_error = NULL, version_num = version_num + 1, updated_at = now() WHERE tenant_key = $1 AND id = $2`,
		a.TenantKey, ses.ID, jsonb(msgs), t.turn, t.key, t.agentID, s.now())
	return t, err
}

// startTurn starts the agent run of a prepared turn. A failed start keeps the message and records last_error.
func (s *Service) startTurn(ctx context.Context, a auth.Identity, id string, t turnStart) (*Session, error) {
	id = strings.ToLower(id)
	s.Audit.Record(ctx, a.UserID, "intake.turn_started", "intake_session:"+id, map[string]any{"turn": t.turn, "agent": t.agentID})
	req := agentstudio.RunRequest{IdempotencyKey: t.key, AgentID: t.agentID, SkillVersion: "", Input: t.input, Evidence: []any{}, DelegatedUser: a.UserID}
	if corr := "intake:" + id; agentstudio.RefPattern.MatchString(corr) {
		req.CorrelationID = corr
	}
	run, err := s.Runner.StartRun(ctx, req)
	if err != nil {
		cerr := apierr.New(http.StatusServiceUnavailable, "agent_studio_unavailable", msgUnavailable)
		if errors.Is(err, agentstudio.ErrRejected) && !errors.Is(err, agentstudio.ErrUnavailable) {
			cerr = apierr.New(http.StatusUnprocessableEntity, "agent_studio_rejected", msgRejected)
		}
		s.log().WarnContext(ctx, "intake run start failed", "intake_session", id, "error", err.Error())
		if ferr := s.fail(ctx, a, id, t.key, cerr.Message); ferr != nil {
			return nil, ferr
		}
		return nil, cerr
	}
	if _, err := s.DB.Exec(ctx, `UPDATE workspace_intake_sessions SET pending_run_id = $4, version_num = version_num + 1, updated_at = now()
		WHERE tenant_key = $1 AND id = $2 AND pending_key = $3`, a.TenantKey, id, t.key, run.RunID); err != nil {
		return nil, err
	}
	return load(ctx, s.DB, a, id, false)
}

// Send appends the user's message and starts one intake agent run for it. Retrying the same text
// after a failed turn reuses the saved message. A failed start keeps the message and records last_error.
func (s *Service) Send(ctx context.Context, a auth.Identity, id, content string) (*Session, error) {
	content = strings.TrimSpace(content)
	if n := utf8.RuneCountInString(content); n < 1 || n > maxMessage {
		return nil, &apierr.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_message",
			Message: fmt.Sprintf("message must have 1 to %d characters", maxMessage), Details: []apierr.Detail{{Code: "invalid_message", Message: "length", Path: "content"}}}
	}
	var t *turnStart
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		ses, err := load(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		switch {
		case ses.Status != statusOpen:
			return conflict("session_closed", "This conversation already created a case. Start a new conversation.")
		case ses.pending.active():
			return conflict("turn_in_progress", "The assistant is still answering the previous message.")
		case processing(ses.Documents):
			return conflict("document_processing", "Wait until the uploaded document has been read.")
		}
		msgs := ses.Messages
		last := len(msgs) - 1
		if !(last >= 0 && msgs[last].Role == RoleUser && msgs[last].Content == content) {
			if userTurns(msgs) >= maxUserTurns {
				return conflict("too_many_turns", fmt.Sprintf("This conversation reached %d messages. Create the case or start a new conversation.", maxUserTurns))
			}
			msgs = append(msgs, Message{Role: RoleUser, Content: content, At: s.now().UTC()})
		}
		t, err = s.prepareTurn(ctx, tx, a, ses, msgs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.startTurn(ctx, a, id, *t)
}

// UpdateDraft replaces the draft with a person's edit (the brief form); missing and ready are
// recomputed from the pack's required fields and the last knowledge-base coverage.
func (s *Service) UpdateDraft(ctx context.Context, a auth.Identity, id string, ifMatch int, raw map[string]any) (*Session, error) {
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		ses, err := load(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		if ses.Status != statusOpen {
			return conflict("session_closed", "This conversation already created a case.")
		}
		if ses.VersionNum != ifMatch {
			return apierr.New(http.StatusPreconditionFailed, "version_mismatch", "This conversation changed since you loaded it. Reload and try again.")
		}
		clean, err := Clean(ses.intake.Fields, raw)
		if err != nil {
			return err
		}
		missing := Missing(ses.intake.Fields, clean)
		_, err = tx.Exec(ctx, `UPDATE workspace_intake_sessions SET draft = $3, missing = $4, ready = $5, version_num = version_num + 1, updated_at = now()
			WHERE tenant_key = $1 AND id = $2`, a.TenantKey, ses.ID, jsonb(clean), jsonb(missing), Ready(true, missing, ses.KBStatus))
		return err
	})
	if err != nil {
		return nil, err
	}
	return load(ctx, s.DB, a, id, false)
}

// FlowcaseIdempotencyKey derives the key forwarded to Flowcase from the client's key and the session.
func FlowcaseIdempotencyKey(clientKey, sessionID string) string {
	sum := sha256.Sum256([]byte(clientKey + "\x00" + strings.ToLower(sessionID)))
	return "ucw-" + hex.EncodeToString(sum[:])[:40]
}

// CreateCase creates the Flowcase case (as the user) from the draft via the pack's field mapping, pins
// the session's pack version to the case and adds a case note (best effort). A session that already
// created its case answers with that case again.
func (s *Service) CreateCase(ctx context.Context, a auth.Identity, id string, ifMatch int, clientKey string) (Created, error) {
	if !clientKeyPattern.MatchString(clientKey) {
		return Created{}, apierr.New(http.StatusBadRequest, "idempotency_key_required", "Send an Idempotency-Key header (8-128 characters: letters, digits, . _ : -).")
	}
	ses, err := load(ctx, s.DB, a, id, false)
	if err != nil {
		return Created{}, err
	}
	if ses.Status == "created" && ses.CaseID != nil {
		return Created{CaseID: *ses.CaseID, Reference: deref(ses.CaseReference)}, nil
	}
	if ses.Status != statusOpen {
		return Created{}, conflict("session_closed", "This conversation is closed.")
	}
	if ses.VersionNum != ifMatch {
		return Created{}, apierr.New(http.StatusPreconditionFailed, "version_mismatch", "This conversation changed since you loaded it. Reload and try again.")
	}
	if p := s.Packs.Get(ses.PackKey, ses.PackVersion); p == nil || !p.Valid() {
		return Created{}, conflict("pack_unavailable", "The domain pack of this conversation is no longer available.")
	}
	in, err := ToCreateInput(ses.intake.Fields, ses.Draft, ses.WorkflowKey)
	if err != nil {
		return Created{}, err
	}
	token, err := s.Token(ctx, a)
	if err != nil {
		if e := apierr.Upstream(err); e != nil {
			return Created{}, e
		}
		return Created{}, err
	}
	det, err := s.Flowcase.CreateCase(ctx, token, FlowcaseIdempotencyKey(clientKey, ses.ID), in)
	if err != nil {
		e := apierr.Upstream(err)
		if e == nil {
			return Created{}, err
		}
		if e.Status == http.StatusBadRequest { // Flowcase input errors are validation failures for the client
			e = &apierr.Error{Status: http.StatusUnprocessableEntity, Code: e.Code, Message: e.Message, Details: e.Details}
		}
		return Created{}, e
	}
	if _, err := s.Binder.Bind(ctx, a.TenantKey, store.Binding{CaseID: det.ID, PackKey: ses.PackKey, PackVersion: ses.PackVersion,
		Binding: store.PinnedAtCreation, BoundBy: a.UserID}); err != nil {
		return Created{}, fmt.Errorf("bind case %s: %w", det.ID, err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE workspace_intake_sessions SET status = 'created', case_id = $3, case_reference = $4,
		version_num = version_num + 1, updated_at = now() WHERE tenant_key = $1 AND id = $2 AND status = 'open'`,
		a.TenantKey, ses.ID, det.ID, det.Reference); err != nil {
		return Created{}, err
	}
	turns := userTurns(ses.Messages)
	s.Audit.Record(ctx, a.UserID, "case.created_via_intake", "case:"+det.ID, map[string]any{"intake_session": ses.ID, "pack_key": ses.PackKey,
		"pack_version": ses.PackVersion, "workflow_key": ses.WorkflowKey, "user_turns": turns})
	noun := "messages"
	if turns == 1 {
		noun = "message"
	}
	note := fmt.Sprintf("Created from the Case Workspace intake chat (%d %s).", turns, noun)
	if err := s.Flowcase.AddNote(ctx, token, det.ID, note); err != nil {
		s.log().WarnContext(ctx, "intake case note failed", "case_id", det.ID, "error", err.Error())
	}
	return Created{CaseID: det.ID, Reference: det.Reference}, nil
}
