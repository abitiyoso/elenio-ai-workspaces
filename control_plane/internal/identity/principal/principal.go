// Package principal holds the verified caller resolved by the session middleware. It is built only from
// the server-side session and directory; request headers, bodies and query strings never contribute.
package principal

import (
	"context"
	"time"
)

const (
	TypeHuman   = "human"
	TypeService = "service"
)

type Principal struct {
	UserID        string
	TenantID      string
	TenantKey     string
	TenantName    string
	DisplayName   string
	Email         string
	PrincipalType string
	Issuer        string
	Subject       string
	SessionID     string // internal session row id; never the cookie value
	WorkspaceID   string // active workspace stored server-side on the session ("" if none)
	AuthTime      time.Time
	IdleExpiresAt time.Time
	AbsExpiresAt  time.Time
}

func (p Principal) IsHuman() bool { return p.PrincipalType == TypeHuman }

type ctxKey struct{}

func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}
