# Integration & Trust Boundary Guide — UCW

## Confirmed by existing project design (not by live API test)

- **Control Plane**: Go BFF + OIDC server session, platform membership/mini-app registry, trusted same-origin route `/apps/case-workspace/*`, delegated per-service verified identity.
- **Flowcase**: generic workflow and case engine with human task/approval and versioned artifact registry concept. Actual structured JSON artifact storage and ETag API must be verified.
- **KMS**: document processing via existing self-hosted Docling, Elasticsearch semantic search and ACL retrieval; KMS owns file, source version and citation.
- **Agent Studio**: Go + Eino ADK runtime, published agent/skill/model versions, service invocation, structured output and trace.

## Proposed service calls — NOT deployed endpoint claims

| UCW screen/action | Service owner | Required data or operation | Auth / failure behavior |
|---|---|---|---|
| Launch UCW | Control Plane | verified identity, membership, `case-workspace:launch` | Missing app grant → cannot launch; direct URL still protected |
| My Cases | Flowcase | scoped case list, pinned workflow and pack key/version | Member with no case visibility receives no cases |
| Case Header/Steps | Flowcase | workflow instance ID, pinned definition version, ordered current stage/readiness | Flowcase 503 → explicit partial error, no fake state |
| Domain Navigation | UCW registry | published pinned JSON pack, compiled permissions and artifact references | Unknown version → safe read-only fallback |
| Artifact Table/Editor | Flowcase artifact API or registered Domain ArtifactProvider | immutable revisions, schema version, `If-Match` | 409 conflict, 403 no edit, no browser approval |
| Documents/Evidence | KMS | exact source version/anchor and ACL | Forbidden source → no direct original blob URL |
| Agent run button | Agent Studio through Flowcase/UCW controlled adapter | published agent version, tool/KMS scopes, idempotency, output draft ref | No direct LLM, no approval, 202 + run ID |
| Task & Approval | Flowcase | authorized task/decision for exact artifact hash/version | AI may not impersonate reviewer; signed callback required |
| Pack publish | UCW/Control Plane registry | manifest validator, publisher delegation, checksum and audit | Draft-only editor; published immutable |

## Version identity matrix

`case_id`, `tenant_id`, `workspace_id`, `workflow_instance_id`, `workflow_definition_version`, `workspace_pack_key + workspace_pack_version`, `artifact_id + artifact_revision`, `schema_version`, `kms_source_id + source_version`, `agent_run_id + agent_version` are **independently versioned**. Do not collapse them into one generic `version` string.

## Security negative tests

- Manually forged `tenant_id` or `X-User-ID` → denied.
- User sees app but is not Flowcase case member → case details denied.
- User is case editor, not SME approver → approve denied by Flowcase backend.
- Manually alter DOM action to invoke unpublished `sim.proposal` → rejected at server and agent runtime.
- KMS source from another tenant/case → denied and no source metadata leaked.
- Publish pack with `view=custom_web_url`, JS injection in `label`, or arbitrary `registered_extension_ref` → failed validation/escaped output.
- Previously published pack v1 remains pinned even after v2, and approved artifact revisions remain immutable.
- Upstream KMS/Agent Studio down → no false health/approval success; partial panels identify their service.

## MVP deployment

`https://workspace.example.internal/apps/case-workspace/*` → React UCW; `.../api/v1/workspace/*` → Go BFF. Ingress routes and backend upstreams must come from approved server ops configuration, never from a user-editable manifest. BFF uses service-to-service audiences and delegated subject scope. Avoid generic iframe embedding and third-party cookie hacks. Read existing Control Plane `AUTH_AND_TRUST_BOUNDARIES.md` and `INTEGRATION_AND_DEPLOYMENT_GUIDE.md` for deployed trust design.

## Artifact storage decision gate

1. If Flowcase API handles structured JSON revisions + citations + ACL + expected version, **reuse Flowcase**.
2. If Flowcase has a registry only, implement `ArtifactProvider` bridge to existing domain store; keep authoritative revision there.
3. If none is present, propose a **generic Artifact Store** with append-only versions and a clear authority boundary, but require explicit approval before implementing. No approval or workflow logic goes into that store.

## Domain extension rule

Manifest-defined modules may only render listed safe widgets and allowlisted actions. Complex specialized functionality can use a separately reviewed React extension registered **by code build / signed extension registry**, with a resource-scoped Go domain adapter. No arbitrary remote JS, SQL or HTTP URLs inside pack JSON.
