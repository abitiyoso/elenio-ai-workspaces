package observability

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerRedactsSensitiveAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelInfo, "test")
	logger.Info("call", "authorization", "Bearer abc.def.ghi", "access_token", "xyz", "client_secret", "s3", "path", "/api/v1/session")
	out := buf.String()
	for _, secret := range []string{"abc.def.ghi", "xyz", "s3"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %q leaked into log: %s", secret, out)
		}
	}
	if !strings.Contains(out, "/api/v1/session") || !strings.Contains(out, Redacted) {
		t.Fatalf("expected non-sensitive attribute and redaction marker: %s", out)
	}
}

func TestAcceptTraceID(t *testing.T) {
	if got := AcceptTraceID("host-trace-12345"); got != "host-trace-12345" {
		t.Fatalf("well-formed id should pass through, got %q", got)
	}
	for _, bad := range []string{"", "short", "has space in it", strings.Repeat("a", 200), "inject\nline"} {
		if got := AcceptTraceID(bad); got == bad || len(got) != 32 {
			t.Fatalf("malformed id %q should be replaced, got %q", bad, got)
		}
	}
}
