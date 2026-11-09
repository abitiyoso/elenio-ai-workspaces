package integration

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

func TestCatalogLeastPrivilegeAndDirectRouteDenied(t *testing.T) {
	e := newEnv(t, envOptions{})
	dimas := e.as(subDimas)
	me := dimas.refreshCSRF()
	mustEqual(t, slices.Equal(appKeys(me), []string{"case-workspace", "flowcase"}), true, "dimas catalog "+join(appKeys(me)))

	r := expectStatus(t, dimas.do(http.MethodGet, "/api/v1/apps/agent-studio/launch", nil, nil), 403)
	mustEqual(t, r["code"].(string), "MISSING_PERMISSION", "launch reason")
	resp := dimas.do(http.MethodGet, "/apps/agent-studio/", nil, map[string]string{"Accept": "text/html"})
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 403, "direct route to agent studio")
	resp = dimas.do(http.MethodGet, "/apps/agent-studio/api/v1/agents", nil, nil)
	resp.Body.Close()
	mustEqual(t, resp.StatusCode, 403, "direct API route to agent studio")
	// Unknown and draft apps are indistinguishable (no registry enumeration).
	expectStatus(t, e.as(subRafi).do(http.MethodPost, "/api/v1/admin/apps", map[string]any{"manifest": manifest("insights", "1.0.0"), "reason": "register draft"}, nil), 201)
	r = expectStatus(t, dimas.do(http.MethodGet, "/api/v1/apps/insights/launch", nil, nil), 403)
	mustEqual(t, r["code"].(string), "APP_UNPUBLISHED", "draft app")
	r = expectStatus(t, dimas.do(http.MethodGet, "/api/v1/apps/does-not-exist/launch", nil, nil), 403)
	mustEqual(t, r["code"].(string), "APP_UNPUBLISHED", "unknown app")
	if e.auditCount("authz.denied") < 4 {
		t.Fatal("route denials were not audited")
	}

	// Unauthenticated browser navigation is sent to login with a safe return path.
	resp = e.browser().do(http.MethodGet, "/apps/flowcase/cases?id=1", nil, map[string]string{"Accept": "text/html"})
	resp.Body.Close()
	mustEqual(t, resp.Header.Get("Location"), "/auth/login?return_to=%2Fapps%2Fflowcase%2Fcases%3Fid%3D1", "login redirect")
	for _, p := range []string{"/apps/flowcase/../admin", "/apps/flowcase/%2e%2e/x", "/apps/Flow/", "/apps/flowcase/a%2fb"} {
		resp = dimas.do(http.MethodGet, p, nil, nil)
		resp.Body.Close()
		if resp.StatusCode != 404 && resp.StatusCode != 301 && resp.StatusCode != 307 {
			t.Fatalf("%s: status %d, want 404/redirect", p, resp.StatusCode)
		}
	}
}

func TestWorkspaceSwitchRequiresMembership(t *testing.T) {
	e := newEnv(t, envOptions{})
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	me := maya.refreshCSRF()
	mustEqual(t, slices.Equal(appKeys(me), []string{"case-workspace", "flowcase", "kms"}), true, "maya sim-b2b catalog "+join(appKeys(me)))

	r := expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("engineering")}, nil), 403)
	mustEqual(t, r["code"].(string), "NOT_MEMBER", "switch to non-member workspace")
	r = expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": "00000000-0000-4000-8000-000000000000"}, nil), 403)
	mustEqual(t, r["code"].(string), "NOT_MEMBER", "unknown workspace looks the same")
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]any{"workspace_id": e.workspaceID("legal-ops"), "user_id": e.userID(subRafi)}, nil), 400)

	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("legal-ops")}, nil), 204)
	me = maya.refreshCSRF()
	mustEqual(t, slices.Equal(appKeys(me), []string{"kms"}), true, "maya legal-ops catalog "+join(appKeys(me)))
	r = expectStatus(t, maya.do(http.MethodGet, "/api/v1/apps/flowcase/launch", nil, nil), 403)
	mustEqual(t, r["code"].(string), "MISSING_PERMISSION", "flowcase in legal-ops")
	if e.auditCount("session.workspace_switched") != 2 {
		t.Fatal("workspace switches not audited")
	}
}

func TestCaseWorkerCannotAdminister(t *testing.T) {
	e := newEnv(t, envOptions{})
	dimas := e.as(subDimas)
	reason := map[string]any{"reason": "attempted escalation"}
	checks := []struct {
		method, path string
		body         any
	}{
		{"PUT", "/api/v1/admin/memberships/" + e.membershipID(subDimas, "sim-b2b") + "/roles", map[string]any{"role_key": "workspace_admin", "reason": "self escalate"}},
		{"PUT", "/api/v1/admin/memberships/" + e.membershipID(subMaya, "sim-b2b") + "/roles", map[string]any{"role_key": "agent_author", "reason": "grant"}},
		{"PUT", "/api/v1/admin/users/" + e.userID(subDimas) + "/tenant-roles", map[string]any{"role_key": "platform_admin", "reason": "self escalate"}},
		{"POST", "/api/v1/admin/apps/kms/disable", reason},
		{"POST", "/api/v1/admin/apps", map[string]any{"manifest": map[string]any{}, "reason": "x-app"}},
		{"GET", "/api/v1/admin/audit", nil},
		{"GET", "/api/v1/admin/users", nil},
		{"GET", "/api/v1/admin/integrations", nil},
		{"POST", "/api/v1/admin/users/" + e.userID(subMaya) + "/suspend", reason},
		{"POST", "/api/v1/admin/workspaces", map[string]any{"key": "evil", "name": "Evil", "description": "", "reason": "nope"}},
		{"POST", "/api/v1/admin/access-preview", map[string]any{"user_id": e.userID(subRafi), "workspace_id": e.workspaceID("sim-b2b"), "app_key": "kms"}},
	}
	for _, c := range checks {
		hdr := map[string]string{}
		if c.path == "/api/v1/admin/apps/kms/disable" {
			hdr["If-Match"] = `"1"`
		}
		resp := dimas.do(c.method, c.path, c.body, hdr)
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("%s %s: status %d, want 403", c.method, c.path, resp.StatusCode)
		}
	}
	me := dimas.refreshCSRF()
	caps := me["capabilities"].(map[string]any)
	if caps["manage_registry"].(bool) || caps["view_audit"].(bool) || len(caps["manage_memberships"].([]any)) != 0 {
		t.Fatalf("case worker capabilities too broad: %v", caps)
	}
}

func TestSelfGrantForbiddenAndDualControl(t *testing.T) {
	e := newEnv(t, envOptions{})
	rafi, nadia := e.as(subRafi), e.as(subNadia)
	// Self grant is refused even for a platform admin.
	r := expectStatus(t, rafi.do(http.MethodPut, "/api/v1/admin/memberships/"+e.membershipID(subRafi, "sim-b2b")+"/roles",
		map[string]any{"role_key": "agent_ops", "reason": "self grant attempt"}, nil), 403)
	mustEqual(t, r["code"].(string), "SELF_GRANT_FORBIDDEN", "self grant")

	// Non-sensitive role: granted directly.
	r = expectStatus(t, rafi.do(http.MethodPut, "/api/v1/admin/memberships/"+e.membershipID(subDimas, "sim-b2b")+"/roles",
		map[string]any{"role_key": "agent_ops", "reason": "release requests for pilot"}, nil), 201)
	mustEqual(t, r["outcome"].(string), "granted", "non-sensitive grant")

	// Sensitive role: pending until an independent approver decides.
	r = expectStatus(t, rafi.do(http.MethodPut, "/api/v1/admin/memberships/"+e.membershipID(subMaya, "sim-b2b")+"/roles",
		map[string]any{"role_key": "workspace_admin", "reason": "delegate sim-b2b membership admin"}, nil), 202)
	mustEqual(t, r["outcome"].(string), "pending_approval", "sensitive grant")
	reqID := r["request"].(map[string]any)["id"].(string)
	r = expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/grant-requests/"+reqID+"/approve", map[string]any{"reason": "approving my own"}, nil), 403)
	mustEqual(t, r["code"].(string), "SELF_APPROVAL_FORBIDDEN", "self approval")
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/admin/grant-requests/"+reqID+"/approve", map[string]any{"reason": "approving my own grant"}, nil), 403)
	// The database CHECK backs the rule even if the service check were bypassed.
	if _, err := e.pool.Exec(context.Background(), `UPDATE role_grant_requests SET decided_by = requested_by WHERE id = $1::uuid`, reqID); err == nil {
		t.Fatal("database allowed requester to decide own request")
	}
	r = expectStatus(t, nadia.do(http.MethodPost, "/api/v1/admin/grant-requests/"+reqID+"/approve", map[string]any{"reason": "independent review ok"}, nil), 200)
	mustEqual(t, r["state"].(string), "approved", "approved")

	// Maya is now a scoped workspace admin: allowed in sim-b2b, denied elsewhere and for tenant roles.
	me := maya.refreshCSRF()
	managed := me["capabilities"].(map[string]any)["manage_memberships"].([]any)
	mustEqual(t, len(managed), 1, "maya manages one workspace")
	expectStatus(t, maya.do(http.MethodGet, "/api/v1/admin/workspaces/"+e.workspaceID("sim-b2b")+"/members", nil, nil), 200)
	expectStatus(t, maya.do(http.MethodGet, "/api/v1/admin/workspaces/"+e.workspaceID("engineering")+"/members", nil, nil), 403)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/admin/workspaces/"+e.workspaceID("engineering")+"/members",
		map[string]any{"user_id": e.userID(subDimas), "reason": "cross workspace"}, nil), 403)
	expectStatus(t, maya.do(http.MethodPut, "/api/v1/admin/users/"+e.userID(subDimas)+"/tenant-roles",
		map[string]any{"role_key": "platform_admin", "reason": "escalate"}, nil), 403)
	expectStatus(t, maya.do(http.MethodGet, "/api/v1/admin/apps", nil, nil), 403)
	// A workspace admin cannot approve sensitive grants either.
	r = expectStatus(t, maya.do(http.MethodPut, "/api/v1/admin/memberships/"+e.membershipID(subDimas, "sim-b2b")+"/roles",
		map[string]any{"role_key": "workspace_admin", "reason": "co-admin for sim-b2b"}, nil), 202)
	reqID2 := r["request"].(map[string]any)["id"].(string)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/admin/grant-requests/"+reqID2+"/approve", map[string]any{"reason": "self-serve"}, nil), 403)
	if e.auditCount("role_grant.approved") != 1 || e.auditCount("role_binding.denied") < 1 {
		t.Fatal("grant decisions not audited")
	}

	// Lockout guard: the last platform admin binding cannot be revoked.
	var nadiaBinding, demoBinding, rafiBinding string
	_ = e.pool.QueryRow(context.Background(), `SELECT b.id::text FROM role_bindings b JOIN users u ON u.id=b.user_id WHERE u.idp_subject=$1 AND b.workspace_id IS NULL`, subNadia).Scan(&nadiaBinding)
	_ = e.pool.QueryRow(context.Background(), `SELECT b.id::text FROM role_bindings b JOIN users u ON u.id=b.user_id WHERE u.idp_subject=$1 AND b.workspace_id IS NULL`, subDemo).Scan(&demoBinding)
	_ = e.pool.QueryRow(context.Background(), `SELECT b.id::text FROM role_bindings b JOIN users u ON u.id=b.user_id WHERE u.idp_subject=$1 AND b.workspace_id IS NULL`, subRafi).Scan(&rafiBinding)
	expectStatus(t, rafi.do(http.MethodDelete, "/api/v1/admin/role-bindings/"+nadiaBinding, map[string]any{"reason": "offboarding nadia"}, nil), 204)
	expectStatus(t, rafi.do(http.MethodDelete, "/api/v1/admin/role-bindings/"+demoBinding, map[string]any{"reason": "offboarding the demo account"}, nil), 204)
	r = expectStatus(t, rafi.do(http.MethodDelete, "/api/v1/admin/role-bindings/"+rafiBinding, map[string]any{"reason": "remove last admin"}, nil), 409)
	mustEqual(t, r["code"].(string), "LAST_PLATFORM_ADMIN", "last admin")
	// Nadia lost platform admin: the next request reflects it (no cached positive decision).
	expectStatus(t, nadia.do(http.MethodGet, "/api/v1/admin/audit", nil, nil), 403)
}

func TestRevocationSuspensionAndOverridesApplyImmediately(t *testing.T) {
	e := newEnv(t, envOptions{})
	rafi := e.as(subRafi)
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 204)
	expectStatus(t, maya.do(http.MethodGet, "/api/v1/apps/kms/launch", nil, nil), 200)

	// Explicit deny wins over role grant.
	o := expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/workspaces/"+e.workspaceID("sim-b2b")+"/overrides",
		map[string]any{"user_id": e.userID(subMaya), "permission_key": "kms:open", "effect": "deny", "reason": "legal hold on sources"}, nil), 201)
	r := expectStatus(t, maya.do(http.MethodGet, "/api/v1/apps/kms/launch", nil, nil), 403)
	mustEqual(t, r["code"].(string), "EXPLICIT_DENY", "override deny")
	expectStatus(t, rafi.do(http.MethodDelete, "/api/v1/admin/overrides/"+o["id"].(string), map[string]any{"reason": "hold lifted"}, nil), 204)
	expectStatus(t, maya.do(http.MethodGet, "/api/v1/apps/kms/launch", nil, nil), 200)
	// Allow overrides cannot confer sensitive permissions.
	r = expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/workspaces/"+e.workspaceID("sim-b2b")+"/overrides",
		map[string]any{"user_id": e.userID(subMaya), "permission_key": "registry:manage", "effect": "allow", "reason": "shortcut"}, nil), 403)
	mustEqual(t, r["code"].(string), "SENSITIVE_ALLOW_FORBIDDEN", "sensitive allow")

	// Suspend membership: next decision denies, session workspace is cleared.
	mid := e.membershipID(subMaya, "sim-b2b")
	expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/memberships/"+mid+"/suspend", map[string]any{"reason": "investigation"}, nil), 200)
	r = expectStatus(t, maya.do(http.MethodGet, "/api/v1/apps/kms/launch", nil, nil), 403)
	if r["code"] != "NOT_MEMBER" && r["code"] != "MEMBERSHIP_SUSPENDED" {
		t.Fatalf("suspended membership code %v", r["code"])
	}
	r = expectStatus(t, maya.do(http.MethodPost, "/api/v1/session/workspace", map[string]string{"workspace_id": e.workspaceID("sim-b2b")}, nil), 403)
	mustEqual(t, r["code"].(string), "MEMBERSHIP_SUSPENDED", "switch to suspended")
	// Revoke: role bindings go with it.
	expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/memberships/"+mid+"/revoke", map[string]any{"reason": "left the team"}, nil), 200)
	var live int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM role_bindings WHERE user_id = $1::uuid AND workspace_id = $2::uuid AND state = 'active'`,
		e.userID(subMaya), e.workspaceID("sim-b2b")).Scan(&live)
	mustEqual(t, live, 0, "bindings after revoke")

	// Suspend user: every session dies immediately and login is refused.
	dimas := e.as(subDimas)
	r = expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/users/"+e.userID(subRafi)+"/suspend", map[string]any{"reason": "self"}, nil), 403)
	mustEqual(t, r["code"].(string), "SELF_ACTION_FORBIDDEN", "self suspend")
	expectStatus(t, rafi.do(http.MethodPost, "/api/v1/admin/users/"+e.userID(subDimas)+"/suspend", map[string]any{"reason": "security incident"}, nil), 200)
	expectStatus(t, dimas.do(http.MethodGet, "/api/v1/me", nil, nil), 401)
	b := e.browser()
	code, state := e.idp.authorize(t, b.startLogin(""), subDimas)
	mustEqual(t, loginError(t, b.callback(code, state, nil)), "subject_disabled", "suspended login")
	var pending int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE event_type IN ('identity.user_suspended', 'membership.revoked', 'membership.suspended')`).Scan(&pending)
	mustEqual(t, pending, 3, "propagation events in outbox")
}

func TestAuthzCheckIsCallerScopedAndAuditIsAppendOnly(t *testing.T) {
	e := newEnv(t, envOptions{})
	maya := e.as(subMaya)
	expectStatus(t, maya.do(http.MethodPost, "/api/v1/authz/check", map[string]any{"action": "app:open", "resource_type": "mini_app",
		"resource_id": "kms", "subject_id": e.userID(subRafi)}, nil), 400)
	d := expectStatus(t, maya.do(http.MethodPost, "/api/v1/authz/check", map[string]any{"action": "registry:manage", "resource_type": "platform_admin", "resource_id": "x"}, nil), 200)
	mustEqual(t, d["effect"].(string), "deny", "maya registry")
	mustEqual(t, d["subject_id"].(string), e.userID(subMaya), "decision subject")
	if d["policy_version"].(string) == "" || d["decision_id"].(string) == "" {
		t.Fatal("decision lacks policy version / id")
	}
	// Platform admin is not a domain approver: there is no platform permission for gate approval at all.
	rafi := e.as(subRafi)
	d = expectStatus(t, rafi.do(http.MethodPost, "/api/v1/authz/check", map[string]any{"action": "case:approve", "resource_type": "platform_admin", "resource_id": "x"}, nil), 200)
	mustEqual(t, d["effect"].(string), "deny", "platform admin case:approve")

	if _, err := e.pool.Exec(context.Background(), `UPDATE platform_audit_events SET action = 'tampered'`); err == nil {
		t.Fatal("audit update allowed")
	}
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM platform_audit_events`); err == nil {
		t.Fatal("audit delete allowed")
	}
	page := expectStatus(t, rafi.do(http.MethodGet, "/api/v1/admin/audit?limit=2", nil, nil), 200)
	if len(page["items"].([]any)) != 2 || page["next_cursor"] == nil {
		t.Fatalf("audit pagination: %v", page)
	}
	next := expectStatus(t, rafi.do(http.MethodGet, "/api/v1/admin/audit?limit=2&cursor="+page["next_cursor"].(string), nil, nil), 200)
	if len(next["items"].([]any)) == 0 {
		t.Fatal("audit second page empty")
	}
}

func join(s []string) string {
	out := ""
	for _, x := range s {
		out += x + ","
	}
	return out
}
