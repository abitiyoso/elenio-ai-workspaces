# Agent Studio / Go + Eino — Implementation Package v1.1 (LLM Setup)

**Goal:** reusable AI Agent Studio + Agent Runtime to plug into **Flowcase** (workflow/approvals) and **KMS** (governed evidence). **First domain pack:** six SIM B2B specialists.

## Contents

- `Agent_Studio_Eino_Prototype.html` — responsive interactive demo UI, now with **Models & Providers**, model deployment registry and agent-model binding. No backend, credentials or real model connection; all data illustrative.
- `AI_AGENT_STUDIO_TECHNICAL_PRD.md` — source-grounded + engineering-proposed requirements, architecture, security, API and UAT.
- `CODEX_MASTER_PROMPT.md` — coding prompt in Phases 0–4, with **stop gates** per phase.
- `samples/sim_six_agents.json` — six named SIM agent bootstrap manifests.
- `samples/design_agent_skill_v1.json` — illustrative governed Design Agent skill.
- `samples/sample_evaluation_suite.json` — positive, negative and injection/evidence tests.
- `contracts/flowcase_agent_invocation.schema.json` — Flowcase → Agent Runtime input contract (proposal).
- `contracts/agent_completion_event.schema.json` — Agent Runtime → Flowcase completion event (proposal).
- `contracts/kms_evidence_response.schema.json` — KMS evidence adapter response (proposal).
- `UI_DESIGN_NOTES.md` — design system, navigation and prototype limitations.
- `previews/` — browser screenshots.

## Start development

1. Extract ZIP into a **new repo** or a dedicated module within AI Workspace. Keep KMS/Flowcase independent services.
2. Open HTML in any browser. Agent creation/editing/run/evaluation/release are simulated locally for demonstration; no real Eino/LLM calls are made.
3. Attach PRD + HTML + `contracts/` + `samples/` to Codex and paste `CODEX_MASTER_PROMPT.md`.
4. **Phase 0 first**, validate actual KMS/Flowcase contracts and pin Eino module versions. Only then build implementation.

## Where to configure LLMs

1. Open **Models & Providers** in the prototype sidebar.
2. Register provider metadata (name, adapter type, allowed endpoint). **Do not enter an API key** in this local HTML prototype.
3. Register model deployment (exact model ID, tools/JSON capabilities, token and timeout limits), then toggle **demo approval** to test the UI binding.
4. Open **Agent Builder** → **Model policy** and select the registered model, save the draft, re-run evaluation and release gates.
5. In production, Platform Admin uses backend secret management, connection tests, model approval API and tenant policies. Actual keys stay in Vault/KMS, never localStorage.

See PRD §18 and `contracts/model_provider_example.json`.

## Key ownership boundaries

- **Flowcase:** workflow/case/task/approval/G0–G5 and immutable case artifacts.
- **Agent Studio:** agent skills, tools, evaluation, controlled release and Eino execution.
- **KMS:** Docling, Elasticsearch, ACL-aware knowledge retrieval, verified evidence source lineage.
- **Host:** login/SSO, trusted identity bootstrap. Agent Studio API verifies claims and enforces per-action permissions itself.

## Official Eino references (checked 30 Oct 2026)

- Eino ADK Quickstart: https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_quickstart/
- Runner and Extension: https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_extension/
- Eino official source: https://github.com/cloudwego/eino
- Eino Examples: https://github.com/cloudwego/eino-examples

Check exact APIs against the pinned release before coding. Some ADK features and signatures evolve.

## Important disclaimers

- This package **does not contain production Go/React implementation**. The HTML uses mock data and browser storage only.
- SIM PRD source working draft does **not** grant real organizational decision authority. SME ownership and delegation require approval.
- Eino `Runner` is an agent execution primitive and **does not replace Flowcase** business process orchestration.
- Docling and Elasticsearch stay behind KMS; **do not redeploy them** for Agent Studio.
