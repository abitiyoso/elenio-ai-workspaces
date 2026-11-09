# Open decisions and blockers

| # | Decision | Current choice | Owner needed |
|---|---|---|---|
| 1 | Corporate IdP | None found → Keycloak 26 on the VM (`realm workspace`). Swap issuer/client via config when a corporate IdP exists. | Security/IAM |
| 2 | TLS for the tailnet VM | Not available (no `tailscale cert`, no sudo) → HTTP + explicit development acknowledgement. Production must terminate HTTPS (ingress) and use `__Host-` Secure cookies. | Platform ops |
| 3 | Tenancy | Single enterprise tenant (`acme-demo`), multiple workspaces; schema is tenant-safe. | Product |
| 4 | Session TTLs | Idle 30 min, absolute 8 h, flow 10 min, service token 5 min. | Security |
| 5 | Revocation SLO | Platform: next request. Downstream: ≤ token TTL (5 min) + optional outbox push. | Security + service teams |
| 6 | Sensitive roles / SoD | `platform_admin`, `workspace_admin` need an independent approver; self-grant always refused. Change-ticket field not yet required. | Security |
| 7 | Downstream user mapping | Flowcase: done via `deploy/flowcase-cp/provision.py` (explicit mapping file, run at deploy). Long-term owner (SCIM / outbox consumer / Flowcase admin UI) is open. KMS: open. | Service teams |
| 8 | Mini-app sub-path serving | Flowcase: `FLOWCASE_BASE_PATH` added. KMS/Agent Studio UIs still assume `/`. | Service teams |
| 9 | KMS verifier | Done: `AUTH_MODE=control_plane_jwt` + `tenant_principals`. Open: delegated service retrieval in this mode (two credentials in one header). | KMS team |
| 10 | Agent Studio identity | Done: `AGENT_STUDIO_HUMAN_AUTH=control_plane_jwt`. Open: http JWKS needs `AGENT_STUDIO_ENV=development`, which also enables the mock model; TLS for the CP would remove that. | AI Ops |
| 11 | Rate limiting at scale | In-memory per instance; multi-instance needs ingress or Redis limits. | Platform ops |
| 12 | Outbox consumer | Events are delivered to the structured log sink only; no message bus is configured. | Platform ops |
| 13 | Secret manager | File-mounted secrets (`*_FILE`); production should mount from Vault/KMS-backed CSI. | Platform ops |
| 17 | Agent run evidence in Flowcase | Agent Studio retrieves KMS evidence itself; it is not linked into Flowcase `artifact_evidence_refs`, so Flowcase stale-evidence tracking does not cover it yet. | Flowcase + AS teams |
| 18 | Proposal agent JSON reliability | One eval run returned prose instead of JSON for missing evidence; consider tightening the Proposal prompt/schema or a JSON-mode deployment. | AI Ops |
| 19 | `devtoken` operator tool | Signs as any user with the CP key (dev seeding only). Remove from production images; keep the key off operator machines. | Platform ops |
| 16 | Duplicate KMS / Agent Studio instances | `kms-cp` and `agent-studio-cp` hold separate data from `kms-phase1` / `agent-studio`; decide which instances survive. | Service teams |
| 15 | Two Flowcase instances | Standalone `flowcase` (:5180, dev identity, Flowcase team) and `flowcase-cp` (Control Plane, trusted_jwt) have separate data. Retire the standalone one, or switch it to trusted_jwt, once the Flowcase team agrees. | Flowcase team |
| 20 | Delegated token exchange | Implemented as a CP-local, allowlisted exchange (ADR-004): `case-workspace` → `flowcase`, `kms`. Open: secret rotation with overlap (one secret per client today), mTLS/private-key-JWT client auth instead of a shared secret, whether downstream services should restrict `act`-bearing tokens (e.g. no approvals via experience apps), and moving to the corporate IdP's RFC 8693 support if one is adopted. | Security + service teams |
| 21 | UCW instance | `case-workspace` is published in the dev registry and proxied to `127.0.0.1:8480`; until `case-workspace-cp` is deployed the route answers 502 "app is not responding". | UCW team |
| 14 | Keycloak demo users | Fictional users from `samples/`; passwords generated at deploy time and kept only in the VM secrets dir. A second platform admin (Nadia Kusuma) was added so dual control can be exercised. | Product |
