// Package observability provides structured logging with secret redaction and trace id propagation.
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var sensitiveKeyParts = []string{"authorization", "token", "secret", "password", "cookie", "jwt", "api_key", "apikey", "credential", "signature"}

// IsSensitiveKey reports whether a log attribute or header name may carry a secret.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

// NewLogger returns a JSON logger that masks attributes whose key looks sensitive.
func NewLogger(w io.Writer, level slog.Level, service string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() != slog.KindGroup && IsSensitiveKey(a.Key) {
				return slog.String(a.Key, Redacted)
			}
			return a
		},
	})
	return slog.New(h).With("service", service)
}

type traceKey struct{}

var traceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)

// NewID returns a random 128-bit hex identifier.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// AcceptTraceID returns incoming if it is a well-formed id, otherwise a fresh id.
func AcceptTraceID(incoming string) string {
	if traceIDPattern.MatchString(incoming) {
		return incoming
	}
	return NewID()
}

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

func TraceID(ctx context.Context) string {
	id, _ := ctx.Value(traceKey{}).(string)
	return id
}
