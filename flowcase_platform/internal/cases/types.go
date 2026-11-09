// Package cases is the durable case runtime (WCP-03/04/05/06/09): case creation pinned to a
// published workflow version, worker-driven node execution with leases and retries, human tasks,
// policy-bound approvals with segregation of duties, rework, pause/resume, and the outbox dispatcher.
// The engine dispatches on node type only; it has no knowledge of particular workflows or packs.
package cases

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/elenio/flowcase/internal/workflows/definition"
)

// SupportedNodeTypes are executable by this runtime (subworkflow is deferred). Agent and service steps
// additionally need a configured agent runtime / connector; see Engine.runtimeGaps.
var SupportedNodeTypes = []definition.NodeType{definition.Start, definition.End, definition.HumanTask, definition.HumanApproval, definition.Condition,
	definition.ParallelSplit, definition.ParallelJoin, definition.AgentTask, definition.ServiceTask, definition.Timer}

const (
	CaseActive      = "active"
	CasePaused      = "paused"
	CaseUnderReview = "under_review"
	CaseCompleted   = "completed"
	CaseCancelled   = "cancelled"
	CaseFailed      = "failed"
)

var (
	ErrForbidden          = errors.New("forbidden")
	ErrNotFound           = errors.New("not found")
	ErrPreconditionFailed = errors.New("version precondition failed")
	ErrIdempotencyReuse   = errors.New("idempotency key was used for a different request")
)

// DeniedError is a 403 with a machine-readable reason (e.g. sod_case_owner, no_delegation).
type DeniedError struct{ Reason, Message string }

func (e *DeniedError) Error() string { return e.Message }

type ConflictError struct{ Code, Message string }

func (e *ConflictError) Error() string { return e.Message }

type InputError struct{ Code, Message, Path string }

func (e *InputError) Error() string { return e.Message }

type ValidationError struct{ Diagnostics []definition.Diagnostic }

func (e *ValidationError) Error() string { return "validation failed" }

type Idempotency struct{ Key, Hash string }

type CreateInput struct {
	WorkflowKey string         `json:"workflow_key"`
	Version     int            `json:"version"` // 0 = latest published
	Title       string         `json:"title"`
	Variables   map[string]any `json:"variables"`
}

type CaseSummary struct {
	ID               string    `json:"id"`
	Reference        string    `json:"reference"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	Outcome          *string   `json:"outcome"`
	WorkflowKey      string    `json:"workflow_key"`
	WorkflowName     string    `json:"workflow_name"`
	WorkflowVersion  int       `json:"workflow_version"`
	DefinitionHash   string    `json:"definition_hash"`
	OwnerName        string    `json:"owner_name"`
	CurrentSteps     []string  `json:"current_steps"`
	OpenTasks        int       `json:"open_tasks"`
	PendingApprovals int       `json:"pending_approvals"`
	Progress         int       `json:"progress"` // % of workflow steps completed at least once
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	VersionNum       int       `json:"version_num"`
}

type Execution struct {
	ID          string          `json:"id"`
	NodeID      string          `json:"node_id"`
	NodeName    string          `json:"node_name"`
	NodeType    string          `json:"node_type"`
	Iteration   int             `json:"iteration"`
	Status      string          `json:"status"`
	Attempt     int             `json:"attempt"`
	LastError   *string         `json:"last_error"`
	Output      json.RawMessage `json:"output"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at"`
}

type ConditionEvaluation struct {
	NodeID      string          `json:"node_id"`
	Expression  json.RawMessage `json:"expression"`
	Inputs      json.RawMessage `json:"inputs"`
	Result      bool            `json:"result"`
	Target      string          `json:"target"`
	EvaluatedAt time.Time       `json:"evaluated_at"`
}

type GraphNode struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Type string   `json:"type"`
	Next []string `json:"next"`
}

type HistoryEntry struct {
	Action     string          `json:"action"`
	ActorName  *string         `json:"actor_name"`
	ActorType  string          `json:"actor_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

type CaseActions struct {
	CanPause  bool `json:"can_pause"`
	CanResume bool `json:"can_resume"`
	CanNote   bool `json:"can_note"`
}

type CaseDetail struct {
	CaseSummary
	Variables  json.RawMessage       `json:"variables"`
	Facets     json.RawMessage       `json:"business_facets"`
	Nodes      []GraphNode           `json:"workflow_nodes"`
	Timeline   []Execution           `json:"timeline"`
	Tasks      []Task                `json:"tasks"`
	Approvals  []Approval            `json:"approvals"`
	Conditions []ConditionEvaluation `json:"condition_evaluations"`
	History    []HistoryEntry        `json:"history"`
	Actions    CaseActions           `json:"actions"`
	Artifacts  []Artifact            `json:"artifacts"`
	AgentRuns  []AgentRunView        `json:"agent_runs"`
	Drafts     []DraftReview         `json:"drafts"`
}

type CaseRef struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}

type Comment struct {
	AuthorName string    `json:"author_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type Task struct {
	ID                 string          `json:"id"`
	Case               CaseRef         `json:"case"`
	NodeID             string          `json:"node_id"`
	Iteration          int             `json:"iteration"`
	Title              string          `json:"title"`
	AssigneeRole       string          `json:"assignee_role"`
	Instructions       string          `json:"instructions"`
	FormSchema         json.RawMessage `json:"form_schema"`
	RevisionNote       *string         `json:"revision_note"`
	Status             string          `json:"status"`
	Output             json.RawMessage `json:"output"`
	CompletedByName    *string         `json:"completed_by_name"`
	DueAt              *time.Time      `json:"due_at"`
	CreatedAt          time.Time       `json:"created_at"`
	CompletedAt        *time.Time      `json:"completed_at"`
	VersionNum         int             `json:"version_num"`
	CanComplete        bool            `json:"can_complete"`
	CanRequestRevision bool            `json:"can_request_revision"`
	Comments           []Comment       `json:"comments,omitempty"`
}

type Decision struct {
	Decision      string    `json:"decision"`
	Rationale     string    `json:"rationale"`
	DecidedByName string    `json:"decided_by_name"`
	DecisionHash  string    `json:"decision_hash"`
	DecidedAt     time.Time `json:"decided_at"`
}

type CoveredArtifact struct {
	Type     string `json:"type"`
	Version  int    `json:"version"`
	Status   string `json:"status"`
	Checksum string `json:"checksum"`
}

type AgentRunView struct {
	NodeID       string          `json:"node_id"`
	Attempt      int             `json:"attempt"`
	Provider     string          `json:"provider"`
	AgentRef     string          `json:"agent_ref"`
	SkillVersion string          `json:"skill_version"`
	Status       string          `json:"status"`
	Evidence     json.RawMessage `json:"evidence"`
	Polls        int             `json:"polls"`
	CreatedAt    time.Time       `json:"created_at"`
	CompletedAt  *time.Time      `json:"completed_at"`
}

type Approval struct {
	Kind              string            `json:"kind"` // gate | rereview
	InvalidatedReason *string           `json:"invalidated_reason"`
	Covered           []CoveredArtifact `json:"covered_artifacts"`
	ID                string            `json:"id"`
	Case              CaseRef           `json:"case"`
	NodeID            string            `json:"node_id"`
	Iteration         int               `json:"iteration"`
	Title             string            `json:"title"`
	PolicyRef         string            `json:"policy_ref"`
	PolicyName        string            `json:"policy_name"`
	Provisional       bool              `json:"policy_provisional"`
	ReturnTo          *string           `json:"return_to"`
	SubjectHash       string            `json:"subject_hash"`
	Subject           json.RawMessage   `json:"subject"`
	Status            string            `json:"status"`
	Decision          *Decision         `json:"decision"`
	CreatedAt         time.Time         `json:"created_at"`
	VersionNum        int               `json:"version_num"`
	CanDecide         bool              `json:"can_decide"`
	BlockedReason     *string           `json:"blocked_reason"`
}

// DraftReview is the case owner's review of an agent draft (author_review steps). Content is the
// artifact version under review: the current one while pending, the submitted/sent-back one after.
type DraftReview struct {
	ID                string          `json:"id"`
	Case              CaseRef         `json:"case"`
	NodeID            string          `json:"node_id"`
	NodeName          string          `json:"node_name"`
	Iteration         int             `json:"iteration"`
	ArtifactType      string          `json:"artifact_type"`
	ArtifactVersion   int             `json:"artifact_version"`
	ArtifactVersionID string          `json:"artifact_version_id"`
	Content           json.RawMessage `json:"content"`
	Status            string          `json:"status"` // pending | submitted | revision_requested | cancelled
	Feedback          *string         `json:"feedback"`
	DecidedByName     *string         `json:"decided_by_name"`
	DecidedAt         *time.Time      `json:"decided_at"`
	CreatedAt         time.Time       `json:"created_at"`
	VersionNum        int             `json:"version_num"`
	CanSubmit         bool            `json:"can_submit"`
	CanRevise         bool            `json:"can_revise"`
	Revisions         int             `json:"revisions"` // revision requests this step has had in the case
}

type DecisionInput struct {
	Decision  string `json:"decision"` // approve | reject | return
	Rationale string `json:"rationale"`
}

type CompleteTaskInput struct {
	Output  map[string]any `json:"output"`
	Comment string         `json:"comment"`
}
