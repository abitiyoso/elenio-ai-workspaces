# Phase 1 UI decisions

## Design read

Reading this as: an internal model-governance console for platform admins and AI Ops, in the supplied navy/cobalt enterprise language, with ENERGY 1 / RHYTHM 2 / MOTION 1.

The existing `UI_DESIGN_NOTES.md` and standalone HTML prototype are the source of the visual direction. The square registry markers repeat the prototype's compact workspace motif; cobalt marks the active route and primary action. Solid white surfaces separate records and forms. Tables scroll horizontally at narrow widths instead of compressing values.

## Scope and behavior

- The first connected view is Models & Providers, backed by the Phase 1 model registry API.
- Provider and deployment records are fetched from tenant-scoped API routes. There are no browser fixtures or local storage values.
- Provider creation only stores HTTPS endpoint metadata. This API does not accept credentials or run provider probes.
- Deployment capabilities are declarations until a backend probe exists. Approval and model binding remain unavailable.
- Other navigation entries explain that their API-backed screens are not implemented; they do not show sample tenant data.
- The host owns login and tenant context. The app does not render a login screen or accept tenant identity from the browser.
