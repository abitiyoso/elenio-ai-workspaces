package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ModelRegistry persists tenant-scoped provider and model metadata. The caller
// must pass a tenant ID obtained from a verified AuthAdapter, never request
// input. PostgreSQL RLS provides a second tenant boundary for every operation.
type ModelRegistry struct {
	db *sql.DB
	// selfApprovalExempt lists subjects allowed to review or decide their own
	// release requests (development walkthroughs only; see cmd/agent-studio).
	selfApprovalExempt map[string]bool
}

// SetSelfApprovalExempt replaces the subjects exempt from release separation of
// duties. Call it once at startup, before serving requests.
func (r *ModelRegistry) SetSelfApprovalExempt(subjects []string) {
	r.selfApprovalExempt = make(map[string]bool, len(subjects))
	for _, s := range subjects {
		r.selfApprovalExempt[s] = true
	}
}

// IsSelfApprovalExempt reports whether subject may take more than one step of
// its own release.
func (r *ModelRegistry) IsSelfApprovalExempt(subject string) bool {
	return r != nil && r.selfApprovalExempt[subject]
}

func NewModelRegistry(db *sql.DB) (*ModelRegistry, error) {
	if db == nil {
		return nil, errors.New("postgres database is required")
	}
	return &ModelRegistry{db: db}, nil
}

type ProviderInput struct {
	Key         string
	DisplayName string
	AdapterType string
	BaseURL     string
	SecretRef   string
	CreatedBy   string
}

// Provider omits SecretRef so callers cannot accidentally serialize the vault
// reference alongside ordinary provider metadata.
type Provider struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	DisplayName string    `json:"display_name"`
	AdapterType string    `json:"adapter_type"`
	BaseURL     string    `json:"base_url"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type DeploymentInput struct {
	ProviderID      string
	Key             string
	ProviderModelID string
	DisplayName     string
	Capabilities    json.RawMessage
	MaxOutputTokens int
	TimeoutMillis   int
	CostPolicy      json.RawMessage
	CreatedBy       string
}

type Deployment struct {
	ID              string          `json:"id"`
	ProviderID      string          `json:"provider_id"`
	Key             string          `json:"key"`
	ProviderModelID string          `json:"provider_model_id"`
	DisplayName     string          `json:"display_name"`
	Capabilities    json.RawMessage `json:"capabilities"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	TimeoutMillis   int             `json:"timeout_ms"`
	Status          string          `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
}

// CreateProvider stores provider metadata and a backend-generated vault
// reference. It never accepts or persists the credential itself.
func (r *ModelRegistry) CreateProvider(ctx context.Context, tenantID string, input ProviderInput) (Provider, error) {
	var provider Provider
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO llm_providers
				(tenant_id, provider_key, display_name, adapter_type, base_url, secret_ref, created_by)
			VALUES ($1::uuid, $2, $3, $4, $5, nullif($6, ''), $7)
			RETURNING id::text, provider_key, display_name, adapter_type, base_url, status, created_at`,
			tenantID, input.Key, input.DisplayName, input.AdapterType, input.BaseURL, input.SecretRef, input.CreatedBy,
		).Scan(&provider.ID, &provider.Key, &provider.DisplayName, &provider.AdapterType,
			&provider.BaseURL, &provider.Status, &provider.CreatedAt); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_subject, action, resource_ref)
			VALUES ($1::uuid, $2, 'model_provider.created', $3)`,
			tenantID, input.CreatedBy, "llm_provider:"+provider.ID)
		return err
	})
	if err != nil {
		return Provider{}, fmt.Errorf("create model provider: %w", err)
	}
	return provider, nil
}

func (r *ModelRegistry) ListProviders(ctx context.Context, tenantID string) ([]Provider, error) {
	providers := make([]Provider, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id::text, provider_key, display_name, adapter_type, base_url, status, created_at
			FROM llm_providers WHERE tenant_id = $1::uuid ORDER BY created_at DESC, id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var provider Provider
			if err := rows.Scan(&provider.ID, &provider.Key, &provider.DisplayName,
				&provider.AdapterType, &provider.BaseURL, &provider.Status, &provider.CreatedAt); err != nil {
				return err
			}
			providers = append(providers, provider)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list model providers: %w", err)
	}
	return providers, nil
}

func (r *ModelRegistry) CreateDeployment(ctx context.Context, tenantID string, input DeploymentInput) (Deployment, error) {
	capabilities, err := normalizeJSONObject(input.Capabilities)
	if err != nil {
		return Deployment{}, fmt.Errorf("capabilities: %w", err)
	}
	costPolicy, err := normalizeJSONObject(input.CostPolicy)
	if err != nil {
		return Deployment{}, fmt.Errorf("cost policy: %w", err)
	}
	var deployment Deployment
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO llm_model_deployments
				(tenant_id, provider_id, deployment_key, provider_model_id, display_name,
				 capabilities_json, max_output_tokens, timeout_ms, cost_policy_json, created_by)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::jsonb, $7, $8, $9::jsonb, $10)
			RETURNING id::text, provider_id::text, deployment_key, provider_model_id,
			          display_name, capabilities_json, max_output_tokens, timeout_ms, status, created_at`,
			tenantID, input.ProviderID, input.Key, input.ProviderModelID, input.DisplayName,
			string(capabilities), input.MaxOutputTokens, input.TimeoutMillis, string(costPolicy), input.CreatedBy,
		).Scan(&deployment.ID, &deployment.ProviderID, &deployment.Key, &deployment.ProviderModelID,
			&deployment.DisplayName, &deployment.Capabilities, &deployment.MaxOutputTokens,
			&deployment.TimeoutMillis, &deployment.Status, &deployment.CreatedAt); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_subject, action, resource_ref)
			VALUES ($1::uuid, $2, 'model_deployment.created', $3)`,
			tenantID, input.CreatedBy, "llm_model_deployment:"+deployment.ID)
		return err
	})
	if err != nil {
		return Deployment{}, fmt.Errorf("create model deployment: %w", err)
	}
	return deployment, nil
}

func (r *ModelRegistry) ListDeployments(ctx context.Context, tenantID string) ([]Deployment, error) {
	deployments := make([]Deployment, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id::text, provider_id::text, deployment_key, provider_model_id, display_name,
			       capabilities_json, max_output_tokens, timeout_ms, status, created_at
			FROM llm_model_deployments WHERE tenant_id = $1::uuid ORDER BY created_at DESC, id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var deployment Deployment
			if err := rows.Scan(&deployment.ID, &deployment.ProviderID, &deployment.Key,
				&deployment.ProviderModelID, &deployment.DisplayName, &deployment.Capabilities,
				&deployment.MaxOutputTokens, &deployment.TimeoutMillis, &deployment.Status,
				&deployment.CreatedAt); err != nil {
				return err
			}
			deployments = append(deployments, deployment)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list model deployments: %w", err)
	}
	return deployments, nil
}

func (r *ModelRegistry) withTenant(ctx context.Context, tenantID string, fn func(*sql.Tx) error) error {
	if tenantID == "" {
		return errors.New("verified tenant ID is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeJSONObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("must be a JSON object")
	}
	return json.Marshal(value)
}
