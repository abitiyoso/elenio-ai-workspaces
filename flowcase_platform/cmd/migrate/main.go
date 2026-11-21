// Command migrate applies or reverts schema migrations and seeds the development identity.
//
//	migrate up | down [steps] | status | seed-dev
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/app"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/config"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/workflows/catalog"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: migrate up | down [steps] | status | seed-dev")
	}
	// Only DATABASE_URL is needed for schema work; seed-dev additionally validates the full config.
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := postgres.Open(ctx, url, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	m, err := app.NewMigrator(pool)
	if err != nil {
		return err
	}

	switch args[0] {
	case "up":
		applied, err := m.Up(ctx)
		fmt.Printf("applied %v (latest %d)\n", applied, m.Latest())
		return err
	case "down":
		steps := 1
		if len(args) > 1 {
			if steps, err = strconv.Atoi(args[1]); err != nil || steps < 1 {
				return errors.New("down steps must be a positive integer")
			}
		}
		reverted, err := m.Down(ctx, steps)
		fmt.Printf("reverted %v\n", reverted)
		return err
	case "status":
		if err := m.Check(ctx); err != nil {
			return fmt.Errorf("schema not current (expected %d migrations): %w", m.Latest(), err)
		}
		fmt.Printf("schema current at %d\n", m.Latest())
		return nil
	case "seed-dev":
		cfg, err := config.FromEnv()
		if err != nil {
			return err
		}
		if !cfg.IsDevelopment() || cfg.Auth.Mode != config.AuthModeDevMock {
			return errors.New("seed-dev requires APP_ENV=development and AUTH_MODE=dev_mock")
		}
		reg, _, err := app.NewRegistry(pool, cfg.PacksConfig)
		if err != nil {
			return err
		}
		snap := catalog.ForTenant(reg.Catalog, cfg.Auth.DevTenantID).Snapshot()
		seed := postgres.DevSeed{TenantID: cfg.Auth.DevTenantID, UserID: cfg.Auth.DevUserID, DisplayName: cfg.Auth.DevDisplayName,
			Roles: cfg.Auth.DevRoles, DomainRoles: snap.AssigneeRoles}
		for _, p := range snap.Policies {
			seed.Policies = append(seed.Policies, p.Ref)
		}
		for _, p := range cfg.Auth.DevPersonas {
			seed.Personas = append(seed.Personas, postgres.DevSeedPersona{UserID: p.UserID, DisplayName: p.Name, Roles: p.Roles})
		}
		if err := postgres.SeedDevelopment(ctx, pool, seed); err != nil {
			return err
		}
		fmt.Println("development identity seeded")
		return seedPacks(ctx, cfg, pool)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// seedPacks imports the SIM and PoC packs as the development identity, through the audited registry.
func seedPacks(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) error {
	reg, ps, err := app.NewRegistry(pool, cfg.PacksConfig)
	if err != nil {
		return err
	}
	var roles []auth.Role
	for _, r := range cfg.Auth.DevRoles {
		if role, ok := auth.ParseRole(r); ok {
			roles = append(roles, role)
		}
	}
	dev := auth.Identity{UserID: cfg.Auth.DevUserID, TenantID: cfg.Auth.DevTenantID, DisplayName: cfg.Auth.DevDisplayName, Kind: auth.KindHuman, Roles: roles}
	if !dev.HasRole(auth.RoleWorkflowDesigner) {
		fmt.Println("packs not imported: DEV_AUTH_ROLES lacks workflow_designer")
		return nil
	}
	log, err := reg.Service.SeedPacks(ctx, dev, ps)
	for _, line := range log {
		fmt.Println("pack", line)
	}
	return err
}
