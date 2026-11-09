package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type EvalSuite struct {
	ID          string    `json:"id"`
	AgentKey    string    `json:"agent_key"`
	Key         string    `json:"key"`
	Description string    `json:"description"`
	CaseCount   int       `json:"case_count"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type EvalCase struct {
	ID             string          `json:"id"`
	SuiteID        string          `json:"suite_id"`
	Key            string          `json:"key"`
	Classification string          `json:"classification"`
	Critical       bool            `json:"critical"`
	Input          json.RawMessage `json:"input"`
	Evidence       json.RawMessage `json:"evidence_fixture"`
	Expectations   json.RawMessage `json:"expectations"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
}

type EvalCaseInput struct {
	Key, Classification, CreatedBy string
	Critical                       bool
	Input, Evidence, Expectations  json.RawMessage
}

type EvalRun struct {
	ID                 string          `json:"id"`
	SuiteID            string          `json:"suite_id"`
	SuiteKey           string          `json:"suite_key"`
	AgentKey           string          `json:"agent_key"`
	AgentVersionID     string          `json:"agent_version_id"`
	AgentSemver        string          `json:"agent_semver"`
	VersionFingerprint string          `json:"version_fingerprint"`
	BaselineEvalRunID  *string         `json:"baseline_eval_run_id,omitempty"`
	Status             string          `json:"status"`
	Summary            json.RawMessage `json:"summary"`
	CreatedBy          string          `json:"created_by"`
	CreatedAt          time.Time       `json:"created_at"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
	Stale              bool            `json:"stale"`
	Results            []EvalResult    `json:"results,omitempty"`
}

type EvalResult struct {
	CaseID         string          `json:"case_id"`
	CaseKey        string          `json:"case_key"`
	Classification string          `json:"classification"`
	Critical       bool            `json:"critical"`
	RunID          string          `json:"run_id"`
	RunStatus      string          `json:"run_status"`
	Passed         *bool           `json:"passed"`
	BaselinePassed *bool           `json:"baseline_passed,omitempty"`
	Checks         json.RawMessage `json:"checks"`
}

// EvalSummary decides the release gate. Any critical failure or critical
// regression blocks, regardless of the aggregate pass rate.
type EvalSummary struct {
	Total               int      `json:"total"`
	Passed              int      `json:"passed"`
	Failed              int      `json:"failed"`
	CriticalFailed      []string `json:"critical_failed"`
	Regressions         []string `json:"regressions"`
	CriticalRegressions []string `json:"critical_regressions"`
	Gate                string   `json:"gate"`
	Reasons             []string `json:"reasons"`
	ModelModes          []string `json:"model_modes"`
}

func (r *ModelRegistry) CreateEvalSuite(ctx context.Context, tenantID, agentKey, key, description, actor string) (EvalSuite, error) {
	var item EvalSuite
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var agentID string
		if err := tx.QueryRowContext(ctx, `SELECT id::text FROM agents WHERE tenant_id=$1::uuid AND agent_key=$2`, tenantID, agentKey).Scan(&agentID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrAgentNotFound
			}
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO eval_suites (tenant_id, agent_id, suite_key, description, created_by)
			VALUES ($1::uuid,$2::uuid,$3,$4,$5) RETURNING id::text, suite_key, description, created_by, created_at`,
			tenantID, agentID, key, description, actor).Scan(&item.ID, &item.Key, &item.Description, &item.CreatedBy, &item.CreatedAt); err != nil {
			return err
		}
		item.AgentKey = agentKey
		return audit(tx, tenantID, actor, "eval_suite.created", "eval_suite:"+item.ID)
	})
	return item, err
}

func (r *ModelRegistry) ListEvalSuites(ctx context.Context, tenantID, agentKey string) ([]EvalSuite, error) {
	items := make([]EvalSuite, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT s.id::text, a.agent_key, s.suite_key, s.description, s.created_by, s.created_at,
			(SELECT count(*) FROM eval_cases c WHERE c.tenant_id=s.tenant_id AND c.suite_id=s.id)
			FROM eval_suites s JOIN agents a ON a.tenant_id=s.tenant_id AND a.id=s.agent_id
			WHERE s.tenant_id=$1::uuid AND ($2='' OR a.agent_key=$2) ORDER BY a.agent_key, s.suite_key LIMIT 200`, tenantID, agentKey)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s EvalSuite
			if err := rows.Scan(&s.ID, &s.AgentKey, &s.Key, &s.Description, &s.CreatedBy, &s.CreatedAt, &s.CaseCount); err != nil {
				return err
			}
			items = append(items, s)
		}
		return rows.Err()
	})
	return items, err
}

func (r *ModelRegistry) CreateEvalCase(ctx context.Context, tenantID, suiteID string, input EvalCaseInput) (EvalCase, error) {
	var item EvalCase
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var err error
		item, err = createEvalCaseTx(ctx, tx, tenantID, suiteID, input)
		return err
	})
	return item, err
}

func createEvalCaseTx(ctx context.Context, tx *sql.Tx, tenantID, suiteID string, input EvalCaseInput) (EvalCase, error) {
	var item EvalCase
	if len(input.Evidence) == 0 {
		input.Evidence = json.RawMessage(`[]`)
	}
	if len(input.Expectations) == 0 {
		input.Expectations = json.RawMessage(`{}`)
	}
	err := tx.QueryRowContext(ctx, `INSERT INTO eval_cases (tenant_id, suite_id, case_key, classification, critical, input_json, evidence_fixture_json, expectations_json, created_by)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9)
		RETURNING id::text, suite_id::text, case_key, classification, critical, input_json, evidence_fixture_json, expectations_json, created_by, created_at`,
		tenantID, suiteID, input.Key, input.Classification, input.Critical, string(input.Input), string(input.Evidence), string(input.Expectations), input.CreatedBy).
		Scan(&item.ID, &item.SuiteID, &item.Key, &item.Classification, &item.Critical, &item.Input, &item.Evidence, &item.Expectations, &item.CreatedBy, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	return item, audit(tx, tenantID, input.CreatedBy, "eval_case.created", "eval_case:"+item.ID)
}

func (r *ModelRegistry) ListEvalCases(ctx context.Context, tenantID, suiteID string) ([]EvalCase, error) {
	items := make([]EvalCase, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id::text, suite_id::text, case_key, classification, critical, input_json, evidence_fixture_json, expectations_json, created_by, created_at
			FROM eval_cases WHERE tenant_id=$1::uuid AND suite_id=$2::uuid ORDER BY case_key`, tenantID, suiteID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c EvalCase
			if err := rows.Scan(&c.ID, &c.SuiteID, &c.Key, &c.Classification, &c.Critical, &c.Input, &c.Evidence, &c.Expectations, &c.CreatedBy, &c.CreatedAt); err != nil {
				return err
			}
			items = append(items, c)
		}
		return rows.Err()
	})
	return items, err
}

// DeleteEvalCase removes a case that has never been executed.
func (r *ModelRegistry) DeleteEvalCase(ctx context.Context, tenantID, caseID, actor string) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM eval_cases c WHERE c.tenant_id=$1::uuid AND c.id=$2::uuid
			AND NOT EXISTS (SELECT 1 FROM eval_results r WHERE r.tenant_id=c.tenant_id AND r.case_id=c.id)`, tenantID, caseID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrInvalidState
		}
		return audit(tx, tenantID, actor, "eval_case.deleted", "eval_case:"+caseID)
	})
}

// StartEvalRun queues one evaluation run per case against a pinned version
// fingerprint and records the active production release's last result on the
// same suite as baseline.
func (r *ModelRegistry) StartEvalRun(ctx context.Context, tenantID, suiteID, versionID, actor string, allowDevMock bool) (EvalRun, error) {
	var evalRunID string
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var agentID, agentKey string
		if err := tx.QueryRowContext(ctx, `SELECT s.agent_id::text, a.agent_key FROM eval_suites s JOIN agents a ON a.tenant_id=s.tenant_id AND a.id=s.agent_id
			WHERE s.tenant_id=$1::uuid AND s.id=$2::uuid`, tenantID, suiteID).Scan(&agentID, &agentKey); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrAgentNotFound
			}
			return err
		}
		version, err := loadAgentVersion(ctx, tx, tenantID, versionID)
		if err != nil {
			return err
		}
		if version.AgentID != agentID {
			return ErrAgentNotFound
		}
		fingerprint, _, _, err := versionFingerprint(ctx, tx, tenantID, version)
		if err != nil {
			return err
		}
		var baseline sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT e.id::text FROM eval_runs e JOIN releases rel ON rel.tenant_id=e.tenant_id AND rel.agent_version_id=e.agent_version_id
			WHERE e.tenant_id=$1::uuid AND e.suite_id=$2::uuid AND e.status='completed' AND rel.environment='production' AND rel.status='active'
			AND e.agent_version_id <> $3::uuid ORDER BY e.completed_at DESC LIMIT 1`, tenantID, suiteID, versionID).Scan(&baseline); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO eval_runs (tenant_id, suite_id, agent_version_id, version_fingerprint, baseline_eval_run_id, created_by)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6) RETURNING id::text`, tenantID, suiteID, versionID, fingerprint, baseline, actor).Scan(&evalRunID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text, critical, input_json, evidence_fixture_json FROM eval_cases WHERE tenant_id=$1::uuid AND suite_id=$2::uuid ORDER BY case_key`, tenantID, suiteID)
		if err != nil {
			return err
		}
		type pending struct {
			id       string
			critical bool
			input    json.RawMessage
			evidence json.RawMessage
		}
		cases := []pending{}
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.critical, &p.input, &p.evidence); err != nil {
				rows.Close()
				return err
			}
			cases = append(cases, p)
		}
		rows.Close()
		if len(cases) == 0 {
			return fmt.Errorf("%w: suite has no cases", ErrInvalidState)
		}
		for _, c := range cases {
			run, _, err := createRunTx(ctx, tx, tenantID, RunCreate{
				AgentKey: agentKey, AgentVersionID: versionID, Kind: "evaluation", CallerKind: "user", CallerSubject: actor,
				IdempotencyKey: "eval:" + evalRunID + ":" + c.id[:8], Input: c.input, Evidence: c.evidence,
				AllowDevMock: allowDevMock, EvalRunID: evalRunID, EvalCaseID: c.id,
			})
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO eval_results (tenant_id, eval_run_id, case_id, run_id, critical) VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5)`,
				tenantID, evalRunID, c.id, run.ID, c.critical); err != nil {
				return err
			}
		}
		return audit(tx, tenantID, actor, "eval_run.started", "eval_run:"+evalRunID)
	})
	if err != nil {
		return EvalRun{}, fmt.Errorf("start evaluation: %w", err)
	}
	return r.GetEvalRun(ctx, tenantID, evalRunID)
}

// EvalCaseForRun returns the case an evaluation run must be scored against.
func (r *ModelRegistry) EvalCaseForRun(ctx context.Context, tenantID, runID string) (EvalCase, bool, error) {
	var item EvalCase
	found := false
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT c.id::text, c.suite_id::text, c.case_key, c.classification, c.critical, c.input_json, c.evidence_fixture_json, c.expectations_json, c.created_by, c.created_at
			FROM agent_runs r JOIN eval_cases c ON c.tenant_id=r.tenant_id AND c.id=r.eval_case_id WHERE r.tenant_id=$1::uuid AND r.id=$2::uuid`, tenantID, runID).
			Scan(&item.ID, &item.SuiteID, &item.Key, &item.Classification, &item.Critical, &item.Input, &item.Evidence, &item.Expectations, &item.CreatedBy, &item.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	return item, found, err
}

// RecordEvalOutcome stores one scored case and completes the evaluation run
// once every case has settled.
func (r *ModelRegistry) RecordEvalOutcome(ctx context.Context, tenantID, runID string, passed bool, checks json.RawMessage) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var evalRunID string
		if err := tx.QueryRowContext(ctx, `UPDATE eval_results SET passed=$3, checks_json=$4::jsonb, completed_at=now()
			WHERE tenant_id=$1::uuid AND run_id=$2::uuid RETURNING eval_run_id::text`, tenantID, runID, passed, string(checks)).Scan(&evalRunID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT 1 FROM eval_runs WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, evalRunID); err != nil {
			return err
		}
		var pendingCount int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM eval_results WHERE tenant_id=$1::uuid AND eval_run_id=$2::uuid AND passed IS NULL`, tenantID, evalRunID).Scan(&pendingCount); err != nil {
			return err
		}
		if pendingCount > 0 {
			return nil
		}
		summary, err := summarizeEval(ctx, tx, tenantID, evalRunID)
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(summary)
		_, err = tx.ExecContext(ctx, `UPDATE eval_runs SET status='completed', summary_json=$3::jsonb, completed_at=now()
			WHERE tenant_id=$1::uuid AND id=$2::uuid AND status='running'`, tenantID, evalRunID, string(encoded))
		return err
	})
}

func summarizeEval(ctx context.Context, tx *sql.Tx, tenantID, evalRunID string) (EvalSummary, error) {
	results, err := loadEvalResults(ctx, tx, tenantID, evalRunID)
	if err != nil {
		return EvalSummary{}, err
	}
	s := EvalSummary{CriticalFailed: []string{}, Regressions: []string{}, CriticalRegressions: []string{}, Reasons: []string{}, ModelModes: []string{}}
	modes := map[string]bool{}
	for _, res := range results {
		s.Total++
		if res.Passed != nil && *res.Passed {
			s.Passed++
		} else {
			s.Failed++
			if res.Critical {
				s.CriticalFailed = append(s.CriticalFailed, res.CaseKey)
			}
			if res.BaselinePassed != nil && *res.BaselinePassed {
				s.Regressions = append(s.Regressions, res.CaseKey)
				if res.Critical {
					s.CriticalRegressions = append(s.CriticalRegressions, res.CaseKey)
				}
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT model_mode FROM agent_runs WHERE tenant_id=$1::uuid AND eval_run_id=$2::uuid ORDER BY 1`, tenantID, evalRunID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var mode string
		if err := rows.Scan(&mode); err != nil {
			return s, err
		}
		if !modes[mode] {
			modes[mode] = true
			s.ModelModes = append(s.ModelModes, mode)
		}
	}
	s.Gate = "pass"
	if len(s.CriticalFailed) > 0 {
		s.Gate = "blocked"
		s.Reasons = append(s.Reasons, "critical_case_failed")
	}
	if len(s.CriticalRegressions) > 0 {
		s.Gate = "blocked"
		s.Reasons = append(s.Reasons, "critical_regression_against_baseline")
	}
	if modes["dev_mock"] {
		s.Gate = "blocked"
		s.Reasons = append(s.Reasons, "evaluated_with_dev_mock_model")
	}
	return s, rows.Err()
}

func loadEvalResults(ctx context.Context, tx *sql.Tx, tenantID, evalRunID string) ([]EvalResult, error) {
	rows, err := tx.QueryContext(ctx, `SELECT res.case_id::text, c.case_key, c.classification, res.critical, res.run_id::text, ar.status, res.passed, res.checks_json,
		(SELECT b.passed FROM eval_results b JOIN eval_runs e ON e.tenant_id=b.tenant_id AND e.id=b.eval_run_id
		 WHERE b.tenant_id=res.tenant_id AND b.case_id=res.case_id AND e.id=er.baseline_eval_run_id)
		FROM eval_results res JOIN eval_cases c ON c.tenant_id=res.tenant_id AND c.id=res.case_id
		JOIN eval_runs er ON er.tenant_id=res.tenant_id AND er.id=res.eval_run_id
		JOIN agent_runs ar ON ar.tenant_id=res.tenant_id AND ar.id=res.run_id
		WHERE res.tenant_id=$1::uuid AND res.eval_run_id=$2::uuid ORDER BY c.case_key`, tenantID, evalRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EvalResult, 0)
	for rows.Next() {
		var res EvalResult
		if err := rows.Scan(&res.CaseID, &res.CaseKey, &res.Classification, &res.Critical, &res.RunID, &res.RunStatus, &res.Passed, &res.Checks, &res.BaselinePassed); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

const evalRunColumns = `e.id::text, e.suite_id::text, s.suite_key, a.agent_key, e.agent_version_id::text, v.semver, e.version_fingerprint,
	e.baseline_eval_run_id::text, e.status, e.summary_json, e.created_by, e.created_at, e.completed_at`
const evalRunFrom = ` FROM eval_runs e JOIN eval_suites s ON s.tenant_id=e.tenant_id AND s.id=e.suite_id
	JOIN agents a ON a.tenant_id=s.tenant_id AND a.id=s.agent_id JOIN agent_versions v ON v.tenant_id=e.tenant_id AND v.id=e.agent_version_id`

func scanEvalRun(row rowScanner) (EvalRun, error) {
	var e EvalRun
	err := row.Scan(&e.ID, &e.SuiteID, &e.SuiteKey, &e.AgentKey, &e.AgentVersionID, &e.AgentSemver, &e.VersionFingerprint,
		&e.BaselineEvalRunID, &e.Status, &e.Summary, &e.CreatedBy, &e.CreatedAt, &e.CompletedAt)
	return e, err
}

func (r *ModelRegistry) GetEvalRun(ctx context.Context, tenantID, evalRunID string) (EvalRun, error) {
	var item EvalRun
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var err error
		item, err = scanEvalRun(tx.QueryRowContext(ctx, `SELECT `+evalRunColumns+evalRunFrom+` WHERE e.tenant_id=$1::uuid AND e.id=$2::uuid`, tenantID, evalRunID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRunNotFound
		}
		if err != nil {
			return err
		}
		if item.Results, err = loadEvalResults(ctx, tx, tenantID, evalRunID); err != nil {
			return err
		}
		item.Stale, err = evalStale(ctx, tx, tenantID, item)
		return err
	})
	return item, err
}

func evalStale(ctx context.Context, tx *sql.Tx, tenantID string, item EvalRun) (bool, error) {
	version, err := loadAgentVersion(ctx, tx, tenantID, item.AgentVersionID)
	if err != nil {
		return false, err
	}
	current, _, _, err := versionFingerprint(ctx, tx, tenantID, version)
	return current != item.VersionFingerprint, err
}

func (r *ModelRegistry) ListEvalRuns(ctx context.Context, tenantID, agentKey string, limit, offset int) ([]EvalRun, int, error) {
	limit, offset = registryPage(limit, offset)
	items := make([]EvalRun, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		where := ` WHERE e.tenant_id=$1::uuid AND ($2='' OR a.agent_key=$2)`
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+evalRunFrom+where, tenantID, agentKey).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+evalRunColumns+evalRunFrom+where+` ORDER BY e.created_at DESC LIMIT $3 OFFSET $4`, tenantID, agentKey, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanEvalRun(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		for i := range items {
			stale, err := evalStale(ctx, tx, tenantID, items[i])
			if err != nil {
				return err
			}
			items[i].Stale = stale
		}
		return nil
	})
	return items, total, err
}
