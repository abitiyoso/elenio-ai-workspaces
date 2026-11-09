// Package observability provides structured logging, request correlation and Prometheus metrics.
package observability

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/elenio/control-plane/internal/platform/secrets"
)

func NewLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l, ReplaceAttr: redactAttr}))
}

var sensitiveKeys = []string{"token", "secret", "password", "cookie", "authorization", "code_verifier", "refresh"}

// redactAttr strips values whose key suggests a credential, as defence in depth: callers must not log them.
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	k := strings.ToLower(a.Key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return slog.String(a.Key, "[redacted]")
		}
	}
	return a
}

type ctxKey struct{}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// RequestID returns a sanitized inbound X-Request-ID or a fresh one.
func RequestID(inbound string) string {
	if requestIDPattern.MatchString(inbound) {
		return inbound
	}
	return secrets.RandomToken(12)
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(ctxKey{}).(string); ok {
		return id
	}
	return "none"
}

// Metrics holds the platform counters and histograms exported on the metrics listener.
type Metrics struct {
	Registry      *prometheus.Registry
	HTTPRequests  *prometheus.CounterVec
	HTTPDuration  *prometheus.HistogramVec
	AuthEvents    *prometheus.CounterVec
	AuthzDecision *prometheus.CounterVec
	ActiveSession prometheus.Gauge
	IdPUp         prometheus.Gauge
	AppHealth     *prometheus.GaugeVec
	ProxyRequests *prometheus.CounterVec
	RateLimited   *prometheus.CounterVec
	TokenExchange *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	r := prometheus.NewRegistry()
	m := &Metrics{
		Registry:      r,
		HTTPRequests:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cp_http_requests_total", Help: "HTTP requests by route and status class."}, []string{"route", "code"}),
		HTTPDuration:  prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cp_http_request_duration_seconds", Help: "HTTP latency by route.", Buckets: prometheus.DefBuckets}, []string{"route"}),
		AuthEvents:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cp_auth_events_total", Help: "Login/callback/logout outcomes."}, []string{"event", "outcome"}),
		AuthzDecision: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cp_authz_decisions_total", Help: "Authorization decisions by effect and reason."}, []string{"effect", "reason"}),
		ActiveSession: prometheus.NewGauge(prometheus.GaugeOpts{Name: "cp_active_sessions", Help: "Non-revoked, non-expired sessions."}),
		IdPUp:         prometheus.NewGauge(prometheus.GaugeOpts{Name: "cp_idp_up", Help: "1 when OIDC discovery succeeded recently."}),
		AppHealth:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cp_app_upstream_healthy", Help: "1 when the mini-app health probe succeeded."}, []string{"app"}),
		ProxyRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cp_app_proxy_requests_total", Help: "Mini-app route requests by app and outcome."}, []string{"app", "outcome"}),
		RateLimited:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cp_rate_limited_total", Help: "Requests rejected by rate limiting."}, []string{"bucket"}),
		TokenExchange: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cp_token_exchanges_total", Help: "Delegated token exchanges by client, audience and outcome."}, []string{"client", "audience", "outcome"}),
	}
	r.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.AuthEvents, m.AuthzDecision, m.ActiveSession, m.IdPUp, m.AppHealth, m.ProxyRequests, m.RateLimited, m.TokenExchange)
	return m
}
