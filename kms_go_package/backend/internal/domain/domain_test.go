package domain

import "testing"

func TestRequireTenant(t *testing.T) {
	if err := RequireTenant("tenant-a", "tenant-a"); err != nil {
		t.Fatalf("same tenant rejected: %v", err)
	}
	if err := RequireTenant("tenant-a", "tenant-b"); err != ErrTenantMismatch {
		t.Fatalf("cross tenant err = %v", err)
	}
	if err := RequireTenant("", "tenant-a"); err != ErrTenantMismatch {
		t.Fatalf("empty context err = %v", err)
	}
}
