# CODEX ADD-ON PROMPT — Model & Provider Management (Go + Eino)

Use the supplied `AI_AGENT_STUDIO_TECHNICAL_PRD.md` v1.1, section 18, and updated `Agent_Studio_Eino_Prototype.html` as ground truth for the LLM setup UX. Implement this as part of Agent Studio, not KMS or Flowcase. Start with a short ADR and migration/API plan, then code only after checking the current repo and pinned Eino APIs. Do not silently assume external providers work.

## Deliverables

1. React **Models & Providers** page: provider list, create/edit endpoint metadata, admin-scoped credential write-only setup, redacted credential status, test connection and capability result, model deployments, approval, disable/revoke, deployment assignment per agent draft, cost/quota/timeouts. Match the supplied navy/cobalt reference.
2. Go provider/model registry backed by PostgreSQL migrations: `llm_providers`, `llm_model_deployments`, `agent_model_bindings`, `model_health_checks`, `model_usage_records`, audit/approval history. No secrets plaintext in Postgres, logs or browser storage; store only `secret_ref` or encrypted references per company policy.
3. Server-side provider adapter interface that creates a compatible Eino ChatModel. Implement one end-to-end production provider based on confirmed infrastructure; leave additional provider adapters pluggable, but never falsely present an unavailable adapter as connected.
4. API routes for providers, write-only credential rotation, capability tests, model deployments, approval, agent model draft binding and health. Enforce Platform Admin permissions and tenant/workspace scope; verify approved deployment and `secret_ref` on every run.
5. Resolve a pinned model deployment at run creation / worker startup, validate tool-calling / structured-output compatibility and usage caps, and pass the ChatModel to Eino ADK `ChatModelAgent` / `Runner`.
6. Editing a draft agent's model binding invalidates its previous evaluation and domain signoff. Published versions remain immutable; expired/revoked credentials/models fail closed without unapproved fallback.
7. Integration tests for cross-tenant isolation, missing credentials, SSRF/host restrictions, revoked model rejection, secret non-disclosure, missing tool-calling support, approved deployment binding, token/time quotas and evaluation invalidation.

## Constraints

- The mini-app uses parent-app SSO. No standalone login page.
- Model credentials never reach Flowcase or KMS.
- No model API calls directly from React. No API key stored in localStorage.
- Restrict outbound HTTP/TLS destinations with explicit server-side allowlists and secure DNS/IP checks; special handling for approved self-hosted endpoints.
- Model IDs and capability claims must be verified against an actual provider; seed entries in HTML are illustrative only.
- CLI/env development credentials may be used only under documented local dev guards.
- Use Eino's current version actually pinned in the repository. Verify constructor signatures by building and testing.
- Stop after this module, report files changed, tests actually run and real vs mocked integrations.
