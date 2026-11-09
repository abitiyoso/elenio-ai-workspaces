package definition

import (
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"
)

// ValidateData checks case variables or task output against the safe schema subset accepted by the
// validator (input_schema / form_schema). Paths are JSON pointers rooted at base.
func ValidateData(schema map[string]any, value map[string]any, base string) []Diagnostic {
	var out []Diagnostic
	validateObject(schema, value, base, &out)
	return out
}

func validateObject(schema map[string]any, value map[string]any, path string, out *[]Diagnostic) {
	props, _ := schema["properties"].(map[string]any)
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			name, _ := r.(string)
			if v, present := value[name]; !present || v == nil {
				*out = append(*out, Errorf(path+"/"+name, "", "required", "%s is required", name))
			}
		}
	}
	if extra, _ := schema["additionalProperties"].(bool); schema["additionalProperties"] != nil && !extra {
		for k := range value {
			if _, declared := props[k]; !declared {
				*out = append(*out, Errorf(path+"/"+k, "", "unknown_property", "%s is not an accepted field", k))
			}
		}
	}
	for name, raw := range props {
		p, _ := raw.(map[string]any)
		v, present := value[name]
		if !present || v == nil {
			continue
		}
		validateValue(p, v, path+"/"+name, out)
	}
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(n), 64)
		return f, err == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func validateValue(p map[string]any, v any, path string, out *[]Diagnostic) {
	fail := func(code, format string, args ...any) { *out = append(*out, Errorf(path, "", code, format, args...)) }
	switch p["type"] {
	case "string":
		s, ok := v.(string)
		if !ok {
			fail("type_mismatch", "must be a string")
			return
		}
		n := utf8.RuneCountInString(s)
		if max, ok := number(p["maxLength"]); ok && float64(n) > max {
			fail("too_long", "must be at most %v characters", max)
		}
		if min, ok := number(p["minLength"]); ok && float64(n) < min {
			fail("too_short", "must be at least %v characters", min)
		}
		switch p["format"] {
		case "date-time":
			if _, err := time.Parse(time.RFC3339, s); err != nil {
				fail("invalid_format", "must be an RFC 3339 date-time")
			}
		case "date":
			if _, err := time.Parse("2006-01-02", s); err != nil {
				fail("invalid_format", "must be a date (YYYY-MM-DD)")
			}
		case "email":
			if _, err := mail.ParseAddress(s); err != nil {
				fail("invalid_format", "must be an email address")
			}
		case "uri":
			if u, err := url.Parse(s); err != nil || u.Scheme == "" {
				fail("invalid_format", "must be an absolute URI")
			}
		}
	case "number", "integer":
		f, ok := number(v)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			fail("type_mismatch", "must be a number")
			return
		}
		if p["type"] == "integer" && f != math.Trunc(f) {
			fail("type_mismatch", "must be an integer")
		}
		if min, ok := number(p["minimum"]); ok && f < min {
			fail("too_small", "must be at least %v", min)
		}
		if max, ok := number(p["maximum"]); ok && f > max {
			fail("too_large", "must be at most %v", max)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			fail("type_mismatch", "must be true or false")
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			fail("type_mismatch", "must be an array")
			return
		}
		if len(list) > 500 {
			fail("too_large", "must have at most 500 items")
		}
		if items, ok := p["items"].(map[string]any); ok {
			for i, item := range list {
				validateValue(items, item, fmt.Sprintf("%s/%d", path, i), out)
			}
		}
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			fail("type_mismatch", "must be an object")
			return
		}
		validateObject(p, obj, path, out)
	}
	if enum, ok := p["enum"].([]any); ok && !slices.ContainsFunc(enum, func(e any) bool { eq, _ := equal(v, e); return eq }) {
		fail("not_allowed", "must be one of the allowed values")
	}
}
