# Existing Agent workbenches in the agreed Elenio system — 2026-11-20

Branch `feat/elenio-shared-agent-workspace`, based on `7454f7509e94e1695e693b91fdd49b1685b55789` (PR #9). This is adoption of the existing agreed design, not a new design direction or backend rewrite. It supersedes the previous checkpoint's statement that Agent Studio still has a dark legacy interior.

Tested implementation/evidence: `be0cc369b9e04fd153a8f860069230cf7e10e91b`, [draft PR #10](https://github.com/abitiyoso/elenio-ai-workspaces/pull/10), stacked on PR #9. The feature tip also contains delivery metadata. Only the private owner feature branch is pushed; original main/upstream and production are preserved.

The original Agent Registry, Skills Studio, Tool Library, Playground, Evaluations, Releases, Runs/Audit and Models/Providers React workbenches remain. Their API calls, schemas, mutations, provider vault, approvals, immutable versions and permissions are unchanged. The service now uses the same Inter Variable font and canonical `packages/ui/tokens.css`, light sidebar, official unchanged logo, readable form/helper text and shared color/status tokens. The main Elenio builder design is preserved.

Registry creation is the primary action. Existing SIM bootstrap/configuration stays available in a native, keyboard-operable disclosure instead of dominating general agent creation. Actual verified CP workspace/person replaces the host placeholder. Return to solution engineering appears only for an exact registered/entitled proxied Flowcase route; CP membership/launch and service authorization remain authoritative. No role switch or browser-supplied identity is added.

Browser accessibility checking found a real keyboard defect in a horizontally scrolling model table. A reusable `TableScroll` component now supplies a labeled focus target for six existing model/evaluation/release/run table regions, including Safari keyboard use.

## ADR — shared assets with isolated source builds

Canonical tokens remain under `packages/ui`; the official logo remains the original supplied asset already used by Flowcase. Agent Studio's existing Dockerfiles build its frontend in isolation. A pinned **source snapshot**, not a second hand-maintained theme, lets that packaging remain usable without copying the whole private monorepo into its Docker context.

`scripts/shared-ui.mjs` verifies four SHA-256 pins in `shared-ui.json`: tokens, official logo, bundled Inter Latin WOFF2 and its SIL OFL license. Text pins normalize LF/CRLF for portable checkouts; logo/font binary pins remain byte-exact. It also checks the canonical source whenever available in the monorepo. `predev`/`prebuild` fail on drift; `--sync` explicitly refreshes the source snapshot after a reviewed canonical change. No CDN dependency or additional frontend package is required. Both monorepo and isolated-snapshot verification pass; a deliberately altered disposable snapshot is rejected.

The existing source packaging recipe had excluded all PNGs and only the exact `.env` filename. Its exclusions now preserve the official brand image and exclude `.env` plus `.env.*`; Agent's Docker context also excludes these nested environment variants. A local source archive check verifies the logo is present and environment files are absent. No deployment script, SSH command or Docker installation was executed.

## Evidence

Four real native workspace browser E2E cases pass, without route interception. They retain CP login and independent-grant checks, original Agent draft creation, workflow→solution→candidate persistence and operator denial, and add:

- Agent Registry at 1440/1280/1024/768/390; Skills and Models at 1440/390, with no horizontal page overflow.
- Actual bundled Inter loaded; served logo bytes equal the approved canonical image; sidebar is light.
- SIM disclosure expands/collapses, and mobile return to the engineering shell uses the entitled route.
- Zero serious/critical axe findings across Agent/Skills/Models desktop and mobile views.

Nine actual captures are named `evidence/shared-agent-studio-{agents,skills,models}-{width}.png`; desktop/mobile captures were opened and inspected. Current browser JSON is `evidence/workspace-e2e-results.json`; focused verification is `shared-agent-verification.json`. Mounted build/typecheck and shared-asset preflight pass; Flowcase lint covers the new E2E. Backend code is unchanged; PR #9's 250 backend/88 unit/40 regression checks remain the preceding checkpoint, not newly rerun results. No new model call or production deployment.

## Remaining work

This does not make every screen/menu equal to the HTML yet. The exact HTML navigation and functional-content reconciliation is tracked in [HTML_ROUTE_MATRIX.md](HTML_ROUTE_MATRIX.md). Service-owned form workbenches still need selected V10 page/content refinements. Native KMS/UCW connection, broader Economics, governed solution release and executable installation remain separate work. Existing backend functionality must be connected and verified before being classified as a new capability gap. Production SSO and independent installation gates remain open.
