// kms-admin runs operator tasks against the configured PostgreSQL and Elasticsearch: rebuilding the search
// index from canonical data, verifying index drift, re-queueing failed outbox events and printing queue status.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/config"
	"github.com/elenio/knowledge-workspace/backend/internal/ops"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

const reindexBatchSize = 32

var indexNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,200}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cfg, err := config.Load()
	if err != nil {
		fail("invalid configuration: %v", err)
	}
	deps, err := platform.New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		fail("dependency initialization failed: %v", err)
	}
	defer deps.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "reindex":
		reindex(ctx, deps)
	case "verify-index":
		verifyIndex(ctx, deps)
	case "requeue-outbox":
		requeueOutbox(ctx, deps)
	case "status":
		status(ctx, deps)
	default:
		usage()
	}
}

func reindex(ctx context.Context, deps *platform.Dependencies) {
	fs := flag.NewFlagSet("reindex", flag.ExitOnError)
	target := fs.String("target", "", "new physical index to build, e.g. kms_chunks_v2")
	alias := fs.String("alias", "", "optional alias to point at the target once it is complete, e.g. kms_chunks_current")
	_ = fs.Parse(os.Args[2:])
	if !indexNamePattern.MatchString(*target) || (*alias != "" && !indexNamePattern.MatchString(*alias)) || *target == *alias {
		fail("--target (and --alias when given) must be distinct lowercase Elasticsearch index names")
	}
	targets, err := deps.Search.AliasTargets(ctx, *target)
	if err != nil {
		fail("check target index: %v", err)
	}
	if len(targets) > 0 {
		fail("--target %s is an alias; reindex into a physical index", *target)
	}
	client, err := deps.Search.WithIndex(*target)
	if err != nil {
		fail("%v", err)
	}
	if err := client.EnsureIndex(ctx); err != nil {
		fail("create target index: %v", err)
	}
	started := time.Now()
	var batch []platform.IndexedChunk
	var written int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if deps.Embedding != nil {
			texts := make([]string, len(batch))
			for i := range batch {
				texts[i] = batch[i].Content
			}
			vectors, err := deps.Embedding.Embed(ctx, texts)
			if err != nil {
				return fmt.Errorf("embed chunks: %w", err)
			}
			if len(vectors) != len(batch) {
				return fmt.Errorf("embedding provider returned %d vectors for %d chunks", len(vectors), len(batch))
			}
			for i := range batch {
				batch[i].Embedding, batch[i].EmbeddingModelID = vectors[i], deps.Embedding.ModelID()
			}
		}
		if err := client.BulkIndexChunks(ctx, batch); err != nil {
			return err
		}
		written += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	err = ops.ForEachIndexableChunk(ctx, deps.Database.SQL(), "", func(chunk platform.IndexedChunk) error {
		batch = append(batch, chunk)
		if len(batch) >= reindexBatchSize {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		fail("reindex stopped after %d chunks: %v (the run is idempotent; rerun the same command)", written, err)
	}
	if err := client.Refresh(ctx); err != nil {
		fail("refresh target index: %v", err)
	}
	actual, err := client.CountChunks(ctx, "")
	if err != nil {
		fail("count target index: %v", err)
	}
	fmt.Printf("target=%s chunks_written=%d chunks_in_index=%d embeddings=%t duration=%s\n", *target, written, actual, deps.Embedding != nil, time.Since(started).Round(time.Millisecond))
	if actual != written {
		fail("target index holds %d chunks but %d were written; it may contain stale chunks from an earlier run; use a new --target", actual, written)
	}
	if *alias == "" {
		fmt.Printf("next: point ELASTICSEARCH_INDEX at %s (or rerun with --alias) and restart kms-api and kms-worker\n", *target)
		return
	}
	if err := deps.Search.SwapAlias(ctx, *alias, *target); err != nil {
		fail("swap alias: %v", err)
	}
	fmt.Printf("alias=%s now points to %s\n", *alias, *target)
	if deps.Search.IndexName() != *alias {
		fmt.Printf("next: set ELASTICSEARCH_INDEX=%s and restart kms-api and kms-worker so writes follow the alias\n", *alias)
	}
}

func verifyIndex(ctx context.Context, deps *platform.Dependencies) {
	fs := flag.NewFlagSet("verify-index", flag.ExitOnError)
	tenant := fs.String("tenant-id", "", "optional tenant UUID")
	_ = fs.Parse(os.Args[2:])
	requireOptionalUUID(*tenant)
	expected, err := ops.ExpectedIndexedChunks(ctx, deps.Database.SQL(), *tenant)
	if err != nil {
		fail("count canonical chunks: %v", err)
	}
	actual, err := deps.Search.CountChunks(ctx, *tenant)
	if err != nil {
		fail("count indexed chunks: %v", err)
	}
	fmt.Printf("index=%s expected_chunks=%d indexed_chunks=%d in_sync=%t\n", deps.Search.IndexName(), expected, actual, expected == actual)
	if expected != actual {
		fmt.Println("hint: check outbox backlog with `kms-admin status`; rebuild with `kms-admin reindex --target <new> --alias <alias>` if drift persists")
		os.Exit(1)
	}
}

func requeueOutbox(ctx context.Context, deps *platform.Dependencies) {
	fs := flag.NewFlagSet("requeue-outbox", flag.ExitOnError)
	tenant := fs.String("tenant-id", "", "optional tenant UUID")
	_ = fs.Parse(os.Args[2:])
	requireOptionalUUID(*tenant)
	count, err := ops.RequeueFailedOutbox(ctx, deps.Database.SQL(), *tenant)
	if err != nil {
		fail("requeue failed outbox events: %v", err)
	}
	fmt.Printf("requeued=%d\n", count)
}

func status(ctx context.Context, deps *platform.Dependencies) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	tenant := fs.String("tenant-id", "", "optional tenant UUID")
	_ = fs.Parse(os.Args[2:])
	requireOptionalUUID(*tenant)
	result, err := ops.LoadStatus(ctx, deps.Database.SQL(), *tenant)
	if err != nil {
		fail("load status: %v", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(result)
}

func requireOptionalUUID(value string) {
	if value != "" && !uuidPattern.MatchString(value) {
		fail("--tenant-id must be a UUID")
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "kms-admin: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  kms-admin reindex --target INDEX [--alias ALIAS]   rebuild search index from PostgreSQL (re-embeds when configured)
  kms-admin verify-index [--tenant-id UUID]          compare canonical and indexed chunk counts
  kms-admin requeue-outbox [--tenant-id UUID]        retry failed index/storage cleanup events
  kms-admin status [--tenant-id UUID]                print ingestion, outbox and review backlog`)
	os.Exit(2)
}
