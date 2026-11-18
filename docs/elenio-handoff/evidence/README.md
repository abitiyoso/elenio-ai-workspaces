# Actual browser and test evidence

Captured with Chromium via Playwright against the local fixture entry. All records and bindings are synthetic. Screenshots use `fullPage:true`, so tall content captures may exceed the configured viewport height.

| Viewport | Home | Project | Workflow | Knowledge |
|---|---|---|---|---|
| 1440×900 | [Home](home-1440.png) | [Project](projects-vendor-payment-1440.png) | [Studio](projects-vendor-payment-workflow-1440.png) | [Knowledge](knowledge-1440.png) |
| 1280×800 | [Home](home-1280.png) | [Project](projects-vendor-payment-1280.png) | [Studio](projects-vendor-payment-workflow-1280.png) | [Knowledge](knowledge-1280.png) |
| 1024×768 | [Home](home-1024.png) | [Project](projects-vendor-payment-1024.png) | [Studio](projects-vendor-payment-workflow-1024.png) | [Knowledge](knowledge-1024.png) |
| 768×1024 | [Home](home-768.png) | [Project](projects-vendor-payment-768.png) | [Studio](projects-vendor-payment-workflow-768.png) | [Knowledge](knowledge-768.png) |
| 390×844 | [Home](home-390.png) | [Project](projects-vendor-payment-390.png) | [Node review](projects-vendor-payment-workflow-390.png) | [Knowledge](knowledge-390.png) |
| 360×800 | [Home](home-360.png) | [Project](projects-vendor-payment-360.png) | [Node review](projects-vendor-payment-workflow-360.png) | [Knowledge](knowledge-360.png) |

[Selected node / resized inspector](inspector-1440.png).

Observed and fixed: unreadable initial whole-graph zoom, missing minimap dimensions, dialog focus restoration, skip-link routing, mobile off-canvas keyboard access and a measured 4.08:1 text contrast failure. Final responsive assertions detect no page horizontal overflow. At readable zoom the desktop graph intentionally pans beyond the viewport; Fit and minimap provide overview. Mobile uses a readable list.

Machine-readable results: [frontend units](unit-results.json), [browser tests](e2e-results.json), [Go pass/fail/skip summary](go-baseline-summary.json), [production dependency audit](npm-audit-production.json), [development audit after compatible fixes](npm-audit-after.json), [baseline audit](npm-audit-before.json).
