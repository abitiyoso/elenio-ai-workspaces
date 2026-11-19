# Phase 2 — core engineering integration

The owner authorized continuation (“go lanjut”) after the Phase 1 correction, then explicitly confirmed that no sandbox exists and requested implementation plus contract testing. This authorizes Phase 2 development, not a production deployment, branch merge or a claim of live integration.

## ADR 005 — project context without a duplicate backend

Use existing Flowcase workflow key + version as the current engineering workspace identity. The connected registry displays “Workflow projects” and explicitly states the one-workflow mapping. Native GET/create/version/save/validate remain authoritative; no second server project database or browser-owned runtime is introduced. A future many-workflow solution registry requires an independently reviewed contract; it is not inferred from fixture IDs.

`elenio.html#/connected` enters service mode, `#/connected/projects/<key>/<version>` restores exact context, and `#/connected/catalog` reads registered dependencies. Local Phase 1 preview stays separate. Failed identity or unavailable services never load sample data as fallback. Service mode does not display the fixture role selector.

## Existing API integration

| Action | Existing contract | Behavior |
|---|---|---|
| Identity | GET `/api/v1/session` | Existing SessionProvider + host bridge; server roles; audience-aware original client. Invalid session bodies fail closed. |
| Registry | GET `/api/v1/workflows?q=…&cursor=…` | Server search and pagination; no fake portfolio counts. |
| Blank/template/native import | POST `/api/v1/workflows` | Draft v1, stable Idempotency-Key across retry; native definition accepted by existing backend. Creation never publishes or executes. |
| Load/version context | GET workflow detail + GET version | Exact key/version route, immutable published/deprecated versions remain read-only. |
| Draft save | PATCH version with strong If-Match | Uses loaded version_num; 412 retains edits and requires explicit discard/reload. |
| Validate | POST `/api/v1/workflow-validations` | Server report and node/path diagnostics; local topology checks remain separate. |
| Fork published | POST versions from_version | Explicit human draft action; opens existing draft if one already exists. No publication endpoint is invoked. |
| Runtime capability | GET `/api/v1/runtime-capabilities` | Editing locked if unknown/malformed; palette derives support from response. Schema membership alone does not establish execution support. |
| Dependencies | GET `/api/v1/workflow-catalog` | Agent/skill references, connector operations and policies shown from actual response; inspector suggestions use registered refs. Registry presence is not proof of approved runtime release. |

## ADR 006 — visual positions and import review

Connected definitions, tokens, roles and execution state are never persisted to device storage. Only visual x/y coordinates are stored in a separate IndexedDB sidecar scoped by tenant ID + user ID + version ID. Storage errors do not change the process definition. Published content hashes remain unaffected by visual movement.

Native import previews added/removed/changed node IDs and changed root fields before explicit replacement. Existing connected draft imports must retain workflow key/version; a different identity uses the create/import flow. All next/config/input_schema/policy/metadata content is preserved; unsupported or invalid content stays subject to server diagnostics. No n8n/BPMN conversion is advertised. Navigating away from an unsaved connected editor requires an explicit discard decision; export remains available during conflicts.

## ADR 007 — reuse governed capability workspaces

Flowcase catalog is the source for references it accepts. Existing Agent Studio, KMS and connector governance UIs are reused through the Control Plane app registry rather than rewritten. When mounted at `/apps/flowcase`, the app catalog is read via the CP root cookie session; no Flowcase bearer token is forwarded to that audience. Launch calls the existing authorization endpoint, then accepts only the exact registered same-origin `/apps/<app_key>/` path with `proxy` integration. Standalone mode exposes an unavailable reason, never guesses routes from sample manifests.

This does not turn unregistered API/MCP definitions into executable tools. Existing scoped adapters, vault handling, KMS ACL/publication and independent review authorities remain unchanged.

## Verification boundary and remaining scope

Browser contract tests intercept API responses and identify the actor as **Contract test fixture**, environment **test**, server **dev_mock**. These prove UI request/response/error handling, not deployed database authorization. Screenshots of connected mode are contract-test screenshots, not live service evidence.

No sandbox, approved model or CP deployment was supplied; the owner confirmed this. General AI blueprint generation/apply, many-workflow project registry, generic connector registration/execution and cross-service authenticated end-to-end acceptance remain open. Existing Agent/KMS authoring is delegated to their governed applications; its actual deployment/entitlements cannot be claimed verified here. Phase 3 runtime/provider execution and all production deployment remain outside this delivery.

Router boundary: the new entry uses the existing library's data hash router so unsaved-document protection covers browser Back as well as in-app links. The authenticated original entry is unchanged. API reference: https://reactrouter.com/7.18.5/api/hooks/useBlocker .

## Local integration follow-up

The later owner request authorized local end-to-end work. PostgreSQL/API/worker are now running in an isolated local sandbox; six real browser scenarios and all database suites ran. The new Execution trace reads real case records and pinned timelines. Earlier statements that no disposable DB exists are historical. No external provider, production app mount or general AI Architect was configured; the owner confirmed provider absence. See LOCAL_E2E.md for current acceptance and limits.
