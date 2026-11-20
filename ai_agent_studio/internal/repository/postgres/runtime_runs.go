package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrAgentNotFound       = errors.New("agent not found")
	ErrRunNotFound         = errors.New("run not found")
	ErrNoActiveRelease     = errors.New("agent has no active release in this environment")
	ErrVersionMismatch     = errors.New("requested version does not match the active release")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
	ErrBackpressure        = errors.New("tenant has too many queued or running runs")
	ErrInvalidState        = errors.New("operation is not allowed in the current state")
)

const maxActiveRunsPerTenant = 50

// Run is a tenant-scoped agent execution record. Output is always a draft for
// human review; there is no approved state.
type Run struct {
	ID                    string          `json:"id"`
	AgentID               string          `json:"agent_id"`
	AgentKey              string          `json:"agent_key"`
	AgentVersionID        string          `json:"agent_version_id"`
	AgentSemver           string          `json:"agent_semver"`
	Kind                  string          `json:"kind"`
	CallerKind            string          `json:"caller_kind"`
	CallerSubject         string          `json:"caller_subject"`
	IdempotencyKey        string          `json:"idempotency_key"`
	ExternalCorrelationID *string         `json:"external_correlation_id,omitempty"`
	CaseRef               *string         `json:"case_ref,omitempty"`
	DelegatedUser         *string         `json:"delegated_user,omitempty"`
	Input                 json.RawMessage `json:"input"`
	EvidenceInput         json.RawMessage `json:"evidence_input"`
	Status                string          `json:"status"`
	ModelMode             string          `json:"model_mode"`
	Pinned                json.RawMessage `json:"pinned"`
	Output                json.RawMessage `json:"output,omitempty"`
	OutputChecksum        *string         `json:"output_checksum,omitempty"`
	Validation            json.RawMessage `json:"validation"`
	ErrorCode             *string         `json:"error_code,omitempty"`
	Attempts              int             `json:"attempts"`
	MaxAttempts           int             `json:"max_attempts"`
	DeadlineAt            time.Time       `json:"deadline_at"`
	CancelRequested       bool            `json:"cancel_requested"`
	TokensIn              int             `json:"tokens_in"`
	TokensOut             int             `json:"tokens_out"`
	ToolCalls             int             `json:"tool_calls"`
	Usage                 []RunUsage      `json:"usage"`
	UsageCoverage         string          `json:"usage_coverage"`
	EvalRunID             *string         `json:"eval_run_id,omitempty"`
	EvalCaseID            *string         `json:"eval_case_id,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	StartedAt             *time.Time      `json:"started_at,omitempty"`
	CompletedAt           *time.Time      `json:"completed_at,omitempty"`
}

type RunCreate struct {
	AgentKey              string
	AgentVersionID        string // sandbox and evaluation runs only
	Kind                  string // production, sandbox, evaluation
	CallerKind            string
	CallerSubject         string
	IdempotencyKey        string
	ExternalCorrelationID string
	CaseRef               string
	DelegatedUser         string // production service runs only: lowercase Control Plane user UUID for KMS delegation
	PinVersion            string // optional semver or id for production runs
	Input                 json.RawMessage
	Evidence              json.RawMessage // flowcase refs or sandbox/evaluation fixtures
	Deadline              time.Duration
	ReleaseEnvironment    string
	AllowDevMock          bool
	EvalRunID             string
	EvalCaseID            string
}

type RunEvent struct {
	Seq       int             `json:"seq"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type EvidenceRef struct {
	Origin            string `json:"origin"`
	SourceID          string `json:"source_id"`
	SourceVersion     string `json:"source_version"`
	Title             string `json:"title"`
	Citation          string `json:"citation"`
	Anchor            string `json:"anchor"`
	TrustStatus       string `json:"trust_status"`
	PublicationStatus string `json:"publication_status"`
	Cited             bool   `json:"cited"`
}

type RunResult struct {
	Status         string
	Output         json.RawMessage
	OutputChecksum string
	Validation     json.RawMessage
	ErrorCode      string
	ProviderError  string // redacted provider failure code, recorded on the run.failed event only
	TokensIn       int
	TokensOut      int
	ToolCalls      int
	CitedSources   []string
}

// RunExecution is everything the worker needs, resolved from pinned ids.
type RunExecution struct {
	Run     Run
	Agent   Agent
	Version AgentVersion
	Skills  []SkillVersion
	Tools   []BoundToolVersion
	Model   *ModelTarget
}

type BoundToolVersion struct {
	ToolVersion
	Scope json.RawMessage `json:"scope"`
}

type ModelTarget struct {
	ProviderID      string          `json:"provider_id"`
	AdapterType     string          `json:"adapter_type"`
	BaseURL         string          `json:"base_url"`
	SecretRef       string          `json:"-"`
	DeploymentID    string          `json:"deployment_id"`
	ProviderModelID string          `json:"provider_model_id"`
	Capabilities    json.RawMessage `json:"capabilities"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	TimeoutMillis   int             `json:"timeout_ms"`
	ConfigVersion   int             `json:"config_version"`
	Environment     string          `json:"environment"`
}

const runColumns = `r.id::text, r.agent_id::text, a.agent_key, r.agent_version_id::text, v.semver, r.run_kind, r.caller_kind,
	r.caller_subject, r.idempotency_key, r.external_correlation_id, r.case_ref, r.input_json, r.evidence_input_json,
	r.status, r.model_mode, r.pinned_json, r.output_json, r.output_checksum, r.validation_json, r.error_code,
	r.attempts, r.max_attempts, r.deadline_at, r.cancel_requested,
	COALESCE((SELECT sum(u.tokens_in) FROM agent_run_usage u WHERE u.tenant_id=r.tenant_id AND u.run_id=r.id),r.tokens_in),
	COALESCE((SELECT sum(u.tokens_out) FROM agent_run_usage u WHERE u.tenant_id=r.tenant_id AND u.run_id=r.id),r.tokens_out),
	COALESCE((SELECT sum(u.tool_calls) FROM agent_run_usage u WHERE u.tenant_id=r.tenant_id AND u.run_id=r.id),r.tool_calls),
	r.eval_run_id::text, r.eval_case_id::text, r.created_at, r.started_at, r.completed_at, r.delegated_user,
	COALESCE((SELECT jsonb_agg(jsonb_build_object('attempt',u.attempt,'deployment_id',COALESCE(u.deployment_id::text,''),'config_version',u.config_version,'tokens_in',u.tokens_in,'tokens_out',u.tokens_out,'tool_calls',u.tool_calls,'usage_reports',u.usage_reports,'missing_usage',u.missing_usage,'recorded_at',u.recorded_at) ORDER BY u.attempt) FROM agent_run_usage u WHERE u.tenant_id=r.tenant_id AND u.run_id=r.id),'[]'::jsonb)`

const runFrom = ` FROM agent_runs r JOIN agents a ON a.tenant_id=r.tenant_id AND a.id=r.agent_id
	JOIN agent_versions v ON v.tenant_id=r.tenant_id AND v.id=r.agent_version_id`

type rowScanner interface{ Scan(...any) error }

func scanRun(row rowScanner) (Run, error) {
	var item Run
	var output, usage []byte
	err := row.Scan(&item.ID, &item.AgentID, &item.AgentKey, &item.AgentVersionID, &item.AgentSemver, &item.Kind, &item.CallerKind,
		&item.CallerSubject, &item.IdempotencyKey, &item.ExternalCorrelationID, &item.CaseRef, &item.Input, &item.EvidenceInput,
		&item.Status, &item.ModelMode, &item.Pinned, &output, &item.OutputChecksum, &item.Validation, &item.ErrorCode,
		&item.Attempts, &item.MaxAttempts, &item.DeadlineAt, &item.CancelRequested, &item.TokensIn, &item.TokensOut, &item.ToolCalls,
		&item.EvalRunID, &item.EvalCaseID, &item.CreatedAt, &item.StartedAt, &item.CompletedAt, &item.DelegatedUser, &usage)
	if err == nil {
		err = json.Unmarshal(usage, &item.Usage)
		item.UsageCoverage = runUsageCoverage(item)
	}
	if output != nil {
		item.Output = output
	}
	return item, err
}

// CreateRun pins the agent, skill, tool and model configuration and queues the
// run. A repeated idempotency key from the same caller returns the original
// run; a different request under that key is rejected.
func (r *ModelRegistry) CreateRun(ctx context.Context, tenantID string, input RunCreate) (Run, bool, error) {
	var item Run
	created := false
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var err error
		item, created, err = createRunTx(ctx, tx, tenantID, input)
		return err
	})
	if err != nil {
		return Run{}, false, fmt.Errorf("create run: %w", err)
	}
	return item, created, nil
}

func createRunTx(ctx context.Context, tx *sql.Tx, tenantID string, input RunCreate) (Run, bool, error) {
	var item Run
	created := false
	err := func() error {
		var agent Agent
		if err := tx.QueryRowContext(ctx, `SELECT id::text, agent_key, display_name, domain, owner_subject, lifecycle_status, created_at
			FROM agents WHERE tenant_id=$1::uuid AND agent_key=$2`, tenantID, input.AgentKey).
			Scan(&agent.ID, &agent.Key, &agent.DisplayName, &agent.Domain, &agent.OwnerSubject, &agent.LifecycleStatus, &agent.CreatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrAgentNotFound
			}
			return err
		}
		if agent.LifecycleStatus == "archived" {
			return ErrAgentNotFound
		}

		versionID := input.AgentVersionID
		modelEnvironment := "development"
		switch input.Kind {
		case "production":
			if err := tx.QueryRowContext(ctx, `SELECT agent_version_id::text FROM releases
				WHERE tenant_id=$1::uuid AND agent_id=$2::uuid AND environment=$3 AND status='active'`,
				tenantID, agent.ID, input.ReleaseEnvironment).Scan(&versionID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrNoActiveRelease
				}
				return err
			}
			modelEnvironment = input.ReleaseEnvironment
		case "sandbox", "evaluation":
			var owner string
			if err := tx.QueryRowContext(ctx, `SELECT agent_id::text FROM agent_versions WHERE tenant_id=$1::uuid AND id=$2::uuid`,
				tenantID, versionID).Scan(&owner); err != nil || owner != agent.ID {
				return ErrAgentNotFound
			}
		default:
			return fmt.Errorf("unsupported run kind %q", input.Kind)
		}

		version, err := loadAgentVersion(ctx, tx, tenantID, versionID)
		if err != nil {
			return err
		}
		if input.PinVersion != "" && input.PinVersion != version.Semver && input.PinVersion != version.ID {
			return ErrVersionMismatch
		}

		pinned, model, err := pinConfiguration(ctx, tx, tenantID, version, modelEnvironment)
		if err != nil {
			return err
		}
		mode := "provider"
		if model == nil {
			if input.Kind == "production" || !input.AllowDevMock {
				return ErrModelNotApproved
			}
			mode = "dev_mock"
		}
		pinned["model"] = map[string]any{"mode": mode, "target": model}
		pinnedJSON, err := json.Marshal(pinned)
		if err != nil {
			return err
		}

		if input.DelegatedUser != "" && (input.Kind != "production" || input.CallerKind != "service") {
			return errors.New("delegated user is only allowed on production service runs")
		}
		hashInput := []string{input.Kind, version.ID, canonicalOrRaw(input.Input), canonicalOrRaw(input.Evidence), input.CaseRef, input.ExternalCorrelationID}
		if input.DelegatedUser != "" {
			// Appended only when present so request hashes of runs created
			// before delegation existed stay valid for idempotent replays.
			hashInput = append(hashInput, "delegated_user="+input.DelegatedUser)
		}
		requestHash := hashParts(hashInput...)
		deadline := input.Deadline
		if deadline <= 0 || deadline > 10*time.Minute {
			deadline = 2 * time.Minute
		}

		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_runs WHERE tenant_id=$1::uuid AND status IN ('queued','running')`, tenantID).Scan(&active); err != nil {
			return err
		}

		var runID string
		err = tx.QueryRowContext(ctx, `INSERT INTO agent_runs (tenant_id, agent_id, agent_version_id, run_kind, caller_kind, caller_subject,
				idempotency_key, request_hash, external_correlation_id, case_ref, input_json, evidence_input_json, model_mode, pinned_json,
				deadline_at, eval_run_id, eval_case_id, delegated_user)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,nullif($9,''),nullif($10,''),$11::jsonb,$12::jsonb,$13,$14::jsonb,
				now() + make_interval(secs => $15), nullif($16,'')::uuid, nullif($17,'')::uuid, nullif($18,''))
			ON CONFLICT (tenant_id, caller_subject, idempotency_key) DO NOTHING
			RETURNING id::text`,
			tenantID, agent.ID, version.ID, input.Kind, input.CallerKind, input.CallerSubject, input.IdempotencyKey, requestHash,
			input.ExternalCorrelationID, input.CaseRef, string(input.Input), string(input.Evidence), mode, string(pinnedJSON),
			deadline.Seconds(), input.EvalRunID, input.EvalCaseID, input.DelegatedUser).Scan(&runID)
		if errors.Is(err, sql.ErrNoRows) {
			var existingHash string
			if err := tx.QueryRowContext(ctx, `SELECT id::text, request_hash FROM agent_runs
				WHERE tenant_id=$1::uuid AND caller_subject=$2 AND idempotency_key=$3`,
				tenantID, input.CallerSubject, input.IdempotencyKey).Scan(&runID, &existingHash); err != nil {
				return err
			}
			if existingHash != requestHash {
				return ErrIdempotencyConflict
			}
			item, err = getRun(ctx, tx, tenantID, runID)
			return err
		}
		if err != nil {
			return err
		}
		if active >= maxActiveRunsPerTenant {
			return ErrBackpressure
		}
		created = true
		if input.Kind == "production" {
			if err := recordInputEvidence(ctx, tx, tenantID, runID, input.Evidence); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO work_queue (kind, tenant_id, ref_id) VALUES ('run', $1::uuid, $2::uuid)`, tenantID, runID); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, tenantID, runID, "run.queued", map[string]any{"kind": input.Kind, "model_mode": mode, "agent_version": version.Semver}); err != nil {
			return err
		}
		if err := audit(tx, tenantID, input.CallerSubject, "run.created", "run:"+runID); err != nil {
			return err
		}
		item, err = getRun(ctx, tx, tenantID, runID)
		return err
	}()
	return item, created, err
}

func canonicalOrRaw(raw json.RawMessage) string {
	if canonical, err := CanonicalJSON(raw); err == nil {
		return string(canonical)
	}
	return string(raw)
}

// CanonicalJSON re-encodes JSON with sorted object keys and preserved numbers.
// It matches Flowcase's agent OutputChecksum canonicalization.
func CanonicalJSON(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return []byte("null"), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func OutputChecksum(raw json.RawMessage) (string, error) {
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func loadAgentVersion(ctx context.Context, tx *sql.Tx, tenantID, versionID string) (AgentVersion, error) {
	var item AgentVersion
	err := tx.QueryRowContext(ctx, `SELECT id::text, agent_id::text, semver, manifest_json, input_schema_json, output_schema_json,
		model_policy_json, tool_policy_json, checksum, state, created_by, created_at, published_by, published_at
		FROM agent_versions WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, versionID).
		Scan(&item.ID, &item.AgentID, &item.Semver, &item.Manifest, &item.InputSchema, &item.OutputSchema, &item.ModelPolicy,
			&item.ToolPolicy, &item.Checksum, &item.State, &item.CreatedBy, &item.CreatedAt, &item.PublishedBy, &item.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrAgentNotFound
	}
	return item, err
}

func loadBoundSkills(ctx context.Context, tx *sql.Tx, tenantID, versionID string) ([]SkillVersion, error) {
	rows, err := tx.QueryContext(ctx, `SELECT s.id::text, s.skill_id::text, s.semver, s.instructions, s.procedures_json, s.examples_json,
		s.forbidden_actions_json, s.output_schema_json, s.checksum, s.state, s.created_by, s.created_at
		FROM agent_skill_bindings b JOIN skill_versions s ON s.tenant_id=b.tenant_id AND s.id=b.skill_version_id
		WHERE b.tenant_id=$1::uuid AND b.agent_version_id=$2::uuid ORDER BY s.id`, tenantID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	skills := make([]SkillVersion, 0)
	for rows.Next() {
		var s SkillVersion
		if err := rows.Scan(&s.ID, &s.SkillID, &s.Semver, &s.Instructions, &s.Procedures, &s.Examples, &s.ForbiddenActions,
			&s.OutputSchema, &s.Checksum, &s.State, &s.CreatedBy, &s.CreatedAt); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, rows.Err()
}

func loadBoundTools(ctx context.Context, tx *sql.Tx, tenantID, versionID string) ([]BoundToolVersion, error) {
	rows, err := tx.QueryContext(ctx, `SELECT tv.id::text, t.id::text, t.tool_key, t.kind, t.owner, t.risk_level, t.lifecycle_status,
		tv.semver, tv.adapter_ref, tv.input_schema_json, tv.output_schema_json, tv.policy_json, tv.checksum, tv.published_at, b.action_scope_json
		FROM agent_tool_bindings b JOIN tool_versions tv ON tv.id=b.tool_version_id JOIN tools t ON t.id=tv.tool_id
		WHERE b.tenant_id=$1::uuid AND b.agent_version_id=$2::uuid ORDER BY t.tool_key`, tenantID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tools := make([]BoundToolVersion, 0)
	for rows.Next() {
		var t BoundToolVersion
		if err := rows.Scan(&t.ID, &t.ToolID, &t.Key, &t.Kind, &t.Owner, &t.RiskLevel, &t.Lifecycle, &t.Semver, &t.AdapterRef,
			&t.InputSchema, &t.OutputSchema, &t.Policy, &t.Checksum, &t.PublishedAt, &t.Scope); err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, rows.Err()
}

// resolveModel returns the approved provider target for a version, or nil when
// no binding is approved for the environment.
func resolveModel(ctx context.Context, tx *sql.Tx, tenantID, versionID, environment string) (*ModelTarget, error) {
	var deploymentID string
	err := tx.QueryRowContext(ctx, `SELECT model_deployment_id::text FROM agent_model_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid`,
		tenantID, versionID).Scan(&deploymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := requireApprovedDeployment(tx, tenantID, deploymentID, environment); err != nil {
		if errors.Is(err, ErrModelNotApproved) {
			return nil, nil
		}
		return nil, err
	}
	target := &ModelTarget{Environment: environment}
	if err := tx.QueryRowContext(ctx, `SELECT p.id::text, p.adapter_type, p.base_url, p.secret_ref, d.id::text, d.provider_model_id,
		d.capabilities_json, d.max_output_tokens, d.timeout_ms, d.config_version
		FROM llm_model_deployments d JOIN llm_providers p ON p.tenant_id=d.tenant_id AND p.id=d.provider_id
		WHERE d.tenant_id=$1::uuid AND d.id=$2::uuid`, tenantID, deploymentID).
		Scan(&target.ProviderID, &target.AdapterType, &target.BaseURL, &target.SecretRef, &target.DeploymentID, &target.ProviderModelID,
			&target.Capabilities, &target.MaxOutputTokens, &target.TimeoutMillis, &target.ConfigVersion); err != nil {
		return nil, err
	}
	return target, nil
}

// VersionFingerprint hashes everything that changes agent behaviour: the
// version definition, bound skill content, tool bindings and model binding.
// Evaluations and release requests are only valid for the fingerprint they ran on.
func versionFingerprint(ctx context.Context, tx *sql.Tx, tenantID string, version AgentVersion) (string, []SkillVersion, []BoundToolVersion, error) {
	skills, err := loadBoundSkills(ctx, tx, tenantID, version.ID)
	if err != nil {
		return "", nil, nil, err
	}
	tools, err := loadBoundTools(ctx, tx, tenantID, version.ID)
	if err != nil {
		return "", nil, nil, err
	}
	parts := []string{version.ID, version.Semver, string(version.Manifest), string(version.InputSchema), string(version.OutputSchema),
		string(version.ModelPolicy), string(version.ToolPolicy)}
	for _, s := range skills {
		parts = append(parts, "skill", s.ID, s.Semver, s.Instructions, string(s.Procedures), string(s.Examples), string(s.ForbiddenActions), string(s.OutputSchema))
	}
	for _, t := range tools {
		parts = append(parts, "tool", t.ID, string(t.Scope))
	}
	var deployment, fallback, override sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT model_deployment_id::text, fallback_deployment_id::text, override_policy_json::text
		FROM agent_model_bindings WHERE tenant_id=$1::uuid AND agent_version_id=$2::uuid`, tenantID, version.ID).Scan(&deployment, &fallback, &override)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil, err
	}
	parts = append(parts, "model", deployment.String, fallback.String, override.String)
	return hashParts(parts...), skills, tools, nil
}

func pinConfiguration(ctx context.Context, tx *sql.Tx, tenantID string, version AgentVersion, environment string) (map[string]any, *ModelTarget, error) {
	fingerprint, skills, tools, err := versionFingerprint(ctx, tx, tenantID, version)
	if err != nil {
		return nil, nil, err
	}
	model, err := resolveModel(ctx, tx, tenantID, version.ID, environment)
	if err != nil {
		return nil, nil, err
	}
	skillPins := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		skillPins = append(skillPins, map[string]any{"id": s.ID, "skill_id": s.SkillID, "semver": s.Semver, "state": s.State, "checksum": s.Checksum})
	}
	toolPins := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		toolPins = append(toolPins, map[string]any{"id": t.ID, "key": t.Key, "semver": t.Semver, "checksum": t.Checksum})
	}
	return map[string]any{
		"agent_version": map[string]any{"id": version.ID, "semver": version.Semver, "state": version.State, "checksum": version.Checksum},
		"fingerprint":   fingerprint,
		"skills":        skillPins,
		"tools":         toolPins,
	}, model, nil
}

func recordInputEvidence(ctx context.Context, tx *sql.Tx, tenantID, runID string, raw json.RawMessage) error {
	var refs []struct {
		SourceID       string `json:"source_id"`
		VersionID      string `json:"version_id"`
		Title          string `json:"title"`
		Classification string `json:"classification"`
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &refs); err != nil {
		return err
	}
	for _, ref := range refs {
		if ref.SourceID == "" {
			continue
		}
		// Flowcase supplies references, not verified content: they stay unverified.
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_evidence_refs (tenant_id, run_id, origin, source_id, source_version, title, trust_status)
			VALUES ($1::uuid,$2::uuid,'flowcase',$3,$4,$5,'unverified') ON CONFLICT DO NOTHING`,
			tenantID, runID, truncate(ref.SourceID, 200), truncate(ref.VersionID, 200), truncate(ref.Title, 500)); err != nil {
			return err
		}
	}
	return nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func getRun(ctx context.Context, tx *sql.Tx, tenantID, runID string) (Run, error) {
	item, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+runFrom+` WHERE r.tenant_id=$1::uuid AND r.id=$2::uuid`, tenantID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrRunNotFound
	}
	return item, err
}

func appendEvent(ctx context.Context, tx *sql.Tx, tenantID, runID, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_run_events (tenant_id, run_id, seq, event_type, payload_json)
		SELECT $1::uuid, $2::uuid, coalesce(max(seq), 0) + 1, $3, $4::jsonb FROM agent_run_events WHERE run_id=$2::uuid`,
		tenantID, runID, eventType, string(encoded))
	return err
}

func (r *ModelRegistry) GetRun(ctx context.Context, tenantID, runID string) (Run, error) {
	var item Run
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var err error
		item, err = getRun(ctx, tx, tenantID, runID)
		return err
	})
	return item, err
}

type RunFilter struct {
	AgentKey, Kind, Status string
	Limit, Offset          int
}

func (r *ModelRegistry) ListRuns(ctx context.Context, tenantID string, filter RunFilter) ([]Run, int, error) {
	limit, offset := registryPage(filter.Limit, filter.Offset)
	items := make([]Run, 0)
	total := 0
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		where := ` WHERE r.tenant_id=$1::uuid AND ($2='' OR a.agent_key=$2) AND ($3='' OR r.run_kind=$3) AND ($4='' OR r.status=$4)`
		args := []any{tenantID, filter.AgentKey, filter.Kind, filter.Status}
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+runFrom+where, args...).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+runColumns+runFrom+where+` ORDER BY r.created_at DESC, r.id LIMIT $5 OFFSET $6`, append(args, limit, offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanRun(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, total, err
}

func (r *ModelRegistry) ListRunEvents(ctx context.Context, tenantID, runID string) ([]RunEvent, error) {
	events := make([]RunEvent, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT seq, event_type, payload_json, created_at FROM agent_run_events
			WHERE tenant_id=$1::uuid AND run_id=$2::uuid ORDER BY seq LIMIT 500`, tenantID, runID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e RunEvent
			if err := rows.Scan(&e.Seq, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
				return err
			}
			events = append(events, e)
		}
		return rows.Err()
	})
	return events, err
}

func (r *ModelRegistry) ListRunEvidence(ctx context.Context, tenantID, runID string) ([]EvidenceRef, error) {
	var refs []EvidenceRef
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var err error
		refs, err = listRunEvidence(ctx, tx, tenantID, runID)
		return err
	})
	return refs, err
}

func listRunEvidence(ctx context.Context, tx *sql.Tx, tenantID, runID string) ([]EvidenceRef, error) {
	refs := make([]EvidenceRef, 0)
	rows, err := tx.QueryContext(ctx, `SELECT origin, source_id, source_version, title, citation, anchor, trust_status, publication_status, cited
		FROM agent_evidence_refs WHERE tenant_id=$1::uuid AND run_id=$2::uuid ORDER BY created_at, source_id`, tenantID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e EvidenceRef
		if err := rows.Scan(&e.Origin, &e.SourceID, &e.SourceVersion, &e.Title, &e.Citation, &e.Anchor, &e.TrustStatus, &e.PublicationStatus, &e.Cited); err != nil {
			return nil, err
		}
		refs = append(refs, e)
	}
	return refs, rows.Err()
}

// BeginRunExecution moves a claimed run to running and loads its pinned
// configuration. It returns ErrInvalidState for runs that are already settled
// and fails the run when its draft configuration changed after it was queued.
func (r *ModelRegistry) BeginRunExecution(ctx context.Context, tenantID, runID string) (RunExecution, error) {
	var exec RunExecution
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var status string
		var cancel bool
		if err := tx.QueryRowContext(ctx, `SELECT status, cancel_requested FROM agent_runs WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`,
			tenantID, runID).Scan(&status, &cancel); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRunNotFound
			}
			return err
		}
		if status != "queued" && status != "running" {
			return ErrInvalidState
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status='running', attempts=attempts+1, started_at=coalesce(started_at, now())
			WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, runID); err != nil {
			return err
		}
		run, err := getRun(ctx, tx, tenantID, runID)
		if err != nil {
			return err
		}
		exec.Run = run
		if err := tx.QueryRowContext(ctx, `SELECT id::text, agent_key, display_name, domain, owner_subject, lifecycle_status, created_at
			FROM agents WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, run.AgentID).
			Scan(&exec.Agent.ID, &exec.Agent.Key, &exec.Agent.DisplayName, &exec.Agent.Domain, &exec.Agent.OwnerSubject, &exec.Agent.LifecycleStatus, &exec.Agent.CreatedAt); err != nil {
			return err
		}
		version, err := loadAgentVersion(ctx, tx, tenantID, run.AgentVersionID)
		if err != nil {
			return err
		}
		exec.Version = version
		fingerprint, skills, tools, err := versionFingerprint(ctx, tx, tenantID, version)
		if err != nil {
			return err
		}
		exec.Skills, exec.Tools = skills, tools
		var pinned struct {
			Fingerprint string `json:"fingerprint"`
			Model       struct {
				Mode   string       `json:"mode"`
				Target *ModelTarget `json:"target"`
			} `json:"model"`
		}
		if err := json.Unmarshal(run.Pinned, &pinned); err != nil {
			return err
		}
		if pinned.Fingerprint != fingerprint {
			exec.Run.ErrorCode = ptr("pinned_config_changed")
			return nil
		}
		if pinned.Model.Mode == "provider" {
			environment := "development"
			if pinned.Model.Target != nil {
				environment = pinned.Model.Target.Environment
			}
			model, err := resolveModel(ctx, tx, tenantID, version.ID, environment)
			if err != nil {
				return err
			}
			if model == nil || pinned.Model.Target == nil || model.DeploymentID != pinned.Model.Target.DeploymentID || model.ConfigVersion != pinned.Model.Target.ConfigVersion {
				exec.Run.ErrorCode = ptr("model_approval_revoked")
				return nil
			}
			exec.Model = model
		}
		return appendEvent(ctx, tx, tenantID, runID, "run.started", map[string]any{"attempt": run.Attempts, "model_mode": run.ModelMode})
	})
	return exec, err
}

func ptr[T any](v T) *T { return &v }

func (r *ModelRegistry) AppendRunEvents(ctx context.Context, tenantID, runID string, events []RunEvent) error {
	if len(events) == 0 {
		return nil
	}
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT 1 FROM agent_runs WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, runID); err != nil {
			return err
		}
		for _, e := range events {
			if err := appendEvent(ctx, tx, tenantID, runID, e.Type, e.Payload); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *ModelRegistry) RecordEvidence(ctx context.Context, tenantID, runID string, refs []EvidenceRef) error {
	if len(refs) == 0 {
		return nil
	}
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		for _, ref := range refs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO agent_evidence_refs (tenant_id, run_id, origin, source_id, source_version, title, citation, anchor, trust_status, publication_status)
				VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
				tenantID, runID, ref.Origin, truncate(ref.SourceID, 200), truncate(ref.SourceVersion, 200), truncate(ref.Title, 500),
				truncate(ref.Citation, 500), truncate(ref.Anchor, 200), ref.TrustStatus, truncate(ref.PublicationStatus, 32)); err != nil {
				return err
			}
		}
		return nil
	})
}

// FinishRun settles a running run exactly once. Production runs from a service
// caller also enqueue their Flowcase result in the same transaction.
func (r *ModelRegistry) FinishRun(ctx context.Context, tenantID, runID string, result RunResult) (Run, error) {
	var item Run
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var output any
		if len(result.Output) > 0 {
			output = string(result.Output)
		}
		validation := result.Validation
		if len(validation) == 0 {
			validation = json.RawMessage(`{}`)
		}
		res, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status=$3, output_json=$4::jsonb, output_checksum=nullif($5,''),
			validation_json=$6::jsonb, error_code=nullif($7,''), tokens_in=$8, tokens_out=$9, tool_calls=$10, completed_at=now()
			WHERE tenant_id=$1::uuid AND id=$2::uuid AND status IN ('queued','running')`,
			tenantID, runID, result.Status, output, result.OutputChecksum, string(validation), result.ErrorCode,
			result.TokensIn, result.TokensOut, result.ToolCalls)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrInvalidState
		}
		if len(result.CitedSources) > 0 {
			sorted := append([]string(nil), result.CitedSources...)
			sort.Strings(sorted)
			if _, err := tx.ExecContext(ctx, `UPDATE agent_evidence_refs SET cited=true WHERE tenant_id=$1::uuid AND run_id=$2::uuid AND source_id = ANY($3)`,
				tenantID, runID, sorted); err != nil {
				return err
			}
		}
		payload := map[string]any{"error_code": result.ErrorCode, "tool_calls": result.ToolCalls}
		if result.ProviderError != "" {
			payload["provider_error"] = result.ProviderError
		}
		if err := appendEvent(ctx, tx, tenantID, runID, "run."+result.Status, payload); err != nil {
			return err
		}
		item, err = getRun(ctx, tx, tenantID, runID)
		if err != nil {
			return err
		}
		return enqueueFlowcaseResult(ctx, tx, tenantID, item)
	})
	return item, err
}

// RequeueRun returns a running run to the queue for a bounded retry.
func (r *ModelRegistry) RequeueRun(ctx context.Context, tenantID, runID, errorCode, providerError string, delay time.Duration) error {
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status='queued', error_code=$3 WHERE tenant_id=$1::uuid AND id=$2::uuid AND status='running'`,
			tenantID, runID, errorCode)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrInvalidState
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO work_queue (kind, tenant_id, ref_id, available_at) VALUES ('run',$1::uuid,$2::uuid, now() + make_interval(secs => $3))
			ON CONFLICT (kind, ref_id) DO UPDATE SET available_at=EXCLUDED.available_at, lease_owner=NULL, lease_expires_at=NULL`,
			tenantID, runID, delay.Seconds()); err != nil {
			return err
		}
		payload := map[string]any{"error_code": errorCode, "delay_seconds": int(delay.Seconds())}
		if providerError != "" {
			payload["provider_error"] = providerError
		}
		return appendEvent(ctx, tx, tenantID, runID, "run.retry_scheduled", payload)
	})
}

// RequestCancel cancels a queued run immediately and flags a running one; the
// worker observes the flag through its lease heartbeat.
func (r *ModelRegistry) RequestCancel(ctx context.Context, tenantID, runID, actor string) (Run, error) {
	var item Run
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE tenant_id=$1::uuid AND id=$2::uuid FOR UPDATE`, tenantID, runID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRunNotFound
			}
			return err
		}
		switch status {
		case "queued":
			if _, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status='cancelled', cancel_requested=true, error_code='cancelled', completed_at=now()
				WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, runID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM work_queue WHERE kind='run' AND ref_id=$1::uuid`, runID); err != nil {
				return err
			}
		case "running":
			if _, err := tx.ExecContext(ctx, `UPDATE agent_runs SET cancel_requested=true WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, runID); err != nil {
				return err
			}
		default:
			return ErrInvalidState
		}
		if err := appendEvent(ctx, tx, tenantID, runID, "run.cancel_requested", map[string]any{"by": actor}); err != nil {
			return err
		}
		if err := audit(tx, tenantID, actor, "run.cancel_requested", "run:"+runID); err != nil {
			return err
		}
		var err error
		item, err = getRun(ctx, tx, tenantID, runID)
		if err != nil {
			return err
		}
		if item.Status == "cancelled" {
			return enqueueFlowcaseResult(ctx, tx, tenantID, item)
		}
		return nil
	})
	return item, err
}

func (r *ModelRegistry) CancelRequested(ctx context.Context, tenantID, runID string) (bool, error) {
	var cancel bool
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT cancel_requested FROM agent_runs WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, runID).Scan(&cancel)
	})
	return cancel, err
}
