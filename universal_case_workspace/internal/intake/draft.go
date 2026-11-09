// Package intake is the pre-case intake chat that UCW owns: the user chats with the pack's intake agent
// (one Agent Studio run per turn, KMS searched as the user through delegated_user) to fill a case brief
// whose fields come from the pinned pack's intake block. The case is created in Flowcase, as the user,
// only when the user asks. Ported from Flowcase internal/intake, with pack-driven fields.
package intake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elenio/case-workspace/internal/apierr"
	"github.com/elenio/case-workspace/internal/flowcase"
	"github.com/elenio/case-workspace/internal/packs"
)

// Draft is the case brief: field key -> string (text, textarea, date as YYYY-MM-DD) or []string (list).
// Keys without a value are absent. encoding/json sorts map keys, so a draft always marshals the same way.
type Draft map[string]any

const (
	maxListItems = 50
	maxListItem  = 500
)

func limit(f packs.IntakeField) int {
	if f.MaxLength > 0 {
		if f.Widget == "date" {
			return 10
		}
		return f.MaxLength
	}
	switch f.Widget {
	case "textarea":
		return 4000
	case "date":
		return 10
	case "list":
		return maxListItem
	}
	return 300
}

func invalid(path, format string, args ...any) *apierr.Error {
	msg := fmt.Sprintf(format, args...)
	return &apierr.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_draft", Message: msg,
		Details: []apierr.Detail{{Code: "invalid_draft", Message: msg, Path: path}}}
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// normalizeDate accepts YYYY-MM-DD (or an RFC 3339 timestamp, reduced to its date).
func normalizeDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.Format(time.DateOnly), true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.DateOnly), true
	}
	return "", false
}

func cleanList(in []string, max int, truncate bool, path string) ([]string, error) {
	var out []string
	for i, c := range in {
		c = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c), "- "))
		if c == "" {
			continue
		}
		if utf8.RuneCountInString(c) > max {
			if !truncate {
				return nil, invalid(fmt.Sprintf("%s[%d]", path, i), "each item may have at most %d characters", max)
			}
			c = truncateRunes(c, max)
		}
		if slices.Contains(out, c) {
			continue
		}
		if len(out) == maxListItems {
			if !truncate {
				return nil, invalid(path, "at most %d items", maxListItems)
			}
			break
		}
		out = append(out, c)
	}
	return out, nil
}

func strings_(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func field(fields []packs.IntakeField, key string) (packs.IntakeField, bool) {
	for _, f := range fields {
		if f.Key == key {
			return f, true
		}
	}
	return packs.IntakeField{}, false
}

// Clean validates a draft a person edited (the brief form): only the pack's fields, strings (lists of
// strings for list fields), trimmed, within length limits, dates as real dates.
func Clean(fields []packs.IntakeField, in map[string]any) (Draft, error) {
	for k := range in {
		if _, ok := field(fields, k); !ok {
			return nil, invalid("draft."+k, "%q is not a field of this intake", k)
		}
	}
	out := Draft{}
	for _, f := range fields {
		v, present := in[f.Key]
		if !present || v == nil {
			continue
		}
		path := "draft." + f.Key
		if f.Widget == "list" {
			list, ok := strings_(v)
			if !ok {
				return nil, invalid(path, "%s must be a list of texts", strings.ToLower(f.Label))
			}
			c, err := cleanList(list, limit(f), false, path)
			if err != nil {
				return nil, err
			}
			if len(c) > 0 {
				out[f.Key] = c
			}
			continue
		}
		s, ok := v.(string)
		if !ok {
			return nil, invalid(path, "%s must be text", strings.ToLower(f.Label))
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if f.Widget == "date" {
			d, ok := normalizeDate(s)
			if !ok {
				return nil, invalid(path, "%s must be a date (YYYY-MM-DD)", strings.ToLower(f.Label))
			}
			s = d
		}
		if utf8.RuneCountInString(s) > limit(f) {
			return nil, invalid(path, "%s may have at most %d characters", strings.ToLower(f.Label), limit(f))
		}
		out[f.Key] = s
	}
	return out, nil
}

// Merge applies an agent's case_update: keys that are not intake fields and values of the wrong type
// are ignored, empty values never erase, long text is truncated, an unparseable date is dropped and a
// list is replaced only by a non-empty list; locked keys are never changed. It returns the keys that
// changed, in field order.
func (d Draft) Merge(fields []packs.IntakeField, update map[string]any, locked ...string) []string {
	var changed []string
	for _, f := range fields {
		v, ok := update[f.Key]
		if !ok || slices.Contains(locked, f.Key) {
			continue
		}
		if f.Widget == "list" {
			list, ok := strings_(v)
			if !ok {
				continue
			}
			c, _ := cleanList(list, limit(f), true, "")
			cur, _ := strings_(d[f.Key])
			if len(c) > 0 && !slices.Equal(c, cur) {
				d[f.Key] = c
				changed = append(changed, f.Key)
			}
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if f.Widget == "date" {
			if s, ok = normalizeDate(s); !ok {
				continue
			}
		}
		s = truncateRunes(s, limit(f))
		if cur, _ := d[f.Key].(string); cur != s {
			d[f.Key] = s
			changed = append(changed, f.Key)
		}
	}
	return changed
}

// Value renders a field's value (lists joined by "; ") and reports whether it has one.
func (d Draft) Value(f packs.IntakeField) (string, bool) {
	if f.Widget == "list" {
		l, _ := strings_(d[f.Key])
		return strings.Join(l, "; "), len(l) > 0
	}
	s, _ := d[f.Key].(string)
	return s, s != ""
}

// Missing lists the labels of required fields without a value.
func Missing(fields []packs.IntakeField, d Draft) []string {
	out := []string{}
	for _, f := range fields {
		if _, ok := d.Value(f); f.Required && !ok {
			out = append(out, f.Label)
		}
	}
	return out
}

// Ready: the model says ready, nothing required is missing and the knowledge base covers the need.
func Ready(modelReady bool, missing []string, kbStatus string) bool {
	return modelReady && len(missing) == 0 && (kbStatus == "covered" || kbStatus == "partial")
}

// ToCreateInput builds the Flowcase case from the draft through each field's maps_to: "title" or
// "variables.<name>"; date fields become RFC 3339 midnight UTC; version 0 = latest published.
func ToCreateInput(fields []packs.IntakeField, d Draft, workflowKey string) (flowcase.CreateInput, error) {
	in := flowcase.CreateInput{WorkflowKey: workflowKey, Version: 0, Variables: map[string]any{}}
	for _, f := range fields {
		if _, ok := d.Value(f); !ok {
			continue
		}
		var v any = d[f.Key]
		if f.Widget == "date" {
			date, ok := normalizeDate(d[f.Key].(string))
			if !ok {
				return in, invalid("draft."+f.Key, "%s must be a date (YYYY-MM-DD)", strings.ToLower(f.Label))
			}
			v = date + "T00:00:00Z"
		}
		if f.Widget == "list" {
			l, _ := strings_(d[f.Key])
			v = l
		}
		switch {
		case f.MapsTo == "title":
			in.Title = v.(string)
		case strings.HasPrefix(f.MapsTo, "variables."):
			in.Variables[strings.TrimPrefix(f.MapsTo, "variables.")] = v
		}
	}
	if strings.TrimSpace(in.Title) == "" {
		return in, invalid("draft", "the case title is required")
	}
	return in, nil
}

// --- agent input ------------------------------------------------------------------------------------

const (
	maxConversation  = 20
	maxContent       = 4000
	maxSummary       = 4000
	maxSummaryValues = 600
)

type turnMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AgentInput is the intake agent's input contract {case_summary, conversation[], draft}.
type AgentInput struct {
	CaseSummary  string        `json:"case_summary"`
	Conversation []turnMessage `json:"conversation"`
	Draft        Draft         `json:"draft"`
}

// CaseSummary renders the target workflow, the current draft and what is still missing.
func CaseSummary(packTitle, workflowKey string, fields []packs.IntakeField, d Draft, missing []string) string {
	var b strings.Builder
	if packTitle != "" {
		fmt.Fprintf(&b, "Workflow: %s (%s)\n", packTitle, workflowKey)
	} else {
		fmt.Fprintf(&b, "Workflow: %s\n", workflowKey)
	}
	b.WriteString("Current draft:\n")
	empty := true
	for _, f := range fields {
		if v, ok := d.Value(f); ok {
			fmt.Fprintf(&b, "- %s: %s\n", f.Key, truncateRunes(strings.ReplaceAll(v, "\n", " "), maxSummaryValues))
			empty = false
		}
	}
	if empty {
		b.WriteString("- (empty)\n")
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "Still missing: %s\n", strings.Join(missing, ", "))
	} else {
		b.WriteString("Still missing: nothing\n")
	}
	return truncateRunes(strings.TrimSpace(b.String()), maxSummary)
}

// BuildInput assembles the run input: summary, the latest 20 user/assistant messages (each at most
// 4000 characters, the last one the user's newest message) and the draft.
func BuildInput(packTitle, workflowKey string, fields []packs.IntakeField, d Draft, missing []string, messages []Message) (json.RawMessage, error) {
	conv := make([]turnMessage, 0, maxConversation)
	for _, m := range messages {
		if (m.Role != RoleUser && m.Role != RoleAssistant) || strings.TrimSpace(m.Content) == "" {
			continue
		}
		conv = append(conv, turnMessage{Role: m.Role, Content: truncateRunes(m.Content, maxContent)})
	}
	if len(conv) == 0 || conv[len(conv)-1].Role != RoleUser {
		return nil, fmt.Errorf("conversation must end with a user message")
	}
	if len(conv) > maxConversation {
		conv = conv[len(conv)-maxConversation:]
	}
	if d == nil {
		d = Draft{}
	}
	return json.Marshal(AgentInput{CaseSummary: CaseSummary(packTitle, workflowKey, fields, d, missing), Conversation: conv, Draft: d})
}
