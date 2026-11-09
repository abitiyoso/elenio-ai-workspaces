// Package validator checks workflow definitions: JSON-schema structure first, then semantic graph
// and reference rules (WCP-02, UAT-08, UAT-11). Publication requires a report without errors.
package validator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/compiler"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

type Result struct {
	Report     definition.Report
	Definition *definition.Definition // nil when the document is not structurally valid
	Graph      *compiler.Graph
}

// Validate parses and checks a definition document against the registered catalog.
func Validate(raw []byte, cat catalog.Catalog) Result {
	if len(raw) > maxDocumentBytes {
		return Result{Report: definition.NewReport([]definition.Diagnostic{
			definition.Errorf("", "", "document_too_large", "definition exceeds %d KiB", maxDocumentBytes>>10)})}
	}
	doc, err := definition.DecodeRaw(raw)
	if err != nil {
		return Result{Report: definition.NewReport([]definition.Diagnostic{definition.Errorf("", "", "invalid_json", "definition is not valid JSON: %v", err)})}
	}
	if diags := checkSchema(doc); len(diags) > 0 {
		sortDiags(diags)
		return Result{Report: definition.NewReport(diags)}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var d definition.Definition
	if err := dec.Decode(&d); err != nil {
		return Result{Report: definition.NewReport([]definition.Diagnostic{definition.Errorf("", "", "invalid_json", "definition could not be decoded: %v", err)})}
	}
	return ValidateDefinition(&d, cat)
}

// ValidateDefinition runs the semantic checks on an already structurally valid definition.
func ValidateDefinition(d *definition.Definition, cat catalog.Catalog) Result {
	s := &semantic{d: d, cat: cat, g: compiler.Build(d)}
	s.run()
	sortDiags(s.diags)
	return Result{Report: definition.NewReport(s.diags), Definition: d, Graph: s.g}
}

func sortDiags(d []definition.Diagnostic) {
	sort.SliceStable(d, func(i, j int) bool {
		if d[i].Severity != d[j].Severity {
			return d[i].Severity == definition.SeverityError
		}
		return d[i].Path < d[j].Path
	})
}

type semantic struct {
	d     *definition.Definition
	cat   catalog.Catalog
	g     *compiler.Graph
	diags []definition.Diagnostic
}

func (s *semantic) errorf(path, node, code, format string, args ...any) {
	s.diags = append(s.diags, definition.Errorf(path, node, code, format, args...))
}

func (s *semantic) warnf(path, node, code, format string, args ...any) {
	s.diags = append(s.diags, definition.Warnf(path, node, code, format, args...))
}

func nodePath(i int) string { return fmt.Sprintf("/nodes/%d", i) }

func (s *semantic) run() {
	ids := map[string]int{}
	var starts []int
	ends := 0
	for i, n := range s.d.Nodes {
		if first, dup := ids[n.ID]; dup {
			s.errorf(nodePath(i)+"/id", n.ID, "duplicate_node_id", "node id %q is already used by /nodes/%d", n.ID, first)
			continue
		}
		ids[n.ID] = i
		switch n.Type {
		case definition.Start:
			starts = append(starts, i)
		case definition.End:
			ends++
		}
	}
	switch {
	case len(starts) == 0:
		s.errorf("/nodes", "", "missing_start", "exactly one start node is required")
	case len(starts) > 1:
		for _, i := range starts[1:] {
			s.errorf(nodePath(i)+"/type", s.d.Nodes[i].ID, "multiple_starts", "only one start node is allowed")
		}
	}
	if ends == 0 {
		s.errorf("/nodes", "", "missing_end", "at least one end node is required")
	}

	for i := range s.d.Nodes {
		n := &s.d.Nodes[i]
		for j, t := range n.Next {
			p := fmt.Sprintf("%s/next/%d", nodePath(i), j)
			if _, ok := ids[t]; !ok {
				s.errorf(p, n.ID, "unknown_target", "next target %q does not exist", t)
			} else if t == n.ID {
				s.errorf(p, n.ID, "self_loop", "a node cannot transition to itself")
			}
		}
		s.edgeCount(i, n)
		s.config(i, n)
	}
	s.policyRefs()
	s.graph(ids)
}

func (s *semantic) edgeCount(i int, n *definition.Node) {
	p := nodePath(i) + "/next"
	switch n.Type {
	case definition.End:
		if len(n.Next) != 0 {
			s.errorf(p, n.ID, "invalid_next", "end nodes cannot have outgoing transitions")
		}
	case definition.Condition, definition.ParallelSplit:
		if len(n.Next) < 2 {
			s.errorf(p, n.ID, "invalid_next", "%s nodes need at least two outgoing transitions", n.Type)
		}
	case definition.Subworkflow:
		// reported by config()
	default:
		if len(n.Next) != 1 {
			s.errorf(p, n.ID, "invalid_next", "%s nodes need exactly one outgoing transition", n.Type)
		}
	}
}

// --- per-type configuration -------------------------------------------------------------------

type kind int

const (
	kString kind = iota
	kBool
	kRef
	kDuration
	kNodeRef
	kSourceList
	kScalarObject
	kDataSchema
	kExpression
	kBranches
	kJoinMode
	kField
	kSkillVersion
)

type spec struct {
	fields   map[string]kind
	required []string
}

var specs = map[definition.NodeType]spec{
	definition.Start:         {fields: map[string]kind{}},
	definition.End:           {fields: map[string]kind{}},
	definition.ParallelSplit: {fields: map[string]kind{}},
	definition.HumanTask: {fields: map[string]kind{"assignee_role": kRef, "output_artifact_type": kRef, "due_in": kDuration,
		"instructions": kString, "form_schema": kDataSchema}, required: []string{"assignee_role"}},
	definition.HumanApproval: {fields: map[string]kind{"policy_ref": kRef, "blocking": kBool, "artifact_snapshot_required": kBool,
		"return_to": kNodeRef, "due_in": kDuration, "instructions": kString}, required: []string{"policy_ref"}},
	definition.AgentTask: {fields: map[string]kind{"agent_ref": kRef, "output_artifact_type": kRef, "context_sources": kSourceList,
		"skill_version": kSkillVersion, "timeout": kDuration, "author_review": kBool}, required: []string{"agent_ref", "output_artifact_type"}},
	definition.ServiceTask: {fields: map[string]kind{"connector_ref": kRef, "operation": kRef, "params": kScalarObject, "timeout": kDuration},
		required: []string{"connector_ref", "operation"}},
	definition.Condition:    {fields: map[string]kind{"expression": kExpression, "branches": kBranches}, required: []string{"expression", "branches"}},
	definition.ParallelJoin: {fields: map[string]kind{"join": kJoinMode, "split_ref": kNodeRef}, required: []string{"join", "split_ref"}},
	definition.Timer:        {fields: map[string]kind{"duration": kDuration, "wait_until_field": kField, "escalate_to_role": kRef}},
}

// Keys that signal an attempt to embed code or endpoints get a specific, actionable error.
var unsafeKeys = []string{"script", "code", "sql", "query", "url", "endpoint", "webhook", "javascript", "js", "eval", "command", "exec", "http", "lambda"}

// ContextSources is the allowlist of evidence scopes an agent task may request.
var ContextSources = []string{"case-files", "kms-approved"}

var (
	durationPattern = regexp.MustCompile(`^([1-9][0-9]{0,3})(m|h|d)$`)
	skillPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)
)

func (s *semantic) config(i int, n *definition.Node) {
	base := nodePath(i) + "/config"
	if n.Type == definition.Subworkflow {
		s.errorf(nodePath(i)+"/type", n.ID, "unsupported_node_type", "subworkflow is deferred and not executable in schema 1.0")
		return
	}
	sp, ok := specs[n.Type]
	if !ok {
		return
	}
	for key := range n.Config {
		if _, allowed := sp.fields[key]; allowed {
			continue
		}
		if slices.Contains(unsafeKeys, key) {
			s.errorf(base+"/"+key, n.ID, "unsafe_config", "%q is not allowed: workflows cannot embed code, queries or endpoints; use registered agents or connectors", key)
		} else {
			s.errorf(base+"/"+key, n.ID, "unknown_config", "%q is not a valid %s setting", key, n.Type)
		}
	}
	for _, key := range sp.required {
		if _, ok := n.Config[key]; !ok {
			s.errorf(base+"/"+key, n.ID, "required", "%s nodes require %s", n.Type, key)
		}
	}
	keys := make([]string, 0, len(sp.fields))
	for k := range sp.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v, present := n.Config[key]
		if !present {
			continue
		}
		s.value(n, base+"/"+key, key, sp.fields[key], v)
	}
	if n.Type == definition.Timer {
		_, hasDur := n.Config["duration"]
		_, hasField := n.Config["wait_until_field"]
		if hasDur == hasField {
			s.errorf(base, n.ID, "invalid_timer", "timer nodes need exactly one of duration or wait_until_field")
		}
	}
}

func (s *semantic) value(n *definition.Node, path, key string, k kind, v any) {
	str, isStr := v.(string)
	switch k {
	case kString:
		if !isStr || len(str) > 2000 {
			s.errorf(path, n.ID, "invalid_type", "%s must be a string of at most 2000 characters", key)
		}
	case kBool:
		if _, ok := v.(bool); !ok {
			s.errorf(path, n.ID, "invalid_type", "%s must be true or false", key)
		}
	case kDuration:
		if !isStr || !validDuration(str) {
			s.errorf(path, n.ID, "invalid_duration", "%s must look like 30m, 48h or 5d (at most 365d)", key)
		}
	case kSkillVersion:
		if !isStr || !skillPattern.MatchString(str) {
			s.errorf(path, n.ID, "invalid_type", "skill_version must be a short version label")
		}
	case kField:
		if !isStr || !definition.FieldPattern.MatchString(str) {
			s.errorf(path, n.ID, "invalid_type", "%s must be a case variable path", key)
		}
	case kNodeRef:
		if !isStr || s.g.Node(str) == nil {
			s.errorf(path, n.ID, "unknown_target", "%s must reference an existing node", key)
		}
	case kJoinMode:
		if str != "all" {
			s.errorf(path, n.ID, "unsupported_join", "only join \"all\" is supported")
		}
	case kRef:
		if !isStr || !refPattern.MatchString(str) {
			s.errorf(path, n.ID, "invalid_ref", "%s must be a registered reference such as domain.name", key)
			return
		}
		s.ref(n, path, key, str)
	case kSourceList:
		list, ok := v.([]any)
		if !ok {
			s.errorf(path, n.ID, "invalid_type", "context_sources must be an array")
			return
		}
		for j, item := range list {
			src, _ := item.(string)
			if !slices.Contains(ContextSources, src) {
				s.errorf(fmt.Sprintf("%s/%d", path, j), n.ID, "unknown_context_source", "context source must be one of %v", ContextSources)
			}
		}
	case kScalarObject:
		obj, ok := v.(map[string]any)
		if !ok || len(obj) > 50 {
			s.errorf(path, n.ID, "invalid_type", "params must be an object with at most 50 entries")
			return
		}
		for pk, pv := range obj {
			switch pv.(type) {
			case string, bool, json.Number, nil:
			default:
				s.errorf(path+"/"+pk, n.ID, "invalid_type", "params values must be scalars")
			}
		}
	case kDataSchema:
		c := &schemaChecker{}
		c.dataSchema(v, path, 1)
		for _, d := range c.diags {
			d.NodeID = n.ID
			s.diags = append(s.diags, d)
		}
	case kExpression:
		expr, diags := definition.ParseExpr(v, path, n.ID)
		s.diags = append(s.diags, diags...)
		if len(diags) == 0 {
			s.expressionFields(n, path, expr)
		}
	case kBranches:
		s.branches(n, path, v)
	}
}

func validDuration(s string) bool {
	m := durationPattern.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	n, _ := strconv.Atoi(m[1])
	limit := map[string]int{"m": 365 * 24 * 60, "h": 365 * 24, "d": 365}[m[2]]
	return n <= limit
}

func (s *semantic) ref(n *definition.Node, path, key, ref string) {
	if s.cat == nil {
		return
	}
	switch key {
	case "policy_ref":
		p, ok := s.cat.Policy(ref)
		if !ok {
			s.errorf(path, n.ID, "unknown_policy_ref", "approval policy %q is not registered", ref)
		} else if p.Provisional {
			s.warnf(path, n.ID, "provisional_policy", "policy %q is provisional and pending business sign-off", ref)
		}
	case "agent_ref":
		if _, ok := s.cat.Agent(ref); !ok {
			s.errorf(path, n.ID, "unknown_agent_ref", "agent %q is not registered", ref)
		}
	case "connector_ref":
		if _, ok := s.cat.Connector(ref); !ok {
			s.errorf(path, n.ID, "unknown_connector_ref", "connector %q is not registered", ref)
		}
	case "operation":
		if c, ok := s.cat.Connector(n.ConfigString("connector_ref")); ok && !slices.Contains(c.Operations, ref) {
			s.errorf(path, n.ID, "unknown_operation", "connector %q has no operation %q", c.Ref, ref)
		}
	case "assignee_role", "escalate_to_role":
		if !s.cat.HasAssigneeRole(ref) {
			s.errorf(path, n.ID, "unknown_assignee_role", "role %q is not registered", ref)
		}
	case "output_artifact_type":
		if !s.cat.HasArtifactType(ref) {
			s.errorf(path, n.ID, "unknown_artifact_type", "artifact type %q is not registered", ref)
		}
	}
}

func (s *semantic) expressionFields(n *definition.Node, path string, e definition.Expr) {
	if e.Field != "" {
		root := e.Field
		if i := bytes.IndexByte([]byte(root), '.'); i >= 0 {
			root = root[:i]
		}
		declared := propertyType(s.d.InputSchema, root)
		switch {
		case declared == "":
			s.warnf(path, n.ID, "undeclared_field", "field %q is not declared in input_schema; it must be set before this condition runs", e.Field)
		case root == e.Field && slices.Contains([]string{"gt", "gte", "lt", "lte"}, e.Op) && declared != "number" && declared != "integer":
			s.errorf(path, n.ID, "type_mismatch", "%s compares numbers but %q is declared as %s", e.Op, e.Field, declared)
		}
	}
	for i, a := range e.Args {
		sub := fmt.Sprintf("%s/conditions/%d", path, i)
		if e.Op == "not" {
			sub = path + "/condition"
		}
		s.expressionFields(n, sub, a)
	}
}

func (s *semantic) branches(n *definition.Node, path string, v any) {
	obj, ok := v.(map[string]any)
	if !ok {
		s.errorf(path, n.ID, "invalid_type", "branches must map \"true\" and \"false\" to node ids")
		return
	}
	for label := range obj {
		if label != "true" && label != "false" {
			s.errorf(path+"/"+label, n.ID, "invalid_branch_label", "branch labels must be \"true\" or \"false\"")
		}
	}
	targets := map[string]bool{}
	for _, label := range []string{"true", "false"} {
		t, ok := obj[label].(string)
		if !ok {
			s.errorf(path+"/"+label, n.ID, "non_exhaustive_condition", "condition must route both the true and the false outcome")
			continue
		}
		if s.g.Node(t) == nil {
			s.errorf(path+"/"+label, n.ID, "unknown_target", "branch target %q does not exist", t)
		}
		targets[t] = true
	}
	if len(targets) == 1 {
		s.errorf(path, n.ID, "redundant_condition", "both outcomes go to the same node")
	}
	next := map[string]bool{}
	for _, t := range n.Next {
		next[t] = true
	}
	for t := range targets {
		if !next[t] {
			s.errorf(path, n.ID, "branches_next_mismatch", "branch target %q must also be listed in next", t)
		}
	}
	for t := range next {
		if !targets[t] {
			s.errorf(path, n.ID, "branches_next_mismatch", "next target %q is not used by any branch", t)
		}
	}
}

func (s *semantic) policyRefs() {
	declared := map[string]bool{}
	for i, ref := range s.d.PolicyRefs {
		declared[ref] = true
		if s.cat != nil {
			if _, ok := s.cat.Policy(ref); !ok {
				s.errorf(fmt.Sprintf("/policy_refs/%d", i), "", "unknown_policy_ref", "approval policy %q is not registered", ref)
			}
		}
	}
	used := map[string]bool{}
	for i, n := range s.d.Nodes {
		if n.Type != definition.HumanApproval {
			continue
		}
		ref := n.ConfigString("policy_ref")
		if ref == "" {
			continue
		}
		used[ref] = true
		if !declared[ref] {
			s.errorf(nodePath(i)+"/config/policy_ref", n.ID, "policy_ref_not_declared", "policy %q must be listed in policy_refs", ref)
		}
	}
	for i, ref := range s.d.PolicyRefs {
		if !used[ref] {
			s.warnf(fmt.Sprintf("/policy_refs/%d", i), "", "unused_policy_ref", "policy %q is declared but no approval uses it", ref)
		}
	}
}

// --- graph rules -------------------------------------------------------------------------------

func (s *semantic) graph(ids map[string]int) {
	g := s.g
	if g.Start == "" {
		return
	}
	for _, p := range g.Pred[g.Start] {
		s.errorf(nodePath(ids[p])+"/next", p, "edge_into_start", "no node may transition into the start node")
	}
	reach := g.Reachable(g.Start)
	toEnd := g.CanReachEnd()
	for i, n := range s.d.Nodes {
		if ids[n.ID] != i {
			continue
		}
		if !reach[n.ID] {
			s.errorf(nodePath(i), n.ID, "unreachable_node", "node %q cannot be reached from start", n.ID)
		} else if !toEnd[n.ID] {
			s.errorf(nodePath(i), n.ID, "dead_end", "no path from %q reaches an end node", n.ID)
		}
	}
	if cycle := g.Cycle(); cycle != nil {
		s.errorf(nodePath(ids[cycle[0]])+"/next", cycle[0], "cycle", "transitions form a loop (%v); rework loops are expressed with an approval's return_to", append(cycle, cycle[0]))
		return // region analysis assumes a DAG
	}
	s.parallel(ids)
	s.returnTo(ids)
}

func (s *semantic) parallel(ids map[string]int) {
	g := s.g
	joinsBySplit := map[string][]string{}
	for i, n := range s.d.Nodes {
		if n.Type != definition.ParallelJoin || ids[n.ID] != i {
			continue
		}
		ref := n.ConfigString("split_ref")
		if sp := g.Node(ref); ref != "" && (sp == nil || sp.Type != definition.ParallelSplit) {
			s.errorf(nodePath(i)+"/config/split_ref", n.ID, "unmatched_join", "split_ref %q is not a parallel_split node", ref)
			continue
		}
		joinsBySplit[ref] = append(joinsBySplit[ref], n.ID)
	}
	for i, n := range s.d.Nodes {
		if n.Type != definition.ParallelSplit || ids[n.ID] != i {
			continue
		}
		joins := joinsBySplit[n.ID]
		switch len(joins) {
		case 0:
			s.errorf(nodePath(i), n.ID, "unmatched_split", "parallel_split %q has no parallel_join with split_ref %q", n.ID, n.ID)
			continue
		case 1:
		default:
			s.errorf(nodePath(i), n.ID, "duplicate_join", "parallel_split %q is referenced by several joins %v", n.ID, joins)
			continue
		}
		join := joins[0]
		for _, branch := range g.Succ[n.ID] {
			if !g.AllPathsReach(branch, join) {
				s.errorf(nodePath(i)+"/next", n.ID, "branch_bypasses_join", "a path from branch %q ends or leaves before join %q; every branch must reach its join", branch, join)
			}
		}
		region := g.Region(n.ID, join)
		inside := func(id string) bool { return region[id] || id == n.ID }
		for id := range region {
			for _, p := range g.Pred[id] {
				if !inside(p) {
					s.errorf(nodePath(ids[p])+"/next", p, "enters_parallel_region", "%q jumps into parallel branch node %q from outside split %q", p, id, n.ID)
				}
			}
		}
		for _, p := range g.Pred[join] {
			if !inside(p) {
				s.errorf(nodePath(ids[p])+"/next", p, "enters_parallel_region", "%q reaches join %q without passing split %q", p, join, n.ID)
			}
		}
	}
}

func (s *semantic) returnTo(ids map[string]int) {
	g := s.g
	for i, n := range s.d.Nodes {
		target := n.ConfigString("return_to")
		if n.Type != definition.HumanApproval || target == "" || g.Node(target) == nil {
			continue
		}
		path := nodePath(i) + "/config/return_to"
		tn := g.Node(target)
		if tn.Type == definition.Start || !g.Reachable(target)[n.ID] || target == n.ID {
			s.errorf(path, n.ID, "invalid_return_to", "return_to must name an earlier step that leads to this approval")
			continue
		}
		for split, join := range g.SplitJoin {
			region := g.Region(split, join)
			if region[n.ID] != region[target] {
				s.errorf(path, n.ID, "invalid_return_to", "return_to cannot cross the parallel region of %q", split)
			}
		}
	}
}
