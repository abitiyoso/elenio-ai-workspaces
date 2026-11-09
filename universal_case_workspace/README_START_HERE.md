# Universal Case Workspace — Implementation Package v1.0

The reusable **experience layer** of AI Workspace, built **after the Control Plane** and using KMS, Flowcase, Agent Studio. Does not duplicate identity, workflow, document processing or Eino.

## Open first

- `Universal_Case_Workspace_Prototype.html`: self-contained interactive HTML. Switch between SIM Tender, Contract Review and PoC cases; watch navigation/fields change from source-controlled domain pack fixtures. **All operations are local simulations.**
- `UNIVERSAL_CASE_WORKSPACE_TECHNICAL_PRD.md`: technical scope, boundaries, data model, APIs, auth, tests and phases.
- `CODEX_MASTER_PROMPT.md`: phased implementation; run **Phase 0 only** first.
- `domain-packs/*.json`: three reusable workspace definition examples.
- `contracts/*.json`: JSON Schema for pack definition and proposed API contracts.
- `samples/demo_cases.json`: fictional case/artifact/task/agent/KMS metadata for screenshots/tests.
- `INTEGRATION_AND_SECURITY_GUIDE.md`: host wrapper / service adapter and artifact ownership decisions.
- `UI_DESIGN_NOTES.md`: UI behavior and visual handoff.
- `previews/`: screenshots and `tests/` browser smoke + manifest validation tests.
- `prototype_source/` and `scripts/build_prototype.py`: editable HTML/JS sources and offline regeneration script.
- `samples/control_plane_miniapp_registration.json`: illustrative registration for Control Plane (validate actual API before use).

## How this fits

```
Control Plane (SSO + user/workspace + mini-app registry)
   └── Universal Case Workspace (generic UI + BFF)
        ├── Flowcase (case/workflow/tasks/approvals)
        ├── KMS (documents/evidence)
        ├── Agent Studio (Eino AI agent runs)
        └── Domain Packs / Domain Artifact Adapters
             ├── SIM Tender
             ├── Contract Review
             └── PoC Assessment
```

## Demo walkthrough

1. Open HTML, select **SIM Tender**; open a case and review *Requirements* schema fields.
2. Switch to **Contract Review**; the **same UI** shows *Clause Register* and *Deviations*.
3. Switch to **PoC Assessment**; the **same UI** shows *Experiments* and *Result Assessments*.
4. Open an artifact, edit allowed fields and save a draft revision. The old version stays in demo history, and approval is **not** performed.
5. Run a contextual agent simulation; it produces an **AI draft**, not an approval.
6. Open Tasks, Approvals, Documents/Evidence and Timeline; these are driven by domain fixtures and mock provider adapters.
7. Open **Domain Packs** to inspect/validate manifest JSON and understand version pinning. Prototype manifest is read-only; production publishing must require server authorization.

**Demo disclaimer:** roles, tasks, evidence, case records, AI runs, permissions, timestamps and health are mocked in browser memory/localStorage. There is no real SSO, Flowcase/Agent Studio, KMS, PostgreSQL or backend. No claim of production integration.

## Start in Codex

Attach **all files** to the coding agent and paste `CODEX_MASTER_PROMPT.md`. Require Phase 0 ADRs and verification of deployed APIs before Phase 1. Do not let Codex turn domain demo conditions into generic core behavior.

## Required decisions

- Does Flowcase already own structured artifact revisions, or only artifact reference registry?
- Will workspace definition versions live in Control Plane registry DB or a new UCW-owned registry?
- What is the exact Control Plane delegated token and Flowcase case-member API contract?
- How are domain-specific actions validated/authorized beyond generic rendering?
