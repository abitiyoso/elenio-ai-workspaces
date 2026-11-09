package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/elenio/agent-studio/internal/simpack"
)

type SIMPackResult struct {
	PackVersion string           `json:"pack_version"`
	Agents      []SIMPackOutcome `json:"agents"`
}

type SIMPackOutcome struct {
	AgentKey       string `json:"agent_key"`
	AgentVersionID string `json:"agent_version_id,omitempty"`
	Outcome        string `json:"outcome"` // configured, already_configured, no_unconfigured_draft
	EvalSuiteID    string `json:"eval_suite_id,omitempty"`
}

// ApplySIMPack bootstraps the SIM identities if needed and fills each agent's
// unconfigured draft with the pack definition, a draft skill, tool bindings
// and the baseline evaluation suite. Drafts an author already configured are
// left untouched, and nothing is published or approved.
func (r *ModelRegistry) ApplySIMPack(ctx context.Context, tenantID, actor string, pack simpack.Pack) (SIMPackResult, error) {
	if _, err := r.BootstrapSIMAgents(ctx, tenantID, actor); err != nil {
		return SIMPackResult{}, err
	}
	result := SIMPackResult{PackVersion: pack.PackVersion, Agents: make([]SIMPackOutcome, 0, len(pack.Agents))}
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		for _, def := range pack.Agents {
			outcome, err := applyPackAgent(ctx, tx, tenantID, actor, pack.PackVersion, def)
			if err != nil {
				return fmt.Errorf("%s: %w", def.Key, err)
			}
			result.Agents = append(result.Agents, outcome)
		}
		return nil
	})
	if err != nil {
		return SIMPackResult{}, fmt.Errorf("apply SIM pack: %w", err)
	}
	return result, nil
}

func applyPackAgent(ctx context.Context, tx *sql.Tx, tenantID, actor, packVersion string, def simpack.Agent) (SIMPackOutcome, error) {
	out := SIMPackOutcome{AgentKey: def.Key}
	var agentID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM agents WHERE tenant_id=$1::uuid AND agent_key=$2 AND lifecycle_status <> 'archived'`, tenantID, def.Key).Scan(&agentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			out.Outcome = "agent_missing_or_archived"
			return out, nil
		}
		return out, err
	}
	var versionID string
	var manifest json.RawMessage
	err := tx.QueryRowContext(ctx, `SELECT id::text, manifest_json FROM agent_versions WHERE tenant_id=$1::uuid AND agent_id=$2::uuid AND state='draft'
		AND coalesce(manifest_json->>'configured','false') = 'false' ORDER BY created_at LIMIT 1 FOR UPDATE`, tenantID, agentID).Scan(&versionID, &manifest)
	if errors.Is(err, sql.ErrNoRows) {
		out.Outcome = "already_configured"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.AgentVersionID = versionID
	if _, err := tx.ExecContext(ctx, `UPDATE agent_versions SET manifest_json=$3::jsonb, input_schema_json=$4::jsonb, output_schema_json=$5::jsonb,
		tool_policy_json=$6::jsonb WHERE tenant_id=$1::uuid AND id=$2::uuid AND state='draft'`,
		tenantID, versionID, string(def.Manifest), string(def.InputSchema), string(def.OutputSchema),
		mustJSON(map[string]any{"tools": def.Tools, "source": "sim-pack " + packVersion})); err != nil {
		return out, err
	}

	var skillID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM skills WHERE tenant_id=$1::uuid AND agent_id=$2::uuid AND skill_key=$3`, tenantID, agentID, def.Skill.Key).Scan(&skillID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `INSERT INTO skills (tenant_id, agent_id, skill_key, domain, created_by) SELECT $1::uuid, $2::uuid, $3, domain, $4
			FROM agents WHERE tenant_id=$1::uuid AND id=$2::uuid RETURNING id::text`, tenantID, agentID, def.Skill.Key, actor).Scan(&skillID)
	}
	if err != nil {
		return out, err
	}
	var skillVersionID string
	err = tx.QueryRowContext(ctx, `INSERT INTO skill_versions (tenant_id, skill_id, semver, instructions, procedures_json, examples_json, forbidden_actions_json, output_schema_json, created_by)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8::jsonb,$9)
		ON CONFLICT (tenant_id, skill_id, semver) DO NOTHING RETURNING id::text`,
		tenantID, skillID, def.Skill.Semver, def.Skill.Instructions, rawOr(def.Skill.Procedures, "[]"), rawOr(def.Skill.Examples, "[]"),
		rawOr(def.Skill.ForbiddenActions, "[]"), string(def.OutputSchema), actor).Scan(&skillVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM skill_versions WHERE tenant_id=$1::uuid AND skill_id=$2::uuid AND semver=$3`, tenantID, skillID, def.Skill.Semver).Scan(&skillVersionID)
	}
	if err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_skill_bindings (tenant_id, agent_version_id, skill_version_id) VALUES ($1::uuid,$2::uuid,$3::uuid) ON CONFLICT DO NOTHING`,
		tenantID, versionID, skillVersionID); err != nil {
		return out, err
	}
	for _, key := range def.Tools {
		res, err := tx.ExecContext(ctx, `INSERT INTO agent_tool_bindings (tenant_id, agent_version_id, tool_version_id)
			SELECT $1::uuid, $2::uuid, v.id FROM tool_versions v JOIN tools t ON t.id=v.tool_id
			WHERE t.tool_key=$3 AND t.lifecycle_status='active' AND v.published_at IS NOT NULL
			ORDER BY v.published_at DESC LIMIT 1 ON CONFLICT DO NOTHING`, tenantID, versionID, key)
		if err != nil {
			return out, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if err := audit(tx, tenantID, actor, "sim_pack.tool_unavailable", "tool:"+key); err != nil {
				return out, err
			}
		}
	}

	var suiteID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM eval_suites WHERE tenant_id=$1::uuid AND agent_id=$2::uuid AND suite_key=$3`, tenantID, agentID, def.EvalSuite.Key).Scan(&suiteID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `INSERT INTO eval_suites (tenant_id, agent_id, suite_key, description, created_by) VALUES ($1::uuid,$2::uuid,$3,$4,$5) RETURNING id::text`,
			tenantID, agentID, def.EvalSuite.Key, def.EvalSuite.Description, actor).Scan(&suiteID)
		if err != nil {
			return out, err
		}
		for _, c := range def.EvalSuite.Cases {
			if _, err := createEvalCaseTx(ctx, tx, tenantID, suiteID, EvalCaseInput{Key: c.Key, Classification: c.Classification, Critical: c.Critical,
				Input: c.Input, Evidence: c.EvidenceFixture, Expectations: c.Expectations, CreatedBy: actor}); err != nil {
				return out, err
			}
		}
	}
	if err != nil {
		return out, err
	}
	out.EvalSuiteID = suiteID
	out.Outcome = "configured"
	return out, audit(tx, tenantID, actor, "sim_pack.applied", "agent_version:"+versionID)
}

func mustJSON(v any) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}

func rawOr(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	return string(raw)
}
