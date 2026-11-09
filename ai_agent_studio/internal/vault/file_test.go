package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileVaultEncryptsScopesAndRevokes(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, "master.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("k", masterKeyLen)), 0o600); err != nil {
		t.Fatal(err)
	}
	vault, err := NewFileVault(filepath.Join(root, "vault"), keyPath)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("provider-secret-value")
	reference, err := vault.Put(context.Background(), "provider:tenant-provider", secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reference, string(secret)) {
		t.Fatal("vault reference contains secret material")
	}
	entries, err := os.ReadDir(vault.directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one encrypted vault entry, entries=%d err=%v", len(entries), err)
	}
	ciphertext, err := os.ReadFile(filepath.Join(vault.directory, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ciphertext), string(secret)) {
		t.Fatal("credential was written in plaintext")
	}
	if _, err := vault.ResolveScoped(context.Background(), reference, "provider:another-provider"); err == nil {
		t.Fatal("credential resolved under another provider scope")
	}
	resolved, err := vault.ResolveScoped(context.Background(), reference, "provider:tenant-provider")
	if err != nil {
		t.Fatal(err)
	}
	if string(resolved) != string(secret) {
		t.Fatal("resolved credential does not match stored credential")
	}
	clear(resolved)
	if err := vault.Revoke(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.ResolveScoped(context.Background(), reference, "provider:tenant-provider"); err == nil {
		t.Fatal("revoked credential still resolves")
	}
}

func TestFileVaultRejectsInsecureMasterKey(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, "master.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("k", masterKeyLen)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileVault(filepath.Join(root, "vault"), keyPath); err == nil {
		t.Fatal("insecure master key permissions were accepted")
	}
}
