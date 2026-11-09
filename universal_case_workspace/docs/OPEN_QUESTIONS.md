# Open questions and README "Required decisions"

## Required decisions (README_START_HERE.md) — answers
1. **Does Flowcase own structured artifact revisions or only a reference registry?** It owns them: `artifacts` +
   `artifact_versions` with JSON `content`, `checksum`, `produced_by_kind`, status, evidence refs and dependencies, and
   `POST /api/v1/cases/{id}/artifacts/{artifact}/versions` with `If-Match` creates a new version. UCW reuses Flowcase
   (`artifact_provider: flowcase_registry`); no UCW artifact store. Editing through UCW is deferred to Phase 3.
2. **Where do workspace definition versions live?** In UCW's own PostgreSQL (`workspace_pack_versions`, immutable),
   loaded from `domain-packs/*.json`. See DOMAIN_PACK_ADR.md.
3. **Control Plane delegated token and Flowcase case-member contract?** Inbound ES256 token aud=case-workspace;
   UCW exchanges it at the CP token exchange for an aud=flowcase user token and calls Flowcase as the user. Flowcase
   decides case visibility (owner/participant/admin rules in Flowcase); UCW never infers membership. See AUTH_BOUNDARY.md.
4. **How are domain actions validated/authorized?** Manifests can only reference allowlisted executors, Flowcase
   commands, agents in `bindings.agent_keys` and a fixed permission vocabulary; they never grant. Every forwarded action is
   authorized again by the owning service (Flowcase drafts/approvals, Agent Studio runs). In the MVP the only forwarded
   actions are draft submit/revision and intake case creation; pack `actions` are displayed, not executed.

## Open
- **Token exchange endpoint** is being implemented by the CP agent; UCW follows the agreed contract and must be
  re-verified against the merged CP code (status codes, `expires_in`, Basic client id `case-workspace`).
- **CP proxy route** `/apps/case-workspace/` → `127.0.0.1:8480` (audience `case-workspace`) and the app registration are
  CP work; `deploy/case-workspace-cp/deploy.sh` expects `~/control-plane/secrets/token_exchange_case_workspace`.
- **Contract Review / PoC packs are not live.** No `legal-review` or `poc-request` workflow and none of their agents
  exist. Flowcase has `internal-poc-request`; re-binding the PoC pack to it is a pack-owner decision (new pack version
  with matching artifact types), not done silently.
- **Artifact editing** (Flowcase `reviseArtifact` with `If-Match`) — Phase 3, with field-level validation from the pack.
- **Approvals and human tasks** are decided in Flowcase (links in the case context); UCW renders them read-only.
- **KMS documents panel** deferred: no KMS call in the MVP; the documents view lists sources cited in artifacts/intake.
  A `KnowledgeProvider` adapter must call KMS as the user (or delegated) and hide sources the user cannot read.
- **Agent availability** for a pack cannot be checked with UCW's run-only Agent Studio grant; a missing/unreleased
  intake agent shows as a failed turn (`agent_studio_rejected` / friendly `last_error`).
- **Exchanged-token revocation**: cached Flowcase tokens live until expiry after a CP logout (bounded by token TTL).
- **Flowcase intake removal**: Flowcase's `internal/intake` and `/api/v1/intake-sessions` should be removed once the
  SPA uses UCW's intake; existing Flowcase intake sessions are not migrated.
- **Pack publishing** (admin API, compatibility report, signatures) is not in the MVP; new versions ship as files.
