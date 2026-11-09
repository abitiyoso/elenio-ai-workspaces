# RBAC and Platform Entitlement Model — Control Plane

## Rule zero: platform open != domain authorization

This layer answers **may user X discover/open app Y within workspace Z?** and **may admin X manage platform resources?** It does not decide whether a user may read a KMS document, approve a Flowcase gate, or publish an Agent Studio model/skill. Those are domain APIs' responsibility, even after platform entitlement passes.

## Built-in role templates (illustrative)

| Role | Core entitlements | Excludes by default |
|---|---|---|
| `platform_admin` | `platform:admin`, `registry:manage`, `memberships:manage`, `audit:view`, app:open for platform apps and `case-workspace:open` | KMS protected source read, customer case G0/G1 approval, agent release unless separately granted |
| `workspace_admin` | `workspace:manage`, `memberships:manage` within assigned workspace, `catalog:view` | global IdP/security settings, cross-workspace access |
| `knowledge_contributor` | `kms:open`, `kms:contribute`, `catalog:view` | other KMS collection ACL; Flowcase by default |
| `case_worker` | `flowcase:open`, `case-workspace:open`, `catalog:view` | any case not assigned or permitted in Flowcase |
| `case_reviewer` | `flowcase:open`, `case-workspace:open`, `review-inbox:view`, `catalog:view` | gate authorization without Flowcase policy |
| `agent_author` | `agent-studio:open`, `agent:author`, `catalog:view` | skill semantic signoff, AI Ops release and model approval |
| `agent_ops` | `agent-studio:open`, `agent:release-request`, `catalog:view` | independent semantic approval, platform global config |
| `workspace_viewer` | `catalog:view`, assigned read-only app opens | editing content or grants |

## Permission representation

An entitlement grant is scoped to `(tenant_id, workspace_id, subject_id, role_id, effective_at, expires_at)`. Role map yields permission strings. `mini_apps[].required_permissions` must all be satisfied to be shown/launchable. An explicit deny wins over allow, and suspended membership denies all workspace access. At resource level each app is responsible for its own policy.

**Example decision:**

```json
{
  "principal": "user-maya", "workspace": "sim-b2b", "app_key": "flowcase",
  "action": "app:open", "allow": true, "reason_code": "ROLE_GRANT",
  "effective_roles": ["case_worker"], "required_permissions": ["flowcase:open"]
}
```

**Second independent check:** if Maya requests `POST /cases/123/approve`, Flowcase must verify case scope, exact artifact version, `approval:decide`, policy and SoD; the platform `flowcase:open` entitlement is **not** sufficient.

## Permissions matrix for prototype role scenarios

| Demo user | Sample roles in SIM B2B | KMS | Flowcase | Agent Studio | Universal Case Workspace |
|---|---|---|---|---|---|
| Rafi Admin | platform_admin, workspace_admin | open | open | open | open |
| Nadia Admin (engineering only) | platform_admin (tenant) | open | open | open | open (in engineering) |
| Maya Analyst | knowledge_contributor, case_worker | open | open | deny | open |
| Dimas Reviewer | case_reviewer, case_worker | deny | open | deny | open |
| Sari Agent Builder | agent_author, knowledge_contributor | open | deny | open | deny |

Universal Case Workspace (`case-workspace` 0.2.0) is **published** in the development registry. Grants: `case_worker`
(since `rbac-2026.10.1`), plus `case_reviewer` and `platform_admin` (migration `0003`, policy `rbac-2026.10.2`).
Sari is deliberately left without it (no case role), which also keeps a negative subject for tests.

Opening UCW grants nothing in Flowcase or KMS by itself. UCW reads Flowcase/KMS data through the Control Plane
token exchange (ADR-004), which re-evaluates `flowcase:open` / `kms:open` for the user in the active workspace on
every exchange: Dimas sees Flowcase data in UCW but cannot obtain a KMS token (`TARGET_APP_DENIED`), and a deny
override or revocation applies to the next exchange.

## Role assignment policy

- Only tenant-scoped `platform_admin` can assign global platform roles or publish registry items. Workspace admins can grant workspace roles within their own membership/scope; cannot self-escalate to platform admin.
- Avoid allowing users to grant sensitive roles to themselves. For high-risk entitlement grants, use an independent reviewer and reason/change ticket.
- Grant/deny decision and grant expiry are audited; app catalog and sessions should invalidate on revoke.
- A UI role selector in demo **simulates** different entitlement views. It does not impersonate a real identity and may not be copied into production authorization.

## Negative tests

1. `case_worker` guesses direct `/apps/agent-studio/` URL: deny server-side, not just hidden tile.
2. `knowledge_contributor` opens KMS but tries another tenant's source: KMS returns deny.
3. `platform_admin` tries G1 approval without Flowcase policy: deny.
4. `agent_author` attempts to publish skill without domain-review + Ops release: deny.
5. `workspace_admin` tries role grant outside own workspace: deny.
6. User with suspended membership cannot switch to that workspace and cannot launch any apps there.
7. A disabled app denies existing route access even if user has a stale app entitlement.
8. Change active workspace requires membership and CSRF; browser-supplied workspace ID alone is insufficient.
9. Service-to-service token cannot be elevated into a human session or human approval.
10. An experience app cannot use the token exchange to reach a core the user cannot open directly, to exchange a
    token minted for another app, or to re-exchange an exchanged (`act`) token.
