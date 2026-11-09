// Package catalog is the registry of references a workflow definition may use: approval policies,
// agents, connectors, assignee roles and artifact types. Anything not registered fails validation,
// so definitions cannot invoke arbitrary agents, tools or endpoints.
package catalog

import (
	"fmt"
	"slices"
	"sort"
)

type Policy struct {
	Ref         string `json:"ref"`
	Name        string `json:"name"`
	Pack        string `json:"pack"`
	Provisional bool   `json:"provisional"` // pending business sign-off; never presented as ratified policy
	Description string `json:"description,omitempty"`
	// SoD lists the segregation-of-duties rules enforced for this policy (default: all rules).
	SoD []string `json:"sod"`
	// Ratification is set only from external configuration citing the business decision.
	Ratification *Ratification `json:"ratification,omitempty"`
}

// Ratification records where a policy binding was approved by the business. Flowcase never infers it.
type Ratification struct {
	Reference string `json:"reference"`
	Date      string `json:"date"`
	By        string `json:"by"`
}

// SoD rules the runtime knows how to enforce.
const (
	SoDNotCaseOwner     = "not_case_owner"
	SoDNotTaskSubmitter = "not_task_submitter"
)

var SoDRules = []string{SoDNotCaseOwner, SoDNotTaskSubmitter}

type Agent struct {
	Ref          string `json:"ref"`
	Name         string `json:"name"`
	Pack         string `json:"pack"`
	SkillVersion string `json:"skill_version"`
	// RuntimeAgentID is the identifier in the external AI runtime (defaults to Ref).
	RuntimeAgentID string `json:"runtime_agent_id"`
}

type Connector struct {
	Ref        string   `json:"ref"`
	Name       string   `json:"name"`
	Pack       string   `json:"pack"`
	Operations []string `json:"operations"`
}

type Catalog interface {
	Policy(ref string) (Policy, bool)
	Agent(ref string) (Agent, bool)
	Connector(ref string) (Connector, bool)
	HasAssigneeRole(ref string) bool
	HasArtifactType(ref string) bool
	Snapshot() Snapshot
}

// Snapshot is the serializable view used by the designer UI.
type Snapshot struct {
	Policies      []Policy    `json:"policies"`
	Agents        []Agent     `json:"agents"`
	Connectors    []Connector `json:"connectors"`
	AssigneeRoles []string    `json:"assignee_roles"`
	ArtifactTypes []string    `json:"artifact_types"`
}

type Static struct {
	policies      map[string]Policy
	agents        map[string]Agent
	connectors    map[string]Connector
	assigneeRoles map[string]bool
	artifactTypes map[string]bool
}

func NewStatic() *Static {
	return &Static{policies: map[string]Policy{}, agents: map[string]Agent{}, connectors: map[string]Connector{},
		assigneeRoles: map[string]bool{}, artifactTypes: map[string]bool{}}
}

// Merge adds entries; registering the same ref twice is a configuration error.
func (s *Static) Merge(o Snapshot) error {
	for _, p := range o.Policies {
		if _, dup := s.policies[p.Ref]; dup {
			return fmt.Errorf("policy %q registered twice", p.Ref)
		}
		if len(p.SoD) == 0 {
			p.SoD = slices.Clone(SoDRules)
		}
		s.policies[p.Ref] = p
	}
	for _, a := range o.Agents {
		if _, dup := s.agents[a.Ref]; dup {
			return fmt.Errorf("agent %q registered twice", a.Ref)
		}
		if a.RuntimeAgentID == "" {
			a.RuntimeAgentID = a.Ref
		}
		s.agents[a.Ref] = a
	}
	for _, c := range o.Connectors {
		if _, dup := s.connectors[c.Ref]; dup {
			return fmt.Errorf("connector %q registered twice", c.Ref)
		}
		s.connectors[c.Ref] = c
	}
	for _, r := range o.AssigneeRoles {
		s.assigneeRoles[r] = true
	}
	for _, t := range o.ArtifactTypes {
		s.artifactTypes[t] = true
	}
	return nil
}

// SetPolicy and SetAgent replace an existing registration (used by external binding config).
func (s *Static) SetPolicy(p Policy) { s.policies[p.Ref] = p }
func (s *Static) SetAgent(a Agent)   { s.agents[a.Ref] = a }

func (s *Static) Policy(ref string) (Policy, bool)       { p, ok := s.policies[ref]; return p, ok }
func (s *Static) Agent(ref string) (Agent, bool)         { a, ok := s.agents[ref]; return a, ok }
func (s *Static) Connector(ref string) (Connector, bool) { c, ok := s.connectors[ref]; return c, ok }
func (s *Static) HasAssigneeRole(ref string) bool        { return s.assigneeRoles[ref] }
func (s *Static) HasArtifactType(ref string) bool        { return s.artifactTypes[ref] }

func (s *Static) Snapshot() Snapshot {
	out := Snapshot{Policies: []Policy{}, Agents: []Agent{}, Connectors: []Connector{}, AssigneeRoles: []string{}, ArtifactTypes: []string{}}
	for _, p := range s.policies {
		out.Policies = append(out.Policies, p)
	}
	for _, a := range s.agents {
		out.Agents = append(out.Agents, a)
	}
	for _, c := range s.connectors {
		out.Connectors = append(out.Connectors, c)
	}
	for r := range s.assigneeRoles {
		out.AssigneeRoles = append(out.AssigneeRoles, r)
	}
	for t := range s.artifactTypes {
		out.ArtifactTypes = append(out.ArtifactTypes, t)
	}
	sort.Slice(out.Policies, func(i, j int) bool { return out.Policies[i].Ref < out.Policies[j].Ref })
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].Ref < out.Agents[j].Ref })
	sort.Slice(out.Connectors, func(i, j int) bool { return out.Connectors[i].Ref < out.Connectors[j].Ref })
	slices.Sort(out.AssigneeRoles)
	slices.Sort(out.ArtifactTypes)
	return out
}
