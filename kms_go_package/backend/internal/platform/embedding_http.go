package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/config"
)

type HTTPEmbeddingProvider struct {
	endpoint string
	apiKey   string
	model    string
	dims     int
	client   *http.Client
}

func NewHTTPEmbeddingProvider(cfg config.Config) (EmbeddingProvider, error) {
	if cfg.EmbeddingEndpoint == "" {
		return nil, nil
	}
	return &HTTPEmbeddingProvider{
		endpoint: cfg.EmbeddingEndpoint,
		apiKey:   cfg.EmbeddingAPIKey,
		model:    cfg.EmbeddingModel,
		dims:     cfg.EmbeddingDimensions,
		client:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (p *HTTPEmbeddingProvider) ModelID() string { return p.model }
func (p *HTTPEmbeddingProvider) Dimensions() int { return p.dims }

func (p *HTTPEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if p == nil {
		return nil, ErrEmbeddingUnavailable
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("embedding input must not be empty")
	}
	payload, err := json.Marshal(map[string]any{"model": p.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding endpoint returned %s", resp.Status)
	}
	var result struct {
		Model string `json:"model"`
		Data  []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 32<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if result.Model != "" && result.Model != p.model {
		return nil, fmt.Errorf("embedding response model %q does not match configured model %q", result.Model, p.model)
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("embedding response count %d does not match input count %d", len(result.Data), len(texts))
	}
	vectors := make([][]float32, len(texts))
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(texts) || len(item.Embedding) != p.dims {
			return nil, fmt.Errorf("embedding response index or vector dimension is invalid")
		}
		for _, value := range item.Embedding {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, fmt.Errorf("embedding response contains a non-finite value")
			}
		}
		if vectors[item.Index] != nil {
			return nil, fmt.Errorf("embedding response contains duplicate indexes")
		}
		vectors[item.Index] = item.Embedding
	}
	for i := range vectors {
		if vectors[i] == nil {
			return nil, fmt.Errorf("embedding response is missing index %d", i)
		}
	}
	return vectors, nil
}

var ErrEmbeddingUnavailable = fmt.Errorf("embedding provider is not configured")

func IsEmbeddingUnavailable(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrEmbeddingUnavailable.Error())
}
