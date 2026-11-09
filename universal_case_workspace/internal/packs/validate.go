package packs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/elenio/case-workspace/contracts"
)

// Problem is one validation finding with a JSON pointer into the manifest.
type Problem struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Fixed vocabularies. A manifest may only reference these; it cannot extend them.
var (
	// PermissionVocabulary is the central action vocabulary a manifest may require (never grant).
	PermissionVocabulary = []string{"artifact:read", "artifact:edit", "agent:invoke", "task:request", "task:complete",
		"approval:decide", "case:note", "document:read"}
	// Executors are the allowlisted action executor categories.
	Executors = []string{"agent_invocation", "flowcase_action", "artifact_command", "registered_extension"}
	// FlowcaseCommands are the allowlisted flowcase_action command keys.
	FlowcaseCommands = []string{"request_review"}
	// ArtifactCommands are the allowlisted artifact_command keys (none in the MVP).
	ArtifactCommands = []string{}
	// ExtensionRegistry is the compiled allowlist of signed UI/server extensions (empty in the MVP).
	ExtensionRegistry = []string{}
	// SourceProviders are the artifact source providers the BFF implements.
	SourceProviders = []string{"flowcase"}
)

const schemaURL = "urn:ucw:workspace-definition"

type denyLoader struct{}

func (denyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference %q refused", url)
}

var compiledSchema = func() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(contracts.WorkspaceDefinitionSchema))
	if err != nil {
		panic("workspace definition schema: " + err.Error())
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(denyLoader{})
	if err := c.AddResource(schemaURL, doc); err != nil {
		panic(err)
	}
	s, err := c.Compile(schemaURL)
	if err != nil {
		panic("workspace definition schema: " + err.Error())
	}
	return s
}()

// ValidateSchema checks raw manifest JSON against the embedded JSON Schema.
func ValidateSchema(raw []byte) []Problem {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return []Problem{{Path: "", Code: "invalid_json", Message: "manifest is not valid JSON"}}
	}
	err = compiledSchema.Validate(inst)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []Problem{{Path: "", Code: "schema_error", Message: err.Error()}}
	}
	var out []Problem
	seen := map[string]bool{}
	for _, u := range ve.BasicOutput().Errors {
		if u.Error == nil {
			continue
		}
		msg := u.Error.String()
		if strings.HasPrefix(msg, "validation failed") || seen[u.InstanceLocation+msg] {
			continue
		}
		seen[u.InstanceLocation+msg] = true
		out = append(out, Problem{Path: u.InstanceLocation, Code: "schema", Message: msg})
	}
	if len(out) == 0 {
		out = append(out, Problem{Path: "", Code: "schema", Message: "manifest does not satisfy the workspace definition schema"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Hostile content: URLs, markup, script-like constructs and template syntax are never valid in any
// manifest string (labels, descriptions, greetings, options, icons ...).
var hostile = []struct {
	re   *regexp.Regexp
	code string
}{
	{regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://`), "url_not_allowed"},
	{regexp.MustCompile(`(?i)\b(javascript|vbscript|mailto):|\bdata:[a-z]+/|\bfile:/`), "url_not_allowed"},
	{regexp.MustCompile(`(?i)\bwww\.[a-z0-9-]+\.`), "url_not_allowed"},
	{regexp.MustCompile(`[<>]`), "markup_not_allowed"},
	{regexp.MustCompile("`"), "markup_not_allowed"},
	{regexp.MustCompile(`(?i)\bon[a-z]+\s*=`), "script_not_allowed"},
	{regexp.MustCompile(`(?i)\b(eval|function|import|require|settimeout|setinterval|alert|fetch)\s*\(`), "script_not_allowed"},
	{regexp.MustCompile(`=>|\{\{|\$\{|<%`), "script_not_allowed"},
	{regexp.MustCompile(`(?i)\b(select\s.+\sfrom|insert\s+into|drop\s+table|delete\s+from|update\s.+\sset)\b`), "sql_not_allowed"},
}

func walkStrings(v any, path string, fn func(path, s string)) {
	switch x := v.(type) {
	case string:
		fn(path, x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fn(path+"/"+k, k) // keys too: an attacker could hide content in an unexpected key
			walkStrings(x[k], path+"/"+k, fn)
		}
	case []any:
		for i, e := range x {
			walkStrings(e, fmt.Sprintf("%s/%d", path, i), fn)
		}
	}
}

// ValidateSemantic applies the checks JSON Schema cannot express. raw must already be valid JSON.
func ValidateSemantic(raw []byte, m *Manifest) []Problem {
	var out []Problem
	add := func(path, code, format string, args ...any) {
		out = append(out, Problem{Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
	}

	var generic any
	_ = json.Unmarshal(raw, &generic)
	walkStrings(generic, "", func(path, s string) {
		for _, h := range hostile {
			if h.re.MatchString(s) {
				add(path, h.code, "value contains content that is not allowed in a manifest (%s)", strings.ReplaceAll(h.code, "_", " "))
				return
			}
		}
	})

	types := map[string]*ArtifactType{}
	sources := map[string]string{}
	for i := range m.ArtifactTypes {
		t := &m.ArtifactTypes[i]
		p := fmt.Sprintf("/artifact_types/%d", i)
		if types[t.Key] != nil {
			add(p+"/key", "duplicate_key", "artifact type key %q is used twice", t.Key)
		}
		types[t.Key] = t
		fields := map[string]bool{}
		for j, f := range t.Fields {
			fp := fmt.Sprintf("%s/fields/%d", p, j)
			if fields[f.Key] {
				add(fp+"/key", "duplicate_key", "field key %q is used twice", f.Key)
			}
			fields[f.Key] = true
			if f.Widget == "select" && len(f.Options) == 0 {
				add(fp+"/options", "select_without_options", "select field %q needs options", f.Key)
			}
			if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
				add(fp, "invalid_range", "field %q has min > max", f.Key)
			}
		}
		if s := t.Source; s != nil {
			if !slices.Contains(SourceProviders, s.Provider) {
				add(p+"/source/provider", "unknown_provider", "artifact source provider %q is not implemented", s.Provider)
			}
			if other, dup := sources[s.FlowcaseArtifactType]; dup {
				add(p+"/source/flowcase_artifact_type", "duplicate_source", "Flowcase artifact type %q is already mapped to %q", s.FlowcaseArtifactType, other)
			}
			sources[s.FlowcaseArtifactType] = t.Key
		}
	}

	agents := map[string]bool{}
	for _, a := range m.Bindings.AgentKeys {
		agents[a] = true
	}

	nav := map[string]bool{}
	for i, n := range m.Navigation {
		p := fmt.Sprintf("/navigation/%d", i)
		if nav[n.Key] {
			add(p+"/key", "duplicate_key", "navigation key %q is used twice", n.Key)
		}
		nav[n.Key] = true
		switch {
		case n.View == "artifact_list" && n.ArtifactType == "":
			add(p+"/artifact_type", "missing_artifact_type", "artifact_list navigation needs an artifact_type")
		case n.ArtifactType != "" && types[n.ArtifactType] == nil:
			add(p+"/artifact_type", "unknown_artifact_type", "artifact type %q is not defined", n.ArtifactType)
		}
		if n.View == "extension" && n.ExtensionRef == "" {
			add(p+"/extension_ref", "missing_extension_ref", "extension navigation needs a registered extension_ref")
		}
		if n.ExtensionRef != "" && !slices.Contains(ExtensionRegistry, n.ExtensionRef) {
			add(p+"/extension_ref", "unknown_extension", "extension %q is not in the compiled extension registry", n.ExtensionRef)
		}
		if n.RequiredPermission != "" && !slices.Contains(PermissionVocabulary, n.RequiredPermission) {
			add(p+"/required_permission", "unknown_permission", "permission %q is not in the action vocabulary", n.RequiredPermission)
		}
	}

	actions := map[string]bool{}
	for i, a := range m.Actions {
		p := fmt.Sprintf("/actions/%d", i)
		if actions[a.Key] {
			add(p+"/key", "duplicate_key", "action key %q is used twice", a.Key)
		}
		actions[a.Key] = true
		if !slices.Contains(PermissionVocabulary, a.RequiredPermission) {
			add(p+"/required_permission", "unknown_permission", "permission %q is not in the action vocabulary", a.RequiredPermission)
		}
		if a.OutputArtifactType != "" && types[a.OutputArtifactType] == nil {
			add(p+"/output_artifact_type", "unknown_artifact_type", "artifact type %q is not defined", a.OutputArtifactType)
		}
		switch a.Executor {
		case "agent_invocation":
			if a.AgentKey == "" || !agents[a.AgentKey] {
				add(p+"/agent_key", "agent_not_bound", "agent %q is not listed in bindings.agent_keys", a.AgentKey)
			}
		case "flowcase_action":
			if !slices.Contains(FlowcaseCommands, a.CommandKey) {
				add(p+"/command_key", "unknown_command", "Flowcase command %q is not allowlisted", a.CommandKey)
			}
		case "artifact_command":
			if !slices.Contains(ArtifactCommands, a.CommandKey) {
				add(p+"/command_key", "unknown_command", "artifact command %q is not allowlisted", a.CommandKey)
			}
		case "registered_extension":
			if !slices.Contains(ExtensionRegistry, a.RegisteredExtensionRef) {
				add(p+"/registered_extension_ref", "unknown_extension", "extension %q is not in the compiled extension registry", a.RegisteredExtensionRef)
			}
		default:
			add(p+"/executor", "unknown_executor", "executor %q is not allowlisted", a.Executor)
		}
	}

	if in := m.Intake; in != nil {
		if !agents[in.AgentKey] {
			add("/intake/agent_key", "agent_not_bound", "intake agent %q is not listed in bindings.agent_keys", in.AgentKey)
		}
		keys, maps := map[string]bool{}, map[string]bool{}
		titles := 0
		for i, f := range in.Fields {
			p := fmt.Sprintf("/intake/fields/%d", i)
			if keys[f.Key] {
				add(p+"/key", "duplicate_key", "intake field key %q is used twice", f.Key)
			}
			keys[f.Key] = true
			if maps[f.MapsTo] {
				add(p+"/maps_to", "duplicate_mapping", "%q is mapped twice", f.MapsTo)
			}
			maps[f.MapsTo] = true
			if f.MapsTo == "title" {
				titles++
				if f.Widget != "text" || !f.Required {
					add(p, "invalid_title_field", "the field mapped to title must be a required text field")
				}
			}
		}
		if titles != 1 {
			add("/intake/fields", "title_mapping_required", "exactly one intake field must map to title")
		}
		if d := in.Documents; d != nil {
			ok := false
			for _, f := range in.Fields {
				ok = ok || (f.Key == d.TargetField && f.Widget == "textarea")
			}
			if !ok {
				add("/intake/documents/target_field", "invalid_document_field", "documents.target_field %q must be a textarea intake field", d.TargetField)
			}
			if !strings.Contains(d.Prompt, "{filename}") {
				add("/intake/documents/prompt", "invalid_document_prompt", "documents.prompt must contain {filename}")
			}
		}
	}
	return out
}

// Validate parses and fully validates a manifest.
func Validate(raw []byte) (*Manifest, []Problem, []Problem) {
	schemaProblems := ValidateSchema(raw)
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, append(schemaProblems, Problem{Code: "invalid_json", Message: "manifest cannot be decoded"}), nil
	}
	return &m, schemaProblems, ValidateSemantic(raw, &m)
}
