// Package localdev provides opt-in loopback test tooling. It is not imported by the production server.
package localdev

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type User struct {
	Subject string `json:"subject"`
	Name    string `json:"name"`
}
type IdentityOptions struct {
	Issuer       string
	Redirect     string
	ClientID     string
	ClientSecret string
	Users        []User
}
type flow struct {
	State, Nonce, Challenge, Binding string
	Expires                          time.Time
}
type code struct {
	Subject, Nonce, Challenge string
	Expires                   time.Time
}
type Identity struct {
	options IdentityOptions
	key     *rsa.PrivateKey
	kid     string
	mu      sync.Mutex
	flows   map[string]flow
	codes   map[string]code
}

func LoopbackOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	return u.Hostname() == "127.0.0.1" && u.Port() != ""
}
func NewIdentity(options IdentityOptions) (*Identity, error) {
	redirect, err := url.Parse(options.Redirect)
	if !LoopbackOrigin(options.Issuer) || err != nil || !LoopbackOrigin("http://"+redirect.Host) || redirect.Path != "/auth/callback" || redirect.RawQuery != "" || redirect.Fragment != "" || redirect.User != nil || redirect.Scheme != "http" || options.ClientID != "control-plane" || len(options.ClientSecret) < 32 || len(options.Users) < 1 || len(options.Users) > 12 {
		return nil, errors.New("local identity requires explicit loopback origins, client and bounded synthetic users")
	}
	seen := map[string]bool{}
	for _, u := range options.Users {
		if len(u.Subject) < 8 || len(u.Subject) > 128 || strings.ContainsAny(u.Subject, "\r\n\t ") || seen[u.Subject] || u.Name == "" || len(u.Name) > 120 {
			return nil, errors.New("invalid local synthetic user")
		}
		seen[u.Subject] = true
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(key.N.Bytes())
	return &Identity{options: options, key: key, kid: base64.RawURLEncoding.EncodeToString(sum[:12]), flows: map[string]flow{}, codes: map[string]code{}}, nil
}
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("local random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (i *Identity) prune() {
	now := time.Now()
	for k, v := range i.flows {
		if now.After(v.Expires) {
			delete(i.flows, k)
		}
	}
	for k, v := range i.codes {
		if now.After(v.Expires) {
			delete(i.codes, k)
		}
	}
}
func jsonResponse(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(body)
}
func (i *Identity) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{"status": "ok", "identity": "local_test_fixture"})
	})
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := i.options.Issuer
		jsonResponse(w, map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/jwks", "end_session_endpoint": base + "/logout", "revocation_endpoint": base + "/revoke", "id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "code_challenge_methods_supported": []string{"S256"}})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": i.kid, "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(i.key.E)).Bytes())}}})
	})
	mux.HandleFunc("GET /authorize", i.authorize)
	mux.HandleFunc("POST /authorize", i.choose)
	mux.HandleFunc("POST /token", i.token)
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		if !i.client(r) {
			http.Error(w, "invalid client", 401)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /logout", func(w http.ResponseWriter, r *http.Request) {
		want := strings.TrimSuffix(i.options.Redirect, "/auth/callback") + "/signed-out"
		if r.URL.Query().Get("post_logout_redirect_uri") != want {
			http.Error(w, "invalid logout target", 400)
			return
		}
		http.Redirect(w, r, want, 302)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := url.Parse(i.options.Issuer)
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if r.Host != u.Host || err != nil || !net.ParseIP(host).IsLoopback() {
			http.Error(w, "loopback host required", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		// Preserve the same-origin form's Origin (no-referrer yields Origin:null in Chromium),
		// while never forwarding authorization query parameters as a referrer.
		w.Header().Set("Referrer-Policy", "strict-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Chromium also checks the form's redirect destination against form-action.
		// Permit only this fixed, constructor-validated loopback callback origin.
		callback, _ := url.Parse(i.options.Redirect)
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' "+callback.Scheme+"://"+callback.Host+"; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}
func (i *Identity) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	challenge, err := base64.RawURLEncoding.DecodeString(q.Get("code_challenge"))
	if q.Get("client_id") != i.options.ClientID || q.Get("redirect_uri") != i.options.Redirect || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || err != nil || len(challenge) != 32 || len(q.Get("state")) < 16 || len(q.Get("state")) > 1024 || len(q.Get("nonce")) < 16 || len(q.Get("nonce")) > 1024 || !strings.Contains(" "+q.Get("scope")+" ", " openid ") {
		http.Error(w, "invalid local authorization request", 400)
		return
	}
	id, binding := randomToken(), randomToken()
	i.mu.Lock()
	i.prune()
	if len(i.flows) >= 128 {
		i.mu.Unlock()
		http.Error(w, "too many local sign-in attempts", 429)
		return
	}
	i.flows[id] = flow{State: q.Get("state"), Nonce: q.Get("nonce"), Challenge: q.Get("code_challenge"), Binding: binding, Expires: time.Now().Add(5 * time.Minute)}
	i.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "elenio_local_identity_flow", Value: binding, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 300})
	_ = identityPage.Execute(w, struct {
		Request string
		Users   []User
	}{id, i.options.Users})
}
func (i *Identity) choose(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != i.options.Issuer {
		http.Error(w, "origin rejected", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	cookie, err := r.Cookie("elenio_local_identity_flow")
	if err != nil {
		http.Error(w, "flow binding missing", 403)
		return
	}
	subject := r.PostForm.Get("subject")
	known := false
	for _, u := range i.options.Users {
		if u.Subject == subject {
			known = true
		}
	}
	if !known {
		http.Error(w, "unknown synthetic identity", 400)
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.prune()
	f, ok := i.flows[r.PostForm.Get("request")]
	if !ok || !equal(f.Binding, cookie.Value) {
		http.Error(w, "expired or unbound flow", 403)
		return
	}
	delete(i.flows, r.PostForm.Get("request"))
	if len(i.codes) >= 128 {
		http.Error(w, "too many pending codes", 429)
		return
	}
	value := randomToken()
	i.codes[value] = code{Subject: subject, Nonce: f.Nonce, Challenge: f.Challenge, Expires: time.Now().Add(time.Minute)}
	target, _ := url.Parse(i.options.Redirect)
	q := target.Query()
	q.Set("code", value)
	q.Set("state", f.State)
	q.Set("iss", i.options.Issuer)
	target.RawQuery = q.Encode()
	http.SetCookie(w, &http.Cookie{Name: "elenio_local_identity_flow", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, target.String(), 302)
}
func (i *Identity) client(r *http.Request) bool {
	client, secret, ok := r.BasicAuth()
	return ok && client == i.options.ClientID && equal(secret, i.options.ClientSecret)
}
func (i *Identity) token(w http.ResponseWriter, r *http.Request) {
	if !i.client(r) {
		http.Error(w, "invalid client", 401)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil {
		http.Error(w, "invalid token request", 400)
		return
	}
	i.mu.Lock()
	i.prune()
	c, ok := i.codes[r.PostForm.Get("code")]
	delete(i.codes, r.PostForm.Get("code"))
	i.mu.Unlock()
	verifier := r.PostForm.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	if !ok || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != i.options.Redirect || len(verifier) < 43 || len(verifier) > 128 || !equal(base64.RawURLEncoding.EncodeToString(sum[:]), c.Challenge) {
		http.Error(w, "invalid grant", 400)
		return
	}
	name := ""
	for _, u := range i.options.Users {
		if u.Subject == c.Subject {
			name = u.Name
		}
	}
	now := time.Now()
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": i.options.Issuer, "aud": i.options.ClientID, "sub": c.Subject, "nonce": c.Nonce, "name": name, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "auth_time": now.Unix()})
	jwtToken.Header["kid"] = i.kid
	signed, err := jwtToken.SignedString(i.key)
	if err != nil {
		http.Error(w, "local signing unavailable", 500)
		return
	}
	jsonResponse(w, map[string]any{"id_token": signed, "access_token": randomToken(), "refresh_token": randomToken(), "token_type": "Bearer", "expires_in": 300})
}

var identityPage = template.Must(template.New("local-sign-in").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Elenio local test sign-in</title><style>body{font:16px/1.6 system-ui;background:#f5f7fa;color:#112b4d;margin:0;padding:32px}main{max-width:520px;background:white;border:1px solid #dfe5ee;border-radius:14px;margin:8vh auto;padding:28px}label,select,button{display:block;width:100%;box-sizing:border-box}select,button{font:inherit;padding:12px;margin:12px 0}button{background:#2864ec;color:white;border:0;border-radius:8px}</style><main><h1>Local workspace sign-in</h1><p>Development test identities only. This is a loopback fixture identity provider, not corporate SSO.</p><form method="post" action="/authorize"><input type="hidden" name="request" value="{{.Request}}"><label>Test identity<select name="subject">{{range .Users}}<option value="{{.Subject}}">{{.Name}}</option>{{end}}</select></label><button>Continue with test identity</button></form></main></html>`))
