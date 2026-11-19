# V10 comparison and correction — 2026-11-19

Scope: the owner's reported font, page structure and content differences on
Home, AI Architect and Workflow Studio. Product Design audit guidance was used.
Actual application and supplied V10 HTML were opened in the in-app browser and
captured before changes. The source was served read-only from the handoff folder.
This is not a whole-product visual acceptance or a pixel-parity claim.

## 1. Home: hierarchy and capability illustration

Before: bundled Inter was present, but the main title was smaller, generic CSS
shapes replaced the V10 capability artwork, and the customer-outcome section was
absent. This weakened the intended starting-page hierarchy (medium severity).

Corrected: 38px desktop title; six original V10 SVG illustrations extracted as
local assets; shared interactive capability map with the unchanged official
logo; customer-outcome section linking to actual operations and Economics.
Model Hub is explicitly unavailable because its governed registry UI is not
connected. The one actual fixture project remains one project; the HTML's three
prototype projects were not copied as fictional records.

Evidence: [application before](evidence/ui-audit/01-app-home-before.jpg),
[V10 reference](evidence/ui-audit/02-reference-home.jpg),
[application after](evidence/ui-audit/08-app-home-after.jpg),
[390px view](evidence/ui-audit/11-app-home-mobile.jpg).

## 2. Architect: conversation and discovery workbench

Before: two generic form cards replaced the reference's solution conversation,
composer and connected-architecture panel (high severity). The blanket model
configuration error would also become inaccurate after provider verification.

Corrected: conversation/history and bottom composer on the left; solution
discovery, blueprint scope and an interactive architecture map on the right.
Approval, customer-hosting and reuse chips append real constraints. Capturing
a requirement records the owner's text for the current mounted page; it does
not fabricate an assistant response. The brief remains in shared in-memory
workspace state across routes; it is not durable across reloads. The generation
button explains the missing authoring integration. It is still disabled.

Evidence: [application before](evidence/ui-audit/04-app-architect-before.jpg),
[V10 reference](evidence/ui-audit/03-reference-architect.jpg),
[application after](evidence/ui-audit/07-app-architect-after.jpg),
[390px full page](evidence/ui-audit/12-app-architect-mobile.jpg).

## 3. Workflow: useful canvas area and readable entry

Before: permanent sidebar and repeated global/status/heading bars constrained
the canvas; initial centering at a fixed zoom clipped the process entry (high
severity). The reference uses a compact, broad engineering workbench.

Corrected: the local editor opens with drawer navigation, a compact header and
full-width canvas (over 1,400 × 630 CSS pixels at 1440 × 900). Focus mode removes
the editor heading/tabs. It opens at a readable native start/next context; Fit
View is an explicit whole-graph overview. Selecting/finding a node takes priority
over the delayed initial viewport adjustment. Native `next`, parallel join,
condition branches, config, versions and coordinates retain their authority.
No execution behavior derives from the visual layout. Mobile uses readable node
inspection rather than shrinking the whole graph.

The fixture has 11 native nodes and 12 connections, including parallel work;
the HTML's six simplified nodes were not substituted for it. Large graph
overviews necessarily use a smaller scale; zoom/search/inspector provide detail.
The connected server editor retains its existing contextual header and permission
controls; it shares the improved canvas viewport behavior, not the local shell's
entire drawer treatment.

Evidence: [application before](evidence/ui-audit/06-app-workflow-before.jpg),
[V10 reference](evidence/ui-audit/05-reference-workflow.jpg),
[readable entry after](evidence/ui-audit/09-app-workflow-after.jpg),
[condition inspector](evidence/ui-audit/10-app-branch-inspector-after.jpg),
[390px node list](evidence/ui-audit/13-app-workflow-mobile.jpg).

## Verification and remaining gaps

The saved browser captures above were opened and visually inspected. Primary
desktop viewport: 1440 × 900; mobile: 390 × 844. Full-page captures can be taller.
Regression browser tests also cover 1280, 1024, 768 and 360 widths, no horizontal
overflow, actual SVG decoding, loaded Inter, brief continuity/constraint capture,
drawer keyboard focus, readable initial graph, Fit View, drag/ports/undo and
serious/critical accessibility checks including Architect.

29 frontend browser cases, 66 unit/component cases and nine real local
API/worker/database cases pass. Static checks and build pass. Source kits and
logo assets remain untouched. Not all HTML pages or feature breadth are
implemented: general authoring, governed model/agent editing, full project
hierarchy, KMS/capability integration, actual Economics ledger and portable
production delivery remain open. Human visual acceptance is still pending.
