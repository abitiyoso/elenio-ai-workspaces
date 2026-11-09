package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validation is persisted with every run and returned to Flowcase. Risk flags
// move a run to needs_review; they never block a human from seeing the draft.
type Validation struct {
	SchemaValid      bool     `json:"schema_valid"`
	SchemaErrors     []string `json:"schema_errors,omitempty"`
	RiskFlags        []string `json:"risk_flags"`
	CitedSources     []string `json:"cited_sources"`
	UnknownCitations []string `json:"unknown_citations,omitempty"`
	UnsupportedPaths []string `json:"unsupported_claim_paths,omitempty"`
	RawOutputPreview string   `json:"raw_output_preview,omitempty"`
}

var (
	claimPattern     = regexp.MustCompile(`(?i)(\b\d{1,3}(\.\d+)?\s?%|\bguarantee[sd]?\b|\bcertified\b|\bzero (downtime|risk|defects?)\b|\b100 ?percent\b)`)
	evidenceKeys     = map[string]bool{"evidence_refs": true, "source_ids": true, "citations": true, "evidence": true, "sources": true}
	approvalKeys     = map[string]bool{"approved": true, "approval": true, "approval_status": true, "gate_decision": true, "decision": true, "signed_off": true}
	approvalValueRex = regexp.MustCompile(`(?i)^(approved|approve|signed[ _-]?off|released|accepted|go)$`)
)

type denyLoader struct{}

func (denyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference %q is not allowed", url)
}

// CompileSchema compiles a tenant-authored JSON Schema with every external
// reference (file or network) refused.
func CompileSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(denyLoader{})
	if err := compiler.AddResource("urn:agent-studio:output", doc); err != nil {
		return nil, err
	}
	return compiler.Compile("urn:agent-studio:output")
}

// ExtractJSONObject accepts a bare object or one fenced code block.
func ExtractJSONObject(content string) (json.RawMessage, error) {
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		if i := strings.LastIndex(text, "```"); i >= 0 {
			text = text[:i]
		}
		text = strings.TrimSpace(text)
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, errors.New("no JSON object in model output")
	}
	candidate := json.RawMessage(text[start : end+1])
	var probe map[string]any
	if err := json.Unmarshal(candidate, &probe); err != nil {
		return nil, err
	}
	return candidate, nil
}

// ValidateOutput checks the schema and the evidence rules: cited ids must be
// sources this run actually saw, material claims need a citation, cited
// unverified sources need review, and no field may assert an approval.
func ValidateOutput(outputSchema, output json.RawMessage, seen map[string]Evidence, extraFlags []string) Validation {
	v := Validation{SchemaValid: true, RiskFlags: []string{}, CitedSources: []string{}}
	if len(outputSchema) > 2 {
		compiled, err := CompileSchema(outputSchema)
		if err != nil {
			v.SchemaValid = false
			v.SchemaErrors = []string{"output schema could not be compiled"}
		} else if instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(output)); err != nil {
			v.SchemaValid = false
			v.SchemaErrors = []string{"output is not valid JSON"}
		} else if err := compiled.Validate(instance); err != nil {
			v.SchemaValid = false
			v.SchemaErrors = []string{truncateText(err.Error(), 1000)}
		}
	}
	var decoded any
	_ = json.Unmarshal(output, &decoded)
	flags := map[string]bool{}
	for _, f := range extraFlags {
		flags[f] = true
	}
	cited := map[string]bool{}
	walkOutput(decoded, "$", func(path string, obj map[string]any) {
		_, hasEvidence := obj["source_id"].(string)
		for key, value := range obj {
			if evidenceKeys[key] {
				ids := citationIDs(value)
				if len(ids) > 0 {
					hasEvidence = true
				}
				for _, id := range ids {
					cited[id] = true
				}
			}
			if approvalKeys[strings.ToLower(key)] {
				switch typed := value.(type) {
				case bool:
					if typed {
						flags["self_approval_attempt"] = true
					}
				case string:
					if approvalValueRex.MatchString(strings.TrimSpace(typed)) {
						flags["self_approval_attempt"] = true
					}
				}
			}
		}
		if hasEvidence {
			return
		}
		for key, value := range obj {
			if text, ok := value.(string); ok && !evidenceKeys[key] && claimPattern.MatchString(text) {
				flags["unsupported_claim"] = true
				v.UnsupportedPaths = append(v.UnsupportedPaths, path+"."+key)
			}
		}
	})
	for id := range cited {
		v.CitedSources = append(v.CitedSources, id)
		evidence, ok := seen[id]
		if !ok {
			v.UnknownCitations = append(v.UnknownCitations, id)
			flags["unknown_citation"] = true
			continue
		}
		if evidence.TrustStatus != "approved" {
			flags["cites_unverified_evidence"] = true
		}
	}
	sort.Strings(v.CitedSources)
	sort.Strings(v.UnknownCitations)
	sort.Strings(v.UnsupportedPaths)
	for f := range flags {
		v.RiskFlags = append(v.RiskFlags, f)
	}
	sort.Strings(v.RiskFlags)
	return v
}

func walkOutput(value any, path string, visit func(string, map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(path, typed)
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkOutput(typed[k], path+"."+k, visit)
		}
	case []any:
		for i, item := range typed {
			walkOutput(item, fmt.Sprintf("%s[%d]", path, i), visit)
		}
	}
}

func citationIDs(value any) []string {
	ids := []string{}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			ids = append(ids, strings.TrimSpace(typed))
		}
	case []any:
		for _, item := range typed {
			ids = append(ids, citationIDs(item)...)
		}
	case map[string]any:
		if id, ok := typed["source_id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
