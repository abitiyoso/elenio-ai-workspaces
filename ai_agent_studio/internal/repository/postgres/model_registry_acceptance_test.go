package postgres_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elenio/agent-studio/internal/httpapi"
	"github.com/elenio/agent-studio/internal/ports"
	"github.com/elenio/agent-studio/internal/repository/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func acceptanceRegistry(t *testing.T) (*sql.DB, *postgres.ModelRegistry) {
	t.Helper()
	if os.Getenv("AGENT_STUDIO_TEST_DATABASE_DISPOSABLE") != "1" {
		t.Skip("requires AGENT_STUDIO_TEST_DATABASE_DISPOSABLE=1 for a disposable local PostgreSQL database")
	}
	dsn := strings.TrimSpace(os.Getenv("AGENT_STUDIO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("requires AGENT_STUDIO_TEST_DATABASE_URL for a disposable local PostgreSQL database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("AGENT_STUDIO_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	host := parsed.Hostname()
	if host == "" {
		// PostgreSQL URLs with no host use a local Unix socket.
		host = "localhost"
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("acceptance tests only permit a local PostgreSQL host")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL database")
	}
	db.SetMaxOpenConns(12)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connect to disposable PostgreSQL database: %v", err)
	}
	var schemaReady bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.agent_versions') IS NOT NULL AND to_regclass('public.agents') IS NOT NULL`).Scan(&schemaReady); err != nil || !schemaReady {
		t.Fatal("disposable test database must have migrations applied (agent_versions and agents tables)")
	}
	registry, err := postgres.NewModelRegistry(db)
	if err != nil {
		t.Fatal(err)
	}
	return db, registry
}

func createAcceptanceAgent(t *testing.T, registry *postgres.ModelRegistry, tenantID string) (postgres.Agent, postgres.AgentVersion) {
	t.Helper()
	key := "test." + strings.ReplaceAll(acceptanceUUID(t), "-", "")[:20]
	agent, err := registry.CreateAgent(context.Background(), tenantID, postgres.AgentInput{
		Key: key, DisplayName: "Acceptance agent", Domain: "acceptance", CreatedBy: "test-runner",
	})
	if err != nil {
		t.Fatalf("create acceptance agent: %v", err)
	}
	versions, err := registry.ListAgentVersions(context.Background(), tenantID, key)
	if err != nil || len(versions) != 1 {
		t.Fatalf("list initial agent version: versions=%d err=%v", len(versions), err)
	}
	return agent, versions[0]
}

func TestAcceptanceCrossTenantLeakAttempts(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	tenantA, tenantB := acceptanceUUID(t), acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantA)
	visible, err := registry.ListAgentVersions(context.Background(), tenantB, agent.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("tenant B listed tenant A versions: %d", len(visible))
	}
	updated, err := registry.UpdateAgentVersion(context.Background(), tenantB, version.ID, postgres.AgentVersionInput{
		Semver: "0.1.1", Manifest: []byte(`{"changed":true}`), InputSchema: []byte(`{}`),
		OutputSchema: []byte(`{}`), ModelPolicy: []byte(`{}`), ToolPolicy: []byte(`{}`),
	}, "tenant-b-user")
	if err == nil {
		t.Fatalf("tenant B updated tenant A version: %+v", updated)
	}
	ownerVersions, err := registry.ListAgentVersions(context.Background(), tenantA, agent.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerVersions) != 1 || ownerVersions[0].ID != version.ID {
		t.Fatalf("tenant A's version changed after tenant B's attempt: %+v", ownerVersions)
	}
}

func TestAcceptancePublishedVersionIsImmutable(t *testing.T) {
	db, registry := acceptanceRegistry(t)
	tenantID := acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantID)
	published, err := registry.PublishAgentVersion(context.Background(), tenantID, version.ID, "publisher")
	if err != nil {
		t.Fatalf("publish draft: %v", err)
	}
	if published.State != "published" || published.Checksum == nil {
		t.Fatalf("published version lacks immutable metadata: %+v", published)
	}
	_, err = registry.UpdateAgentVersion(context.Background(), tenantID, version.ID, postgres.AgentVersionInput{
		Semver: "0.1.1", Manifest: []byte(`{"changed":true}`), InputSchema: []byte(`{}`),
		OutputSchema: []byte(`{}`), ModelPolicy: []byte(`{}`), ToolPolicy: []byte(`{}`),
	}, "editor")
	if err == nil {
		t.Fatal("repository allowed editing a published version")
	}
	verifyPublishedUnchanged(t, registry, tenantID, agent.Key, version.ID, *published.Checksum)

	for _, statement := range []string{
		`UPDATE agent_versions SET manifest_json='{"tampered":true}'::jsonb WHERE tenant_id=$1::uuid AND id=$2::uuid`,
		`DELETE FROM agent_versions WHERE tenant_id=$1::uuid AND id=$2::uuid`,
	} {
		tx, err := dbBeginTenant(db, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		_, execErr := tx.Exec(statement, tenantID, version.ID)
		_ = tx.Rollback()
		if execErr == nil {
			t.Fatalf("database trigger allowed immutable version mutation: %s", statement)
		}
	}
	verifyPublishedUnchanged(t, registry, tenantID, agent.Key, version.ID, *published.Checksum)
}

func verifyPublishedUnchanged(t *testing.T, registry *postgres.ModelRegistry, tenantID, agentKey, versionID, checksum string) {
	t.Helper()
	versions, err := registry.ListAgentVersions(context.Background(), tenantID, agentKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range versions {
		if item.ID != versionID {
			continue
		}
		if item.State != "published" || item.Checksum == nil || *item.Checksum != checksum || !isUnconfigured(item.Manifest) {
			t.Fatalf("published version changed: %+v", item)
		}
		return
	}
	t.Fatal("published version disappeared")
}

func isUnconfigured(manifest []byte) bool {
	var value struct {
		Configured bool `json:"configured"`
	}
	return json.Unmarshal(manifest, &value) == nil && !value.Configured
}

func dbBeginTenant(db *sql.DB, tenantID string) (*sql.Tx, error) {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`SELECT set_config('app.tenant_id',$1,true)`, tenantID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

type acceptanceAuth struct{ tenantID string }

func (a acceptanceAuth) VerifyRequest(context.Context, *http.Request) (ports.Principal, error) {
	return ports.Principal{TenantID: a.tenantID, Subject: "acceptance-user"}, nil
}

type deniedAcceptanceAuthorizer struct{ actions []string }

func (a *deniedAcceptanceAuthorizer) Authorize(_ context.Context, _ ports.Principal, action, _ string) error {
	a.actions = append(a.actions, action)
	return errors.New("denied for acceptance test")
}

func TestAcceptanceUnauthorizedEditingIsDenied(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	tenantID := acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantID)
	authorizer := &deniedAcceptanceAuthorizer{}
	handler := httpapi.CatalogHandler{Registry: registry, Auth: acceptanceAuth{tenantID}, Authorizer: authorizer}
	body := `{"semver":"0.1.1","manifest":{"unauthorized":true},"input_schema":{},"output_schema":{},"model_policy":{},"tool_policy":{}}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/catalog/agent-versions/"+version.ID, strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthorized edit returned %d: %s", response.Code, response.Body.String())
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != "agent.version.update" {
		t.Fatalf("unexpected authorization checks: %v", authorizer.actions)
	}
	versions, err := registry.ListAgentVersions(context.Background(), tenantID, agent.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || !isUnconfigured(versions[0].Manifest) {
		t.Fatalf("unauthorized edit changed the draft: %+v", versions)
	}
}

func TestAcceptanceConcurrentPublishHasOneWinner(t *testing.T) {
	_, registry := acceptanceRegistry(t)
	tenantID := acceptanceUUID(t)
	agent, version := createAcceptanceAgent(t, registry, tenantID)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, actor := range []string{"publisher-a", "publisher-b"} {
		wait.Add(1)
		go func(actor string) {
			defer wait.Done()
			<-start
			_, err := registry.PublishAgentVersion(context.Background(), tenantID, version.ID, actor)
			results <- err
		}(actor)
	}
	close(start)
	wait.Wait()
	close(results)
	winners, losers := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			losers++
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("concurrent publish should have one winner and one rejected release; winners=%d losers=%d", winners, losers)
	}
	versions, err := registry.ListAgentVersions(context.Background(), tenantID, agent.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].State != "published" {
		t.Fatalf("concurrent publish left an unexpected version state: %+v", versions)
	}
}

func acceptanceUUID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	encoded := hex.EncodeToString(value[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:])
}
