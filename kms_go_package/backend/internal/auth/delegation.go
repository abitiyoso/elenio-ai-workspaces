package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	// ScopeRetrievalQuery is held by every retrieval service client.
	ScopeRetrievalQuery = "retrieval:query"
	// ScopeRetrievalDelegate lets a retrieval service client name the human it acts for with
	// DelegatedUserHeader instead of forwarding that human's session token.
	ScopeRetrievalDelegate = "retrieval:delegate"
	// DelegatedUserHeader carries the delegated principal id (the Control Plane user UUID, which is also
	// tenant_principals.principal_id). It is honoured only on the retrieval endpoint.
	DelegatedUserHeader = "X-KMS-Delegated-User"
)

// DelegationRejection is a definitive refusal of a delegated-user request. Reason is a stable code that is
// safe to log and audit; it never contains the header value.
type DelegationRejection struct{ Reason string }

func (e *DelegationRejection) Error() string { return "delegated user rejected: " + e.Reason }

// HasScope reports whether the service client holds scope.
func HasScope(identity ServiceIdentity, scope string) bool { return hasScope(identity, scope) }

// ResolveDelegatedUser resolves the human a retrieval service client acts for from DelegatedUserHeader.
//
// When the header is absent it returns delegated=false and the caller must authenticate the human some other
// way. When present, the client must hold ScopeRetrievalDelegate, the header must carry exactly one UUID, and
// that principal must have an active tenant_principals row in the service client's own tenant; roles come from
// that row. A refusal is a *DelegationRejection; a directory failure wraps ErrVerifierUnavailable.
func ResolveDelegatedUser(ctx context.Context, r *http.Request, service ServiceIdentity, directory PrincipalDirectory) (Identity, bool, error) {
	values := r.Header.Values(DelegatedUserHeader)
	if len(values) == 0 {
		return Identity{}, false, nil
	}
	if !hasScope(service, ScopeRetrievalDelegate) {
		return Identity{}, true, &DelegationRejection{Reason: "delegation_not_permitted"}
	}
	if len(values) != 1 {
		return Identity{}, true, &DelegationRejection{Reason: "delegated_user_repeated"}
	}
	principalID := strings.ToLower(strings.TrimSpace(values[0]))
	if !isUUID(principalID) {
		return Identity{}, true, &DelegationRejection{Reason: "delegated_user_not_uuid"}
	}
	if service.TenantID == "" {
		return Identity{}, true, &DelegationRejection{Reason: "service_tenant_missing"}
	}
	if directory == nil {
		return Identity{}, true, fmt.Errorf("%w: principal directory is not configured", ErrVerifierUnavailable)
	}
	record, err := directory.LookupPrincipal(ctx, service.TenantID, principalID)
	if errors.Is(err, ErrPrincipalNotProvisioned) {
		return Identity{}, true, &DelegationRejection{Reason: "principal_not_provisioned"}
	}
	if err != nil {
		return Identity{}, true, fmt.Errorf("%w: principal directory lookup failed: %v", ErrVerifierUnavailable, err)
	}
	if record.Status != principalStatusActive {
		return Identity{}, true, &DelegationRejection{Reason: "principal_disabled"}
	}
	roles := make([]string, 0, len(record.Roles))
	roles = append(roles, record.Roles...)
	return Identity{UserID: principalID, TenantID: service.TenantID, Roles: roles}, true, nil
}
