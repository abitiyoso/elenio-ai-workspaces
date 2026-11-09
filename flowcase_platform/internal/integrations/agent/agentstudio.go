package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ProviderAgentStudio is the provider name of the AI Agent Studio runtime. Agent Studio posts signed
// results to POST /api/v1/agent-callbacks/agent-studio.
const ProviderAgentStudio = "agent-studio"

var (
	// ErrRejected marks a request the runtime refused (4xx). Retrying the same request cannot succeed.
	ErrRejected = errors.New("agent runtime rejected the request")
	// ErrUnavailable marks a transient failure (network, timeout, 429, 5xx). A later attempt may succeed.
	ErrUnavailable = errors.New("agent runtime unavailable")
)

// RuntimeError is a non-2xx answer from Agent Studio. It unwraps to ErrRejected or ErrUnavailable,
// and a 404 on a run additionally to ErrUnknownRun.
type RuntimeError struct {
	Op        string
	Status    int
	Message   string
	Retryable bool
	notFound  bool
}

func (e *RuntimeError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("agent studio %s: %s", e.Op, e.Message)
	}
	return fmt.Sprintf("agent studio %s: HTTP %d: %s", e.Op, e.Status, e.Message)
}

func (e *RuntimeError) Unwrap() []error {
	out := []error{ErrRejected}
	if e.Retryable {
		out = []error{ErrUnavailable}
	}
	if e.notFound {
		out = append(out, ErrUnknownRun)
	}
	return out
}

// SubjectResolver maps a Flowcase user id (Scope.UserID) to that user's directory subject. In
// trusted_jwt mode users.subject holds the Control Plane user id. An empty subject means "unknown".
type SubjectResolver func(ctx context.Context, tenantID, userID string) (string, error)

type AgentStudioConfig struct {
	BaseURL        string
	ServiceToken   string
	CallbackSecret string
	// Timeout bounds every HTTP call (default 15s). Health uses min(Timeout, 3s).
	Timeout        time.Duration
	HTTPClient     *http.Client
	ResolveSubject SubjectResolver
	Now            func() time.Time
}

type agentStudio struct {
	base    *url.URL
	token   string
	secret  []byte
	client  *http.Client
	timeout time.Duration
	resolve SubjectResolver
	now     func() time.Time
}

const (
	// MaxAgentStudioRequest stays below Agent Studio's 256 KiB run body limit.
	MaxAgentStudioRequest = 240 << 10
	maxResponseBytes      = 2 << 20
	maxCaseSummary        = 4000
	maxBrief              = 3000
	maxRFPExcerpt         = 20000
	maxCustomer           = 200
	maxConstraints        = 50
	maxConstraint         = 500
)

var (
	asIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
	asRefPattern         = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	uuidPattern          = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	runIDPattern         = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)
)

// NewAgentStudio returns the Agent Studio runtime client. Configuration is validated at startup by
// the config package; this re-checks the essentials so a misconfiguration can never call out.
func NewAgentStudio(cfg AgentStudioConfig) (AgentExecutor, error) {
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("agent studio: base URL must be an absolute http(s) URL without query, fragment or credentials")
	}
	if len(cfg.ServiceToken) < 32 || strings.ContainsFunc(cfg.ServiceToken, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return nil, errors.New("agent studio: service token must have at least 32 printable characters")
	}
	if len(cfg.CallbackSecret) < 32 {
		return nil, errors.New("agent studio: AGENT_CALLBACK_SECRET (>= 32 characters) is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &agentStudio{base: u, token: cfg.ServiceToken, secret: []byte(cfg.CallbackSecret), client: client, timeout: timeout,
		resolve: cfg.ResolveSubject, now: now}, nil
}

func (a *agentStudio) Provider() string { return ProviderAgentStudio }

// AgentStudioRunRequest is the exact body of POST /api/v1/runs. case_id is deliberately never sent:
// the case is identified by correlation_id only.
type AgentStudioRunRequest struct {
	IdempotencyKey string          `json:"idempotency_key"`
	AgentID        string          `json:"agent_id"`
	SkillVersion   string          `json:"skill_version"`
	Input          json.RawMessage `json:"input"`
	Evidence       []Evidence      `json:"evidence"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	DelegatedUser  string          `json:"delegated_user,omitempty"`
}

func rejected(op, format string, args ...any) error {
	return &RuntimeError{Op: op, Status: 0, Message: fmt.Sprintf(format, args...)}
}

// BuildRunRequest maps Flowcase's StartRunRequest onto the Agent Studio run contract.
func (a *agentStudio) BuildRunRequest(ctx context.Context, req StartRunRequest) (AgentStudioRunRequest, error) {
	key := strings.ReplaceAll(req.IdempotencyKey, "#", ":") // older keys used exec#attempt
	if !asIdempotencyPattern.MatchString(key) {
		return AgentStudioRunRequest{}, rejected("start run", "idempotency key %q is not acceptable to Agent Studio", key)
	}
	if req.AgentID == "" {
		return AgentStudioRunRequest{}, rejected("start run", "agent id is required")
	}
	skill := req.SkillVersion
	if skill == "unpinned" {
		skill = "" // empty = the agent's active release
	}
	out := AgentStudioRunRequest{IdempotencyKey: key, AgentID: req.AgentID, SkillVersion: skill, Evidence: req.Evidence}
	if out.Evidence == nil {
		out.Evidence = []Evidence{}
	}
	if asRefPattern.MatchString(req.Scope.CaseID) {
		out.CorrelationID = req.Scope.CaseID
	}
	if a.resolve != nil && req.Scope.UserID != "" {
		subject, err := a.resolve(ctx, req.Scope.TenantID, req.Scope.UserID)
		if err != nil {
			return AgentStudioRunRequest{}, fmt.Errorf("%w: resolve delegated user: %v", ErrUnavailable, err)
		}
		if uuidPattern.MatchString(subject) {
			out.DelegatedUser = strings.ToLower(subject)
		}
	}
	for _, level := range []int{0, 1, 2} {
		input, err := MapSIMInput(req.Input, level)
		if err != nil {
			return AgentStudioRunRequest{}, rejected("start run", "agent input: %v", err)
		}
		out.Input = input
		if b, _ := json.Marshal(out); len(b) <= MaxAgentStudioRequest {
			return out, nil
		}
	}
	return AgentStudioRunRequest{}, rejected("start run", "request exceeds %d bytes even without upstream artifact content", MaxAgentStudioRequest)
}

func (a *agentStudio) StartRun(ctx context.Context, req StartRunRequest) (Run, error) {
	body, err := a.BuildRunRequest(ctx, req)
	if err != nil {
		return Run{}, err
	}
	raw, _ := json.Marshal(body)
	var run Run
	if err := a.do(ctx, "start run", http.MethodPost, "/api/v1/runs", raw, &run); err != nil {
		return Run{}, err
	}
	if run.RunID == "" {
		return Run{}, &RuntimeError{Op: "start run", Status: http.StatusBadGateway, Message: "response has no run_id", Retryable: true}
	}
	return run, nil
}

func (a *agentStudio) GetRun(ctx context.Context, _ Scope, runID string) (Run, error) {
	if !runIDPattern.MatchString(runID) {
		return Run{}, ErrUnknownRun
	}
	var run Run
	if err := a.do(ctx, "get run", http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID), nil, &run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (a *agentStudio) CancelRun(ctx context.Context, _ Scope, runID string) error {
	if !runIDPattern.MatchString(runID) {
		return ErrUnknownRun
	}
	return a.do(ctx, "cancel run", http.MethodPost, "/api/v1/runs/"+url.PathEscape(runID)+"/cancel", []byte(`{}`), nil)
}

func (a *agentStudio) VerifyResult(_ context.Context, cb CallbackEnvelope) (VerifiedResult, error) {
	return VerifyHMACCallback(a.secret, cb, a.now())
}

func (a *agentStudio) Health(ctx context.Context) Health {
	h := Health{Mode: "agent_studio"}
	ctx, cancel := context.WithTimeout(ctx, min(a.timeout, 3*time.Second))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base.String()+"/health/ready", nil)
	if err == nil {
		var resp *http.Response
		if resp, err = a.client.Do(req); err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				h.Status, h.Message = "ok", "Agent Studio at "+a.base.Host+" is ready; runs use the agents' active releases unless pinned."
				return h
			}
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
	}
	h.Status, h.Message = "unavailable", "Agent Studio at "+a.base.Host+" is not ready ("+err.Error()+")."
	return h
}

func (a *agentStudio) do(ctx context.Context, op, method, path string, body []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base.String()+path, rd)
	if err != nil {
		return rejected(op, "build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: %s: read response: %v", ErrUnavailable, op, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return statusError(op, resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if len(raw) > maxResponseBytes {
		return &RuntimeError{Op: op, Status: resp.StatusCode, Message: "response too large"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return &RuntimeError{Op: op, Status: http.StatusBadGateway, Message: "malformed response", Retryable: true}
	}
	return nil
}

func statusError(op string, status int, raw []byte) error {
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	msg := http.StatusText(status)
	if json.Unmarshal(raw, &body) == nil {
		if body.Error != "" {
			msg = body.Error
		} else if body.Message != "" {
			msg = body.Message
		}
	}
	msg = truncateRunes(strings.TrimSpace(msg), 300)
	return &RuntimeError{Op: op, Status: status, Message: msg,
		Retryable: status >= 500 || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout,
		notFound:  status == http.StatusNotFound && op != "start run"}
}

// --- SIM input mapping ------------------------------------------------------------------------------

// flowcaseInput is the agent input Flowcase builds (cases.agentInput).
type flowcaseInput struct {
	Task               string             `json:"task"`
	NodeID             string             `json:"node_id"`
	CaseReference      string             `json:"case_reference"`
	CaseTitle          string             `json:"case_title"`
	Iteration          json.Number        `json:"iteration"`
	OutputArtifactType string             `json:"output_artifact_type"`
	CaseData           map[string]any     `json:"case_data"`
	RevisionNote       string             `json:"revision_note"`
	UpstreamArtifacts  []upstreamArtifact `json:"upstream_artifacts"`
}

type upstreamArtifact struct {
	Type    string          `json:"type"`
	NodeID  string          `json:"node_id"`
	Version int             `json:"version"`
	Status  string          `json:"status"`
	Content json.RawMessage `json:"content"`
}

// briefKeys are case_data fields that map to their own Agent Studio fields, not the summary list.
var briefKeys = map[string]bool{"case_summary": true, "customer_name": true, "customer": true, "rfp_excerpt": true, "constraints": true}

// MapSIMInput converts Flowcase's agent input into the Agent Studio SIM input_schema
// ({case_summary, rfp_excerpt?, customer?, upstream_artifacts?, constraints?}, additionalProperties
// false). shrink > 0 reduces upstream artifact content (1: excerpts, 2: metadata only).
func MapSIMInput(raw json.RawMessage, shrink int) (json.RawMessage, error) {
	var in flowcaseInput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("unreadable: %w", err)
	}
	out := map[string]any{"case_summary": caseSummary(in)}
	if s := firstString(in.CaseData, "customer_name", "customer", "customer_id"); s != "" {
		out["customer"] = truncateRunes(s, maxCustomer)
	}
	if s := firstString(in.CaseData, "rfp_excerpt"); s != "" {
		out["rfp_excerpt"] = truncateRunes(s, maxRFPExcerpt)
	}
	if c := constraints(in.CaseData["constraints"]); len(c) > 0 {
		out["constraints"] = c
	}
	if len(in.UpstreamArtifacts) > 0 {
		ups := map[string]any{}
		for _, u := range in.UpstreamArtifacts {
			if u.Type == "" {
				continue
			}
			entry := map[string]any{"producer_node": u.NodeID, "version": u.Version, "status": u.Status}
			content := artifactDraft(u.Content)
			switch {
			case shrink == 0:
				entry["content"] = content
			case shrink == 1:
				entry["content"] = map[string]any{"truncated": true, "excerpt": truncateRunes(string(content), 4000)}
			default:
				entry["content_omitted"] = true
			}
			ups[u.Type] = entry
		}
		if len(ups) > 0 {
			out["upstream_artifacts"] = ups
		}
	}
	return json.Marshal(out)
}

// artifactDraft strips Agent Studio's result envelope so a later agent sees the prior draft itself.
func artifactDraft(raw json.RawMessage) json.RawMessage {
	var env map[string]json.RawMessage
	if json.Unmarshal(raw, &env) == nil {
		if d, ok := env["artifact_draft"]; ok && len(d) > 0 && string(d) != "null" {
			return d
		}
	}
	if len(raw) == 0 {
		return json.RawMessage(`null`)
	}
	return raw
}

func caseSummary(in flowcaseInput) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	switch {
	case in.CaseTitle != "" && in.CaseReference != "":
		line("Case: %s (%s)", in.CaseTitle, in.CaseReference)
	case in.CaseTitle != "":
		line("Case: %s", in.CaseTitle)
	case in.CaseReference != "":
		line("Case: %s", in.CaseReference)
	}
	task := in.Task
	if task == "" {
		task = in.NodeID
	}
	if task == "" {
		task = "Agent task"
	}
	line("Task: %s (step %s, iteration %s)", task, orDash(in.NodeID), orDash(in.Iteration.String()))
	if in.OutputArtifactType != "" {
		line("Expected output: %s", in.OutputArtifactType)
	}
	if note := strings.TrimSpace(in.RevisionNote); note != "" {
		line("Revision requested (case owner or reviewer feedback — revise the previous draft accordingly): %s", truncateRunes(note, 1000))
	}
	if brief := strings.TrimSpace(firstString(in.CaseData, "case_summary")); brief != "" {
		line("Brief: %s", truncateRunes(brief, maxBrief))
	}
	keys := make([]string, 0, len(in.CaseData))
	for k := range in.CaseData {
		if !briefKeys[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		line("Case data:")
		for _, k := range keys {
			line("- %s: %s", k, renderValue(in.CaseData[k]))
		}
	}
	if len(in.UpstreamArtifacts) > 0 {
		var names []string
		for _, u := range in.UpstreamArtifacts {
			names = append(names, fmt.Sprintf("%s v%d (%s)", u.Type, u.Version, u.Status))
		}
		line("Prior artifacts (see upstream_artifacts): %s", strings.Join(names, ", "))
	}
	return truncateRunes(strings.TrimSpace(b.String()), maxCaseSummary)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func renderValue(v any) string {
	switch x := v.(type) {
	case string:
		return truncateRunes(x, 300)
	case json.Number:
		return x.String()
	case bool:
		return fmt.Sprint(x)
	case nil:
		return "-"
	}
	b, _ := json.Marshal(v)
	return truncateRunes(string(b), 300)
}

func firstString(data map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := data[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// constraints accepts an array of strings or one newline-separated string.
func constraints(v any) []string {
	var raw []string
	switch x := v.(type) {
	case string:
		raw = strings.Split(x, "\n")
	case []any:
		for _, item := range x {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	}
	var out []string
	for _, s := range raw {
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "- "))
		if s == "" {
			continue
		}
		out = append(out, truncateRunes(s, maxConstraint))
		if len(out) == maxConstraints {
			break
		}
	}
	return out
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
