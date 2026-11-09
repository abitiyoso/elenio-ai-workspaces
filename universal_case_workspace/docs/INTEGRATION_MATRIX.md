# Integration matrix (verified against source, 2026-10-31)

"Live" = the upstream endpoint exists in the named repository and UCW calls it. "Fixture" = only in UCW's bundled
packs/samples. Nothing here was exercised against the running VM instances; the BFF is tested against httptest
fakes that mirror the upstream code (see `internal/integration`).

## Control Plane (`../control_plane`)

| What | Status | Source / notes |
|---|---|---|
| Inbound token: ES256, `kid` = JWK thumbprint, claims `iss sub aud iat nbf exp jti principal_type tenant_id workspace_id scope sid policy_version correlation_id auth_time` (`act` only on exchanged tokens) | Live | `internal/integrations/tokens.go` `Issuer.Mint` |
| JWKS `GET {CP_PUBLIC_URL}/.well-known/jwks.json` | Live | `Issuer.JWKS` |
| Proxy `/apps/case-workspace/*` → `127.0.0.1:8480` with prefix stripped and `Authorization: Bearer <aud=case-workspace>` injected | Owned by the CP agent (`deploy/upstreams.vm.json` has no `case-workspace` entry yet) | same pattern as `flowcase`, `kms`, `agent-studio` entries |
| Token exchange `POST /api/v1/token-exchange` (Basic `case-workspace:<secret>`, `{subject_token, audience}` → `{access_token, expires_in}`) | Being added by the CP agent; **not present in the CP code read for this work** (only referenced in a comment in `tokens.go`). UCW implements the agreed shape; 401/403 → `session_inactive` | `internal/controlplane/exchange.go` |
| Contract checker `tests/contract/tokencheck` negative suite | Live; UCW's verifier unit test mirrors every case (`internal/auth/verifier_test.go`). Outsider check is off for UCW (`-outsider-check=false`): UCW has no user directory | `tests/contract/tokencheck/main.go` |

## Flowcase (`../flowcase_platform`, instance `flowcase-cp`, API `127.0.0.1:8280`, `AUTH_MODE=trusted_jwt`, aud `flowcase`)

| UCW use | Flowcase endpoint | Status |
|---|---|---|
| `/me/context` display name + roles | `GET /api/v1/session` → `{user_id, display_name, roles}` | Live (`internal/api/server.go`) |
| Case list | `GET /api/v1/cases?q&status&cursor` → `{items: CaseSummary[], next_cursor}` | Live |
| Pending drafts per case (list) | `GET /api/v1/drafts?status=pending` (cases the caller owns; all for tenant admins) | Live (`cases.go`, `drafts.go`) |
| Case detail | `GET /api/v1/cases/{id}` → `CaseDetail` incl. `workflow_nodes, timeline, tasks, approvals, artifacts[versions], agent_runs, drafts, history` | Live (`internal/cases/types.go`) |
| Draft submit / revision | `POST /api/v1/drafts/{id}/submit` `{note?}`, `/revision` `{feedback}` with `If-Match` | Live (migration 000008) |
| Workflow live check | `GET /api/v1/workflows/{key}` (`latest_published_version`), `GET /api/v1/workflows/{key}/versions/{v}` (`definition.input_schema.required`) | Live (any tenant member) |
| Create case from intake | `POST /api/v1/cases` `{workflow_key, version: 0, title, variables}` + `Idempotency-Key` (8–128) ; requires Flowcase role `case_owner` | Live |
| Case note | `POST /api/v1/cases/{id}/events` `{type:"note", body}` | Live |
| Artifact edit | `POST /api/v1/cases/{id}/artifacts/{artifact}/versions` with `If-Match` | Live in Flowcase, **not used** (Phase 3) |
| Approvals decide / tasks complete | `POST /api/v1/approvals/{id}/decision`, `/tasks/{id}/complete` | Live in Flowcase, **not used** (decided in Flowcase UI) |

Workflows (seeded by `migrate seed-dev`): `sim-tender` v1–v3 published (v3 = `author_review` on the six agent steps, input_schema
required `opportunity_id, deadline, customer_id`), v4 draft; `internal-poc-request` v1. **No `legal-review` or `poc-request` workflow exists.**
Flowcase's own intake chat (`internal/intake`, `/api/v1/intake-sessions`) is ported into UCW and is to be removed from Flowcase.

## Agent Studio (`../ai_agent_studio`, instance `agent-studio-cp`, `127.0.0.1:8290`)

| UCW use | Endpoint / contract | Status |
|---|---|---|
| Start intake turn | `POST /api/v1/runs` `{idempotency_key, agent_id, skill_version:"", input, evidence:[], correlation_id, delegated_user}` (body ≤ 256 KiB, unknown fields rejected) | Live (`internal/httpapi/runtime.go`) |
| Poll / cancel | `GET /api/v1/runs/{id}`, `POST /api/v1/runs/{id}/cancel`; a service client sees only runs it started | Live |
| Result | `{run_id, status, agent_id, skill_version, output, output_checksum}`; `output` = envelope `{artifact_draft, review{status, validation{cited_sources}}, pinned, model_mode, sources[]}`; checksum = SHA-256 of canonical JSON | Live (`repository/postgres/runtime_queue.go` `FlowcaseOutput`, `runtime_runs.go` `OutputChecksum`) |
| Patterns | idempotency `^[A-Za-z0-9._:-]{8,128}$` → UCW key `ucw-intake-<session uuid>:<turn>` (≤ 50 chars); correlation `^[A-Za-z0-9._:-]{1,128}$` → `intake:<uuid>`; `delegated_user` lower-case UUID, service callers only | Verified |
| Agent `sim.case_intake` | input `{case_summary ≤4000, conversation[≤20]{role,content≤4000}, draft}`, output `{reply, case_update{…}, kb_status, kb_items, missing, ready, quick_replies, unresolved}` | Live (`internal/simpack/sim_pack_v1.json`, pack 1.1.0) |
| Service grant `case-workspace` (run.invoke/read/cancel) | Added to `control_plane/deploy/agent-studio-cp/deploy.sh` and upserted by `deploy/case-workspace-cp/deploy.sh` | New (not yet deployed) |
| Agents `legal.clause_analyzer`, `poc.experiment_planner` | — | Fixture only (do not exist) |

`delegated_user`: Flowcase sends the case owner's `users.subject`, which under `trusted_jwt` is the Control Plane user id
(`agentstudio.go` `SubjectResolver`). UCW already has that id as the token `sub` and sends it directly. KMS resolves
`X-KMS-Delegated-User` against `tenant_principals.principal_id` = Control Plane user UUID (`kms_go_package/README.md`
"Delegated service retrieval", `backend/internal/auth/delegation.go`), so KMS retrieval in intake turns runs as the chatting user.

## Docling (`docling-serve`, shared on the VM at `127.0.0.1:5001`)

| UCW use | Endpoint / contract | Status |
|---|---|---|
| Convert an uploaded intake document | `POST /v1/convert/file/async` multipart `files`, `to_formats=md`, `do_ocr=true`, `ocr_lang=id,en`, `image_export_mode=placeholder` → `{task_id}`; header `X-Api-Key` (same key as kms-cp) | Live (verified on the VM with a scanned 12-page KAK: ~40 s, 18.7k characters) |
| Status / result | `GET /v1/status/poll/{task_id}` → `task_status` (`pending\|started\|success\|partial_success\|failure`; 404 unknown); `GET /v1/result/{task_id}` → `{status, document{md_content}}` | Live (`internal/docling/client.go`) |

Uploads are polled from `GET /intake-sessions/{id}` like agent runs, so no request is held open during OCR.

## KMS (`../kms_go_package`)

Not called by UCW in the MVP. Source titles come from Agent Studio envelopes (`sources`) inside Flowcase artifact
content and intake turns. `internal/knowledge` holds the `Provider` port with a stub only.

## Domain packs (UCW)

| Pack | Flowcase workflow | Agents | Live |
|---|---|---|---|
| `sim.tender` 1.1.0 | `sim-tender` (live, v3) | 6 workflow agents + `sim.case_intake` (live) | Startable (intake) |
| `sim.tender` 1.0.0 | `sim-tender` | same keys | Valid, superseded; kept for history/pins |
| `legal.contract-review` 1.0.0 | `legal-review` (absent) | `legal.clause_analyzer` (absent) | Not startable |
| `lab.poc-assessment` 1.0.0 | `poc-request` (absent; Flowcase has `internal-poc-request`) | `poc.experiment_planner` (absent) | Not startable |
