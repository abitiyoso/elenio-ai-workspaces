# UCW BFF API — MVP contract (backend ⇄ frontend)

Status: agreed contract for the target-first MVP (Phase 0 + 1 + intake chat + author draft review).
The browser reaches the BFF same-origin through the Control Plane proxy: `/apps/case-workspace/api/v1/workspace/...`
(the proxy strips `/apps/case-workspace`, so the BFF serves `/api/v1/workspace/...`). The SPA is served by the same
Go binary at `/` (proxied as `/apps/case-workspace/`). The browser never holds a token: the Control Plane injects
`Authorization: Bearer <ES256 token, aud=case-workspace>` on every proxied request.

## Conventions
- JSON only. Errors: `{"error": {"code": "...", "message": "...", "trace_id": "...", "details": [{"code","message","path"}]?}}`.
  Status: 401 unauthenticated / session no longer active, 403 forbidden (code says why), 404 masked not-found,
  409 conflict, 412 version mismatch (stale If-Match), 422 invalid input, 503 upstream unavailable (code names the service:
  `flowcase_unavailable`, `agent_studio_unavailable`, `kms_unavailable`), 202 accepted.
- Mutations require same-origin (enforced by the CP proxy) and an `Idempotency-Key` header where noted.
- Versioned resources return `ETag: "<n>"`; writes take `If-Match: "<n>"`.
- Composite reads never fail as a whole because one upstream is down: they return what they have plus
  `partial_errors: [{"source": "flowcase|agent_studio|kms|registry", "code": "...", "message": "..."}]`.
- Times are RFC 3339 UTC. IDs are opaque strings (Flowcase UUIDs pass through).

## Identity
### GET /api/v1/workspace/me/context
```json
{ "user_id": "<cp user id>", "display_name": "Maya Putri", "tenant_key": "acme-demo", "workspace_key": "sim-b2b",
  "platform_roles": ["case_worker", "..."],          // from the token scope claim (display only, never used to grant)
  "flowcase": {"available": true, "roles": ["case_owner"], "can_create_cases": true},
  "partial_errors": [] }
```

## Domain packs (read-only library in MVP)
### GET /api/v1/workspace/packs
`{"items": [PackSummary]}` where
```json
PackSummary = { "pack_key": "sim.tender", "version": "1.1.0", "title": "...", "description": "...", "status": "published",
  "checksum": "sha256:...", "brand": {"accent": "#2864EC", "icon": "briefcase"}, "flowcase_workflow_key": "sim-tender",
  "validation": {"schema_ok": true, "semantic_ok": true, "errors": [{"path": "/navigation/2/artifact_type", "code": "...", "message": "..."}]},
  "live": {"startable": true, "reasons": []},     // false + reasons when the bound workflow/agents are not live (e.g. Contract Review)
  "has_intake": true }
```
### GET /api/v1/workspace/packs/{pack_key}/versions/{version}
`{"pack": PackSummary, "manifest": { ...the validated manifest JSON... }}`

## Cases
### GET /api/v1/workspace/cases?q=&status=&cursor=
```json
{ "items": [CaseCard], "next_cursor": null, "partial_errors": [] }
CaseCard = { "id": "...", "reference": "CASE-2026-0002", "title": "...", "status": "active|under_review|paused|completed|cancelled|failed",
  "outcome": null, "workflow_key": "sim-tender", "workflow_version": 3, "owner_name": "Maya Putri", "current_steps": ["intake"],
  "open_tasks": 0, "pending_approvals": 1, "pending_drafts": 1, "progress": 40, "created_at": "...", "updated_at": "...",
  "pack": {"pack_key": "sim.tender", "version": "1.1.0", "title": "SIM Tender", "brand": {...}} | null }
```
Only cases the caller can see in Flowcase (the BFF calls Flowcase as the user).

### GET /api/v1/workspace/cases/{id}
```json
{ "case": CaseCard + { "variables": {...}, "business_facets": {...} },
  "pack": { "pack_key": "sim.tender", "version": "1.1.0", "title": "...", "brand": {...}, "binding": "pinned_at_creation|pinned_on_first_view",
            "bound_at": "...", "status": "ok|unavailable" },          // unavailable → safe generic read-only shell + warning
  "navigation": [ {"id": "overview", "view": "overview", "label": "Overview"},
                  {"id": "requirements", "view": "artifact_list", "label": "Requirements", "artifact_type": "requirement"}, ... ],
  "artifact_types": [ PackArtifactType ],     // from the pinned manifest: key, title, description, fields[], source{provider, flowcase_artifact_type, rows_path?}
  "workflow": { "version": 3, "nodes": [ {"id": "g1", "name": "G1 — Requirement baseline", "type": "human_approval",
                "status": "completed|active|waiting_review|waiting_approval|failed|pending"} ] },
  "artifacts": [ { "id": "...", "type": "requirement-register", "pack_artifact_type": "requirement" | null, "version": 2,
                   "status": "ready_for_review|...", "producer_node_id": "intake", "content": {...Flowcase content...},
                   "sources": [{"source_id","version_id","title","citation","anchor"}], "updated_at": "..." } ],
  "drafts": [ FlowcaseDraftReview ],          // passthrough of Flowcase CaseDetail.drafts (id, node_id, node_name, iteration, artifact_type,
                                              // artifact_version, content, status, feedback, decided_by_name, decided_at, created_at,
                                              // version_num, can_submit, can_revise, revisions)
  "tasks": [ ...Flowcase tasks... ], "approvals": [ ...Flowcase approvals (read-only in MVP)... ],
  "agent_runs": [ ...Flowcase agent_runs... ], "history": [ ...Flowcase history... ],
  "permissions": { "can_review_drafts": true, "can_note": true },
  "links": { "flowcase_case": "/apps/flowcase/cases/<id>", "flowcase_approvals": "/apps/flowcase/approvals" },
  "partial_errors": [] }
```
`ETag` = Flowcase case `version_num`.

### POST /api/v1/workspace/cases/{id}/drafts/{draftId}/submit
Body `{"note": "optional ≤1000"}`, header `If-Match: "<draft version_num>"`. Forwards to Flowcase
`POST /api/v1/drafts/{id}/submit`. Returns the refreshed case context (same shape as GET case), 200.
### POST /api/v1/workspace/cases/{id}/drafts/{draftId}/revision
Body `{"feedback": "5..1000 chars"}`, `If-Match`. Forwards to Flowcase `POST /api/v1/drafts/{id}/revision`. Returns refreshed case, 200.
Flowcase errors pass through with their status/code (403 not_case_owner, 409 draft_not_pending / revision_limit /
case_not_active, 412, 400 feedback_required).

## Intake chat (pre-case conversation; UCW owns the session)
Session JSON (`ETag` = version_num):
```json
IntakeSession = { "id": "...", "pack_key": "sim.tender", "pack_version": "1.1.0", "workflow_key": "sim-tender",
  "status": "open|created|abandoned",
  "fields": [ {"key": "title", "label": "Case title", "widget": "text|textarea|date|list", "required": true, "help": "..."} ],  // from the pack's intake.fields
  "greeting": "...", "starters": ["...", "...", "..."],
  "draft": { "title": "...", "customer_name": "...", "deadline": "2026-12-15", "constraints": ["..."] },
  "messages": [ { "role": "user|assistant", "content": "...", "at": "...",
                  "updated_fields": ["title"], "kb_items": [{"name","kind","evidence_refs":[...]}],
                  "quick_replies": ["..."], "sources": [{"source_id","version_id","title","citation","anchor"}],
                  "unresolved": [{"topic","reason"}] } ],
  "kb_status": "covered|partial|not_covered|unknown", "kb_items": [...latest...], "sources": [...all cited so far...],
  "ready": false, "missing": ["Opportunity ID"], "pending": false, "last_error": null,
  "case_id": null, "case_reference": null, "turn": 1, "version_num": 4 }
```
- `POST /api/v1/workspace/intake-sessions` body `{"pack_key": "sim.tender"}` → 201 session. Requires the pack to have
  `intake`, be startable, and the user to be able to create Flowcase cases (`flowcase.can_create_cases`), else 403 `cannot_create_cases`.
- `GET /api/v1/workspace/intake-sessions/{id}` → session; if a turn is pending the BFF polls Agent Studio once and applies a
  terminal result before answering. Owner-only (others: 404).
- `POST /api/v1/workspace/intake-sessions/{id}/messages` body `{"content": "1..4000"}` → 202 session (`pending: true`).
  409 `turn_in_progress` | `too_many_turns` (30 user turns) | `session_closed`; 503 `agent_studio_unavailable` (message kept, `last_error` set).
- `PUT /api/v1/workspace/intake-sessions/{id}/draft` body `{"draft": {...}}`, `If-Match` → session (server validates fields, recomputes missing/ready).
- `POST /api/v1/workspace/intake-sessions/{id}/case` body `{}`, `If-Match` + `Idempotency-Key` → 201 `{"case_id": "...", "reference": "CASE-..."}`.
  Creates the Flowcase case (as the user) from the draft using the pack's field mapping, pins the pack version to the case,
  adds a case note "Created from the Case Workspace intake chat (N messages)". Flowcase validation errors pass through as 422 with details.

### Documents in the intake chat (pack `intake.documents`, SIM 1.3.0+)
The session also carries `"documents": [{"id","filename","media_type","size_bytes","status":"processing|ready|failed","chars","error","created_at"}]`
(never the text) and `"uploads": {"max_files","max_bytes","accept":[".pdf",".docx",".txt",".md"],"truncated"}` (`null` when the pack
accepts no documents). A user message sent for a document has `"document": {"id","filename"}`.
- `POST /api/v1/workspace/intake-sessions/{id}/documents` multipart field `file` (≤ 15 MB) → 202 session. The type is decided by content
  (`%PDF-`, DOCX zip with .docx, UTF-8 .txt/.md); the name is reduced to a base name. PDF/DOCX go to docling-serve (async, OCR for scanned
  pages, `ocr_lang` id+en) and stay `processing`; TXT/MD are ready at once. 415 `unsupported_document`, 413 `document_too_large`,
  409 `uploads_disabled` | `turn_in_progress` | `document_processing` | `too_many_documents` | `duplicate_document` | `too_many_turns` |
  `session_closed`, 503 `document_conversion_unavailable`.
- `GET .../intake-sessions/{id}` also polls a processing document once. When it is ready the BFF writes the text of all ready documents
  (each under `# <filename>`, cut to the field's max_length) into `documents.target_field` of the draft, then sends the pack's
  `documents.prompt` (with `{filename}`) as the user's message and starts the agent turn. While a document is ready the agent's
  case_update never rewrites that field; the user can still edit it in the brief. Failed/lost/overdue (10 min) conversions become `failed`
  with a friendly `error`; failed documents do not count against `max_files` and the same file can be uploaded again.
- `DELETE /api/v1/workspace/intake-sessions/{id}/documents/{docId}` → 200 session; rebuilds the document field from the documents left.
- `POST .../messages` answers 409 `document_processing` while a document is being read.
The original file is not stored (only its SHA-256); audit events (`intake.document_uploaded|ready|failed|removed`) carry ids, media type,
size and checksum, never the name or text.

## Frontend notes (web/, appended by the SPA build)
How the SPA reads this contract, and gaps it had to bridge. None of these block the MVP; the UI tolerates both shapes.
- **Navigation id**: case `navigation[]` items are read as `id ?? key ?? view` (the raw manifest uses `key`). Please emit `id` = manifest `key`; it is the `/cases/:id/:navId` URL segment.
- **`source.rows_path` syntax** is not specified. The SPA accepts a JSON pointer (`/artifact_draft/requirements`) or a dotted path (`artifact_draft.requirements`), tried against the raw Flowcase `content` first and then against the unwrapped `content.artifact_draft`. Rows must be objects keyed by the manifest `fields[].key`; for `evidence_ref` fields the SPA falls back to the row's `evidence_refs` (Agent Studio convention). Please pick one syntax and document it.
- **Agent vs human artifact**: `artifacts[]` has no `produced_by` kind. The SPA marks "AI draft — needs human review" when status ≠ `approved` and (an `agent_runs[]` entry has the artifact's `producer_node_id`, or that workflow node's `type` contains `agent`, or the content is an Agent Studio envelope). Suggest adding `produced_by_kind: "agent"|"human"|"revision"` (Flowcase has it on versions).
- **Producer of a not-yet-produced type**: the empty state says "Produced by <step> — not yet available". The step is guessed from `drafts[]`/`artifacts[]` with the same `source.flowcase_artifact_type`; otherwise "a later workflow step". Suggest `artifact_types[].source.producer_node_id` (or a `producer_node_name`) in the case context.
- **Live agent run statuses** are not enumerated. The SPA polls the case every 4 s while any `agent_runs[].status` ∈ {queued, pending, ready, running, submitted, waiting_external, in_progress, accepted}, or while a draft it just sent back is still `revision_requested` (up to 3 min). `workflow.nodes[].status` `active`/`running` on a step with a `revision_requested` draft also shows "revising".
- **Drafts order**: `drafts[]` is assumed newest first (as Flowcase returns it) to find the latest draft per step.
- **Idempotency-Key on draft submit/revision**: the SPA sends one (contract only requires `If-Match`); please accept/ignore it.
- **`registry` partial errors / 503 codes**: the SPA labels `source: "registry"` as "Pack registry" and understands `registry_unavailable` in addition to the three listed 503 codes.
- **Pack unavailable**: when `pack.status = "unavailable"` (or `pack` is null, or `navigation` is empty) the SPA ignores `navigation` and shows a fixed generic shell: overview, tasks, approvals, documents, ai_activity, timeline.
- **Links**: `links.*` are rendered only if they are same-origin relative paths (start with `/`, not `//`).
- **Intake `missing[]`** is matched to `fields[].label` (to hide items the user already typed in the unsaved form); keep the labels identical. `widget: "list"` is edited one item per line and sent as `string[]`.
- **SPA serving**: the bundle is built with base `/apps/case-workspace/` (assets at `/apps/case-workspace/assets/*`, i.e. `/assets/*` after the proxy strips the prefix). The Go server must answer deep links (`/cases/...`, `/packs/...`, `/cases/new?...`) with `index.html`, and must not cache `index.html` (assets are content-hashed).

## Backend notes (Go BFF, cmd/workspace-api) — authoritative details and additions
All additions are backward compatible with the shapes above.
- **Navigation**: every `navigation[]` item has `id` (= manifest `key`), `view`, `label`, and optionally `artifact_type`, `icon`, `required_permission`.
  The generic shell (pack unavailable) is `overview, tasks, approvals, timeline`.
- **`artifact_types[].source`**: `{provider: "flowcase", flowcase_artifact_type, rows_path?, producer_node_id?}`.
  `rows_path` is a **dotted path** (`^[a-z_][a-z0-9_]*(\.[a-z_][a-z0-9_]*){0,7}$`, e.g. `requirements`) applied to the artifact draft:
  `content.artifact_draft` when `content` is an Agent Studio envelope (has `artifact_draft`), otherwise `content` itself. Without `rows_path` the
  artifact renders as one document. Row objects are keyed by `fields[].key`. `evidence_ref` field values may be a string or an array of source ids.
  `producer_node_id` is the Flowcase workflow node that produces the type (display only; SIM 1.1.0: intake, matching, design, project, cost, proposal).
  One Flowcase artifact type maps to at most one pack artifact type (validator rule).
- **`artifacts[]`** also carry `produced_by_kind: "agent"|"human"` (Flowcase version `produced_by_kind`; `revision` = a human edit → `human`).
  `content` is Flowcase's current-version content unchanged; `sources` are read from the envelope's `sources`.
- **`agent_runs[]`** is Flowcase's list unchanged; its status vocabulary is `started | succeeded | failed | cancelled | timed_out | superseded`
  — only **`started`** is in progress. Workflow node statuses `active` mean the step is executing or waiting on a human/external party.
- **Workflow node status mapping** (`workflow.nodes[].status`): pending draft review on the node → `waiting_review`; pending approval → `waiting_approval`;
  otherwise the node's latest non-superseded execution: `completed` → `completed`, `failed` → `failed`, `cancelled` → `pending`,
  `pending|ready|running|waiting_human|waiting_external` → `active`; no execution → `pending`. All graph nodes are listed (incl. start/end/split/join; `type` says which).
- **`drafts[]`** are returned newest first (sorted by `created_at` desc by the BFF).
- **Draft submit/revision** accept and ignore an `Idempotency-Key` header (Flowcase decisions are guarded by `If-Match`). Missing `If-Match` → 428
  `precondition_required`. A draft that is not in the case → 404 `not_found` (nothing forwarded). Submit body `{"note"?}`, revision body `{"feedback"}`.
- **Partial errors** `source` values: `flowcase`, `registry` (codes `registry_unavailable` — UCW database unreachable; `pack_unavailable` — no usable pack,
  generic shell shown), later `agent_studio`, `kms`. `pack.status = "unavailable"` keeps `pack_key`/`version`/`binding` of an existing (but unusable) pin,
  else they are empty strings and `bound_at` is null.
- **Session/identity errors**: 401 `unauthenticated` (no/invalid CP token), 401 `session_inactive` (token exchange refused: CP session ended),
  503 `auth_unavailable` (CP JWKS unreachable), 503 `flowcase_unavailable`, 403 `flowcase_access_denied` (Flowcase rejected the exchanged token,
  e.g. user not provisioned in Flowcase). Flowcase 4xx answers otherwise pass through (status, code, message, details).
- **`GET /cases`**: Flowcase unreachable → 200 with `items: []` + partial error; inactive session → 401. A case not yet pinned shows the pack it
  will be pinned to on first open (not persisted by the list).
- **`GET /me/context`**: `platform_roles` = the token `scope` claim values (display only). Never fails because of Flowcase (see partial errors).
- **`GET /packs`** also returns `partial_errors` (when Flowcase could not be asked). `live.reasons` explain why a version is not startable
  (invalid, not published, superseded by a newer version, Flowcase workflow not published, a Flowcase-required case variable not collected as a
  required intake field, Flowcase unreachable). Agent availability cannot be checked (UCW's Agent Studio grant is run-only); a missing agent surfaces at the first turn.
  `GET /packs/{key}/versions/{v}` returns `manifest: null` for an invalid version.
- **Intake session** extras: `fields[].max_length`, `created_at`, `updated_at`. `missing[]` contains exactly the `fields[].label` strings of required
  fields without a value. Draft values: strings (`date` = `YYYY-MM-DD`) or `string[]` for `list`.
- **Intake errors**: create → 404 `pack_not_found`, 409 `pack_not_startable` (no valid published version, no intake, or not live; reasons in
  `details`), 403 `cannot_create_cases`. messages → 422 `invalid_message`, 409 `turn_in_progress|too_many_turns|session_closed`,
  503 `agent_studio_unavailable`, 422 `agent_studio_rejected` (Agent Studio refused the request; message kept, `last_error` set in both cases).
  draft → 422 `invalid_draft` (details path `draft.<key>`), 412 `version_mismatch`, 409 `session_closed`. case → 400 `idempotency_key_required`,
  412 `version_mismatch`, 409 `session_closed|pack_unavailable`, 422 Flowcase validation (incl. Flowcase 400 input errors) with details.
  A session that already created its case answers 201 with that case again.
- **SPA serving**: every non-`/api/` GET that is not an existing file is answered with `index.html` (`Cache-Control: no-store`);
  `/assets/*` get `public, max-age=31536000, immutable`; a missing `/assets/*` file is 404.
