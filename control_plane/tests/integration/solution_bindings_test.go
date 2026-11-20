package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/elenio/control-plane/internal/solutions"
	"github.com/golang-jwt/jwt/v5"
)

func TestSolutionRetainsExactSourceBindingEvidenceAndRejectsInvalidSnapshots(t *testing.T) {
	var e *env
	var mode atomic.Value
	mode.Store("valid")
	var redirects atomic.Int64
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
	defer trap.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("forwarded mutation or browser identity")
		}
		token, err := jwt.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), func(*jwt.Token) (any, error) { return e.tokens.PublicKey(), nil }, jwt.WithAudience("flowcase"), jwt.WithIssuer(e.cfg.PublicBaseURL.String()), jwt.WithValidMethods([]string{"ES256"}))
		if err != nil || !token.Valid {
			w.WriteHeader(401)
			return
		}
		hash := strings.Repeat("a", 64)
		if !strings.HasSuffix(r.URL.Path, "/dependencies") {
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_key": "purchase-review", "version": 1, "status": "published", "definition_hash": hash,
				"definition": map[string]any{"schema_version": "1.0", "key": "purchase-review", "version": 1, "metadata": map[string]string{"secret": "must-not-export"}, "nodes": []any{
					map[string]any{"id": "start", "type": "start"}, map[string]any{"id": "extract", "type": "agent_task", "config": map[string]any{"agent_ref": "finance.extract", "instructions": "must-not-export"}}, map[string]any{"id": "end", "type": "end"},
				}}})
			return
		}
		switch mode.Load().(string) {
		case "redirect":
			http.Redirect(w, r, trap.URL, 302)
			return
		case "denied":
			w.WriteHeader(403)
			return
		case "oversized":
			_, _ = w.Write([]byte(strings.Repeat("x", 170<<10)))
			return
		}
		contract := "flowcase.case.v1"
		if mode.Load() == "changed" {
			contract = "sim.v1"
		}
		bindings := []map[string]any{{"kind": "agent", "ref": "finance.extract", "node_id": "extract", "registered": true, "runtime_agent_id": "finance.extract", "skill_version": "1.0.0", "input_contract": contract}}
		payload := map[string]any{"workflow_key": "purchase-review", "version": 1, "definition_hash": hash, "bindings": bindings}
		raw, _ := json.Marshal(payload)
		contentHash, _ := solutions.CanonicalSnapshotHash(raw)
		payload["schema"] = "elenio.workflow-bindings.v1"
		payload["content_hash"] = contentHash
		payload["observed_at"] = time.Now().UTC()
		switch mode.Load().(string) {
		case "tampered":
			bindings[0]["input_contract"] = "sim.v1"
		case "stale":
			payload["observed_at"] = time.Now().Add(-2 * time.Minute)
		case "unknown field":
			payload["secret"] = "must-not-export"
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer upstream.Close()
	e = newEnv(t, envOptions{upstreams: map[string]config.Upstream{"flowcase": {URL: upstream.URL, Audience: "flowcase", Mode: "proxy", StripPrefix: true}}})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	body := solutionPlan("binding-solution")
	body["workflows"] = []map[string]any{{"key": "purchase-review", "version": 1}}
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", body, solutionHeaders("binding-solution-01")), 201)
	path := "/api/v1/solutions/" + plan["id"].(string)
	candidate := expectStatus(t, b.do("POST", path+"/candidates", map[string]string{"version": "1.0.0"}, candidateHeaders("binding-candidate-01")), 201)
	original := candidate["snapshot"].(map[string]any)["readiness"].(map[string]any)["workflows"].([]any)[0].(map[string]any)["binding_snapshot"].(map[string]any)
	for _, invalid := range []string{"tampered", "stale", "unknown field", "redirect", "denied", "oversized"} {
		mode.Store(invalid)
		report := expectStatus(t, b.do("GET", path+"/readiness", nil, nil), 200)
		workflow := report["workflows"].([]any)[0].(map[string]any)
		if workflow["binding_snapshot"] != nil || workflow["state"] != "verified" || report["release_ready"] != false {
			t.Fatalf("%s accepted or workflow state substituted: %v", invalid, workflow)
		}
		raw, _ := json.Marshal(report)
		if strings.Contains(string(raw), "must-not-export") {
			t.Fatal("untrusted body copied into report")
		}
	}
	mode.Store("changed")
	report := expectStatus(t, b.do("GET", path+"/readiness", nil, nil), 200)
	changed := report["workflows"].([]any)[0].(map[string]any)["binding_snapshot"].(map[string]any)
	if changed["content_hash"] == original["content_hash"] {
		t.Fatal("catalog drift not observed")
	}
	retained := expectStatus(t, b.do("GET", path+"/candidates/"+candidate["id"].(string), nil, nil), 200)
	retainedBinding := retained["snapshot"].(map[string]any)["readiness"].(map[string]any)["workflows"].([]any)[0].(map[string]any)["binding_snapshot"].(map[string]any)
	if retainedBinding["content_hash"] != original["content_hash"] {
		t.Fatal("immutable candidate evidence replaced by later catalog")
	}
	if redirects.Load() != 0 {
		t.Fatal("trusted upstream redirect followed")
	}
}
