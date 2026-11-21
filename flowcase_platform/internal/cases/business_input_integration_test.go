package cases_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elenio/flowcase/internal/app"
	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/registry"
)

// These are typed-transport fixtures through the original engine and an explicit HTTP double,
// not approved business applications, document extraction or paid provider acceptance.
func TestStructuredBusinessInputUsesSameEngineWithIndependentReview(t *testing.T) {
	for _, tc := range []struct {
		key              string
		properties, vars map[string]any
	}{
		{"invoice-input-contract", map[string]any{
			"invoice_number": map[string]any{"type": "string"}, "external_id": map[string]any{"type": "integer"},
			"total": map[string]any{"type": "number"}, "validated": map[string]any{"type": "boolean"},
		}, map[string]any{"invoice_number": "INV-001", "external_id": json.Number("9007199254740993"), "total": json.Number("124.50"), "validated": false}},
		{"access-input-contract", map[string]any{
			"request_id": map[string]any{"type": "string"}, "urgent": map[string]any{"type": "boolean"},
			"systems": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, map[string]any{"request_id": "ACCESS-001", "urgent": true, "systems": []any{"CRM", "Wiki"}}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			e := newEnv(t)
			base := e.eng.Catalog.(*catalog.Static)
			ref := tc.key + ".extract"
			policy := tc.key + ".approval"
			bindings := packs.Bindings{Tenants: map[string]catalog.Snapshot{e.tenant: {
				Agents:        []catalog.Agent{{Ref: ref, Name: "Contract extraction", RuntimeAgentID: ref + "-runtime", SkillVersion: "1.0.0", InputContract: "flowcase.case.v1"}},
				ArtifactTypes: []string{tc.key + ".draft"}, Policies: []catalog.Policy{{Ref: policy, Name: "Independent business fixture review"}},
			}}}
			configPath := filepath.Join(t.TempDir(), "packs.json")
			raw, _ := json.Marshal(bindings)
			if err := os.WriteFile(configPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			configured, _, err := app.NewRegistry(e.pool, configPath)
			if err != nil {
				t.Fatal(err)
			}
			e.eng.Catalog, e.reg = configured.Catalog, configured.Service
			if _, found := base.Agent(ref); found {
				t.Fatal("tenant registration escaped to shared catalog")
			}
			e.exec(`INSERT INTO policy_delegations (tenant_id, policy_ref, user_id) VALUES ($1, $2, $3)`, e.tenant, policy, e.approver.UserID)
			studio := &studioServer{runs: map[string]string{}, polls: map[string]int{}}
			srv := httptest.NewServer(studio)
			defer srv.Close()
			client, err := agent.NewAgentStudio(agent.AgentStudioConfig{BaseURL: srv.URL, ServiceToken: studioToken, CallbackSecret: string(secret), Timeout: 5 * time.Second, ResolveSubject: app.DirectorySubject(e.pool)})
			if err != nil {
				t.Fatal(err)
			}
			e.phase3(client)
			cpUser := "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
			e.exec("UPDATE users SET subject=$2 WHERE id=$1", e.owner.UserID, cpUser)
			tc.properties["kms_source_ids"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
			doc := cases.RawJSON(map[string]any{"schema_version": "1.0", "key": tc.key, "name": "Typed input fixture", "version": 1,
				"input_schema": map[string]any{"type": "object", "additionalProperties": false, "properties": tc.properties},
				"policy_refs":  []any{policy}, "nodes": []any{
					map[string]any{"id": "start", "type": "start", "name": "Start", "next": []any{"extract"}},
					map[string]any{"id": "extract", "type": "agent_task", "name": "Extract", "config": map[string]any{"agent_ref": ref, "output_artifact_type": tc.key + ".draft", "author_review": true}, "next": []any{"review"}},
					map[string]any{"id": "review", "type": "human_approval", "name": "Independent review", "config": map[string]any{"policy_ref": policy}, "next": []any{"end"}},
					map[string]any{"id": "end", "type": "end", "name": "End", "next": []any{}},
				}})
			designer := e.user("business-designer", auth.KindHuman, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher)
			foreign := designer
			foreign.TenantID = "00000000-0000-4000-8000-000000000099"
			if report, err := e.reg.ValidateDocument(e.ctx, foreign, doc); err != nil || report.Valid {
				t.Fatalf("foreign tenant accepted business references: %+v %v", report, err)
			}
			created, err := e.reg.CreateDefinition(e.ctx, designer, registry.Idempotency{Key: tc.key, Hash: h(tc.key)}, registry.CreateInput{Key: tc.key, Definition: doc})
			if err != nil || !created.Report.Valid {
				t.Fatalf("create definition: %v %+v", err, created.Report)
			}
			if _, err = e.reg.Publish(e.ctx, designer, tc.key, 1, created.Version.VersionNum); err != nil {
				t.Fatal(err)
			}
			tc.vars["kms_source_ids"] = []any{"s-ok", "s-private", "s-foreign"}
			id := e.createCase(tc.key, tc.vars)
			// Simulate unrelated internal state; only declared business fields may cross the boundary.
			e.exec(`UPDATE cases SET variables=variables||'{"internal_private":"must not leave case"}'::jsonb WHERE id=$1`, id)
			d := e.advance(id, "extract")
			draft := pendingDraft(d)
			if draft == nil || d.Status == cases.CaseCompleted {
				t.Fatal("model output skipped author review")
			}
			studio.mu.Lock()
			if len(studio.requests) != 1 {
				t.Fatalf("expected one run, got %d", len(studio.requests))
			}
			req := studio.requests[0]
			studio.mu.Unlock()
			var runtimeID, skillVersion string
			_ = json.Unmarshal(req["agent_id"], &runtimeID)
			_ = json.Unmarshal(req["skill_version"], &skillVersion)
			if runtimeID != ref+"-runtime" || skillVersion != "1.0.0" {
				t.Fatal("runtime ignored the tenant's explicit agent/version binding")
			}
			var input struct {
				CaseData       json.RawMessage `json:"case_data"`
				EvidencePolicy string          `json:"evidence_policy"`
			}
			if err = json.Unmarshal(req["input"], &input); err != nil {
				t.Fatal(err)
			}
			expected := map[string]any{}
			for k, v := range tc.vars {
				if k != "kms_source_ids" {
					expected[k] = v
				}
			}
			want, _ := agent.OutputChecksum(cases.RawJSON(expected))
			got, _ := agent.OutputChecksum(input.CaseData)
			if got != want || input.EvidencePolicy != cases.EvidencePolicy || bytes.Contains(req["input"], []byte("internal_private")) || bytes.Contains(req["input"], []byte("kms_source_ids")) {
				t.Fatalf("business envelope changed or leaked state: %s", req["input"])
			}
			var evidence []agent.Evidence
			_ = json.Unmarshal(req["evidence"], &evidence)
			if len(evidence) != 1 || evidence[0].SourceID != "s-ok" {
				t.Fatalf("unauthorized evidence passed: %+v", evidence)
			}
			var delegated string
			_ = json.Unmarshal(req["delegated_user"], &delegated)
			if delegated != cpUser {
				t.Fatal("case owner delegation lost")
			}
			if err = e.eng.SubmitDraft(e.ctx, e.owner, draft.ID, draft.VersionNum, "Reviewed fixture"); err != nil {
				t.Fatal(err)
			}
			e.drain()
			approval := pendingApproval(t, e.detail(id))
			if approval.PolicyName != "Independent business fixture review" || !approval.Provisional {
				t.Fatal("tenant policy metadata or provisional status lost")
			}
			if err = e.eng.Decide(e.ctx, e.outsider, approval.ID, approval.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "No delegation"}); err == nil {
				t.Fatal("catalog registration granted approval authority")
			}
			if err = e.eng.Decide(e.ctx, e.owner, approval.ID, approval.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "Owner must not approve"}); err == nil {
				t.Fatal("case owner bypassed segregation of duties")
			}
			if err = e.eng.Decide(e.ctx, e.approver, approval.ID, approval.VersionNum, cases.DecisionInput{Decision: "approve", Rationale: "Independent fixture review"}); err != nil {
				t.Fatal(err)
			}
			e.drain()
			if e.detail(id).Status != cases.CaseCompleted {
				t.Fatal("independent decision did not complete")
			}
			var contract string
			if err = e.pool.QueryRow(e.ctx, `SELECT data->>'input_contract' FROM audit_events WHERE tenant_id=$1 AND action='agent.started' ORDER BY seq DESC LIMIT 1`, e.tenant).Scan(&contract); err != nil || contract != "flowcase.case.v1" {
				t.Fatalf("contract provenance not retained: %q %v", contract, err)
			}
			// A removed deployment registration must not fall back to an arbitrary runtime key.
			second := e.createCase(tc.key, tc.vars)
			e.eng.Catalog = base
			e.drain()
			studio.mu.Lock()
			count := len(studio.requests)
			studio.mu.Unlock()
			if count != 1 {
				t.Fatal("removed tenant registration still reached Agent runtime")
			}
			var state string
			if err = e.pool.QueryRow(e.ctx, `SELECT status FROM node_executions WHERE case_id=$1 AND node_id='extract'`, second).Scan(&state); err != nil || state != "failed" {
				t.Fatalf("missing registration did not fail closed: %s %v", state, err)
			}
		})
	}
}
