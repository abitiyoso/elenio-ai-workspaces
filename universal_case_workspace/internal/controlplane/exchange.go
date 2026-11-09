// Package controlplane exchanges the caller's incoming Control Plane token (aud=case-workspace) for a
// user-scoped token for another audience (e.g. flowcase) through the Control Plane token exchange.
// UCW authenticates to the exchange as the confidential client "case-workspace" (HTTP Basic).
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/elenio/case-workspace/internal/auth"
)

var (
	// ErrSessionInactive: the exchange refused the subject token (401/403), e.g. the CP session ended.
	ErrSessionInactive = errors.New("control plane session is no longer active")
	// ErrUnavailable: network failure, timeout or 5xx/unexpected answer from the exchange.
	ErrUnavailable = errors.New("control plane token exchange unavailable")
)

const (
	refreshBefore = 30 * time.Second
	maxEntries    = 10000
)

// Exchanger obtains audience-bound user tokens, cached per (user, session, audience).
type Exchanger interface {
	Token(ctx context.Context, id auth.Identity, audience string) (string, error)
}

type Config struct {
	BaseURL      string // CP internal origin, e.g. http://100.126.146.52:5200
	ClientID     string
	ClientSecret string
	Timeout      time.Duration
	HTTPClient   *http.Client
	Now          func() time.Time
}

type entry struct {
	token string
	until time.Time
}

type Client struct {
	url, clientID, secret string
	client                *http.Client
	now                   func() time.Time

	mu    sync.Mutex
	cache map[string]entry
}

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" || cfg.ClientID == "" || len(cfg.ClientSecret) < 16 {
		return nil, errors.New("controlplane: base URL, client id and a client secret (>= 16 chars) are required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{url: strings.TrimRight(cfg.BaseURL, "/") + "/api/v1/token-exchange", clientID: cfg.ClientID, secret: cfg.ClientSecret,
		client: client, now: cfg.Now, cache: map[string]entry{}}, nil
}

func cacheKey(id auth.Identity, audience string) string {
	return id.UserID + "\x00" + id.SessionRef + "\x00" + audience
}

// Token returns a cached token valid for at least 30 more seconds, or performs the exchange.
func (c *Client) Token(ctx context.Context, id auth.Identity, audience string) (string, error) {
	key := cacheKey(id, audience)
	now := c.now()
	c.mu.Lock()
	if e, ok := c.cache[key]; ok && now.Before(e.until) {
		c.mu.Unlock()
		return e.token, nil
	}
	c.mu.Unlock()

	body, _ := json.Marshal(map[string]string{"subject_token": id.Token, "audience": audience})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.SetBasicAuth(c.clientID, c.secret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", ErrSessionInactive
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" || out.ExpiresIn <= 0 {
		return "", fmt.Errorf("%w: malformed exchange response", ErrUnavailable)
	}
	until := now.Add(time.Duration(out.ExpiresIn)*time.Second - refreshBefore)
	if until.After(now) {
		c.mu.Lock()
		if len(c.cache) >= maxEntries {
			for k, e := range c.cache {
				if !now.Before(e.until) {
					delete(c.cache, k)
				}
			}
			if len(c.cache) >= maxEntries {
				c.cache = map[string]entry{}
			}
		}
		c.cache[key] = entry{token: out.AccessToken, until: until}
		c.mu.Unlock()
	}
	return out.AccessToken, nil
}
