# Phase 1 design and interaction decisions

Official documentation checked 2026-11-18. These are interaction benchmarks, not copied product code or branding.

| Reference | Pattern observed | Elenio decision / evidence |
|---|---|---|
| [n8n node interactions](https://docs.n8n.io/build/understand-workflows/workflow-components/work-with-nodes) | Searchable node insertion and contextual node configuration | Searchable add palette; typed ports; inspector selected from graph or node search. No n8n runtime embedded. |
| [Linear search](https://linear.app/docs/search) | Fast keyboard entry, focused resource search and local view filtering | Ctrl/⌘ K command dialog; graph-specific node search; compact global/project hierarchy. Shortcuts chosen for Elenio, not advertised as matching Linear. |
| [Notion views, filters and sorts](https://www.notion.com/help/views-filters-and-sorts) | Document rows with properties, local filtering/sorting and contextual page opening | Document-first Knowledge table, collection navigation, status filter, alphabetical sort and on-demand preview. Counts derive from explicit fixture records. |
| [Langfuse observability](https://langfuse.com/docs/observability/overview) | Trace observations related to usage/cost and quality | Execution trace separated from design. No fabricated activity chart, token count, cost or financial benefit. Node economics stays unpriced until approved rates/usage exist. |
| [Spline events and states](https://docs.spline.design/interaction-states-events-and-actions/concepts/events-interactivity) | User input changes selected object state and associated behavior | Selectable geometric agent identities reveal role/authority. Hover/selection is interaction state, never a claim of running an agent. Reduced-motion preference respected. |

## Visual direction

Official supplied PNG logo used byte-for-byte; navy/cobalt interface on light neutral surfaces. Purposeful ice-blue/violet knowledge/agent accents and amber human authority. No external fonts, generated logo or remote visual assets. V10 functional screenshots, V9 economics guidance, PRD baseline and latest UX text are references; rejected zig-zag graph/sparse Knowledge layout avoided.

## Decisions found through browser QA

* Initial full-graph fit made eleven horizontal nodes unreadable. Default viewport now uses readable zoom; pan, minimap and explicit fit offer an overview. Selecting/searching a node focuses it. Branch geometry comes from ELK and `next`, never array order. Coordinates are separate from the canonical definition.
* At small widths a readable node review list replaces editing the shrunken canvas. The same node inspector exposes true/false targets and configuration; reviewer preview is read-only.
* Knowledge is a useful document list before a detail is selected; preview opens on demand. Revoked/review/indexing fixtures cannot be described as published evidence. Ingest and governance actions explain their missing live session dependency.
* Dialogs trap focus, Escape closes and restores focus. Mobile navigation hides off-canvas links and supports Escape/Tab containment. Measured contrast defects are corrected before final screenshot capture.
* Home does not generate a fake AI blueprint: an unconfigured Architect explains prerequisites and preserves the typed brief while its dialog is open.

## Explicit limits at visual gate

Phase 1 is a fixture-backed frontend slice. Server project mapping, capability negotiation, live GET/save/validate integration into the new shell, immutable version history, actual agent/model binding, ingestion, ACL/release enforcement, runtime trace, economics ledger and customer deployment remain for later accepted phases. The original authenticated app and API clients remain intact. The fixture role selector is a review aid, not security evidence. No approval or publish action is simulated.
