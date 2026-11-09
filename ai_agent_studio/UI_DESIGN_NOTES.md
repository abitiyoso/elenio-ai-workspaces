# UI Design Notes — Agent Studio

Palette: `#101C34` navy sidebar; `#2864EC` cobalt primary; `#F3F6FB` app canvas; `#FFFFFF` document surfaces; slate texts; success `#12866B`; amber `#C08022`; critical `#C44F64`.

- Visual direction: restrained Apple-inspired bento, sharp enterprise productivity, meaningful whitespace. **Do not** use full-page washed glass or low contrast.
- Typography: system Inter-style sans. Strong page headings, restrained eyebrow labels, no giant AI graphics.
- Layout: 252px fixed sidebar / 68px topbar; bento overview; registry tables, two-column builder and inspector; workflow-centric model/skill/tool overview.
- Navigation: Overview, Agent Registry, Skills Studio, Tool Library, Playground, Evaluations, Releases, Runs & Audit.
- Actions in standalone demo: navigation, registry search/filter, agent select/edit draft, skill editor, tools toggles (draft), sandbox mock run, test evaluation simulation, domain signoff and Ops publishing simulation with gates, event log, basic agent creation and toast/feedback.
- Prototype uses localStorage for demonstration; no real deployment, authentication, Eino, Flowcase or KMS integration.
- Responsive: sidebar becomes drawer; table horizontal scroll instead of crushing columns.
- Production migration: React+TS and real Go API; use status fields returned from backend, never client-enforced approval or policy.

## Model Settings (v1.1)
- New **Models & Providers** view: provider metadata and model registry with explicit demo approval.
- Agent Builder selects an approved model deployment; no provider credentials in frontend.
- This HTML does not test provider connectivity, store API keys or invoke Eino. Production uses server-side secrets, model capability checks and authorization.
