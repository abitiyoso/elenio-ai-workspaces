# Elenio shared UI foundation

Source-only React primitives and CSS design tokens, compatible with React 18/19. The consuming Vite app supplies/deduplicates React and Radix Dialog peer dependencies. Import tokens before application composition styles. No network assets, runtime state, service API clients or authentication belong here.

`Button`: primary/secondary/quiet/danger, native button attributes and focus semantics. Disabled actions need an adjacent reason.
`Badge`: text-first lifecycle labels, color is supplementary.
`Notice`: status/alert messages.
`DialogPanel`: Radix focus trap, Escape/dismiss, accessible title/description and trigger focus restoration.

Component showcase: Flowcase `elenio.html#/design-system`. Adoption by other apps is incremental after the Phase 1 acceptance gate; this package does not migrate their React versions.
