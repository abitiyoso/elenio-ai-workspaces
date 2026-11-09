// Package reqctx carries the request (correlation) id through contexts and onto upstream calls.
package reqctx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

type key struct{}

var accepted = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// Accept returns the client-supplied id when it is a safe token, otherwise a fresh random id.
func Accept(v string) string {
	if accepted.MatchString(v) {
		return v
	}
	return New()
}

func New() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func With(ctx context.Context, id string) context.Context { return context.WithValue(ctx, key{}, id) }

func ID(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}
