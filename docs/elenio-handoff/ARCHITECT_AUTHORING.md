# Architect authoring — 2026-11-19

The authorized 9Router model now powers the actual Architect screen through the existing Agent Studio service. A requirement becomes a durable sandbox proposal, then an explicitly reviewed and validated native Flowcase draft. This closes the two blockers reported in PR #5: Windows FileVault startup and application authoring. It does not close every development-kit phase or grant production approval.

Branch: `feat/elenio-architect-authoring`, based on `feat/elenio-nine-router` at `c3b17a7a2b071f15c763bfed561741bf76fb6637`. Delivery commit/PR metadata will be appended after push.

## Working journey

1. Readiness checks both authenticated sessions, tenant, required permissions, registered version, model binding, capability-backed development approval, and the absence of tools/skills. The UI fails closed on a mismatched tenant or unavailable service.
2. The user explicitly sends a requirement. The UI discloses that the requirement and capability catalog go to the approved model and may consume billable tokens. It invokes the existing sandbox-run API; the browser receives no provider key.
3. A bounded, tool-free model run returns clarification questions or a proposal with assumptions, minimal agent recommendations, missing dependencies and a native workflow. The model must reuse verified catalog references and cannot invent an executable missing connector or release.
4. Durable server history survives reload. Status/actual token counts, cancellation and retry are available. A lost creation response retains the idempotency key; resuming a run does not generate another model request.
5. Preview opens editable native JSON, topology checks and a field/node diff. Native Flowcase validation is required; editing invalidates prior validation. A new identity/readiness/run/validation check runs before confirmed creation.
6. Only **Confirm create draft** creates the workflow. It opens the connected editor with canonical `next`, condition configuration, policy references and version 1. No publish, case start, agent release or production mutation is attached to this action.

The V10 conversation/discovery composition, original brand artwork and shared component system remain. Mobile stacks proposal/history underneath the conversation; long conversations support keyboard focus and scrolling. Branches use generic True/False labels. Human visual acceptance remains open; this is not pixel parity with HTML V9/V10.

## Existing authorities and contracts

- Agent Studio owns provider credentials, capability evidence, deployment approval, version/binding, sandbox queue, Eino execution, run state and cancellation. `contracts/architect_agent.json` registers `elenio.solution-architect` version **0.3.0**, one iteration, zero tools, strict per-node output configurations, at most 20 nodes. Older version 0.2 proposals can be reviewed but still require current native validation; existing versions are not overwritten.
- Flowcase owns catalog references, runtime capabilities, topology/native validation, tenant permissions and draft creation. Run/version annotations in workflow metadata are provenance notes, not an authorization mechanism.
- The frontend uses same-origin `/agent-api/v1` in local development and the existing `/apps/agent-studio/api/v1` Control Plane route in a production build. It creates no alternate model transport or custom backend authoring endpoint.
- Local development sessions are explicitly labelled. They may have different configured subjects in the same disposable tenant; production requires the shared verified Control Plane subject and trusted Flowcase identity. Local tests do not prove production SSO/token exchange.
- The registered model deployment is approved for **development** only. No evaluated production agent release, model release, business-policy approval or self-approval exemption was created.

## Windows vault decision

The shared AES-GCM vault and master-key design are retained. POSIX systems retain 0700/0600 checks. Windows uses native security descriptors when creating vault directories/ciphertext: current service owner, protected owner-only DACL, atomic create-new files. Existing paths are inspected through a native handle; wrong owner, shared/null/unsupported ACLs and reparse points are rejected. Tests retain the insecure-master rejection and add shared-directory rejection. No permissive Windows bypass was added.

Implementation references: Microsoft [GetSecurityInfo](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-getsecurityinfo) and [DACL semantics](https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-getsecuritydescriptordacl), plus the repository-pinned `golang.org/x/sys/windows` v0.29.0. The existing dependency is now direct; no version upgrade. Linux vault tests cross-compile, but Linux runtime validation was not performed on this host.

Only the owner's named `NINE_ROUTER_API_KEY` was imported into the ignored server environment file and then the encrypted server vault over loopback. Master key, credential, database contents, raw service logs and binaries remain in ignored local state. No credential input exists in the browser. Provider bodies are not reflected in UI errors. The mock model can be explicitly disabled in development; enabling that flag outside development is rejected.

## Reproduce

From the repository root, with dependencies, Playwright, PostgreSQL binaries and the ignored server credential file already provisioned:

```powershell
./scripts/Start-LocalE2E.ps1 -PostgresBin ../tools/postgresql/pgsql/bin -GoExe ../tools/go/bin/go.exe
# Build ai_agent_studio/apps/agent-studio-web once with its existing npm scripts.
./scripts/Start-LocalAgentStudio.ps1 -PostgresBin ../tools/postgresql/pgsql/bin -GoExe ../tools/go/bin/go.exe
./scripts/Configure-LocalArchitect.ps1
```

The Agent service binds only to `127.0.0.1:8190`; Flowcase uses 8080/8081, PostgreSQL 55448. Agent preview data uses the separate `agent_live` database and tracked existing migrations; integration tests use `agent_tests`. The Agent process uses the encrypted private vault and explicitly disables the development mock model. The configuration script imports only the named server variable, probes when development approval is absent, registers/compares the immutable contract and binds it through existing APIs. Its first capability probe consumes tokens; metadata reruns do not request a completion.

Start Vite in `flowcase_platform/web` with `npm run dev`, then open [Architect](http://127.0.0.1:5173/elenio.html#/architect). `Stop-LocalE2E.ps1` stops only recorded owned processes and retains local data.

Normal tests do not call 9Router. Opt-in real-model test, from the frontend directory:

```powershell
$env:ELENIO_LOCAL_E2E='1'
$env:ELENIO_AI_E2E='1'
npm run test:e2e -- --config=playwright.ai.config.ts
```

This explicitly billable suite checks development sessions before mutation. To inspect the stored latest real proposal without another model completion, select only `-g 'stored real-model' --reporter=list`; the fresh-provider test is excluded. Do not point local integration harnesses at customer databases.

## Observed verification

| Layer | Result | Scope |
|---|---|---|
| Five backend modules | 374 passed, 0 failed, 0 skipped | Real disposable PostgreSQL; existing IdP/provider/Docling doubles where harnesses specify them |
| Frontend unit/component | 77 passed | Includes 11 authoring API/contract boundary cases |
| Browser regression | 34 passed | Includes five new authoring journeys with explicit intercepted service contracts |
| Existing local live browser suite | 9 passed | Real Flowcase API/worker/database; branching, operations and independent approval |
| Fresh real-model authoring E2E | 1 passed | Actual Agent API/vault/9Router/queue plus native validation and draft persistence |
| Stored real-proposal UI | 1 passed separately | 1440/1024/768/390 px; no new model calls; serious/critical axe at desktop/mobile |
| Final lint and production build | Passed | TypeScript included; existing lazy ELK size warning retained |

The fresh-model and stored-proposal cases were run separately. `evidence/architect-ai-results.json` records the former one-case passing invocation. Four actual authoring runs occurred during development; the final strict contract version is 0.3.0. A prior 0.2.0 response passed its permissive JSON schema but failed native configuration validation; the per-node schema was tightened and a fresh 0.3.0 response passed. No native validation guard was removed. The accessibility check found a keyboard-inaccessible scrolling conversation and passed after adding focusability. Existing unit `act` warnings remain.

Latest verified fresh run: `09cc6182-b661-414d-ae2d-9709ea7b5231`, agent version ID `f0b88bda-1104-4e5f-877f-008f2445f9e5`, provider mode, succeeded, schema valid, 4,460 input / 1,130 output tokens, zero tools. Created workflow `purchase-request-review-09cc6182`, draft version 1, six nodes/six connections. The synthetic purchase-review request contains true/false exception routing and independent approval. Its registered `poc.budget` policy remains provisional; the warning is visible and requires business confirmation. No monetary inference without an approved rate card.

Actual screenshots: [desktop proposal](evidence/architect-authoring/proposal-desktop.jpg), [saved native draft](evidence/architect-authoring/generated-workflow-desktop.jpg), [mobile stored proposal](evidence/architect-stored-proposal-390.png), [native validation](evidence/architect-real-validation-1440.png). The mobile screenshot includes the keyboard-focused skip link. Automated and in-app-browser screenshots are genuine local app captures; reference HTML and official logo were not modified.

## Remaining gates

Production CP routing/token exchange, production model/agent release and a real model-backed business execution pilot remain unverified. AI extraction, ERP/payment connectors and the provisional business policy have no new readiness claim. General solution hierarchy, artifact uploads, broad capability/KMS authoring UI, actual Economics event ledger/general graph costing, portable release package and full later-phase acceptance remain open. Current authoring is a development sandbox with human review, not an autonomous production builder. No upstream write, main merge or deployment.
