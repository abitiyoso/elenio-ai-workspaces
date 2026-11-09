# Agent Studio Phase 0 Architecture Decisions

Date: 2026-10-30  
Status: Proposed for implementation; external contract decisions remain pending.

## ADR-001: Runtime, workflow ownership, and integration boundaries

### Decision

Agent Studio is a reusable Go service with a React mini-app. Its runtime uses Eino ADK `ChatModelAgent` and `Runner` for bounded, version-pinned agent runs. Agent Studio owns agent, skill, tool, evaluation, release, run, trace, and audit metadata. Flowcase owns business workflow state, human tasks, approvals, and case artifact lifecycle. KMS owns parsing, indexing, source ACLs, and evidence retrieval.

An agent run can emit a validated draft artifact or findings for human review. It cannot approve a Flowcase gate, approve pricing, release a proposal, or impersonate an SME. Flowcase invocation and callback shapes remain proposals until the Flowcase team confirms them. Existing Flowcase `AgentExecutor` and KMS `KnowledgeProvider` interfaces are ports with disabled/development stubs, not live integrations.

### Authentication and identity trust boundary

- The parent workspace performs login. Agent Studio has no standalone login flow.
- The API verifies a trusted host/session token or an authenticated Flowcase service identity server-side. Request bodies, URL parameters, agent text, and caller-supplied identity/tenant/role headers never establish identity or delegated scope.
- Each run and tool call rechecks tenant, workspace/case, source, action, and tool permissions. KMS applies its own ACL using the verified principal/delegation.
- Do not map an agent service identity to a Flowcase approver. The host issuer, audience, JWKS/session exchange, claims, delegation format, and service-to-service mechanism are unresolved and must be confirmed before production auth is implemented.
- Development mock identity is accepted only in an explicitly configured local development environment and must fail closed in other environments.

### KMS contract decision

The repository contains an implemented KMS `POST /api/v1/search` endpoint. It accepts `query`, `mode`, optional `collection_ids`, `case_id`, and `top_k`; it uses the verified host identity and reauthorizes search hits before returning snippets. Results contain `document_id`, `version_id`, `source_anchor`, `citation`, `trust_status`, and `publication_status` among other fields. The endpoint supports keyword search; semantic/hybrid modes return an unavailable error if embeddings are not configured.

No `fetch_evidence` route was found. The proposed Agent Studio KMS schema uses `source_id`, `source_version`, `citation_anchor`, and statuses `conflicted`/`unverified`/`withdrawn`, which do not match the current search response. The runtime must not call KMS storage directly or claim a fetch-by-source/version contract. A dedicated evidence lookup operation and status mapping require KMS owner confirmation. `contracts/kms_search_request.schema.json` and `contracts/kms_search_response.schema.json` describe only the current public search API; `kms_evidence_response.schema.json` remains an unconfirmed future adapter proposal.

### Flowcase contract decision

The Flowcase API currently exposes foundation routes, not an agent invocation or callback API. `docs/CONTRACTS.md` explicitly marks the Agent Runtime contract pending; its `AgentExecutor` interface and development stub do not establish wire-level behavior or signed callback support. The `flowcase_agent_invocation` and `agent_completion_event` schemas remain proposals. Confirm pinned version selection, request authentication/delegation, case/artifact references, idempotency, callback signing and deduplication, cancellation, deadlines, and retry semantics with Flowcase before implementing a production adapter.

### Model provider and Eino decision

- Use Eino's model-neutral ChatModel interface; provider adapters are server-side. No vendor or model is hardcoded into agent manifests.
- A deployment must be tenant/environment approved and capability-checked before it can be bound to a draft agent. The worker resolves an immutable deployment snapshot and credential reference server-side for each run. Missing/revoked approval or credentials fail closed; no silent unapproved fallback.
- Provider endpoints are configured by administrators and constrained by TLS, outbound host policy, DNS/IP validation, and SSRF defenses. A prompt or tool result cannot choose a model, endpoint, or credential.
- The latest stable Eino release visible in the official repository on this review date is `v0.9.22` (released 2026-10-19); the repository's official ADK quickstart recommends the v0.9 line. v0.10 entries are alpha and are not selected for the initial stable runtime.
- Proposed baseline: Go module `github.com/cloudwego/eino v0.9.22`, Go language baseline `1.24.0` (matching the adjacent Flowcase and KMS modules). This is a proposed pin only: this package has no `go.mod`, and the sandbox could not reach `proxy.golang.org`, so dependency download and compile-time signature verification remain blocked. Add the pin and compile a minimal ADK adapter before treating it as accepted.
- Official docs show `adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{...})`, `adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, ...})`, and `runner.Query/Run` returning an event iterator. This is documentation-level confirmation, not a compile check against a resolved module.

### Tool policy

Tools are named, versioned server adapters with strict input/output schemas, explicit tenant and action scopes, per-call authorization, bounded timeout/call limits, and audit/redaction policy. KMS tools are read-only. Flowcase artifact tools may propose draft artifacts only. No arbitrary HTTP URL, SQL, shell, direct KMS database/Elasticsearch access, or model-supplied authorization is allowed. Retrieved source text is untrusted evidence and cannot modify system instructions or tool policy.

### Version and release policy

Published agent, skill, tool, and model-policy versions are immutable. Every run records the pinned versions and source references. Draft changes require sandboxing/evaluation, independent domain review, then separate AI Ops release approval. Publishing a new version affects eligible new runs only; it does not silently upgrade an in-flight Flowcase case or rewrite an approved artifact.

## Official references

- Eino ADK quickstart: https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_quickstart/
- Eino ADK Runner and extensions: https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_extension/
- Eino release list: https://github.com/cloudwego/eino/releases
- Adjacent KMS contract: `../kms_go_package/backend/openapi.yaml`
- Adjacent Flowcase contract notes: `../flowcase_platform/docs/CONTRACTS.md`
- Adjacent Flowcase API: `../flowcase_platform/api/openapi.yaml`

