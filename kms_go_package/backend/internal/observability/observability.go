// Package observability carries per-request diagnostics and exposes counters in the Prometheus text format
// without pulling in a metrics client dependency.
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RequestInfo is filled in as a request moves through middleware; the auth layer adds the tenant.
// It never holds request bodies or document content.
type RequestInfo struct {
	ID       string
	TenantID string
	Logger   *slog.Logger
}

type requestInfoKey struct{}

func WithRequestInfo(ctx context.Context, info *RequestInfo) context.Context {
	return context.WithValue(ctx, requestInfoKey{}, info)
}

func RequestInfoFrom(ctx context.Context) *RequestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(*RequestInfo)
	return info
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestID keeps a well-formed caller-supplied ID so host and KMS logs correlate, otherwise generates one.
func RequestID(supplied string) string {
	if requestIDPattern.MatchString(supplied) {
		return supplied
	}
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(raw[:])
}

// DurationBuckets are upper bounds in seconds for HTTP latency histograms.
var DurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type requestKey struct{ method, route, status string }

type histogram struct {
	counts []uint64
	sum    float64
	total  uint64
}

type Metrics struct {
	mu        sync.Mutex
	requests  map[requestKey]uint64
	latencies map[string]*histogram
}

func NewMetrics() *Metrics {
	return &Metrics{requests: map[requestKey]uint64{}, latencies: map[string]*histogram{}}
}

// ObserveHTTP records one request. Route must be the matched pattern, never the raw path, to bound cardinality.
func (m *Metrics) ObserveHTTP(method, route string, status int, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[requestKey{method: method, route: route, status: strconv.Itoa(status)}]++
	h := m.latencies[route]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(DurationBuckets))}
		m.latencies[route] = h
	}
	seconds := elapsed.Seconds()
	for i, bound := range DurationBuckets {
		if seconds <= bound {
			h.counts[i]++
		}
	}
	h.sum += seconds
	h.total++
}

// Gauge is a point-in-time value computed at scrape time, such as queue backlog.
type Gauge struct {
	Name   string
	Help   string
	Labels map[string]string
	Value  float64
}

func (m *Metrics) WritePrometheus(w io.Writer, gauges []Gauge) error {
	m.mu.Lock()
	requestKeys := make([]requestKey, 0, len(m.requests))
	for key := range m.requests {
		requestKeys = append(requestKeys, key)
	}
	sort.Slice(requestKeys, func(i, j int) bool {
		a, b := requestKeys[i], requestKeys[j]
		return a.route+a.method+a.status < b.route+b.method+b.status
	})
	var out strings.Builder
	out.WriteString("# HELP kms_http_requests_total HTTP requests by method, matched route and status.\n# TYPE kms_http_requests_total counter\n")
	for _, key := range requestKeys {
		fmt.Fprintf(&out, "kms_http_requests_total{method=%q,route=%q,status=%q} %d\n", key.method, key.route, key.status, m.requests[key])
	}
	routes := make([]string, 0, len(m.latencies))
	for route := range m.latencies {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	out.WriteString("# HELP kms_http_request_duration_seconds HTTP request latency by matched route.\n# TYPE kms_http_request_duration_seconds histogram\n")
	for _, route := range routes {
		h := m.latencies[route]
		for i, bound := range DurationBuckets {
			fmt.Fprintf(&out, "kms_http_request_duration_seconds_bucket{route=%q,le=%q} %d\n", route, strconv.FormatFloat(bound, 'f', -1, 64), h.counts[i])
		}
		fmt.Fprintf(&out, "kms_http_request_duration_seconds_bucket{route=%q,le=\"+Inf\"} %d\n", route, h.total)
		fmt.Fprintf(&out, "kms_http_request_duration_seconds_sum{route=%q} %g\n", route, h.sum)
		fmt.Fprintf(&out, "kms_http_request_duration_seconds_count{route=%q} %d\n", route, h.total)
	}
	m.mu.Unlock()

	written := map[string]bool{}
	for _, gauge := range gauges {
		if !written[gauge.Name] {
			fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s gauge\n", gauge.Name, gauge.Help, gauge.Name)
			written[gauge.Name] = true
		}
		out.WriteString(gauge.Name)
		if len(gauge.Labels) > 0 {
			names := make([]string, 0, len(gauge.Labels))
			for name := range gauge.Labels {
				names = append(names, name)
			}
			sort.Strings(names)
			pairs := make([]string, 0, len(names))
			for _, name := range names {
				pairs = append(pairs, fmt.Sprintf("%s=%q", name, gauge.Labels[name]))
			}
			out.WriteString("{" + strings.Join(pairs, ",") + "}")
		}
		fmt.Fprintf(&out, " %g\n", gauge.Value)
	}
	_, err := io.WriteString(w, out.String())
	return err
}
