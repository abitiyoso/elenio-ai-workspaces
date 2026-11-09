// Package audit records workspace audit events (actor, action, resource, correlation id, redacted
// metadata). Failures are logged and never fail the user's request.
package audit

import (
	"context"
	"log/slog"

	"github.com/elenio/case-workspace/internal/reqctx"
	"github.com/elenio/case-workspace/internal/store"
)

// Sink stores one event.
type Sink interface {
	Audit(ctx context.Context, e store.AuditEvent) error
}

type Recorder struct {
	Sink   Sink
	Tenant string
	Log    *slog.Logger
}

// Record writes an event; metadata must already be redacted (ids, counts, codes; never content).
func (r *Recorder) Record(ctx context.Context, actor, action, resource string, metadata map[string]any) {
	if r == nil || r.Sink == nil {
		return
	}
	if actor == "" {
		actor = "system"
	}
	err := r.Sink.Audit(context.WithoutCancel(ctx), store.AuditEvent{TenantKey: r.Tenant, ActorRef: actor, Action: action, ResourceRef: resource,
		CorrelationID: reqctx.ID(ctx), Metadata: metadata})
	if err != nil && r.Log != nil {
		r.Log.WarnContext(ctx, "audit write failed", "action", action, "resource", resource, "error", err.Error())
	}
}
