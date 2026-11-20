package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

// BindingObservation describes current deployment catalog metadata, never a domain release.
type BindingObservation struct {
	Kind           string   `json:"kind"`
	Ref            string   `json:"ref"`
	NodeID         string   `json:"node_id,omitempty"`
	Registered     bool     `json:"registered"`
	RuntimeAgentID string   `json:"runtime_agent_id,omitempty"`
	SkillVersion   string   `json:"skill_version,omitempty"`
	InputContract  string   `json:"input_contract,omitempty"`
	Operation      string   `json:"operation,omitempty"`
	Provisional    *bool    `json:"provisional,omitempty"`
	SoD            []string `json:"sod,omitempty"`
}

type BindingSnapshot struct {
	Schema         string               `json:"schema"`
	WorkflowKey    string               `json:"workflow_key"`
	Version        int                  `json:"version"`
	DefinitionHash string               `json:"definition_hash"`
	Bindings       []BindingObservation `json:"bindings"`
	ContentHash    string               `json:"content_hash"`
	ObservedAt     time.Time            `json:"observed_at"`
}

func (s *Service) ObserveBindings(ctx context.Context, a auth.Identity, key string, version int) (BindingSnapshot, error) {
	v, err := s.GetVersion(ctx, a, key, version)
	if err != nil {
		return BindingSnapshot{}, err
	}
	if v.Status != StatusPublished || v.DefinitionHash == nil {
		return BindingSnapshot{}, &ConflictError{Code: "invalid_state", Message: "Only a published workflow has delivery binding evidence"}
	}
	var d definition.Definition
	if err = json.Unmarshal(v.Definition, &d); err != nil {
		return BindingSnapshot{}, err
	}
	if s.Catalog == nil || len(d.Nodes) > 500 {
		return BindingSnapshot{}, fmt.Errorf("deployment catalog is unavailable or outside supported bounds")
	}
	out := BindingSnapshot{Schema: "elenio.workflow-bindings.v1", WorkflowKey: key, Version: version, DefinitionHash: *v.DefinitionHash, Bindings: []BindingObservation{}, ObservedAt: time.Now().UTC()}
	for _, n := range d.Nodes {
		b := BindingObservation{NodeID: n.ID}
		switch n.Type {
		case definition.AgentTask:
			b.Kind, b.Ref = "agent", n.ConfigString("agent_ref")
			if a, ok := s.Catalog.Agent(b.Ref); ok {
				b.Registered = true
				b.RuntimeAgentID = a.RuntimeAgentID
				if b.RuntimeAgentID == "" {
					b.RuntimeAgentID = b.Ref
				}
				b.SkillVersion = n.ConfigString("skill_version")
				if b.SkillVersion == "" {
					b.SkillVersion = a.SkillVersion
				}
				b.InputContract = a.InputContract
			}
		case definition.HumanApproval:
			b.Kind, b.Ref = "approval_policy", n.ConfigString("policy_ref")
			if p, ok := s.Catalog.Policy(b.Ref); ok {
				b.Registered = true
				b.Provisional = &p.Provisional
				b.SoD = slices.Clone(p.SoD)
			}
		case definition.HumanTask:
			b.Kind, b.Ref = "assignee_role", n.ConfigString("assignee_role")
			b.Registered = s.Catalog.HasAssigneeRole(b.Ref)
		case definition.ServiceTask:
			b.Kind, b.Ref, b.Operation = "connector", n.ConfigString("connector_ref"), n.ConfigString("operation")
			if c, ok := s.Catalog.Connector(b.Ref); ok {
				b.Registered = slices.Contains(c.Operations, b.Operation)
			}
		default:
			continue
		}
		out.Bindings = append(out.Bindings, b)
	}
	sort.Slice(out.Bindings, func(i, j int) bool {
		a, b := out.Bindings[i], out.Bindings[j]
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		return a.Kind < b.Kind
	})
	// Exclude observation time; a binding change must change the digest even for the same workflow.
	raw, err := json.Marshal(map[string]any{"workflow_key": key, "version": version, "definition_hash": out.DefinitionHash, "bindings": out.Bindings})
	if err != nil {
		return BindingSnapshot{}, err
	}
	value, err := definition.DecodeRaw(raw)
	if err != nil {
		return BindingSnapshot{}, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return BindingSnapshot{}, err
	}
	sum := sha256.Sum256(canonical)
	out.ContentHash = hex.EncodeToString(sum[:])
	return out, nil
}
