package main

import (
	"context"
	"testing"
)

func TestDiagnosticCredentialCannotResolveAnotherProviderOrReference(t *testing.T) {
	vault := diagnosticCredential{key: []byte("synthetic-test-credential")}
	for _, binding := range [][2]string{
		{"diagnostic:nine-router", "provider:other-provider"},
		{"other-reference", "provider:nine-router-diagnostic"},
		{"diagnostic:nine-router", ""},
	} {
		if secret, err := vault.ResolveScoped(context.Background(), binding[0], binding[1]); err == nil || len(secret) != 0 {
			t.Fatal("diagnostic credential escaped its exact provider binding")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if secret, err := vault.ResolveScoped(ctx, "diagnostic:nine-router", "provider:nine-router-diagnostic"); err == nil || len(secret) != 0 {
		t.Fatal("cancelled diagnostic resolved a credential")
	}
}

func TestExecutorCanClearResolvedCredentialWithoutMutatingOwner(t *testing.T) {
	vault := diagnosticCredential{key: []byte("synthetic-test-credential")}
	resolved, err := vault.ResolveScoped(context.Background(), "diagnostic:nine-router", "provider:nine-router-diagnostic")
	if err != nil {
		t.Fatal(err)
	}
	clear(resolved)
	if string(vault.key) != "synthetic-test-credential" {
		t.Fatal("resolved credential shared mutable backing storage")
	}
}
