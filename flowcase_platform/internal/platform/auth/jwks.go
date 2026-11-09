package auth

import (
	"context"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const maxJWKSBytes = 1 << 20

// JWKSCache fetches and caches the host's signing keys. Unknown key ids trigger at most one refresh
// per MinRefreshInterval so forged kids cannot be used to hammer the host.
type JWKSCache struct {
	URL                string
	Client             *http.Client
	TTL                time.Duration
	MinRefreshInterval time.Duration
	Now                func() time.Time

	mu          sync.Mutex
	keys        map[string]crypto.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

func NewJWKSCache(url string, ttl time.Duration) *JWKSCache {
	return &JWKSCache{URL: url, TTL: ttl, MinRefreshInterval: 30 * time.Second, Client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *JWKSCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *JWKSCache) Key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.keys == nil || now.Sub(c.fetchedAt) > c.TTL {
		if err := c.refreshLocked(ctx, now); err != nil && c.keys == nil {
			return nil, err
		}
	}
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	if now.Sub(c.lastAttempt) >= c.MinRefreshInterval {
		if err := c.refreshLocked(ctx, now); err != nil {
			return nil, unauthenticated("unknown_kid")
		}
		if key, ok := c.keys[kid]; ok {
			return key, nil
		}
	}
	return nil, unauthenticated("unknown_kid")
}

// Ready reports whether keys could be loaded; used by readiness checks.
func (c *JWKSCache) Ready(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.keys != nil && now.Sub(c.fetchedAt) <= c.TTL {
		return nil
	}
	return c.refreshLocked(ctx, now)
}

func (c *JWKSCache) refreshLocked(ctx context.Context, now time.Time) error {
	c.lastAttempt = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return misconfigured("jwks_request")
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.Client.Do(req)
	if err != nil {
		return misconfigured("jwks_unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return misconfigured("jwks_status")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxJWKSBytes+1))
	if err != nil || len(body) > maxJWKSBytes {
		return misconfigured("jwks_body")
	}
	keys, err := ParseJWKS(body)
	if err != nil || len(keys) == 0 {
		return misconfigured("jwks_invalid")
	}
	c.keys, c.fetchedAt = keys, now
	return nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// ParseJWKS accepts RSA (>= 2048 bit) and EC P-256 signing keys; anything else is skipped.
func ParseJWKS(data []byte) (map[string]crypto.PublicKey, error) {
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	out := map[string]crypto.PublicKey{}
	for _, k := range set.Keys {
		if k.Kid == "" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		switch k.Kty {
		case "RSA":
			if k.Alg != "" && k.Alg != "RS256" {
				continue
			}
			n, errN := b64(k.N)
			e, errE := b64(k.E)
			if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
				continue
			}
			pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
			if pub.N.BitLen() < 2048 || pub.E < 3 {
				continue
			}
			out[k.Kid] = pub
		case "EC":
			if k.Crv != "P-256" || (k.Alg != "" && k.Alg != "ES256") {
				continue
			}
			x, errX := b64(k.X)
			y, errY := b64(k.Y)
			if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
				continue
			}
			// Reject points not on the curve before building the ecdsa key.
			if _, err := ecdh.P256().NewPublicKey(append(append([]byte{4}, x...), y...)); err != nil {
				continue
			}
			out[k.Kid] = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		}
	}
	return out, nil
}

func b64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
