package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/elenio/case-workspace/internal/flowcase"
	"github.com/elenio/case-workspace/internal/packs"
	"github.com/elenio/case-workspace/internal/store"
)

func registry(t *testing.T) *packs.Registry {
	t.Helper()
	reg, err := packs.Load(context.Background(), "../../domain-packs", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestResolveBindingRules(t *testing.T) {
	reg := registry(t)
	// first view: newest valid published pack for the workflow
	p, b, persist := ResolveBinding(nil, "sim-tender", reg, "u1")
	if p == nil || p.Version != "1.3.0" || persist == nil || persist.Binding != store.PinnedOnFirstView || b != persist {
		t.Fatalf("first view: %v %v %v", p, b, persist)
	}
	// an existing binding to an older version is kept (no silent re-pin)
	old := &store.Binding{CaseID: "c", PackKey: "sim.tender", PackVersion: "1.0.0", Binding: store.PinnedOnFirstView}
	p, b, persist = ResolveBinding(old, "sim-tender", reg, "u1")
	if p == nil || p.Version != "1.0.0" || persist != nil || b != old {
		t.Fatalf("existing: %v %v %v", p, b, persist)
	}
	// a binding to a version that is no longer valid -> unavailable, binding kept, nothing persisted
	gone := &store.Binding{CaseID: "c", PackKey: "sim.tender", PackVersion: "9.9.9"}
	p, b, persist = ResolveBinding(gone, "sim-tender", reg, "u1")
	if p != nil || b != gone || persist != nil {
		t.Fatal("unavailable pinned pack must not fall back to another version")
	}
	// no pack for the workflow -> generic shell, nothing bound
	p, b, persist = ResolveBinding(nil, "unknown-workflow", reg, "u1")
	if p != nil || b != nil || persist != nil {
		t.Fatal("unknown workflow must not bind")
	}
}

func at(m int) time.Time { return time.Date(2026, 10, 1, 10, m, 0, 0, time.UTC) }

func TestNodeStatuses(t *testing.T) {
	d := &flowcase.CaseDetail{
		Nodes: []flowcase.GraphNode{{ID: "start", Type: "start"}, {ID: "g0", Type: "human_approval"}, {ID: "intake", Type: "agent_task"},
			{ID: "g1", Type: "human_approval"}, {ID: "matching", Type: "agent_task"}, {ID: "design", Type: "agent_task"}, {ID: "end", Type: "end"}},
		Timeline: []flowcase.Execution{
			{NodeID: "start", Status: "completed", CreatedAt: at(0)},
			{NodeID: "g0", Status: "completed", CreatedAt: at(1)},
			{NodeID: "intake", Status: "completed", Iteration: 1, CreatedAt: at(2)},
			{NodeID: "intake", Status: "superseded", Iteration: 2, CreatedAt: at(9)},
			{NodeID: "g1", Status: "waiting_human", CreatedAt: at(3)},
			{NodeID: "matching", Status: "waiting_human", CreatedAt: at(4)},
			{NodeID: "design", Status: "failed", CreatedAt: at(5)},
		},
		Approvals: json.RawMessage(`[{"id":"a1","node_id":"g1","status":"pending"}]`),
		Drafts:    json.RawMessage(`[{"id":"d1","node_id":"matching","status":"pending"},{"id":"d0","node_id":"intake","status":"submitted"}]`),
	}
	got := map[string]string{}
	for _, n := range NodeStatuses(d) {
		got[n.ID] = n.Status
	}
	want := map[string]string{"start": "completed", "g0": "completed", "intake": "completed", "g1": "waiting_approval",
		"matching": "waiting_review", "design": "failed", "end": "pending"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
}

// fakeDetail is a Flowcase case of a workflow with one artifact per given type.
func fakeDetail(workflowKey string, artifactTypes ...string) *flowcase.CaseDetail {
	d := &flowcase.CaseDetail{CaseSummary: flowcase.CaseSummary{ID: "11111111-2222-4333-8444-555555555555", Reference: "CASE-1", WorkflowKey: workflowKey,
		WorkflowVersion: 1, VersionNum: 3}, Nodes: []flowcase.GraphNode{{ID: "step", Name: "Step", Type: "agent_task"}}}
	for i, t := range artifactTypes {
		content := json.RawMessage(`{"artifact_draft":{"rows":[{"a":1}]},"sources":[{"source_id":"s-` + t + `","title":"Doc"}]}`)
		d.Artifacts = append(d.Artifacts, flowcase.Artifact{ID: "art-" + t, Type: t, ProducerNodeID: "step", CurrentVersion: 1,
			Versions: []flowcase.ArtifactVersion{{Version: 1, Status: "ready_for_review", Content: content, CreatedAt: at(i)}}})
	}
	return d
}

// TestSameDataPathForAllPacks proves one generic composition renders three unrelated packs: navigation,
// artifact types and artifact mapping come only from each manifest.
func TestSameDataPathForAllPacks(t *testing.T) {
	reg := registry(t)
	seen := 0
	for _, key := range []string{"sim.tender", "legal.contract-review", "lab.poc-assessment"} {
		p := reg.LatestPublished(key)
		if p == nil {
			t.Fatalf("%s missing", key)
		}
		m := p.Manifest
		var sourceTypes []string
		for _, at := range m.ArtifactTypes {
			if at.Source != nil {
				sourceTypes = append(sourceTypes, at.Source.FlowcaseArtifactType)
			}
		}
		d := fakeDetail(m.Bindings.FlowcaseWorkflowKey, append(sourceTypes, "unmapped-type")...)
		pp, b, _ := ResolveBinding(nil, d.WorkflowKey, reg, "u1")
		c := Compose(d, pp, b, "/apps/flowcase")
		if c.Pack.Status != PackOK || c.Pack.PackKey != key {
			t.Fatalf("%s: pack = %+v", key, c.Pack)
		}
		if len(c.Navigation) != len(m.Navigation) || len(c.ArtifactTypes) != len(m.ArtifactTypes) {
			t.Fatalf("%s: navigation/artifact types not from manifest", key)
		}
		for i, n := range m.Navigation {
			if c.Navigation[i].ID != n.Key || c.Navigation[i].Label != n.Label || c.Navigation[i].ArtifactType != n.ArtifactType {
				t.Fatalf("%s: nav %d = %+v", key, i, c.Navigation[i])
			}
		}
		for _, a := range c.Artifacts {
			if a.Type == "unmapped-type" {
				if a.PackArtifactType != nil {
					t.Fatal("unmapped artifact got a pack type")
				}
				continue
			}
			if a.PackArtifactType == nil || m.ArtifactTypeFor(a.Type).Key != *a.PackArtifactType {
				t.Fatalf("%s: artifact %s not mapped", key, a.Type)
			}
			if len(a.Sources) != 1 || !strings.Contains(string(a.Content), "artifact_draft") {
				t.Fatalf("%s: content must pass through unchanged and sources be extracted", key)
			}
		}
		seen++
	}
	if seen != 3 {
		t.Fatal("not all packs exercised")
	}
}

func TestGenericShellWhenNoPack(t *testing.T) {
	c := Compose(fakeDetail("sim-tender", "requirement-register"), nil, nil, "/apps/flowcase")
	if c.Pack.Status != PackUnavailable || len(c.ArtifactTypes) != 0 {
		t.Fatalf("pack = %+v", c.Pack)
	}
	var views []string
	for _, n := range c.Navigation {
		views = append(views, n.View)
	}
	if strings.Join(views, ",") != "overview,tasks,approvals,timeline" {
		t.Fatalf("shell = %v", views)
	}
	if c.Artifacts[0].PackArtifactType != nil {
		t.Fatal("no pack -> no artifact type mapping")
	}
}

// TestNoPackSpecificBranchesInCore greps the non-test Go sources for pack keys, workflow keys or agent
// keys of the bundled packs: domain semantics live in the manifests only.
func TestNoPackSpecificBranchesInCore(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)sim[.\-_ ]?tender|sim\.[a-z_]+|case[_-]intake|contract[-_ ]review|legal[.\-]|poc[-_ ]|lab\.|requirement-register|capability-fit-gap|proposal-draft|cost-review`)
	roots := []string{"../../internal", "../../cmd"}
	checked := 0
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && (info.Name() == "testkit") {
				return filepath.SkipDir
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if forbidden.MatchString(line) {
					t.Errorf("%s:%d mentions a specific pack/workflow/agent: %s", path, i+1, strings.TrimSpace(line))
				}
			}
			checked++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked < 10 {
		t.Fatalf("only %d files checked", checked)
	}
}
