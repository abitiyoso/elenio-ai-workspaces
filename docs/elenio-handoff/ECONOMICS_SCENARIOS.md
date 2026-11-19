# Economics scenario checkpoint — 2026-11-19

## ADR 008 — local planning before a measured cost ledger

The owner requested continued development while an approved AI/provider remains absent. Deliver an isolated estimated-scenario planner at `elenio.html#/economics`, linked from the global navigation and local project Economics panel. Reuse shared UI tokens/primitives and preserve the backend. This is a vendor-process planning template with explicit assumptions, not automatic costing of the currently edited graph. It is not the complete Phase 4 acceptance gate.

The planner reproduces the synthetic fixture in `docs/AI_ECONOMICS.md`. It exposes whole-IDR cost per node visit, unmatched and posting reach, expected extraction attempts, acceptable completion, human review, overhead, implementation/amortization and conventional baseline. Rates are user-entered scenario assumptions with source and effective date; no public/provider prices or approved rate-card status are inferred.

Money is accumulated with BigInt fixed-point scale 100,000,000; probabilities and expected attempts use basis points. Division for amortization truncates below one fixed-point quantum; display rounds IDR. Unsafe display totals and invalid/negative/unknown-field inputs are rejected. Exported inputs remain ordinary JSON numbers; calculated BigInt values are not serialized. Missing reached rates are unpriced and suppress totals/ROI, explicit zero is allowed, and zero acceptable volume returns N/A. Failed attempts stay in the numerator. TCO includes implementation once; monthly allocation alone amortizes it. Nonpositive unit advantage never produces a false minimum-volume break-even; a lower fixed-cost scenario explains the low-volume comparison.

The template assumes extraction, lookup and policy always execute, exception reach is unmatched probability, posting reach is explicitly entered, trigger/approval machine charge is zero, and human labor applies to each attempted case. No graph inference, correlated branch model, parallel/join analysis, general retry distribution, currency conversion, token normalization or quality-compliance optimizer is advertised. These remain open engineering work.

## Scenario revisions and portability

Explicit Save browser revision uses a separate IndexedDB database `elenio-local-economics-v1`, store `revisions`, auto-incrementing revision ID. An atomic read/write transaction checks the 20-revision limit and appends a new snapshot; prior revisions are never overwritten. Save is disabled during loading/saving. Storage failures retain current inputs and expose an error. Nothing is auto-saved or sent to a service. These are local planning records, not tenant-shared/approved objects; no auth token, workflow definition, credential or customer document is stored by this feature.

JSON export includes schema version, revision, creation timestamp, scenario inputs, source and effective date. Import validates schema, fields, numeric bounds and effective date before an explicit replacement; browser history is retained. A local exported revision does not assert server-side authorization or approval. Currency is IDR only.

## Validation and scope

Six deterministic unit cases cover the full kit fixture (machine 1,050, variable 2,550, operating 4,550,000, allocated 5,550,000, baseline 12,000,000, reduction 53.75%, break-even 318, payback ~1.61 months, 12-month TCO 66,600,000 vs 144,000,000, cost-based ROI ~116.22%). Additional assertions cover p=0/p=1, denied posting, extraction retries, failed-case cost, zero denominator, unpriced vs free, negative benefit and invalid ranges.

Browser cases cover editable calculations, unknown-rate state, browser revision reload, JSON roundtrip/rejected schema, responsive overflow at 1440/768/390 and serious/critical axe checks. Actual app screenshots are `evidence/economics-*.png`. These screenshots contain synthetic planning assumptions only. See current JSON test reports for exact results.

No backend, live rate integration, usage ledger, event dedup/adjustment, tenant cost rollup, budget enforcement, actual-model comparison or FX pipeline is delivered. Existing provider/Windows vault blockers remain. No production branch merge, upstream write or deployment.
