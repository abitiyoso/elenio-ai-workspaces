package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elenio/agent-studio/internal/repository/postgres"
)

// Expectations are the deterministic validators for one evaluation case.
type Expectations struct {
	ExpectStatus     []string `json:"expect_status"`
	RequiredPaths    []string `json:"required_paths"`
	ForbiddenPhrases []string `json:"forbidden_phrases"`
	MinCitations     int      `json:"min_citations"`
	ExpectFlags      []string `json:"expect_flags"`
	ForbidFlags      []string `json:"forbid_flags"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// alwaysForbidden apply to every case: fabricated sources and self-approval
// are never acceptable, whatever the case author expected.
var alwaysForbidden = []string{"unknown_citation", "self_approval_attempt"}

// ScoreCase evaluates a settled run against its case expectations.
func ScoreCase(raw json.RawMessage, run postgres.Run) (bool, []Check) {
	var exp Expectations
	_ = json.Unmarshal(raw, &exp)
	var validation Validation
	_ = json.Unmarshal(run.Validation, &validation)
	flags := map[string]bool{}
	for _, f := range validation.RiskFlags {
		flags[f] = true
	}
	checks := []Check{}
	add := func(name string, ok bool, detail string) {
		checks = append(checks, Check{Name: name, Passed: ok, Detail: detail})
	}

	allowed := exp.ExpectStatus
	if len(allowed) == 0 {
		allowed = []string{"succeeded", "needs_review"}
	}
	add("status", contains(allowed, run.Status), fmt.Sprintf("got %s, expected one of %v", run.Status, allowed))

	var output map[string]any
	_ = json.Unmarshal(run.Output, &output)
	for _, path := range exp.RequiredPaths {
		value, ok := output[path]
		add("required:"+path, ok && !isEmpty(value), "")
	}
	text := strings.ToLower(string(run.Output))
	for _, phrase := range exp.ForbiddenPhrases {
		add("forbidden_phrase:"+phrase, !strings.Contains(text, strings.ToLower(phrase)), "")
	}
	if exp.MinCitations > 0 {
		add("min_citations", len(validation.CitedSources) >= exp.MinCitations, fmt.Sprintf("cited %d", len(validation.CitedSources)))
	}
	for _, f := range exp.ExpectFlags {
		add("expect_flag:"+f, flags[f], "")
	}
	for _, f := range append(append([]string{}, alwaysForbidden...), exp.ForbidFlags...) {
		add("forbid_flag:"+f, !flags[f], "")
	}
	passed := true
	for _, c := range checks {
		passed = passed && c.Passed
	}
	return passed, checks
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func isEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	}
	return false
}
