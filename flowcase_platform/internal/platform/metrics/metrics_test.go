package metrics

import (
	"context"
	"strings"
	"testing"
)

func TestExposition(t *testing.T) {
	r := New(nil)
	r.Add("flowcase_http_requests_total", "Requests.", Labels{"route": "GET /x", "code": "2xx"}, 1)
	r.Add("flowcase_http_requests_total", "Requests.", Labels{"route": "GET /x", "code": "2xx"}, 1)
	r.Observe("flowcase_http_request_duration_seconds", "Latency.", []float64{0.1, 1}, Labels{"route": "GET /x"}, 0.05)
	r.Collect("flowcase_cases", "Cases.", func(context.Context) ([]Sample, error) {
		return []Sample{{Labels: Labels{"status": "active"}, Value: 3}}, nil
	})
	var b strings.Builder
	r.Write(context.Background(), &b)
	out := b.String()
	for _, want := range []string{
		`flowcase_http_requests_total{code="2xx",route="GET /x"} 2`,
		`flowcase_http_request_duration_seconds_bucket{route="GET /x",le="0.1"} 1`,
		`flowcase_http_request_duration_seconds_count{route="GET /x"} 1`,
		`# TYPE flowcase_cases gauge`,
		`flowcase_cases{status="active"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}
