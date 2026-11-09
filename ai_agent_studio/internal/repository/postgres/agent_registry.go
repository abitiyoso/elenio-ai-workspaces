package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Agent struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"`
	DisplayName     string    `json:"display_name"`
	Domain          string    `json:"domain"`
	OwnerSubject    *string   `json:"owner_subject"`
	LifecycleStatus string    `json:"lifecycle_status"`
	CreatedAt       time.Time `json:"created_at"`
}

type AgentInput struct {
	Key         string
	DisplayName string
	Domain      string
	CreatedBy   string
}

type SIMBootstrapResult struct {
	Agents []Agent `json:"agents"`
}

var simAgentTemplates = []struct {
	key, name, domain string
}{
	{"sim.intake", "Intake Agent", "business-requirements"},
	{"sim.solution_matching", "Solution Matching Agent", "capability-fit"},
	{"sim.standard_project", "Standard Project Agent", "delivery-planning"},
	{"sim.design", "Design Agent", "architecture"},
	{"sim.cost_challenge", "Cost Challenge Agent", "costing-review"},
	{"sim.proposal", "Proposal Agent", "proposal-composition"},
	{"sim.case_intake", "Case Intake Assistant", "case-intake"},
}

// BootstrapSIMAgents creates the tenant-local SIM identities (the six workflow
// specialists plus the pre-case intake assistant) and an unconfigured draft
// version for each. Existing tenant records are preserved, so re-running it on
// an older tenant only adds identities that are missing.
func (r *ModelRegistry) BootstrapSIMAgents(ctx context.Context, tenantID, actor string) (SIMBootstrapResult, error) {
	result := SIMBootstrapResult{Agents: make([]Agent, 0, len(simAgentTemplates))}
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		for _, template := range simAgentTemplates {
			var agent Agent
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO agents (tenant_id, agent_key, display_name, domain, created_by)
				VALUES ($1::uuid, $2, $3, $4, $5)
				ON CONFLICT (tenant_id, agent_key) DO NOTHING
				RETURNING id::text, agent_key, display_name, domain, owner_subject, lifecycle_status, created_at`,
				tenantID, template.key, template.name, template.domain, actor,
			).Scan(&agent.ID, &agent.Key, &agent.DisplayName, &agent.Domain, &agent.OwnerSubject, &agent.LifecycleStatus, &agent.CreatedAt); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					if err := tx.QueryRowContext(ctx, `
						SELECT id::text, agent_key, display_name, domain, owner_subject, lifecycle_status, created_at
						FROM agents WHERE tenant_id = $1::uuid AND agent_key = $2`, tenantID, template.key,
					).Scan(&agent.ID, &agent.Key, &agent.DisplayName, &agent.Domain, &agent.OwnerSubject, &agent.LifecycleStatus, &agent.CreatedAt); err != nil {
						return err
					}
				} else {
					return err
				}
			} else {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO audit_events (tenant_id, actor_subject, action, resource_ref)
					VALUES ($1::uuid, $2, 'agent.sim_template_created', $3)`, tenantID, actor, "agent:"+agent.ID); err != nil {
					return err
				}
			}

			var versionID string
			err := tx.QueryRowContext(ctx, `
				SELECT id::text FROM agent_versions
				WHERE tenant_id = $1::uuid AND agent_id = $2::uuid
				ORDER BY created_at LIMIT 1`, tenantID, agent.ID).Scan(&versionID)
			if err == sql.ErrNoRows {
				manifest, _ := json.Marshal(map[string]any{
					"template":    "sim-b2b-six-agent", // pack_key of the SIM pack; kept stable as an identifier
					"configured":  false,
					"description": "Unconfigured SIM specialist identity. SME, model, skills, and tool grants require explicit owner review.",
				})
				if err := tx.QueryRowContext(ctx, `
					INSERT INTO agent_versions
						(tenant_id, agent_id, semver, manifest_json, input_schema_json, output_schema_json, created_by)
					VALUES ($1::uuid, $2::uuid, '0.1.0', $3::jsonb, '{}'::jsonb, '{}'::jsonb, $4)
					RETURNING id::text`, tenantID, agent.ID, string(manifest), actor).Scan(&versionID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO audit_events (tenant_id, actor_subject, action, resource_ref)
					VALUES ($1::uuid, $2, 'agent_version.draft_created', $3)`, tenantID, actor, "agent_version:"+versionID); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			result.Agents = append(result.Agents, agent)
		}
		return nil
	})
	if err != nil {
		return SIMBootstrapResult{}, fmt.Errorf("bootstrap SIM agents: %w", err)
	}
	return result, nil
}

func (r *ModelRegistry) ListAgents(ctx context.Context, tenantID string, limit, offset int) ([]Agent, int, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	agents := make([]Agent, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE tenant_id = $1::uuid`, tenantID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT id::text, agent_key, display_name, domain, owner_subject, lifecycle_status, created_at
			FROM agents WHERE tenant_id = $1::uuid ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var agent Agent
			if err := rows.Scan(&agent.ID, &agent.Key, &agent.DisplayName, &agent.Domain,
				&agent.OwnerSubject, &agent.LifecycleStatus, &agent.CreatedAt); err != nil {
				return err
			}
			agents = append(agents, agent)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list agents: %w", err)
	}
	return agents, total, nil
}

func (r *ModelRegistry) CreateAgent(ctx context.Context, tenantID string, input AgentInput) (Agent, error) {
	var agent Agent
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO agents (tenant_id, agent_key, display_name, domain, created_by)
			VALUES ($1::uuid, $2, $3, $4, $5)
			RETURNING id::text, agent_key, display_name, domain, owner_subject, lifecycle_status, created_at`,
			tenantID, input.Key, input.DisplayName, input.Domain, input.CreatedBy,
		).Scan(&agent.ID, &agent.Key, &agent.DisplayName, &agent.Domain, &agent.OwnerSubject, &agent.LifecycleStatus, &agent.CreatedAt); err != nil {
			return err
		}
		manifest, _ := json.Marshal(map[string]any{"configured": false})
		var versionID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO agent_versions
				(tenant_id, agent_id, semver, manifest_json, input_schema_json, output_schema_json, created_by)
			VALUES ($1::uuid, $2::uuid, '0.1.0', $3::jsonb, '{}'::jsonb, '{}'::jsonb, $4)
			RETURNING id::text`, tenantID, agent.ID, string(manifest), input.CreatedBy).Scan(&versionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_subject, action, resource_ref)
			VALUES ($1::uuid, $2, 'agent.created', $3), ($1::uuid, $2, 'agent_version.draft_created', $4)`,
			tenantID, input.CreatedBy, "agent:"+agent.ID, "agent_version:"+versionID)
		return err
	})
	if err != nil {
		return Agent{}, fmt.Errorf("create agent: %w", err)
	}
	return agent, nil
}

func (r *ModelRegistry) ArchiveAgent(ctx context.Context, tenantID, key, actor string) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE agents SET lifecycle_status = 'archived', updated_at = now()
			WHERE tenant_id = $1::uuid AND agent_key = $2 AND lifecycle_status <> 'archived'`, tenantID, key)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return sql.ErrNoRows
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_subject, action, resource_ref)
			VALUES ($1::uuid, $2, 'agent.archived', $3)`, tenantID, actor, "agent:"+key)
		return err
	})
}
