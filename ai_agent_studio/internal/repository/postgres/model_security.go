package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrCapabilityCheckRequired = errors.New("a current successful capability check is required")
	ErrModelNotApproved        = errors.New("model deployment is not approved for this tenant environment")
)

type ProviderSecurityRecord struct {
	ID          string
	Key         string
	AdapterType string
	BaseURL     string
	Status      string
	SecretRef   string
}

type ModelApproval struct {
	ID                 string     `json:"id"`
	DeploymentID       string     `json:"deployment_id"`
	Environment        string     `json:"environment"`
	Purpose            string     `json:"purpose"`
	State              string     `json:"state"`
	CapabilityCheckRef *string    `json:"capability_check_ref,omitempty"`
	DecisionReason     string     `json:"decision_reason"`
	DecidedBy          *string    `json:"decided_by,omitempty"`
	DecidedAt          *time.Time `json:"decided_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type AgentModelBinding struct {
	AgentVersionID     string          `json:"agent_version_id"`
	ModelDeploymentID  string          `json:"model_deployment_id"`
	FallbackDeployment *string         `json:"fallback_model_deployment_id,omitempty"`
	Environment        string          `json:"environment"`
	OverridePolicy     json.RawMessage `json:"override_policy"`
}

type ProviderProbeTarget struct {
	Provider   ProviderSecurityRecord
	Deployment Deployment
}

type ProviderProbeRecord struct {
	ID                      string
	ProviderID              string
	DeploymentID            string
	Outcome                 string
	Capabilities            json.RawMessage
	RedactedErrorCode       string
	DeploymentConfigVersion int
	CheckedAt               time.Time
}

func (r *ModelRegistry) GetProviderSecurity(ctx context.Context, tenantID, providerID string) (ProviderSecurityRecord, error) {
	var provider ProviderSecurityRecord
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id::text,provider_key,adapter_type,base_url,status,coalesce(secret_ref,'')
			FROM llm_providers WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, providerID).
			Scan(&provider.ID, &provider.Key, &provider.AdapterType, &provider.BaseURL, &provider.Status, &provider.SecretRef)
	})
	if err != nil {
		return ProviderSecurityRecord{}, fmt.Errorf("get provider security record: %w", err)
	}
	return provider, nil
}

func (r *ModelRegistry) SetProviderCredentialRef(ctx context.Context, tenantID, providerID, secretRef, actor string) (string, error) {
	if secretRef == "" {
		return "", errors.New("credential reference is required")
	}
	var previous string
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(secret_ref,'') FROM llm_providers WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, providerID).Scan(&previous); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE llm_providers SET secret_ref=$3,status=CASE WHEN status='disabled' THEN 'disabled' ELSE 'pending_test' END,updated_at=now()
			WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, providerID, secretRef); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE llm_model_deployments SET status=CASE WHEN status='disabled' THEN 'disabled' ELSE 'pending_test' END,config_version=config_version+1,updated_at=now()
			WHERE tenant_id=$1::uuid AND provider_id=$2::uuid`, tenantID, providerID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE model_approvals SET state='revoked',capability_check_ref=NULL,decision_reason='provider credential changed',decided_by=NULL,decided_at=NULL
			WHERE tenant_id=$1::uuid AND deployment_id IN (SELECT id FROM llm_model_deployments WHERE tenant_id=$1::uuid AND provider_id=$2::uuid) AND state='approved'`, tenantID, providerID); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "model_provider.credential_rotated", "llm_provider:"+providerID)
	})
	if err != nil {
		return "", fmt.Errorf("set provider credential reference: %w", err)
	}
	return previous, nil
}

func (r *ModelRegistry) DeleteProviderCredentialRef(ctx context.Context, tenantID, providerID, actor string) (string, error) {
	var previous string
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(secret_ref,'') FROM llm_providers WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, providerID).Scan(&previous); err != nil {
			return err
		}
		if previous == "" {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(ctx, `UPDATE llm_providers SET secret_ref=NULL,status='disabled',updated_at=now()
			WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, providerID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE llm_model_deployments SET status='disabled',config_version=config_version+1,updated_at=now()
			WHERE tenant_id=$1::uuid AND provider_id=$2::uuid`, tenantID, providerID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE model_approvals SET state='revoked',capability_check_ref=NULL,decision_reason='provider credential removed',decided_by=NULL,decided_at=NULL
			WHERE tenant_id=$1::uuid AND deployment_id IN (SELECT id FROM llm_model_deployments WHERE tenant_id=$1::uuid AND provider_id=$2::uuid) AND state='approved'`, tenantID, providerID); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "model_provider.credential_removed", "llm_provider:"+providerID)
	})
	if err != nil {
		return "", fmt.Errorf("delete provider credential reference: %w", err)
	}
	return previous, nil
}

func (r *ModelRegistry) GetProviderProbeTarget(ctx context.Context, tenantID, providerID, deploymentID string) (ProviderProbeTarget, error) {
	var target ProviderProbeTarget
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id::text,provider_key,adapter_type,base_url,status,coalesce(secret_ref,'')
			FROM llm_providers WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, providerID).
			Scan(&target.Provider.ID, &target.Provider.Key, &target.Provider.AdapterType, &target.Provider.BaseURL, &target.Provider.Status, &target.Provider.SecretRef); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id::text,provider_id::text,deployment_key,provider_model_id,display_name,
			capabilities_json,max_output_tokens,timeout_ms,status,created_at
			FROM llm_model_deployments WHERE tenant_id=$1::uuid AND provider_id=$2::uuid AND id=$3::uuid`, tenantID, providerID, deploymentID).
			Scan(&target.Deployment.ID, &target.Deployment.ProviderID, &target.Deployment.Key, &target.Deployment.ProviderModelID,
				&target.Deployment.DisplayName, &target.Deployment.Capabilities, &target.Deployment.MaxOutputTokens,
				&target.Deployment.TimeoutMillis, &target.Deployment.Status, &target.Deployment.CreatedAt); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ProviderProbeTarget{}, fmt.Errorf("get provider probe target: %w", err)
	}
	return target, nil
}

func (r *ModelRegistry) RecordProviderProbe(ctx context.Context, tenantID, providerID, deploymentID, actor string, outcome string, capabilities json.RawMessage, redactedCode string) (ProviderProbeRecord, error) {
	if outcome != "passed" && outcome != "failed" {
		return ProviderProbeRecord{}, errors.New("probe outcome must be passed or failed")
	}
	caps, err := normalizeJSONObject(capabilities)
	if err != nil {
		return ProviderProbeRecord{}, fmt.Errorf("verified capabilities: %w", err)
	}
	var result ProviderProbeRecord
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var declared json.RawMessage
		var credentialConfigured bool
		if err := tx.QueryRowContext(ctx, `SELECT d.capabilities_json,p.secret_ref IS NOT NULL FROM llm_model_deployments d
			JOIN llm_providers p ON p.tenant_id=d.tenant_id AND p.id=d.provider_id
			WHERE d.tenant_id=$1::uuid AND d.provider_id=$2::uuid AND d.id=$3::uuid FOR UPDATE OF d,p`, tenantID, providerID, deploymentID).
			Scan(&declared, &credentialConfigured); err != nil {
			return err
		}
		if !credentialConfigured {
			return errors.New("provider credential is not configured")
		}
		idBytes := make([]byte, 16)
		if _, err := rand.Read(idBytes); err != nil {
			return err
		}
		result.ID = hex.EncodeToString(idBytes)
		var version int
		if err := tx.QueryRowContext(ctx, `UPDATE llm_model_deployments SET status=$4,config_version=config_version+1,updated_at=now()
			WHERE tenant_id=$1::uuid AND provider_id=$2::uuid AND id=$3::uuid RETURNING config_version`,
			tenantID, providerID, deploymentID, probeStatus(outcome)).Scan(&version); err != nil {
			return err
		}
		var declaredMap map[string]json.RawMessage
		if err := json.Unmarshal(declared, &declaredMap); err != nil || declaredMap == nil {
			declaredMap = make(map[string]json.RawMessage)
		}
		var verified map[string]json.RawMessage
		if err := json.Unmarshal(caps, &verified); err != nil || verified == nil {
			return errors.New("verified capabilities must be an object")
		}
		verification, err := json.Marshal(map[string]any{
			"id": result.ID, "outcome": outcome, "checked_at": time.Now().UTC(),
			"config_version": version, "capabilities": verified, "error_code": redactedCode,
		})
		if err != nil {
			return err
		}
		declaredMap["verification"] = verification
		stored, err := json.Marshal(declaredMap)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE llm_model_deployments SET capabilities_json=$4::jsonb WHERE tenant_id=$1::uuid AND provider_id=$2::uuid AND id=$3::uuid`, tenantID, providerID, deploymentID, string(stored)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE llm_providers SET status=$3,updated_at=now() WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, providerID, probeStatus(outcome)); err != nil {
			return err
		}
		result.ProviderID, result.DeploymentID, result.Outcome, result.Capabilities = providerID, deploymentID, outcome, caps
		result.DeploymentConfigVersion, result.CheckedAt = version, time.Now().UTC()
		resource := "llm_provider:" + providerID + "/deployment:" + deploymentID + "/probe:" + result.ID + "/" + outcome
		return audit(tx, tenantID, actor, "model_provider.probed", resource)
	})
	if err != nil {
		return ProviderProbeRecord{}, fmt.Errorf("record provider probe: %w", err)
	}
	return result, nil
}

func (r *ModelRegistry) ListModelApprovals(ctx context.Context, tenantID, deploymentID, environment string) ([]ModelApproval, error) {
	items := make([]ModelApproval, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id::text,deployment_id::text,environment,purpose,state,capability_check_ref,
			decision_reason,decided_by,decided_at,created_at FROM model_approvals
			WHERE tenant_id=$1::uuid AND deployment_id=$2::uuid AND environment=$3 ORDER BY purpose`, tenantID, deploymentID, environment)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item ModelApproval
			if err := rows.Scan(&item.ID, &item.DeploymentID, &item.Environment, &item.Purpose, &item.State,
				&item.CapabilityCheckRef, &item.DecisionReason, &item.DecidedBy, &item.DecidedAt, &item.CreatedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list model approvals: %w", err)
	}
	return items, nil
}

func (r *ModelRegistry) SetModelApproval(ctx context.Context, tenantID, deploymentID, environment, state, reason, actor string) (ModelApproval, error) {
	if !validEnvironment(environment) || (state != "approved" && state != "rejected" && state != "revoked") {
		return ModelApproval{}, errors.New("model approval state or environment is invalid")
	}
	var item ModelApproval
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var capabilityRef sql.NullString
		if state == "approved" {
			var currentVersion int
			var recordedVersion int
			var probeID sql.NullString
			if err := tx.QueryRowContext(ctx, `SELECT d.config_version,coalesce(d.capabilities_json->'verification'->>'id',''),
				coalesce((d.capabilities_json->'verification'->>'config_version')::integer,0)
				FROM llm_model_deployments d JOIN llm_providers p ON p.tenant_id=d.tenant_id AND p.id=d.provider_id
				WHERE d.tenant_id=$1::uuid AND d.id=$2::uuid AND d.status='active' AND p.status='active' AND p.secret_ref IS NOT NULL
				AND (coalesce((d.capabilities_json->'verification'->'capabilities'->'capabilities'->>'json')::boolean,false)
					OR coalesce((d.capabilities_json->'verification'->'capabilities'->'capabilities'->>'tools')::boolean,false))`, tenantID, deploymentID).
				Scan(&currentVersion, &probeID, &recordedVersion); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrCapabilityCheckRequired
				}
				return err
			}
			if !probeID.Valid || recordedVersion != currentVersion {
				return ErrCapabilityCheckRequired
			}
			capabilityRef = probeID
		}
		var decidedBy any
		var decidedAt any
		if state == "approved" {
			decidedBy = actor
			decidedAt = time.Now().UTC()
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO model_approvals
			(tenant_id,deployment_id,environment,purpose,state,capability_check_ref,decision_reason,decided_by,decided_at)
			VALUES ($1::uuid,$2::uuid,$3,'agent_runtime',$4,$5,$6,$7,$8)
			ON CONFLICT (tenant_id,deployment_id,environment,purpose) DO UPDATE SET
			state=EXCLUDED.state,capability_check_ref=EXCLUDED.capability_check_ref,decision_reason=EXCLUDED.decision_reason,
			decided_by=EXCLUDED.decided_by,decided_at=EXCLUDED.decided_at
			RETURNING id::text,deployment_id::text,environment,purpose,state,capability_check_ref,decision_reason,decided_by,decided_at,created_at`,
			tenantID, deploymentID, environment, state, nullableString(capabilityRef), reason, decidedBy, decidedAt).
			Scan(&item.ID, &item.DeploymentID, &item.Environment, &item.Purpose, &item.State,
				&item.CapabilityCheckRef, &item.DecisionReason, &item.DecidedBy, &item.DecidedAt, &item.CreatedAt); err != nil {
			return err
		}
		return audit(tx, tenantID, actor, "model_deployment.approval_"+state, "llm_model_deployment:"+deploymentID+"/"+environment)
	})
	if err != nil {
		return ModelApproval{}, fmt.Errorf("set model approval: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) GetAgentModelBinding(ctx context.Context, tenantID, versionID string) (AgentModelBinding, error) {
	var item AgentModelBinding
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var override json.RawMessage
		if err := tx.QueryRowContext(ctx, `SELECT agent_version_id::text,model_deployment_id::text,
			fallback_deployment_id::text,override_policy_json FROM agent_model_bindings
			WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid`, tenantID, versionID).
			Scan(&item.AgentVersionID, &item.ModelDeploymentID, &item.FallbackDeployment, &override); err != nil {
			return err
		}
		item.Environment = environmentFromPolicy(override)
		item.OverridePolicy = override
		return nil
	})
	if err != nil {
		return AgentModelBinding{}, fmt.Errorf("get agent model binding: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) SetAgentModelBinding(ctx context.Context, tenantID, versionID, deploymentID, fallbackID, environment, actor string, overrides json.RawMessage) (AgentModelBinding, error) {
	if !validEnvironment(environment) {
		return AgentModelBinding{}, errors.New("model binding environment is invalid")
	}
	policy, err := normalizeJSONObject(overrides)
	if err != nil {
		return AgentModelBinding{}, fmt.Errorf("model binding override policy: %w", err)
	}
	var policyMap map[string]json.RawMessage
	if err := json.Unmarshal(policy, &policyMap); err != nil {
		return AgentModelBinding{}, err
	}
	environmentJSON, _ := json.Marshal(environment)
	policyMap["environment"], policyMap["purpose"] = environmentJSON, json.RawMessage(`"agent_runtime"`)
	policy, err = json.Marshal(policyMap)
	if err != nil {
		return AgentModelBinding{}, err
	}
	var item AgentModelBinding
	err = r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if err := requireApprovedDeployment(tx, tenantID, deploymentID, environment); err != nil {
			return err
		}
		if fallbackID != "" {
			if err := requireApprovedDeployment(tx, tenantID, fallbackID, environment); err != nil {
				return fmt.Errorf("fallback deployment: %w", err)
			}
		}
		var fallback any
		if fallbackID != "" {
			fallback = fallbackID
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO agent_model_bindings
			(tenant_id,agent_version_id,model_deployment_id,fallback_deployment_id,override_policy_json)
			SELECT $1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::jsonb FROM agent_versions v
			WHERE v.tenant_id=$1::uuid AND v.id=$2::uuid AND v.state='draft'
			ON CONFLICT (tenant_id,agent_version_id) DO UPDATE SET model_deployment_id=EXCLUDED.model_deployment_id,
			fallback_deployment_id=EXCLUDED.fallback_deployment_id,override_policy_json=EXCLUDED.override_policy_json`,
			tenantID, versionID, deploymentID, fallback, string(policy))
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return sql.ErrNoRows
		}
		if err := tx.QueryRowContext(ctx, `SELECT agent_version_id::text,model_deployment_id::text,fallback_deployment_id::text,override_policy_json
			FROM agent_model_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid`, tenantID, versionID).
			Scan(&item.AgentVersionID, &item.ModelDeploymentID, &item.FallbackDeployment, &item.OverridePolicy); err != nil {
			return err
		}
		item.Environment = environment
		return audit(tx, tenantID, actor, "agent.model_binding_set", "agent_version:"+versionID+"/deployment:"+deploymentID+"/"+environment)
	})
	if err != nil {
		return AgentModelBinding{}, fmt.Errorf("set agent model binding: %w", err)
	}
	return item, nil
}

func (r *ModelRegistry) DeleteAgentModelBinding(ctx context.Context, tenantID, versionID, actor string) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM agent_model_bindings b USING agent_versions v
			WHERE b.tenant_id=$1::uuid AND b.agent_version_id=$2::uuid AND v.tenant_id=b.tenant_id AND v.id=b.agent_version_id AND v.state='draft'`, tenantID, versionID)
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
		return audit(tx, tenantID, actor, "agent.model_binding_deleted", "agent_version:"+versionID)
	})
}

func requireApprovedDeployment(tx *sql.Tx, tenantID, deploymentID, environment string) error {
	var approved bool
	if err := tx.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM llm_model_deployments d JOIN llm_providers p ON p.tenant_id=d.tenant_id AND p.id=d.provider_id
		JOIN model_approvals a ON a.tenant_id=d.tenant_id AND a.deployment_id=d.id
		WHERE d.tenant_id=$1::uuid AND d.id=$2::uuid AND d.status='active' AND p.status='active' AND p.secret_ref IS NOT NULL
		AND a.environment=$3 AND a.purpose='agent_runtime' AND a.state='approved'
		AND a.capability_check_ref=d.capabilities_json->'verification'->>'id'
		AND (d.capabilities_json->'verification'->>'config_version')::integer=d.config_version
	)`, tenantID, deploymentID, environment).Scan(&approved); err != nil {
		return err
	}
	if !approved {
		return ErrModelNotApproved
	}
	return nil
}

func probeStatus(outcome string) string {
	if outcome == "passed" {
		return "active"
	}
	return "error"
}

func validEnvironment(environment string) bool {
	switch environment {
	case "development", "staging", "production":
		return true
	default:
		return false
	}
}

func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func environmentFromPolicy(policy json.RawMessage) string {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(policy, &values); err != nil {
		return ""
	}
	var environment string
	_ = json.Unmarshal(values["environment"], &environment)
	return environment
}
