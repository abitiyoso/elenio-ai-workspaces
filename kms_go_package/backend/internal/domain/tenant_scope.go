package domain

import "errors"

var ErrTenantMismatch = errors.New("tenant scope mismatch")

// RequireTenant prevents a service operation from acting outside the verified tenant context.
func RequireTenant(contextTenantID, resourceTenantID string) error {
	if contextTenantID == "" || resourceTenantID == "" || contextTenantID != resourceTenantID {
		return ErrTenantMismatch
	}
	return nil
}
