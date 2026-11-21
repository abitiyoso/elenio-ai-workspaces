// Package app wires configuration into concrete adapters shared by the api and worker binaries.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/api"
	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/connector"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/config"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/workflows/registry"
	"github.com/elenio/flowcase/migrations"
)

func NewMigrator(pool *pgxpool.Pool) (postgres.Migrator, error) {
	migs, err := postgres.LoadMigrations(migrations.FS)
	if err != nil {
		return postgres.Migrator{}, err
	}
	return postgres.Migrator{Pool: pool, Migrations: migs}, nil
}

// DatabaseChecks are readiness probes for connectivity and schema version.
func DatabaseChecks(pool *pgxpool.Pool, m postgres.Migrator) []api.Check {
	return []api.Check{
		{Name: "postgres", Run: pool.Ping},
		{Name: "schema", Run: m.Check},
	}
}

// NewAuth builds the AuthAdapter selected by config. Config validation already guarantees dev_mock
// is only reachable with APP_ENV=development and the explicit acknowledgement.
func NewAuth(cfg config.Config, pool *pgxpool.Pool) (auth.AuthAdapter, []api.Check, error) {
	switch cfg.Auth.Mode {
	case config.AuthModeDevMock:
		if !cfg.IsDevelopment() {
			return nil, nil, fmt.Errorf("dev_mock auth refused outside development")
		}
		roles, err := parseRoles(cfg.Auth.DevRoles)
		if err != nil {
			return nil, nil, fmt.Errorf("DEV_AUTH_ROLES: %w", err)
		}
		var personas []auth.DevPersona
		for _, p := range cfg.Auth.DevPersonas {
			pr, err := parseRoles(p.Roles)
			if err != nil {
				return nil, nil, fmt.Errorf("DEV_AUTH_PERSONAS %s: %w", p.Key, err)
			}
			personas = append(personas, auth.DevPersona{Key: p.Key, UserID: p.UserID, DisplayName: p.Name, Roles: pr})
		}
		return auth.NewDevMockAdapter(cfg.Auth.DevUserID, cfg.Auth.DevTenantID, cfg.Auth.DevDisplayName, roles, personas...), nil, nil
	case config.AuthModeTrustedJWT:
		keys := auth.NewJWKSCache(cfg.Auth.JWKSURL, cfg.Auth.JWKSCacheTTL)
		verifier := auth.NewJWTVerifier(auth.JWTVerifierConfig{
			Issuer: cfg.Auth.Issuer, Audience: cfg.Auth.Audience, TenantClaim: cfg.Auth.TenantClaim,
			ClockSkew: cfg.Auth.ClockSkew, MaxTokenTTL: cfg.Auth.MaxTokenTTL, Keys: keys,
		})
		checks := []api.Check{{Name: "host_jwks", Run: func(ctx context.Context) error { return keys.Ready(ctx) }}}
		return auth.NewTrustedAdapter(verifier, auth.PostgresDirectory{DB: pool}, cfg.Auth.SelfApprovalExemptSubjects...), checks, nil
	}
	return nil, nil, fmt.Errorf("unsupported AUTH_MODE %q", cfg.Auth.Mode)
}

func NewKnowledge(cfg config.Config) kms.KnowledgeProvider {
	if cfg.KMSMode == config.IntegrationStub {
		return kms.NewDevelopmentStub()
	}
	return kms.NewDisabled()
}

// NewAgents builds the configured AI runtime client. pool backs the delegated-user lookup of the
// agent_studio client (nil disables delegation, e.g. in tests without a database).
func NewAgents(cfg config.Config, pool *pgxpool.Pool) (agent.AgentExecutor, error) {
	switch cfg.AgentMode {
	case config.IntegrationStub:
		return agent.NewDevelopmentStub(cfg.AgentCallbackSecret), nil
	case config.AgentModeAgentStudio:
		var resolve agent.SubjectResolver
		if pool != nil {
			resolve = DirectorySubject(pool)
		}
		return agent.NewAgentStudio(agent.AgentStudioConfig{BaseURL: cfg.AgentStudio.BaseURL, ServiceToken: cfg.AgentStudio.Token,
			CallbackSecret: cfg.AgentCallbackSecret, Timeout: cfg.AgentStudio.Timeout, ResolveSubject: resolve})
	}
	return agent.NewDisabled(), nil
}

// DirectorySubject resolves a Flowcase user id to the directory subject of an active, human tenant
// member (in trusted_jwt mode the Control Plane user id). Unknown or inactive users resolve to "".
func DirectorySubject(pool *pgxpool.Pool) agent.SubjectResolver {
	return func(ctx context.Context, tenantID, userID string) (string, error) {
		var subject string
		err := pool.QueryRow(ctx, `SELECT u.subject FROM users u JOIN memberships m ON m.user_id = u.id
			WHERE m.tenant_id = $1::uuid AND u.id = $2::uuid AND u.kind = 'human' AND u.status = 'active' AND m.status = 'active'`,
			tenantID, userID).Scan(&subject)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return subject, err
	}
}

// NewConnectors returns the allowlisted connector implementations available in this runtime.
// Only the development stub exists until a real connector contract is confirmed.
func NewConnectors(cfg config.Config) connector.Registry {
	if cfg.ConnectorsMode == config.IntegrationStub {
		return connector.Static{"poc.notify": connector.NewStub()}
	}
	return connector.Static{}
}

// NewRegistry loads the embedded domain packs and builds the workflow registry service.
// External bindings (PACKS_CONFIG) map agents to runtime ids and set per-policy enforcement.
func NewRegistry(pool *pgxpool.Pool, packsConfig string) (*api.Registry, []packs.Pack, error) {
	ps, err := packs.Load()
	if err != nil {
		return nil, nil, err
	}
	cat, err := packs.Catalog(ps)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := packs.LoadBindings(packsConfig)
	if err != nil {
		return nil, nil, err
	}
	configured, err := bindings.Configure(cat)
	if err != nil {
		return nil, nil, err
	}
	tpls, err := packs.Templates(ps)
	if err != nil {
		return nil, nil, err
	}
	svc := &registry.Service{DB: pool, Catalog: configured, Templates: tpls}
	return &api.Registry{Service: svc, Catalog: configured, Templates: tpls}, ps, nil
}

func parseRoles(names []string) ([]auth.Role, error) {
	roles := make([]auth.Role, 0, len(names))
	for _, r := range names {
		role, ok := auth.ParseRole(r)
		if !ok {
			return nil, fmt.Errorf("unknown role %q", r)
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// NewEngine builds the case runtime with the configured integration adapters.
func NewEngine(cfg config.Config, pool *pgxpool.Pool, reg *api.Registry, logger *slog.Logger, connectors connector.Registry) (*cases.Engine, error) {
	agents, err := NewAgents(cfg, pool)
	if err != nil {
		return nil, err
	}
	return &cases.Engine{DB: pool, Catalog: reg.Catalog, Logger: logger, Agents: agents, Knowledge: NewKnowledge(cfg), Connectors: connectors}, nil
}
