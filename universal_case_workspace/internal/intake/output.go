package intake

// Ported from Flowcase internal/intake/output.go: Agent Studio result envelope unwrapping and turn validation.

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Source is a cited knowledge source as Agent Studio reports it (envelope "sources").
type Source struct {
	SourceID  string `json:"source_id"`
	VersionID string `json:"version_id,omitempty"`
	Title     string `json:"title"`
	Citation  string `json:"citation,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
}

// KBItem is a knowledge-base use case, capability or solution the agent matched the need to.
type KBItem struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// Unresolved is a topic the agent could not confirm from the knowledge base.
type Unresolved struct {
	Topic        string   `json:"topic"`
	Reason       string   `json:"reason"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// Message is one chat message. Assistant messages carry what that turn changed and cited.
type Message struct {
	Role          string       `json:"role"`
	Content       string       `json:"content"`
	At            time.Time    `json:"at"`
	Sources       []Source     `json:"sources,omitempty"`
	UpdatedFields []string     `json:"updated_fields,omitempty"`
	KBItems       []KBItem     `json:"kb_items,omitempty"`
	QuickReplies  []string     `json:"quick_replies,omitempty"`
	Unresolved    []Unresolved `json:"unresolved,omitempty"`
	// Document marks a user message sent for an uploaded document.
	Document *MessageDocument `json:"document,omitempty"`
}

// Turn is a validated intake agent result.
type Turn struct {
	Reply        string
	CaseUpdate   map[string]any
	KBStatus     string
	KBItems      []KBItem
	ModelReady   bool
	QuickReplies []string
	Unresolved   []Unresolved
	Sources      []Source
}

var (
	kbStatuses = []string{"covered", "partial", "not_covered", "unknown"}
	kbKinds    = []string{"use_case", "capability", "solution"}
)

const (
	maxReply        = 8000
	maxQuickReplies = 3
	maxQuickReply   = 200
	maxKBItems      = 20
	maxRefs         = 20
	maxUnresolved   = 10
	maxSources      = 20
	maxAllSources   = 100
)

func cleanRefs(in []string) []string {
	out := []string{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r != "" && len(r) <= 200 && !slices.Contains(out, r) && len(out) < maxRefs {
			out = append(out, r)
		}
	}
	return out
}

// ParseTurn unwraps Agent Studio's result envelope ({artifact_draft, review, model_mode, sources})
// and validates the chat output. Lists are capped, unknown enum values dropped, and only sources
// the turn actually cites are kept (all of them when the envelope names no citations).
func ParseTurn(raw json.RawMessage) (Turn, error) {
	var env struct {
		ArtifactDraft json.RawMessage `json:"artifact_draft"`
		Review        struct {
			Validation struct {
				CitedSources []string `json:"cited_sources"`
			} `json:"validation"`
		} `json:"review"`
		Sources []Source `json:"sources"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return Turn{}, err
	}
	body := env.ArtifactDraft
	if len(body) == 0 || string(body) == "null" {
		body = raw
	}
	var out struct {
		Reply        string         `json:"reply"`
		CaseUpdate   map[string]any `json:"case_update"`
		KBStatus     string         `json:"kb_status"`
		KBItems      []KBItem       `json:"kb_items"`
		Ready        bool           `json:"ready"`
		QuickReplies []string       `json:"quick_replies"`
		Unresolved   []Unresolved   `json:"unresolved"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&out); err != nil {
		return Turn{}, err
	}
	t := Turn{Reply: truncateRunes(strings.TrimSpace(out.Reply), maxReply), CaseUpdate: out.CaseUpdate, KBStatus: out.KBStatus, ModelReady: out.Ready}
	if t.Reply == "" {
		return Turn{}, errors.New("empty reply")
	}
	if !slices.Contains(kbStatuses, t.KBStatus) {
		t.KBStatus = "unknown"
	}
	cited := map[string]bool{}
	for _, id := range env.Review.Validation.CitedSources {
		cited[id] = true
	}
	for _, it := range out.KBItems {
		name := truncateRunes(strings.TrimSpace(it.Name), 200)
		if name == "" || !slices.Contains(kbKinds, it.Kind) || len(t.KBItems) == maxKBItems {
			continue
		}
		refs := cleanRefs(it.EvidenceRefs)
		for _, r := range refs {
			cited[r] = true
		}
		t.KBItems = append(t.KBItems, KBItem{Name: name, Kind: it.Kind, EvidenceRefs: refs})
	}
	for _, q := range out.QuickReplies {
		q = truncateRunes(strings.TrimSpace(q), maxQuickReply)
		if q != "" && !slices.Contains(t.QuickReplies, q) && len(t.QuickReplies) < maxQuickReplies {
			t.QuickReplies = append(t.QuickReplies, q)
		}
	}
	for _, u := range out.Unresolved {
		topic := truncateRunes(strings.TrimSpace(u.Topic), 200)
		if topic == "" || len(t.Unresolved) == maxUnresolved {
			continue
		}
		refs := cleanRefs(u.EvidenceRefs)
		for _, r := range refs {
			cited[r] = true
		}
		t.Unresolved = append(t.Unresolved, Unresolved{Topic: topic, Reason: truncateRunes(strings.TrimSpace(u.Reason), 500), EvidenceRefs: refs})
	}
	seen := map[string]bool{}
	for _, s := range env.Sources {
		s.SourceID, s.Title = strings.TrimSpace(s.SourceID), strings.TrimSpace(s.Title)
		if s.SourceID == "" || s.Title == "" || seen[s.SourceID] || (len(cited) > 0 && !cited[s.SourceID]) || len(t.Sources) == maxSources {
			continue
		}
		seen[s.SourceID] = true
		t.Sources = append(t.Sources, Source{SourceID: truncateRunes(s.SourceID, 200), VersionID: truncateRunes(s.VersionID, 200),
			Title: truncateRunes(s.Title, 300), Citation: truncateRunes(s.Citation, 500), Anchor: truncateRunes(s.Anchor, 200)})
	}
	return t, nil
}

// mergeSources adds the turn's sources to the session's, newest last, keeping at most 100.
func mergeSources(all, add []Source) []Source {
	for _, s := range add {
		all = slices.DeleteFunc(all, func(x Source) bool { return x.SourceID == s.SourceID })
		all = append(all, s)
	}
	if len(all) > maxAllSources {
		all = all[len(all)-maxAllSources:]
	}
	return all
}
