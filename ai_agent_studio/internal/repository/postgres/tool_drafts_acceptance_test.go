package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elenio/agent-studio/internal/repository/postgres"
)

func TestAcceptanceTenantToolDraftsStayIsolatedAndUnpublished(t *testing.T) {
	db, registry := acceptanceRegistry(t)
	tenantA, tenantB := acceptanceUUID(t), acceptanceUUID(t)
	toolKey := "test." + strings.ReplaceAll(acceptanceUUID(t), "-", "")[:20]

	tool, err := registry.CreateToolDraft(context.Background(), tenantA, postgres.ToolDraftInput{
		Key: toolKey, DisplayName: "Acceptance Tool", Kind: "read_only", RiskLevel: "low", CreatedBy: "tenant-a-user",
	})
	if err != nil {
		t.Fatalf("create tool draft: %v", err)
	}
	version, err := registry.CreateToolDraftVersion(context.Background(), tenantA, tool.ID, postgres.ToolDraftVersionInput{
		Semver: "0.1.0", AdapterRef: "kms.search", InputSchema: json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`), Policy: json.RawMessage(`{"read_only":true}`), CreatedBy: "tenant-a-user",
	})
	if err != nil {
		t.Fatalf("create tool draft version: %v", err)
	}
	if version.State != "draft" || version.Checksum == "" {
		t.Fatalf("expected checksummed, unexecutable draft; got state=%q checksum=%q", version.State, version.Checksum)
	}

	visible, total, err := registry.ListToolDrafts(context.Background(), tenantB, 25, 0)
	if err != nil {
		t.Fatalf("list from tenant B: %v", err)
	}
	if total != 0 || len(visible) != 0 {
		t.Fatalf("tenant B saw tenant A tool drafts: total=%d items=%d", total, len(visible))
	}
	if _, err := registry.UpdateToolDraft(context.Background(), tenantB, tool.ID, postgres.ToolDraftInput{
		Key: toolKey, DisplayName: "Tenant B overwrite", Kind: "read_only", RiskLevel: "low",
	}, "tenant-b-user"); err == nil {
		t.Fatal("tenant B updated tenant A tool draft")
	}
	if _, err := registry.UpdateToolDraftVersion(context.Background(), tenantB, version.ID, postgres.ToolDraftVersionInput{
		Semver: "0.1.1", AdapterRef: "kms.search", InputSchema: json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`), Policy: json.RawMessage(`{"read_only":true}`),
	}, "tenant-b-user"); err == nil {
		t.Fatal("tenant B updated tenant A tool draft version")
	}

	ownerTool, err := registry.GetToolDraft(context.Background(), tenantA, tool.ID)
	if err != nil || ownerTool.DisplayName != "Acceptance Tool" {
		t.Fatalf("tenant A tool draft changed after cross-tenant attempts: item=%+v err=%v", ownerTool, err)
	}
	ownerVersions, total, err := registry.ListToolDraftVersions(context.Background(), tenantA, tool.ID, 25, 0)
	if err != nil || total != 1 || len(ownerVersions) != 1 || ownerVersions[0].ID != version.ID || ownerVersions[0].State != "draft" {
		t.Fatalf("tenant A version changed or became executable: versions=%+v total=%d err=%v", ownerVersions, total, err)
	}

	var globalCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM tools WHERE tool_key=$1`, toolKey).Scan(&globalCount); err != nil {
		t.Fatal(err)
	}
	if globalCount != 0 {
		t.Fatalf("tenant tool draft leaked into platform-global executable catalog: count=%d", globalCount)
	}
	if err := registry.ArchiveToolDraft(context.Background(), tenantA, tool.ID, "tenant-a-user"); err != nil {
		t.Fatalf("archive tool draft: %v", err)
	}
	if _, err := registry.UpdateToolDraftVersion(context.Background(), tenantA, version.ID, postgres.ToolDraftVersionInput{
		Semver: "0.1.1", AdapterRef: "kms.search", InputSchema: json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`), Policy: json.RawMessage(`{"read_only":true}`),
	}, "tenant-a-user"); err == nil {
		t.Fatal("archived tool draft version was editable")
	}
}
