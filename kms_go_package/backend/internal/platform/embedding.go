package platform

import "context"

// EmbeddingProvider is intentionally provider-neutral. Dimensions/model must be configured before ES index creation.
type EmbeddingProvider interface {
	ModelID() string
	Dimensions() int
	Embed(context.Context, []string) ([][]float32, error)
}

func EmbeddingStatus(provider EmbeddingProvider) string {
	if provider == nil {
		return "disabled"
	}
	return "configured"
}
