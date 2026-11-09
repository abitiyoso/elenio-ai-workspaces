// Package idempotency claims client-supplied Idempotency-Keys inside the same transaction as the
// command they protect, so a retried request either replays the original result or is rejected.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
)

var (
	ErrKeyRequired = errors.New("Idempotency-Key header (8-128 characters) is required")
	ErrReused      = errors.New("idempotency key was used for a different request")
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// Key identifies one command; Hash fingerprints its request body.
type Key struct {
	Key  string
	Hash string
}

// RequestHash fingerprints the parts of a command request.
func RequestHash(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Claim registers the key. It returns (ref, true) when the same request already completed.
func Claim(ctx context.Context, tx pgx.Tx, tenantID, userID, scope string, k Key) (string, bool, error) {
	if !keyPattern.MatchString(k.Key) {
		return "", false, ErrKeyRequired
	}
	tag, err := tx.Exec(ctx, `INSERT INTO idempotency_keys (tenant_id, user_id, scope, key, request_hash) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING`, tenantID, userID, scope, k.Key, k.Hash)
	if err != nil {
		return "", false, err
	}
	if tag.RowsAffected() == 1 {
		return "", false, nil
	}
	var hash string
	var ref *string
	if err := tx.QueryRow(ctx, `SELECT request_hash, resource_ref FROM idempotency_keys WHERE tenant_id = $1 AND user_id = $2 AND scope = $3 AND key = $4`,
		tenantID, userID, scope, k.Key).Scan(&hash, &ref); err != nil {
		return "", false, err
	}
	if hash != k.Hash || ref == nil {
		return "", false, ErrReused
	}
	return *ref, true, nil
}

// Complete records the resource the command produced, for replays.
func Complete(ctx context.Context, tx pgx.Tx, tenantID, userID, scope, key, ref string) error {
	_, err := tx.Exec(ctx, `UPDATE idempotency_keys SET resource_ref = $5 WHERE tenant_id = $1 AND user_id = $2 AND scope = $3 AND key = $4`,
		tenantID, userID, scope, key, ref)
	return err
}
