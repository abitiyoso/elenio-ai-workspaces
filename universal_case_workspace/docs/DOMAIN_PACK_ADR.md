# ADR: Domain pack registry, schema extensions and pinning

Status: accepted (MVP) · 2026-10-31

## Context
Packs (workspace definitions) describe navigation, artifact types, actions and now the intake brief of one case
domain. The PRD left open whether the registry lives in the Control Plane database or a UCW database, and how a case
is pinned to a pack version.

## Decisions

1. **Separate service, own database.** UCW is a dedicated Go service `case-workspace` (module
   `github.com/elenio/case-workspace`, binary `cmd/workspace-api`) with a small PostgreSQL it alone owns
   (`workspace_pack_versions`, `workspace_case_bindings`, `workspace_intake_sessions`, `workspace_audit_events`).
   No other service's database is read or written. The Control Plane keeps identity, sessions and the app route;
   folding UCW state into the CP database would couple CP releases to pack evolution.
2. **Packs are files, versions are immutable rows.** At startup `UCW_PACKS_DIR/*.json` is validated against
   `contracts/workspace_definition.schema.json` (santhosh-tekuri/jsonschema/v6, external refs refused) and the semantic
   validator (`internal/packs/validate.go`):
   unique navigation/artifact/action/field/intake keys; `artifact_list` targets exist; actions' and intake's agent in
   `bindings.agent_keys`; executors and Flowcase command keys allowlisted; `required_permission` from a fixed vocabulary
   (`artifact:read|edit, agent:invoke, task:request|complete, approval:decide, case:note, document:read`);
   extension refs only from a compiled registry (empty in the MVP); every string (and key) rejected if it contains a
   URL/scheme, markup (`<`, `>`, backtick), event handlers, script calls, template syntax or SQL statements;
   one Flowcase artifact type per pack artifact type; exactly one required text intake field maps to `title`.
   A valid version is inserted into `workspace_pack_versions` with `sha256:` of its canonical JSON (sorted keys, so
   reformatting is not drift). Rows are insert-only (trigger blocks UPDATE/DELETE).
3. **Drift fails closed per pack.** If a file's checksum differs from the stored row for the same `pack_key@version`,
   the row is kept, that version is marked invalid (`checksum_drift`) and logged/audited; other packs are unaffected.
   Invalid versions are listed (`validation.errors`) but never pinned or started. Stored versions whose file disappeared
   stay available so pinned cases keep rendering.
4. **Schema extensions (contract change).**
   - `artifact_types[].source = {provider: "flowcase", flowcase_artifact_type, rows_path?, producer_node_id?}`.
     `rows_path` is a dotted path into the artifact draft (`content.artifact_draft` of an Agent Studio envelope, else
     `content`); without it the artifact is one document. Field keys map to row object keys.
   - top-level `intake = {agent_key, greeting, starters[≤5], fields[{key, label, widget text|textarea|date|list, required,
     maps_to "title"|"variables.<name>", max_length?, help?}]}`. `date` values become RFC 3339 midnight UTC on case creation.
5. **SIM pack 1.1.0** (`domain-packs/sim-tender.v1.1.json`) matches the live `sim-tender` v3 workflow: artifact types
   requirement (`requirement-register`/`requirements`), solution_fit (`capability-fit-gap`/`fit_gap`), design_option
   (`hld`/`hld_options`), work_package (`wbs`/`wbs`), cost_finding (`cost-review`/`findings`), proposal_section
   (`proposal-draft`/`sections`) — rows/fields taken from the Agent Studio output schemas; navigation overview,
   six artifact lists, tasks, approvals, documents, ai_activity, timeline; `bindings.artifact_provider =
   flowcase_registry`; `sim.case_intake` added to `agent_keys`; intake required fields = title + Flowcase's required
   `customer_id, opportunity_id, deadline`. 1.0.0 is kept unchanged (it validates) for history.
   Contract Review and PoC packs are kept as fixtures; their workflows/agents do not exist live, so `live.startable=false`.
6. **Pinning.** `workspace_case_bindings(tenant_key, case_id)` is written once (`INSERT … ON CONFLICT DO NOTHING`):
   `pinned_at_creation` when the intake creates the case (the session's pack version), `pinned_on_first_view` for cases
   created elsewhere, to the newest valid *published* pack whose `bindings.flowcase_workflow_key` equals the case's
   workflow key — only after Flowcase confirmed the user can see the case. A pinned version that later becomes
   invalid yields `pack.status = unavailable` and the generic shell (overview/tasks/approvals/timeline); it is never
   silently re-pinned. Pack version and Flowcase workflow version are independent pins (Flowcase pins the workflow).
7. **Live checks** (`GET /packs`, intake gate) ask Flowcase as the caller (cached 30 s per user and workflow):
   workflow has a published version, and every input_schema `required` variable is collected by a required intake field.
   Agent existence cannot be checked with UCW's run-only Agent Studio grant.

## Consequences
- Publishing a new pack version = adding a new file with a new version; editing a stored version is impossible.
- No admin publish API in the MVP (read-only library); a later publish flow writes the same table.
- No code path branches on a pack key; a unit test greps non-test sources for bundled pack/workflow/agent keys.
