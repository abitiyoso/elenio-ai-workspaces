# Universal Case Workspace Package QA — 31 October 2026

**Scope:** standalone HTML design prototype and JSON fixtures only. No production Go, React, SSO, database, KMS, Flowcase or Agent Studio instance was executed in this package QA.

## Automated local verification

| Check | Result |
|---|---|
| JSON Schema Draft 2020-12 syntax and all domain pack matches | PASS |
| Domain Pack semantic references (navigation → artifact type, action → registered agent, unique IDs) | PASS (fixture validator) |
| Case fixtures pinned to corresponding pack versions and artifact type keys | PASS |
| JavaScript syntax (`node --check`) | PASS |
| Desktop Chromium smoke test, no page errors | PASS |
| SIM Tender case generated 8 configured tabs; requirement fields rendered | PASS |
| Legal Contract case generated Clause and Deviation modules | PASS |
| PoC case generated Experiments and Result Assessments; numeric field typed number | PASS |
| Artifact save created vNext draft, retained revision history | PASS |
| Contextual agent demo produced **AI draft**; no approval | PASS |
| Review dialog not allowed to approve a Flowcase gate | PASS |
| Viewer role hides draft save action (demo UI only) | PASS |
| Domain Pack Library shows three examples and JSON preview | PASS |
| Mobile navigation and 390px viewport document width | PASS |

## Prototype limitations

- All data, cases, task statuses, approval views, KMS citations and AI run states are fictional UI fixtures.
- Browser localStorage supports demo-only state persistence; no authZ or server access may rely on it.
- The mock role selector never establishes real user identity or permissions.
- Manifest checks in the browser are structural and incomplete; production server must enforce full schema, semantic, referenced workflow/agent and policy validation.
- There is no real support yet for `If-Match`/ETag conflicts, delegated service tokens, signed callbacks, distributed audit, KMS source access, or Flowcase approval decisions.
- The JSON contracts and registration samples are proposed, not confirmed deployed service schemas.

## Re-run

```
python scripts/build_prototype.py
python tests/validate_packs.py
python tests/test_ui_smoke.py
```

Requires Python `jsonschema`, Playwright Python package and Chromium. The UI itself requires no dependencies: open `Universal_Case_Workspace_Prototype.html` directly.
