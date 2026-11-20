# Elenio shared UI foundation

Source-only React primitives and CSS design tokens, compatible with React 18/19. The consuming Vite app supplies/deduplicates React and Radix Dialog peer dependencies. Import tokens before application composition styles. No network assets, runtime state, service API clients or authentication belong here.

`Button`: primary/secondary/quiet/danger, native button attributes and focus semantics. Disabled actions need an adjacent reason.
`Badge`: text-first lifecycle labels, color is supplementary.
`Notice`: status/alert messages.
`DialogPanel`: Radix focus trap, Escape/dismiss, accessible title/description and trigger focus restoration.

Component showcase: Flowcase `elenio.html#/design-system`. Adoption by other apps is incremental after the Phase 1 acceptance gate; this package does not migrate their React versions.

`mobileNavigation.ts` supplies shared drawer focus, Tab wrapping, Escape and trigger restoration without owning identity or service state. Original KMS/UCW use verified snapshots so their separate frontend packaging remains available. `scripts/prepare-workbench.mjs` is the canonical snapshot tool; run each app's `scripts/shared-ui.mjs --sync` only for an explicitly reviewed update. Its seven pins cover the tool itself, tokens, mobile navigation, exact official logo/icon, Inter Latin WOFF2 and OFL license. `predev`/`prebuild` check local pins and canonical files when present; text hashes normalize LF/CRLF. No CDN or new UI dependency is needed. Agent retains its previously verified packaging adapter.
