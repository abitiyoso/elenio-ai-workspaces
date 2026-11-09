# Universal Case Workspace UI & Interaction Spec

## Visual design

- Navy `#101C34` sidebar, Cobalt `#2864EC` primary action, white work surfaces and blue-gray background `#F3F6FB`.
- Apple-inspired bento overview **without fading** labels or excessive glass. High-contrast readable tables/editors. Strong hierarchy and restrained rounded corners.
- Distinct **generic** app navigation and **domain-generated** case navigation. No hidden SIM-specific items in core left sidebar.
- Sticky case header with case title/status, pinned workflow stage, owner and pack/version. Compact horizontal flow strip; right rail with reviews and AI activity.

## Screens

1. **My Cases** / Overview with cross-domain tiles, counts, filters and recent activity.
2. **Case Workspace**, with tabs generated from selected case's Workspace Definition JSON.
3. **Artifact List & Inspector** using schema widget registry (text, textarea, select, number, date, boolean, evidence_ref); editor supports draft revision and explicitly **does not approve**.
4. **Tasks** (Flowcase), **Approvals** (Flowcase) and **Evidence** (KMS).
5. **AI Activity** (Agent Studio) showing manifest-allowed actions and mock run result as draft.
6. **Timeline & Audit** showing source-stamped case activity.
7. **Domain Packs** explaining selected published template, mapped workflow and agent actions; manifest JSON preview and local schema validation results.

## Prototype buttons/actions

- Select domain/case, navigate domain-generated tabs, open artifact drawer, edit and save draft, run contextual AI action simulation, mark a demo task completed, inspect approvals **without final approval** (requests review only), view KMS citations (metadata only), and inspect JSON manifest.
- Local simulated edits use browser memory/localStorage, never a backend. Do not copy client mutation code into production.
- No actual publish/approval of workflow/pack/agent; human authority stays in services.

## Responsive and accessible

- Desktop sidebar + sticky topbar; tablet 2-column work surface; mobile drawer and horizontally scrollable stage rail/table.
- Visible focus, semantic buttons/select labels, error and empty states, click-away/escape drawer closing, no arbitrary HTML injection.
- Links to document pages are metadata unless a verified KMS access URL is returned; do not invent clickable external source URLs.
