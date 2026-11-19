# PRD extension — Elenio AI OS delivery

Extends the original PRD and master prompt; does not remove any FR/UAT requirement. Source: [owner addendum](../PRODUCT_DELIVERY_ADDENDUM.md), 2026-11-19. Existing agreed design is preserved. Full commercial billing, Kubernetes automation and advanced licensing are lower priority than one complete working customer solution on Docker Compose.

| ID | Requirement / owner | Phase | Acceptance evidence |
|---|---|---|---|
| OS-01 | Shared core for internal engineering, standalone solution, enterprise platform | 2/5/6 | Explicit mode composition and rights; customer solution excludes engineering tools unless included |
| OS-02 | Solution Publisher: business UI, API service or both, preview and dependency resolution | 2/3/5 | Version-pinned authorized workflow/agent/skill/tool/model/knowledge/integration/permission inventory; no editable-plan approval shortcut |
| OS-03 | Reusable OCI software and immutable versioned configuration releases | 5/6 | Digest/image inventory, definition/binding/config checksums, approved release fingerprint; no software rebuild for configuration-only change |
| OS-04 | Portable Compose bundle and target-owned URLs, reverse proxy, HTTPS, SSO | 6 | Clean install, actual DNS/certificate/login and service readiness, not a rendered URL or manifest |
| OS-05 | Customer independence and data sovereignty | 6 | Execute with Elenio development disconnected; customer-owned CP/IdP/data/storage/vault/models; external-network dependencies declared |
| OS-06 | Licensing-ready entitlements separate from identity/authorization | 2/6 | Central interface for mode/workspace/builder/user/capacity/features/support/offline signature; actual limits server enforced; no mandatory external license call |
| OS-07 | Economics across agent/workflow/execution/workspace/BU/organization | 4 | Estimate, measured test and actual reconciled independently; tools/retries/OCR/infra/human/license costs, baseline comparison |
| OS-08 | Complete standalone customer application scenario | 5/6/7 | Build/test/approve/publish/install, dedicated URL, authorized case/artifacts/human approval/costs; backup/restart/upgrade/rollback retains operational data |
| OS-09 | Complete full-enterprise scenario | 6/7 | Separate customer install; enterprise admin independently publishes two departmental workspaces with isolated data/access and shared authorized resources |

The current solution registry advances OS-01/02 planning identity only. It does not satisfy solution release, OCI packaging, target install or final UAT. Current audit and blockers: ../AI_OS_DELIVERY.md. Original editable PRD DOCX and synced source materials remain unchanged.

## Publisher candidate increment

OS-02/03 now have declarative page composition/preview and immutable, versioned
review snapshots. These are planning candidates, not independently approved
solution releases or installable bundles. Direct native reference discovery
does not resolve transitive agent/model/tool/KMS dependencies. Exact current
functions and evidence: ../PUBLISHER_CANDIDATES.md. Final installation and
runtime scenarios remain required.
