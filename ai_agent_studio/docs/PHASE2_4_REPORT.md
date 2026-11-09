# Phases 2–4 Report: Runtime, Governance, SIM Pack

Date: 2026-10-30. Release: `p1-20261030-04` (schema `000003_runtime_governance`).
The owner asked for Phases 2–4 in one pass, overriding the master prompt's
per-phase stop gates. Contract owners for Flowcase, KMS and host SSO have not
signed off, so those boundaries are implemented against the contracts found in
the sibling repositories and fail closed where a dependency is missing.

## What was built

| Area | Implementation | Real or mocked |
|---|---|---|
| Eino runtime | `internal/runtime/executor.go`: `adk.NewChatModelAgent` + `adk.NewRunner` (eino v0.9.22, compile-verified), bounded iterations (default 8, max 15), run deadline, per-tool and per-run call caps, redacted trace events, token usage. | Real. |
| Model provider | eino-ext OpenAI adapter v0.1.13 for `openai`/`openai_compatible`. Deployment, approval and config version are re-checked at execution; the credential is read from the vault only then, with the provider scope; the HTTP client is the egress-pinned client from the probe policy. | Real adapter, tested against a fake OpenAI-compatible server. No live provider call has been made. |
| Dev mock model | Deterministic `ToolCallingChatModel` that calls the search tool and fills the output schema. Only when `AGENT_STUDIO_ENV=development`, never for production runs, labelled `model_mode=dev_mock`, and it blocks the evaluation gate. | Mock, by design. |
| Tools | `kms.search` (`kms.retrieval.v1`) and `math.calculate` (`builtin.math.v1`) seeded as published global tool versions. Unknown adapters are never executed. | Real adapters. |
| KMS | `KMSRetrieval` calls KMS `POST /api/v1/retrieval/query` with an audience-bound service token plus a delegated user session, maps document/version/anchor/trust/publication status. Without a delegated session it fails closed (`kms_delegation_unavailable`) and the run goes to `needs_review` with `evidence_unavailable`. Sandbox and evaluation runs use labelled fixtures only. | Adapter real, not wired on the VM: no KMS service credential and no SSO delegation exist yet. |
| Flowcase | Inbound `POST /api/v1/runs` accepts Flowcase's `StartRunRequest` shape (`idempotency_key, agent_id, skill_version, input, evidence`) from a service token; outbound results go through a transactional outbox to Flowcase's proposed `POST /api/v1/agent-callbacks/{provider}` with the same HMAC scheme (`X-Flowcase-Timestamp`, `X-Flowcase-Signature: v1=…`), `message_id = <run>:final`, canonical output checksum. No `approved` field exists; review state travels in `output.review`. | Real code, signature compatibility tested; not wired on the VM (no shared secret or service token issued). |
| Worker | Lease-based claims from `work_queue` (ids only, no tenant data), heartbeat lease renewal, cancellation, bounded retry with jitter for provider 429/5xx, exactly-once settlement, crash recovery by lease expiry, lazy trace retention purge. | Real. |
| Output validation | JSON Schema (santhosh-tekuri v6, external `$ref` refused), plus evidence rules: fabricated source ids (`unknown_citation`), material claims without citations (`unsupported_claim`, e.g. "99% accuracy"), citing unverified/expired sources, approval assertions (`self_approval_attempt`), injection text in evidence. Any flag → `needs_review`; schema failure → `failed`. Input is validated against the version input schema.. A final reply that is not JSON or fails the schema gets one repair call (no tools; the model sees its reply and the validator errors) and is validated again; if that call errors or still fails, the first failure is recorded (`run.repair` trace event) | Real. |
| Evaluations | Suites/cases, per-case runs on a pinned version fingerprint, deterministic checks (status, required paths, forbidden phrases, citations, expected/forbidden flags; fabrication and self-approval always fail), baseline comparison against the active production release, gate `blocked` on any critical failure, critical regression or dev-mock model. | Real. |
| Releases | Request → independent domain review → AI Ops decision. Requester ≠ reviewer ≠ approver (API and DB check constraints), gate and fingerprint re-checked in the deciding transaction, publishes draft skills + agent version atomically, supersedes the previous release, rollback re-routes new runs only. Direct `agent.version.publish` now returns 409. | Real. |
| SIM pack v1 | `internal/simpack/sim_pack_v1.json`: six agents with definitions, shared injection/evidence rules, one skill each, strict output schemas (requirements, fit-gap, WBS, HLD, cost findings, proposal sections), tool grants and a four-case suite (grounded, missing evidence, injection, expired-source conflict). `POST /api/v1/agents/bootstrap/sim/configure` fills only unconfigured drafts; nothing is published. SME reviewer roles are proposals only. Pack 1.1.0 adds a seventh agent, `sim.case_intake` (Case Intake Assistant, gate G0, `kms.search`, max 4 iterations): one chat turn per run for Flowcase's pre-case intake chat (input `case_summary` + `conversation` + optional `draft`; output `reply`, `case_update`, `kb_status`, `kb_items`, `missing`, `ready`, `quick_replies`, `unresolved`), with the same four-case suite shape. Re-running bootstrap/configure on an existing tenant only adds and configures it. Pack 1.2.0 (`sim.design` skill `design.core` 1.1.0) widens `option_for_review` from 200 to 500 characters and tells the model to put only the option name and a short reason there: live runs wrote 235–269 characters and failed with `output_schema_invalid`. | Real content; fixtures are synthetic. |
| Ops | `GET /metrics` (status/kind counts, p50/p95, tokens, tool denials, grounded rate, eval gate, rollbacks, outbox), `GET /integrations/status`, `GET /audit-events`, feedback items (`POST/GET /feedback`) that never edit skills, per-tenant rate limit (60 run creations/min) and backpressure (50 active runs). | Real. |
| UI | Playground, Evaluations, Releases, Runs & Audit, Overview metrics, Apply SIM pack. | Real API data. |

## Endpoints (all `/api/v1`)

Runs: `POST /runs` (service `run.invoke` or user `run.create`; production release only), `GET /runs`, `GET /runs/{id}`, `GET /runs/{id}/events`, `POST /runs/{id}/cancel`.
Sandbox: `POST /sandbox/runs` (`sandbox.run`). Evaluations: `GET/POST /evaluations/suites`, `GET/POST /evaluations/suites/{id}/cases`, `DELETE /evaluations/cases/{id}`, `GET/POST /evaluations/runs`, `GET /evaluations/runs/{id}`.
Releases: `GET /releases`, `GET/POST /releases/requests`, `POST /releases/requests/{id}/domain-review|ops-decision|withdraw`, `POST /releases/{id}/rollback`.
Ops: `GET /metrics`, `GET /integrations/status`, `GET /audit-events`, `GET/POST /feedback`, `GET /session` (now includes `permissions`).

```bash
# Flowcase invocation (service token); a repeat with the same key returns the same run
curl -X POST https://agent-studio.example/api/v1/runs \
  -H "Authorization: Bearer $FLOWCASE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"idempotency_key":"wf-12:node-3:attempt-1","agent_id":"sim.intake","skill_version":"1.0.0",
       "case_id":"case-881","input":{"case_summary":"..."},"evidence":[{"source_id":"doc-1","version_id":"v2","title":"RFP"}]}'

# Sandbox run against a draft
curl -X POST http://100.126.146.52:5190/api/v1/sandbox/runs -H 'Content-Type: application/json' \
  -d '{"agent_key":"sim.intake","agent_version_id":"<draft id>","input":{"case_summary":"..."},
       "evidence_fixture":[{"source_id":"rfp-4","title":"RFP","snippet":"shall support SAML","trust_status":"approved"}]}'
```

## Permissions

Human actions added: `run.read`, `run.create`, `run.cancel`, `sandbox.run`, `eval.read`, `eval.write`, `eval.run`, `release.read`, `release.request`, `release.review`, `release.approve`, `release.rollback`, `feedback.read`, `feedback.create`, `metrics.read`, `audit.read`.
Service tokens (`AGENT_STUDIO_SERVICE_TOKENS_FILE`, SHA-256 digests, tenant-bound, expiring) can hold only `run.invoke`, `run.read`, `run.cancel`, see only runs they started, and are refused on every non-run route. A bearer request never falls back to the human or development identity.

The VM access map grants the operator authoring, sandbox, evaluation, release *request*, feedback and ops reads. On 2026-10-31 the owner also granted model approval and binding (`model_deployment.approval.read`, `model_deployment.approve`, `model_deployment.revoke`, `agent.model_binding.read|write|delete`) so one agent can run on a real model; the UI now has approve/revoke controls (Models & Providers) and a Model binding panel (agent detail). `release.review`, `release.approve` and `release.rollback` stay withheld: release authority and SME ownership are unassigned, and separation of duties means one login can never both request and approve a release anyway.

## Tests actually run

- `go test -race -count=1 ./...` with DB acceptance tests against a disposable PostgreSQL 17 (migrated by `deploy/run-migrations.sh`, connected as the non-superuser runtime role): all packages pass.
- New acceptance coverage: release gates (critical failure, stale fingerprint), self-review/self-approval refusal, ops-before-review refusal, published version pinning for existing runs, pin mismatch, rollback routing (UAT-05/06/07); idempotent invocation, cross-caller keys, body-mismatch conflict, single settlement, outbox payload/checksum/no approval field, lease expiry reclaim (UAT-02/12); tenant isolation for runs, trace, cancel and suites; append-only trace; dev mock never allowed for production; SIM pack idempotent and draft-only (UAT-01); worker + Eino executing a full evaluation suite; 20 concurrent duplicate invocations → one run; two concurrent AI Ops approvals → one release; 30 runs across 4 workers settle exactly once (~0.8 s).
- Unit coverage: validator (UAT-09 unsupported 99% claim, fabricated source, expired source, self-approval), external `$ref` refusal, arithmetic parser, fixture search, KMS adapter fail-closed + mapping (UAT-03 expired → expired), Flowcase signature compatibility and retry codes, eval scoring, executor with dev mock (injection → needs_review, input schema rejection, production refuses mock), executor through the real eino-ext OpenAI adapter (tool call → JSON, token usage, vault scope), service-token auth, rate limiter, service route restrictions.
- Local end-to-end in development mode: SIM pack applied twice (idempotent), sandbox `sim.intake` run through Eino ReAct (tool call → cited JSON), four-case evaluation 4/4 passing with the gate correctly `blocked` for dev mock, release request rejected by the gate.

## Not done or not verifiable here

- No live LLM call: the operator map withholds model approval/binding and no capability probe with token-cost acknowledgement has been run. Production runs therefore fail closed with `no model deployment is approved`.
- KMS live retrieval needs a KMS retrieval service credential and a delegated user session; Agent Studio has no SSO session to delegate yet.
- Flowcase does not yet call Agent Studio: Flowcase's `AgentExecutor` HTTP client, provider registration, the shared callback secret and a service token must be configured on both sides.
- `kms.fetch_evidence`, `flowcase.artifact.get/propose` and `costing.read_reference` tools from the sample manifest are not implemented (no matching APIs); the pack binds only `kms.search` and `math.calculate`.
- Eino checkpoint/resume is not used; crash recovery replays the run from the start, which is safe because tools have no side effects.
- Canary/percentage rollout is not implemented; releases switch new runs per environment.
- Trace retention (30 days) and input storage are engineering defaults pending Security/Legal approval. Run input is stored in the tenant-scoped table; trace events store counts and names only.
- Two pilot cases with real SME approval (PRD P4 exit) cannot be produced without assigned SMEs.
