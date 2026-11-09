package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const maxProbeResponseBytes = 1 << 20

// OpenAICompatibleProbe performs an endpoint-specific observation of the
// OpenAI-compatible API surface. It never executes a tool call returned by a
// model. The supplied client must come from ProviderEndpointPolicy.NewClient.
//
// Protocol references:
// https://developers.openai.com/api/reference/resources/models/methods/list
// https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create
type OpenAICompatibleProbe struct{}

func NewOpenAICompatibleProbe() ProviderProbe { return OpenAICompatibleProbe{} }

func (OpenAICompatibleProbe) Probe(ctx context.Context, input ProviderProbeInput, credential []byte, client *http.Client) (ProviderProbeResult, error) {
	if input.AdapterType != "openai_compatible" {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "unsupported_capability"}
	}
	if input.Mode != "connectivity" && input.Mode != "capabilities" {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "unsupported_capability"}
	}
	if input.Mode == "connectivity" && len(input.RequestedCapabilities) != 0 {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "unsupported_capability"}
	}
	if client == nil || len(credential) == 0 || strings.ContainsAny(string(credential), "\r\n") {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "authentication_failed"}
	}
	if input.ProviderModelID == "" {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "model_unavailable"}
	}

	modelsURL, err := probeEndpointURL(input.BaseURL, "models")
	if err != nil {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "invalid_response"}
	}
	modelsResponse, err := sendProbeRequest(ctx, client, http.MethodGet, modelsURL, credential, nil)
	if err != nil {
		return ProviderProbeResult{}, classifyProbeTransportError(err)
	}
	defer modelsResponse.Body.Close()
	if modelsResponse.StatusCode != http.StatusOK {
		return ProviderProbeResult{}, classifyProbeHTTPStatus(modelsResponse.StatusCode)
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := decodeProbeJSON(modelsResponse.Body, &catalog); err != nil {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "invalid_response"}
	}
	modelAvailable := false
	for _, item := range catalog.Data {
		if item.ID == input.ProviderModelID {
			modelAvailable = true
			break
		}
	}
	if !modelAvailable {
		return ProviderProbeResult{}, &ProviderProbeError{Code: "model_unavailable"}
	}

	result := ProviderProbeResult{
		Capabilities:      map[string]bool{"catalog": true},
		Evidence:          map[string]string{"adapter": "openai_compatible", "scope": "provider_endpoint", "observation": "models_list", "mode": input.Mode},
		MayIncurTokenCost: false,
	}
	if input.Mode == "connectivity" {
		result.Evidence["endpoint"] = sanitizedProbeEndpoint(input.BaseURL)
		result.Evidence["model_id"] = input.ProviderModelID
		return result, nil
	}
	for _, capability := range []string{"json", "tools"} {
		if !input.RequestedCapabilities[capability] {
			continue
		}
		completionURL, err := probeEndpointURL(input.BaseURL, "chat/completions")
		if err != nil {
			return ProviderProbeResult{}, &ProviderProbeError{Code: "invalid_response"}
		}
		payload := completionProbePayload(input.ProviderModelID, capability)
		response, err := sendProbeRequest(ctx, client, http.MethodPost, completionURL, credential, payload)
		result.MayIncurTokenCost = true
		if err != nil {
			return result, classifyProbeTransportError(err)
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			return result, classifyProbeHTTPStatus(response.StatusCode)
		}
		var completion chatCompletionProbeResponse
		decodeErr := decodeProbeJSON(response.Body, &completion)
		_ = response.Body.Close()
		if decodeErr != nil {
			return result, &ProviderProbeError{Code: "invalid_response"}
		}
		if err := verifyCompletionProbe(completion, capability); err != nil {
			return result, err
		}
		result.Capabilities[capability] = true
		result.Evidence[capability] = "observed"
	}
	result.Evidence["model_id"] = input.ProviderModelID
	result.Evidence["endpoint"] = sanitizedProbeEndpoint(input.BaseURL)
	result.Evidence["observation"] = "models_list_and_requested_chat_completions"
	return result, nil
}

func probeEndpointURL(baseURL, suffix string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid provider base URL")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + suffix
	u.RawPath = ""
	return u.String(), nil
}

func sanitizedProbeEndpoint(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "provider_endpoint"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return strings.TrimRight(u.String(), "/")
}

func sendProbeRequest(ctx context.Context, client *http.Client, method, endpoint string, credential []byte, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+string(credential))
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > maxProbeResponseBytes {
		_ = response.Body.Close()
		return nil, &ProviderProbeError{Code: "invalid_response"}
	}
	return response, nil
}

func decodeProbeJSON(body io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxProbeResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxProbeResponseBytes {
		return errors.New("provider response exceeds the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("provider response is not one JSON document")
	}
	return nil
}

type chatCompletionProbeResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string `json:"content"`
			Refusal   string `json:"refusal"`
			ToolCalls []struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func completionProbePayload(model, capability string) map[string]any {
	payload := map[string]any{
		"model": model, "n": 1, "stream": false, "store": false,
		"max_completion_tokens": 24,
	}
	if capability == "json" {
		payload["response_format"] = map[string]string{"type": "json_object"}
		payload["messages"] = []map[string]string{{"role": "user", "content": "Return exactly the JSON object {\"agent_studio_probe\":true}."}}
		return payload
	}
	payload["tools"] = []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":        "agent_studio_probe_noop",
			"description": "A harmless capability probe. Do not perform any action.",
			"parameters": map[string]any{
				"type": "object", "properties": map[string]any{"ok": map[string]string{"type": "boolean"}},
				"required": []string{"ok"}, "additionalProperties": false,
			},
		},
	}}
	payload["tool_choice"] = "required"
	payload["messages"] = []map[string]string{{"role": "user", "content": "Call the agent_studio_probe_noop tool with {\"ok\":true}."}}
	return payload
}

func verifyCompletionProbe(response chatCompletionProbeResponse, capability string) error {
	if len(response.Choices) == 0 {
		return &ProviderProbeError{Code: "invalid_response"}
	}
	choice := response.Choices[0]
	if strings.TrimSpace(choice.Message.Refusal) != "" {
		return &ProviderProbeError{Code: "refusal"}
	}
	if choice.FinishReason == "length" {
		return &ProviderProbeError{Code: "truncated"}
	}
	if capability == "json" {
		if choice.FinishReason != "stop" {
			return &ProviderProbeError{Code: "unsupported_capability"}
		}
		var value map[string]json.RawMessage
		if json.Unmarshal([]byte(choice.Message.Content), &value) != nil || value == nil {
			return &ProviderProbeError{Code: "unsupported_capability"}
		}
		return nil
	}
	if choice.FinishReason != "tool_calls" {
		return &ProviderProbeError{Code: "unsupported_capability"}
	}
	for _, call := range choice.Message.ToolCalls {
		if call.Type == "function" && call.Function.Name == "agent_studio_probe_noop" {
			return nil
		}
	}
	return &ProviderProbeError{Code: "unsupported_capability"}
}

func classifyProbeHTTPStatus(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &ProviderProbeError{Code: "authentication_failed"}
	case http.StatusTooManyRequests:
		return &ProviderProbeError{Code: "rate_limited"}
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return &ProviderProbeError{Code: "timeout"}
	default:
		if status >= 500 {
			return &ProviderProbeError{Code: "provider_unavailable"}
		}
		return &ProviderProbeError{Code: "unsupported_capability"}
	}
}

func classifyProbeTransportError(err error) error {
	var probeErr *ProviderProbeError
	if errors.As(err, &probeErr) {
		return probeErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ProviderProbeError{Code: "timeout"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &ProviderProbeError{Code: "timeout"}
	}
	return &ProviderProbeError{Code: "provider_unavailable"}
}
