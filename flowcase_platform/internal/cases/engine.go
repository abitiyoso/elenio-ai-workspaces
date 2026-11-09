package cases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/audit"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/connector"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/events"
	"github.com/elenio/flowcase/internal/platform/idempotency"
	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/compiler"
	"github.com/elenio/flowcase/internal/workflows/definition"
)

// NotifyChannel wakes workers when executions become ready (sent on commit).
const NotifyChannel = "flowcase_work"

// ExecInfo is what a FaultInjector sees; tests use it to simulate handler failures.
type ExecInfo struct {
	CaseID, NodeID string
	NodeType       definition.NodeType
	Attempt        int
}

type Engine struct {
	DB            *pgxpool.Pool
	Catalog       catalog.Catalog
	Logger        *slog.Logger
	Agents        agent.AgentExecutor
	Knowledge     kms.KnowledgeProvider
	Connectors    connector.Registry
	LeaseDuration time.Duration
	MaxAttempts   int
	// AgentPoll is the delay between status polls of a running agent; AgentTimeout bounds a run.
	AgentPoll    time.Duration
	AgentTimeout time.Duration
	// FaultInjector, when set (tests only), runs before each handler; an error is treated as a handler failure.
	FaultInjector func(ExecInfo) error
	// FailAfterSideEffect, when set (tests only), runs after a connector call and before the result is
	// committed, simulating a crash between the side effect and the database write.
	FailAfterSideEffect func(ExecInfo) error

	graphs sync.Map // workflow_version_id -> *pinned (versions are immutable, so caching is safe)
}

type pinned struct {
	def   *definition.Definition
	graph *compiler.Graph
	hash  string
}

func (e *Engine) lease() time.Duration {
	if e.LeaseDuration > 0 {
		return e.LeaseDuration
	}
	return 30 * time.Second
}

func (e *Engine) maxAttempts() int {
	if e.MaxAttempts > 0 {
		return e.MaxAttempts
	}
	return 5
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (e *Engine) pinnedVersion(ctx context.Context, q querier, versionID string) (*pinned, error) {
	if p, ok := e.graphs.Load(versionID); ok {
		return p.(*pinned), nil
	}
	var raw []byte
	var hash *string
	if err := q.QueryRow(ctx, `SELECT definition, definition_hash FROM workflow_versions WHERE id = $1 AND status IN ('published', 'deprecated')`,
		versionID).Scan(&raw, &hash); err != nil {
		return nil, fmt.Errorf("load pinned version %s: %w", versionID, err)
	}
	d, err := decodeDefinition(raw)
	if err != nil {
		return nil, err
	}
	p := &pinned{def: d, graph: compiler.Build(d), hash: *hash}
	e.graphs.Store(versionID, p)
	return p, nil
}

func decodeDefinition(raw []byte) (*definition.Definition, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var d definition.Definition
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("decode definition: %w", err)
	}
	return &d, nil
}

func decodeVars(raw []byte) map[string]any {
	vars := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&vars)
	return vars
}

func sha(v any) string {
	b, _ := json.Marshal(v) // map keys are sorted, so this is canonical for our JSON values
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func notify(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_notify($1, '')`, NotifyChannel)
	return err
}

func systemAudit(ctx context.Context, c *caseRow, action, subjectType, subjectID string, data map[string]any) audit.Event {
	return audit.Event{TenantID: c.TenantID, ActorType: "system", Action: action, SubjectType: subjectType, SubjectID: subjectID,
		CaseID: c.ID, TraceID: audit.TraceID(ctx), Data: data}
}

func userAudit(ctx context.Context, a auth.Identity, c *caseRow, action, subjectType, subjectID string, data map[string]any) audit.Event {
	return audit.Event{TenantID: a.TenantID, ActorType: "user", ActorID: a.UserID, Action: action, SubjectType: subjectType,
		SubjectID: subjectID, CaseID: c.ID, TraceID: audit.TraceID(ctx), Data: data}
}

func outbox(ctx context.Context, tx pgx.Tx, c *caseRow, topic, aggregateType, aggregateID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["case_id"], payload["case_reference"] = c.ID, c.Reference
	return events.Enqueue(ctx, tx, events.Outbox{TenantID: c.TenantID, Topic: topic, AggregateType: aggregateType, AggregateID: aggregateID,
		DedupeKey: topic + ":" + aggregateID, Payload: payload, TraceID: audit.TraceID(ctx)})
}

// --- rows ---------------------------------------------------------------------------------------

type caseRow struct {
	ID, TenantID, Reference, Title, Status, VersionID, WorkflowKey, OwnerID, CreatedBy string
	WorkflowVersion, VersionNum                                                        int
	Variables                                                                          []byte
}

func lockCase(ctx context.Context, tx pgx.Tx, tenantID, caseID string) (*caseRow, error) {
	var c caseRow
	err := tx.QueryRow(ctx, `SELECT id::text, tenant_id::text, reference, title, status, workflow_version_id::text, workflow_key, owner_id::text,
			created_by::text, workflow_version, version_num, variables
		FROM cases WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, caseID).Scan(
		&c.ID, &c.TenantID, &c.Reference, &c.Title, &c.Status, &c.VersionID, &c.WorkflowKey, &c.OwnerID, &c.CreatedBy, &c.WorkflowVersion, &c.VersionNum, &c.Variables)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (c *caseRow) running() bool { return c.Status == CaseActive || c.Status == CaseUnderReview }

type execRow struct {
	ID, NodeID, NodeType, Status, TokenID string
	LeaseOwner                            *string
	Iteration, Attempt                    int
	Input                                 []byte
	WakeAt                                *time.Time
}

func lockExec(ctx context.Context, tx pgx.Tx, id string) (*execRow, error) {
	var x execRow
	err := tx.QueryRow(ctx, `SELECT id::text, node_id, node_type, status, token_id::text, lease_owner, iteration, attempt, input, wake_at
		FROM node_executions WHERE id = $1 FOR UPDATE`, id).Scan(&x.ID, &x.NodeID, &x.NodeType, &x.Status, &x.TokenID, &x.LeaseOwner, &x.Iteration, &x.Attempt, &x.Input, &x.WakeAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// --- case creation ------------------------------------------------------------------------------

// CreateCase pins the case to an exact published version (id, number and content hash), validates
// the input variables against the version's input_schema, and schedules the start node.
func (e *Engine) CreateCase(ctx context.Context, a auth.Identity, idem Idempotency, in CreateInput) (string, error) {
	if !a.HasRole(auth.RoleCaseOwner) {
		return "", ErrForbidden
	}
	if l := len([]rune(in.Title)); l < 1 || l > 300 {
		return "", &InputError{Code: "invalid_title", Message: "title must have 1 to 300 characters", Path: "title"}
	}
	if in.Variables == nil {
		in.Variables = map[string]any{}
	}
	var caseID string
	err := pgx.BeginFunc(ctx, e.DB, func(tx pgx.Tx) error {
		ref, replayed, err := idempotency.Claim(ctx, tx, a.TenantID, a.UserID, "case.create", idempotency.Key{Key: idem.Key, Hash: idem.Hash})
		if err != nil {
			return mapIdemErr(err)
		}
		if replayed {
			caseID = ref
			return nil
		}
		var versionID, hash, name string
		var version int
		err = tx.QueryRow(ctx, `SELECT v.id::text, v.version, v.definition_hash, d.name FROM workflow_versions v
			JOIN workflow_definitions d ON d.id = v.definition_id AND d.tenant_id = v.tenant_id
			WHERE d.tenant_id = $1 AND d.key = $2 AND v.status = 'published' AND ($3 = 0 OR v.version = $3)
			ORDER BY v.version DESC LIMIT 1`, a.TenantID, in.WorkflowKey, in.Version).Scan(&versionID, &version, &hash, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			return &InputError{Code: "workflow_not_startable", Message: "no published version of that workflow (deprecated and draft versions cannot start cases)", Path: "workflow_key"}
		}
		if err != nil {
			return err
		}
		p, err := e.pinnedVersion(ctx, tx, versionID)
		if err != nil {
			return err
		}
		var diags []definition.Diagnostic
		for i, n := range p.def.Nodes {
			if !slices.Contains(SupportedNodeTypes, n.Type) {
				diags = append(diags, definition.Errorf(fmt.Sprintf("/nodes/%d/type", i), n.ID, "unsupported_node_type",
					"%s steps are not executable by this runtime", n.Type))
			}
		}
		diags = append(diags, e.runtimeGaps(p)...)
		if p.def.InputSchema != nil {
			diags = append(diags, definition.ValidateData(p.def.InputSchema, in.Variables, "/variables")...)
		}
		if len(diags) > 0 {
			return &ValidationError{Diagnostics: diags}
		}

		var seq int
		year := time.Now().UTC().Year()
		if err := tx.QueryRow(ctx, `INSERT INTO case_counters (tenant_id, year, last) VALUES ($1, $2, 1)
			ON CONFLICT (tenant_id, year) DO UPDATE SET last = case_counters.last + 1 RETURNING last`, a.TenantID, year).Scan(&seq); err != nil {
			return err
		}
		reference := fmt.Sprintf("CASE-%d-%04d", year, seq)
		vars, _ := json.Marshal(in.Variables)
		if err := tx.QueryRow(ctx, `INSERT INTO cases (tenant_id, reference, title, workflow_version_id, workflow_key, workflow_version,
				definition_hash, status, owner_id, variables, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9, $8) RETURNING id::text`,
			a.TenantID, reference, in.Title, versionID, in.WorkflowKey, version, hash, a.UserID, vars).Scan(&caseID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO case_members (tenant_id, case_id, user_id, relation, added_by) VALUES ($1, $2, $3, 'owner', $3)`,
			a.TenantID, caseID, a.UserID); err != nil {
			return err
		}
		var tokenID string
		if err := tx.QueryRow(ctx, `INSERT INTO execution_tokens (tenant_id, case_id) VALUES ($1, $2) RETURNING id::text`, a.TenantID, caseID).Scan(&tokenID); err != nil {
			return err
		}
		start := p.graph.Start
		if _, err := tx.Exec(ctx, `INSERT INTO node_executions (tenant_id, case_id, token_id, node_id, node_type, status, idempotency_key, max_attempts)
			VALUES ($1, $2, $3, $4, 'start', 'ready', $5, $6)`, a.TenantID, caseID, tokenID, start, "start:"+caseID, e.maxAttempts()); err != nil {
			return err
		}
		c := &caseRow{ID: caseID, TenantID: a.TenantID, Reference: reference}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, c, "case.created", "case", caseID,
			map[string]any{"reference": reference, "workflow_key": in.WorkflowKey, "workflow_version": version, "definition_hash": hash})); err != nil {
			return err
		}
		if err := outbox(ctx, tx, c, "case.created", "case", caseID, map[string]any{"workflow_key": in.WorkflowKey, "workflow_version": version}); err != nil {
			return err
		}
		if err := notify(ctx, tx); err != nil {
			return err
		}
		return idempotency.Complete(ctx, tx, a.TenantID, a.UserID, "case.create", idem.Key, caseID)
	})
	return caseID, err
}

func mapIdemErr(err error) error {
	switch {
	case errors.Is(err, idempotency.ErrKeyRequired):
		return &InputError{Code: "idempotency_key_required", Message: err.Error(), Path: "Idempotency-Key"}
	case errors.Is(err, idempotency.ErrReused):
		return ErrIdempotencyReuse
	}
	return err
}

// --- worker processing --------------------------------------------------------------------------

// ProcessReady claims up to limit executions (ready ones, plus running ones whose lease expired after
// a worker crash) and runs each in its own transaction. It returns how many it claimed.
func (e *Engine) ProcessReady(ctx context.Context, workerID string, limit int) (int, error) {
	rows, err := e.DB.Query(ctx, `
		UPDATE node_executions ne SET status = 'running', lease_owner = $1, lease_expires_at = now() + make_interval(secs => $2),
			attempt = CASE WHEN ne.status = 'waiting_external' THEN ne.attempt ELSE ne.attempt + 1 END,
			started_at = coalesce(ne.started_at, now()), updated_at = now()
		WHERE ne.id IN (
			SELECT n.id FROM node_executions n JOIN cases c ON c.id = n.case_id
			WHERE c.status IN ('active', 'under_review')
			  AND ((n.status = 'ready' AND coalesce(n.next_attempt_at, n.created_at) <= now())
			    OR (n.status = 'running' AND n.lease_expires_at < now())
			    OR (n.status = 'waiting_external' AND coalesce(n.wake_at, n.next_attempt_at) <= now()))
			ORDER BY coalesce(n.next_attempt_at, n.created_at)
			FOR UPDATE OF n SKIP LOCKED
			LIMIT $3)
		RETURNING ne.id::text`, workerID, e.lease().Seconds(), limit)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := e.run(ctx, workerID, id); err != nil {
			e.Logger.WarnContext(ctx, "node execution failed", "execution_id", id, "error", err.Error())
			if ferr := e.fail(ctx, workerID, id, err); ferr != nil {
				return len(ids), ferr
			}
		}
	}
	return len(ids), nil
}

var errPermanent = errors.New("permanent failure")

func (e *Engine) run(ctx context.Context, workerID, execID string) error {
	return pgx.BeginFunc(ctx, e.DB, func(tx pgx.Tx) error {
		var tenantID, caseID string
		if err := tx.QueryRow(ctx, `SELECT tenant_id::text, case_id::text FROM node_executions WHERE id = $1`, execID).Scan(&tenantID, &caseID); err != nil {
			return err
		}
		c, err := lockCase(ctx, tx, tenantID, caseID) // lock order: case, then execution
		if err != nil {
			return err
		}
		x, err := lockExec(ctx, tx, execID)
		if err != nil {
			return err
		}
		if x.Status != "running" || x.LeaseOwner == nil || *x.LeaseOwner != workerID {
			return nil // lease lost to another worker; it owns the outcome now
		}
		if !c.running() {
			// Paused while queued: hand the execution back without consuming an attempt.
			_, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'ready', lease_owner = NULL, lease_expires_at = NULL,
				attempt = greatest(attempt - 1, 0), updated_at = now() WHERE id = $1`, x.ID)
			return err
		}
		p, err := e.pinnedVersion(ctx, tx, c.VersionID)
		if err != nil {
			return err
		}
		node := p.graph.Node(x.NodeID)
		if node == nil {
			return fmt.Errorf("%w: node %q is not in the pinned version", errPermanent, x.NodeID)
		}
		if e.FaultInjector != nil {
			if err := e.FaultInjector(ExecInfo{CaseID: c.ID, NodeID: x.NodeID, NodeType: node.Type, Attempt: x.Attempt}); err != nil {
				return err
			}
		}
		return e.handle(ctx, tx, c, x, p, node)
	})
}

func (e *Engine) handle(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, p *pinned, node *definition.Node) error {
	switch node.Type {
	case definition.Start:
		if err := completeExec(ctx, tx, x.ID, nil); err != nil {
			return err
		}
		return e.schedule(ctx, tx, c, x, node.Next)
	case definition.End:
		if err := completeExec(ctx, tx, x.ID, nil); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE execution_tokens SET status = 'completed', current_node_id = $2, updated_at = now() WHERE id = $1`, x.TokenID, x.NodeID); err != nil {
			return err
		}
		return e.finishIfDone(ctx, tx, c, node)
	case definition.Condition:
		return e.evaluate(ctx, tx, c, x, node)
	case definition.HumanTask:
		return e.openTask(ctx, tx, c, x, node)
	case definition.HumanApproval:
		return e.openApproval(ctx, tx, c, x, p, node)
	case definition.ParallelSplit:
		return e.split(ctx, tx, c, x, node)
	case definition.ParallelJoin:
		return e.join(ctx, tx, c, x, node)
	case definition.AgentTask:
		return e.agentTask(ctx, tx, c, x, p, node)
	case definition.ServiceTask:
		return e.serviceTask(ctx, tx, c, x, node)
	case definition.Timer:
		return e.timer(ctx, tx, c, x, node)
	}
	return fmt.Errorf("%w: %s steps are not executable in this runtime", errPermanent, node.Type)
}

func completeExec(ctx context.Context, tx pgx.Tx, id string, output map[string]any) error {
	var out []byte
	if output != nil {
		out, _ = json.Marshal(output)
	}
	_, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'completed', output = $2, completed_at = now(), lease_owner = NULL,
		lease_expires_at = NULL, last_error = NULL, updated_at = now(), version_num = version_num + 1 WHERE id = $1`, id, out)
	return err
}

// schedule creates the successor executions of pred. The idempotency key is derived from the
// predecessor, so re-delivering the same completion can never schedule a successor twice.
func (e *Engine) schedule(ctx context.Context, tx pgx.Tx, c *caseRow, pred *execRow, targets []string) error {
	return e.scheduleIn(ctx, tx, c, pred.TokenID, pred.ID, pred.ID, targets)
}

// scheduleIn schedules targets in token; keys are keyBase+">"+target, so the same logical transition
// (a predecessor completion, a join firing) can only ever create each successor once.
func (e *Engine) scheduleIn(ctx context.Context, tx pgx.Tx, c *caseRow, tokenID, predID, keyBase string, targets []string) error {
	p, err := e.pinnedVersion(ctx, tx, c.VersionID)
	if err != nil {
		return err
	}
	for _, t := range targets {
		n := p.graph.Node(t)
		if n == nil {
			return fmt.Errorf("%w: unknown successor %q", errPermanent, t)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO node_executions (tenant_id, case_id, token_id, node_id, node_type, iteration, status,
				idempotency_key, predecessor_id, max_attempts)
			SELECT $1, $2, $3, $4, $5, coalesce(max(iteration), 0) + 1, 'ready', $6, $7, $8
			FROM node_executions WHERE case_id = $2 AND token_id = $3 AND node_id = $4
			ON CONFLICT (idempotency_key) DO NOTHING`,
			c.TenantID, c.ID, tokenID, t, string(n.Type), keyBase+">"+t, predID, e.maxAttempts()); err != nil {
			return err
		}
	}
	if len(targets) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE execution_tokens SET current_node_id = $2, updated_at = now() WHERE id = $1`, tokenID, targets[0]); err != nil {
			return err
		}
	}
	return notify(ctx, tx)
}

func (e *Engine) evaluate(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, node *definition.Node) error {
	expr, diags := definition.ParseExpr(node.Config["expression"], "/expression", node.ID)
	if len(diags) > 0 {
		return fmt.Errorf("%w: invalid expression in pinned version", errPermanent)
	}
	vars := decodeVars(c.Variables)
	result, err := expr.Eval(vars)
	if err != nil {
		return fmt.Errorf("evaluate %s: %w", node.ID, err)
	}
	branches, _ := node.Config["branches"].(map[string]any)
	target, _ := branches[strconv.FormatBool(result)].(string)
	if target == "" {
		return fmt.Errorf("%w: no branch for %v", errPermanent, result)
	}
	inputs := map[string]any{}
	for _, f := range expr.Fields() {
		inputs[f] = lookupField(vars, f)
	}
	exprJSON, _ := json.Marshal(node.Config["expression"])
	inputsJSON, _ := json.Marshal(inputs)
	if _, err := tx.Exec(ctx, `INSERT INTO condition_evaluations (tenant_id, case_id, node_execution_id, node_id, expression, inputs, result, target)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (node_execution_id) DO NOTHING`,
		c.TenantID, c.ID, x.ID, node.ID, exprJSON, inputsJSON, result, target); err != nil {
		return err
	}
	if err := completeExec(ctx, tx, x.ID, map[string]any{"result": result, "target": target}); err != nil {
		return err
	}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "condition.evaluated", "node", node.ID,
		map[string]any{"result": result, "target": target, "inputs": inputs})); err != nil {
		return err
	}
	return e.schedule(ctx, tx, c, x, []string{target})
}

func lookupField(vars map[string]any, path string) any {
	var cur any = vars
	for _, part := range bytes.Split([]byte(path), []byte(".")) {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[string(part)]
	}
	return cur
}

func dueAt(node *definition.Node) *time.Time {
	s := node.ConfigString("due_in")
	if len(s) < 2 {
		return nil
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return nil
	}
	unit := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[s[len(s)-1]]
	t := time.Now().Add(time.Duration(n) * unit)
	return &t
}

func revisionNote(x *execRow) *string {
	var in struct {
		Note string `json:"revision_note"`
	}
	if json.Unmarshal(x.Input, &in) == nil && in.Note != "" {
		return &in.Note
	}
	return nil
}

func (e *Engine) openTask(ctx context.Context, tx pgx.Tx, c *caseRow, x *execRow, node *definition.Node) error {
	var form []byte
	if fs, ok := node.Config["form_schema"]; ok {
		form, _ = json.Marshal(fs)
	}
	var taskID string
	err := tx.QueryRow(ctx, `INSERT INTO tasks (tenant_id, case_id, node_execution_id, node_id, iteration, title, assignee_role, instructions,
			form_schema, revision_note, due_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) ON CONFLICT (node_execution_id) DO NOTHING RETURNING id::text`,
		c.TenantID, c.ID, x.ID, node.ID, x.Iteration, node.Name, node.ConfigString("assignee_role"), node.ConfigString("instructions"),
		form, revisionNote(x), dueAt(node)).Scan(&taskID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'waiting_human', lease_owner = NULL, lease_expires_at = NULL,
		updated_at = now() WHERE id = $1`, x.ID); err != nil {
		return err
	}
	if taskID == "" {
		return nil // task already existed (redelivery); nothing new to announce
	}
	data := map[string]any{"node_id": node.ID, "iteration": x.Iteration, "assignee_role": node.ConfigString("assignee_role")}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "task.assigned", "task", taskID, data)); err != nil {
		return err
	}
	return outbox(ctx, tx, c, "task.assigned", "task", taskID, data)
}

// finishIfDone completes the case once no execution is outstanding.
func (e *Engine) finishIfDone(ctx context.Context, tx pgx.Tx, c *caseRow, end *definition.Node) error {
	var open bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM node_executions WHERE case_id = $1
		AND status IN ('pending', 'ready', 'running', 'waiting_human', 'waiting_external'))`, c.ID).Scan(&open); err != nil {
		return err
	}
	if open {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE cases SET status = 'completed', closed_at = now(), updated_at = now(), version_num = version_num + 1,
		business_facets = business_facets || jsonb_build_object('outcome', 'completed', 'end_node', $2::text) WHERE id = $1`, c.ID, end.ID); err != nil {
		return err
	}
	if err := audit.Record(ctx, tx, systemAudit(ctx, c, "case.completed", "case", c.ID, map[string]any{"end_node": end.ID})); err != nil {
		return err
	}
	return outbox(ctx, tx, c, "case.completed", "case", c.ID, map[string]any{"end_node": end.ID})
}

// closeCase ends a case early (rejection or failure) and cancels outstanding work.
func closeCase(ctx context.Context, tx pgx.Tx, c *caseRow, status, outcome string) error {
	for _, q := range []string{
		`UPDATE node_executions SET status = 'cancelled', lease_owner = NULL, lease_expires_at = NULL, updated_at = now()
			WHERE case_id = $1 AND status IN ('pending', 'ready', 'running', 'waiting_human', 'waiting_external')`,
		`UPDATE tasks SET status = 'cancelled', updated_at = now(), version_num = version_num + 1 WHERE case_id = $1 AND status = 'assigned'`,
		`UPDATE approval_requests SET status = 'cancelled', updated_at = now(), version_num = version_num + 1 WHERE case_id = $1 AND status = 'pending'`,
		`UPDATE draft_reviews SET status = 'cancelled', updated_at = now(), version_num = version_num + 1 WHERE case_id = $1 AND status = 'pending'`,
		`UPDATE execution_tokens SET status = 'cancelled', updated_at = now() WHERE case_id = $1 AND status IN ('active', 'waiting')`,
	} {
		if _, err := tx.Exec(ctx, q, c.ID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE cases SET status = $2, closed_at = now(), updated_at = now(), version_num = version_num + 1,
		business_facets = business_facets || jsonb_build_object('outcome', $3::text) WHERE id = $1`, c.ID, status, outcome)
	return err
}

// rewind supersedes a waiting execution and schedules a new iteration of target with the reason,
// used for approval "return", task revision and draft revision (target = the waiting agent step). Downstream steps re-run with higher iterations.
func (e *Engine) rewind(ctx context.Context, tx pgx.Tx, c *caseRow, waiting *execRow, target, reason, actorID string) error {
	p, err := e.pinnedVersion(ctx, tx, c.VersionID)
	if err != nil {
		return err
	}
	n := p.graph.Node(target)
	if n == nil {
		return fmt.Errorf("%w: unknown rework target %q", errPermanent, target)
	}
	for _, q := range []string{
		`UPDATE node_executions SET status = 'superseded', completed_at = now(), updated_at = now(), version_num = version_num + 1 WHERE id = $1`,
		`UPDATE tasks SET status = 'cancelled', updated_at = now(), version_num = version_num + 1 WHERE node_execution_id = $1 AND status = 'assigned'`,
		`UPDATE approval_requests SET status = 'cancelled', updated_at = now(), version_num = version_num + 1 WHERE node_execution_id = $1 AND status = 'pending'`,
		`UPDATE draft_reviews SET status = 'cancelled', updated_at = now(), version_num = version_num + 1 WHERE node_execution_id = $1 AND status = 'pending'`,
	} {
		if _, err := tx.Exec(ctx, q, waiting.ID); err != nil {
			return err
		}
	}
	input, _ := json.Marshal(map[string]any{"revision_note": reason, "returned_from": waiting.NodeID, "returned_by": actorID})
	if _, err := tx.Exec(ctx, `INSERT INTO node_executions (tenant_id, case_id, token_id, node_id, node_type, iteration, status, input,
			idempotency_key, predecessor_id, max_attempts)
		SELECT $1, $2, $3, $4, $5, coalesce(max(iteration), 0) + 1, 'ready', $6, $7, $8, $9
		FROM node_executions WHERE case_id = $2 AND token_id = $3 AND node_id = $4`,
		c.TenantID, c.ID, waiting.TokenID, target, string(n.Type), input, "rework:"+waiting.ID+">"+target, waiting.ID, e.maxAttempts()); err != nil {
		return err
	}
	if err := refreshReviewStatus(ctx, tx, c.ID); err != nil {
		return err
	}
	return notify(ctx, tx)
}

// refreshReviewStatus keeps under_review exactly while an approval is pending.
func refreshReviewStatus(ctx context.Context, tx pgx.Tx, caseID string) error {
	_, err := tx.Exec(ctx, `UPDATE cases SET status = CASE WHEN EXISTS (SELECT 1 FROM approval_requests WHERE case_id = $1 AND status = 'pending')
			THEN 'under_review' ELSE 'active' END, updated_at = now(), version_num = version_num + 1
		WHERE id = $1 AND status IN ('active', 'under_review')`, caseID)
	return err
}

// fail records a failed attempt: retry with exponential backoff, or fail the node and the case
// once attempts are exhausted (permanent errors fail immediately). Never auto-approves anything.
func (e *Engine) fail(ctx context.Context, workerID, execID string, cause error) error {
	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return pgx.BeginFunc(ctx, e.DB, func(tx pgx.Tx) error {
		var tenantID, caseID string
		if err := tx.QueryRow(ctx, `SELECT tenant_id::text, case_id::text FROM node_executions WHERE id = $1`, execID).Scan(&tenantID, &caseID); err != nil {
			return err
		}
		c, err := lockCase(ctx, tx, tenantID, caseID)
		if err != nil {
			return err
		}
		x, err := lockExec(ctx, tx, execID)
		if err != nil {
			return err
		}
		if x.Status != "running" || x.LeaseOwner == nil || *x.LeaseOwner != workerID {
			return nil
		}
		var maxAttempts int
		if err := tx.QueryRow(ctx, `SELECT max_attempts FROM node_executions WHERE id = $1`, execID).Scan(&maxAttempts); err != nil {
			return err
		}
		if x.Attempt < maxAttempts && !errors.Is(cause, errPermanent) {
			backoff := min(1<<x.Attempt, 300)
			_, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'ready', lease_owner = NULL, lease_expires_at = NULL, last_error = $2,
				next_attempt_at = now() + make_interval(secs => $3), updated_at = now() WHERE id = $1`, execID, msg, backoff)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE node_executions SET status = 'failed', lease_owner = NULL, lease_expires_at = NULL, last_error = $2,
			completed_at = now(), updated_at = now() WHERE id = $1`, execID, msg); err != nil {
			return err
		}
		if err := closeCase(ctx, tx, c, CaseFailed, "failed_at_"+x.NodeID); err != nil {
			return err
		}
		data := map[string]any{"node_id": x.NodeID, "attempts": x.Attempt, "error": msg}
		if err := audit.Record(ctx, tx, systemAudit(ctx, c, "case.failed", "case", c.ID, data)); err != nil {
			return err
		}
		return outbox(ctx, tx, c, "case.failed", "case", c.ID, data)
	})
}
