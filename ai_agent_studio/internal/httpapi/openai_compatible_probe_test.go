package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAICompatibleProbeUsesBoundedSeparateCalls(t *testing.T) {
	var requests atomic.Int32
	server := newProbeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-credential" {
			t.Errorf("authorization header was not set correctly")
		}
		switch r.URL.Path {
		case "/v1/models":
			if r.Method != http.MethodGet {
				t.Errorf("models request method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"compatible-model"}]}`))
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode completion request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if body["model"] != "compatible-model" || body["n"] != float64(1) || body["stream"] != false || body["store"] != false || body["max_completion_tokens"].(float64) > 24 {
				t.Errorf("completion probe was not bounded: %#v", body)
			}
			if body["response_format"] == nil || body["tools"] != nil {
				t.Errorf("JSON capability request did not use JSON mode alone: %#v", body)
			}
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"agent_studio_probe\":true}"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	probe := NewOpenAICompatibleProbe()
	result, err := probe.Probe(context.Background(), ProviderProbeInput{
		AdapterType: "openai_compatible", BaseURL: server.URL + "/v1", ProviderModelID: "compatible-model", Mode: "capabilities",
		RequestedCapabilities: map[string]bool{"json": true},
	}, []byte("test-credential"), server.Client())
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if !result.Capabilities["json"] || !result.MayIncurTokenCost || result.Evidence["scope"] != "provider_endpoint" || requests.Load() != 2 {
		t.Fatalf("unexpected probe result or call count: result=%+v requests=%d", result, requests.Load())
	}
}

func TestOpenAICompatibleConnectivityProbeUsesCatalogOnly(t *testing.T) {
	var calls atomic.Int32
	server := newProbeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("connectivity probe made unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"catalog-model"}]}`))
	}))
	defer server.Close()

	result, err := NewOpenAICompatibleProbe().Probe(context.Background(), ProviderProbeInput{
		AdapterType: "openai_compatible", BaseURL: server.URL + "/v1", ProviderModelID: "catalog-model",
		Mode: "connectivity", RequestedCapabilities: map[string]bool{},
	}, []byte("test-credential"), server.Client())
	if err != nil {
		t.Fatalf("connection-only probe failed: %v", err)
	}
	if !result.Capabilities["catalog"] || result.MayIncurTokenCost || calls.Load() != 1 || result.Evidence["scope"] != "provider_endpoint" {
		t.Fatalf("unexpected connection-only probe result: %+v calls=%d", result, calls.Load())
	}
}

func TestOpenAICompatibleToolProbeNeverExecutesReturnedTool(t *testing.T) {
	var chatCalls atomic.Int32
	server := newProbeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"compatible-model"}]}`))
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected endpoint called: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		chatCalls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode completion request: %v", err)
			return
		}
		if body["tool_choice"] != "required" || body["tools"] == nil || body["n"] != float64(1) || body["store"] != false || body["stream"] != false {
			t.Errorf("tool probe request is not bounded/explicit: %#v", body)
		}
		if maxTokens, ok := body["max_completion_tokens"].(float64); !ok || maxTokens < 1 || maxTokens > 24 {
			t.Errorf("tool probe token cap is missing or too large: %#v", body["max_completion_tokens"])
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"type":"function","function":{"name":"agent_studio_probe_noop","arguments":"{\"ok\":true}"}}]}}]}`))
	}))
	defer server.Close()
	result, err := NewOpenAICompatibleProbe().Probe(context.Background(), ProviderProbeInput{
		AdapterType: "openai_compatible", BaseURL: server.URL + "/v1", ProviderModelID: "compatible-model", Mode: "capabilities",
		RequestedCapabilities: map[string]bool{"tools": true},
	}, []byte("test-credential"), server.Client())
	if err != nil {
		t.Fatalf("tool probe failed: %v", err)
	}
	if !result.Capabilities["tools"] || chatCalls.Load() != 1 {
		t.Fatalf("tool call was not merely observed: result=%+v chat_calls=%d", result, chatCalls.Load())
	}
}

func TestOpenAICompatibleProbeClassifiesFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		delay      time.Duration
		completion string
		wantCode   string
	}{
		{name: "authentication", status: http.StatusUnauthorized, wantCode: "authentication_failed"},
		{name: "rate limit", status: http.StatusTooManyRequests, wantCode: "rate_limited"},
		{name: "timeout", delay: 40 * time.Millisecond, wantCode: "timeout"},
		{name: "refusal", completion: `{"choices":[{"finish_reason":"stop","message":{"refusal":"not available"}}]}`, wantCode: "refusal"},
		{name: "truncation", completion: `{"choices":[{"finish_reason":"length","message":{"content":"{"}}]}`, wantCode: "truncated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newProbeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/models" {
					if test.delay > 0 {
						time.Sleep(test.delay)
						return
					}
					if test.status != 0 {
						w.WriteHeader(test.status)
						return
					}
					_, _ = w.Write([]byte(`{"data":[{"id":"compatible-model"}]}`))
					return
				}
				_, _ = w.Write([]byte(test.completion))
			}))
			defer server.Close()
			client := server.Client()
			if test.delay > 0 {
				client.Timeout = 10 * time.Millisecond
			}
			_, err := NewOpenAICompatibleProbe().Probe(context.Background(), ProviderProbeInput{
				AdapterType: "openai_compatible", BaseURL: server.URL + "/v1", ProviderModelID: "compatible-model", Mode: "capabilities",
				RequestedCapabilities: map[string]bool{"json": true},
			}, []byte("test-credential"), client)
			if err == nil {
				t.Fatal("expected the probe to fail")
			}
			if code := providerProbeErrorCode(err); code != test.wantCode {
				t.Fatalf("error code = %q, want %q (err %v)", code, test.wantCode, err)
			}
		})
	}
}

func TestProviderProbeErrorDoesNotExposeProviderText(t *testing.T) {
	err := &ProviderProbeError{Code: "rate_limited"}
	if strings.Contains(err.Error(), "test-credential") || providerProbeErrorCode(err) != "rate_limited" {
		t.Fatalf("probe error was not safely classified: %v", err)
	}
}

func newProbeTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener is unavailable in this environment: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}
