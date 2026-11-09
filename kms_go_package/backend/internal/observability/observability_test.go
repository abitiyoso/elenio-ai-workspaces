package observability

import (
	"strings"
	"testing"
	"time"
)

func TestRequestIDKeepsWellFormedCallerIDAndReplacesUnsafeOnes(t *testing.T) {
	if got := RequestID("host-trace-0001"); got != "host-trace-0001" {
		t.Fatalf("well-formed caller ID was replaced: %q", got)
	}
	for _, unsafe := range []string{"", "short", "bad id with spaces", "x\ny-injected-header", strings.Repeat("a", 65)} {
		got := RequestID(unsafe)
		if got == unsafe || !requestIDPattern.MatchString(got) {
			t.Fatalf("unsafe request ID %q produced %q", unsafe, got)
		}
	}
}

func TestMetricsUseRoutePatternAndCumulativeBuckets(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveHTTP("GET", "GET /api/v1/documents", 200, 20*time.Millisecond)
	metrics.ObserveHTTP("GET", "GET /api/v1/documents", 500, 2*time.Second)
	var out strings.Builder
	if err := metrics.WritePrometheus(&out, []Gauge{{Name: "kms_outbox_events", Help: "Outbox events.", Labels: map[string]string{"status": "failed"}, Value: 3}}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		`kms_http_requests_total{method="GET",route="GET /api/v1/documents",status="200"} 1`,
		`kms_http_requests_total{method="GET",route="GET /api/v1/documents",status="500"} 1`,
		`kms_http_request_duration_seconds_bucket{route="GET /api/v1/documents",le="0.025"} 1`,
		`kms_http_request_duration_seconds_bucket{route="GET /api/v1/documents",le="2.5"} 2`,
		`kms_http_request_duration_seconds_count{route="GET /api/v1/documents"} 2`,
		"# TYPE kms_outbox_events gauge",
		`kms_outbox_events{status="failed"} 3`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics output missing %q:\n%s", want, text)
		}
	}
}
