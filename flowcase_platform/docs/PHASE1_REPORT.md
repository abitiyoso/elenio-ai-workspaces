# Phase 1 — Verification Report (Workflow registry, compiler, validator)

Date: 2026-10-30. Scope: `CODEX_MASTER_PROMPT.md` → "Phase 1 — Workflow registry, compiler and validator".
No case execution, tasks or approvals runtime (Phase 2).

## Delivered

| Item | Where |
|---|---|
| Definition model, canonical SHA-256 hashing (lifecycle `status` excluded so publishing never changes the hash) | `internal/workflows/definition` |
| Safe condition language: structured objects only, allowlisted operators (`eq ne gt gte lt lte in not_in exists and or not`), depth ≤ 5, ≤ 50 operators, deterministic evaluation; string/JS expressions rejected | `definition/expression.go` |
| Compiler: graph with successors/predecessors, split→join pairing, join expected-branch counts, topological order (reused by the Phase 2 engine) | `internal/workflows/compiler` |
| Validator: JSON-schema structure with JSON-pointer paths, then semantic rules (see below) | `internal/workflows/validator` |
| Catalog of registered refs + domain packs (SIM: G0–G5 provisional policies, six specialist agents, roles, artifact types; PoC: `poc.budget`, roles); templates embedded from `samples/` (drift-tested) | `internal/workflows/catalog`, `internal/packs` |
| Registry service: create (template / import / blank), duplicate, fork next draft, edit draft (`If-Match`), validate, publish (human publisher only), deprecate; one open draft per workflow; idempotency keys; audit + outbox in the same transaction | `internal/workflows/registry`, migration `000004` |
| REST API (19 operations), error mapping (403/404/409/412/422/428), OpenAPI 3.1 | `internal/api/workflows.go`, `api/openapi.yaml` |
| Dev seeding imports both packs through the audited registry (SIM v1 + PoC v1 published, SIM v2 draft) | `cmd/migrate seed-dev` |
| UI: Workflow Library (filters, search, create from template), workflow detail with version history, duplicate, deprecate; Designer with layered DAG + SVG connectors, inspector (editable for drafts only), validate, save with `If-Match` and conflict banner, publish confirmation, export JSON; Audit page; Overview KPIs/activity from the API | `web/src/pages`, `web/src/features/designer` |
| VM deployment (tailnet-only dev stack) with re-deploy script | `docker-compose.vm.yml`, `web/Dockerfile`, `web/nginx.conf`, `deploy/deploy-vm.sh` |

### Validation rules (WCP-02, UAT-08, UAT-11)

Structure: allowed top-level/node properties only, `schema_version` 1.0, key/ID patterns, node type enum,
safe data-schema subset for `input_schema`/`form_schema` (no `$ref`, no `pattern`), size limits (256 KiB, 200 nodes).
Graph: unique IDs, exactly one start with no incoming edges, ≥ 1 end with no outgoing edges, existing targets,
no self-loops, per-type outgoing edge counts, every node reachable, every node reaches an end, no cycles
(rework is expressed by `human_approval.return_to`, checked to point at an earlier step in the same parallel region).
Parallelism: each split has exactly one `join: "all"` naming it; every branch path reaches the join; nothing enters
the region or the join from outside. Conditions: exhaustive `true`/`false` branches equal to `next`, typed against
`input_schema` (numeric operators on non-numeric fields are errors, undeclared fields warnings).
References: per-type config allowlist (`url`, `script`, `sql`, … give an explicit `unsafe_config` error),
registered policy/agent/connector/operation/role/artifact type, agent `context_sources` allowlist, approval
`policy_ref` declared in `policy_refs`. Provisional policies produce warnings, never silent acceptance.
`subworkflow` is rejected as deferred (documented).

## Verification actually executed

| Check | Result |
|---|---|
| `make check` (gofmt, vet, Go unit + PostgreSQL integration with `FLOWCASE_REQUIRE_DB=1`, web typecheck, ESLint 0 warnings, Vitest, build) | **pass** |
| Go tests incl. subtests | 146 passed, 0 failed, 0 skipped |
| Frontend tests | 24 passed |
| OpenAPI: YAML parses, all `$ref`s resolve, every documented operation routed (contract test) | pass |
| Local end-to-end in browser against real API/DB: edit step → save (ETag) → server validate → publish v2 via dialog → read-only + "new draft" offered → audit stream shows each action | pass |
| VM redeploy: migration 000004 applied, packs seeded, API on current image, `/readyz` ready, Library/detail render, no console errors | pass |
| GitHub Actions | not executed (no remote repository) |

## Tests for the Phase 1 exit criteria

| Criterion | Test |
|---|---|
| Missing edges | `TestDefectsAreRejected/missing_edge_target`, `…/self_loop`, `…/edge_into_start` |
| Invalid conditions | `…/string_expression`, `…/javascript_operator`, `…/extra_expression_property`, `…/numeric_comparison_with_string`, `…/comparison_on_string_field`, `…/too_deep`, `…/non_exhaustive_condition`, `…/branches_disagree_with_next`; `TestExpressionEvaluation` |
| Unmatched joins | `…/split_without_join`, `…/join_referencing_non-split`, `…/branch_bypasses_join`, `…/jump_into_parallel_region`, `…/unsupported_join_mode` |
| Publication immutability | `TestPublishedVersionsCannotBeEdited` (service + DB trigger), `TestWorkflowLifecycleOverHTTP` (409 `immutable_version`), `TestPublishedWorkflowVersionIsImmutable` |
| Version pinning | `TestCasePinnedToV1AfterV2Publish` (case row stays on v1 hash; v1 content unchanged after v2 publish) |
| UAT-08 unknown policy blocks publish | `…/unknown_policy_(UAT-08)`, `TestDraftLifecycleWithConcurrencyAndValidation` |
| Authorization (role, human-only publish, cross-tenant) | `TestAuthorization`, HTTP 403s in `TestWorkflowLifecycleOverHTTP` |
| Optimistic concurrency / idempotency | `TestDraftLifecycleWithConcurrencyAndValidation`, `TestIdempotentCreateAndDuplicate` |
| Audit + outbox on publish | `TestPublishWritesAuditAndOutbox` |
| Two domain packs, no workflow-key branches | `TestSamplePacksAreValid`, `TestSeedImportsBothPacks` (registry/validator contain no pack-specific code) |

## Issues found and fixed during this phase

- VM: `crypto.randomUUID` is unavailable over plain HTTP on a non-localhost origin → request ids now use `getRandomValues` (regression test added).
- VM deploy: compose did not recreate services sharing the rebuilt backend image → deploy script force-recreates app services (Postgres untouched).
- Designer layout: orphan nodes could push real nodes into the overflow row → in-degrees counted only from start-reachable nodes (tested).

## Decisions to confirm

1. **Author vs publisher separation**: the same human may currently author and publish if they hold both roles (dev identity does). If SoD is required for publication, it is a small server rule.
2. **Drafts may be saved with semantic errors** (structural errors are rejected); publish always re-validates. Confirm this editing model.
3. **Provisional policies**: SIM G0–G5 bindings publish with warnings. If publishing against provisional policies should be blocked in some environments, add a config switch.
4. **Audit visibility**: Phase 1 limits the audit log to designers, publishers and tenant admins; case-scoped audit arrives with Phase 2.

## Still pending externally

Unchanged from Phase 0: host token contract, KMS retrieval API, AI runtime contract, business G0–G5/DoA policy, production operations choices (`docs/CONTRACTS.md`).
