package definition

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Condition expressions are structured JSON objects over an allowlisted operator set. Strings are
// never parsed or evaluated as code, and there is no access to anything but case variables.
//
//	{"operator":"gt","field":"estimated_cost","value":0}
//	{"operator":"in","field":"region","value":["id","sg"]}
//	{"operator":"exists","field":"customer_id"}
//	{"operator":"and","conditions":[...]}   {"operator":"or","conditions":[...]}
//	{"operator":"not","condition":{...}}

const (
	MaxExprDepth = 5
	MaxExprNodes = 50
	maxInValues  = 50
	maxStringLen = 256
)

var FieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}(\.[a-z][a-z0-9_]{0,63}){0,4}$`)

var (
	comparisonOps = []string{"eq", "ne", "gt", "gte", "lt", "lte"}
	setOps        = []string{"in", "not_in"}
	logicalOps    = []string{"and", "or"}
	allOps        = slices.Concat(comparisonOps, setOps, logicalOps, []string{"not", "exists"})
)

type Expr struct {
	Op    string
	Field string
	Value any // scalar, or []any for in/not_in
	Args  []Expr
}

// ParseExpr validates raw (decoded with UseNumber) and returns the expression with diagnostics.
func ParseExpr(raw any, path, nodeID string) (Expr, []Diagnostic) {
	p := exprParser{path: path, nodeID: nodeID}
	e := p.parse(raw, path, 1)
	return e, p.diags
}

type exprParser struct {
	path, nodeID string
	count        int
	diags        []Diagnostic
}

func (p *exprParser) fail(path, code, format string, args ...any) {
	p.diags = append(p.diags, Errorf(path, p.nodeID, code, format, args...))
}

func (p *exprParser) parse(raw any, path string, depth int) Expr {
	p.count++
	if p.count == MaxExprNodes+1 {
		p.fail(p.path, "expression_too_large", "expression has more than %d operators", MaxExprNodes)
	}
	if depth > MaxExprDepth {
		p.fail(path, "expression_too_deep", "expression nesting exceeds %d levels", MaxExprDepth)
		return Expr{}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		if _, isString := raw.(string); isString {
			p.fail(path, "unsafe_expression", "string expressions are not evaluated; use a structured {operator, field, value} object")
		} else {
			p.fail(path, "invalid_expression", "expression must be an object")
		}
		return Expr{}
	}
	op, _ := obj["operator"].(string)
	if !slices.Contains(allOps, op) {
		p.fail(path+"/operator", "unsafe_expression", "operator %q is not allowed; use one of %s", op, strings.Join(allOps, ", "))
		return Expr{}
	}
	e := Expr{Op: op}
	var allowed []string
	switch {
	case slices.Contains(comparisonOps, op):
		allowed = []string{"operator", "field", "value"}
		e.Field = p.field(obj, path)
		v, ok := obj["value"]
		if !ok {
			p.fail(path+"/value", "invalid_expression", "%s requires a value", op)
		} else if !isScalar(v) {
			p.fail(path+"/value", "invalid_expression", "%s value must be a string, number, boolean or null", op)
		} else if op != "eq" && op != "ne" {
			if _, isNum := v.(json.Number); !isNum {
				p.fail(path+"/value", "invalid_expression", "%s compares numbers; value must be a number", op)
			}
		}
		if s, isStr := v.(string); isStr && len(s) > maxStringLen {
			p.fail(path+"/value", "invalid_expression", "string value exceeds %d characters", maxStringLen)
		}
		e.Value = v
	case slices.Contains(setOps, op):
		allowed = []string{"operator", "field", "value"}
		e.Field = p.field(obj, path)
		list, ok := obj["value"].([]any)
		if !ok || len(list) == 0 || len(list) > maxInValues {
			p.fail(path+"/value", "invalid_expression", "%s requires a list of 1 to %d values", op, maxInValues)
		}
		for i, v := range list {
			if !isScalar(v) {
				p.fail(fmt.Sprintf("%s/value/%d", path, i), "invalid_expression", "%s values must be scalars", op)
			}
		}
		e.Value = list
	case op == "exists":
		allowed = []string{"operator", "field"}
		e.Field = p.field(obj, path)
	case slices.Contains(logicalOps, op):
		allowed = []string{"operator", "conditions"}
		list, ok := obj["conditions"].([]any)
		if !ok || len(list) < 2 || len(list) > 10 {
			p.fail(path+"/conditions", "invalid_expression", "%s requires 2 to 10 conditions", op)
		}
		for i, sub := range list {
			e.Args = append(e.Args, p.parse(sub, fmt.Sprintf("%s/conditions/%d", path, i), depth+1))
		}
	case op == "not":
		allowed = []string{"operator", "condition"}
		sub, ok := obj["condition"]
		if !ok {
			p.fail(path+"/condition", "invalid_expression", "not requires a condition")
		} else {
			e.Args = []Expr{p.parse(sub, path+"/condition", depth+1)}
		}
	}
	for k := range obj {
		if !slices.Contains(allowed, k) {
			p.fail(path+"/"+k, "unsafe_expression", "property %q is not allowed in a %s expression", k, op)
		}
	}
	return e
}

func (p *exprParser) field(obj map[string]any, path string) string {
	f, _ := obj["field"].(string)
	if !FieldPattern.MatchString(f) {
		p.fail(path+"/field", "invalid_expression", "field must be a case variable path such as estimated_cost or customer.tier")
	}
	return f
}

func isScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool, json.Number, float64:
		return true
	}
	return false
}

// Fields returns every variable path the expression reads.
func (e Expr) Fields() []string {
	var out []string
	if e.Field != "" {
		out = append(out, e.Field)
	}
	for _, a := range e.Args {
		out = append(out, a.Fields()...)
	}
	return out
}

// Eval evaluates deterministically against case variables. Missing fields make comparisons false
// (exists returns false); comparing incompatible types is an error so the engine can fail the node
// instead of silently routing.
func (e Expr) Eval(vars map[string]any) (bool, error) {
	switch e.Op {
	case "and":
		for _, a := range e.Args {
			ok, err := a.Eval(vars)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case "or":
		for _, a := range e.Args {
			ok, err := a.Eval(vars)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "not":
		ok, err := e.Args[0].Eval(vars)
		return !ok, err
	}
	v, present := lookup(vars, e.Field)
	switch e.Op {
	case "exists":
		return present && v != nil, nil
	case "in", "not_in":
		if !present {
			return e.Op == "not_in", nil
		}
		found := false
		for _, candidate := range e.Value.([]any) {
			eq, err := equal(v, candidate)
			if err != nil {
				return false, err
			}
			if eq {
				found = true
				break
			}
		}
		return found == (e.Op == "in"), nil
	case "eq", "ne":
		if !present {
			return e.Op == "ne", nil
		}
		eq, err := equal(v, e.Value)
		return eq == (e.Op == "eq"), err
	}
	if !present || v == nil {
		return false, nil
	}
	a, okA := toFloat(v)
	b, okB := toFloat(e.Value)
	if !okA || !okB {
		return false, fmt.Errorf("field %s is not numeric", e.Field)
	}
	switch e.Op {
	case "gt":
		return a > b, nil
	case "gte":
		return a >= b, nil
	case "lt":
		return a < b, nil
	case "lte":
		return a <= b, nil
	}
	return false, fmt.Errorf("unsupported operator %q", e.Op)
}

func lookup(vars map[string]any, path string) (any, bool) {
	var cur any = vars
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(n), 64)
		return f, err == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func equal(a, b any) (bool, error) {
	if a == nil || b == nil {
		return a == nil && b == nil, nil
	}
	if fa, ok := toFloat(a); ok {
		fb, ok := toFloat(b)
		return ok && fa == fb, nil
	}
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv, nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv, nil
	}
	return false, fmt.Errorf("cannot compare %T values", a)
}
