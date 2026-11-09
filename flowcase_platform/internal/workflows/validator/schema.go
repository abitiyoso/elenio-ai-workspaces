package validator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/elenio/flowcase/internal/workflows/definition"
)

// Structural checks mirroring contracts/WORKFLOW_DEFINITION_SCHEMA.json, with JSON-pointer paths.
// The bundled JSON Schema is conceptual; semantic.go adds the cross-node rules it cannot express.

var (
	keyPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	nodeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	refPattern    = regexp.MustCompile(`^[a-z][a-z0-9]*([.-][a-z0-9]+){0,10}$`)
	propPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

const (
	maxDocumentBytes = 256 << 10
	maxNodes         = 200
)

var topLevelKeys = []string{"schema_version", "key", "name", "version", "status", "description", "tenant_scope", "input_schema", "policy_refs", "metadata", "nodes"}
var nodeKeys = []string{"id", "type", "name", "config", "next"}

type schemaChecker struct{ diags []definition.Diagnostic }

func (c *schemaChecker) fail(path, code, format string, args ...any) {
	c.diags = append(c.diags, definition.Errorf(path, "", code, format, args...))
}

func checkSchema(doc any) []definition.Diagnostic {
	c := &schemaChecker{}
	root, ok := doc.(map[string]any)
	if !ok {
		c.fail("", "invalid_type", "definition must be a JSON object")
		return c.diags
	}
	c.unknownKeys(root, "", topLevelKeys)
	for _, k := range []string{"schema_version", "key", "name", "version", "nodes"} {
		if _, ok := root[k]; !ok {
			c.fail("/"+k, "required", "%s is required", k)
		}
	}
	if v, ok := root["schema_version"]; ok && v != definition.SchemaVersion {
		c.fail("/schema_version", "unsupported_schema_version", "schema_version must be %q", definition.SchemaVersion)
	}
	if v, ok := root["key"]; ok {
		if s, isStr := v.(string); !isStr || !keyPattern.MatchString(s) {
			c.fail("/key", "invalid_key", "key must be lowercase letters, digits and hyphens, starting with a letter (max 63)")
		}
	}
	if v, ok := root["name"]; ok {
		c.stringLen(v, "/name", 3, 200)
	}
	if v, ok := root["version"]; ok {
		if n, isNum := v.(json.Number); !isNum || !positiveInt(n) {
			c.fail("/version", "invalid_type", "version must be an integer >= 1")
		}
	}
	if v, ok := root["status"]; ok && !slices.Contains([]any{"draft", "validated", "published", "deprecated"}, v) {
		c.fail("/status", "invalid_enum", "status must be draft, validated, published or deprecated (it is server-managed and ignored)")
	}
	if v, ok := root["description"]; ok {
		c.stringLen(v, "/description", 0, 2000)
	}
	if v, ok := root["tenant_scope"]; ok && v != "organization" && v != "global" {
		c.fail("/tenant_scope", "invalid_enum", "tenant_scope must be organization or global")
	}
	if v, ok := root["input_schema"]; ok {
		c.dataSchema(v, "/input_schema", 1)
	}
	if v, ok := root["policy_refs"]; ok {
		list, isList := v.([]any)
		if !isList {
			c.fail("/policy_refs", "invalid_type", "policy_refs must be an array of strings")
		}
		seen := map[string]bool{}
		for i, item := range list {
			s, isStr := item.(string)
			p := fmt.Sprintf("/policy_refs/%d", i)
			switch {
			case !isStr || !refPattern.MatchString(s):
				c.fail(p, "invalid_ref", "policy ref must look like domain.policy-name")
			case seen[s]:
				c.fail(p, "duplicate", "policy ref %q is listed twice", s)
			}
			seen[s] = true
		}
	}
	if v, ok := root["metadata"]; ok {
		if _, isObj := v.(map[string]any); !isObj {
			c.fail("/metadata", "invalid_type", "metadata must be an object")
		}
	}
	if v, ok := root["nodes"]; ok {
		list, isList := v.([]any)
		switch {
		case !isList:
			c.fail("/nodes", "invalid_type", "nodes must be an array")
		case len(list) < 2:
			c.fail("/nodes", "too_few_nodes", "a workflow needs at least a start and an end node")
		case len(list) > maxNodes:
			c.fail("/nodes", "too_many_nodes", "a workflow may have at most %d nodes", maxNodes)
		}
		for i, item := range list {
			c.node(item, fmt.Sprintf("/nodes/%d", i))
		}
	}
	return c.diags
}

func (c *schemaChecker) node(v any, path string) {
	n, ok := v.(map[string]any)
	if !ok {
		c.fail(path, "invalid_type", "node must be an object")
		return
	}
	c.unknownKeys(n, path, nodeKeys)
	for _, k := range []string{"id", "type", "name", "next"} {
		if _, ok := n[k]; !ok {
			c.fail(path+"/"+k, "required", "node %s is required", k)
		}
	}
	if v, ok := n["id"]; ok {
		if s, isStr := v.(string); !isStr || !nodeIDPattern.MatchString(s) {
			c.fail(path+"/id", "invalid_node_id", "node id must be lowercase letters, digits, _ or -, starting with a letter")
		}
	}
	if v, ok := n["type"]; ok {
		s, _ := v.(string)
		if !slices.Contains(definition.NodeTypes, definition.NodeType(s)) {
			c.fail(path+"/type", "unsupported_node_type", "node type %q is not supported", s)
		}
	}
	if v, ok := n["name"]; ok {
		c.stringLen(v, path+"/name", 1, 200)
	}
	if v, ok := n["config"]; ok {
		if _, isObj := v.(map[string]any); !isObj {
			c.fail(path+"/config", "invalid_type", "config must be an object")
		}
	}
	if v, ok := n["next"]; ok {
		list, isList := v.([]any)
		if !isList {
			c.fail(path+"/next", "invalid_type", "next must be an array of node ids")
			return
		}
		seen := map[string]bool{}
		for i, t := range list {
			s, isStr := t.(string)
			p := fmt.Sprintf("%s/next/%d", path, i)
			if !isStr {
				c.fail(p, "invalid_type", "next entries must be node ids")
				continue
			}
			if seen[s] {
				c.fail(p, "duplicate", "next lists %q twice", s)
			}
			seen[s] = true
		}
	}
}

func (c *schemaChecker) unknownKeys(obj map[string]any, path string, allowed []string) {
	for k := range obj {
		if !slices.Contains(allowed, k) {
			c.fail(path+"/"+k, "unknown_property", "property %q is not part of the definition schema", k)
		}
	}
}

func (c *schemaChecker) stringLen(v any, path string, min, max int) {
	s, ok := v.(string)
	if !ok || len([]rune(s)) < min || len([]rune(s)) > max {
		c.fail(path, "invalid_string", "must be a string of %d to %d characters", min, max)
	}
}

var (
	dataTypes   = []any{"string", "number", "integer", "boolean", "array", "object"}
	dataFormats = []any{"date-time", "date", "email", "uri"}
	objectKeys  = []string{"type", "title", "description", "properties", "required", "additionalProperties"}
	propKeys    = []string{"type", "title", "description", "format", "enum", "items", "minimum", "maximum", "minLength", "maxLength", "properties", "required", "additionalProperties", "x-hidden"}
)

// dataSchema accepts a small, safe JSON Schema subset for case input and task forms: no $ref,
// no remote schemas, no patterns (avoids ReDoS), bounded nesting.
func (c *schemaChecker) dataSchema(v any, path string, depth int) {
	obj, ok := v.(map[string]any)
	if !ok || obj["type"] != "object" {
		c.fail(path, "invalid_data_schema", "schema must be an object with type \"object\"")
		return
	}
	if depth > 3 {
		c.fail(path, "invalid_data_schema", "schemas may nest at most 3 objects deep")
		return
	}
	c.schemaKeys(obj, path, objectKeys)
	props, isObj := obj["properties"].(map[string]any)
	if _, present := obj["properties"]; present && !isObj {
		c.fail(path+"/properties", "invalid_data_schema", "properties must be an object")
	}
	for name, raw := range props {
		pp := path + "/properties/" + name
		if !propPattern.MatchString(name) {
			c.fail(pp, "invalid_data_schema", "property names must be lowercase snake_case")
		}
		c.property(raw, pp, depth)
	}
	if r, ok := obj["required"]; ok {
		list, isList := r.([]any)
		if !isList {
			c.fail(path+"/required", "invalid_data_schema", "required must be an array")
		}
		for i, item := range list {
			s, _ := item.(string)
			if _, declared := props[s]; !declared {
				c.fail(fmt.Sprintf("%s/required/%d", path, i), "invalid_data_schema", "required property %q is not declared", s)
			} else if p, _ := props[s].(map[string]any); p["x-hidden"] == true {
				c.fail(fmt.Sprintf("%s/required/%d", path, i), "invalid_data_schema", "hidden property %q cannot be required (forms never show it)", s)
			}
		}
	}
	if a, ok := obj["additionalProperties"]; ok {
		if _, isBool := a.(bool); !isBool {
			c.fail(path+"/additionalProperties", "invalid_data_schema", "additionalProperties must be a boolean")
		}
	}
}

func (c *schemaChecker) property(v any, path string, depth int) {
	p, ok := v.(map[string]any)
	if !ok {
		c.fail(path, "invalid_data_schema", "property schema must be an object")
		return
	}
	if !slices.Contains(dataTypes, p["type"]) {
		c.fail(path+"/type", "invalid_data_schema", "type must be one of string, number, integer, boolean, array, object")
		return
	}
	if p["type"] == "object" {
		c.dataSchema(p, path, depth+1)
		return
	}
	c.schemaKeys(p, path, propKeys)
	// x-hidden keeps an optional field accepted (e.g. for a future integration) without forms showing it.
	if h, ok := p["x-hidden"]; ok {
		if _, isBool := h.(bool); !isBool {
			c.fail(path+"/x-hidden", "invalid_data_schema", "x-hidden must be a boolean")
		}
	}
	if f, ok := p["format"]; ok && !slices.Contains(dataFormats, f) {
		c.fail(path+"/format", "invalid_data_schema", "format must be date-time, date, email or uri")
	}
	if e, ok := p["enum"]; ok {
		list, isList := e.([]any)
		if !isList || len(list) == 0 || len(list) > 100 {
			c.fail(path+"/enum", "invalid_data_schema", "enum must list 1 to 100 values")
		}
	}
	if p["type"] == "array" {
		items, ok := p["items"].(map[string]any)
		if !ok {
			c.fail(path+"/items", "invalid_data_schema", "array properties need an items schema")
		} else if items["type"] == "array" {
			c.fail(path+"/items", "invalid_data_schema", "nested arrays are not supported")
		} else {
			c.property(items, path+"/items", depth)
		}
	}
}

func (c *schemaChecker) schemaKeys(obj map[string]any, path string, allowed []string) {
	for k := range obj {
		if !slices.Contains(allowed, k) {
			c.fail(path+"/"+k, "invalid_data_schema", "schema keyword %q is not supported", k)
		}
	}
}

func positiveInt(n json.Number) bool {
	i, err := n.Int64()
	return err == nil && i >= 1 && i <= 1_000_000
}

// propertyType returns the declared JSON type of a top-level input property, or "".
func propertyType(schema map[string]any, name string) string {
	props, _ := schema["properties"].(map[string]any)
	p, _ := props[name].(map[string]any)
	t, _ := p["type"].(string)
	return t
}
