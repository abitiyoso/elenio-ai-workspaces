// Command tokencheck runs live contract checks against a mini-app that verifies Control Plane tokens.
// It runs next to the Control Plane (it needs the service-token signing key file), mints tokens with the same
// issuer code the BFF uses, then tampers with them. A fixed negative suite runs against -probe; app-specific
// domain checks are passed with repeated -expect flags.
//
//	tokencheck -key <signing key> -issuer http://cp.example -target http://127.0.0.1:5380 -aud kms \
//	    -probe /api/v1/session -member <cp user id> -privileged <cp user id> -outsider <cp user id> \
//	    -expect 'member cannot create collection|POST|/api/v1/collections|member|403|{"name":"x"}'
//
// Tokens carry session id "contract-check", which has no live Control Plane session: apps that call the CP token
// exchange (the Universal Case Workspace) must therefore fail closed on any downstream data path.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/elenio/control-plane/internal/integrations"
)

type expectFlags []string

func (e *expectFlags) String() string     { return strings.Join(*e, ";") }
func (e *expectFlags) Set(v string) error { *e = append(*e, v); return nil }

// Status matchers: a number, "ok" (2xx) or "notdenied" (anything but 401/403, e.g. 400/422 for a bad body).
func matches(want string, got int) bool {
	switch want {
	case "ok":
		return got >= 200 && got < 300
	case "notdenied":
		return got != 0 && got != 401 && got != 403
	}
	n, err := strconv.Atoi(want)
	return err == nil && n == got
}

func main() {
	keyFile := flag.String("key", "", "ES256 signing key file")
	issuer := flag.String("issuer", "", "Control Plane origin (token iss)")
	target := flag.String("target", "", "mini-app base URL")
	aud := flag.String("aud", "", "audience the mini-app expects")
	otherAuds := flag.String("other-auds", "flowcase,kms,agent-studio", "audiences that must be rejected")
	tenant := flag.String("tenant", "acme-demo", "tenant key")
	probe := flag.String("probe", "/api/v1/session", "GET path that returns 2xx for a verified, provisioned principal")
	member := flag.String("member", "", "CP user id provisioned in the app (baseline role)")
	privileged := flag.String("privileged", "", "CP user id with an elevated app role (optional)")
	outsider := flag.String("outsider", "", "CP user id NOT provisioned in the app")
	outsiderCheck := flag.Bool("outsider-check", true, "expect a valid token for -outsider to be rejected (off for apps without their own user directory, e.g. the Universal Case Workspace, where the CP alone decides app access)")
	checkKind := flag.Bool("check-principal-type", true, "expect principal_type=service tokens to be rejected (off for apps that take the principal kind from their own directory)")
	var expects expectFlags
	flag.Var(&expects, "expect", "label|METHOD|PATH|member|privileged|outsider|STATUS|optional JSON body")
	flag.Parse()
	if *keyFile == "" || *issuer == "" || *target == "" || *aud == "" || *member == "" || (*outsiderCheck && *outsider == "") {
		fail("-key, -issuer, -target, -aud, -member and -outsider (unless -outsider-check=false) are required")
	}
	pemKey, err := os.ReadFile(*keyFile)
	if err != nil {
		fail("read key: %v", err)
	}
	is, err := integrations.NewIssuer(*issuer, 5*time.Minute, pemKey, nil)
	if err != nil {
		fail("issuer: %v", err)
	}
	priv, err := jwt.ParseECPrivateKeyFromPEM(pemKey)
	if err != nil {
		fail("parse key: %v", err)
	}
	valid := func(sub, audience string) string {
		tok, _, err := is.Mint(audience, integrations.Subject{UserID: sub, TenantKey: *tenant, WorkspaceKey: "sim-b2b", PrincipalType: "human",
			SessionID: "contract-check", Scopes: []string{"app:open"}, PolicyVersion: "contract", CorrelationID: "contract-check", AuthTime: time.Now()})
		if err != nil {
			fail("mint: %v", err)
		}
		return tok
	}
	custom := func(mut func(jwt.MapClaims) (jwt.SigningMethod, any)) string {
		now := time.Now()
		c := jwt.MapClaims{"iss": *issuer, "sub": *member, "aud": *aud, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
			"jti": fmt.Sprint(now.UnixNano()), "tenant_id": *tenant, "workspace_id": "sim-b2b", "principal_type": "human"}
		method, key := mut(c)
		t := jwt.NewWithClaims(method, c)
		t.Header["kid"] = is.KeyID()
		s, err := t.SignedString(key)
		if err != nil {
			fail("sign: %v", err)
		}
		return s
	}
	es := func(jwt.MapClaims) (jwt.SigningMethod, any) { return jwt.SigningMethodES256, priv }
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	type check struct {
		name, method, path, token, want, body string
		hdr                                   map[string]string
	}
	checks := []check{
		{name: "valid member token", method: "GET", path: *probe, token: valid(*member, *aud), want: "ok"},
		{name: "no token", method: "GET", path: *probe, want: "401"},
		{name: "forged identity headers, no token", method: "GET", path: *probe, want: "401", hdr: map[string]string{
			"X-User-ID": *member, "X-Tenant-ID": *tenant, "X-Role": "platform_admin", "Tailscale-User-Login": "admin@example.invalid", "X-Flowcase-Dev-Persona": "approver"}},
	}
	for _, a := range strings.Split(*otherAuds, ",") {
		if a = strings.TrimSpace(a); a != "" && a != *aud {
			checks = append(checks, check{name: "wrong audience (" + a + ")", method: "GET", path: *probe, token: valid(*member, a), want: "401"})
		}
	}
	checks = append(checks,
		check{name: "wrong issuer", method: "GET", path: *probe, want: "401", token: custom(func(c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["iss"] = "https://evil.example"
			return es(c)
		})},
		check{name: "expired", method: "GET", path: *probe, want: "401", token: custom(func(c jwt.MapClaims) (jwt.SigningMethod, any) {
			past := time.Now().Add(-20 * time.Minute)
			c["iat"], c["nbf"], c["exp"] = past.Unix(), past.Unix(), past.Add(10*time.Minute).Unix()
			return es(c)
		})},
		check{name: "not yet valid (nbf +10m)", method: "GET", path: *probe, want: "401", token: custom(func(c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["nbf"] = time.Now().Add(10 * time.Minute).Unix()
			return es(c)
		})},
		check{name: "lifetime > 15m", method: "GET", path: *probe, want: "401", token: custom(func(c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["exp"] = time.Now().Add(2 * time.Hour).Unix()
			return es(c)
		})},
		check{name: "foreign key, same kid", method: "GET", path: *probe, want: "401", token: custom(func(jwt.MapClaims) (jwt.SigningMethod, any) {
			return jwt.SigningMethodES256, otherKey
		})},
		check{name: "alg none", method: "GET", path: *probe, want: "401", token: custom(func(jwt.MapClaims) (jwt.SigningMethod, any) {
			return jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType
		})},
		check{name: "HS256 confusion", method: "GET", path: *probe, want: "401", token: custom(func(jwt.MapClaims) (jwt.SigningMethod, any) {
			return jwt.SigningMethodHS256, []byte(is.KeyID())
		})},
		check{name: "other tenant claim", method: "GET", path: *probe, want: "401", token: custom(func(c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["tenant_id"] = "other-tenant"
			return es(c)
		})},
	)
	if *outsiderCheck {
		checks = append(checks, check{name: "valid token, subject not provisioned in app", method: "GET", path: *probe, token: valid(*outsider, *aud), want: "401"})
	}
	if *checkKind {
		checks = append(checks, check{name: "service principal type", method: "GET", path: *probe, want: "401", token: custom(func(c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["principal_type"] = "service"
			return es(c)
		})})
	}
	subjects := map[string]string{"member": *member, "privileged": *privileged, "outsider": *outsider}
	for _, e := range expects {
		parts := strings.SplitN(e, "|", 6)
		if len(parts) < 5 {
			fail("bad -expect %q", e)
		}
		sub, ok := subjects[parts[3]]
		if !ok || sub == "" {
			fail("-expect %q names unknown/empty subject %q", e, parts[3])
		}
		c := check{name: parts[0], method: parts[1], path: parts[2], token: valid(sub, *aud), want: parts[4]}
		if len(parts) == 6 {
			c.body = parts[5]
		}
		checks = append(checks, c)
	}

	failed := 0
	for _, c := range checks {
		got, body := call(*target, c.method, c.path, c.token, c.body, c.hdr)
		ok := matches(c.want, got)
		mark := "PASS"
		if !ok {
			mark, failed = "FAIL", failed+1
		}
		detail := ""
		if !ok {
			detail = "\n      " + strings.ReplaceAll(body, "\n", " ")
		}
		fmt.Printf("%s  %-46s %-6s %-34s → %d (want %s)%s\n", mark, c.name, c.method, c.path, got, c.want, detail)
	}
	fmt.Printf("\n%d/%d checks passed\n", len(checks)-failed, len(checks))
	if failed > 0 {
		os.Exit(1)
	}
}

func call(base, method, path, token, body string, hdr map[string]string) (int, string) {
	var rdr io.Reader
	if body != "" || method == "POST" || method == "PUT" || method == "PATCH" {
		if body == "" {
			body = "{}"
		}
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, rdr)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Accept", "application/json")
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", fmt.Sprintf("contract-%d", time.Now().UnixNano()))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
	return resp.StatusCode, string(b)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tokencheck: "+format+"\n", args...)
	os.Exit(2)
}
