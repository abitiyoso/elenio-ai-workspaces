package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"testing"
)

type fixedProviderResolver []netip.Addr

func (r fixedProviderResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r, nil
}

func TestProviderEndpointPolicyPinsOnlyPublicAllowlistedHosts(t *testing.T) {
	policy, err := NewProviderEndpointPolicy([]string{"api.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	policy.resolver = fixedProviderResolver{netip.MustParseAddr("203.0.113.10")}
	if _, err := policy.NewClient(context.Background(), "https://api.example.com/v1"); err == nil {
		t.Fatal("documentation-range address was accepted")
	}
	policy.resolver = fixedProviderResolver{netip.MustParseAddr("8.8.8.8")}
	client, err := policy.NewClient(context.Background(), "https://api.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("provider probe transport must not use an HTTP proxy")
	}
	if err := client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatal("provider probe client must not follow redirects")
	}
	client.CloseIdleConnections()
	if _, err := policy.NewClient(context.Background(), "https://other.example.com/v1"); err == nil {
		t.Fatal("non-allowlisted host was accepted")
	}
	if _, err := policy.NewClient(context.Background(), "http://api.example.com/v1"); err == nil {
		t.Fatal("non-HTTPS endpoint was accepted")
	}
}

func TestConnectionOnlyCapabilitiesAllowEmptyRequest(t *testing.T) {
	for _, raw := range []string{"", `{}`, "null"} {
		requested, err := connectionOnlyCapabilities([]byte(raw))
		if err != nil || len(requested) != 0 {
			t.Errorf("connectionOnlyCapabilities(%q) = %v, %v", raw, requested, err)
		}
	}
	if _, err := connectionOnlyCapabilities([]byte(`{"json":true}`)); err == nil {
		t.Fatal("connection-only mode accepted a token-consuming capability")
	}
	verified, err := verifiedCapabilities(map[string]bool{"catalog": true}, map[string]bool{})
	if err != nil || len(verified) == 0 {
		t.Fatalf("catalog-only verification was rejected: %s err=%v", verified, err)
	}
}

func TestRequestedCapabilitiesRequireExplicitSupportedCapability(t *testing.T) {
	for _, raw := range []string{`{}`, `{"json":false}`, `{"admin":true}`, `null`} {
		if _, err := requestedCapabilities([]byte(raw)); err == nil {
			t.Errorf("requestedCapabilities(%s) accepted invalid input", raw)
		}
	}
	requested, err := requestedCapabilities([]byte(`{"json":true,"tools":false}`))
	if err != nil || !requested["json"] {
		t.Fatalf("explicit JSON capability was rejected: requested=%v err=%v", requested, err)
	}
	if _, err := verifiedCapabilities(map[string]bool{"json": false}, requested); err == nil {
		t.Fatal("probe result that did not verify the requested capability was accepted")
	}
}

func TestVerifiedCapabilitiesAcceptsJSONAndToolsTogether(t *testing.T) {
	actual := map[string]bool{"catalog": true, "json": true, "tools": true}
	if _, err := verifiedCapabilities(actual, map[string]bool{"json": true, "tools": true}); err != nil {
		t.Fatalf("catalog+json+tools must verify a combined probe: %v", err)
	}
	if _, err := verifiedCapabilities(map[string]bool{"catalog": true, "json": true, "tools": true, "vision": true}, map[string]bool{"json": true}); err == nil {
		t.Fatal("unexpected capability keys must still be rejected")
	}
	if _, err := verifiedCapabilities(map[string]bool{"catalog": true, "tools": true}, map[string]bool{"json": true, "tools": true}); err == nil {
		t.Fatal("an unverified requested capability must be rejected")
	}
}
