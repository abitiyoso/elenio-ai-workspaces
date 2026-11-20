package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/elenio/control-plane/internal/solutions"
)

type solutionBinding struct {
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
type solutionBindingSnapshot struct {
	Schema         string            `json:"schema"`
	WorkflowKey    string            `json:"workflow_key"`
	Version        int               `json:"version"`
	DefinitionHash string            `json:"definition_hash"`
	Bindings       []solutionBinding `json:"bindings"`
	ContentHash    string            `json:"content_hash"`
	ObservedAt     time.Time         `json:"observed_at"`
}

func observeSolutionBindings(ctx context.Context, client *http.Client, target, token string, out *solutionDependency, limit int) {
	out.BindingReason = "Deployment bindings are unavailable or invalid; registered refs alone do not establish release readiness."
	if !out.InventoryComplete || len(out.Resources) > limit {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/dependencies", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (160<<10)+1))
	if err != nil || len(raw) > 160<<10 {
		return
	}
	var snapshot solutionBindingSnapshot
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&snapshot) != nil || dec.Decode(new(any)) != io.EOF || !validSolutionBindings(snapshot, *out, limit) {
		return
	}
	out.BindingSnapshot = &snapshot
	out.BindingReason = "Current deployment catalog bindings observed. Agent/model releases, policy authority, external dependencies and target installation still require verification."
	for i := range out.Resources {
		for _, b := range snapshot.Bindings {
			resource := &out.Resources[i]
			if resource.Kind == b.Kind && resource.Ref == b.Ref && resource.NodeID == b.NodeID {
				resource.Reason = "Deployment catalog binding observed; authoritative domain approval, target permissions and installation remain unresolved."
			}
		}
	}
}

func validSolutionBindings(s solutionBindingSnapshot, out solutionDependency, limit int) bool {
	if s.Schema != "elenio.workflow-bindings.v1" || s.WorkflowKey != out.Key || s.Version != out.Version || s.DefinitionHash != out.DefinitionHash ||
		!workflowHash.MatchString(s.ContentHash) || s.ObservedAt.IsZero() || time.Since(s.ObservedAt) > time.Minute || s.ObservedAt.After(time.Now().Add(time.Second)) || s.Bindings == nil || len(s.Bindings) > limit {
		return false
	}
	expected := map[string]bool{}
	key := func(kind, node, ref string) string { return kind + "|" + node + "|" + ref }
	for _, r := range out.Resources {
		if r.NodeID != "" && (r.Kind == "agent" || r.Kind == "approval_policy" || r.Kind == "connector" || r.Kind == "assignee_role") {
			expected[key(r.Kind, r.NodeID, r.Ref)] = true
		}
	}
	if len(expected) != len(s.Bindings) {
		return false
	}
	for _, b := range s.Bindings {
		k := key(b.Kind, b.NodeID, b.Ref)
		if !expected[k] {
			return false
		}
		delete(expected, k)
		if !resourceName(b.Ref) || !resourceName(b.NodeID) || !b.Registered {
			return false
		}
		switch b.Kind {
		case "agent":
			if !resourceName(b.RuntimeAgentID) || (b.SkillVersion != "" && !resourceName(b.SkillVersion)) ||
				(b.InputContract != "" && b.InputContract != "sim.v1" && b.InputContract != "flowcase.case.v1") || b.Operation != "" || b.Provisional != nil || len(b.SoD) > 0 {
				return false
			}
		case "approval_policy":
			if b.Provisional == nil || len(b.SoD) > 2 || b.RuntimeAgentID != "" || b.SkillVersion != "" || b.InputContract != "" || b.Operation != "" {
				return false
			}
			seen := map[string]bool{}
			for _, rule := range b.SoD {
				if (rule != "not_case_owner" && rule != "not_task_submitter") || seen[rule] {
					return false
				}
				seen[rule] = true
			}
		case "connector":
			if !resourceName(b.Operation) || b.RuntimeAgentID != "" || b.SkillVersion != "" || b.InputContract != "" || b.Provisional != nil || len(b.SoD) > 0 {
				return false
			}
		case "assignee_role":
			if b.RuntimeAgentID != "" || b.SkillVersion != "" || b.InputContract != "" || b.Operation != "" || b.Provisional != nil || len(b.SoD) > 0 {
				return false
			}
		default:
			return false
		}
	}
	payload, _ := json.Marshal(map[string]any{"workflow_key": s.WorkflowKey, "version": s.Version, "definition_hash": s.DefinitionHash, "bindings": s.Bindings})
	hash, err := solutions.CanonicalSnapshotHash(payload)
	return err == nil && hash == s.ContentHash
}

func resourceName(value string) bool {
	if len(value) < 1 || len(value) > 128 || strings.Contains(value, "://") {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.:/-", c)) {
			return false
		}
	}
	return true
}
