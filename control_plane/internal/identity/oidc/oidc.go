// Package oidc is the confidential OIDC client used by the BFF: discovery, PKCE authorization requests,
// server-side code exchange and ID token verification (issuer, audience, signature, expiry, nonce).
package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrUnavailable = errors.New("identity provider unavailable")
	ErrInvalid     = errors.New("invalid authentication response")
)

// Reason codes for rejected logins (safe to log and to show as ?error=).
type Rejection struct{ Code string }

func (r *Rejection) Error() string        { return "oidc: " + r.Code }
func (r *Rejection) Is(target error) bool { return target == ErrInvalid }

func reject(code string) error { return &Rejection{Code: code} }

// Code returns the rejection code of err, or "login_failed".
func Code(err error) string {
	var r *Rejection
	if errors.As(err, &r) {
		return r.Code
	}
	if errors.Is(err, ErrUnavailable) {
		return "idp_unavailable"
	}
	return "login_failed"
}

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	PostLogout   string
	HTTPClient   *http.Client
	Now          func() time.Time
}

type Identity struct {
	Issuer      string
	Subject     string
	DisplayName string
	Email       string
	AuthTime    time.Time
}

type Status struct {
	State       string // ok | unavailable | not_configured
	LastChecked time.Time
	Detail      string
	Hash        string
}

type Client struct {
	cfg Config

	mu         sync.Mutex
	provider   *gooidc.Provider
	verifier   *gooidc.IDTokenVerifier
	oauth      *oauth2.Config
	endSession string
	revocation string
	status     Status
	lastFail   time.Time
}

func New(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Client{cfg: cfg, status: Status{State: "unknown"}}
}

func (c *Client) Issuer() string   { return c.cfg.Issuer }
func (c *Client) ClientID() string { return c.cfg.ClientID }
func (c *Client) Scopes() []string { return append([]string(nil), c.cfg.Scopes...) }

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Ensure performs discovery once (and retries after failures, at most every 5 seconds). When the IdP is
// down, new logins fail closed; existing sessions are unaffected until their own expiry.
func (c *Client) Ensure(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return nil
	}
	if !c.lastFail.IsZero() && c.cfg.Now().Sub(c.lastFail) < 5*time.Second {
		return ErrUnavailable
	}
	ctx = gooidc.ClientContext(ctx, c.cfg.HTTPClient)
	p, err := gooidc.NewProvider(ctx, c.cfg.Issuer)
	if err != nil {
		c.lastFail = c.cfg.Now()
		c.status = Status{State: "unavailable", LastChecked: c.lastFail, Detail: "discovery failed"}
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	var extra struct {
		EndSession string   `json:"end_session_endpoint"`
		Revocation string   `json:"revocation_endpoint"`
		Methods    []string `json:"code_challenge_methods_supported"`
	}
	_ = p.Claims(&extra)
	if len(extra.Methods) > 0 && !contains(extra.Methods, "S256") {
		c.lastFail = c.cfg.Now()
		c.status = Status{State: "unavailable", LastChecked: c.lastFail, Detail: "IdP does not advertise PKCE S256"}
		return fmt.Errorf("%w: PKCE S256 unsupported", ErrUnavailable)
	}
	raw, _ := json.Marshal(p.Endpoint())
	sum := sha256.Sum256(raw)
	c.provider = p
	c.verifier = p.Verifier(&gooidc.Config{ClientID: c.cfg.ClientID, Now: c.cfg.Now})
	c.oauth = &oauth2.Config{ClientID: c.cfg.ClientID, ClientSecret: c.cfg.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: c.cfg.RedirectURL, Scopes: c.cfg.Scopes}
	c.endSession, c.revocation = extra.EndSession, extra.Revocation
	c.status = Status{State: "ok", LastChecked: c.cfg.Now(), Detail: "discovery ok", Hash: hex.EncodeToString(sum[:8])}
	return nil
}

// Probe re-runs discovery for health reporting without disturbing the cached provider.
func (c *Client) Probe(ctx context.Context) Status {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.cfg.Issuer, "/")+"/.well-known/openid-configuration", nil)
	resp, err := c.cfg.HTTPClient.Do(req)
	st := Status{LastChecked: c.cfg.Now()}
	if err != nil {
		st.State, st.Detail = "unavailable", "discovery unreachable"
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			st.State, st.Detail = "ok", "discovery ok"
		} else {
			st.State, st.Detail = "unavailable", fmt.Sprintf("discovery HTTP %d", resp.StatusCode)
		}
	}
	c.mu.Lock()
	st.Hash = c.status.Hash
	c.status = st
	c.mu.Unlock()
	return st
}

// AuthCodeURL builds the authorization request with S256 PKCE, state and nonce.
func (c *Client) AuthCodeURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	if err := c.Ensure(ctx); err != nil {
		return "", err
	}
	return c.oauth.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// Exchange redeems the code server-side (client secret + PKCE verifier) and verifies the ID token.
func (c *Client) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, *oauth2.Token, error) {
	if err := c.Ensure(ctx); err != nil {
		return Identity{}, nil, err
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.cfg.HTTPClient)
	tok, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			return Identity{}, nil, reject("code_rejected")
		}
		return Identity{}, nil, fmt.Errorf("%w: token endpoint: %v", ErrUnavailable, err)
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		return Identity{}, nil, reject("missing_id_token")
	}
	id, err := c.verifyIDToken(ctx, rawID, nonce)
	if err != nil {
		return Identity{}, nil, err
	}
	return id, tok, nil
}

func (c *Client) verifyIDToken(ctx context.Context, raw, nonce string) (Identity, error) {
	ctx = gooidc.ClientContext(ctx, c.cfg.HTTPClient)
	t, err := c.verifier.Verify(ctx, raw) // signature (JWKS), iss, aud contains client_id, exp
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "expired"):
			return Identity{}, reject("id_token_expired")
		case strings.Contains(msg, "issuer"), strings.Contains(msg, "different provider"):
			return Identity{}, reject("wrong_issuer")
		case strings.Contains(msg, "audience"):
			return Identity{}, reject("wrong_audience")
		}
		return Identity{}, reject("id_token_invalid")
	}
	if t.Nonce == "" || !constantEq(t.Nonce, nonce) {
		return Identity{}, reject("nonce_mismatch")
	}
	var claims struct {
		Azp               string `json:"azp"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		EmailVerified     *bool  `json:"email_verified"`
		AuthTime          int64  `json:"auth_time"`
	}
	if err := t.Claims(&claims); err != nil {
		return Identity{}, reject("id_token_invalid")
	}
	// Multiple audiences require azp to name this client (OIDC Core 3.1.3.7).
	if len(t.Audience) > 1 && claims.Azp != c.cfg.ClientID {
		return Identity{}, reject("wrong_audience")
	}
	if t.Subject == "" || len(t.Subject) > 255 {
		return Identity{}, reject("invalid_subject")
	}
	id := Identity{Issuer: t.Issuer, Subject: t.Subject, DisplayName: firstNonEmpty(claims.Name, claims.PreferredUsername, t.Subject)}
	if claims.EmailVerified == nil || *claims.EmailVerified {
		id.Email = claims.Email
	}
	id.AuthTime = t.IssuedAt
	if claims.AuthTime > 0 {
		id.AuthTime = time.Unix(claims.AuthTime, 0)
	}
	return id, nil
}

// EndSessionURL returns the RP-initiated logout URL (no tokens in the query), or "" if unsupported.
func (c *Client) EndSessionURL() string {
	c.mu.Lock()
	end := c.endSession
	c.mu.Unlock()
	if end == "" {
		return ""
	}
	u, err := url.Parse(end)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("client_id", c.cfg.ClientID)
	q.Set("post_logout_redirect_uri", c.cfg.PostLogout)
	u.RawQuery = q.Encode()
	return u.String()
}

// Revoke revokes a refresh token at the IdP (RFC 7009), best effort.
func (c *Client) Revoke(ctx context.Context, refreshToken string) error {
	c.mu.Lock()
	endpoint := c.revocation
	c.mu.Unlock()
	if endpoint == "" || refreshToken == "" {
		return nil
	}
	form := url.Values{"token": {refreshToken}, "token_type_hint": {"refresh_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(url.QueryEscape(c.cfg.ClientID), url.QueryEscape(c.cfg.ClientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("revocation HTTP %d", resp.StatusCode)
	}
	return nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func constantEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
