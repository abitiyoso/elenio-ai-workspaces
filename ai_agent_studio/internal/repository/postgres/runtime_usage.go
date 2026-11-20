package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type RunUsage struct {
	Attempt       int       `json:"attempt"`
	DeploymentID  string    `json:"deployment_id,omitempty"`
	ConfigVersion int       `json:"config_version"`
	TokensIn      int       `json:"tokens_in"`
	TokensOut     int       `json:"tokens_out"`
	ToolCalls     int       `json:"tool_calls"`
	UsageReports  int       `json:"usage_reports"`
	MissingUsage  int       `json:"missing_usage"`
	RecordedAt    time.Time `json:"recorded_at"`
}

// Persist observations even when the attempt must retry or its worker loses the lease.
func (r *ModelRegistry) RecordRunUsage(ctx context.Context, tenantID, runID string, usage RunUsage) error {
	if usage.Attempt < 1 || usage.ConfigVersion < 0 || usage.TokensIn < 0 || usage.TokensOut < 0 || usage.TokensIn > 1<<31-1 || usage.TokensOut > 1<<31-1 || usage.ToolCalls < 0 || usage.UsageReports < 0 || usage.MissingUsage < 0 ||
		(usage.UsageReports == 0 && (usage.TokensIn != 0 || usage.TokensOut != 0)) {
		return ErrInvalidState
	}
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var attempts int
		if err := tx.QueryRowContext(ctx, `SELECT attempts FROM agent_runs WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, runID).Scan(&attempts); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRunNotFound
			}
			return err
		}
		if usage.Attempt > attempts {
			return ErrInvalidState
		}
		var existing RunUsage
		err := tx.QueryRowContext(ctx, `SELECT attempt,COALESCE(deployment_id::text,''),config_version,tokens_in,tokens_out,tool_calls,usage_reports,missing_usage FROM agent_run_usage WHERE tenant_id=$1::uuid AND run_id=$2::uuid AND attempt=$3`, tenantID, runID, usage.Attempt).Scan(&existing.Attempt, &existing.DeploymentID, &existing.ConfigVersion, &existing.TokensIn, &existing.TokensOut, &existing.ToolCalls, &existing.UsageReports, &existing.MissingUsage)
		if err == nil {
			usage.RecordedAt = time.Time{}
			if existing != usage {
				return ErrInvalidState
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_run_usage(tenant_id,run_id,attempt,deployment_id,config_version,tokens_in,tokens_out,tool_calls,usage_reports,missing_usage) VALUES($1::uuid,$2::uuid,$3,nullif($4,'')::uuid,$5,$6,$7,$8,$9,$10)`, tenantID, runID, usage.Attempt, usage.DeploymentID, usage.ConfigVersion, usage.TokensIn, usage.TokensOut, usage.ToolCalls, usage.UsageReports, usage.MissingUsage)
		return err
	})
}

func runUsageCoverage(run Run) string {
	if len(run.Usage) == 0 {
		if run.CompletedAt != nil && (run.TokensIn > 0 || run.TokensOut > 0 || run.ToolCalls > 0) {
			return "legacy_final_attempt"
		}
		return "unavailable"
	}
	reports, missing := 0, 0
	for _, usage := range run.Usage {
		reports += usage.UsageReports
		missing += usage.MissingUsage
		if usage.UsageReports == 0 {
			missing++
		}
	}
	if reports == 0 {
		return "unavailable"
	}
	if missing > 0 || len(run.Usage) < run.Attempts {
		return "partial"
	}
	return "complete"
}
