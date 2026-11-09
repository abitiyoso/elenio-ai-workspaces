package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elenio/control-plane/internal/audit"
	"github.com/elenio/control-plane/internal/authorization"
	"github.com/elenio/control-plane/internal/identity/principal"
	"github.com/elenio/control-plane/internal/integrations"
	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/platform/secrets"
)

// Delegated token exchange (ADR-004). An experience app's BFF (the "client") presents the Control Plane token it
// received through /apps/<client>/ and gets a short-lived token for one allowlisted downstream audience, for the
// same user, session and workspace. The exchange re-runs the same app-open decision the proxy runs for the
// target app, so it can never grant more than the user would get by opening that app directly.

const (
	grantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	tokenTypeJWT           = "urn:ietf:params:oauth:token-type:jwt"
	tokenTypeAccessToken   = "urn:ietf:params:oauth:token-type:access_token"
	// maxExchangedTokenTTL caps exchanged tokens below the issuer TTL (SERVICE_TOKEN_TTL, itself <= 15m).
	maxExchangedTokenTTL = 5 * time.Minute
	// Error codes specific to the exchange (401 = client or subject token not acceptable, 403 = policy).
	codeInvalidClient          = "INVALID_CLIENT"
	codeInvalidRequest         = "INVALID_REQUEST"
	codeAudienceNotAllowed     = "AUDIENCE_NOT_ALLOWED"
	codeAudienceNotConfigured  = "AUDIENCE_NOT_CONFIGURED"
	codeInvalidSubjectToken    = "INVALID_SUBJECT_TOKEN"
	codeSubjectAudienceInvalid = "SUBJECT_TOKEN_AUDIENCE_MISMATCH"
	codeSubjectTokenDelegated  = "SUBJECT_TOKEN_DELEGATED"
	codeSubjectNotHuman        = "SUBJECT_NOT_HUMAN"
	codeSessionInactive        = "SESSION_INACTIVE"
	codeSessionWorkspace       = "SESSION_WORKSPACE_CHANGED"
	codeSubjectInactive        = "SUBJECT_INACTIVE"
	codeClientAppDenied        = "CLIENT_APP_DENIED"
	codeTargetAppDenied        = "TARGET_APP_DENIED"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// unknownClientHash keeps the unknown-client path doing the same constant-time comparison work.
var unknownClientHash = hex.EncodeToString(secrets.Hash(secrets.RandomToken(32)))

type exchangeRequest struct {
	GrantType          string `json:"grant_type"`
	SubjectToken       string `json:"subject_token"`
	SubjectTokenType   string `json:"subject_token_type"`
	Audience           string `json:"audience"`
	RequestedTokenType string `json:"requested_token_type"`
}

type exchangeResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	IssuedTokenType string `json:"issued_token_type"`
}

// exchangeTrail accumulates what is known about one exchange for the audit row and metric labels.
type exchangeTrail struct {
	clientID      string // authenticated client id ("" until authenticated)
	claimedClient string // sanitized client id presented in Basic auth (audit only)
	audience      string // requested audience, once known to be allowlisted for the client
	rawAudience   string
	userID        string
	workspaceID   string
	policyVersion string
}

func (t *exchangeTrail) metricLabels() (string, string) {
	client, aud := "unknown", "other"
	if t.clientID != "" {
		client = t.clientID
	}
	if t.audience != "" {
		aud = t.audience
	}
	return client, aud
}

func (s *Server) exchangeMetric(t *exchangeTrail, outcome string) {
	if s.metrics != nil {
		client, aud := t.metricLabels()
		s.metrics.TokenExchange.WithLabelValues(client, aud, outcome).Inc()
	}
}

func (s *Server) auditExchange(ctx context.Context, t *exchangeTrail, allowed bool, reason string, extra map[string]any) error {
	meta := map[string]any{"client_id": t.clientID, "reason_code": reason}
	if t.clientID == "" {
		meta["client_id"] = t.claimedClient
		meta["client_authenticated"] = false
	}
	for k, v := range extra {
		meta[k] = v
	}
	target := t.audience
	if target == "" {
		target = clip(t.rawAudience, 64)
	}
	if target == "" {
		target = "unknown"
	}
	ev := audit.Event{ActorUserID: t.userID, ActorType: "service", ServiceID: t.clientID, TenantID: s.tenantID, WorkspaceID: t.workspaceID,
		Action: "token.exchange_denied", TargetType: "token_audience", TargetID: target, Decision: "deny", ReasonCode: reason,
		PolicyVersion: t.policyVersion, Metadata: meta}
	if allowed {
		ev.Action, ev.Decision = "token.exchanged", "allow"
	}
	return audit.Record(ctx, s.pool, ev)
}

// handleTokenExchange implements POST /api/v1/token-exchange. It is server-to-server: no cookie is read, so the
// cookie CSRF/Origin checks do not apply; client authentication is HTTP Basic with a deployment-configured secret.
func (s *Server) handleTokenExchange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := &exchangeTrail{}
	deny := func(status int, code, reason, msg string) {
		if reason == "" {
			reason = code
		}
		if err := s.auditExchange(ctx, t, false, reason, nil); err != nil {
			s.log.Error("token_exchange_audit_failed", "request_id", observability.RequestIDFrom(ctx), "error", err.Error())
		}
		s.exchangeMetric(t, "denied_"+codeOutcome(status))
		s.log.Info("token_exchange_denied", "request_id", observability.RequestIDFrom(ctx), "client", t.clientID, "audience", t.audience,
			"code", code, "reason", reason)
		if status == http.StatusUnauthorized && t.clientID == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="control-plane-token-exchange"`)
		}
		writeError(w, r, status, code, msg)
	}

	// 1. Client authentication (constant-time over sha256 of the presented secret).
	id, secret, hasBasic := r.BasicAuth()
	if appRouteKeyPattern.MatchString(id) {
		t.claimedClient = id
	}
	client, known := s.cfg.TokenExchangeClients[id]
	want := unknownClientHash
	if known {
		want = client.SecretSHA256
	}
	sum := sha256.Sum256([]byte(secret))
	secretOK := secrets.Equal(hex.EncodeToString(sum[:]), want)
	if !hasBasic || !known || secret == "" || !secretOK {
		if s.limited(w, r, s.authLimit, "token_exchange_client", "txc:"+clientIP(r)) {
			return
		}
		deny(http.StatusUnauthorized, codeInvalidClient, "", "client authentication failed")
		return
	}
	t.clientID = id

	// 2. Request shape (loosely RFC 8693; optional type fields must name a JWT exchange when present).
	var in exchangeRequest
	if err := decode(r, &in); err != nil {
		deny(http.StatusBadRequest, codeInvalidRequest, "INVALID_BODY", "request body must be a JSON object with subject_token and audience")
		return
	}
	t.rawAudience = in.Audience
	if in.SubjectToken == "" || in.Audience == "" ||
		(in.GrantType != "" && in.GrantType != grantTypeTokenExchange) ||
		(in.SubjectTokenType != "" && in.SubjectTokenType != tokenTypeJWT && in.SubjectTokenType != tokenTypeAccessToken) ||
		(in.RequestedTokenType != "" && in.RequestedTokenType != tokenTypeJWT) {
		deny(http.StatusBadRequest, codeInvalidRequest, "", "subject_token and audience are required; only JWT token exchange is supported")
		return
	}

	// 3. Audience must be allowlisted for this client and have a trusted upstream with an audience.
	allowed := false
	for _, a := range client.Audiences {
		if a == in.Audience {
			allowed = true
			break
		}
	}
	if !allowed {
		deny(http.StatusForbidden, codeAudienceNotAllowed, "", "audience is not allowed for this client")
		return
	}
	t.audience = in.Audience
	targetUp, ok := s.upstreams.Get(in.Audience)
	if !ok || targetUp.Mode != "proxy" || targetUp.Audience == "" {
		deny(http.StatusForbidden, codeAudienceNotConfigured, "", "audience has no trusted upstream in this deployment")
		return
	}

	// 4. Subject token: our own signature (current or previous kid), exact issuer, lifetime, minted for this client.
	v, err := s.tokens.Verify(in.SubjectToken)
	if err != nil {
		deny(http.StatusUnauthorized, codeInvalidSubjectToken, "", "subject token is not a valid Control Plane token")
		return
	}
	c := v.Claims
	if aud, _ := c.GetAudience(); len(aud) != 1 || aud[0] != t.clientID {
		deny(http.StatusUnauthorized, codeSubjectAudienceInvalid, "", "subject token was not issued to this client")
		return
	}
	if _, has := c["act"]; has {
		deny(http.StatusUnauthorized, codeSubjectTokenDelegated, "", "delegated (exchanged) tokens cannot be exchanged again")
		return
	}
	if pt, _ := c["principal_type"].(string); pt != principal.TypeHuman {
		deny(http.StatusUnauthorized, codeSubjectNotHuman, "", "only human user tokens can be exchanged")
		return
	}
	sub, _ := c.GetSubject()
	tenantKey, _ := c["tenant_id"].(string)
	wsKey, _ := c["workspace_id"].(string)
	sid, _ := c["sid"].(string)
	ctxVer, _ := c["ctx_ver"].(string)
	if !uuidPattern.MatchString(sub) || tenantKey != s.cfg.TenantKey || wsKey == "" || sid == "" || ctxVer != integrations.ContextVersion {
		deny(http.StatusUnauthorized, codeInvalidSubjectToken, "", "subject token lacks the required identity claims")
		return
	}
	t.userID = sub

	// 5. Live state: the session behind the token must still be active, in the same workspace, and the user active.
	sess, err := s.findExchangeSession(ctx, sub, v.KeyID, sid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if sess == nil {
		deny(http.StatusUnauthorized, codeSessionInactive, "", "the session behind the subject token is no longer active")
		return
	}
	if sess.workspaceKey == "" || sess.workspaceKey != wsKey {
		deny(http.StatusUnauthorized, codeSessionWorkspace, "", "the session's active workspace changed; obtain a fresh subject token")
		return
	}
	t.workspaceID = sess.workspaceID
	var idpSubject, userStatus, tenantStatus, principalType string
	err = s.pool.QueryRow(ctx, `SELECT u.idp_subject, u.status, t.status, u.principal_type FROM users u JOIN tenants t ON t.id = u.tenant_id
WHERE u.id = $1::uuid AND u.tenant_id = $2::uuid`, sub, s.tenantID).Scan(&idpSubject, &userStatus, &tenantStatus, &principalType)
	if errors.Is(err, pgx.ErrNoRows) {
		deny(http.StatusUnauthorized, codeSubjectInactive, "", "the subject is not an active user")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if userStatus != "active" || tenantStatus != "active" || principalType != principal.TypeHuman {
		deny(http.StatusUnauthorized, codeSubjectInactive, authorization.ReasonSubjectDisabled, "the subject is not an active user")
		return
	}
	if s.limited(w, r, s.apiLimit, "token_exchange", "tx:"+t.clientID+":"+sess.id) {
		return
	}

	// 6. Policy: the user must still be allowed to open the client app and the target app in this workspace
	// (the same decision the /apps/<key>/ route makes on every request).
	cid := observability.RequestIDFrom(ctx)
	var target authorization.Decision
	for _, check := range []struct{ app, code, msg string }{
		{t.clientID, codeClientAppDenied, "the user may no longer open the requesting app in this workspace"},
		{t.audience, codeTargetAppDenied, "the user may not open the requested app in this workspace"},
	} {
		d, err := s.engine.App(ctx, sub, sess.workspaceID, check.app, cid)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.countDecision(&d)
		t.policyVersion = d.PolicyVersion
		if !d.Allowed() {
			deny(http.StatusForbidden, check.code, d.ReasonCode, check.msg+" ("+d.ReasonCode+")")
			return
		}
		target = d
	}

	// 7. Mint: same subject/tenant/workspace/session/auth_time; scopes from the target decision; actor = client.
	ttl := maxExchangedTokenTTL
	if is := s.tokens.TTL(); is < ttl {
		ttl = is
	}
	token, exp, err := s.tokens.Mint(targetUp.Audience, integrations.Subject{UserID: sub, TenantKey: tenantKey, WorkspaceKey: wsKey,
		IdPIssuer: sess.idpIssuer, IdPSubject: idpSubject, PrincipalType: principal.TypeHuman, SessionID: sess.id,
		Scopes: appScopes(target), PolicyVersion: target.PolicyVersion, CorrelationID: cid, AuthTime: sess.authTime,
		Actor: "app:" + t.clientID, MaxTTL: ttl})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Fail closed: no token leaves without its audit row.
	if err := s.auditExchange(ctx, t, true, target.ReasonCode, map[string]any{"issued_audience": targetUp.Audience,
		"expires_at": exp.UTC().Format(time.RFC3339), "decision_id": target.DecisionID}); err != nil {
		s.fail(w, r, err)
		return
	}
	s.exchangeMetric(t, "issued")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, exchangeResponse{AccessToken: token, TokenType: "Bearer",
		ExpiresIn: int(time.Until(exp).Round(time.Second).Seconds()), IssuedTokenType: tokenTypeJWT})
}

type exchangeSession struct {
	id, workspaceID, workspaceKey, idpIssuer string
	authTime                                 time.Time
}

// findExchangeSession matches the token's sid (HMAC of the session id under the verifying kid) against the
// user's live sessions. Revoked, idle-expired and absolutely-expired sessions never match.
func (s *Server) findExchangeSession(ctx context.Context, userID, kid, sid string) (*exchangeSession, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id::text, COALESCE(s.workspace_id::text, ''), COALESCE(w.key, ''), s.idp_issuer, s.auth_time
FROM sessions s LEFT JOIN workspaces w ON w.id = s.workspace_id
WHERE s.user_id = $1::uuid AND s.tenant_id = $2::uuid AND s.revoked_at IS NULL AND s.idle_expires_at > now() AND s.absolute_expires_at > now()`,
		userID, s.tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found *exchangeSession
	for rows.Next() {
		var es exchangeSession
		if err := rows.Scan(&es.id, &es.workspaceID, &es.workspaceKey, &es.idpIssuer, &es.authTime); err != nil {
			return nil, err
		}
		if secrets.Equal(integrations.SessionClaim(kid, es.id), sid) {
			found = &es
		}
	}
	return found, rows.Err()
}

func codeOutcome(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	}
	return "error"
}

var appRouteKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`)
