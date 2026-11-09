package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Skill records tenant-owned skill metadata. Tool definitions are global in
// the current schema and are deliberately not exposed as tenant-owned writes.
type Skill struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Key       string    `json:"key"`
	Domain    string    `json:"domain"`
	Owner     *string   `json:"owner_subject"`
	CreatedAt time.Time `json:"created_at"`
}

type SkillInput struct {
	Key, Domain, CreatedBy string
}

type SkillVersion struct {
	ID               string          `json:"id"`
	SkillID          string          `json:"skill_id"`
	Semver           string          `json:"semver"`
	Instructions     string          `json:"instructions"`
	Procedures       json.RawMessage `json:"procedures"`
	Examples         json.RawMessage `json:"examples"`
	ForbiddenActions json.RawMessage `json:"forbidden_actions"`
	OutputSchema     json.RawMessage `json:"output_schema"`
	Checksum         *string         `json:"checksum,omitempty"`
	State            string          `json:"state"`
	CreatedBy        string          `json:"created_by"`
	CreatedAt        time.Time       `json:"created_at"`
	PublishedBy      *string         `json:"published_by,omitempty"`
	PublishedAt      *time.Time      `json:"published_at,omitempty"`
}

type SkillVersionInput struct {
	Semver, Instructions, CreatedBy                      string
	Procedures, Examples, ForbiddenActions, OutputSchema json.RawMessage
}

type AgentVersion struct {
	ID           string          `json:"id"`
	AgentID      string          `json:"agent_id"`
	Semver       string          `json:"semver"`
	Manifest     json.RawMessage `json:"manifest"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	ModelPolicy  json.RawMessage `json:"model_policy"`
	ToolPolicy   json.RawMessage `json:"tool_policy"`
	Checksum     *string         `json:"checksum,omitempty"`
	State        string          `json:"state"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
	PublishedBy  *string         `json:"published_by,omitempty"`
	PublishedAt  *time.Time      `json:"published_at,omitempty"`
}

type AgentVersionInput struct {
	Semver, CreatedBy                                            string
	Manifest, InputSchema, OutputSchema, ModelPolicy, ToolPolicy json.RawMessage
}

type ToolVersion struct {
	ID           string          `json:"id"`
	ToolID       string          `json:"tool_id"`
	Key          string          `json:"key"`
	Kind         string          `json:"kind"`
	Owner        string          `json:"owner"`
	RiskLevel    string          `json:"risk_level"`
	Lifecycle    string          `json:"lifecycle_status"`
	Semver       string          `json:"semver"`
	AdapterRef   string          `json:"adapter_ref"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Policy       json.RawMessage `json:"policy"`
	Checksum     *string         `json:"checksum,omitempty"`
	PublishedAt  *time.Time      `json:"published_at,omitempty"`
}

type BoundSkill struct {
	AgentVersionID string `json:"agent_version_id"`
	SkillID        string `json:"skill_id"`
	SkillVersionID string `json:"skill_version_id"`
}
type BoundTool struct {
	AgentVersionID string `json:"agent_version_id"`
	ToolVersionID  string `json:"tool_version_id"`
}

func (r *ModelRegistry) CreateSkill(ctx context.Context, tenantID, agentKey string, input SkillInput) (Skill, SkillVersion, error) {
	var skill Skill
	var version SkillVersion
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO skills (tenant_id, agent_id, skill_key, domain, created_by)
			SELECT $1::uuid, a.id, $3, $4, $5 FROM agents a
			WHERE a.tenant_id = $1::uuid AND a.agent_key = $2 AND a.lifecycle_status <> 'archived'
			RETURNING id::text, agent_id::text, skill_key, domain, owner_subject, created_at`,
			tenantID, agentKey, input.Key, input.Domain, input.CreatedBy,
		).Scan(&skill.ID, &skill.AgentID, &skill.Key, &skill.Domain, &skill.Owner, &skill.CreatedAt); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO skill_versions (tenant_id, skill_id, semver, instructions, output_schema_json, created_by)
			VALUES ($1::uuid, $2::uuid, '0.1.0', '', '{}'::jsonb, $3)
			RETURNING id::text, skill_id::text, semver, instructions, procedures_json, examples_json,
			 forbidden_actions_json, output_schema_json, checksum, state, created_by, created_at, published_by, published_at`,
			tenantID, skill.ID, input.CreatedBy,
		).Scan(&version.ID, &version.SkillID, &version.Semver, &version.Instructions, &version.Procedures,
			&version.Examples, &version.ForbiddenActions, &version.OutputSchema, &version.Checksum, &version.State,
			&version.CreatedBy, &version.CreatedAt, &version.PublishedBy, &version.PublishedAt); err != nil {
			return err
		}
		if err := audit(tx, tenantID, input.CreatedBy, "skill.created", "skill:"+skill.ID); err != nil {
			return err
		}
		return audit(tx, tenantID, input.CreatedBy, "skill_version.draft_created", "skill_version:"+version.ID)
	})
	if err != nil {
		return Skill{}, SkillVersion{}, fmt.Errorf("create skill: %w", err)
	}
	return skill, version, nil
}

func (r *ModelRegistry) ListSkills(ctx context.Context, tenantID, agentKey string) ([]Skill, error) {
	items := make([]Skill, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT s.id::text, s.agent_id::text, s.skill_key, s.domain, s.owner_subject, s.created_at
			FROM skills s JOIN agents a ON a.tenant_id=s.tenant_id AND a.id=s.agent_id
			WHERE s.tenant_id=$1::uuid AND a.agent_key=$2 ORDER BY s.skill_key`, tenantID, agentKey)
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
		return nil, fmt.Errorf("list skills: %w", err)
	}
	return items, nil
}

func (r *ModelRegistry) UpdateSkillDomain(ctx context.Context, tenantID, skillID, domain, actor string) (Skill, error) {
	var item Skill
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `UPDATE skills SET domain=$3 WHERE tenant_id=$1::uuid AND id=$2::uuid
			RETURNING id::text,agent_id::text,skill_key,domain,owner_subject,created_at`, tenantID, skillID, domain).
			Scan(&item.ID, &item.AgentID, &item.Key, &item.Domain, &item.Owner, &item.CreatedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "skill.updated", "skill:"+skillID)
	})
	if err != nil {
		return Skill{}, fmt.Errorf("update skill: %w", err)
	}
	return item, nil
}

// DeleteDraftSkill removes a skill only while all of its versions are drafts.
// Published skill history is retained; an attempted delete fails closed.
func (r *ModelRegistry) DeleteDraftSkill(ctx context.Context, tenantID, skillID, actor string) error {
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var key string
		if err := tx.QueryRowContext(ctx, `SELECT skill_key FROM skills WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, skillID).Scan(&key); err != nil {
			return err
		}
		var published int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM skill_versions WHERE tenant_id=$1::uuid AND skill_id=$2::uuid AND state<>'draft'`, tenantID, skillID).Scan(&published); err != nil {
			return err
		}
		if published > 0 {
			return errors.New("skill with published versions cannot be deleted")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM agent_skill_bindings WHERE tenant_id=$1::uuid AND skill_version_id IN (SELECT id FROM skill_versions WHERE tenant_id=$1::uuid AND skill_id=$2::uuid)`, tenantID, skillID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM skill_versions WHERE tenant_id=$1::uuid AND skill_id=$2::uuid`, tenantID, skillID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM skills WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, skillID); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "skill.deleted", "skill:"+key)
	})
	if err != nil {
		return fmt.Errorf("delete draft skill: %w", err)
	}
	return nil
}

func (r *ModelRegistry) ListSkillVersions(ctx context.Context, tenantID, skillID string) ([]SkillVersion, error) {
	items := make([]SkillVersion, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id::text, skill_id::text, semver, instructions, procedures_json, examples_json,
			forbidden_actions_json, output_schema_json, checksum, state, created_by, created_at, published_by, published_at
			FROM skill_versions WHERE tenant_id=$1::uuid AND skill_id=$2::uuid ORDER BY created_at DESC`, tenantID, skillID)
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
		return nil, fmt.Errorf("list skill versions: %w", err)
	}
	return items, nil
}

func (r *ModelRegistry) CreateSkillVersion(ctx context.Context, tenantID, skillID string, input SkillVersionInput) (SkillVersion, error) {
	procedures, err := normalizeJSONArray(input.Procedures)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("procedures: %w", err)
	}
	examples, err := normalizeJSONArray(input.Examples)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("examples: %w", err)
	}
	forbidden, err := normalizeJSONArray(input.ForbiddenActions)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("forbidden actions: %w", err)
	}
	schema, err := normalizeJSONObject(input.OutputSchema)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("output schema: %w", err)
	}
	var item SkillVersion
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO skill_versions (tenant_id,skill_id,semver,instructions,procedures_json,examples_json,forbidden_actions_json,output_schema_json,created_by)
			VALUES ($1::uuid,$2::uuid,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8::jsonb,$9)
			RETURNING id::text,skill_id::text,semver,instructions,procedures_json,examples_json,forbidden_actions_json,output_schema_json,checksum,state,created_by,created_at,published_by,published_at`,
			tenantID, skillID, input.Semver, input.Instructions, string(procedures), string(examples), string(forbidden), string(schema), input.CreatedBy).Scan(
			&item.ID, &item.SkillID, &item.Semver, &item.Instructions, &item.Procedures, &item.Examples, &item.ForbiddenActions, &item.OutputSchema, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, input.CreatedBy, "skill_version.draft_created", "skill_version:"+item.ID)
	})
	if err != nil {
		return SkillVersion{}, fmt.Errorf("create skill version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) UpdateSkillVersion(ctx context.Context, tenantID, versionID string, input SkillVersionInput, actor string) (SkillVersion, error) {
	procedures, err := normalizeJSONArray(input.Procedures)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("procedures: %w", err)
	}
	examples, err := normalizeJSONArray(input.Examples)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("examples: %w", err)
	}
	forbidden, err := normalizeJSONArray(input.ForbiddenActions)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("forbidden actions: %w", err)
	}
	schema, err := normalizeJSONObject(input.OutputSchema)
	if err != nil {
		return SkillVersion{}, fmt.Errorf("output schema: %w", err)
	}
	var item SkillVersion
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `UPDATE skill_versions SET semver=$3,instructions=$4,procedures_json=$5::jsonb,examples_json=$6::jsonb,forbidden_actions_json=$7::jsonb,output_schema_json=$8::jsonb
			WHERE tenant_id=$1::uuid AND id=$2::uuid AND state='draft'
			RETURNING id::text,skill_id::text,semver,instructions,procedures_json,examples_json,forbidden_actions_json,output_schema_json,checksum,state,created_by,created_at,published_by,published_at`,
			tenantID, versionID, input.Semver, input.Instructions, string(procedures), string(examples), string(forbidden), string(schema)).Scan(
			&item.ID, &item.SkillID, &item.Semver, &item.Instructions, &item.Procedures, &item.Examples, &item.ForbiddenActions, &item.OutputSchema, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "skill_version.draft_updated", "skill_version:"+item.ID)
	})
	if err != nil {
		return SkillVersion{}, fmt.Errorf("update skill version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) PublishSkillVersion(ctx context.Context, tenantID, versionID, actor string) (SkillVersion, error) {
	var item SkillVersion
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id::text,skill_id::text,semver,instructions,procedures_json,examples_json,forbidden_actions_json,output_schema_json,state,created_by,created_at
			FROM skill_versions WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, versionID).Scan(&item.ID, &item.SkillID, &item.Semver, &item.Instructions, &item.Procedures, &item.Examples, &item.ForbiddenActions, &item.OutputSchema, &item.State, &item.CreatedBy, &item.CreatedAt); err != nil {
			return err
		}
		if item.State != "draft" {
			return errors.New("only draft skill versions can be published")
		}
		checksum := hashParts(item.Semver, item.Instructions, string(item.Procedures), string(item.Examples), string(item.ForbiddenActions), string(item.OutputSchema))
		if err := tx.QueryRowContext(ctx, `UPDATE skill_versions SET state='published',checksum=$3,published_by=$4,published_at=now()
			WHERE tenant_id=$1::uuid AND id=$2::uuid RETURNING checksum,state,published_by,published_at`, tenantID, versionID, checksum, actor).Scan(&item.Checksum, &item.State, &item.PublishedBy, &item.PublishedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "skill_version.published", "skill_version:"+item.ID)
	})
	if err != nil {
		return SkillVersion{}, fmt.Errorf("publish skill version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) ListAgentVersions(ctx context.Context, tenantID, agentKey string) ([]AgentVersion, error) {
	items := make([]AgentVersion, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT v.id::text,v.agent_id::text,v.semver,v.manifest_json,v.input_schema_json,v.output_schema_json,v.model_policy_json,v.tool_policy_json,v.checksum,v.state,v.created_by,v.created_at,v.published_by,v.published_at
			FROM agent_versions v JOIN agents a ON a.tenant_id=v.tenant_id AND a.id=v.agent_id WHERE v.tenant_id=$1::uuid AND a.agent_key=$2 ORDER BY v.created_at DESC`, tenantID, agentKey)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item AgentVersion
			if err := rows.Scan(&item.ID, &item.AgentID, &item.Semver, &item.Manifest, &item.InputSchema, &item.OutputSchema, &item.ModelPolicy, &item.ToolPolicy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	return items, nil
}

func (r *ModelRegistry) CreateAgentVersion(ctx context.Context, tenantID, agentKey string, input AgentVersionInput) (AgentVersion, error) {
	manifest, err := normalizeJSONObject(input.Manifest)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("manifest: %w", err)
	}
	inSchema, err := normalizeJSONObject(input.InputSchema)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("input schema: %w", err)
	}
	outSchema, err := normalizeJSONObject(input.OutputSchema)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("output schema: %w", err)
	}
	modelPolicy, err := normalizeJSONObject(input.ModelPolicy)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("model policy: %w", err)
	}
	toolPolicy, err := normalizeJSONObject(input.ToolPolicy)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("tool policy: %w", err)
	}
	var item AgentVersion
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO agent_versions (tenant_id,agent_id,semver,manifest_json,input_schema_json,output_schema_json,model_policy_json,tool_policy_json,created_by)
			SELECT $1::uuid,a.id,$3,$4::jsonb,$5::jsonb,$6::jsonb,$7::jsonb,$8::jsonb,$9 FROM agents a WHERE a.tenant_id=$1::uuid AND a.agent_key=$2 AND a.lifecycle_status<>'archived'
			RETURNING id::text,agent_id::text,semver,manifest_json,input_schema_json,output_schema_json,model_policy_json,tool_policy_json,checksum,state,created_by,created_at,published_by,published_at`,
			tenantID, agentKey, input.Semver, string(manifest), string(inSchema), string(outSchema), string(modelPolicy), string(toolPolicy), input.CreatedBy).Scan(&item.ID, &item.AgentID, &item.Semver, &item.Manifest, &item.InputSchema, &item.OutputSchema, &item.ModelPolicy, &item.ToolPolicy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, input.CreatedBy, "agent_version.draft_created", "agent_version:"+item.ID)
	})
	if err != nil {
		return AgentVersion{}, fmt.Errorf("create agent version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) UpdateAgentVersion(ctx context.Context, tenantID, versionID string, input AgentVersionInput, actor string) (AgentVersion, error) {
	manifest, err := normalizeJSONObject(input.Manifest)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("manifest: %w", err)
	}
	inSchema, err := normalizeJSONObject(input.InputSchema)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("input schema: %w", err)
	}
	outSchema, err := normalizeJSONObject(input.OutputSchema)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("output schema: %w", err)
	}
	modelPolicy, err := normalizeJSONObject(input.ModelPolicy)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("model policy: %w", err)
	}
	toolPolicy, err := normalizeJSONObject(input.ToolPolicy)
	if err != nil {
		return AgentVersion{}, fmt.Errorf("tool policy: %w", err)
	}
	var item AgentVersion
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `UPDATE agent_versions SET semver=$3,manifest_json=$4::jsonb,input_schema_json=$5::jsonb,output_schema_json=$6::jsonb,model_policy_json=$7::jsonb,tool_policy_json=$8::jsonb
			WHERE tenant_id=$1::uuid AND id=$2::uuid AND state='draft'
			RETURNING id::text,agent_id::text,semver,manifest_json,input_schema_json,output_schema_json,model_policy_json,tool_policy_json,checksum,state,created_by,created_at,published_by,published_at`,
			tenantID, versionID, input.Semver, string(manifest), string(inSchema), string(outSchema), string(modelPolicy), string(toolPolicy)).Scan(&item.ID, &item.AgentID, &item.Semver, &item.Manifest, &item.InputSchema, &item.OutputSchema, &item.ModelPolicy, &item.ToolPolicy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "agent_version.draft_updated", "agent_version:"+item.ID)
	})
	if err != nil {
		return AgentVersion{}, fmt.Errorf("update agent version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) PublishAgentVersion(ctx context.Context, tenantID, versionID, actor string) (AgentVersion, error) {
	var item AgentVersion
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id::text,agent_id::text,semver,manifest_json,input_schema_json,output_schema_json,model_policy_json,tool_policy_json,state,created_by,created_at FROM agent_versions WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, versionID).Scan(&item.ID, &item.AgentID, &item.Semver, &item.Manifest, &item.InputSchema, &item.OutputSchema, &item.ModelPolicy, &item.ToolPolicy, &item.State, &item.CreatedBy, &item.CreatedAt); err != nil {
			return err
		}
		if item.State != "draft" {
			return errors.New("only draft agent versions can be published")
		}
		checksum := hashParts(item.Semver, string(item.Manifest), string(item.InputSchema), string(item.OutputSchema), string(item.ModelPolicy), string(item.ToolPolicy))
		if err := tx.QueryRowContext(ctx, `UPDATE agent_versions SET state='published',checksum=$3,published_by=$4,published_at=now() WHERE tenant_id=$1::uuid AND id=$2::uuid RETURNING checksum,state,published_by,published_at`, tenantID, versionID, checksum, actor).Scan(&item.Checksum, &item.State, &item.PublishedBy, &item.PublishedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "agent_version.published", "agent_version:"+item.ID)
	})
	if err != nil {
		return AgentVersion{}, fmt.Errorf("publish agent version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) ListTools(ctx context.Context) ([]ToolVersion, error) {
	items := make([]ToolVersion, 0)
	rows, err := r.db.QueryContext(ctx, `SELECT t.id::text,t.tool_key,t.kind,t.owner,t.risk_level,t.lifecycle_status,v.id::text,v.semver,v.adapter_ref,v.input_schema_json,v.output_schema_json,v.policy_json,v.checksum,v.published_at
		FROM tools t JOIN tool_versions v ON v.tool_id=t.id ORDER BY t.tool_key,v.semver DESC`)
	if err != nil {
		return nil, fmt.Errorf("list global tool catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ToolVersion
		if err := rows.Scan(&item.ToolID, &item.Key, &item.Kind, &item.Owner, &item.RiskLevel, &item.Lifecycle, &item.ID, &item.Semver, &item.AdapterRef, &item.InputSchema, &item.OutputSchema, &item.Policy, &item.Checksum, &item.PublishedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ModelRegistry) ListAgentToolBindings(ctx context.Context, tenantID, agentVersionID string) ([]BoundTool, error) {
	items := make([]BoundTool, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT b.agent_version_id::text,b.tool_version_id::text FROM agent_tool_bindings b WHERE b.tenant_id=$1::uuid AND b.agent_version_id=$2::uuid ORDER BY b.tool_version_id`, tenantID, agentVersionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item BoundTool
			if err := rows.Scan(&item.AgentVersionID, &item.ToolVersionID); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list agent tool bindings: %w", err)
	}
	return items, nil
}

func (r *ModelRegistry) BindTool(ctx context.Context, tenantID, agentVersionID, toolVersionID, actor string, scope json.RawMessage) error {
	policy, err := normalizeJSONObject(scope)
	if err != nil {
		return fmt.Errorf("action scope: %w", err)
	}
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var published sql.NullTime
		var lifecycle string
		if err := tx.QueryRowContext(ctx, `SELECT v.published_at,t.lifecycle_status FROM tool_versions v JOIN tools t ON t.id=v.tool_id WHERE v.id=$1::uuid`, toolVersionID).Scan(&published, &lifecycle); err != nil {
			return err
		}
		if !published.Valid || lifecycle != "active" {
			return errors.New("tool version must be published and active before binding")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_tool_bindings (tenant_id,agent_version_id,tool_version_id,action_scope_json) VALUES ($1::uuid,$2::uuid,$3::uuid,$4::jsonb)`, tenantID, agentVersionID, toolVersionID, string(policy)); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "agent_tool.bound", "agent_version:"+agentVersionID+"/tool_version:"+toolVersionID)
	})
	if err != nil {
		return fmt.Errorf("bind tool: %w", err)
	}
	return nil
}

func (r *ModelRegistry) UnbindTool(ctx context.Context, tenantID, agentVersionID, toolVersionID, actor string) error {
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM agent_tool_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid AND tool_version_id=$3::uuid`, tenantID, agentVersionID, toolVersionID)
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
		return audit(tx, tenantID, actor, "agent_tool.unbound", "agent_version:"+agentVersionID+"/tool_version:"+toolVersionID)
	})
	if err != nil {
		return fmt.Errorf("unbind tool: %w", err)
	}
	return nil
}

func (r *ModelRegistry) ListAgentSkillBindings(ctx context.Context, tenantID, agentVersionID string) ([]BoundSkill, error) {
	items := make([]BoundSkill, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT b.agent_version_id::text,sv.skill_id::text,b.skill_version_id::text
			FROM agent_skill_bindings b JOIN skill_versions sv ON sv.tenant_id=b.tenant_id AND sv.id=b.skill_version_id
			WHERE b.tenant_id=$1::uuid AND b.agent_version_id=$2::uuid ORDER BY b.skill_version_id`, tenantID, agentVersionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item BoundSkill
			if err := rows.Scan(&item.AgentVersionID, &item.SkillID, &item.SkillVersionID); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list agent skill bindings: %w", err)
	}
	return items, nil
}

func (r *ModelRegistry) BindSkill(ctx context.Context, tenantID, agentVersionID, skillVersionID, actor string) error {
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_skill_bindings (tenant_id,agent_version_id,skill_version_id) VALUES ($1::uuid,$2::uuid,$3::uuid)`, tenantID, agentVersionID, skillVersionID); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "agent_skill.bound", "agent_version:"+agentVersionID+"/skill_version:"+skillVersionID)
	})
	if err != nil {
		return fmt.Errorf("bind skill: %w", err)
	}
	return nil
}

func (r *ModelRegistry) UnbindSkill(ctx context.Context, tenantID, agentVersionID, skillVersionID, actor string) error {
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM agent_skill_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid AND skill_version_id=$3::uuid`, tenantID, agentVersionID, skillVersionID)
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
		return audit(tx, tenantID, actor, "agent_skill.unbound", "agent_version:"+agentVersionID+"/skill_version:"+skillVersionID)
	})
	if err != nil {
		return fmt.Errorf("unbind skill: %w", err)
	}
	return nil
}

func audit(tx *sql.Tx, tenantID, actor, action, resource string) error {
	_, err := tx.Exec(`INSERT INTO audit_events (tenant_id,actor_subject,action,resource_ref) VALUES ($1::uuid,$2,$3,$4)`, tenantID, actor, action, resource)
	return err
}

func hashParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeJSONArray(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`[]`), nil
	}
	var value []json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("must be a JSON array")
	}
	return json.Marshal(value)
}
