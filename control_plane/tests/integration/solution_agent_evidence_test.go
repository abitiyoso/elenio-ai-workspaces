package integration

import (
	"context"
	"encoding/json"
	"fmt"
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

func hashSourceMetadata(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := solutions.CanonicalSnapshotHash(raw)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestPublisherAgentEvidenceUsesIndependentSourceAuthorityAndRetainsCandidates(t *testing.T) {
	var e *env
	var mode atomic.Value
	mode.Store("valid")
	var sourceCalls, redirects atomic.Int64
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
	defer trap.Close()
	trusted := func(w http.ResponseWriter, r *http.Request, audience string) bool {
		if e == nil {
			w.WriteHeader(503)
			return false
		}
		if r.Method != "GET" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("source received mutation/browser identity")
		}
		token, err := jwt.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), func(*jwt.Token) (any, error) { return e.tokens.PublicKey(), nil }, jwt.WithAudience(audience), jwt.WithIssuer(e.cfg.PublicBaseURL.String()), jwt.WithValidMethods([]string{"ES256"}))
		if err != nil || !token.Valid {
			t.Error("wrong audience/identity")
			w.WriteHeader(401)
			return false
		}
		if token.Claims.(jwt.MapClaims)["sub"] != e.userID(subRafi) {
			t.Error("caller replaced")
		}
		return true
	}
	workflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !trusted(w, r, "flowcase") {
			return
		}
		nodes := []any{map[string]any{"id": "start", "type": "start"}}
		bindings := []map[string]any{}
		count := 2
		if mode.Load() == "many" {
			count = 21
		}
		for i := 0; i < count; i++ {
			ref := "finance.extract"
			if mode.Load() == "many" {
				ref = fmt.Sprintf("finance.extract.%d", i)
			}
			node := fmt.Sprintf("extract-%d", i)
			nodes = append(nodes, map[string]any{"id": node, "type": "agent_task", "config": map[string]any{"agent_ref": ref}})
			bindings = append(bindings, map[string]any{"kind": "agent", "ref": ref, "node_id": node, "registered": true, "runtime_agent_id": ref, "skill_version": "1.0.0", "input_contract": "flowcase.case.v1"})
		}
		nodes = append(nodes, map[string]any{"id": "end", "type": "end"})
		if strings.HasSuffix(r.URL.Path, "/dependencies") {
			body := map[string]any{"workflow_key": "purchase-review", "version": 1, "definition_hash": strings.Repeat("a", 64), "bindings": bindings}
			body["content_hash"] = hashSourceMetadata(t, body)
			body["schema"] = "elenio.workflow-bindings.v1"
			body["observed_at"] = time.Now().UTC()
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_key": "purchase-review", "version": 1, "status": "published", "definition_hash": strings.Repeat("a", 64), "definition": map[string]any{"schema_version": "1.0", "key": "purchase-review", "version": 1, "nodes": nodes}})
	}))
	defer workflow.Close()
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !trusted(w, r, "agent-studio") {
			return
		}
		if r.URL.Path == "/api/v1/releases" {
			if mode.Load() == "denied" {
				w.WriteHeader(403)
				return
			}
			if mode.Load() == "redirect" {
				http.Redirect(w, r, trap.URL, 302)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
			return
		}
		sourceCalls.Add(1)
		if r.URL.Path != "/api/v1/releases/evidence" || len(r.URL.Query()) != 2 || r.URL.Query().Get("skill_version") != "1.0.0" {
			t.Error("source pin/path changed")
			w.WriteHeader(400)
			return
		}
		switch mode.Load().(string) {
		case "denied":
			w.WriteHeader(403)
			return
		case "redirect":
			http.Redirect(w, r, trap.URL, 302)
			return
		case "oversized":
			_, _ = w.Write([]byte(strings.Repeat("x", 129<<10)))
			return
		}
		body := map[string]any{"schema": "elenio.agent-release-evidence.v1", "agent_key": r.URL.Query().Get("agent_key"), "environment": "production",
			"release_id": "00000000-0000-4000-8000-000000000701", "agent_version": map[string]any{"id": "00000000-0000-4000-8000-000000000702", "version": "1.0.0", "checksum": strings.Repeat("b", 64), "state": "published"},
			"fingerprint": strings.Repeat("c", 64), "request_id": "00000000-0000-4000-8000-000000000703", "evaluation_id": "00000000-0000-4000-8000-000000000704", "evaluation_gate": "pass",
			"independent_approval": true, "self_approval_exempt": false, "evaluation_primary_config_matches": true, "primary_model": map[string]any{"id": "00000000-0000-4000-8000-000000000705", "config_version": 3, "approved": true},
			"skills": []any{}, "tools": []any{}, "blockers": []any{}, "state": "verified"}
		if mode.Load() == "blocked" {
			body["state"] = "blocked"
			body["blockers"] = []any{"primary_model_not_currently_approved"}
			body["primary_model"].(map[string]any)["approved"] = false
		}
		if mode.Load() == "wrong pin" {
			body["agent_key"] = "other.agent"
		}
		body["content_hash"] = hashSourceMetadata(t, body)
		body["observed_at"] = time.Now().UTC()
		switch mode.Load().(string) {
		case "tampered":
			body["primary_model"].(map[string]any)["config_version"] = 4
		case "stale":
			body["observed_at"] = time.Now().Add(-2 * time.Minute)
		case "private":
			body["credential_ref"] = "must-not-export"
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer agent.Close()
	e = newEnv(t, envOptions{upstreams: map[string]config.Upstream{
		"flowcase": {URL: workflow.URL, Audience: "flowcase", Mode: "proxy", StripPrefix: true}, "agent-studio": {URL: agent.URL, Audience: "agent-studio", Mode: "proxy", StripPrefix: true},
	}})
	b := e.as(subRafi)
	expectStatus(t, b.do("POST", "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	planBody := solutionPlan("agent-evidence")
	planBody["workflows"] = []map[string]any{{"key": "purchase-review", "version": 1}}
	plan := expectStatus(t, b.do("POST", "/api/v1/solutions", planBody, solutionHeaders("source-agent-create")), 201)
	path := "/api/v1/solutions/" + plan["id"].(string)
	report := expectStatus(t, b.do("GET", path+"/readiness", nil, map[string]string{"X-Tenant-ID": "forged"}), 200)
	observations := report["agent_releases"].([]any)
	if len(observations) != 1 || sourceCalls.Load() != 1 || observations[0].(map[string]any)["evidence"].(map[string]any)["state"] != "verified" || report["release_ready"] != false {
		t.Fatalf("dedup/source scope: %+v calls=%d", observations, sourceCalls.Load())
	}
	candidate := expectStatus(t, b.do("POST", path+"/candidates", map[string]string{"version": "1.0.0"}, candidateHeaders("source-agent-candidate")), 201)
	original := candidate["snapshot"].(map[string]any)["readiness"].(map[string]any)["agent_releases"].([]any)[0].(map[string]any)["evidence"].(map[string]any)["content_hash"]
	for _, invalid := range []string{"denied", "redirect", "oversized", "tampered", "stale", "wrong pin", "private"} {
		mode.Store(invalid)
		checked := expectStatus(t, b.do("GET", path+"/readiness", nil, nil), 200)
		if checked["agent_releases"].([]any)[0].(map[string]any)["evidence"] != nil || checked["release_ready"] != false {
			t.Fatal("invalid source evidence accepted", invalid)
		}
		raw, _ := json.Marshal(checked)
		if strings.Contains(string(raw), "must-not-export") {
			t.Fatal("private source output copied")
		}
	}
	mode.Store("denied")
	expectStatus(t, b.do("GET", path+"/candidates/"+candidate["id"].(string), nil, nil), 403)
	expectStatus(t, b.do("GET", path+"/candidates", nil, nil), 403)
	expectStatus(t, b.do("POST", path+"/candidates", map[string]string{"version": "1.0.0"}, candidateHeaders("source-agent-candidate")), 403)
	mode.Store("blocked")
	negative := expectStatus(t, b.do("GET", path+"/readiness", nil, nil), 200)
	if negative["agent_releases"].([]any)[0].(map[string]any)["evidence"].(map[string]any)["state"] != "blocked" {
		t.Fatal("negative source facts hidden")
	}
	retained := expectStatus(t, b.do("GET", path+"/candidates/"+candidate["id"].(string), nil, nil), 200)
	if retained["snapshot"].(map[string]any)["readiness"].(map[string]any)["agent_releases"].([]any)[0].(map[string]any)["evidence"].(map[string]any)["content_hash"] != original {
		t.Fatal("candidate source facts replaced")
	}
	mode.Store("many")
	before := sourceCalls.Load()
	bounded := expectStatus(t, b.do("GET", path+"/readiness", nil, nil), 200)
	if len(bounded["agent_releases"].([]any)) != 20 || sourceCalls.Load()-before != 20 || !strings.Contains(fmt.Sprint(bounded["blockers"]), "20 distinct") {
		t.Fatal("source request bound not enforced")
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE mini_apps SET status='disabled' WHERE app_key='agent-studio'`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, b.do("GET", path+"/candidates/"+candidate["id"].(string), nil, nil), 403)
	before = sourceCalls.Load()
	disabled := expectStatus(t, b.do("GET", path+"/readiness", nil, nil), 200)
	if disabled["agent_releases"].([]any)[0].(map[string]any)["evidence"] != nil || sourceCalls.Load() != before {
		t.Fatal("app grant revocation bypassed")
	}
	if redirects.Load() != 0 {
		t.Fatal("source redirect followed")
	}
}
