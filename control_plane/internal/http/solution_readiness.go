package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/solutions"
)

type solutionDependency struct {
	Key               string                         `json:"key"`
	Version           int                            `json:"version"`
	State             string                         `json:"state"`
	Reason            string                         `json:"reason"`
	DefinitionHash    string                         `json:"definition_hash,omitempty"`
	Resources         []solutions.ResourceDependency `json:"resources"`
	InventoryComplete bool                           `json:"inventory_complete"`
	BindingSnapshot   *solutionBindingSnapshot       `json:"binding_snapshot,omitempty"`
	BindingReason     string                         `json:"binding_reason,omitempty"`
}
type solutionReadiness struct {
	SolutionID        string               `json:"solution_id"`
	Revision          int                  `json:"revision"`
	CheckedAt         time.Time            `json:"checked_at"`
	Workflows         []solutionDependency `json:"workflows"`
	Blockers          []string             `json:"blockers"`
	ReleaseReady      bool                 `json:"release_ready"`
	ApplicationIssues []string             `json:"application_issues"`
}

var workflowHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Readiness is read-only evidence. It never publishes, mints additional domain permissions,
// probes a model, calls a body-supplied URL or marks a solution as an approved release.
func (s *Server) handleSolutionReadiness(w http.ResponseWriter, r *http.Request, p principal.Principal) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out, err := s.collectSolutionReadiness(ctx, p, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) collectSolutionReadiness(ctx context.Context, p principal.Principal, id string) (solutionReadiness, error) {
	plan, err := s.solutionService().Get(ctx, p, id)
	if err != nil {
		return solutionReadiness{}, err
	}
	out := solutionReadiness{SolutionID: plan.ID, Revision: plan.Revision, CheckedAt: time.Now().UTC(), Workflows: []solutionDependency{}, Blockers: []string{"Application composition, all transitive dependencies, solution approval and target installation are not verified by this workflow check."}, ReleaseReady: false}
	out.ApplicationIssues = []string{}
	if plan.Definition.Output != "api" && plan.Definition.Application == nil {
		out.ApplicationIssues = append(out.ApplicationIssues, "Business application pages are not configured.")
	}
	if plan.Definition.Application != nil {
		out.ApplicationIssues = append(out.ApplicationIssues, "Page-to-runtime schema mapping, business permissions and target application installation require verification.")
	}
	if len(plan.Definition.Workflows) == 0 {
		out.Blockers = append(out.Blockers, "No workflow versions are linked.")
		return out, nil
	}
	decision, err := s.engine.App(ctx, p.UserID, p.WorkspaceID, "flowcase", observability.RequestIDFrom(ctx))
	if err != nil {
		return solutionReadiness{}, err
	}
	up, configured := s.upstreams.Get("flowcase")
	reason := ""
	if !decision.Allowed() {
		reason = "Workflow service access is not granted in this workspace."
	} else if !configured || up.Mode != "proxy" {
		reason = "The trusted Flowcase upstream is not configured."
	}
	var token string
	if reason == "" {
		var workspaceKey string
		err = s.pool.QueryRow(ctx, `SELECT key FROM workspaces WHERE id=$1::uuid AND tenant_id=$2::uuid`, p.WorkspaceID, p.TenantID).Scan(&workspaceKey)
		if err != nil {
			return solutionReadiness{}, err
		}
		token, _, err = s.tokens.Mint(up.Audience, integrations.Subject{UserID: p.UserID, TenantKey: p.TenantKey, WorkspaceKey: workspaceKey, IdPIssuer: p.Issuer, IdPSubject: p.Subject, PrincipalType: principal.TypeHuman, SessionID: p.SessionID, Scopes: appScopes(decision), PolicyVersion: decision.PolicyVersion, CorrelationID: observability.RequestIDFrom(ctx), AuthTime: p.AuthTime})
		if err != nil {
			return solutionReadiness{}, err
		}
	}
	// No environment HTTP proxy and no redirects; only the deployment-configured upstream.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	remainingResources := 200
	truncated := false
	for _, ref := range plan.Definition.Workflows {
		dependency := solutionDependency{Key: ref.Key, Version: ref.Version, State: "blocked", Reason: reason, Resources: []solutions.ResourceDependency{}}
		if reason == "" {
			target, parseErr := url.Parse(up.URL)
			if parseErr != nil || target.Host == "" {
				dependency.Reason = "Trusted workflow upstream configuration is invalid."
			} else {
				path := "/api/v1/workflows/" + ref.Key + "/versions/" + strconv.Itoa(ref.Version)
				if !up.StripPrefix {
					path = "/apps/flowcase" + path
				}
				target.Path = singleJoin(target.Path, path)
				target.RawPath = ""
				target.RawQuery = ""
				target.Fragment = ""
				dependency = s.checkSolutionWorkflow(ctx, client, target.String(), token, ref.Key, ref.Version, remainingResources)
			}
		}
		if len(dependency.Resources) > remainingResources {
			dependency.Resources = dependency.Resources[:remainingResources]
			dependency.InventoryComplete = false
			truncated = true
		}
		remainingResources -= len(dependency.Resources)
		out.Workflows = append(out.Workflows, dependency)
	}
	if truncated {
		out.Blockers = append(out.Blockers, "Reference inventory exceeds the 200-resource report limit; the remaining dependencies are unresolved.")
	}
	return out, nil
}
func (s *Server) checkSolutionWorkflow(ctx context.Context, client *http.Client, target, token, key string, version, bindingLimit int) solutionDependency {
	out := solutionDependency{Key: key, Version: version, State: "blocked", Reason: "Workflow service is unavailable or returned an invalid response.", Resources: []solutions.ResourceDependency{}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return out
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return out
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		if response.StatusCode == 401 || response.StatusCode == 403 {
			out.Reason = "Workflow version access is not granted."
		} else if response.StatusCode == 404 {
			out.Reason = "Pinned workflow version was not found."
		}
		return out
	}
	// Parse no customer input/output, artifacts, provider bodies or workflow configuration into this report.
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return out
	}
	var snapshot struct {
		WorkflowKey    string          `json:"workflow_key"`
		Version        int             `json:"version"`
		Status         string          `json:"status"`
		DefinitionHash string          `json:"definition_hash"`
		Definition     json.RawMessage `json:"definition"`
	}
	if json.Unmarshal(data, &snapshot) != nil || snapshot.WorkflowKey != key || snapshot.Version != version {
		return out
	}
	if snapshot.Status != "published" {
		out.Reason = "Pinned workflow is not published. Publish through the existing workflow gate first."
		return out
	}
	if !workflowHash.MatchString(snapshot.DefinitionHash) {
		out.Reason = "Published workflow has no valid immutable definition hash."
		return out
	}
	out.State = "verified"
	out.Reason = "Published workflow version observed through its authorized service. Transitive dependencies and deployment remain separate gates."
	out.DefinitionHash = snapshot.DefinitionHash
	out.Resources, out.InventoryComplete = solutions.Inventory(snapshot.Definition, key, version)
	observeSolutionBindings(ctx, client, target, token, &out, bindingLimit)
	return out
}
