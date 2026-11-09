# UI Design / Prototype Specification

## Style direction

Professional **Navy + Cobalt + White** (from the user's final KMS preference), Apple-inspired bento rhythm while maintaining readable enterprise tables. Navy sidebar (`#101C33`), cobalt actions (`#2764EA`), pale slate application background (`#F3F6FC`), white document/work areas (`#FFFFFF`), dark ink (`#18243B`). A **restrained translucent top bar** is the only prominent glassmorphism effect. Cards use clear borders, depth and generous padding; no faded text, neon/purple gradients or promotional AI graphics.

## UI screens

- Overview — bento KPIs, priority cases, actionable tasks, approval queue, activity.
- Workflows — searchable workflow catalog, published/draft statuses, examples and template creation.
- Designer — visual flow + node inspector; select/add/edit nodes, run local validation, save draft, clone new version. Demo publish changes local state only.
- Cases — filterable case list; inspect progression, details and create a sample case.
- My Tasks — simulated task completion; see state changes.
- Approvals — simulated approve/reject with rationale; production is server-authorized only.
- Integrations — explicit KMS / Agent Runtime / Host Auth connector boundary and status.
- Audit — demo local events; production log must be immutable and server-side.

## Demo controls

Data is seeded with fictional case IDs and illustrative process steps. Workflow and case actions use browser localStorage for **demo data only**. No actual workflow executor, KMS, AI, identity provider, approvals or persistence exists. The browser cannot act as authority. Prototype is a UI reference for Codex, not an executable platform service.

### Test walkthrough

1. Open Workflows, select **SIM B2B Tender**, click **Open Designer**.
2. Select any node in the diagram, adjust title/type/next in inspector, save draft or validate (demo validation only).
3. Open **Cases**, create a sample case from published workflow.
4. Visit **My Tasks** or **Approvals**, perform a demo action and inspect Activity.
5. Open **Integrations**: note explicit KMS / AI / wrapper boundaries.
6. Refresh browser: local demo state remains (where browser localStorage is available). Use Reset Demo for fresh seed.

## Production handoff

Replace every demo state operation with typed, authorized REST endpoints. Do not let frontend mutate a published workflow version or decide approver identity, tenant, policy or transition legality. Preserve version IDs, audit and correct role permissions. Mobile sidebar should be keyboard-accessible and dismissible; tables become stacked cards on narrow screens.
