// Package ports defines boundaries that infrastructure adapters must satisfy.
package ports

import (
	"context"
	"net/http"
)

// Principal is created only by a server-side AuthAdapter after it verifies a
// trusted host or service credential. Request-supplied tenant, role, and user
// headers are not identity evidence.
type Principal struct {
	Issuer    string
	Subject   string
	TenantID  string
	Workspace string
	Kind      string
}

// AuthAdapter verifies the host's credential and resolves current tenant and
// workspace membership. Its implementation must fail closed when those
// checks cannot be performed; token format and claims remain host-owned.
type AuthAdapter interface {
	VerifyRequest(context.Context, *http.Request) (Principal, error)
}

// Authorizer performs action-level checks after identity verification.
type Authorizer interface {
	Authorize(context.Context, Principal, string, string) error
}

// SecretVault stores provider credentials outside the Agent Studio database.
// References may be persisted; secret material may only be returned to a
// backend adapter that needs it and must never be serialized to a client.
type SecretVault interface {
	Put(context.Context, string, []byte) (string, error)
	Resolve(context.Context, string) ([]byte, error)
	Revoke(context.Context, string) error
}
