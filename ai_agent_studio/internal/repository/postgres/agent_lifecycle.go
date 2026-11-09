package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// UpdateAgentMetadata changes only unversioned display metadata. Agents with a
// published version must be changed through a draft version and review flow.
func (r *ModelRegistry) UpdateAgentMetadata(ctx context.Context, tenantID, key, displayName, domain, actor string) (Agent, error) {
	var item Agent
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `UPDATE agents a SET display_name=$3,domain=$4,updated_at=now()
			WHERE a.tenant_id=$1::uuid AND a.agent_key=$2 AND a.lifecycle_status='draft'
			AND NOT EXISTS (SELECT 1 FROM agent_versions v WHERE v.tenant_id=a.tenant_id AND v.agent_id=a.id AND v.state IN ('published','deprecated'))
			RETURNING id::text,agent_key,display_name,domain,owner_subject,lifecycle_status,created_at`,
			tenantID, key, displayName, domain).Scan(&item.ID, &item.Key, &item.DisplayName, &item.Domain, &item.OwnerSubject, &item.LifecycleStatus, &item.CreatedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "agent.metadata_updated", "agent:"+item.ID)
	})
	if err != nil {
		return Agent{}, fmt.Errorf("update agent metadata: %w", err)
	}
	return item, nil
}

// CloneAgent copies the latest agent definition into a new unconfigured draft.
// Model and tool policies are cleared and no bindings are copied; capabilities
// must be re-authorized separately.
func (r *ModelRegistry) CloneAgent(ctx context.Context, tenantID, sourceKey string, input AgentInput) (Agent, error) {
	var item Agent
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var manifest, inputSchema, outputSchema json.RawMessage
		if err := tx.QueryRowContext(ctx, `SELECT v.manifest_json,v.input_schema_json,v.output_schema_json
			FROM agent_versions v JOIN agents a ON a.tenant_id=v.tenant_id AND a.id=v.agent_id
			WHERE v.tenant_id=$1::uuid AND a.agent_key=$2 ORDER BY v.created_at DESC,v.id DESC LIMIT 1`, tenantID, sourceKey).
			Scan(&manifest, &inputSchema, &outputSchema); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO agents (tenant_id,agent_key,display_name,domain,created_by)
			VALUES ($1::uuid,$2,$3,$4,$5)
			RETURNING id::text,agent_key,display_name,domain,owner_subject,lifecycle_status,created_at`,
			tenantID, input.Key, input.DisplayName, input.Domain, input.CreatedBy).Scan(
			&item.ID, &item.Key, &item.DisplayName, &item.Domain, &item.OwnerSubject, &item.LifecycleStatus, &item.CreatedAt); err != nil {
			return err
		}
		manifest, err := unconfigureManifest(manifest)
		if err != nil {
			return err
		}
		var versionID string
		if err := tx.QueryRowContext(ctx, `INSERT INTO agent_versions
			(tenant_id,agent_id,semver,manifest_json,input_schema_json,output_schema_json,model_policy_json,tool_policy_json,created_by)
			VALUES ($1::uuid,$2::uuid,'0.1.0',$3::jsonb,$4::jsonb,$5::jsonb,'{}'::jsonb,'{}'::jsonb,$6)
			RETURNING id::text`, tenantID, item.ID, string(manifest), string(inputSchema), string(outputSchema), input.CreatedBy).Scan(&versionID); err != nil {
			return err
		}
		if err := audit(tx, tenantID, input.CreatedBy, "agent.cloned", "agent:"+item.ID); err != nil {
			return err
		}
		return audit(tx, tenantID, input.CreatedBy, "agent_version.draft_created", "agent_version:"+versionID)
	})
	if err != nil {
		return Agent{}, fmt.Errorf("clone agent: %w", err)
	}
	return item, nil
}

func unconfigureManifest(raw json.RawMessage) (json.RawMessage, error) {
	manifest, err := normalizeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("source manifest: %w", err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &values); err != nil {
		return nil, err
	}
	values["configured"] = json.RawMessage("false")
	return json.Marshal(values)
}
