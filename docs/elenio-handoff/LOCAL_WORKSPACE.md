# Existing services in the local Elenio workspace — 2026-11-20

**Superseding integration update:** [HTML_WORKSPACE_INTEGRATION.md](HTML_WORKSPACE_INTEGRATION.md) adds original KMS on 5380→8380 and UCW on 8480 using independent source grants and actual CP exchange. All five original services now run in the native harness. KMS metadata/collection creation and UCW delegated case context are browser verified. Storage/search/PDF/embedding and UCW model intake remain configuration gates. Agent now uses the shared light system from PR #10. The initial three-service checkpoint below is retained as history, not the current integration limit.

Branch: `feat/elenio-local-workspace-integration`, based on Publisher checkpoint `08ef430701e2c36bc8cf8c5906bb882cf22b3220`. This increment connects existing functions. It does not replace their backends or close whole-product acceptance.

Tested implementation/evidence SHA: `c09dfcda9660e326fc8b70d72e3c707f4e5c2f82`. Delivery: [draft PR #9](https://github.com/abitiyoso/elenio-ai-workspaces/pull/9), stacked on PR #8. The feature tip also contains delivery metadata; compare local/remote refs for its final SHA. Private owner repo verified as `abitiyoso/elenio-ai-workspaces`; main stays `8bfae869e8ad3adaf10b496428ffbcf22c196db9` and upstream pushes stay disabled.

## Reused implementation

| Function already in the team source | Integration in this increment |
|---|---|
| CP OIDC login, server sessions, CSRF, app entitlements, audience tokens and reverse proxy | Run the original CP server natively; expose the mounted Elenio frontend and original Agent Studio through its original proxy |
| CP workspace selection and membership | Show the actual active workspace/person in the existing shell control; link to the original CP selector before changing server session scope |
| Flowcase identity directory, native workflow authoring, validation and immutable publication | Preserve domain user IDs; add verified JWT issuer/subject to the session response for cross-service identity comparison |
| Agent registry, skill/tool workbench, Model Hub, evaluation and release UI/API | Existing governed app launcher opens the service's existing views with whitelisted hash deep links; no duplicate authoring backend |
| Agent credential vault, model probes, approvals and Eino worker | Reuse the existing configured server/vault; remove the UI's obsolete team-specific probe-host check and leave the actual backend endpoint allowlist authoritative |
| Original Agent Studio static server | Fix Windows URL asset resolution: filesystem UNC normalization incorrectly returned index.html for JavaScript; missing assets now return 404 |
| CP solution plans and immutable Publisher candidates from prior increments | Verify their UI against a real persistent CP session and real upstream/database, replacing the previous fixture-only UI evidence for these paths |

The Elenio shell keeps its agreed logo, bundled font, tokens and layout. The service-owned Agent Studio interior retains its original UI in this increment. Unified visual adoption inside that legacy module is still open; opening a working original module is not a claim of complete V10 parity. Skills/tool mutation and production releases retain their existing source restrictions and independent approvals.

## Run and inspect

Prerequisites are the existing local PostgreSQL cluster/Go tools, installed frontend dependencies, built `control_plane/web`, and the previously configured local Agent Studio database/private vault. No new model credential is imported by this script.

From the repository root:

```powershell
./scripts/Start-LocalWorkspace.ps1 -GoExe ../tools/go/bin/go.exe -PostgresBin ../tools/postgresql/pgsql/bin -Restart
```

Open `http://127.0.0.1:8200/apps/flowcase/elenio.html`. The loopback-only sign-in explicitly says **development test identities**. It is an opt-in fixture IdP, not corporate SSO. It uses browser-bound authorization requests, signed ID tokens, PKCE, nonce, single-use expiring codes, exact callback/Origin and loopback host checks. Production `control-plane-api` does not import this tooling.

Select **Demo All Access** for builder verification. Use the workspace control to open the original CP selector and select **Platform Engineering** before opening Agent Studio. Its app is intentionally limited to Engineering by the original registry. Select **Maya Putri / SIM B2B Solutions** to verify operator restrictions. These are the original synthetic source directory identities; no CP role is copied into a domain grant and no self-approval exemption is configured.

| Listener | Existing service / data boundary |
|---|---|
| 8200 | Original CP API/web/proxy; separate `control_plane_workspace_live` DB |
| 8210 | Explicit local fixture IdP; no corporate users |
| 5280 → 8280 / 8281 | Native frontend gateway → original trusted-JWT Flowcase API/worker; separate `flowcase_workspace_live` DB |
| 8290 | Original CP-authenticated Agent Studio; existing `agent_live` DB and protected encrypted FileVault |

The earlier 5173/8080/8190 development services remain separate. The two Agent workers use the source runtime's database leases. No new business-model run is scheduled by workspace startup. All listeners bind `127.0.0.1`. Protected workspace server keys/config/logs live in ignored `.local-e2e/workspace-private/`; persisted session signing/sealing keys survive restart. No browser token storage is added. `Stop-LocalE2E.ps1` stops only PID/executable pairs owned by this sandbox, then retains the database files.

## Verification

```powershell
./scripts/Test-LocalServices.ps1 -GoExe ../tools/go/bin/go.exe -PostgresBin ../tools/postgresql/pgsql/bin -Modules control-plane,flowcase,agent
# In flowcase_platform/web:
$env:ELENIO_LOCAL_E2E='1'; $env:ELENIO_WORKSPACE_E2E='1'
npx playwright test --config playwright.workspace.config.ts
```

Latest scoped results: CP **53**, Flowcase **110**, Agent Studio **87** backend tests pass with real disposable PostgreSQL; zero failures/skips. CP local-identity tests were rerun after the final browser-policy correction, and the full Agent suite after the Windows fix. Flowcase frontend **88** unit/component, **40** existing browser regressions and **3** new workspace browser cases pass; lint and both mounted frontend builds pass. Existing act() warnings and the lazy ELK size warning remain. KMS/UCW suites were not rerun for unchanged code in this increment.

The three new browser cases use **no route interception**:

1. Real CP login/session and Engineering selection; verified CP issuer/subject agree across services while Flowcase's domain user ID remains distinct. Existing Agent Registry, Skills Studio and Models/Providers load; a real draft agent persists without automatically assigning a SME owner. Provider registration is observed; no probe/generation or new billable model call is made.
2. Create/edit/save a native workflow, validate/publish a synthetic start/end version under the original publisher permission, compose and reload a solution, capture/reload a real immutable candidate. Verify 1440/768/390 layout and zero serious/critical axe findings for the candidate view. The candidate stays unapproved and undeployed.
3. A case operator cannot access solutions or Agent Studio and cannot promote themselves with browser tenant/role/Serve/dev-persona headers.

Evidence: `evidence/workspace-e2e-results.json`, `workspace-existing-agents-1440.png`, `workspace-publisher-{1440,768,390}.png`, `workspace-verification.json`. Screenshots were inspected. Native services and PostgreSQL are real; the identity provider and people are explicitly synthetic. Earlier contract screenshots keep their original labels and provenance.

## Open acceptance

KMS/UCW CP upstreams remain explicitly pending in this local harness; search, document conversion/storage/index dependencies and their existing service/delegation configuration must be connected and evidenced. Existing backend functions are not classified as missing merely because their new shell flow is unfinished. Agent evaluations/releases exist but their full governed business pilot is not proven by this authoring smoke test. General Economics ledger/rates/rollups, transitive Publisher readiness/solution approvals, executable pinned release bundles and independent installation scenarios remain open. No Docker sandbox has been supplied; clean install/upgrade/backup/restore/rollback remain external gates. No upstream write, main merge or production deployment is performed.
