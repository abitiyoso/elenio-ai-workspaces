# ELENIO AI OS — FINAL CODEX MASTER PROMPT
## Full-product completion · Continue existing work · Lean execution

You are continuing **Elenio AI OS** in the **existing** repository `https://github.com/abitiyoso/elenio-ai-workspaces`. **Finish the agreed product, not just a demo, Invoice Approval MVP, UI refresh, or one PR.** This prompt supersedes previous *unsent* draft prompts; preserve the already-agreed PRD, security requirements and completed work.

### 1. Verify current state; do not restart
- Inspect `git status`, HEAD, remotes and open PR chain before editing. Latest reviewed work was draft PR #12 on `feat/elenio-existing-workbench-polish`; **verify, don't assume it is still latest**. `main` may lag. The owner repo is currently **public**; do not alter visibility.
- `origin` must be the owner's repo. Original `kawakibireku/ai-workspace` is **read-only upstream**. Never recreate/reclone the project, reset valid work, rewrite the five original services, force-push, write upstream, merge into `main`, or deploy production without explicit approval.
- Read the relevant repository `AGENTS.md`, `docs/elenio-handoff/STATUS.md`, `FEATURE_MATRIX_LIVE.md`, `PRODUCT_DELIVERY_ADDENDUM.md`, `HTML_ROUTE_MATRIX.md`, and **only pertinent** PRD/acceptance sections. Reuse native Control Plane, Flowcase, Agent Studio, KMS and Universal Case Workspace with their existing APIs, grants, audit and approval boundaries.

### 2. Product end states — all mandatory
**A. Internal Solution Factory:** An Elenio engineer can create a durable customer Solution from requirements → AI-reviewed blueprint → configurable workflow, agents, approved models, business data, knowledge (when needed), integrations, UI/API → real preview/execution → evaluation, economics and release approval → publish and operate. Every stage uses the **same saved/versioned Solution context**, not disconnected mock screens.

**B. Independent Customer Solution:** Publish an **approved, immutable, installable** business UI, API service, or both, with required runtimes/configuration; install on a clean, separate customer-like host using pinned OCI images and Docker Compose. Validate customer-owned secrets/data/identity, real HTTPS/URL configuration, working login and business operations, persistence after restart, backup/restore, upgrade and rollback. Normal execution must not depend on Elenio's development environment. External inference dependencies must be disclosed.

**C. Reusable platform:** Complete **Invoice Approval** as a real business test (PDF intake → authorized AI extraction → validation/correction → independent Finance approval/rejection → durable status, trace, audit and measurable usage). Then build a **materially different** second application through the same reusable capabilities, **without copying bespoke invoice code**.

**D. Full self-hosted Enterprise AI OS:** Install the engineering platform in a separate customer-like environment. Their authorized IT builder can create, independently publish and operate **two distinct departmental workspaces**, with verified tenant/business-unit isolation, RBAC, governance, service health and cost visibility. Implement server-enforced license/entitlement controls and an offline-compatible activation approach, without mandatory calls to Elenio's development control plane.

**E. Product readiness:** Complete applicable PRD V2 FR/UAT, model/skill/tool/connector/KMS governance, actual-vs-test-vs-estimated AI Economics, logs/traces/operations, security, accessibility, operator documentation and reproducible install/maintenance procedures. No fabricated model output, metrics, costs, deployment URLs or passing tests.

### 3. Approved product UX — refine, don't redesign
- UX reference: **`Elenio_AI_OS_V10_Updated_Guided.html`** (supply the file once if absent from checkout). Keep **Home V10 prompt-first** and the intuitive **Describe → Blueprint → Configure → Preview & Test → Publish** journey. Keep official Elenio logo **unchanged**, existing light design tokens, premium simplicity and responsive/keyboard accessibility.
- **Application Composer/Builder lives inside Solution → Build**, sharing saved workflow/data/role configuration with the **actual customer Preview**. Extend existing Composer, never duplicate it. Preserve advanced Agent/Workflow/Knowledge tools as contextual workbenches.
- Avoid menu proliferation. Runtime health is within Operations, provider routing within existing Model Hub, and publishing within Solution delivery. No separate Infra Monitor product, unnecessary dashboards or superficial placeholders.
- An HTML prototype and a candidate manifest are references **only**, not proof of working backend or deployment.

### 4. Execution and token discipline
1. **Diff first, once:** determine verified / partial / missing / blocked by actual source, tests and PR evidence; prioritize gaps preventing end-to-end completion. Do not re-audit every file on each turn.
2. **Reuse before build:** connect already-implemented components, keep existing API/security authorities and version pins; avoid duplicate framework/tooling or large refactors unrelated to acceptance.
3. **Implement vertically:** persist real Solution definitions; bridge model-backed execution, authorized business UI and data, governed release, customer installer, second use case, enterprise edition and final UAT. Use small reviewable commits/PRs and targeted tests; periodically run broader regression checks.
4. **Evidence or blocked:** test real permissions, failure/retry, isolation and persistence. Label fixture/contract tests clearly. If approved credentials, Docker/VM, signing keys, DNS/TLS or human signoff are missing, complete all safe independent work, give exact reproduction steps and mark the specific acceptance gate **BLOCKED**. Never bypass approval or claim complete.
5. **Keep moving toward the FULL end state**, not merely the first milestone, while tools and permissions permit. Do not request approval for routine code/tests; obtain explicit approval for production deployment, `main` merges, external charges or risky actions.
6. **Conserve tokens:** read focused files and diffs, avoid repeatedly pasting entire PRDs/logs, reuse existing tests, and update one concise status/checkpoint document for subsequent sessions. At each checkpoint report only: **implemented, test evidence, remaining blockers, next action**. No lengthy speculative plans or marketing prose.

### Definition of DONE
All end states **A–E** pass reproducible acceptance in isolated test environments, with working deliverables and evidence. If external conditions prevent a gate, report **NOT DONE / BLOCKED** accurately; do not substitute source code, HTML or README claims for successful execution. Do not merge `main` or deploy production without authorization.

**Start now by inspecting the current feature branch and PR chain, then implement the highest-impact missing dependency. Continue from existing code.**
