package apps

import "testing"

func valid() Manifest {
	return Manifest{SchemaVersion: "workspace.app.v1", AppKey: "kms", DisplayName: "Knowledge Hub", Category: "platform", Icon: "book-open",
		OwnerTeam: "Knowledge", DeploymentMode: "same_origin_route", RouteBase: "/apps/kms/", Environment: "dev", Version: "1.0.0",
		Status: "draft", RequiredEntitlements: []string{"kms:open"}, RequiresDomainAuthorization: true}
}

func TestValidManifest(t *testing.T) {
	if issues := Validate(valid(), "dev"); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestRouteAttacks(t *testing.T) {
	for _, r := range []string{"/apps/kms/../", "/apps/%2e%2e/", "//evil.example/", "https://evil.example/", "javascript:alert(1)",
		"/apps/kms", "/apps/kms//", "/apps/KMS/", "/apps/kms/?x=1", "/apps/kms/#x", "\\\\evil\\", "/apps/kms/\n", "/apps/other/", ""} {
		if ValidateRoute(r, "kms") == nil {
			t.Errorf("route %q accepted", r)
		}
	}
}

func TestStrictFieldsRejectUpstream(t *testing.T) {
	_, issues := ParseManifest([]byte(`{"schema_version":"workspace.app.v1","app_key":"kms","upstream_url":"http://10.0.0.1"}`))
	if len(issues) == 0 || issues[0].Code != "UNKNOWN_FIELD" {
		t.Fatalf("unknown field accepted: %v", issues)
	}
}

func TestCanonicalChecksumStable(t *testing.T) {
	_, a, _ := Canonical(valid())
	_, b, _ := Canonical(valid())
	m := valid()
	m.Version = "1.0.1"
	_, c, _ := Canonical(m)
	if a != b || a == c || len(a) != 64 {
		t.Fatal("checksum not stable/distinct")
	}
}

func TestSemver(t *testing.T) {
	if !semverGreater("1.10.0", "1.9.9") || semverGreater("1.0.0", "1.0.0") || semverGreater("0.9.0", "1.0.0") {
		t.Fatal("semver compare wrong")
	}
}
