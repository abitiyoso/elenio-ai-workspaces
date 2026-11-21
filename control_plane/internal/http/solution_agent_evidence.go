package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/apperr"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/solutions"
)

type sourceDependencyPin struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Checksum  string `json:"checksum"`
	State     string `json:"state"`
	ScopeHash string `json:"scope_hash,omitempty"`
}
type sourceModelPin struct {
	ID            string `json:"id"`
	ConfigVersion int    `json:"config_version"`
	Approved      bool   `json:"approved"`
}
type sourceAgentEvidence struct {
	Schema                         string                `json:"schema"`
	AgentKey                       string                `json:"agent_key"`
	Environment                    string                `json:"environment"`
	ReleaseID                      string                `json:"release_id"`
	AgentVersion                   sourceDependencyPin   `json:"agent_version"`
	Fingerprint                    string                `json:"fingerprint"`
	RequestID                      string                `json:"request_id,omitempty"`
	EvaluationID                   string                `json:"evaluation_id,omitempty"`
	EvaluationGate                 string                `json:"evaluation_gate"`
	IndependentApproval            bool                  `json:"independent_approval"`
	SelfApprovalExempt             bool                  `json:"self_approval_exempt"`
	EvaluationPrimaryConfigMatches bool                  `json:"evaluation_primary_config_matches"`
	PrimaryModel                   *sourceModelPin       `json:"primary_model,omitempty"`
	FallbackModel                  *sourceModelPin       `json:"fallback_model,omitempty"`
	Skills                         []sourceDependencyPin `json:"skills"`
	Tools                          []sourceDependencyPin `json:"tools"`
	Blockers                       []string              `json:"blockers"`
	State                          string                `json:"state"`
	ContentHash                    string                `json:"content_hash"`
	ObservedAt                     time.Time             `json:"observed_at"`
}
type solutionAgentObservation struct {
	RuntimeAgentID  string               `json:"runtime_agent_id"`
	ExpectedVersion string               `json:"expected_version"`
	Reason          string               `json:"reason"`
	Evidence        *sourceAgentEvidence `json:"evidence,omitempty"`
}

var sourceUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var sourceSemver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var sourceCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,95}$`)

func (s *Server) observeSolutionAgentReleases(ctx context.Context, p principal.Principal, client *http.Client, out *solutionReadiness) {
	seen := map[string]bool{}
references:
	for _, w := range out.Workflows {
		if w.BindingSnapshot == nil {
			continue
		}
		for _, binding := range w.BindingSnapshot.Bindings {
			if binding.Kind != "agent" {
				continue
			}
			key := binding.RuntimeAgentID + "|" + binding.SkillVersion
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(out.AgentReleases) >= 20 {
				out.Blockers = append(out.Blockers, "Agent release observation exceeds the 20 distinct pin limit; remaining releases are unresolved.")
				break references
			}
			out.AgentReleases = append(out.AgentReleases, solutionAgentObservation{RuntimeAgentID: binding.RuntimeAgentID, ExpectedVersion: binding.SkillVersion, Reason: "Agent release evidence unavailable; original source access and release gates remain required."})
		}
	}
	if len(out.AgentReleases) == 0 {
		return
	}
	root, token, err := s.solutionAgentSource(ctx, p)
	if err != nil {
		return
	}
	remainingBytes := 64 << 10
	for i := range out.AgentReleases {
		observation := &out.AgentReleases[i]
		target := *root
		target.Path = singleJoin(target.Path, "/api/v1/releases/evidence")
		target.RawPath = ""
		target.Fragment = ""
		target.RawQuery = url.Values{"agent_key": {observation.RuntimeAgentID}, "skill_version": {observation.ExpectedVersion}}.Encode()
		evidence := readSourceAgentEvidence(ctx, client, target.String(), token, *observation)
		if evidence == nil {
			continue
		}
		raw, err := json.Marshal(evidence)
		if err != nil || len(raw) > remainingBytes {
			observation.Reason = "Agent release evidence exceeds the report metadata limit; the source remains unresolved."
			continue
		}
		remainingBytes -= len(raw)
		observation.Evidence = evidence
		observation.Reason = "Authoritative Agent metadata observed; contract compatibility, KMS/tool target permissions, Solution approval and installation remain separate gates."
	}
}

func (s *Server) solutionAgentSource(ctx context.Context, p principal.Principal) (*url.URL, string, error) {
	decision, err := s.engine.App(ctx, p.UserID, p.WorkspaceID, "agent-studio", observability.RequestIDFrom(ctx))
	if err != nil {
		return nil, "", err
	}
	if !decision.Allowed() {
		return nil, "", apperr.Forbidden("AGENT_SOURCE_READ_DENIED", "Current Agent Studio app access is required to read retained source governance evidence")
	}
	up, configured := s.upstreams.Get("agent-studio")
	if !configured || up.Mode != "proxy" {
		return nil, "", apperr.New(503, "AGENT_SOURCE_UNAVAILABLE", "Trusted Agent Studio authorization could not be verified")
	}
	root, err := url.Parse(up.URL)
	if err != nil || root.Host == "" {
		return nil, "", apperr.New(503, "AGENT_SOURCE_UNAVAILABLE", "Trusted Agent Studio configuration is invalid")
	}
	var workspaceKey string
	if err = s.pool.QueryRow(ctx, `SELECT key FROM workspaces WHERE id=$1::uuid AND tenant_id=$2::uuid`, p.WorkspaceID, p.TenantID).Scan(&workspaceKey); err != nil {
		return nil, "", err
	}
	token, _, err := s.tokens.Mint(up.Audience, integrations.Subject{UserID: p.UserID, TenantKey: p.TenantKey, WorkspaceKey: workspaceKey, IdPIssuer: p.Issuer, IdPSubject: p.Subject, PrincipalType: principal.TypeHuman, SessionID: p.SessionID, Scopes: appScopes(decision), PolicyVersion: decision.PolicyVersion, CorrelationID: observability.RequestIDFrom(ctx), AuthTime: p.AuthTime})
	if err != nil {
		return nil, "", err
	}
	if !up.StripPrefix {
		root.Path = singleJoin(root.Path, "/apps/agent-studio")
	}
	root.RawPath = ""
	root.RawQuery = ""
	root.Fragment = ""
	return root, token, nil
}

// Retained source facts remain protected by the reader's current source grant, including replay.
func (s *Server) requireCandidateAgentRead(ctx context.Context, p principal.Principal, candidates []solutions.Candidate) error {
	needed := false
	for _, candidate := range candidates {
		var report struct {
			AgentReleases []struct {
				Evidence json.RawMessage `json:"evidence"`
			} `json:"agent_releases"`
		}
		if json.Unmarshal(candidate.Snapshot.Readiness, &report) != nil {
			needed = true
			break
		}
		for _, o := range report.AgentReleases {
			if len(o.Evidence) > 0 && string(o.Evidence) != "null" {
				needed = true
				break
			}
		}
		if needed {
			break
		}
	}
	if !needed {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	root, token, err := s.solutionAgentSource(ctx, p)
	if err != nil {
		return err
	}
	root.Path = singleJoin(root.Path, "/api/v1/releases")
	root.RawQuery = url.Values{"agent_key": {"elenio.publisher.authorization-check"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return apperr.New(503, "AGENT_SOURCE_UNAVAILABLE", "Current source governance authorization could not be verified")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return apperr.Forbidden("AGENT_SOURCE_READ_DENIED", "Original Agent Studio release.read permission is required to read retained governance evidence")
	}
	if response.StatusCode != http.StatusOK {
		return apperr.New(503, "AGENT_SOURCE_UNAVAILABLE", "Current source governance authorization could not be verified")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	return nil
}

func readSourceAgentEvidence(ctx context.Context, client *http.Client, target, token string, expected solutionAgentObservation) *sourceAgentEvidence {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		return nil
	}
	var e sourceAgentEvidence
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF || !validSourceAgentEvidence(e, expected) {
		return nil
	}
	return &e
}

func validSourceAgentEvidence(e sourceAgentEvidence, expected solutionAgentObservation) bool {
	if e.Schema != "elenio.agent-release-evidence.v1" || e.AgentKey != expected.RuntimeAgentID ||
		(e.Environment != "production" && e.Environment != "staging") || !sourceUUID.MatchString(e.ReleaseID) ||
		(e.State != "verified" && e.State != "blocked") || !workflowHash.MatchString(e.ContentHash) ||
		e.ObservedAt.IsZero() || time.Since(e.ObservedAt) > time.Minute || e.ObservedAt.After(time.Now().Add(time.Second)) ||
		e.Skills == nil || e.Tools == nil || e.Blockers == nil || len(e.Skills) > 100 || len(e.Tools) > 100 || len(e.Blockers) > 30 {
		return false
	}
	validPin := func(pin sourceDependencyPin) bool {
		return sourceUUID.MatchString(pin.ID) && len(pin.Version) <= 40 && sourceSemver.MatchString(pin.Version) &&
			(pin.Checksum == "" || workflowHash.MatchString(pin.Checksum)) && sourceCode.MatchString(pin.State) && (pin.ScopeHash == "" || workflowHash.MatchString(pin.ScopeHash))
	}
	if !validPin(e.AgentVersion) || e.AgentVersion.ScopeHash != "" {
		return false
	}
	if expected.ExpectedVersion != "" && expected.ExpectedVersion != "unpinned" && expected.ExpectedVersion != e.AgentVersion.ID && expected.ExpectedVersion != e.AgentVersion.Version {
		return false
	}
	if (e.Fingerprint != "" && !workflowHash.MatchString(e.Fingerprint)) || (e.RequestID != "" && !sourceUUID.MatchString(e.RequestID)) || (e.EvaluationID != "" && !sourceUUID.MatchString(e.EvaluationID)) ||
		(e.EvaluationGate != "" && e.EvaluationGate != "pass" && e.EvaluationGate != "blocked") {
		return false
	}
	for _, list := range [][]sourceDependencyPin{e.Skills, e.Tools} {
		seen := map[string]bool{}
		for _, pin := range list {
			if !validPin(pin) || seen[pin.ID] {
				return false
			}
			seen[pin.ID] = true
		}
	}
	for _, model := range []*sourceModelPin{e.PrimaryModel, e.FallbackModel} {
		if model != nil && (!sourceUUID.MatchString(model.ID) || model.ConfigVersion < 1) {
			return false
		}
	}
	for _, code := range e.Blockers {
		if !sourceCode.MatchString(code) {
			return false
		}
	}
	if (e.SelfApprovalExempt && e.IndependentApproval) || (e.IndependentApproval && e.RequestID == "") || (e.EvaluationPrimaryConfigMatches && e.PrimaryModel == nil) {
		return false
	}
	if e.State == "verified" {
		if len(e.Blockers) != 0 || e.AgentVersion.State != "published" || !workflowHash.MatchString(e.AgentVersion.Checksum) ||
			!workflowHash.MatchString(e.Fingerprint) || e.RequestID == "" || e.EvaluationID == "" || e.EvaluationGate != "pass" ||
			!e.IndependentApproval || e.SelfApprovalExempt || !e.EvaluationPrimaryConfigMatches || e.PrimaryModel == nil || !e.PrimaryModel.Approved || e.FallbackModel != nil {
			return false
		}
		for _, pin := range e.Skills {
			if pin.State != "published" || !workflowHash.MatchString(pin.Checksum) || pin.ScopeHash != "" {
				return false
			}
		}
		for _, pin := range e.Tools {
			if pin.State != "active" || !workflowHash.MatchString(pin.Checksum) || !workflowHash.MatchString(pin.ScopeHash) {
				return false
			}
		}
	} else if len(e.Blockers) == 0 {
		return false
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	delete(fields, "content_hash")
	delete(fields, "observed_at")
	raw, err = json.Marshal(fields)
	if err != nil {
		return false
	}
	hash, err := solutions.CanonicalSnapshotHash(raw)
	return err == nil && hash == e.ContentHash
}

func boundAgentReport(out *solutionReadiness) {
	raw, _ := json.Marshal(out)
	if len(raw) <= 180<<10 {
		return
	}
	for i := len(out.AgentReleases) - 1; i >= 0; i-- {
		if out.AgentReleases[i].Evidence == nil {
			continue
		}
		out.AgentReleases[i].Evidence = nil
		out.AgentReleases[i].Reason = "Agent release evidence exceeds the aggregate report limit; the source remains unresolved."
		raw, _ = json.Marshal(out)
		if len(raw) <= 180<<10 {
			return
		}
	}
	for i := len(out.Workflows) - 1; i >= 0; i-- {
		if out.Workflows[i].BindingSnapshot == nil {
			continue
		}
		out.Workflows[i].BindingSnapshot = nil
		out.Workflows[i].BindingReason = "Deployment binding evidence exceeds the aggregate report limit; it remains unresolved."
		raw, _ = json.Marshal(out)
		if len(raw) <= 180<<10 {
			return
		}
	}
	out.Blockers = append(out.Blockers, "Report metadata exceeds 180 KiB; omitted resources remain unresolved.")
	for i := len(out.Workflows) - 1; i >= 0; i-- {
		if len(out.Workflows[i].Resources) == 0 {
			continue
		}
		out.Workflows[i].Resources = []solutions.ResourceDependency{}
		out.Workflows[i].InventoryComplete = false
		raw, _ = json.Marshal(out)
		if len(raw) <= 180<<10 {
			return
		}
	}
}
