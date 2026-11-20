// Package agent is Flowcase's port to the shared external AI Agent Runtime. Agents propose outputs;
// they never approve, authorize prices or submit to customers. Their output only ever becomes
// artifact content for human review.
package agent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDisabled        = errors.New("agent runtime is disabled")
	ErrInvalidCallback = errors.New("agent callback failed verification")
	ErrUnknownRun      = errors.New("agent run not found")
)

type Scope struct {
	TenantID string
	UserID   string
	CaseID   string
	TraceID  string
}

// Evidence is a KMS reference passed to an agent. Snippets are untrusted data, never instructions.
type Evidence struct {
	SourceID       string `json:"source_id"`
	VersionID      string `json:"version_id"`
	Title          string `json:"title"`
	Classification string `json:"classification"`
}

type StartRunRequest struct {
	Scope          Scope           `json:"-"`
	InputContract  string          `json:"-"`               // deployment-owned catalog binding, never a workflow/browser override
	IdempotencyKey string          `json:"idempotency_key"` // node execution + attempt; the runtime must dedupe on it
	AgentID        string          `json:"agent_id"`
	SkillVersion   string          `json:"skill_version"`
	Input          json.RawMessage `json:"input"`
	Evidence       []Evidence      `json:"evidence"`
}

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

func (s RunStatus) Terminal() bool { return s == RunSucceeded || s == RunFailed || s == RunCancelled }

type Run struct {
	RunID            string            `json:"run_id"`
	Status           RunStatus         `json:"status"`
	AgentID          string            `json:"agent_id"`
	SkillVersion     string            `json:"skill_version"`
	Output           json.RawMessage   `json:"output,omitempty"`
	OutputChecksum   string            `json:"output_checksum,omitempty"`
	Error            string            `json:"error,omitempty"`
	TraceIDs         []string          `json:"trace_ids,omitempty"`
	UsageObservation *UsageObservation `json:"usage_observation,omitempty"`
}

type UsageObservation struct {
	Schema           string `json:"schema"`
	Class            string `json:"class"`
	Coverage         string `json:"coverage"`
	TokensIn         int    `json:"tokens_in"`
	TokensOut        int    `json:"tokens_out"`
	Attempts         int    `json:"attempts"`
	ObservedAttempts int    `json:"observed_attempts"`
	PricingStatus    string `json:"pricing_status"`
}

func (o *UsageObservation) Valid() bool {
	if o == nil || o.Schema != "elenio.agent-usage.v1" || o.PricingStatus != "unpriced" || o.TokensIn < 0 || o.TokensOut < 0 || o.TokensIn > 9007199254740991 || o.TokensOut > 9007199254740991 || o.Attempts < 0 || o.ObservedAttempts < 0 || o.ObservedAttempts > o.Attempts {
		return false
	}
	if o.Class != "actual" && o.Class != "test" && o.Class != "mock" {
		return false
	}
	switch o.Coverage {
	case "complete":
		return o.Attempts > 0 && o.ObservedAttempts == o.Attempts
	case "partial":
		return o.ObservedAttempts > 0
	case "unavailable":
		return o.TokensIn == 0 && o.TokensOut == 0
	case "legacy_final_attempt":
		return o.ObservedAttempts == 0
	default:
		return false
	}
}

// CallbackEnvelope is the raw inbound callback; verification needs headers and body bytes as received.
type CallbackEnvelope struct {
	Provider string
	Headers  map[string][]string
	Body     []byte
}

type VerifiedResult struct {
	MessageID string
	TenantID  string
	Run       Run
}

type Health struct {
	Mode    string `json:"mode"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type AgentExecutor interface {
	Provider() string
	StartRun(ctx context.Context, req StartRunRequest) (Run, error)
	GetRun(ctx context.Context, scope Scope, runID string) (Run, error)
	CancelRun(ctx context.Context, scope Scope, runID string) error
	VerifyResult(ctx context.Context, cb CallbackEnvelope) (VerifiedResult, error)
	Health(ctx context.Context) Health
}

// OutputChecksum is the SHA-256 of the canonical JSON output (sorted keys, numbers preserved).
func OutputChecksum(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	canon, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// --- proposed signed callback contract (PENDING runtime confirmation) ------------------------------
//
//	POST /api/v1/agent-callbacks/{provider}
//	X-Flowcase-Timestamp: <unix seconds>
//	X-Flowcase-Signature: v1=<hex HMAC-SHA256(secret, timestamp + "." + body)>
//	{"message_id": "...", "audience": "flowcase", "tenant_id": "...", "run": {Run}}

const (
	HeaderTimestamp = "X-Flowcase-Timestamp"
	HeaderSignature = "X-Flowcase-Signature"
	Audience        = "flowcase"
	MaxClockSkew    = 5 * time.Minute
)

type callbackBody struct {
	MessageID string `json:"message_id"`
	Audience  string `json:"audience"`
	TenantID  string `json:"tenant_id"`
	Run       Run    `json:"run"`
}

func sign(secret []byte, ts string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// SignCallback produces the headers a runtime (or a test) sends with a callback body.
func SignCallback(secret []byte, now time.Time, body []byte) map[string][]string {
	ts := strconv.FormatInt(now.Unix(), 10)
	return map[string][]string{HeaderTimestamp: {ts}, HeaderSignature: {sign(secret, ts, body)}}
}

func header(h map[string][]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) == 1 {
			return v[0]
		}
	}
	return ""
}

// VerifyHMACCallback checks signature (constant time), timestamp window, audience, message id and,
// for successful runs, that the output matches its checksum. Replays are stopped by inbox dedup.
func VerifyHMACCallback(secret []byte, cb CallbackEnvelope, now time.Time) (VerifiedResult, error) {
	fail := func(reason string) (VerifiedResult, error) {
		return VerifiedResult{}, fmt.Errorf("%w: %s", ErrInvalidCallback, reason)
	}
	if len(secret) < 32 {
		return fail("callback secret not configured")
	}
	ts := header(cb.Headers, HeaderTimestamp)
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fail("missing timestamp")
	}
	if d := now.Sub(time.Unix(unix, 0)); d > MaxClockSkew || d < -MaxClockSkew {
		return fail("timestamp outside window")
	}
	if !hmac.Equal([]byte(header(cb.Headers, HeaderSignature)), []byte(sign(secret, ts, cb.Body))) {
		return fail("bad signature")
	}
	var b callbackBody
	dec := json.NewDecoder(bytes.NewReader(cb.Body))
	dec.UseNumber()
	if err := dec.Decode(&b); err != nil {
		return fail("malformed body")
	}
	if b.Audience != Audience || b.MessageID == "" || len(b.MessageID) > 200 || b.TenantID == "" || b.Run.RunID == "" {
		return fail("missing audience, message, tenant or run")
	}
	if b.Run.Status == RunSucceeded {
		sum, err := OutputChecksum(b.Run.Output)
		if err != nil || sum != b.Run.OutputChecksum {
			return fail("output checksum mismatch")
		}
	}
	return VerifiedResult{MessageID: b.MessageID, TenantID: b.TenantID, Run: b.Run}, nil
}

// --- adapters -------------------------------------------------------------------------------------

type disabled struct{}

func NewDisabled() AgentExecutor { return disabled{} }

func (disabled) Provider() string                                       { return "disabled" }
func (disabled) StartRun(context.Context, StartRunRequest) (Run, error) { return Run{}, ErrDisabled }
func (disabled) GetRun(context.Context, Scope, string) (Run, error)     { return Run{}, ErrDisabled }
func (disabled) CancelRun(context.Context, Scope, string) error         { return ErrDisabled }
func (disabled) VerifyResult(context.Context, CallbackEnvelope) (VerifiedResult, error) {
	return VerifiedResult{}, ErrInvalidCallback
}
func (disabled) Health(context.Context) Health {
	return Health{Mode: "disabled", Status: "disabled", Message: "No AI runtime contract is configured yet."}
}

// stub is a development-only, stateless executor: runs "complete" on the first poll with fictional,
// clearly labelled output derived from the agent id. Callbacks use the proposed HMAC contract.
type stub struct{ secret []byte }

func NewDevelopmentStub(callbackSecret string) AgentExecutor {
	return stub{secret: []byte(callbackSecret)}
}

func (stub) Provider() string { return "stub" }

func (stub) StartRun(_ context.Context, req StartRunRequest) (Run, error) {
	if req.IdempotencyKey == "" || req.AgentID == "" {
		return Run{}, errors.New("idempotency key and agent id are required")
	}
	return Run{RunID: "stub:" + req.AgentID + ":" + req.IdempotencyKey, Status: RunQueued, AgentID: req.AgentID, SkillVersion: req.SkillVersion}, nil
}

// StubOutput is the fictional result the stub returns for an agent.
func StubOutput(agentID string) json.RawMessage {
	out, _ := json.Marshal(map[string]any{
		"stub":    true,
		"notice":  "Fictional output from the development agent stub. No model produced this.",
		"agent":   agentID,
		"summary": fmt.Sprintf("Draft prepared by %s (development stub).", agentID),
		"items":   []map[string]any{{"id": "item-1", "text": "Placeholder finding for review"}},
	})
	return out
}

func (stub) GetRun(_ context.Context, _ Scope, runID string) (Run, error) {
	parts := strings.SplitN(runID, ":", 3)
	if len(parts) != 3 || parts[0] != "stub" {
		return Run{}, ErrUnknownRun
	}
	out := StubOutput(parts[1])
	sum, _ := OutputChecksum(out)
	return Run{RunID: runID, Status: RunSucceeded, AgentID: parts[1], Output: out, OutputChecksum: sum, TraceIDs: []string{"stub-trace"}}, nil
}

func (stub) CancelRun(context.Context, Scope, string) error { return nil }

func (s stub) VerifyResult(_ context.Context, cb CallbackEnvelope) (VerifiedResult, error) {
	return VerifyHMACCallback(s.secret, cb, time.Now())
}

func (s stub) Health(context.Context) Health {
	msg := "Development stub: runs complete with fictional output; no model is called."
	if len(s.secret) < 32 {
		msg += " Signed callbacks disabled (no AGENT_CALLBACK_SECRET)."
	}
	return Health{Mode: "stub", Status: "stub", Message: msg}
}
