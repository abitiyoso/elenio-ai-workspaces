// Package api is the HTTP transport. Every /api/v1 route is authenticated through the AuthAdapter;
// handlers read identity only from the request context.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/elenio/flowcase/internal/cases"
	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/kms"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/metrics"
)

// Check is a named readiness probe. Its error text is logged but never returned to clients.
type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

type Dependencies struct {
	Logger      *slog.Logger
	Environment string
	Auth        auth.AuthAdapter
	Readiness   []Check
	Knowledge   kms.KnowledgeProvider
	Agents      agent.AgentExecutor
	Registry    *Registry     // nil disables the workflow routes (tests of the platform surface only)
	Cases       *cases.Engine // nil disables the case, task and approval routes
	Metrics     *metrics.Registry
}

func NewServer(d Dependencies) http.Handler {
	s := &server{deps: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	// Unknown /api/ paths are authenticated too, so anonymous callers cannot probe for routes.
	mux.Handle("GET /api/v1/session", s.authenticate(http.HandlerFunc(s.session)))
	mux.Handle("GET /api/v1/integrations", s.authenticate(http.HandlerFunc(s.integrations)))
	if d.Registry != nil {
		s.registerWorkflowRoutes(mux)
	}
	if d.Cases != nil {
		s.registerCaseRoutes(mux)
	}
	mux.Handle("/api/", s.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "route_not_found", "No such API route.", nil)
	})))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "route_not_found", "No such route.", nil)
	})

	return traceIDs(securityHeaders(requestLogger(d.Logger, d.Metrics, recoverer(d.Logger, mux))))
}

type server struct{ deps Dependencies }

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type checkResult struct {
	Status string `json:"status"`
}

func (s *server) runChecks(ctx context.Context) (map[string]checkResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	results := map[string]checkResult{}
	ready := true
	for _, c := range s.deps.Readiness {
		if err := c.Run(ctx); err != nil {
			ready = false
			results[c.Name] = checkResult{Status: "unavailable"}
			s.deps.Logger.WarnContext(ctx, "readiness check failed", "check", c.Name, "error", err.Error())
			continue
		}
		results[c.Name] = checkResult{Status: "ok"}
	}
	return results, ready
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	results, ready := s.runChecks(r.Context())
	status, label := http.StatusOK, "ready"
	if !ready {
		status, label = http.StatusServiceUnavailable, "not_ready"
	}
	writeJSON(w, status, map[string]any{"status": label, "checks": results})
}

type personaView struct {
	Key         string   `json:"key"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
}

type sessionResponse struct {
	Persona     string        `json:"persona,omitempty"`
	DevPersonas []personaView `json:"dev_personas,omitempty"`
	UserID      string        `json:"user_id"`
	TenantID    string        `json:"tenant_id"`
	DisplayName string        `json:"display_name"`
	Roles       []string      `json:"roles"`
	AuthMode    string        `json:"auth_mode"`
	Environment string        `json:"environment"`
	ExpiresAt   *time.Time    `json:"expires_at"`
}

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	roles := make([]string, 0, len(id.Roles))
	for _, role := range id.Roles {
		roles = append(roles, string(role))
	}
	res := sessionResponse{UserID: id.UserID, TenantID: id.TenantID, DisplayName: id.DisplayName, Roles: roles, AuthMode: id.AuthMode, Environment: s.deps.Environment}
	if !id.ExpiresAt.IsZero() {
		exp := id.ExpiresAt.UTC()
		res.ExpiresAt = &exp
	}
	if lister, ok := s.deps.Auth.(interface{ Personas() []auth.DevPersona }); ok && id.AuthMode == "dev_mock" {
		res.Persona = id.Persona
		for _, p := range lister.Personas() {
			v := personaView{Key: p.Key, DisplayName: p.DisplayName}
			for _, r := range p.Roles {
				v.Roles = append(v.Roles, string(r))
			}
			res.DevPersonas = append(res.DevPersonas, v)
		}
	}
	writeJSON(w, http.StatusOK, res)
}

type integrationStatus struct {
	Mode    string `json:"mode"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// integrations reports adapter modes and health without endpoints, keys or other configuration secrets.
func (s *server) integrations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authStatus := integrationStatus{Mode: s.deps.Auth.Mode(), Status: "ok", Message: "Host token verified server-side; roles from Flowcase directory."}
	if authStatus.Mode == "dev_mock" {
		authStatus = integrationStatus{Mode: "dev_mock", Status: "warning", Message: "Development mock identity. Never enabled outside APP_ENV=development."}
	}
	kh := s.deps.Knowledge.Health(ctx)
	ah := s.deps.Agents.Health(ctx)
	_, ready := s.runChecks(ctx)
	runtime := integrationStatus{Mode: "postgres", Status: "ok", Message: "System of record reachable and schema current."}
	if !ready {
		runtime.Status, runtime.Message = "unavailable", "One or more readiness checks failed."
	}
	writeJSON(w, http.StatusOK, map[string]integrationStatus{
		"host_auth":     authStatus,
		"knowledge":     {Mode: kh.Mode, Status: kh.Status, Message: kh.Message},
		"agent_runtime": {Mode: ah.Mode, Status: ah.Status, Message: ah.Message},
		"case_runtime":  runtime,
	})
}
