// Package flowcase is UCW's client for the Flowcase API. Every call carries a user-scoped token
// (aud=flowcase) obtained from the Control Plane token exchange, so Flowcase applies its own case
// visibility and role checks to the real user. UCW never talks to Flowcase's database.
package flowcase

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
	"strconv"
	"strings"
	"time"

	"github.com/elenio/case-workspace/internal/reqctx"
)

var (
	// ErrUnavailable: network failure, timeout or 5xx.
	ErrUnavailable = errors.New("flowcase unavailable")
)

// Error is a non-2xx Flowcase answer (4xx), passed through to the client with its status and code.
type Error struct {
	Status  int      `json:"-"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []Detail `json:"details,omitempty"`
}

type Detail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("flowcase HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

const maxResponse = 8 << 20

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ValidID reports whether s looks like a Flowcase id (UUID); anything else cannot exist.
func ValidID(s string) bool { return idPattern.MatchString(s) }

type Client struct {
	base   string
	client *http.Client
}

func New(baseURL string, timeout time.Duration, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return nil, errors.New("flowcase: base URL must be an absolute http(s) URL")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if hc == nil {
		hc = &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{base: u.String(), client: hc}, nil
}

// do performs one request. Header values are set as given (If-Match, Idempotency-Key).
func (c *Client) do(ctx context.Context, token, method, path string, body any, headers map[string]string, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id := reqctx.ID(ctx); id != "" {
		req.Header.Set("X-Request-ID", id)
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", ErrUnavailable, err)
	}
	if len(raw) > maxResponse {
		return nil, fmt.Errorf("%w: response too large", ErrUnavailable)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var env struct {
			Error Error `json:"error"`
		}
		e := &Error{Status: resp.StatusCode, Code: "flowcase_error", Message: http.StatusText(resp.StatusCode)}
		if json.Unmarshal(raw, &env) == nil && env.Error.Code != "" {
			e.Code, e.Message, e.Details = env.Error.Code, env.Error.Message, env.Error.Details
		}
		return resp.Header, e
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return nil, fmt.Errorf("%w: malformed response: %v", ErrUnavailable, err)
		}
	}
	return resp.Header, nil
}

// --- types (subset of Flowcase internal/cases/types.go; passthrough parts stay raw) -----------------

type CaseSummary struct {
	ID               string    `json:"id"`
	Reference        string    `json:"reference"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	Outcome          *string   `json:"outcome"`
	WorkflowKey      string    `json:"workflow_key"`
	WorkflowName     string    `json:"workflow_name"`
	WorkflowVersion  int       `json:"workflow_version"`
	OwnerName        string    `json:"owner_name"`
	CurrentSteps     []string  `json:"current_steps"`
	OpenTasks        int       `json:"open_tasks"`
	PendingApprovals int       `json:"pending_approvals"`
	Progress         int       `json:"progress"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	VersionNum       int       `json:"version_num"`
}

type GraphNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Execution struct {
	NodeID    string    `json:"node_id"`
	Iteration int       `json:"iteration"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ArtifactVersion struct {
	ID        string          `json:"id"`
	Version   int             `json:"version"`
	Status    string          `json:"status"`
	Kind      string          `json:"produced_by_kind"`
	Content   json.RawMessage `json:"content"`
	CreatedAt time.Time       `json:"created_at"`
}

type Artifact struct {
	ID             string            `json:"id"`
	Type           string            `json:"artifact_type"`
	ProducerNodeID string            `json:"producer_node_id"`
	CurrentVersion int               `json:"current_version"`
	Versions       []ArtifactVersion `json:"versions"`
}

// NodeItem is the part of a task, approval or draft review the BFF reads (the rest passes through).
type NodeItem struct {
	ID           string `json:"id"`
	NodeID       string `json:"node_id"`
	Status       string `json:"status"`
	ArtifactType string `json:"artifact_type"`
	CanSubmit    bool   `json:"can_submit"`
	CanRevise    bool   `json:"can_revise"`
	VersionNum   int    `json:"version_num"`
	Case         struct {
		ID string `json:"id"`
	} `json:"case"`
}

type CaseDetail struct {
	CaseSummary
	Variables json.RawMessage `json:"variables"`
	Facets    json.RawMessage `json:"business_facets"`
	Nodes     []GraphNode     `json:"workflow_nodes"`
	Timeline  []Execution     `json:"timeline"`
	Tasks     json.RawMessage `json:"tasks"`
	Approvals json.RawMessage `json:"approvals"`
	History   json.RawMessage `json:"history"`
	AgentRuns json.RawMessage `json:"agent_runs"`
	Drafts    json.RawMessage `json:"drafts"`
	Artifacts []Artifact      `json:"artifacts"`
	Actions   struct {
		CanNote bool `json:"can_note"`
	} `json:"actions"`
}

func items(raw json.RawMessage) []NodeItem {
	var out []NodeItem
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (d *CaseDetail) TaskItems() []NodeItem     { return items(d.Tasks) }
func (d *CaseDetail) ApprovalItems() []NodeItem { return items(d.Approvals) }
func (d *CaseDetail) DraftItems() []NodeItem    { return items(d.Drafts) }

type Session struct {
	UserID      string   `json:"user_id"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
}

type CaseList struct {
	Items      []CaseSummary `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}

type Workflow struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	LatestPublished *int   `json:"latest_published_version"`
}

type WorkflowVersion struct {
	Version    int    `json:"version"`
	Status     string `json:"status"`
	Definition struct {
		InputSchema struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"input_schema"`
	} `json:"definition"`
}

type CreateInput struct {
	WorkflowKey string         `json:"workflow_key"`
	Version     int            `json:"version"`
	Title       string         `json:"title"`
	Variables   map[string]any `json:"variables"`
}

// --- calls ------------------------------------------------------------------------------------------

func (c *Client) Session(ctx context.Context, token string) (Session, error) {
	var s Session
	_, err := c.do(ctx, token, http.MethodGet, "/api/v1/session", nil, nil, &s)
	return s, err
}

func (c *Client) ListCases(ctx context.Context, token, q, status, cursor string) (CaseList, error) {
	v := url.Values{}
	for k, val := range map[string]string{"q": q, "status": status, "cursor": cursor} {
		if val != "" {
			v.Set(k, val)
		}
	}
	path := "/api/v1/cases"
	if len(v) > 0 {
		path += "?" + v.Encode()
	}
	var out CaseList
	_, err := c.do(ctx, token, http.MethodGet, path, nil, nil, &out)
	return out, err
}

func (c *Client) GetCase(ctx context.Context, token, id string) (*CaseDetail, error) {
	if !ValidID(id) {
		return nil, &Error{Status: http.StatusNotFound, Code: "not_found", Message: "Not found."}
	}
	var d CaseDetail
	if _, err := c.do(ctx, token, http.MethodGet, "/api/v1/cases/"+id, nil, nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (c *Client) ListDrafts(ctx context.Context, token, status string) ([]NodeItem, error) {
	var out struct {
		Items []NodeItem `json:"items"`
	}
	_, err := c.do(ctx, token, http.MethodGet, "/api/v1/drafts?status="+url.QueryEscape(status), nil, nil, &out)
	return out.Items, err
}

func etag(n int) string { return `"` + strconv.Itoa(n) + `"` }

func (c *Client) SubmitDraft(ctx context.Context, token, id string, ifMatch int, note string) error {
	body := map[string]string{}
	if note != "" {
		body["note"] = note
	}
	_, err := c.do(ctx, token, http.MethodPost, "/api/v1/drafts/"+id+"/submit", body, map[string]string{"If-Match": etag(ifMatch)}, nil)
	return err
}

func (c *Client) ReviseDraft(ctx context.Context, token, id string, ifMatch int, feedback string) error {
	_, err := c.do(ctx, token, http.MethodPost, "/api/v1/drafts/"+id+"/revision", map[string]string{"feedback": feedback},
		map[string]string{"If-Match": etag(ifMatch)}, nil)
	return err
}

func (c *Client) GetWorkflow(ctx context.Context, token, key string) (Workflow, error) {
	var w Workflow
	_, err := c.do(ctx, token, http.MethodGet, "/api/v1/workflows/"+url.PathEscape(key), nil, nil, &w)
	return w, err
}

func (c *Client) GetWorkflowVersion(ctx context.Context, token, key string, version int) (WorkflowVersion, error) {
	var v WorkflowVersion
	_, err := c.do(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/workflows/%s/versions/%d", url.PathEscape(key), version), nil, nil, &v)
	return v, err
}

// CreateCase creates a case as the user (Flowcase requires Idempotency-Key) and returns the new case.
func (c *Client) CreateCase(ctx context.Context, token, idempotencyKey string, in CreateInput) (*CaseDetail, error) {
	if in.Variables == nil {
		in.Variables = map[string]any{}
	}
	var d CaseDetail
	if _, err := c.do(ctx, token, http.MethodPost, "/api/v1/cases", in, map[string]string{"Idempotency-Key": idempotencyKey}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (c *Client) AddNote(ctx context.Context, token, caseID, body string) error {
	_, err := c.do(ctx, token, http.MethodPost, "/api/v1/cases/"+caseID+"/events", map[string]string{"type": "note", "body": body}, nil, nil)
	return err
}
