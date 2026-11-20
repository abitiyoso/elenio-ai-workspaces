package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/solutions"
)

// Only the configured Flowcase audience receives the caller identity; domain roles stay in Flowcase.
func (s *Server) solutionFlowcase(ctx context.Context, p principal.Principal, method, path string, body []byte, key string) ([]byte, error) {
	decision, err := s.engine.App(ctx, p.UserID, p.WorkspaceID, "flowcase", observability.RequestIDFrom(ctx))
	if err != nil {
		return nil, err
	}
	if !decision.Allowed() {
		return nil, apperr.Forbidden("FLOWCASE_DENIED", "Workflow service access is not granted in this workspace")
	}
	up, ok := s.upstreams.Get("flowcase")
	if !ok || up.Mode != "proxy" {
		return nil, apperr.New(503, "FLOWCASE_UNAVAILABLE", "Trusted Flowcase upstream is not configured")
	}
	var workspaceKey string
	if err = s.pool.QueryRow(ctx, `SELECT key FROM workspaces WHERE id=$1::uuid AND tenant_id=$2::uuid`, p.WorkspaceID, p.TenantID).Scan(&workspaceKey); err != nil {
		return nil, err
	}
	token, _, err := s.tokens.Mint(up.Audience, integrations.Subject{UserID: p.UserID, TenantKey: p.TenantKey, WorkspaceKey: workspaceKey, IdPIssuer: p.Issuer, IdPSubject: p.Subject, PrincipalType: principal.TypeHuman, SessionID: p.SessionID, Scopes: appScopes(decision), PolicyVersion: decision.PolicyVersion, CorrelationID: observability.RequestIDFrom(ctx), AuthTime: p.AuthTime})
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(up.URL)
	if err != nil || target.Host == "" {
		return nil, apperr.New(503, "FLOWCASE_UNAVAILABLE", "Trusted workflow upstream configuration is invalid")
	}
	if !up.StripPrefix {
		path = "/apps/flowcase" + path
	}
	target.Path = singleJoin(target.Path, path)
	target.RawPath = ""
	target.RawQuery = ""
	target.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, apperr.New(503, "FLOWCASE_UNAVAILABLE", "Flowcase could not be reached; retry with the same request key")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 201 {
		code := response.StatusCode
		if code == 401 {
			code = 403
		}
		if code < 400 || code > 599 {
			code = 502
		}
		return nil, apperr.New(code, "FLOWCASE_REJECTED", fmt.Sprintf("Flowcase rejected this execution request (%d). Its case and domain permissions still apply.", response.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, apperr.New(502, "INVALID_CASE_RESPONSE", "Workflow service returned an invalid case response")
	}
	return data, nil
}

type solutionCase struct {
	ID              string `json:"id"`
	Reference       string `json:"reference"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	WorkflowKey     string `json:"workflow_key"`
	WorkflowVersion int    `json:"workflow_version"`
	DefinitionHash  string `json:"definition_hash"`
}

func (s *Server) readSolutionCase(ctx context.Context, p principal.Principal, item solutions.Execution) (solutionCase, error) {
	data, err := s.solutionFlowcase(ctx, p, "GET", "/api/v1/cases/"+item.CaseID, nil, "")
	if err != nil {
		return solutionCase{}, err
	}
	var out solutionCase
	if json.Unmarshal(data, &out) != nil || out.ID != item.CaseID || out.WorkflowKey != item.WorkflowKey || out.WorkflowVersion != item.WorkflowVersion || !workflowHash.MatchString(out.DefinitionHash) {
		return out, apperr.New(502, "INVALID_CASE_RESPONSE", "Case response does not match the saved solution execution pin")
	}
	return out, nil
}
func (s *Server) handleCreateSolutionExecution(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	revision, ok := solutionRevision(w, r)
	if !ok {
		return
	}
	var input solutions.ExecutionInput
	if err := decode(r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(input.Variables) == 0 {
		input.Variables = json.RawMessage(`{}`)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	service := s.solutionService()
	item, err := service.BeginExecution(ctx, p, r.PathValue("id"), revision, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if item.CaseID == "" {
		body, marshalErr := json.Marshal(struct {
			WorkflowKey string          `json:"workflow_key"`
			Version     int             `json:"version"`
			Title       string          `json:"title"`
			Variables   json.RawMessage `json:"variables"`
		}{item.WorkflowKey, item.WorkflowVersion, input.Title, input.Variables})
		if marshalErr != nil {
			s.fail(w, r, marshalErr)
			return
		}
		body, marshalErr = solutions.CanonicalJSON(body)
		if marshalErr != nil {
			s.fail(w, r, marshalErr)
			return
		}
		data, callErr := s.solutionFlowcase(ctx, p, "POST", "/api/v1/cases", body, "solution-"+item.ID)
		if callErr != nil {
			s.fail(w, r, callErr)
			return
		}
		var created solutionCase
		if json.Unmarshal(data, &created) != nil || len(created.ID) != 36 || created.WorkflowKey != item.WorkflowKey || created.WorkflowVersion != item.WorkflowVersion || !workflowHash.MatchString(created.DefinitionHash) {
			s.fail(w, r, apperr.New(502, "INVALID_CASE_RESPONSE", "Workflow service returned a case outside the requested version pin"))
			return
		}
		if err = service.FinishExecution(ctx, p, item, created.ID); err != nil {
			s.fail(w, r, err)
			return
		}
		item.CaseID = created.ID
	}
	current, err := s.readSolutionCase(ctx, p, item)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"execution": item, "case": current})
}
func (s *Server) handleSolutionExecutions(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	items, next, err := s.solutionService().ListExecutions(ctx, p, r.PathValue("id"), r.URL.Query().Get("cursor"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type result struct {
		Execution solutions.Execution `json:"execution"`
		Case      *solutionCase       `json:"case,omitempty"`
	}
	out := []result{}
	for _, item := range items {
		entry := result{Execution: item}
		if item.CaseID != "" {
			current, err := s.readSolutionCase(ctx, p, item)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			entry.Case = &current
		}
		out = append(out, entry)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": out, "next_cursor": next, "scope": "current_user_solution_executions"})
}
