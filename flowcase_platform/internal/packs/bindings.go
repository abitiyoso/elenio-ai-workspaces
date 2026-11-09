package packs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/elenio/flowcase/internal/workflows/catalog"
)

// Bindings is the external, deployment-owned configuration of a domain pack (PACKS_CONFIG): which
// runtime agent and skill version each registered agent maps to, and how each approval policy is
// enforced. It can only refine registrations the packs declare; it cannot add new refs.
type Bindings struct {
	Agents   map[string]AgentBinding  `json:"agents"`
	Policies map[string]PolicyBinding `json:"policies"`
}

type AgentBinding struct {
	RuntimeAgentID string `json:"runtime_agent_id"`
	SkillVersion   string `json:"skill_version"`
}

type PolicyBinding struct {
	SoD []string `json:"sod"`
	// Ratification must cite the business decision; without it the policy stays provisional.
	Ratification *catalog.Ratification `json:"ratification"`
}

var (
	// Runtime agent ids are external keys, e.g. Agent Studio's "sim.solution_matching".
	runtimeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_./:-]{1,120}$`)
	skillPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)
)

// LoadBindings reads and strictly decodes a bindings file. An empty path means "no bindings".
func LoadBindings(path string) (*Bindings, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("PACKS_CONFIG: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var b Bindings
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("PACKS_CONFIG: %w", err)
	}
	return &b, nil
}

// Apply validates the bindings against the catalog and applies them. Unknown refs, unknown SoD
// rules and incomplete ratification records are configuration errors (fail at startup).
func (b *Bindings) Apply(c *catalog.Static) error {
	if b == nil {
		return nil
	}
	for ref, ab := range b.Agents {
		a, ok := c.Agent(ref)
		if !ok {
			return fmt.Errorf("PACKS_CONFIG agents.%s: agent is not registered by any pack", ref)
		}
		if ab.RuntimeAgentID != "" {
			if !runtimeIDPattern.MatchString(ab.RuntimeAgentID) {
				return fmt.Errorf("PACKS_CONFIG agents.%s.runtime_agent_id is invalid", ref)
			}
			a.RuntimeAgentID = ab.RuntimeAgentID
		}
		if ab.SkillVersion != "" {
			if !skillPattern.MatchString(ab.SkillVersion) {
				return fmt.Errorf("PACKS_CONFIG agents.%s.skill_version is invalid", ref)
			}
			a.SkillVersion = ab.SkillVersion
		}
		c.SetAgent(a)
	}
	for ref, pb := range b.Policies {
		p, ok := c.Policy(ref)
		if !ok {
			return fmt.Errorf("PACKS_CONFIG policies.%s: policy is not registered by any pack", ref)
		}
		if pb.SoD != nil {
			for _, r := range pb.SoD {
				if !slices.Contains(catalog.SoDRules, r) {
					return fmt.Errorf("PACKS_CONFIG policies.%s.sod: unknown rule %q (known: %v)", ref, r, catalog.SoDRules)
				}
			}
			p.SoD = slices.Clone(pb.SoD)
		}
		if r := pb.Ratification; r != nil {
			if _, err := time.Parse("2006-01-02", r.Date); err != nil || len(r.Reference) < 3 || r.By == "" {
				return fmt.Errorf("PACKS_CONFIG policies.%s.ratification needs reference, date (YYYY-MM-DD) and by", ref)
			}
			p.Ratification, p.Provisional = r, false
		}
		c.SetPolicy(p)
	}
	return nil
}
