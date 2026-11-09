package platform

import (
	"testing"

	"github.com/elenio/knowledge-workspace/backend/internal/config"
)

func TestUnconfiguredEmbeddingProviderIsNilInterface(t *testing.T) {
	provider, err := NewHTTPEmbeddingProvider(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if provider != nil {
		t.Fatalf("unconfigured embedding provider should be nil, got %#v", provider)
	}
	if got := EmbeddingStatus(provider); got != "disabled" {
		t.Fatalf("readiness state=%q, want disabled", got)
	}
}
