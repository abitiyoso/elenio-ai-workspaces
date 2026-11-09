package platform

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/elenio/knowledge-workspace/backend/internal/config"
)

type Dependencies struct {
	Database    *Database
	Search      *SearchClient
	ObjectStore ObjectStore
	Embedding   EmbeddingProvider
	Logger      *slog.Logger
}

func New(cfg config.Config, logger *slog.Logger) (*Dependencies, error) {
	db, err := OpenDatabase(context.Background(), cfg.PostgresURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	search, err := NewSearchClient(cfg.ElasticsearchURL, cfg.ElasticsearchIndex, cfg.EmbeddingDimensions)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure Elasticsearch: %w", err)
	}
	objectStore, err := NewS3ObjectStore(context.Background(), cfg)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure object storage: %w", err)
	}
	embedding, err := NewHTTPEmbeddingProvider(cfg)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure embedding provider: %w", err)
	}
	return &Dependencies{Database: db, Search: search, ObjectStore: objectStore, Embedding: embedding, Logger: logger}, nil
}

func (d *Dependencies) Close() {
	if d != nil && d.Database != nil {
		_ = d.Database.Close()
	}
}
