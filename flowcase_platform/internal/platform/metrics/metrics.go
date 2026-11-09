// Package metrics is a minimal Prometheus text-format registry (counters, histograms and gauges
// collected at scrape time). It is served on an internal listener only, never on the public API.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Labels map[string]string

func (l Labels) key() string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s=%q`, k, l[k]))
	}
	return strings.Join(parts, ",")
}

// Sample is one gauge value produced by a collector.
type Sample struct {
	Labels Labels
	Value  float64
}

type family struct {
	help, kind string
	values     map[string]float64 // label key -> value (counters)
	buckets    []float64          // histograms
	hist       map[string]*histogram
}

type histogram struct {
	counts []uint64
	sum    float64
	count  uint64
}

type collector struct {
	name, help string
	fn         func(ctx context.Context) ([]Sample, error)
}

type Registry struct {
	mu         sync.Mutex
	families   map[string]*family
	collectors []collector
	Logger     *slog.Logger
}

func New(logger *slog.Logger) *Registry {
	return &Registry{families: map[string]*family{}, Logger: logger}
}

func (r *Registry) fam(name, help, kind string) *family {
	f, ok := r.families[name]
	if !ok {
		f = &family{help: help, kind: kind, values: map[string]float64{}, hist: map[string]*histogram{}}
		r.families[name] = f
	}
	return f
}

// Add increments a counter.
func (r *Registry) Add(name, help string, labels Labels, v float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fam(name, help, "counter").values[labels.key()] += v
}

// Observe records a histogram observation (buckets are fixed on first use).
func (r *Registry) Observe(name, help string, buckets []float64, labels Labels, v float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.fam(name, help, "histogram")
	if f.buckets == nil {
		f.buckets = buckets
	}
	k := labels.key()
	h := f.hist[k]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(f.buckets))}
		f.hist[k] = h
	}
	for i, b := range f.buckets {
		if v <= b {
			h.counts[i]++
		}
	}
	h.sum += v
	h.count++
}

// Collect registers a gauge family computed at scrape time.
func (r *Registry) Collect(name, help string, fn func(ctx context.Context) ([]Sample, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors = append(r.collectors, collector{name: name, help: help, fn: fn})
}

func series(name, labels string) string {
	if labels == "" {
		return name
	}
	return name + "{" + labels + "}"
}

func withLabel(labels, extra string) string {
	if labels == "" {
		return extra
	}
	return labels + "," + extra
}

// Write renders the text exposition format.
func (r *Registry) Write(ctx context.Context, b *strings.Builder) {
	r.mu.Lock()
	names := make([]string, 0, len(r.families))
	for n := range r.families {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := r.families[n]
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", n, f.help, n, f.kind)
		if f.kind == "counter" {
			keys := make([]string, 0, len(f.values))
			for k := range f.values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(b, "%s %g\n", series(n, k), f.values[k])
			}
			continue
		}
		keys := make([]string, 0, len(f.hist))
		for k := range f.hist {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h := f.hist[k]
			for i, le := range f.buckets {
				fmt.Fprintf(b, "%s %d\n", series(n+"_bucket", withLabel(k, fmt.Sprintf(`le="%g"`, le))), h.counts[i])
			}
			fmt.Fprintf(b, "%s %d\n", series(n+"_bucket", withLabel(k, `le="+Inf"`)), h.count)
			fmt.Fprintf(b, "%s %g\n%s %d\n", series(n+"_sum", k), h.sum, series(n+"_count", k), h.count)
		}
	}
	collectors := append([]collector(nil), r.collectors...)
	r.mu.Unlock()
	for _, c := range collectors {
		samples, err := c.fn(ctx)
		if err != nil {
			if r.Logger != nil {
				r.Logger.WarnContext(ctx, "metrics collector failed", "metric", c.name, "error", err.Error())
			}
			continue
		}
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n", c.name, c.help, c.name)
		for _, s := range samples {
			fmt.Fprintf(b, "%s %g\n", series(c.name, s.Labels.key()), s.Value)
		}
	}
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
		defer cancel()
		var b strings.Builder
		r.Write(ctx, &b)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	})
}

// HTTPBuckets are request-duration buckets in seconds.
var HTTPBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
