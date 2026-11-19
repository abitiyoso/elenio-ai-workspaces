// provider-check exercises the existing allowlisted OpenAI-compatible probe.
// It reads only a process environment credential; it never emits provider bodies or secrets.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/elenio/agent-studio/internal/httpapi"
	"github.com/elenio/agent-studio/internal/repository/postgres"
	agentruntime "github.com/elenio/agent-studio/internal/runtime"
)

func main() {
	runtimeCheck := flag.Bool("runtime", false, "also execute one synthetic, tool-free agent through the existing Eino runtime")
	flag.Parse()
	key := []byte(os.Getenv("NINE_ROUTER_API_KEY"))
	_ = os.Unsetenv("NINE_ROUTER_API_KEY")
	if len(key) == 0 {
		fmt.Println(`{"ok":false,"code":"credential_missing"}`)
		os.Exit(1)
	}
	defer clear(key)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	policy, err := httpapi.NewProviderEndpointPolicy([]string{"9router.elenio.id"})
	if err != nil {
		fail("endpoint_policy")
	}
	client, err := policy.NewClient(ctx, "https://9router.elenio.id/v1")
	if err != nil {
		fail("endpoint_unavailable")
	}
	defer client.CloseIdleConnections()
	// Reasoning models can take longer than the generic health-check timeout.
	client.Timeout = 80 * time.Second
	result, err := httpapi.NewOpenAICompatibleProbe().Probe(ctx, httpapi.ProviderProbeInput{AdapterType: "openai_compatible", BaseURL: "https://9router.elenio.id/v1", ProviderModelID: "cx/gpt-6-luna", Mode: "capabilities", RequestedCapabilities: map[string]bool{"json": true}}, key, client)
	if err != nil {
		if e, ok := err.(*httpapi.ProviderProbeError); ok {
			fail(e.Code)
		}
		fail("probe_failed")
	}
	evidence := map[string]any{"ok": true, "provider": "9Router Elenio", "model": "cx/gpt-6-luna", "api": "Chat Completions", "result": result, "production_approved": false}
	if *runtimeCheck {
		// This command is a synthetic connectivity diagnostic, never a production
		// registry, release or credential-vault substitute for the API/worker.
		executor := agentruntime.Executor{Vault: diagnosticCredential{key: key}, Egress: policy}
		out, err := executor.Execute(ctx, diagnosticExecution(), nil)
		if err != nil || !out.Validation.SchemaValid || (out.Status != "succeeded" && out.Status != "needs_review") {
			fail("runtime_check_failed")
		}
		var reply struct {
			Connected bool `json:"connected"`
		}
		if json.Unmarshal(out.Output, &reply) != nil || !reply.Connected || out.ToolCalls != 0 {
			fail("runtime_output_invalid")
		}
		evidence["runtime"] = map[string]any{"adapter": "existing_eino_executor", "status": out.Status, "schema_valid": out.Validation.SchemaValid, "tools_called": out.ToolCalls, "tokens_in": out.TokensIn, "tokens_out": out.TokensOut, "synthetic_input_only": true}
	}
	_ = json.NewEncoder(os.Stdout).Encode(evidence)
}

type diagnosticCredential struct{ key []byte }

func (v diagnosticCredential) ResolveScoped(ctx context.Context, reference, scope string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if reference != "diagnostic:nine-router" || scope != "provider:nine-router-diagnostic" {
		return nil, errors.New("diagnostic credential scope denied")
	}
	return append([]byte(nil), v.key...), nil
}

func diagnosticExecution() postgres.RunExecution {
	return postgres.RunExecution{
		Run:     postgres.Run{ID: "nine-router-diagnostic", Kind: "sandbox", ModelMode: "provider", Input: json.RawMessage(`{"purpose":"synthetic connectivity check"}`), EvidenceInput: json.RawMessage(`[]`), DeadlineAt: time.Now().Add(90 * time.Second)},
		Agent:   postgres.Agent{Key: "connectivity.check", DisplayName: "Connectivity Check", Domain: "diagnostics"},
		Version: postgres.AgentVersion{Manifest: json.RawMessage(`{"max_iterations":1,"instructions":"Return exactly {\"connected\":true}. This is a synthetic connectivity diagnostic, with no business claims, evidence, external tools or mutations."}`), InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object","properties":{"connected":{"type":"boolean","const":true}},"required":["connected"],"additionalProperties":false}`)},
		Model:   &postgres.ModelTarget{ProviderID: "nine-router-diagnostic", AdapterType: "openai_compatible", BaseURL: "https://9router.elenio.id/v1", ProviderModelID: "cx/gpt-6-luna", SecretRef: "diagnostic:nine-router", Capabilities: json.RawMessage(`{"capabilities":{"json":true}}`), MaxOutputTokens: 256, TimeoutMillis: 80000},
	}
}
func fail(code string) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": false, "code": code})
	os.Exit(1)
}
