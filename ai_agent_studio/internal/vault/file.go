package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	fileMagic    = "ASV1"
	refPrefix    = "filevault://"
	masterKeyLen = 32
	maxSecretLen = 64 << 10
)

// FileVault encrypts each credential with AES-256-GCM before writing it to a
// private directory. The 32-byte master key must be provisioned separately.
type FileVault struct {
	directory string
	key       []byte
}

// NewFileVault loads an existing raw 32-byte key. Key files must be regular,
// non-symlink files readable by the service and inaccessible to group/others.
// It never creates or rotates the master key.
func NewFileVault(directory, masterKeyFile string) (*FileVault, error) {
	directory = strings.TrimSpace(directory)
	masterKeyFile = strings.TrimSpace(masterKeyFile)
	if directory == "" || masterKeyFile == "" {
		return nil, errors.New("vault directory and separate master key file are required")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve vault directory: %w", err)
	}
	keyPath, err := filepath.Abs(masterKeyFile)
	if err != nil {
		return nil, fmt.Errorf("resolve master key path: %w", err)
	}
	if relative, err := filepath.Rel(root, keyPath); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, errors.New("master key file must be stored outside the vault directory")
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, err
	}
	key, err := readMasterKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &FileVault{directory: root, key: key}, nil
}

func (v *FileVault) Put(ctx context.Context, scope string, secret []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if v == nil || len(v.key) != masterKeyLen {
		return "", errors.New("credential vault is unavailable")
	}
	if strings.TrimSpace(scope) == "" || len(scope) > 255 || strings.ContainsAny(scope, "\r\n\x00") {
		return "", errors.New("secret scope is invalid")
	}
	if len(secret) == 0 || len(secret) > maxSecretLen {
		return "", errors.New("secret size is invalid")
	}

	aead, err := newAEAD(v.key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("create credential nonce: %w", err)
	}
	scopeBytes := []byte(scope)
	sealed := aead.Seal(nil, nonce, secret, scopeBytes)
	record := make([]byte, 0, len(fileMagic)+2+len(scopeBytes)+len(nonce)+len(sealed))
	record = append(record, fileMagic...)
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(scopeBytes)))
	record = append(record, length[:]...)
	record = append(record, scopeBytes...)
	record = append(record, nonce...)
	record = append(record, sealed...)

	idBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, idBytes); err != nil {
		return "", fmt.Errorf("create credential reference: %w", err)
	}
	id := hex.EncodeToString(idBytes)
	path := filepath.Join(v.directory, id)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create encrypted credential: %w", err)
	}
	if _, err := file.Write(record); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write encrypted credential: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("sync encrypted credential: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close encrypted credential: %w", err)
	}
	return refPrefix + id, nil
}

func (v *FileVault) Resolve(ctx context.Context, reference string) ([]byte, error) {
	return v.resolveScoped(ctx, reference, "")
}

// ResolveScoped verifies the provider binding stored as authenticated data.
// Callers should use this for provider credentials instead of Resolve.
func (v *FileVault) ResolveScoped(ctx context.Context, reference, expectedScope string) ([]byte, error) {
	if strings.TrimSpace(expectedScope) == "" {
		return nil, errors.New("expected secret scope is required")
	}
	return v.resolveScoped(ctx, reference, expectedScope)
}

func (v *FileVault) resolveScoped(ctx context.Context, reference, expectedScope string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if v == nil || len(v.key) != masterKeyLen {
		return nil, errors.New("credential vault is unavailable")
	}
	id := strings.TrimPrefix(reference, refPrefix)
	if id == reference || len(id) != 64 {
		return nil, errors.New("credential reference is invalid")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return nil, errors.New("credential reference is invalid")
	}
	record, err := os.ReadFile(filepath.Join(v.directory, id))
	if err != nil {
		return nil, fmt.Errorf("read encrypted credential: %w", err)
	}
	if len(record) < len(fileMagic)+2 || string(record[:len(fileMagic)]) != fileMagic {
		return nil, errors.New("encrypted credential has an invalid format")
	}
	scopeLength := int(binary.BigEndian.Uint16(record[len(fileMagic) : len(fileMagic)+2]))
	position := len(fileMagic) + 2
	if scopeLength == 0 || position+scopeLength > len(record) {
		return nil, errors.New("encrypted credential has an invalid scope")
	}
	scope := string(record[position : position+scopeLength])
	position += scopeLength
	if expectedScope != "" && scope != expectedScope {
		return nil, errors.New("credential scope does not match provider")
	}
	aead, err := newAEAD(v.key)
	if err != nil {
		return nil, err
	}
	if position+aead.NonceSize()+aead.Overhead() > len(record) {
		return nil, errors.New("encrypted credential is truncated")
	}
	nonce := record[position : position+aead.NonceSize()]
	ciphertext := record[position+aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(scope))
	if err != nil {
		return nil, errors.New("encrypted credential authentication failed")
	}
	return plaintext, nil
}

func (v *FileVault) Revoke(ctx context.Context, reference string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v == nil {
		return errors.New("credential vault is unavailable")
	}
	id := strings.TrimPrefix(reference, refPrefix)
	if id == reference || len(id) != 64 {
		return errors.New("credential reference is invalid")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return errors.New("credential reference is invalid")
	}
	if err := os.Remove(filepath.Join(v.directory, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("revoke encrypted credential: %w", err)
	}
	return nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize credential encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize credential authentication: %w", err)
	}
	return aead, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create vault directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect vault directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("vault directory must be a non-symlink private directory with mode 0700")
	}
	return nil
}

func readMasterKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect master key file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Mode().Perm()&0o400 == 0 {
		return nil, errors.New("master key must be a readable regular file with no group/other permissions")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open master key file: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, openedInfo) {
		return nil, errors.New("master key file changed while opening")
	}
	key, err := io.ReadAll(io.LimitReader(file, masterKeyLen+1))
	if err != nil {
		return nil, fmt.Errorf("read master key file: %w", err)
	}
	if len(key) != masterKeyLen {
		return nil, errors.New("master key file must contain exactly 32 raw bytes")
	}
	return key, nil
}
