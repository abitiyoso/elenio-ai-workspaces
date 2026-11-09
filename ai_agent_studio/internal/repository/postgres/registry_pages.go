package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

func registryPage(limit, offset int) (int, int) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (r *ModelRegistry) ListProvidersPage(ctx context.Context, tenantID string, limit, offset int) ([]Provider, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]Provider, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM llm_providers WHERE tenant_id=$1::uuid`, tenantID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text,provider_key,display_name,adapter_type,base_url,status,created_at
			FROM llm_providers WHERE tenant_id=$1::uuid ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Provider
			if err := rows.Scan(&item.ID, &item.Key, &item.DisplayName, &item.AdapterType, &item.BaseURL, &item.Status, &item.CreatedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list model providers page: %w", err)
	}
	return items, total, nil
}

func (r *ModelRegistry) ListDeploymentsPage(ctx context.Context, tenantID string, limit, offset int) ([]Deployment, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]Deployment, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM llm_model_deployments WHERE tenant_id=$1::uuid`, tenantID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text,provider_id::text,deployment_key,provider_model_id,display_name,
			capabilities_json,max_output_tokens,timeout_ms,status,created_at FROM llm_model_deployments
			WHERE tenant_id=$1::uuid ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Deployment
			if err := rows.Scan(&item.ID, &item.ProviderID, &item.Key, &item.ProviderModelID, &item.DisplayName,
				&item.Capabilities, &item.MaxOutputTokens, &item.TimeoutMillis, &item.Status, &item.CreatedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list model deployments page: %w", err)
	}
	return items, total, nil
}

func (r *ModelRegistry) ListSkillsPage(ctx context.Context, tenantID, agentKey string, limit, offset int) ([]Skill, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]Skill, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM skills s JOIN agents a ON a.tenant_id=s.tenant_id AND a.id=s.agent_id
			WHERE s.tenant_id=$1::uuid AND a.agent_key=$2`, tenantID, agentKey).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT s.id::text,s.agent_id::text,s.skill_key,s.domain,s.owner_subject,s.created_at
			FROM skills s JOIN agents a ON a.tenant_id=s.tenant_id AND a.id=s.agent_id
			WHERE s.tenant_id=$1::uuid AND a.agent_key=$2 ORDER BY s.skill_key,s.id LIMIT $3 OFFSET $4`, tenantID, agentKey, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Skill
			if err := rows.Scan(&item.ID, &item.AgentID, &item.Key, &item.Domain, &item.Owner, &item.CreatedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list skills page: %w", err)
	}
	return items, total, nil
}

func (r *ModelRegistry) ListSkillVersionsPage(ctx context.Context, tenantID, skillID string, limit, offset int) ([]SkillVersion, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]SkillVersion, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM skill_versions WHERE tenant_id=$1::uuid AND skill_id=$2::uuid`, tenantID, skillID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text,skill_id::text,semver,instructions,procedures_json,examples_json,
			forbidden_actions_json,output_schema_json,checksum,state,created_by,created_at,published_by,published_at
			FROM skill_versions WHERE tenant_id=$1::uuid AND skill_id=$2::uuid ORDER BY created_at DESC,id LIMIT $3 OFFSET $4`, tenantID, skillID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item SkillVersion
			if err := rows.Scan(&item.ID, &item.SkillID, &item.Semver, &item.Instructions, &item.Procedures, &item.Examples, &item.ForbiddenActions, &item.OutputSchema, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list skill versions page: %w", err)
	}
	return items, total, nil
}

func (r *ModelRegistry) ListAgentVersionsPage(ctx context.Context, tenantID, agentKey string, limit, offset int) ([]AgentVersion, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]AgentVersion, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_versions v JOIN agents a ON a.tenant_id=v.tenant_id AND a.id=v.agent_id
			WHERE v.tenant_id=$1::uuid AND a.agent_key=$2`, tenantID, agentKey).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT v.id::text,v.agent_id::text,v.semver,v.manifest_json,v.input_schema_json,v.output_schema_json,
			v.model_policy_json,v.tool_policy_json,v.checksum,v.state,v.created_by,v.created_at,v.published_by,v.published_at
			FROM agent_versions v JOIN agents a ON a.tenant_id=v.tenant_id AND a.id=v.agent_id
			WHERE v.tenant_id=$1::uuid AND a.agent_key=$2 ORDER BY v.created_at DESC,v.id LIMIT $3 OFFSET $4`, tenantID, agentKey, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item AgentVersion
			if err := rows.Scan(&item.ID, &item.AgentID, &item.Semver, &item.Manifest, &item.InputSchema, &item.OutputSchema,
				&item.ModelPolicy, &item.ToolPolicy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list agent versions page: %w", err)
	}
	return items, total, nil
}

func (r *ModelRegistry) ListToolsPage(ctx context.Context, limit, offset int) ([]ToolVersion, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]ToolVersion, 0)
	total := 0
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM tools t JOIN tool_versions v ON v.tool_id=t.id`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count global tools: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT t.id::text,t.tool_key,t.kind,t.owner,t.risk_level,t.lifecycle_status,v.id::text,v.semver,v.adapter_ref,
		v.input_schema_json,v.output_schema_json,v.policy_json,v.checksum,v.published_at
		FROM tools t JOIN tool_versions v ON v.tool_id=t.id ORDER BY t.tool_key,v.semver DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list global tool catalog page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ToolVersion
		if err := rows.Scan(&item.ToolID, &item.Key, &item.Kind, &item.Owner, &item.RiskLevel, &item.Lifecycle, &item.ID, &item.Semver,
			&item.AdapterRef, &item.InputSchema, &item.OutputSchema, &item.Policy, &item.Checksum, &item.PublishedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
