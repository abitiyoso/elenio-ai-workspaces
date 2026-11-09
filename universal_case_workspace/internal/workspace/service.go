package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/elenio/case-workspace/internal/apierr"
	"github.com/elenio/case-workspace/internal/audit"
	"github.com/elenio/case-workspace/internal/auth"
	"github.com/elenio/case-workspace/internal/controlplane"
	"github.com/elenio/case-workspace/internal/flowcase"
	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/internal/store"
)

// FlowcaseAudience is the audience UCW exchanges the caller's token for.
const FlowcaseAudience = "flowcase"

// Flowcase role that may create cases (Flowcase cases.Engine.CreateCase requires it).
const roleCaseOwner = "case_owner"

// BindingStore is the case<->pack binding persistence.
type BindingStore interface {
	GetBinding(ctx context.Context, tenant, caseID string) (*store.Binding, error)
	GetBindings(ctx context.Context, tenant string, caseIDs []string) (map[string]store.Binding, error)
	Bind(ctx context.Context, tenant string, b store.Binding) (*store.Binding, error)
}

type Service struct {
	Exchange   controlplane.Exchanger
	Flowcase   *flowcase.Client
	Packs      *packs.Registry
	Bindings   BindingStore
	Audit      *audit.Recorder
	FlowcaseUI string // public path of the Flowcase app, e.g. /apps/flowcase
	Log        *slog.Logger
	LiveTTL    time.Duration
	Now        func() time.Time

	mu   sync.Mutex
	live map[string]liveEntry
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// FlowcaseToken returns the caller's Flowcase-audience token.
func (s *Service) FlowcaseToken(ctx context.Context, id auth.Identity) (string, error) {
	return s.Exchange.Token(ctx, id, FlowcaseAudience)
}

func partial(source string, e *apierr.Error) PartialError {
	return PartialError{Source: source, Code: e.Code, Message: e.Message}
}

// --- me/context -------------------------------------------------------------------------------------

type FlowcaseContext struct {
	Available      bool     `json:"available"`
	Roles          []string `json:"roles"`
	CanCreateCases bool     `json:"can_create_cases"`
}

type MeContext struct {
	UserID        string          `json:"user_id"`
	DisplayName   string          `json:"display_name"`
	TenantKey     string          `json:"tenant_key"`
	WorkspaceKey  string          `json:"workspace_key"`
	PlatformRoles []string        `json:"platform_roles"`
	Flowcase      FlowcaseContext `json:"flowcase"`
	PartialErrors []PartialError  `json:"partial_errors"`
}

// MeContext never fails because of Flowcase: an inactive session or an unreachable Flowcase is reported
// as flowcase.available=false plus a partial error.
func (s *Service) MeContext(ctx context.Context, id auth.Identity) MeContext {
	roles := id.Scopes
	if roles == nil {
		roles = []string{}
	}
	out := MeContext{UserID: id.UserID, TenantKey: id.TenantKey, WorkspaceKey: id.WorkspaceKey, PlatformRoles: roles,
		Flowcase: FlowcaseContext{Roles: []string{}}, PartialErrors: []PartialError{}}
	sess, err := s.flowcaseSession(ctx, id)
	if err != nil {
		e := apierr.Upstream(err)
		if e == nil {
			s.log().ErrorContext(ctx, "flowcase session lookup failed", "error", err.Error())
			e = apierr.FlowcaseUnavailable
		}
		out.PartialErrors = append(out.PartialErrors, partial("flowcase", e))
		return out
	}
	out.DisplayName = sess.DisplayName
	out.Flowcase = FlowcaseContext{Available: true, Roles: sess.Roles, CanCreateCases: slices.Contains(sess.Roles, roleCaseOwner)}
	if out.Flowcase.Roles == nil {
		out.Flowcase.Roles = []string{}
	}
	return out
}

func (s *Service) flowcaseSession(ctx context.Context, id auth.Identity) (flowcase.Session, error) {
	token, err := s.FlowcaseToken(ctx, id)
	if err != nil {
		return flowcase.Session{}, err
	}
	return s.Flowcase.Session(ctx, token)
}

// --- packs ------------------------------------------------------------------------------------------

type Validation struct {
	SchemaOK   bool            `json:"schema_ok"`
	SemanticOK bool            `json:"semantic_ok"`
	Errors     []packs.Problem `json:"errors"`
}

type Live struct {
	Startable bool     `json:"startable"`
	Reasons   []string `json:"reasons"`
}

type PackSummary struct {
	PackKey             string       `json:"pack_key"`
	Version             string       `json:"version"`
	Title               string       `json:"title"`
	Description         string       `json:"description"`
	Status              string       `json:"status"`
	Checksum            string       `json:"checksum"`
	Brand               *packs.Brand `json:"brand"`
	FlowcaseWorkflowKey string       `json:"flowcase_workflow_key"`
	Validation          Validation   `json:"validation"`
	Live                Live         `json:"live"`
	HasIntake           bool         `json:"has_intake"`
}

type liveEntry struct {
	info  liveInfo
	until time.Time
}

type liveInfo struct {
	published bool
	version   int
	required  []string
	err       *apierr.Error
}

// workflowLive checks (as the user, cached briefly) whether the workflow has a published version and,
// when asked, which case variables its input_schema requires.
func (s *Service) workflowLive(ctx context.Context, id auth.Identity, token, key string, needSchema bool) liveInfo {
	ck := id.UserID + "\x00" + key + fmt.Sprint(needSchema)
	ttl := s.LiveTTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	s.mu.Lock()
	if e, ok := s.live[ck]; ok && s.now().Before(e.until) {
		s.mu.Unlock()
		return e.info
	}
	s.mu.Unlock()
	info := liveInfo{}
	wf, err := s.Flowcase.GetWorkflow(ctx, token, key)
	var fe *flowcase.Error
	switch {
	case errors.As(err, &fe) && fe.Status == http.StatusNotFound:
	case err != nil:
		info.err = apierr.Upstream(err)
		if info.err == nil {
			info.err = apierr.FlowcaseUnavailable
		}
	case wf.LatestPublished != nil:
		info.published, info.version = true, *wf.LatestPublished
		if needSchema {
			v, err := s.Flowcase.GetWorkflowVersion(ctx, token, key, info.version)
			if err != nil {
				info.err = apierr.Upstream(err)
				if info.err == nil {
					info.err = apierr.FlowcaseUnavailable
				}
			} else {
				info.required = v.Definition.InputSchema.Required
				if info.required == nil {
					info.required = []string{}
				}
			}
		}
	}
	if info.err == nil || info.err.Status < 500 {
		s.mu.Lock()
		if s.live == nil || len(s.live) > 5000 {
			s.live = map[string]liveEntry{}
		}
		s.live[ck] = liveEntry{info: info, until: s.now().Add(ttl)}
		s.mu.Unlock()
	}
	return info
}

func summary(p *packs.Pack) PackSummary {
	ps := PackSummary{PackKey: p.Key, Version: p.Version, Checksum: p.Checksum,
		Validation: Validation{SchemaOK: p.SchemaOK(), SemanticOK: p.SemanticOK(), Errors: p.Errors()},
		Live:       Live{Reasons: []string{}}}
	if ps.Validation.Errors == nil {
		ps.Validation.Errors = []packs.Problem{}
	}
	if m := p.Manifest; m != nil {
		ps.Title, ps.Description, ps.Status, ps.Brand = m.Title, m.Description, m.Status, m.Brand
		ps.FlowcaseWorkflowKey, ps.HasIntake = m.Bindings.FlowcaseWorkflowKey, m.Intake != nil
	}
	return ps
}

// liveStatus decides whether new work may start on this pack version. token == "" means the caller's
// Flowcase token is not available (tokenErr says why).
func (s *Service) liveStatus(ctx context.Context, id auth.Identity, p *packs.Pack, token string, tokenErr *apierr.Error) Live {
	l := Live{Reasons: []string{}}
	if !p.Valid() {
		l.Reasons = append(l.Reasons, "The pack failed validation.")
		return l
	}
	m := p.Manifest
	if m.Status != "published" {
		l.Reasons = append(l.Reasons, fmt.Sprintf("The pack version is %s.", m.Status))
		return l
	}
	if latest := s.Packs.LatestPublished(p.Key); latest != nil && latest.Version != p.Version {
		l.Reasons = append(l.Reasons, fmt.Sprintf("Superseded by version %s; cases pinned to this version keep it.", latest.Version))
		return l
	}
	if token == "" {
		l.Reasons = append(l.Reasons, "Flowcase could not be checked: "+tokenErr.Message)
		return l
	}
	info := s.workflowLive(ctx, id, token, m.Bindings.FlowcaseWorkflowKey, m.Intake != nil)
	switch {
	case info.err != nil:
		l.Reasons = append(l.Reasons, "Flowcase could not be checked: "+info.err.Message)
	case !info.published:
		l.Reasons = append(l.Reasons, fmt.Sprintf("Flowcase workflow %q has no published version visible to you.", m.Bindings.FlowcaseWorkflowKey))
	}
	if m.Intake != nil && info.err == nil && info.published {
		collected := map[string]bool{}
		for _, f := range m.Intake.Fields {
			if f.Required && strings.HasPrefix(f.MapsTo, "variables.") {
				collected[strings.TrimPrefix(f.MapsTo, "variables.")] = true
			}
		}
		for _, r := range info.required {
			if !collected[r] {
				l.Reasons = append(l.Reasons, fmt.Sprintf("Flowcase requires case variable %q, which the intake does not collect as a required field.", r))
			}
		}
	}
	l.Startable = len(l.Reasons) == 0
	return l
}

type PackList struct {
	Items         []PackSummary  `json:"items"`
	PartialErrors []PartialError `json:"partial_errors"`
}

func (s *Service) ListPacks(ctx context.Context, id auth.Identity) PackList {
	out := PackList{Items: []PackSummary{}, PartialErrors: []PartialError{}}
	token, err := s.FlowcaseToken(ctx, id)
	var terr *apierr.Error
	if err != nil {
		if terr = apierr.Upstream(err); terr == nil {
			terr = apierr.FlowcaseUnavailable
		}
		out.PartialErrors = append(out.PartialErrors, partial("flowcase", terr))
	}
	for _, p := range s.Packs.All() {
		ps := summary(p)
		ps.Live = s.liveStatus(ctx, id, p, token, terr)
		out.Items = append(out.Items, ps)
	}
	return out
}

type PackDetail struct {
	Pack     PackSummary `json:"pack"`
	Manifest any         `json:"manifest"`
}

func (s *Service) GetPack(ctx context.Context, id auth.Identity, key, version string) (*PackDetail, error) {
	p := s.Packs.Get(key, version)
	if p == nil {
		return nil, apierr.New(http.StatusNotFound, "pack_not_found", "No such domain pack version.")
	}
	ps := summary(p)
	token, err := s.FlowcaseToken(ctx, id)
	var terr *apierr.Error
	if err != nil {
		if terr = apierr.Upstream(err); terr == nil {
			terr = apierr.FlowcaseUnavailable
		}
	}
	ps.Live = s.liveStatus(ctx, id, p, token, terr)
	d := &PackDetail{Pack: ps}
	if p.Valid() {
		d.Manifest = p.Canonical
	}
	return d, nil
}

// Gate is the intake precondition: the caller can create Flowcase cases and the pack is startable.
func (s *Service) Gate(ctx context.Context, id auth.Identity, p *packs.Pack) error {
	token, err := s.FlowcaseToken(ctx, id)
	if err != nil {
		if e := apierr.Upstream(err); e != nil {
			return e
		}
		return err
	}
	sess, err := s.Flowcase.Session(ctx, token)
	if err != nil {
		if e := apierr.Upstream(err); e != nil {
			return e
		}
		return err
	}
	if !slices.Contains(sess.Roles, roleCaseOwner) {
		return apierr.New(http.StatusForbidden, "cannot_create_cases", "Your Flowcase roles do not allow creating cases.")
	}
	if l := s.liveStatus(ctx, id, p, token, nil); !l.Startable {
		e := apierr.New(http.StatusConflict, "pack_not_startable", "This domain pack cannot start new cases right now.")
		for _, r := range l.Reasons {
			e.Details = append(e.Details, apierr.Detail{Code: "not_startable", Message: r})
		}
		return e
	}
	return nil
}

// --- cases ------------------------------------------------------------------------------------------

type CaseList struct {
	Items         []CaseCard     `json:"items"`
	NextCursor    *string        `json:"next_cursor"`
	PartialErrors []PartialError `json:"partial_errors"`
}

// ListCases returns the cases the caller can see in Flowcase. Flowcase being unreachable yields an empty
// list with a partial error; an inactive session is a 401.
func (s *Service) ListCases(ctx context.Context, id auth.Identity, q, status, cursor string) (*CaseList, error) {
	out := &CaseList{Items: []CaseCard{}, PartialErrors: []PartialError{}}
	token, err := s.FlowcaseToken(ctx, id)
	if err != nil {
		e := apierr.Upstream(err)
		if e == nil || e.Status < 500 {
			if e == nil {
				return nil, err
			}
			return nil, e
		}
		out.PartialErrors = append(out.PartialErrors, partial("flowcase", e))
		return out, nil
	}
	list, err := s.Flowcase.ListCases(ctx, token, q, status, cursor)
	if err != nil {
		e := apierr.Upstream(err)
		if e == nil {
			return nil, err
		}
		if e.Status < 500 {
			return nil, e
		}
		out.PartialErrors = append(out.PartialErrors, partial("flowcase", e))
		return out, nil
	}
	out.NextCursor = list.NextCursor
	pending := map[string]int{}
	if drafts, err := s.Flowcase.ListDrafts(ctx, token, "pending"); err != nil {
		e := apierr.Upstream(err)
		if e == nil {
			e = apierr.FlowcaseUnavailable
		}
		out.PartialErrors = append(out.PartialErrors, PartialError{Source: "flowcase", Code: e.Code, Message: "Pending draft counts are unavailable: " + e.Message})
	} else {
		for _, d := range drafts {
			pending[d.Case.ID]++
		}
	}
	ids := make([]string, 0, len(list.Items))
	for _, c := range list.Items {
		ids = append(ids, c.ID)
	}
	bindings, err := s.Bindings.GetBindings(ctx, id.TenantKey, ids)
	if err != nil {
		s.log().ErrorContext(ctx, "load case bindings", "error", err.Error())
		out.PartialErrors = append(out.PartialErrors, PartialError{Source: "registry", Code: "registry_unavailable", Message: "Domain pack bindings are unavailable."})
		bindings = nil
	}
	for _, c := range list.Items {
		var ref *PackRef
		if bindings != nil {
			if b, ok := bindings[c.ID]; ok {
				if p := s.Packs.Get(b.PackKey, b.PackVersion); p != nil && p.Valid() {
					ref = refOf(p)
				}
			} else {
				ref = refOf(s.Packs.LatestForWorkflow(c.WorkflowKey)) // pinned when the case is first opened
			}
		}
		out.Items = append(out.Items, cardFrom(c, pending[c.ID], ref))
	}
	return out, nil
}

func (s *Service) caseDetail(ctx context.Context, id auth.Identity, caseID string) (*flowcase.CaseDetail, string, error) {
	if !flowcase.ValidID(caseID) {
		return nil, "", apierr.NotFound
	}
	token, err := s.FlowcaseToken(ctx, id)
	if err != nil {
		if e := apierr.Upstream(err); e != nil {
			return nil, "", e
		}
		return nil, "", err
	}
	d, err := s.Flowcase.GetCase(ctx, token, caseID)
	if err != nil {
		if e := apierr.Upstream(err); e != nil {
			return nil, "", e
		}
		return nil, "", err
	}
	return d, token, nil
}

// GetCase composes the case workspace. Visibility is decided by Flowcase (called as the user) before any
// UCW state is read or written, so a guessed id never reveals or creates a binding.
func (s *Service) GetCase(ctx context.Context, id auth.Identity, caseID string) (*CaseContext, error) {
	d, _, err := s.caseDetail(ctx, id, caseID)
	if err != nil {
		return nil, err
	}
	return s.compose(ctx, id, d), nil
}

func (s *Service) compose(ctx context.Context, id auth.Identity, d *flowcase.CaseDetail) *CaseContext {
	var partials []PartialError
	existing, err := s.Bindings.GetBinding(ctx, id.TenantKey, d.ID)
	var p *packs.Pack
	var b *store.Binding
	if err != nil {
		s.log().ErrorContext(ctx, "load case binding", "case_id", d.ID, "error", err.Error())
		partials = append(partials, PartialError{Source: "registry", Code: "registry_unavailable", Message: "The domain pack binding is unavailable."})
	} else {
		var toPersist *store.Binding
		p, b, toPersist = ResolveBinding(existing, d.WorkflowKey, s.Packs, id.UserID)
		if toPersist != nil {
			toPersist.CaseID = d.ID
			stored, err := s.Bindings.Bind(ctx, id.TenantKey, *toPersist)
			switch {
			case err != nil:
				s.log().ErrorContext(ctx, "bind case to pack", "case_id", d.ID, "error", err.Error())
				partials = append(partials, PartialError{Source: "registry", Code: "registry_unavailable", Message: "The domain pack binding could not be stored."})
				p, b = nil, nil
			case stored.PackKey != toPersist.PackKey || stored.PackVersion != toPersist.PackVersion:
				p, b, _ = ResolveBinding(stored, d.WorkflowKey, s.Packs, id.UserID) // a concurrent view won
			default:
				b = stored
				s.Audit.Record(ctx, id.UserID, "case.pack_bound", "case:"+d.ID, map[string]any{"pack_key": stored.PackKey, "pack_version": stored.PackVersion,
					"binding": stored.Binding})
			}
		}
	}
	c := Compose(d, p, b, s.FlowcaseUI)
	if c.Pack.Status == PackUnavailable && err == nil {
		msg := "No valid domain pack is available for this case's workflow; showing the generic read-only workspace."
		if b != nil {
			msg = fmt.Sprintf("The pinned domain pack %s %s is not available; showing the generic read-only workspace.", b.PackKey, b.PackVersion)
		}
		partials = append(partials, PartialError{Source: "registry", Code: "pack_unavailable", Message: msg})
	}
	c.PartialErrors = append(c.PartialErrors, partials...)
	return &c
}

// ReviewDraft forwards a draft submit or revision request to Flowcase after checking the draft belongs
// to the case, then returns the refreshed case. Flowcase errors pass through.
func (s *Service) ReviewDraft(ctx context.Context, id auth.Identity, caseID, draftID string, ifMatch int, submit bool, text string) (*CaseContext, error) {
	if !flowcase.ValidID(draftID) {
		return nil, apierr.NotFound
	}
	d, token, err := s.caseDetail(ctx, id, caseID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, it := range d.DraftItems() {
		found = found || it.ID == draftID
	}
	if !found {
		return nil, apierr.NotFound
	}
	action := "draft.revision_forwarded"
	if submit {
		action = "draft.submit_forwarded"
		err = s.Flowcase.SubmitDraft(ctx, token, draftID, ifMatch, text)
	} else {
		err = s.Flowcase.ReviseDraft(ctx, token, draftID, ifMatch, text)
	}
	meta := map[string]any{"case_id": caseID, "if_match": ifMatch}
	if err != nil {
		e := apierr.Upstream(err)
		if e == nil {
			return nil, err
		}
		meta["outcome"], meta["code"] = "rejected", e.Code
		s.Audit.Record(ctx, id.UserID, action, "draft:"+draftID, meta)
		return nil, e
	}
	meta["outcome"] = "accepted"
	s.Audit.Record(ctx, id.UserID, action, "draft:"+draftID, meta)
	d, _, err = s.caseDetail(ctx, id, caseID)
	if err != nil {
		return nil, err
	}
	return s.compose(ctx, id, d), nil
}
