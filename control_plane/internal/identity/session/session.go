// Package session implements the server-side session store, pending authorization flows and CSRF tokens.
// The browser holds only a random 256-bit opaque value; PostgreSQL stores its SHA-256.
package session

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/control-plane/internal/platform/postgres"
	"github.com/elenio/control-plane/internal/platform/secrets"
)

var (
	ErrNoSession   = errors.New("no valid session")
	ErrFlowInvalid = errors.New("authorization flow invalid, expired or replayed")
)

// cookieValue matches exactly the format we issue (32 random bytes, base64url). Anything else is ignored.
var cookieValue = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func WellFormed(raw string) bool { return cookieValue.MatchString(raw) }

type Session struct {
	ID            string
	UserID        string
	TenantID      string
	WorkspaceID   string
	Issuer        string
	AuthTime      time.Time
	IdleExpiresAt time.Time
	AbsExpiresAt  time.Time
}

type Store struct {
	Pool    *pgxpool.Pool
	IdleTTL time.Duration
	AbsTTL  time.Duration
	FlowTTL time.Duration
	Sealer  *secrets.Sealer
	CSRFKey []byte
	Now     func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Create issues a brand-new session (fresh identifier on every login: no fixation). It returns the raw
// cookie value, which is never stored.
func (s *Store) Create(ctx context.Context, q postgres.Querier, userID, tenantID, workspaceID, issuer string, authTime time.Time, refreshToken, userAgent string) (string, Session, error) {
	raw := secrets.RandomToken(32)
	now := s.now()
	sess := Session{UserID: userID, TenantID: tenantID, WorkspaceID: workspaceID, Issuer: issuer, AuthTime: authTime,
		IdleExpiresAt: now.Add(s.IdleTTL), AbsExpiresAt: now.Add(s.AbsTTL)}
	var sealed []byte
	if refreshToken != "" && s.Sealer != nil {
		sealed = s.Sealer.Seal([]byte(refreshToken), "refresh:"+userID)
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	var ws *string
	if workspaceID != "" {
		ws = &workspaceID
	}
	err := q.QueryRow(ctx, `
INSERT INTO sessions (token_hash, user_id, tenant_id, workspace_id, idp_issuer, auth_time, created_at, last_seen_at,
    idle_expires_at, absolute_expires_at, refresh_token_enc, user_agent)
VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $7, $8, $9, $10, $11) RETURNING id::text`,
		secrets.Hash(raw), userID, tenantID, ws, issuer, authTime, now, sess.IdleExpiresAt, sess.AbsExpiresAt, sealed, userAgent).Scan(&sess.ID)
	if err != nil {
		return "", Session{}, err
	}
	return raw, sess, nil
}

// Lookup validates the cookie value: well-formed, known, not revoked, within idle and absolute expiry.
// The idle window slides (written at most once per minute to limit write amplification).
func (s *Store) Lookup(ctx context.Context, raw string) (Session, error) {
	if !WellFormed(raw) {
		return Session{}, ErrNoSession
	}
	now := s.now()
	var sess Session
	var ws *string
	var lastSeen time.Time
	err := s.Pool.QueryRow(ctx, `
SELECT id::text, user_id::text, tenant_id::text, workspace_id::text, idp_issuer, auth_time, idle_expires_at, absolute_expires_at, last_seen_at
FROM sessions WHERE token_hash = $1 AND revoked_at IS NULL AND idle_expires_at > $2 AND absolute_expires_at > $2`,
		secrets.Hash(raw), now).Scan(&sess.ID, &sess.UserID, &sess.TenantID, &ws, &sess.Issuer, &sess.AuthTime, &sess.IdleExpiresAt, &sess.AbsExpiresAt, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	if ws != nil {
		sess.WorkspaceID = *ws
	}
	if now.Sub(lastSeen) > time.Minute {
		idle := now.Add(s.IdleTTL)
		if idle.After(sess.AbsExpiresAt) {
			idle = sess.AbsExpiresAt
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2, idle_expires_at = $3 WHERE id = $1::uuid AND revoked_at IS NULL`, sess.ID, now, idle); err == nil {
			sess.IdleExpiresAt = idle
		}
	}
	return sess, nil
}

// Revoke ends one session and returns its sealed refresh token (for best-effort IdP revocation).
func (s *Store) Revoke(ctx context.Context, q postgres.Querier, id, reason string) (string, error) {
	var sealed []byte
	var userID string
	err := q.QueryRow(ctx, `WITH old AS (SELECT id, refresh_token_enc FROM sessions WHERE id = $1::uuid AND revoked_at IS NULL FOR UPDATE)
UPDATE sessions s SET revoked_at = now(), revoke_reason = $2, refresh_token_enc = NULL FROM old
WHERE s.id = old.id RETURNING s.user_id::text, old.refresh_token_enc`, id, reason).Scan(&userID, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil || sealed == nil || s.Sealer == nil {
		return "", err
	}
	plain, err := s.Sealer.Open(sealed, "refresh:"+userID)
	if err != nil {
		return "", nil
	}
	return string(plain), nil
}

// RevokeUser ends every live session of a user (suspension, security incident, admin action).
func (s *Store) RevokeUser(ctx context.Context, q postgres.Querier, userID, reason string) (int64, error) {
	tag, err := q.Exec(ctx, `UPDATE sessions SET revoked_at = now(), revoke_reason = $2, refresh_token_enc = NULL
WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID, reason)
	return tag.RowsAffected(), err
}

// ClearWorkspace drops the active workspace from a user's sessions in that workspace (membership removed).
func (s *Store) ClearWorkspace(ctx context.Context, q postgres.Querier, userID, workspaceID string) error {
	_, err := q.Exec(ctx, `UPDATE sessions SET workspace_id = NULL WHERE user_id = $1::uuid AND workspace_id = $2::uuid AND revoked_at IS NULL`, userID, workspaceID)
	return err
}

func (s *Store) SetWorkspace(ctx context.Context, q postgres.Querier, id, workspaceID string) error {
	_, err := q.Exec(ctx, `UPDATE sessions SET workspace_id = $2::uuid WHERE id = $1::uuid AND revoked_at IS NULL`, id, workspaceID)
	return err
}

func (s *Store) CountActive(ctx context.Context) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE revoked_at IS NULL AND idle_expires_at > now() AND absolute_expires_at > now()`).Scan(&n)
	return n, err
}

// Purge deletes long-expired sessions and auth flows (sessions are not audit records; audit lives elsewhere).
func (s *Store) Purge(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `DELETE FROM auth_flows WHERE expires_at < now() - interval '1 hour'`); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE absolute_expires_at < now() - interval '7 days'`)
	return err
}

// CSRFToken is bound to the session id with an HMAC so it needs no storage and dies with the session.
func (s *Store) CSRFToken(sessionID string) string { return secrets.HMAC(s.CSRFKey, "csrf:"+sessionID) }

func (s *Store) ValidCSRF(sessionID, presented string) bool {
	return presented != "" && secrets.Equal(s.CSRFToken(sessionID), presented)
}

// Flow is a pending authorization request.
type Flow struct {
	State    string
	Nonce    string
	Verifier string
	Binding  string // value of the short-lived browser-binding cookie (login CSRF protection)
	ReturnTo string
}

// BeginFlow stores state/nonce/PKCE verifier server-side. Only hashes of state and binding are stored; the
// verifier is sealed.
func (s *Store) BeginFlow(ctx context.Context, verifier, returnTo string) (Flow, error) {
	f := Flow{State: secrets.RandomToken(32), Nonce: secrets.RandomToken(32), Verifier: verifier,
		Binding: secrets.RandomToken(32), ReturnTo: returnTo}
	_, err := s.Pool.Exec(ctx, `INSERT INTO auth_flows (state_hash, binding_hash, nonce, code_verifier_enc, return_to, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)`, secrets.Hash(f.State), secrets.Hash(f.Binding), f.Nonce,
		s.Sealer.Seal([]byte(verifier), "pkce:"+f.State), returnTo, s.now().Add(s.FlowTTL))
	return f, err
}

// ConsumeFlow atomically marks the flow used (replays fail) and checks the browser binding cookie.
func (s *Store) ConsumeFlow(ctx context.Context, state, binding string) (Flow, error) {
	if !WellFormed(state) {
		return Flow{}, ErrFlowInvalid
	}
	var bindingHash, sealed []byte
	f := Flow{State: state}
	err := s.Pool.QueryRow(ctx, `UPDATE auth_flows SET consumed_at = now()
WHERE state_hash = $1 AND consumed_at IS NULL AND expires_at > $2
RETURNING binding_hash, nonce, code_verifier_enc, return_to`, secrets.Hash(state), s.now()).Scan(&bindingHash, &f.Nonce, &sealed, &f.ReturnTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return Flow{}, ErrFlowInvalid
	}
	if err != nil {
		return Flow{}, err
	}
	if !WellFormed(binding) || !secrets.Equal(string(secrets.Hash(binding)), string(bindingHash)) {
		return Flow{}, ErrFlowInvalid
	}
	v, err := s.Sealer.Open(sealed, "pkce:"+state)
	if err != nil {
		return Flow{}, ErrFlowInvalid
	}
	f.Verifier = string(v)
	return f, nil
}
