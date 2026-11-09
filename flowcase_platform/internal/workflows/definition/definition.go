// Package definition holds the workflow definition document model (schema 1.0), its diagnostics
// and canonical hashing. Definitions are declarative data: nothing in them is ever executed as code.
package definition

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const SchemaVersion = "1.0"

type NodeType string

const (
	Start         NodeType = "start"
	End           NodeType = "end"
	HumanTask     NodeType = "human_task"
	AgentTask     NodeType = "agent_task"
	ServiceTask   NodeType = "service_task"
	HumanApproval NodeType = "human_approval"
	Condition     NodeType = "condition"
	ParallelSplit NodeType = "parallel_split"
	ParallelJoin  NodeType = "parallel_join"
	Timer         NodeType = "timer"
	Subworkflow   NodeType = "subworkflow"
)

// NodeTypes lists every type the schema names, including subworkflow which the 1.0 runtime defers.
var NodeTypes = []NodeType{Start, End, HumanTask, AgentTask, ServiceTask, HumanApproval, Condition, ParallelSplit, ParallelJoin, Timer, Subworkflow}

type Definition struct {
	SchemaVersion string         `json:"schema_version"`
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	Version       int            `json:"version"`
	Status        string         `json:"status,omitempty"`
	Description   string         `json:"description,omitempty"`
	TenantScope   string         `json:"tenant_scope,omitempty"`
	InputSchema   map[string]any `json:"input_schema,omitempty"`
	PolicyRefs    []string       `json:"policy_refs,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Nodes         []Node         `json:"nodes"`
}

type Node struct {
	ID     string         `json:"id"`
	Type   NodeType       `json:"type"`
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
	Next   []string       `json:"next"`
}

func (d *Definition) Node(id string) (*Node, bool) {
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return &d.Nodes[i], true
		}
	}
	return nil, false
}

// ConfigString returns a string config value, or "" when absent or not a string.
func (n *Node) ConfigString(key string) string {
	s, _ := n.Config[key].(string)
	return s
}

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is one validation finding. Path is a JSON pointer into the submitted document.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Path     string   `json:"path"`
	NodeID   string   `json:"node_id,omitempty"`
}

type Report struct {
	Valid       bool         `json:"valid"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func NewReport(diags []Diagnostic) Report {
	if diags == nil {
		diags = []Diagnostic{}
	}
	valid := true
	for _, d := range diags {
		if d.Severity == SeverityError {
			valid = false
			break
		}
	}
	return Report{Valid: valid, Diagnostics: diags}
}

func (r Report) Errors() []Diagnostic {
	var out []Diagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			out = append(out, d)
		}
	}
	return out
}

func Errorf(path, nodeID, code, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: SeverityError, Code: code, Message: fmt.Sprintf(format, args...), Path: path, NodeID: nodeID}
}

func Warnf(path, nodeID, code, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: SeverityWarning, Code: code, Message: fmt.Sprintf(format, args...), Path: path, NodeID: nodeID}
}

// DecodeRaw parses JSON preserving number literals and rejecting trailing data.
func DecodeRaw(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected data after JSON document")
	}
	return v, nil
}

// Canonical returns deterministic JSON for the definition content. Lifecycle status is server state,
// not content, so it is excluded: publishing never changes the hash of what was validated.
func Canonical(d Definition) ([]byte, error) {
	d.Status = ""
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	// Round-trip through a generic value so map keys inside config/metadata are sorted.
	v, err := DecodeRaw(raw)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Hash is the SHA-256 of the canonical content, hex encoded.
func Hash(d Definition) (string, error) {
	c, err := Canonical(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:]), nil
}
