package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ToolDraft is tenant-authored metadata. It is deliberately separate from the
// platform-global tools table until a reviewed publishing bridge exists.
type ToolDraft struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"`
	DisplayName     string    `json:"display_name"`
	Kind            string    `json:"kind"`
	RiskLevel       string    `json:"risk_level"`
	LifecycleStatus string    `json:"lifecycle_status"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ToolDraftInput struct {
	Key, DisplayName, Kind, RiskLevel, CreatedBy string
}

type ToolDraftVersion struct {
	ID           string          `json:"id"`
	ToolID       string          `json:"tool_id"`
	Semver       string          `json:"semver"`
	AdapterRef   string          `json:"adapter_ref"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Policy       json.RawMessage `json:"policy"`
	Checksum     string          `json:"checksum"`
	State        string          `json:"state"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type ToolDraftVersionInput struct {
	Semver, AdapterRef, CreatedBy     string
	InputSchema, OutputSchema, Policy json.RawMessage
}

func (r *ModelRegistry) ListToolDrafts(ctx context.Context, tenantID string, limit, offset int) ([]ToolDraft, int, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	items := make([]ToolDraft, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tenant_tool_drafts WHERE tenant_id=$1::uuid`, tenantID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text,tool_key,display_name,kind,risk_level,lifecycle_status,created_by,created_at,updated_at
			FROM tenant_tool_drafts WHERE tenant_id=$1::uuid ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item ToolDraft
			if err := rows.Scan(&item.ID, &item.Key, &item.DisplayName, &item.Kind, &item.RiskLevel, &item.LifecycleStatus, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list tenant tool drafts: %w", err)
	}
	return items, total, nil
}

func (r *ModelRegistry) GetToolDraft(ctx context.Context, tenantID, toolID string) (ToolDraft, error) {
	var item ToolDraft
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id::text,tool_key,display_name,kind,risk_level,lifecycle_status,created_by,created_at,updated_at
			FROM tenant_tool_drafts WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, toolID).Scan(
			&item.ID, &item.Key, &item.DisplayName, &item.Kind, &item.RiskLevel, &item.LifecycleStatus, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	})
	if err != nil {
		return ToolDraft{}, fmt.Errorf("get tenant tool draft: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) CreateToolDraft(ctx context.Context, tenantID string, input ToolDraftInput) (ToolDraft, error) {
	var item ToolDraft
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO tenant_tool_drafts
			(tenant_id,tool_key,display_name,kind,risk_level,created_by)
			VALUES ($1::uuid,$2,$3,$4,$5,$6)
			RETURNING id::text,tool_key,display_name,kind,risk_level,lifecycle_status,created_by,created_at,updated_at`,
			tenantID, input.Key, input.DisplayName, input.Kind, input.RiskLevel, input.CreatedBy).Scan(
			&item.ID, &item.Key, &item.DisplayName, &item.Kind, &item.RiskLevel, &item.LifecycleStatus, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, input.CreatedBy, "tool_draft.created", "tenant_tool_draft:"+item.ID)
	})
	if err != nil {
		return ToolDraft{}, fmt.Errorf("create tenant tool draft: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) UpdateToolDraft(ctx context.Context, tenantID, toolID string, input ToolDraftInput, actor string) (ToolDraft, error) {
	var item ToolDraft
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `UPDATE tenant_tool_drafts SET tool_key=$3,display_name=$4,kind=$5,risk_level=$6,updated_at=now()
			WHERE tenant_id=$1::uuid AND id=$2::uuid AND lifecycle_status='draft'
			RETURNING id::text,tool_key,display_name,kind,risk_level,lifecycle_status,created_by,created_at,updated_at`,
			tenantID, toolID, input.Key, input.DisplayName, input.Kind, input.RiskLevel).Scan(
			&item.ID, &item.Key, &item.DisplayName, &item.Kind, &item.RiskLevel, &item.LifecycleStatus, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "tool_draft.updated", "tenant_tool_draft:"+item.ID)
	})
	if err != nil {
		return ToolDraft{}, fmt.Errorf("update tenant tool draft: %w", err)
	}
	return item, nil
}

// DeleteToolDraft deletes an unversioned draft. Once a version exists, use the
// archive transition so audit history and authored work remain intact.
func (r *ModelRegistry) DeleteToolDraft(ctx context.Context, tenantID, toolID, actor string) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var key string
		if err := tx.QueryRowContext(ctx, `DELETE FROM tenant_tool_drafts d
			WHERE d.tenant_id=$1::uuid AND d.id=$2::uuid AND d.lifecycle_status='draft'
			AND NOT EXISTS (SELECT 1 FROM tenant_tool_draft_versions v WHERE v.tenant_id=d.tenant_id AND v.tool_id=d.id)
			RETURNING d.tool_key`, tenantID, toolID).Scan(&key); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "tool_draft.deleted", "tenant_tool_draft:"+toolID)
	})
}

func (r *ModelRegistry) ArchiveToolDraft(ctx context.Context, tenantID, toolID, actor string) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE tenant_tool_drafts SET lifecycle_status='archived',updated_at=now()
			WHERE tenant_id=$1::uuid AND id=$2::uuid AND lifecycle_status='draft'`, tenantID, toolID)
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
		return audit(tx, tenantID, actor, "tool_draft.archived", "tenant_tool_draft:"+toolID)
	})
}

func (r *ModelRegistry) ListToolDraftVersions(ctx context.Context, tenantID, toolID string, limit, offset int) ([]ToolDraftVersion, int, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	items := make([]ToolDraftVersion, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tenant_tool_draft_versions WHERE tenant_id=$1::uuid AND tool_id=$2::uuid`, tenantID, toolID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text,tool_id::text,semver,adapter_ref,input_schema_json,output_schema_json,policy_json,checksum,state,created_by,created_at,updated_at
			FROM tenant_tool_draft_versions WHERE tenant_id=$1::uuid AND tool_id=$2::uuid ORDER BY created_at DESC,id LIMIT $3 OFFSET $4`, tenantID, toolID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item ToolDraftVersion
			if err := rows.Scan(&item.ID, &item.ToolID, &item.Semver, &item.AdapterRef, &item.InputSchema, &item.OutputSchema, &item.Policy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list tenant tool draft versions: %w", err)
	}
	return items, total, nil
}

func (r *ModelRegistry) CreateToolDraftVersion(ctx context.Context, tenantID, toolID string, input ToolDraftVersionInput) (ToolDraftVersion, error) {
	inputSchema, err := normalizeJSONObject(input.InputSchema)
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("input schema: %w", err)
	}
	outputSchema, err := normalizeJSONObject(input.OutputSchema)
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("output schema: %w", err)
	}
	policy, err := normalizeJSONObject(input.Policy)
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("policy: %w", err)
	}
	checksum := hashParts(input.Semver, input.AdapterRef, string(inputSchema), string(outputSchema), string(policy))
	var item ToolDraftVersion
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO tenant_tool_draft_versions
			(tenant_id,tool_id,semver,adapter_ref,input_schema_json,output_schema_json,policy_json,checksum,created_by)
			SELECT $1::uuid,d.id,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8,$9
			FROM tenant_tool_drafts d WHERE d.tenant_id=$1::uuid AND d.id=$2::uuid AND d.lifecycle_status='draft'
			RETURNING id::text,tool_id::text,semver,adapter_ref,input_schema_json,output_schema_json,policy_json,checksum,state,created_by,created_at,updated_at`,
			tenantID, toolID, input.Semver, input.AdapterRef, string(inputSchema), string(outputSchema), string(policy), checksum, input.CreatedBy).Scan(
			&item.ID, &item.ToolID, &item.Semver, &item.AdapterRef, &item.InputSchema, &item.OutputSchema, &item.Policy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, input.CreatedBy, "tool_draft_version.created", "tenant_tool_draft_version:"+item.ID)
	})
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("create tenant tool draft version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) UpdateToolDraftVersion(ctx context.Context, tenantID, versionID string, input ToolDraftVersionInput, actor string) (ToolDraftVersion, error) {
	inputSchema, err := normalizeJSONObject(input.InputSchema)
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("input schema: %w", err)
	}
	outputSchema, err := normalizeJSONObject(input.OutputSchema)
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("output schema: %w", err)
	}
	policy, err := normalizeJSONObject(input.Policy)
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("policy: %w", err)
	}
	checksum := hashParts(input.Semver, input.AdapterRef, string(inputSchema), string(outputSchema), string(policy))
	var item ToolDraftVersion
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `UPDATE tenant_tool_draft_versions v SET semver=$3,adapter_ref=$4,input_schema_json=$5::jsonb,
			output_schema_json=$6::jsonb,policy_json=$7::jsonb,checksum=$8,updated_at=now()
			FROM tenant_tool_drafts d
			WHERE v.tenant_id=$1::uuid AND v.id=$2::uuid AND v.state='draft'
			AND d.tenant_id=v.tenant_id AND d.id=v.tool_id AND d.lifecycle_status='draft'
			RETURNING v.id::text,v.tool_id::text,v.semver,v.adapter_ref,v.input_schema_json,v.output_schema_json,v.policy_json,v.checksum,v.state,v.created_by,v.created_at,v.updated_at`,
			tenantID, versionID, input.Semver, input.AdapterRef, string(inputSchema), string(outputSchema), string(policy), checksum).Scan(
			&item.ID, &item.ToolID, &item.Semver, &item.AdapterRef, &item.InputSchema, &item.OutputSchema, &item.Policy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "tool_draft_version.updated", "tenant_tool_draft_version:"+item.ID)
	})
	if err != nil {
		return ToolDraftVersion{}, fmt.Errorf("update tenant tool draft version: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) DeleteToolDraftVersion(ctx context.Context, tenantID, versionID, actor string) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var toolID string
		if err := tx.QueryRowContext(ctx, `DELETE FROM tenant_tool_draft_versions v USING tenant_tool_drafts d
			WHERE v.tenant_id=$1::uuid AND v.id=$2::uuid AND v.state='draft'
			AND d.tenant_id=v.tenant_id AND d.id=v.tool_id AND d.lifecycle_status='draft'
			RETURNING v.tool_id::text`, tenantID, versionID).Scan(&toolID); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "tool_draft_version.deleted", "tenant_tool_draft_version:"+versionID)
	})
}
