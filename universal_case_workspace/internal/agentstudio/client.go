// Package agentstudio is UCW's client for the Agent Studio run API. UCW authenticates with its own
// service token (client "case-workspace", run.invoke/read/cancel only) and names the human it acts for
// in delegated_user (the Control Plane user id, which is also the KMS principal id), exactly like
// Flowcase does for its workflow agents.
package agentstudio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrUnavailable: network, timeout, 408/429/5xx. A later attempt may succeed.
	ErrUnavailable = errors.New("agent studio unavailable")
	// ErrRejected: other 4xx. Retrying the same request cannot succeed.
	ErrRejected = errors.New("agent studio rejected the request")
	// ErrUnknownRun: 404 on a run.
	ErrUnknownRun = errors.New("agent run not found")
	// ErrDisabled: no Agent Studio configured.
	ErrDisabled = errors.New("agent studio not configured")
)

// Agent Studio's accepted patterns (internal/httpapi/runtime.go).
var (
	IdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
	RefPattern         = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	agentKeyPattern    = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,62}$`)
	uuidPattern        = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	runIDPattern       = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)
)

const (
	// MaxRequest stays below Agent Studio's 256 KiB run body limit.
	MaxRequest  = 240 << 10
	maxResponse = 2 << 20
	// MaxOutput bounds an accepted run output.
	MaxOutput = 256 << 10
)

// RunRequest is the exact body of POST /api/v1/runs.
type RunRequest struct {
	IdempotencyKey string          `json:"idempotency_key"`
	AgentID        string          `json:"agent_id"`
	SkillVersion   string          `json:"skill_version"`
	Input          json.RawMessage `json:"input"`
	Evidence       []any           `json:"evidence"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	DelegatedUser  string          `json:"delegated_user,omitempty"`
}

type Run struct {
	RunID          string          `json:"run_id"`
	Status         string          `json:"status"`
	AgentID        string          `json:"agent_id"`
	SkillVersion   string          `json:"skill_version"`
	Output         json.RawMessage `json:"output,omitempty"`
	OutputChecksum string          `json:"output_checksum,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// Terminal: anything but queued/running is settled (Agent Studio maps needs_review to succeeded).
func (r Run) Terminal() bool { return r.Status != "queued" && r.Status != "running" && r.Status != "" }

func (r Run) Succeeded() bool { return r.Status == "succeeded" }

// Error is a non-2xx answer.
type Error struct {
	Op        string
	Status    int
	Message   string
	retryable bool
	notFound  bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("agent studio %s: HTTP %d: %s", e.Op, e.Status, e.Message)
}

func (e *Error) Unwrap() []error {
	out := []error{ErrRejected}
	if e.retryable {
		out = []error{ErrUnavailable}
	}
	if e.notFound {
		out = append(out, ErrUnknownRun)
	}
	return out
}

// Runner is the port the intake uses.
type Runner interface {
	StartRun(ctx context.Context, req RunRequest) (Run, error)
	GetRun(ctx context.Context, runID string) (Run, error)
	CancelRun(ctx context.Context, runID string) error
}

type Client struct {
	base   string
	token  string
	client *http.Client
}

func New(baseURL, serviceToken string, timeout time.Duration, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return nil, errors.New("agentstudio: base URL must be an absolute http(s) URL")
	}
	if len(serviceToken) < 32 || strings.ContainsFunc(serviceToken, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return nil, errors.New("agentstudio: service token must have at least 32 printable characters")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if hc == nil {
		hc = &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{base: u.String(), token: serviceToken, client: hc}, nil
}

// Validate checks a request against Agent Studio's accepted patterns before sending it.
func Validate(req RunRequest) error {
	switch {
	case !IdempotencyPattern.MatchString(req.IdempotencyKey):
		return fmt.Errorf("%w: idempotency key not acceptable", ErrRejected)
	case !agentKeyPattern.MatchString(req.AgentID):
		return fmt.Errorf("%w: agent id not acceptable", ErrRejected)
	case req.CorrelationID != "" && !RefPattern.MatchString(req.CorrelationID):
		return fmt.Errorf("%w: correlation id not acceptable", ErrRejected)
	case req.DelegatedUser != "" && !uuidPattern.MatchString(req.DelegatedUser):
		return fmt.Errorf("%w: delegated user must be a UUID", ErrRejected)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(req.Input, &obj) != nil || obj == nil {
		return fmt.Errorf("%w: input must be a JSON object", ErrRejected)
	}
	if req.Evidence == nil {
		req.Evidence = []any{}
	}
	if b, _ := json.Marshal(req); len(b) > MaxRequest {
		return fmt.Errorf("%w: request exceeds %d bytes", ErrRejected, MaxRequest)
	}
	return nil
}

func (c *Client) StartRun(ctx context.Context, req RunRequest) (Run, error) {
	if req.Evidence == nil {
		req.Evidence = []any{}
	}
	if err := Validate(req); err != nil {
		return Run{}, err
	}
	body, _ := json.Marshal(req)
	var run Run
	if err := c.do(ctx, "start run", http.MethodPost, "/api/v1/runs", body, &run); err != nil {
		return Run{}, err
	}
	if run.RunID == "" {
		return Run{}, &Error{Op: "start run", Status: http.StatusBadGateway, Message: "response has no run_id", retryable: true}
	}
	return run, nil
}

func (c *Client) GetRun(ctx context.Context, runID string) (Run, error) {
	if !runIDPattern.MatchString(runID) {
		return Run{}, ErrUnknownRun
	}
	var run Run
	err := c.do(ctx, "get run", http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID), nil, &run)
	return run, err
}

func (c *Client) CancelRun(ctx context.Context, runID string) error {
	if !runIDPattern.MatchString(runID) {
		return ErrUnknownRun
	}
	return c.do(ctx, "cancel run", http.MethodPost, "/api/v1/runs/"+url.PathEscape(runID)+"/cancel", []byte(`{}`), nil)
}

func (c *Client) do(ctx context.Context, op, method, path string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRejected, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return fmt.Errorf("%w: %s: read: %v", ErrUnavailable, op, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		msg := http.StatusText(resp.StatusCode)
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		s := resp.StatusCode
		return &Error{Op: op, Status: s, Message: msg, retryable: s >= 500 || s == 429 || s == 408, notFound: s == 404 && op != "start run"}
	}
	if out == nil {
		return nil
	}
	if len(raw) > maxResponse {
		return &Error{Op: op, Status: resp.StatusCode, Message: "response too large"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return &Error{Op: op, Status: http.StatusBadGateway, Message: "malformed response", retryable: true}
	}
	return nil
}

// OutputChecksum is the SHA-256 of the canonical JSON output (sorted keys, numbers preserved), the
// same canonicalization Agent Studio and Flowcase use.
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

// Disabled is the Runner used when Agent Studio is not configured.
type Disabled struct{}

func (Disabled) StartRun(context.Context, RunRequest) (Run, error) { return Run{}, ErrDisabled }
func (Disabled) GetRun(context.Context, string) (Run, error)       { return Run{}, ErrDisabled }
func (Disabled) CancelRun(context.Context, string) error           { return ErrDisabled }
