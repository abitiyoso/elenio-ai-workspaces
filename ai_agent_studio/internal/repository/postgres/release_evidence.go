package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"time"
)

type ReleaseDependencyPin struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Checksum  string `json:"checksum"`
	State     string `json:"state"`
	ScopeHash string `json:"scope_hash,omitempty"`
}
type ReleaseModelPin struct {
	ID            string `json:"id"`
	ConfigVersion int    `json:"config_version"`
	Approved      bool   `json:"approved"`
}
type ReleaseEvidence struct {
	Schema                         string                 `json:"schema"`
	AgentKey                       string                 `json:"agent_key"`
	Environment                    string                 `json:"environment"`
	ReleaseID                      string                 `json:"release_id"`
	AgentVersion                   ReleaseDependencyPin   `json:"agent_version"`
	Fingerprint                    string                 `json:"fingerprint"`
	RequestID                      string                 `json:"request_id,omitempty"`
	EvaluationID                   string                 `json:"evaluation_id,omitempty"`
	EvaluationGate                 string                 `json:"evaluation_gate"`
	IndependentApproval            bool                   `json:"independent_approval"`
	SelfApprovalExempt             bool                   `json:"self_approval_exempt"`
	EvaluationPrimaryConfigMatches bool                   `json:"evaluation_primary_config_matches"`
	PrimaryModel                   *ReleaseModelPin       `json:"primary_model,omitempty"`
	FallbackModel                  *ReleaseModelPin       `json:"fallback_model,omitempty"`
	Skills                         []ReleaseDependencyPin `json:"skills"`
	Tools                          []ReleaseDependencyPin `json:"tools"`
	Blockers                       []string               `json:"blockers"`
	State                          string                 `json:"state"`
	ContentHash                    string                 `json:"content_hash"`
	ObservedAt                     time.Time              `json:"observed_at"`
}

// ObserveRelease reuses domain records without creating a run, altering routing or exporting config.
func (r *ModelRegistry) ObserveRelease(ctx context.Context, tenantID, agentKey, pin, environment string) (ReleaseEvidence, error) {
	out := ReleaseEvidence{Schema: "elenio.agent-release-evidence.v1", AgentKey: agentKey, Environment: environment, State: "blocked",
		Skills: []ReleaseDependencyPin{}, Tools: []ReleaseDependencyPin{}, Blockers: []string{}, ObservedAt: time.Now().UTC()}
	if environment != "production" && environment != "staging" {
		return out, ErrInvalidState
	}
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rel, err := scanRelease(tx.QueryRowContext(ctx, "SELECT "+releaseColumns+releaseFrom+` WHERE rel.tenant_id=$1::uuid AND a.agent_key=$2 AND a.lifecycle_status<>'archived' AND rel.environment=$3 AND rel.status='active'`, tenantID, agentKey, environment))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoActiveRelease
		}
		if err != nil {
			return err
		}
		version, err := loadAgentVersion(ctx, tx, tenantID, rel.AgentVersionID)
		if err != nil {
			return err
		}
		if pin != "" && pin != "unpinned" && pin != version.ID && pin != version.Semver {
			return ErrVersionMismatch
		}
		out.ReleaseID = rel.ID
		out.AgentVersion = ReleaseDependencyPin{ID: version.ID, Version: version.Semver, State: version.State, Checksum: stringValue(version.Checksum)}
		if version.State != "published" || out.AgentVersion.Checksum == "" {
			out.Blockers = append(out.Blockers, "agent_version_not_published")
		}
		var skillCount, toolCount int
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM agent_skill_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid),(SELECT count(*) FROM agent_tool_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid)`, tenantID, version.ID).Scan(&skillCount, &toolCount); err != nil {
			return err
		}
		if skillCount > 100 || toolCount > 100 {
			out.Blockers = append(out.Blockers, "dependency_inventory_exceeds_limit")
			return nil
		}
		fingerprint, skills, tools, err := versionFingerprint(ctx, tx, tenantID, version)
		if err != nil {
			return err
		}
		out.Fingerprint = fingerprint
		if len(skills) > 100 || len(tools) > 100 {
			out.Blockers = append(out.Blockers, "dependency_inventory_exceeds_limit")
			return nil
		}
		for _, s := range skills {
			out.Skills = append(out.Skills, ReleaseDependencyPin{ID: s.ID, Version: s.Semver, State: s.State, Checksum: stringValue(s.Checksum)})
			if s.State != "published" || s.Checksum == nil || *s.Checksum == "" {
				out.Blockers = append(out.Blockers, "skill_version_not_published")
			}
		}
		for _, t := range tools {
			scopeHash, err := OutputChecksum(t.Scope)
			if err != nil {
				return err
			}
			out.Tools = append(out.Tools, ReleaseDependencyPin{ID: t.ID, Version: t.Semver, State: t.Lifecycle, Checksum: stringValue(t.Checksum), ScopeHash: scopeHash})
			if t.Lifecycle != "active" || t.PublishedAt == nil || t.Checksum == nil || *t.Checksum == "" {
				out.Blockers = append(out.Blockers, "tool_version_not_active")
			}
		}
		// Rollback routing rows cite the earlier approved request for the exact restored version.
		var requestID string
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM release_requests WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid AND environment=$3 AND status='published' AND decided_at<=$4 ORDER BY decided_at DESC,id LIMIT 1`, tenantID, version.ID, environment, rel.CreatedAt).Scan(&requestID)
		if errors.Is(err, sql.ErrNoRows) {
			out.Blockers = append(out.Blockers, "approved_release_request_missing")
		} else if err != nil {
			return err
		} else {
			q, err := scanRequest(tx.QueryRowContext(ctx, "SELECT "+requestColumns+requestFrom+` WHERE q.tenant_id=$1::uuid AND q.id=$2::uuid`, tenantID, requestID))
			if err != nil {
				return err
			}
			out.RequestID, out.EvaluationID, out.SelfApprovalExempt = q.ID, q.EvalRunID, q.SelfApprovalExempt
			out.IndependentApproval = !q.SelfApprovalExempt && q.RequestedBy != "" && q.ReviewedBy != nil && q.DecidedBy != nil &&
				q.ReviewedAt != nil && q.DecidedAt != nil && !q.ReviewedAt.After(*q.DecidedAt) &&
				*q.ReviewedBy != "" && *q.DecidedBy != "" && q.RequestedBy != *q.ReviewedBy && q.RequestedBy != *q.DecidedBy && *q.ReviewedBy != *q.DecidedBy
			if !out.IndependentApproval {
				out.Blockers = append(out.Blockers, "independent_release_approval_unverified")
			}
			var evalVersion, evalFingerprint, status, requestFingerprint string
			var summaryRaw json.RawMessage
			if err = tx.QueryRowContext(ctx, `SELECT e.agent_version_id::text,e.version_fingerprint,e.status,e.summary_json,q.version_fingerprint FROM eval_runs e JOIN release_requests q ON q.tenant_id=e.tenant_id AND q.eval_run_id=e.id WHERE e.tenant_id=$1::uuid AND q.id=$2::uuid`, tenantID, q.ID).Scan(&evalVersion, &evalFingerprint, &status, &summaryRaw, &requestFingerprint); err != nil {
				return err
			}
			var summary EvalSummary
			if err = json.Unmarshal(summaryRaw, &summary); err != nil {
				return err
			}
			out.EvaluationGate = summary.Gate
			if status != "completed" || evalVersion != version.ID || evalFingerprint != fingerprint || requestFingerprint != fingerprint || summary.Gate != "pass" || len(summary.ModelModes) != 1 || summary.ModelModes[0] != "provider" {
				out.Blockers = append(out.Blockers, "current_provider_evaluation_unverified")
			}
		}
		var primary, fallback sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT model_deployment_id::text,fallback_deployment_id::text FROM agent_model_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid`, tenantID, version.ID).Scan(&primary, &fallback)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		observeModel := func(id string) (*ReleaseModelPin, error) {
			m := &ReleaseModelPin{ID: id}
			if err := tx.QueryRowContext(ctx, `SELECT config_version FROM llm_model_deployments WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, id).Scan(&m.ConfigVersion); err != nil {
				return nil, err
			}
			err := requireApprovedDeployment(tx, tenantID, id, environment)
			if err != nil && !errors.Is(err, ErrModelNotApproved) {
				return nil, err
			}
			m.Approved = err == nil
			return m, nil
		}
		if primary.Valid {
			out.PrimaryModel, err = observeModel(primary.String)
			if err != nil {
				return err
			}
		}
		if out.PrimaryModel == nil || !out.PrimaryModel.Approved {
			out.Blockers = append(out.Blockers, "primary_model_not_currently_approved")
		}
		if fallback.Valid {
			out.FallbackModel, err = observeModel(fallback.String)
			if err != nil {
				return err
			}
			if !out.FallbackModel.Approved {
				out.Blockers = append(out.Blockers, "fallback_model_not_currently_approved")
			}
			// shortcut: legacy evaluation pins omit fallback config versions; verify those pins before portable approval.
			out.Blockers = append(out.Blockers, "evaluation_fallback_config_unverified")
		}
		if out.PrimaryModel != nil && out.EvaluationID != "" {
			var total, matches int
			if err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE ar.model_mode='provider' AND ar.pinned_json->'model'->'target'->>'deployment_id'=$3 AND ar.pinned_json->'model'->'target'->>'config_version'=$4)
    FROM eval_results res JOIN agent_runs ar ON ar.tenant_id=res.tenant_id AND ar.id=res.run_id WHERE res.tenant_id=$1::uuid AND res.eval_run_id=$2::uuid`, tenantID, out.EvaluationID, out.PrimaryModel.ID, strconv.Itoa(out.PrimaryModel.ConfigVersion)).Scan(&total, &matches); err != nil {
				return err
			}
			out.EvaluationPrimaryConfigMatches = total > 0 && matches == total
		}
		if !out.EvaluationPrimaryConfigMatches {
			out.Blockers = append(out.Blockers, "evaluation_primary_config_unverified")
		}
		if len(out.Blockers) == 0 {
			out.State = "verified"
		}
		return nil
	})
	if err != nil {
		return ReleaseEvidence{}, err
	}
	sort.Strings(out.Blockers)
	out.Blockers = slices.Compact(out.Blockers)
	raw, err := json.Marshal(out)
	if err != nil {
		return ReleaseEvidence{}, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return ReleaseEvidence{}, err
	}
	delete(fields, "content_hash")
	delete(fields, "observed_at")
	raw, err = json.Marshal(fields)
	if err != nil {
		return ReleaseEvidence{}, err
	}
	out.ContentHash, err = OutputChecksum(raw)
	return out, err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
