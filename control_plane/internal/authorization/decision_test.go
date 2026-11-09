package authorization

import (
	"testing"
	"time"
)

func facts() Facts {
	return Facts{UserID: "u", TenantID: "t", UserStatus: "active", PrincipalType: "human", WorkspaceID: "w", WorkspaceKey: "sim-b2b",
		WorkspaceStatus: "active", MembershipState: "active", PolicyVersion: "v1",
		WorkspaceRoles: []BoundRole{{Key: "case_worker", Scope: "workspace", Permissions: []string{"catalog:view", "flowcase:open"}}},
		SensitivePerms: map[string]bool{"registry:manage": true}}
}

func app() *App {
	return &App{Key: "flowcase", Status: "published", HasPublishedVersion: true, RequiredEntitlements: []string{"flowcase:open"}, WorkspaceAllowlist: []string{"sim-b2b"}}
}

func TestDecideApp(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		f      func(*Facts, *App) *App
		effect string
		reason string
	}{
		{"granted", func(*Facts, *App) *App { return nil }, "allow", ReasonRoleGrant},
		{"disabled user", func(f *Facts, _ *App) *App { f.UserStatus = "suspended"; return nil }, "deny", ReasonSubjectDisabled},
		{"no membership", func(f *Facts, _ *App) *App { f.MembershipState = ""; return nil }, "deny", ReasonNotMember},
		{"suspended membership", func(f *Facts, _ *App) *App { f.MembershipState = "suspended"; return nil }, "deny", ReasonMembershipSuspended},
		{"foreign workspace", func(f *Facts, _ *App) *App { f.WorkspaceStatus = ""; return nil }, "deny", ReasonNotMember},
		{"app disabled", func(_ *Facts, a *App) *App { a.Status = "disabled"; return a }, "deny", ReasonAppDisabled},
		{"app draft", func(_ *Facts, a *App) *App { a.Status, a.HasPublishedVersion = "draft", false; return a }, "deny", ReasonAppUnpublished},
		{"allowlist", func(_ *Facts, a *App) *App { a.WorkspaceAllowlist = []string{"engineering"}; return a }, "deny", ReasonWorkspaceNotAllowed},
		{"missing permission", func(_ *Facts, a *App) *App { a.RequiredEntitlements = []string{"agent-studio:open"}; return a }, "deny", ReasonMissingPermission},
		{"explicit deny wins", func(f *Facts, _ *App) *App { f.Overrides = []Override{{"flowcase:open", "deny"}}; return nil }, "deny", ReasonExplicitDeny},
		{"allow override", func(f *Facts, a *App) *App {
			f.Overrides = []Override{{"kms:open", "allow"}}
			a.RequiredEntitlements = []string{"kms:open"}
			return a
		}, "allow", ReasonOverrideAllow},
		{"sensitive allow ignored", func(f *Facts, a *App) *App {
			f.Overrides = []Override{{"registry:manage", "allow"}}
			a.RequiredEntitlements = []string{"registry:manage"}
			return a
		}, "deny", ReasonMissingPermission},
	}
	for _, c := range cases {
		f, a := facts(), app()
		if na := c.f(&f, a); na != nil {
			a = na
		}
		d := DecideApp(f, a, "cid", now)
		if d.Effect != c.effect || d.ReasonCode != c.reason {
			t.Errorf("%s: got %s/%s want %s/%s", c.name, d.Effect, d.ReasonCode, c.effect, c.reason)
		}
		if d.PolicyVersion != "v1" || d.DecisionID == "" {
			t.Errorf("%s: decision missing metadata", c.name)
		}
	}
	if d := DecideApp(facts(), nil, "c", now); d.ReasonCode != ReasonAppUnpublished {
		t.Error("unknown app should look unpublished")
	}
}

func TestPlatformPermissionsNeverFromWorkspaceRoles(t *testing.T) {
	f := facts()
	f.WorkspaceRoles = append(f.WorkspaceRoles, BoundRole{Key: "x", Scope: "workspace", Permissions: []string{"registry:manage"}})
	if DecideTenantPermission(f, "registry:manage", "c", time.Now()).Allowed() {
		t.Fatal("workspace role granted a tenant permission")
	}
	f.TenantRoles = []BoundRole{{Key: "platform_admin", Scope: "tenant", Permissions: []string{"registry:manage"}}}
	if !DecideTenantPermission(f, "registry:manage", "c", time.Now()).Allowed() {
		t.Fatal("tenant role not honoured")
	}
	f.PrincipalType = "service"
	if DecideTenantPermission(f, "registry:manage", "c", time.Now()).Allowed() {
		t.Fatal("service principal got platform admin")
	}
}
