package localdev

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func localIdentity(t *testing.T) (*Identity, *httptest.Server, *http.Client) {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	issuer := "http://" + server.Listener.Addr().String()
	idp, err := NewIdentity(IdentityOptions{Issuer: issuer, Redirect: "http://127.0.0.1:8200/auth/callback", ClientID: "control-plane", ClientSecret: strings.Repeat("test-client-", 4), Users: []User{{Subject: "local-user-1", Name: "Synthetic builder"}}})
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = idp.Handler()
	server.Start()
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return idp, server, client
}
func beginLocal(t *testing.T, idp *Identity, client *http.Client, verifier string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {"control-plane"}, "redirect_uri": {idp.options.Redirect}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "state": {strings.Repeat("s", 32)}, "nonce": {strings.Repeat("n", 32)}, "scope": {"openid profile email"}}
	response, err := client.Get(idp.options.Issuer + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("authorization status %d", response.StatusCode)
	}
	if response.Header.Get("Referrer-Policy") != "strict-origin" || !strings.Contains(response.Header.Get("Content-Security-Policy"), "form-action 'self' http://127.0.0.1:8200;") {
		t.Fatal("browser policy must allow only the fixed callback and preserve form Origin without query referrers")
	}
	body, _ := io.ReadAll(response.Body)
	match := regexp.MustCompile(`name="request" value="([^"]+)"`).FindSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing browser-bound local flow")
	}
	return string(match[1])
}
func chooseLocal(t *testing.T, idp *Identity, client *http.Client, request, origin string) *http.Response {
	t.Helper()
	form := url.Values{"request": {request}, "subject": {"local-user-1"}}
	req, _ := http.NewRequest("POST", idp.options.Issuer+"/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", origin)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func tokenLocal(t *testing.T, idp *Identity, client *http.Client, code, verifier string) *http.Response {
	t.Helper()
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {idp.options.Redirect}}
	req, _ := http.NewRequest("POST", idp.options.Issuer+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("control-plane", idp.options.ClientSecret)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func TestLocalIdentityRunsBoundCodePKCEAndSignedNonce(t *testing.T) {
	idp, _, client := localIdentity(t)
	verifier := randomToken()
	flowID := beginLocal(t, idp, client, verifier)
	response := chooseLocal(t, idp, client, flowID, idp.options.Issuer)
	response.Body.Close()
	if response.StatusCode != 302 {
		t.Fatalf("choose status %d", response.StatusCode)
	}
	callback, _ := url.Parse(response.Header.Get("Location"))
	if callback.Scheme+"://"+callback.Host+callback.Path != idp.options.Redirect || callback.Query().Get("state") != strings.Repeat("s", 32) {
		t.Fatal("callback scope/state changed")
	}
	value := callback.Query().Get("code")
	response = tokenLocal(t, idp, client, value, verifier)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("exchange status %d", response.StatusCode)
	}
	var body struct {
		IDToken string `json:"id_token"`
	}
	if json.NewDecoder(response.Body).Decode(&body) != nil {
		t.Fatal("invalid response")
	}
	parsed, err := jwt.Parse(body.IDToken, func(*jwt.Token) (any, error) { return &idp.key.PublicKey, nil }, jwt.WithIssuer(idp.options.Issuer), jwt.WithAudience("control-plane"), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !parsed.Valid {
		t.Fatal("local signed identity rejected")
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["nonce"] != strings.Repeat("n", 32) || claims["sub"] != "local-user-1" {
		t.Fatal("nonce or selected subject changed")
	}
	duplicate := tokenLocal(t, idp, client, value, verifier)
	duplicate.Body.Close()
	if duplicate.StatusCode != 400 {
		t.Fatal("authorization code replay accepted")
	}
}
func TestLocalIdentityRejectsCrossOriginUnboundAndWrongPKCE(t *testing.T) {
	idp, _, client := localIdentity(t)
	verifier := randomToken()
	flowID := beginLocal(t, idp, client, verifier)
	response := chooseLocal(t, idp, client, flowID, "https://foreign.invalid")
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin selection accepted")
	}
	stranger := &http.Client{CheckRedirect: client.CheckRedirect}
	response = chooseLocal(t, idp, stranger, flowID, idp.options.Issuer)
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("unbound browser accepted")
	}
	response = chooseLocal(t, idp, client, flowID, idp.options.Issuer)
	response.Body.Close()
	target, _ := url.Parse(response.Header.Get("Location"))
	value := target.Query().Get("code")
	response = tokenLocal(t, idp, client, value, randomToken())
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal("wrong PKCE verifier accepted")
	}
	response = tokenLocal(t, idp, client, value, verifier)
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal("failed exchange left reusable authorization code")
	}
}
func TestLocalIdentityRejectsExternalHostsAndExpiredCodes(t *testing.T) {
	idp, _, client := localIdentity(t)
	req, _ := http.NewRequest("GET", idp.options.Issuer+"/healthz", nil)
	req.Host = "foreign.invalid"
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("foreign Host accepted")
	}
	verifier := randomToken()
	flowID := beginLocal(t, idp, client, verifier)
	response = chooseLocal(t, idp, client, flowID, idp.options.Issuer)
	response.Body.Close()
	target, _ := url.Parse(response.Header.Get("Location"))
	value := target.Query().Get("code")
	idp.mu.Lock()
	item := idp.codes[value]
	item.Expires = time.Now().Add(-time.Second)
	idp.codes[value] = item
	idp.mu.Unlock()
	response = tokenLocal(t, idp, client, value, verifier)
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal("expired local code accepted")
	}
	options := idp.options
	options.Issuer = "http://0.0.0.0:8210"
	if _, err := NewIdentity(options); err == nil {
		t.Fatal("non-loopback binding accepted")
	}
	options = idp.options
	options.Redirect = "https://foreign.invalid/auth/callback"
	if _, err := NewIdentity(options); err == nil {
		t.Fatal("external callback accepted")
	}
}
