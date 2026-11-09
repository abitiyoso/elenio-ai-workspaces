package integrations

import (
	"context"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/elenio/control-plane/internal/platform/config"
	"github.com/elenio/control-plane/internal/platform/observability"
)

type Health struct {
	Status    string     `json:"status"` // healthy | unhealthy | unknown | not_configured
	CheckedAt *time.Time `json:"checked_at"`
	Detail    string     `json:"detail"`
}

// Upstreams wraps the trusted deployment config and the latest health probe results.
type Upstreams struct {
	cfg     map[string]config.Upstream
	client  *http.Client
	metrics *observability.Metrics
	mu      sync.RWMutex
	health  map[string]Health
}

func NewUpstreams(cfg map[string]config.Upstream, metrics *observability.Metrics) *Upstreams {
	return &Upstreams{cfg: cfg, metrics: metrics, health: map[string]Health{},
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (u *Upstreams) Get(appKey string) (config.Upstream, bool) {
	c, ok := u.cfg[appKey]
	return c, ok
}

func (u *Upstreams) Keys() []string {
	keys := make([]string, 0, len(u.cfg))
	for k := range u.cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Integration returns "proxy" only when the deployment config explicitly enables forwarding.
func (u *Upstreams) Integration(appKey string) string {
	if c, ok := u.cfg[appKey]; ok && c.Mode == "proxy" {
		return "proxy"
	}
	return "pending"
}

func (u *Upstreams) Health(appKey string) Health {
	c, ok := u.cfg[appKey]
	if !ok || c.HealthURL == "" {
		return Health{Status: "not_configured", Detail: "no trusted health endpoint configured"}
	}
	u.mu.RLock()
	defer u.mu.RUnlock()
	if h, ok := u.health[appKey]; ok {
		return h
	}
	return Health{Status: "unknown", Detail: "not probed yet"}
}

// Probe checks every configured health URL once. Only the status code is recorded (no response bodies).
func (u *Upstreams) Probe(ctx context.Context) {
	for key, c := range u.cfg {
		if c.HealthURL == "" {
			continue
		}
		now := time.Now().UTC()
		h := Health{CheckedAt: &now}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.HealthURL, nil)
		resp, err := u.client.Do(req)
		switch {
		case err != nil:
			h.Status, h.Detail = "unhealthy", "unreachable"
		default:
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				h.Status, h.Detail = "healthy", "health endpoint OK"
			} else {
				h.Status, h.Detail = "unhealthy", "health endpoint HTTP "+http.StatusText(resp.StatusCode)
			}
		}
		if u.metrics != nil {
			v := 0.0
			if h.Status == "healthy" {
				v = 1
			}
			u.metrics.AppHealth.WithLabelValues(key).Set(v)
		}
		u.mu.Lock()
		u.health[key] = h
		u.mu.Unlock()
	}
}

func (u *Upstreams) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		u.Probe(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
