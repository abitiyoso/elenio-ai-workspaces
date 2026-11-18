# Phase 1 implementation guide

Run from `flowcase_platform/web`:

```powershell
npm ci --ignore-scripts
npm run dev
# Open http://127.0.0.1:5173/elenio.html#/
# Existing authenticated app remains http://127.0.0.1:5173/
npm run lint
npm run typecheck
npm test
npm run build
npx playwright install chromium
npx playwright test
```

The production build includes two HTML entries; `elenio.html` is explicitly a design fixture, never the default runtime shell. No live credentials are needed for it. All network-dependent actions are unavailable with an explanation. The fixture draft is saved only when Save draft is selected; IndexedDB database `elenio-phase1-fixtures` contains a single canonical draft and independent positions, not auth tokens or execution state. Export JSON is available if device storage fails. No multi-user concurrency claim is made for device storage; the existing backend client retains If-Match semantics.

## Source map

* `packages/ui`: shared tokens, buttons, lifecycle labels, status/alert messages, accessible dialogs. Component gallery at `#/design-system`.
* `src/platform/Platform.tsx`: responsive shell, role preview, project context, manual/Architect entry, command navigation, project readiness.
* `src/platform/Studio.tsx`: draft editor, node/config/edge inspector, palette, local preflight, undo/redo, native import/export and trace unavailable state.
* `src/platform/WorkflowCanvas.tsx`: React Flow controlled semantic graph with typed ports, drag/pan/zoom/fit/minimap, search-driven selection.
* `src/platform/graphModel.ts`: semantics adapter. Condition editing changes `next` and `config.branches` atomically; imports preserve metadata, schemas, policies and opaque extension fields. Invalid imports/topology are diagnosed, never run.
* `src/platform/layout.ts`: lazy ELK layered layout, separate from semantic serialization. The larger ELK algorithm chunk is loaded only for layout, not on Home.
* `src/platform/Knowledge.tsx`: searchable/filterable/sortable synthetic document explorer and contextual source preview; no invented jobs, ingestion or retrieval call.
* `src/platform/fixtures.ts`: all synthetic records/bindings. Vendor sample includes parallel checks, all-join, matched bypass, unmatched analyst and independent human approval. No real ERP adapter or executable release is implied.

## Review procedure

Home → sample project → Workflow Studio. Pan/zoom or find a node, inspect true/false branches, drag/connect, add/delete steps, undo/redo and inspect diagnostics. Save locally, navigate away and reload. Export/import native JSON. Switch to Reviewer and verify editing is unavailable. Knowledge → search/filter → preview source → Escape; inspect unpublished/revoked status. On mobile review nodes and select an inspector rather than shrinking a whole desktop graph.

No Phase 2 implementation, production branch merge or deployment occurs before owner acceptance. See STATUS for actual results and blockers.

Owner-review correction: `Launchpad.tsx` restores the final V10 Home content; `ProjectSections.tsx` holds contextual project tabs; AI Architect at `#/architect` preserves the current brief across navigation. Inter Variable loads from bundled Fontsource assets. Actual AI generation, independent new-project creation and Agent Studio authoring are not connected; unavailable actions explain these limits. See KIT_ALIGNMENT_PHASE1.md.
