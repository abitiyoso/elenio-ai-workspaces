// Package audit appends redacted, actor-attributed platform events and transactional outbox messages.
// The audit table is append-only (enforced by a database trigger).
package audit

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/elenio/control-plane/internal/platform/observability"
	"github.com/elenio/control-plane/internal/platform/postgres"
)

type Event struct {
	ActorUserID   string // empty for system events
	ActorType     string // human | service | system
	ServiceID     string
	TenantID      string
	WorkspaceID   string
	Action        string
	TargetType    string
	TargetID      string
	Decision      string // allow | deny | ""
	ReasonCode    string
	PolicyVersion string
	Rationale     string
	Metadata      map[string]any
}

var sensitive = []string{"token", "secret", "password", "cookie", "authorization", "verifier", "code", "nonce", "state"}

// Redact removes credential-like keys recursively. Callers should never pass credentials; this is a backstop.
func Redact(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		redacted := false
		for _, s := range sensitive {
			if strings.Contains(lk, s) && lk != "reason_code" {
				redacted = true
				break
			}
		}
		if redacted {
			out[k] = "[redacted]"
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = Redact(nested)
			continue
		}
		out[k] = v
	}
	return out
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Record writes one event using q (pass a transaction to make the audit atomic with the change).
func Record(ctx context.Context, q postgres.Querier, e Event) error {
	if e.ActorType == "" {
		e.ActorType = "human"
		if e.ActorUserID == "" {
			e.ActorType = "system"
		}
	}
	meta, err := json.Marshal(Redact(e.Metadata))
	if err != nil {
		meta = []byte(`{}`)
	}
	_, err = q.Exec(ctx, `
INSERT INTO platform_audit_events (actor_user_id, actor_type, service_id, tenant_id, workspace_id, action, target_type,
    target_id, decision, reason_code, policy_version, correlation_id, rationale, metadata)
VALUES ($1::uuid, $2, $3, $4::uuid, $5::uuid, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		nullable(e.ActorUserID), e.ActorType, nullable(e.ServiceID), nullable(e.TenantID), nullable(e.WorkspaceID),
		e.Action, e.TargetType, e.TargetID, nullable(e.Decision), nullable(e.ReasonCode), nullable(e.PolicyVersion),
		observability.RequestIDFrom(ctx), nullable(e.Rationale), meta)
	return err
}

// Enqueue writes a transactional outbox event for downstream propagation (dedupe key makes it idempotent).
func Enqueue(ctx context.Context, q postgres.Querier, eventType, dedupeKey string, payload map[string]any) error {
	body, err := json.Marshal(Redact(payload))
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO outbox_events (event_type, payload, dedupe_key) VALUES ($1, $2, $3) ON CONFLICT (dedupe_key) DO NOTHING`,
		eventType, body, dedupeKey)
	return err
}
