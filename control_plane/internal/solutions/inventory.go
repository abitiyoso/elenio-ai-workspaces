package solutions

import (
	"encoding/json"
	"regexp"
)

type ResourceDependency struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Version string `json:"version,omitempty"`
	NodeID  string `json:"node_id,omitempty"`
	State   string `json:"state"`
	Reason  string `json:"reason"`
}

var resourceRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

// Inventory extracts only reference names, never instructions, config params, customer artifacts,
// endpoint URLs or credentials. Discovery is not readiness: unresolved resources stay blocked.
func Inventory(raw json.RawMessage, key string, version int) ([]ResourceDependency, bool) {
	out := []ResourceDependency{}
	var d struct {
		Schema   string   `json:"schema_version"`
		Key      string   `json:"key"`
		Version  int      `json:"version"`
		Policies []string `json:"policy_refs"`
		Nodes    []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Config struct {
				Agent     string   `json:"agent_ref"`
				Skill     string   `json:"skill_version"`
				Connector string   `json:"connector_ref"`
				Operation string   `json:"operation"`
				Role      string   `json:"assignee_role"`
				Policy    string   `json:"policy_ref"`
				Sources   []string `json:"context_sources"`
			} `json:"config"`
		} `json:"nodes"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &d) != nil || d.Schema != "1.0" || d.Key != key || d.Version != version || len(d.Nodes) < 2 || len(d.Nodes) > 500 || len(d.Policies) > 100 {
		return []ResourceDependency{{Kind: "workflow_definition", Ref: key, State: "blocked", Reason: "The exact native workflow definition is unavailable or outside the supported inventory bounds."}}, false
	}
	complete := true
	add := func(kind, ref, revision, node string) {
		if len(out) >= 500 {
			complete = false
			return
		}
		if !resourceRef.MatchString(ref) || (revision != "" && !resourceRef.MatchString(revision)) || (node != "" && !resourceRef.MatchString(node)) {
			complete = false
			return
		}
		out = append(out, ResourceDependency{Kind: kind, Ref: ref, Version: revision, NodeID: node, State: "blocked", Reason: "Reference discovered; authoritative version, approval and target binding are not yet resolved."})
	}
	for _, ref := range d.Policies {
		add("approval_policy", ref, "", "")
	}
	for _, n := range d.Nodes {
		if !resourceRef.MatchString(n.ID) {
			complete = false
			continue
		}
		switch n.Type {
		case "agent_task":
			if n.Config.Agent == "" {
				complete = false
			} else {
				add("agent", n.Config.Agent, "", n.ID)
			}
			if n.Config.Skill != "" {
				add("agent_skill_binding", n.Config.Agent, n.Config.Skill, n.ID)
			}
			for _, source := range n.Config.Sources {
				switch source {
				case "kms-approved":
					add("knowledge_scope", source, "", n.ID)
				case "case-files":
					add("case_artifact_scope", source, "", n.ID)
				default:
					complete = false
				}
			}
		case "service_task":
			if n.Config.Connector == "" || !resourceRef.MatchString(n.Config.Operation) {
				complete = false
			} else {
				add("connector", n.Config.Connector, n.Config.Operation, n.ID)
			}
		case "human_task":
			if n.Config.Role == "" {
				complete = false
			} else {
				add("assignee_role", n.Config.Role, "", n.ID)
			}
		case "human_approval":
			if n.Config.Policy == "" {
				complete = false
			} else {
				add("approval_policy", n.Config.Policy, "", n.ID)
			}
		case "start", "end", "condition", "parallel_split", "parallel_join", "timer":
		default:
			complete = false
			add("unsupported_node_type", n.Type, "", n.ID)
		}
	}
	if !complete && len(out) < 500 {
		out = append(out, ResourceDependency{Kind: "inventory_gap", Ref: key, State: "blocked", Reason: "Workflow inventory is incomplete or exceeds supported bounds; it cannot establish release readiness."})
	}
	return out, complete
}
