package integrations

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestIssuer(t *testing.T, previous []byte) *Issuer {
	t.Helper()
	is, err := NewIssuer("https://cp.example", 5*time.Minute, nil, previous)
	if err != nil {
		t.Fatal(err)
	}
	return is
}

func TestMintActorAndMaxTTL(t *testing.T) {
	is := newTestIssuer(t, nil)
	tok, exp, err := is.Mint("flowcase", Subject{UserID: "u", TenantKey: "acme", PrincipalType: "human", SessionID: "s",
		Actor: "app:case-workspace", MaxTTL: time.Minute, AuthTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(exp); d > time.Minute+time.Second {
		t.Fatalf("MaxTTL not applied: %v", d)
	}
	v, err := is.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	act, _ := v.Claims["act"].(map[string]any)
	if act["sub"] != "app:case-workspace" || v.Claims["sid"] != SessionClaim(is.KeyID(), "s") {
		t.Fatalf("claims: %v", v.Claims)
	}
	// MaxTTL can never extend the issuer TTL; no actor means no act claim.
	tok, exp, _ = is.Mint("flowcase", Subject{UserID: "u", MaxTTL: time.Hour})
	if time.Until(exp) > 5*time.Minute+time.Second {
		t.Fatal("MaxTTL extended the issuer TTL")
	}
	v, _ = is.Verify(tok)
	if _, has := v.Claims["act"]; has {
		t.Fatal("act claim without actor")
	}
}

func TestVerifyRejectsForeignAndMalformed(t *testing.T) {
	is := newTestIssuer(t, nil)
	other := newTestIssuer(t, nil)
	sign := func(method jwt.SigningMethod, key any, kid string, mut func(jwt.MapClaims)) string {
		now := time.Now()
		c := jwt.MapClaims{"iss": "https://cp.example", "sub": "u", "aud": "x", "iat": now.Unix(), "exp": now.Add(time.Minute).Unix()}
		if mut != nil {
			mut(c)
		}
		tk := jwt.NewWithClaims(method, c)
		tk.Header["kid"] = kid
		s, err := tk.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	foreign, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherTok, _, _ := other.Mint("x", Subject{UserID: "u"})
	cases := map[string]string{
		"other issuer key":    otherTok,
		"foreign key own kid": sign(jwt.SigningMethodES256, foreign, is.KeyID(), nil),
		"alg none":            sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, is.KeyID(), nil),
		"hs256 with kid":      sign(jwt.SigningMethodHS256, []byte(is.KeyID()), is.KeyID(), nil),
		"wrong issuer": sign(jwt.SigningMethodES256, is.key, is.KeyID(), func(c jwt.MapClaims) {
			c["iss"] = "https://evil.example"
		}),
		"expired": sign(jwt.SigningMethodES256, is.key, is.KeyID(), func(c jwt.MapClaims) {
			c["iat"], c["exp"] = time.Now().Add(-10*time.Minute).Unix(), time.Now().Add(-5*time.Minute).Unix()
		}),
		"lifetime too long": sign(jwt.SigningMethodES256, is.key, is.KeyID(), func(c jwt.MapClaims) {
			c["exp"] = time.Now().Add(time.Hour).Unix()
		}),
		"no iat":  sign(jwt.SigningMethodES256, is.key, is.KeyID(), func(c jwt.MapClaims) { delete(c, "iat") }),
		"no exp":  sign(jwt.SigningMethodES256, is.key, is.KeyID(), func(c jwt.MapClaims) { delete(c, "exp") }),
		"garbage": "not.a.jwt",
		"empty":   "",
	}
	for name, raw := range cases {
		if _, err := is.Verify(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestVerifyAcceptsPreviousKey(t *testing.T) {
	old := newTestIssuer(t, nil)
	jwks, _ := json.Marshal(map[string]any{"keys": []JWK{old.JWKS()["keys"].([]JWK)[0]}})
	// The new issuer has a different signing key but keeps the old public key during rotation.
	der, _ := x509.MarshalPKCS8PrivateKey(mustKey(t))
	cur, err := NewIssuer("https://cp.example", 5*time.Minute, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), jwks)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, _ := old.Mint("x", Subject{UserID: "u", SessionID: "s"})
	v, err := cur.Verify(tok)
	if err != nil {
		t.Fatalf("previous-kid token rejected: %v", err)
	}
	if v.KeyID != old.KeyID() || v.Claims["sid"] != SessionClaim(old.KeyID(), "s") {
		t.Fatalf("unexpected verification result: kid=%s", v.KeyID)
	}
}

func mustKey(t *testing.T) *ecdsa.PrivateKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
