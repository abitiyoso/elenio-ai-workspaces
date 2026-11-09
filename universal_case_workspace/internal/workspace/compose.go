// Package workspace composes the case workspace view models from Flowcase (read as the user) and the
// pinned domain pack. Everything here is generic: navigation, artifact types and field definitions come
// from the pack manifest; workflow steps, tasks, approvals and drafts come from Flowcase. There is no
// branching on a particular pack or workflow.
package workspace

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/elenio/case-workspace/internal/flowcase"
	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/internal/store"
)

type PackRef struct {
	PackKey string       `json:"pack_key"`
	Version string       `json:"version"`
	Title   string       `json:"title"`
	Brand   *packs.Brand `json:"brand"`
}

type CaseCard struct {
	ID               string    `json:"id"`
	Reference        string    `json:"reference"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	Outcome          *string   `json:"outcome"`
	WorkflowKey      string    `json:"workflow_key"`
	WorkflowVersion  int       `json:"workflow_version"`
	OwnerName        string    `json:"owner_name"`
	CurrentSteps     []string  `json:"current_steps"`
	OpenTasks        int       `json:"open_tasks"`
	PendingApprovals int       `json:"pending_approvals"`
	PendingDrafts    int       `json:"pending_drafts"`
	Progress         int       `json:"progress"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Pack             *PackRef  `json:"pack"`
}

type CaseFull struct {
	CaseCard
	Variables      json.RawMessage `json:"variables"`
	BusinessFacets json.RawMessage `json:"business_facets"`
}

type PackBinding struct {
	PackKey string       `json:"pack_key"`
	Version string       `json:"version"`
	Title   string       `json:"title"`
	Brand   *packs.Brand `json:"brand"`
	Binding string       `json:"binding"`
	BoundAt *time.Time   `json:"bound_at"`
	Status  string       `json:"status"` // ok | unavailable
}

type NavEntry struct {
	ID                 string `json:"id"`
	View               string `json:"view"`
	Label              string `json:"label"`
	ArtifactType       string `json:"artifact_type,omitempty"`
	Icon               string `json:"icon,omitempty"`
	RequiredPermission string `json:"required_permission,omitempty"`
}

type WorkflowNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type Workflow struct {
	Version int            `json:"version"`
	Nodes   []WorkflowNode `json:"nodes"`
}

type Source struct {
	SourceID  string `json:"source_id"`
	VersionID string `json:"version_id"`
	Title     string `json:"title"`
	Citation  string `json:"citation"`
	Anchor    string `json:"anchor"`
}

type ArtifactView struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	PackArtifactType *string         `json:"pack_artifact_type"`
	Version          int             `json:"version"`
	Status           string          `json:"status"`
	ProducerNodeID   string          `json:"producer_node_id"`
	ProducedByKind   string          `json:"produced_by_kind"`
	Content          json.RawMessage `json:"content"`
	Sources          []Source        `json:"sources"`
	UpdatedAt        *time.Time      `json:"updated_at"`
}

type Permissions struct {
	CanReviewDrafts bool `json:"can_review_drafts"`
	CanNote         bool `json:"can_note"`
}

type PartialError struct {
	Source  string `json:"source"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CaseContext struct {
	Case          CaseFull             `json:"case"`
	Pack          PackBinding          `json:"pack"`
	Navigation    []NavEntry           `json:"navigation"`
	ArtifactTypes []packs.ArtifactType `json:"artifact_types"`
	Workflow      Workflow             `json:"workflow"`
	Artifacts     []ArtifactView       `json:"artifacts"`
	Drafts        json.RawMessage      `json:"drafts"`
	Tasks         json.RawMessage      `json:"tasks"`
	Approvals     json.RawMessage      `json:"approvals"`
	AgentRuns     json.RawMessage      `json:"agent_runs"`
	History       json.RawMessage      `json:"history"`
	Permissions   Permissions          `json:"permissions"`
	Links         map[string]string    `json:"links"`
	PartialErrors []PartialError       `json:"partial_errors"`

	VersionNum int `json:"-"`
}

const (
	PackOK          = "ok"
	PackUnavailable = "unavailable"
)

// ResolveBinding applies the pinning rules. An existing binding is kept forever: its pack version is
// used if still valid, otherwise the case gets the generic shell (never a silent re-pin). A case without
// binding is bound on first view to the newest valid published pack for its workflow key; toPersist is
// that new binding (nil when nothing must be stored).
func ResolveBinding(existing *store.Binding, workflowKey string, reg *packs.Registry, viewer string) (p *packs.Pack, binding *store.Binding, toPersist *store.Binding) {
	if existing != nil {
		if q := reg.Get(existing.PackKey, existing.PackVersion); q != nil && q.Valid() {
			return q, existing, nil
		}
		return nil, existing, nil
	}
	if q := reg.LatestForWorkflow(workflowKey); q != nil {
		b := &store.Binding{PackKey: q.Key, PackVersion: q.Version, Binding: store.PinnedOnFirstView, BoundBy: viewer}
		return q, b, b
	}
	return nil, nil, nil
}

// genericShell is the read-only navigation for cases without a usable pack.
var genericShell = []NavEntry{
	{ID: "overview", View: "overview", Label: "Overview"},
	{ID: "tasks", View: "tasks", Label: "Tasks"},
	{ID: "approvals", View: "approvals", Label: "Approvals"},
	{ID: "timeline", View: "timeline", Label: "Timeline"},
}

func cardFrom(s flowcase.CaseSummary, pendingDrafts int, ref *PackRef) CaseCard {
	steps := s.CurrentSteps
	if steps == nil {
		steps = []string{}
	}
	return CaseCard{ID: s.ID, Reference: s.Reference, Title: s.Title, Status: s.Status, Outcome: s.Outcome, WorkflowKey: s.WorkflowKey,
		WorkflowVersion: s.WorkflowVersion, OwnerName: s.OwnerName, CurrentSteps: steps, OpenTasks: s.OpenTasks, PendingApprovals: s.PendingApprovals,
		PendingDrafts: pendingDrafts, Progress: s.Progress, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Pack: ref}
}

func refOf(p *packs.Pack) *PackRef {
	if p == nil || p.Manifest == nil {
		return nil
	}
	return &PackRef{PackKey: p.Key, Version: p.Version, Title: p.Manifest.Title, Brand: p.Manifest.Brand}
}

// NodeStatuses derives one status per workflow node from Flowcase data, in this precedence:
// pending draft review for the node -> waiting_review; pending approval -> waiting_approval; otherwise the
// node's latest execution (superseded ones ignored): completed -> completed, failed/cancelled ->
// failed/pending, pending/ready/running/waiting_human/waiting_external -> active; no execution -> pending.
func NodeStatuses(d *flowcase.CaseDetail) []WorkflowNode {
	review, approval := map[string]bool{}, map[string]bool{}
	for _, it := range d.DraftItems() {
		if it.Status == "pending" {
			review[it.NodeID] = true
		}
	}
	for _, it := range d.ApprovalItems() {
		if it.Status == "pending" {
			approval[it.NodeID] = true
		}
	}
	latest := map[string]flowcase.Execution{}
	for _, e := range d.Timeline {
		if e.Status == "superseded" {
			continue
		}
		cur, ok := latest[e.NodeID]
		if !ok || e.CreatedAt.After(cur.CreatedAt) || (e.CreatedAt.Equal(cur.CreatedAt) && e.Iteration > cur.Iteration) {
			latest[e.NodeID] = e
		}
	}
	out := make([]WorkflowNode, 0, len(d.Nodes))
	for _, n := range d.Nodes {
		status := "pending"
		switch {
		case review[n.ID]:
			status = "waiting_review"
		case approval[n.ID]:
			status = "waiting_approval"
		default:
			if e, ok := latest[n.ID]; ok {
				switch e.Status {
				case "completed":
					status = "completed"
				case "failed":
					status = "failed"
				case "cancelled":
					status = "pending"
				default:
					status = "active"
				}
			}
		}
		out = append(out, WorkflowNode{ID: n.ID, Name: n.Name, Type: n.Type, Status: status})
	}
	return out
}

// envelopeSources reads "sources" from an Agent Studio result envelope stored as artifact content.
// Content itself is passed to the client unchanged.
func envelopeSources(content json.RawMessage) []Source {
	var env struct {
		Sources []Source `json:"sources"`
	}
	out := []Source{}
	if json.Unmarshal(content, &env) != nil {
		return out
	}
	for _, s := range env.Sources {
		if s.SourceID != "" && len(out) < 100 {
			out = append(out, s)
		}
	}
	return out
}

func artifactViews(arts []flowcase.Artifact, m *packs.Manifest) []ArtifactView {
	out := make([]ArtifactView, 0, len(arts))
	for _, a := range arts {
		v := ArtifactView{ID: a.ID, Type: a.Type, Version: a.CurrentVersion, ProducerNodeID: a.ProducerNodeID, Sources: []Source{}}
		if m != nil {
			if t := m.ArtifactTypeFor(a.Type); t != nil {
				key := t.Key
				v.PackArtifactType = &key
			}
		}
		for _, ver := range a.Versions {
			if ver.Version == a.CurrentVersion {
				ts := ver.CreatedAt
				v.Status, v.Content, v.UpdatedAt, v.Sources = ver.Status, ver.Content, &ts, envelopeSources(ver.Content)
				v.ProducedByKind = producedBy(ver.Kind)
			}
		}
		if v.Content == nil {
			v.Content = json.RawMessage(`null`)
		}
		out = append(out, v)
	}
	return out
}

// producedBy maps Flowcase's produced_by_kind (agent | human | revision) onto agent | human: a human
// revision of an agent draft is human-authored content.
func producedBy(kind string) string {
	if kind == "agent" {
		return "agent"
	}
	return "human"
}

// newestFirst orders Flowcase draft reviews by created_at descending (stable), keeping each item raw.
func newestFirst(raw json.RawMessage) json.RawMessage {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return raw
	}
	at := make([]time.Time, len(items))
	for i, it := range items {
		var v struct {
			CreatedAt time.Time `json:"created_at"`
		}
		_ = json.Unmarshal(it, &v)
		at[i] = v.CreatedAt
	}
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return at[idx[a]].After(at[idx[b]]) })
	out := make([]json.RawMessage, len(items))
	for i, j := range idx {
		out[i] = items[j]
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return b
}

func rawOr(r json.RawMessage, def string) json.RawMessage {
	if len(r) == 0 || string(r) == "null" {
		return json.RawMessage(def)
	}
	return r
}

// Compose builds the case context. p is the resolved pack (nil -> generic shell), b the binding (may be
// nil when the case is not bound).
func Compose(d *flowcase.CaseDetail, p *packs.Pack, b *store.Binding, flowcaseUI string) CaseContext {
	drafts := d.DraftItems()
	pending, canReview := 0, false
	for _, it := range drafts {
		if it.Status == "pending" {
			pending++
		}
		canReview = canReview || it.CanSubmit || it.CanRevise
	}
	ctx := CaseContext{
		Case:          CaseFull{CaseCard: cardFrom(d.CaseSummary, pending, refOf(p)), Variables: rawOr(d.Variables, "{}"), BusinessFacets: rawOr(d.Facets, "{}")},
		Workflow:      Workflow{Version: d.WorkflowVersion, Nodes: NodeStatuses(d)},
		Drafts:        newestFirst(rawOr(d.Drafts, "[]")),
		Tasks:         rawOr(d.Tasks, "[]"),
		Approvals:     rawOr(d.Approvals, "[]"),
		AgentRuns:     rawOr(d.AgentRuns, "[]"),
		History:       rawOr(d.History, "[]"),
		Permissions:   Permissions{CanReviewDrafts: canReview, CanNote: d.Actions.CanNote},
		Links:         map[string]string{"flowcase_case": flowcaseUI + "/cases/" + d.ID, "flowcase_approvals": flowcaseUI + "/approvals"},
		PartialErrors: []PartialError{},
		VersionNum:    d.VersionNum,
	}
	var m *packs.Manifest
	if p != nil && p.Valid() {
		m = p.Manifest
		ctx.Pack = PackBinding{PackKey: p.Key, Version: p.Version, Title: m.Title, Brand: m.Brand, Status: PackOK}
		for _, n := range m.Navigation {
			ctx.Navigation = append(ctx.Navigation, NavEntry{ID: n.Key, View: n.View, Label: n.Label, ArtifactType: n.ArtifactType, Icon: n.Icon,
				RequiredPermission: n.RequiredPermission})
		}
		ctx.ArtifactTypes = m.ArtifactTypes
	} else {
		ctx.Pack = PackBinding{Status: PackUnavailable}
		ctx.Navigation = append([]NavEntry{}, genericShell...)
		ctx.ArtifactTypes = []packs.ArtifactType{}
	}
	if b != nil {
		ctx.Pack.PackKey, ctx.Pack.Version, ctx.Pack.Binding = b.PackKey, b.PackVersion, b.Binding
		if !b.BoundAt.IsZero() {
			t := b.BoundAt
			ctx.Pack.BoundAt = &t
		}
	}
	ctx.Artifacts = artifactViews(d.Artifacts, m)
	return ctx
}
