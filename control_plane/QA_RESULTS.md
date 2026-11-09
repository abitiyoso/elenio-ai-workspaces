# QA Report — AI Workspace Control Plane Prototype v1.0

**Date:** 30 October 2026. Browser: headless Chromium / Playwright. **Scope: local offline prototype only.** No real IdP, BFF, KMS, Flowcase or Agent Studio requests executed.

| Check | Result |
|---|---|
| HTML inline JavaScript syntax (`node --check`) | Passed |
| All JSON examples parse | Passed |
| Three JSON schemas validate under Draft 2020-12 | Passed |
| All four sample app manifests pass app schema | Passed |
| OpenAPI 3.1 YAML parses | Passed (10 paths) |
| Home renders three registered published apps for demo admin | Passed |
| App Catalog and admin users/workspace roles render | Passed |
| Access preview denies Maya → Agent Studio, allows Maya → KMS | Passed |
| Register draft app → publish locally → visible for entitled user | Passed |
| Disable local app → removed from Available catalog | Passed |
| Switch actor → visibility updates (Maya sees KMS+Flowcase) | Passed |
| Non-admin attempting People page sees access-denied UI | Passed |
| Switch Maya to Legal workspace → only KMS available | Passed |
| Audit records app lifecycle events | Passed |
| Mobile drawer and catalog navigation | Passed |
| JavaScript errors on tested routes | None |
| Responsive mobile layout | No global document overflow in 390px test viewport |

**Not tested and not claimed:** OIDC code exchange, PKCE/state/nonce, live session cookies, SQL migrations, identity provider provisioning, runtime authz middleware, downstream service verification, production app routing/proxy, load/failover, accessibility audit and deployment. These must be verified during implementation phases in `CODEX_MASTER_PROMPT.md`.
