# Original PRD v1 — text extraction

Source: original editable DOCX in references/. Figures remain in the DOCX. This is baseline; later decisions in the development kit supersede conflicting target design details.

PRODUCT REQUIREMENTS DOCUMENT  /  EXPERIENCE ARCHITECTURE

ELENIO
AI WORKSPACES

Enterprise AI Solution Engineering & Orchestration Platform

Dari business requirement menjadi executable AI workflow, reusable capability, dan solusi self-hosted yang siap dioperasikan customer.

UX concept screenshot · HTML interactive prototype (illustrative, not a production system).

VERSION 1.0    •    16 NOVEMBER 2026    •    DRAFT FOR PRODUCT & ENGINEERING ALIGNMENT

## Document Control

| Field | Detail |
| --- | --- |
| Product | Elenio AI Workspaces |
| Document type | Product Requirements Document (PRD), UX Journey Blueprint, Solution Architecture & Delivery Specification |
| Version / Date | v1.0 / 16 November 2026 |
| Status | Proposed target product; readiness subject to architecture review and actual UAT |
| Audience | Product Owner, CEO/Business, Solution Architect, UI/UX, Frontend, Backend, DevOps, QA, Security, Implementation Team |
| Source basis | Repository kawakibireku/ai-workspace (public main branch); existing interactive HTML; Elenio visual identity assets; user-approved positioning and scope |
| Repository | https://github.com/kawakibireku/ai-workspace |
| Prototype | Elenio_AI_Workspaces_Interactive_Journey.html (standalone frontend concept) |
| Ownership | Elenio Digital Indonesia · Product & Engineering |

| Status legend (wajib digunakan sepanjang dokumen); EXISTING = ditemukan pada source/documentation repository; PARTIAL = fondasi/adapter ada tetapi journey end-to-end belum terbukti; TARGET = requirement baru; NEEDS VALIDATION = membutuhkan live integration test, security review, atau keputusan desain. |
| --- |


### How to use this document

Mulai dari product scope dan navigation architecture untuk memastikan apa yang dijual dan siapa penggunanya.

Gunakan journey diagrams dan screen specifications untuk UX/UI prototype, informasi yang ditampilkan, dan states yang harus didukung.

Gunakan requirement IDs serta acceptance criteria untuk engineering backlog dan QA traceability.

Gunakan integration boundary, deployment contract, NFR, dan release gates sebagai dasar production rollout.

Semua prioritas P0/P1/P2 adalah usulan planning; bukan klaim seluruh fitur sudah production-ready.

### Table of contents

| Chapter | Contents |
| --- | --- |
| 01 | 01. Executive Summary & Product North Star |
| 02 | 02. Business Problems, Personas & Value Proposition |
| 03 | 03. Existing Baseline, Scope & Product Boundaries |
| 04 | 04. Information Architecture & Object Model |
| 05 | 05. Master User Journey & Entry Points |
| 06 | 06. Detailed User Journeys A–J |
| 07 | 07. Screen-by-Screen UX Specifications |
| 08 | 08. Capability Catalogue & Functional Requirements |
| 09 | 09. Sample End-to-End Business Workflows |
| 10 | 10. Technical & Service Architecture |
| 11 | 11. Contracts, Events, Data & Reusability |
| 12 | 12. Self-Hosted Deployment & Handover |
| 13 | 13. Security, Risk & Governance |
| 14 | 14. Non-Functional Requirements & Analytics |
| 15 | 15. QA / UAT Scenarios & Acceptance |
| 16 | 16. Delivery Roadmap & Dependencies |
| 17 | 17. Decisions, Assumptions & Open Questions |
| 18 | 18. Glossary & References |

## 01  |  Executive Summary & Product North Star

| Product statement; Elenio AI Workspaces adalah enterprise AI engineering platform yang memungkinkan IT Team atau Elenio sebagai solution provider mengubah kebutuhan bisnis menjadi executable workflows melalui AI-assisted solutioning, reusable agents/skills/knowledge/integrations, testing, governance, dan portable customer runtime. |
| --- |


### 1.1 Product vision

Build once. Reuse capabilities. Deploy into the customer environment. Deliver measurable business operations - not only an AI chatbot or diagram.

### 1.2 Primary product outcome

| Outcome | Definition of done |
| --- | --- |
| Business requirement → executable solution | Permintaan bisnis menghasilkan blueprint yang disetujui, workflow tervalidasi, resource bindings dan operator-facing experience. |
| Reusable enterprise capability | Agent, skill, template, connector, dan knowledge collection terdaftar, versioned, permission-scoped dan dapat digunakan di proyek lain. |
| Customer-ready delivery | Release artifact memiliki dependency manifest, deployment plan, health probes, runbook dan dapat dijalankan di environment customer. |
| Operational control | Run, case, approval, error, evidence, latency, token/cost, dan audit dapat ditelusuri menurut tenant/project/version. |
| Responsible automation | Aksi berisiko menggunakan permission, review gates, separation of duties dan sumber knowledge yang dapat diaudit. |

### 1.3 Product positioning

Category: Enterprise AI Solution Engineering & Workflow Orchestration Platform. Buyer: CIO/CTO, Head of IT, Digital Transformation Lead, dan pemilik fungsi bisnis dengan sponsor IT. Builder: IT Team, Solution Architect, AI Engineer dan Elenio delivery team. Operator: business users yang menjalankan case, melakukan approval, dan menerima hasil.

EXPLICIT DIFFERENTIATION

AI-assisted architecture from natural language, but with human-readable blueprint and versioned editable workflow.

Hybrid orchestration: deterministic business rules + AI agents + human approval + enterprise integrations.

Reusable capability registry: satu source of truth lintas proyek dengan tenant isolation dan ownership jelas.

Portable delivery: runtime tetap dapat dieksekusi di infrastruktur customer sesuai dependency yang disepakati.

Operator-facing business application: customer tidak harus membuka engineering canvas untuk memakai sistem.

### 1.4 Delivery models

| Mode | Who builds | Who runs | License / packaging implication |
| --- | --- | --- | --- |
| Self-Build Enterprise | Customer IT Team | Customer | Full private platform deployment / tenant license |
| Managed by Elenio | Elenio solution team | Customer or Elenio | Solution runtime package + implementation & maintenance scope |
| Co-Build & Handover | Customer + Elenio | Customer | Joint engineering, training, audit & transition assets |

### 1.5 In / out of product promise

| Included in target | Not included / explicit limitation |
| --- | --- |
| Build, test, orchestrate, deploy AI business workflow | No arbitrary one-click import compatibility with every automation platform |
| MCP/API/DB connections within approved permissions | No unrestricted execution of unknown external MCP tools |
| KMS evidence and versioned agent skills | No silent cross-tenant or public sharing of customer content |
| Self-host runtime / full private stack as separate tiers | No guarantee that cloud LLM dependencies are offline without local model configuration |
| Generated forms, inbox, case views, result dashboards | Not a complete replacement for SAP, HRIS, or existing systems of record |

## 02  |  Business Problems, Personas & Value Proposition

### 2.1 Problems to solve

| Problem | Current friction | Proposed product response |
| --- | --- | --- |
| Requirements are unstructured | Business users describe pain but solution team must manually extract processes, inputs and dependencies. | AI Architect creates structured brief, discovery questions, architecture and scope. |
| One-off agent implementations | Prompt/tool/skill logic copied between projects, low reuse and hard governance. | Agent Studio and capability version registry. |
| Workflow-engineer gap | Agent output not connected to approvals, ERP actions, retries and case ownership. | Flowcase orchestration + human tasks + integration contracts. |
| Enterprise knowledge fragmented | Policy/SOP documents lack verified source lineage and access controls. | Governed knowledge collections with ACL-aware retrieval. |
| Customer delivery not repeatable | Each rollout needs bespoke deploy scripts and undocumented dependencies. | Versioned runtime bundle, preflight, target config and runbook. |
| Low operational confidence | Success judged by demo, not traceable end-to-end production runs. | Simulation/evaluation, release gates, monitoring and operational KPI. |

### 2.2 Personas / jobs-to-be-done

| Persona | Primary JTBD | Key modules |
| --- | --- | --- |
| IT Manager / AI Lead | Govern portfolio, cost, ownership, release readiness and outcomes. | Projects, Operations, Control Center |
| Solution Architect | Convert process requirements into a valid system architecture. | AI Architect, Workflow, Integration, Deploy |
| AI Engineer | Build evaluated agents, skills and model policy. | Agent Studio, Knowledge, Testing |
| Integration Engineer | Connect internal APIs/MCP and manage schema/credentials. | Integration Hub, Workflow Studio |
| Business SME / Approver | Review evidence and approve decisions within authority. | Case Workspace, Approval Inbox |
| Operations / Support | Monitor failures, restart safely and track SLA. | Operations, Runs, Audit |
| Platform Admin / Security | Manage tenant, identity, secrets, models, resources and compliance. | Control Center, Model/Policy Hub |
| Customer Business User | Perform tasks, submit forms, see cases and outcomes. | Generated Business App / Case Workspace |

### 2.3 Role boundaries

Builder roles can design workflows and agents but do not automatically obtain business approval rights. Reviewers can approve specific artifacts within their delegated policies but should not directly alter the workflow that they are reviewing. Platform admins configure permissions and infrastructures; they should not implicitly bypass case-level SoD.

### 2.4 Product KPIs & measurement definitions

| KPI | Formula / interpretation | Baseline / target |
| --- | --- | --- |
| Time-to-first-valid-workflow | Start of requirements → first simulation passing required tests. | Baseline TBD / improvement target to set after pilot |
| Time-to-customer-deploy | Blueprint accepted → verified customer runtime release. | Baseline TBD |
| Capability reuse rate | Deployments referencing previously published capability versions / total deployments. | Baseline TBD |
| Successful automated case rate | Cases completed without unhandled exception / started cases. | Baseline TBD |
| Human decision traceability | Sensitive decisions with reviewer, evidence and audit / all sensitive decisions. | Target 100% for defined high-risk actions |
| Regression-free release rate | Releases passing full required regression suite / attempted releases. | Target 100% gate compliance |
| Runtime unit economics | Cost by run, agent, model, tool and domain. | Tracked and budgeted; limits tenant-defined |

## 03  |  Existing Baseline, Scope & Product Boundaries

### 3.1 Repository baseline (source/document evidence, not live UAT)

| Existing module | Observed foundation | Current caution |
| --- | --- | --- |
| control_plane | Go BFF, OIDC/Keycloak, RBAC, membership, mini-app registry, proxy, audit. | Deployment docs show development/control-plane integration; production SSO/SLA need validation. |
| flowcase_platform | Go workflow/case runtime, designer, tasks, approvals, SoD, versioning, metrics. | Some end-to-end connectors/agent calls described as dev stubs pending real integration. |
| ai_agent_studio | Go/Eino agent runtime, model registry, skills, evaluations, release approvals, audit. | Code has adapters and tests, but live provider and all release-authority workflows were not uniformly verified. |
| kms_go_package | Governed document ingestion, Docling, Elasticsearch, versioning, ACL, retrieval. | Semantic search dependent on embedding provider; external service configuration required. |
| universal_case_workspace | General case UX, SIM intake, domain packs, artifacts, tasks, contextual agent calls. | SIM use case is more mature than other proposed packs; confirm deployed contract exchange. |

| Architecture rule; Extend and integrate these five services. Do not rewrite existing Go/Eino, Flowcase, or KMS foundations without an explicit capability/security rationale. Treat repository documents as evidence of code intent and tests, not proof of full customer production readiness. |
| --- |


### 3.2 Target gaps and recommended ownership

| Gap / target feature | Primary service owner | Implementation approach |
| --- | --- | --- |
| General chat-to-blueprint | AI Architect / new orchestration BFF, agent runtime | Create generic solution metadata; reuse existing agent infrastructure |
| Global reusable capability registry | Control Plane + owned domain registries | Indexes metadata; authoritative versions remain with owning service |
| MCP/API integration hub | Integration service / Flowcase ports | Approved connector registry, schema, policy and secret refs |
| Unified project lifecycle UX | Control Plane shell + project UX | Single context across five mini-apps and common breadcrumb |
| Template imports & dependency resolver | Template registry + validators | Normalized internal manifest plus source-specific adapters |
| Customer deployment bundle | Deployment service + CI/CD | Signed immutable artifact, dependency lock, environment mapping |
| Generated business-facing app | Universal Case Workspace / domain pack | Safe UI schema and approved components, not arbitrary generated code |

### 3.3 Priorities and explicit non-goals

| Priority | Scope |
| --- | --- |
| P0 — Pilot end-to-end | Unified project UX; AI Architect; workflow canvas; agent/skill binding; governed KMS; API/MCP basics; simulation; self-hosted Docker package; run observability; RBAC and audit. |
| P1 — Enterprise V1 | Advanced template import; shared connector packs; customer private platform deployment; model routing; complete business app generator; release/canary; SSO expansion; regression suite automation. |
| P2 — Scale | Enterprise marketplace; enterprise service catalogue; multi-cluster deployment; air-gapped mode; cross-project orchestration; chargeback; advanced SLA/compliance analytics. |
| Explicit non-goals (MVP) | General-purpose BI/ML workbench like full Dataiku; universal low-code ERP replacement; unreviewed auto-payments; unrestricted agent internet tools; unlimited imports. |

## 04  |  Information Architecture & Object Model

### 4.1 Global navigation

| Category | Menu | What user sees |
| --- | --- | --- |
| Home | Dashboard / AI Architect | Start with business intent, recent projects, status and high-priority actions. |
| Build | Projects / Workflow Studio / Agent Studio | Organize solution assets, create/edit technical runtime definitions. |
| Capabilities | Template Repository / Knowledge Hub / Integration Hub | Discover/share reusable workflow, agent, skill, KMS and connectors. |
| Deliver & Operate | Testing / Deployments / Operations | Validate, release, install and observe solutions. |
| Admin | Platform Settings | Tenants, user/role, models, policies, audit, system health. |

### 4.2 Context hierarchy

| Data model hierarchy; Organization (tenant) → Workspace (team/security boundary) → Project/Solution → Workflow Definition/Version → Run/Case. Reusable assets (Agent, Skill, Connector, Knowledge Collection, Template) are linked by pinned identifiers and versions, not duplicated by default. |
| --- |


### 4.3 Project workspace tabs

| Tab | Primary content | Critical action |
| --- | --- | --- |
| Overview | Blueprint, requirements, owners, dependencies, readiness and last release | Approve / reopen blueprint |
| Workflow | Canvas, node inspector, schemas, validation, flow version | Save draft / validate |
| Agents & Skills | Bound agents/skills, versions, evaluations and ownership | Open agent editor / bind |
| Knowledge & Integrations | KMS collection/tool refs, access and readiness | Test resource binding |
| Test | Simulation datasets, traces, errors, evaluation results | Run scenario / promote gate |
| Deploy | Targets, manifests, rollouts, version history | Build package / deploy |
| Runs & Cases | Executions, approvals, exceptions, audit and artifacts | Review / retry authorized action |

### 4.4 Universal statuses

Project status: idea → requirement_discovery → blueprint_review → building → validation → release_ready → deployed → operating → archived. Workflow definition: draft → validation_failed/validated → review_pending → published → superseded/retired. Run: queued → running → awaiting_human / needs_review → succeeded / failed / cancelled / timed_out. Deployment: package_ready → awaiting_customer_config → deploying → healthy / degraded / failed → rolled_back / retired.

Figure 1. Proposed version and release states; status authorities are enforced server-side.

## 05  |  Master User Journey & Entry Points

Figure 2. One solution lifecycle across all entry points.

### 5.1 Six entry points

| Entry | User intention | First screen / output |
| --- | --- | --- |
| Build with AI | “Saya ingin membuat workflow pembayaran vendor.” | AI Architect → requirement brief + blueprint |
| Build manually | “Saya sudah punya process map dan logic.” | Workflow Studio → new draft / canvas |
| Use / import template | “Ada recruitment workflow yang bisa saya gunakan?” | Template Repository → detail / dependency report |
| Manage capabilities | “Saya ingin membuat skill atau menghubungkan API.” | Agent/Knowledge/Integration Hub → versioned reusable asset |
| Deploy solution | “Workflow ini harus berjalan di server customer.” | Deployments → target preflight / bundle |
| Operate solution | “Saya perlu cek approval, errors, atau cost.” | Operations → run/case detail |

### 5.2 UX principles

Multiple entry points, single project context: maintain active tenant/workspace/project across features.

AI-assisted but engineer-controlled: chat suggestions produce reviewable diffs; no silent destructive edits.

Reuse before rebuild: show matching templates, agents, skills and connectors before creating duplicates.

Progressive disclosure: business request first; advanced schema/tools/permissions surfaced on demand.

Builder UX and Operator UX are separate modes with role-specific navigation.

No fake completion: disconnected services must show explicit unmet dependencies, degraded health or blocked states.

No credentials in browser/manifest: use server-side secret references and customer-side injection.

Responsive but canvas-aware: mobile supports review/approvals; dense visual editing is desktop-first with accessible fallbacks.

### 5.3 Saved project state versus transient chat

Conversation turns, parsed requirements, blueprint revisions, canvas edits, and resource decisions must be durably persisted. Chat is a control surface, not the system of record. Every applied change produces a meaningful revision with actor, timestamp, evidence and structural diff.

## 06  |  Detailed User Journeys A–J

### Journey A — AI Architect / Chat-to-Solution

Figure 3. Requirement-to-blueprint loop, with explicit approval before build.

| Stage | User action | System responsibility / output |
| --- | --- | --- |
| Entry | Click New Solution / type business need / upload SOP. | Start persistent intake session with tenant, project and source provenance. |
| Discovery | Answer targeted questions about trigger, SLA, data source, decisions. | Extract entities, constraints, missing details; distinguish stated vs assumed. |
| Recommendation | Review proposed process and resource counts. | Show deterministic steps vs agents; agent rationale, skills, KMS, integration and risk. |
| Blueprint | Edit steps via chat or GUI; compare options. | Generate editable graph, dependency manifest, acceptance tests and limitations. |
| Approval | Confirm “Use this blueprint”. | Record blueprint decision and pin revision; reject silent major changes. |
| Build | Select “Generate Workflow”. | Compile draft workflow and prepare resource bindings; flag unmet dependencies. |

Alternative paths. No detail → targeted follow-up; unsupported integration → missing connector task; unsafe action → requires human review; conflicting SOP → request source owner verification; insufficient permissions → request access rather than silently proceed.

Acceptance. A user can return to an intake session without losing sources; blueprint always lists agent count rationale and open assumptions; workflow generation is blocked until a blueprint review is recorded; generated nodes are editable.

### Journey B — Manual Workflow Studio

Figure 4. Engineer-controlled workflow creation and publish gate.

Start an empty workflow or duplicate an authorized workflow version; choose project and trigger type.

Compose typed nodes: trigger, condition, transformation, agent, approved tool, human task, wait, loop, subworkflow and output.

Define typed input/output contracts, mappings, timeouts, idempotency and node ownership.

Bind pinned agent versions, KMS collections, connector versions and approved secret references.

Use graph validator to detect missing edges, incompatible schemas, unsupported cycles, unbound tools and policy failures.

Run dry-run/sandbox with fixtures and inspect every transition and pending human task.

Request review; publishing creates immutable version, approval trace and release notes.

Key UX rule. Left sidebar = node library; center = graph with zoom/pan; right = configuration inspector; bottom = console/trace. Chat copilot can propose a graph patch but requires diff confirmation. Provide keyboard-readable node list for accessibility.

### Journey C — Template Browse, Clone & Import

Figure 5. Template reuse path and dependency resolution.

Search gallery by domain, outcome, number of agents, complexity, required systems and certified status.

Open a template details view: process preview, expected inputs/outputs, dependencies, owner, version, tests and usage note.

For import, upload a supported artifact; validate MIME, provenance, schema, size, dangerous expressions and external references.

System emits compatibility report: mapped nodes, unmapped nodes, prohibited actions, required connectors and known limitations.

Clone into a new project as an independent draft. Never import stored third-party or customer credentials.

Parameterize tenant-specific settings, bind KMS and connectors, simulate, then publish or save internally.

Error states. Unsupported source → read-only compatibility report; missing connector → blocked ready-to-run state; version incompatibility → migration/adaptation suggestion; no permission → request reuse grant; untrusted script → reject execution.

### Journey D — Agent Builder & Skill Lifecycle

Figure 6. Versioned agent development, evaluation and independent release.

Create agent from goal, clone existing agent or import skill manifest; define owner and intended business outcome.

Set model/provider policy (capabilities, token limits, tool calls, timeout, allowed region), system instruction, schemas and guardrails.

Assign versioned skills and least-privilege tool grants; bind KMS collections within access scope.

Run sandbox fixtures; inspect structured output, citations, reasoning traces allowed by policy, tool attempts and cost.

Execute evaluations: happy path, insufficient evidence, schema mismatch, prompt injection, forbidden actions, expired sources.

Request independent SME/AI Ops review. Critical failures block release; publish pinned agent/skill versions; existing runs remain pinned.

Skill model. Skill = reusable bounded capability with name, intent, input/output schema, method/tool eligibility, side-effect category, eval suite, publisher, version and compatibility policy. Avoid storing skills as ungoverned prompt snippets.

### Journey E — Enterprise Knowledge Hub

Figure 7. Knowledge ingestion, trust lifecycle and agent binding.

Create tenant-scoped knowledge collection: domain, steward, source sensitivity, allowed teams and retention.

Upload or sync documents; process OCR/Docling, normalized text, chunking, indexing and optional embeddings.

Display ingestion state (queued/processing/ready/failed), source version and parsing exceptions.

Review and publish approved document version; revoke or expire superseded content.

Bind collections to agent/project with user/case ACL and delegated identity.

Display answer citation with exact source/version/anchor; block unauthorized, unverified, expired or revoked evidence.

Error states. Embedding provider unavailable → keyword-only remains possible if configured; failed OCR → recoverable ingestion job; revoked document → immediately excluded from retrieval; index drift → admin action/health warning.

### Journey F — Integration Hub / API & MCP

Figure 8. Reusable connector creation with safe identity and policy boundaries.

Select REST API, MCP server, webhook, database, or connector pack, and define owner and tenant scope.

Define base URL, authentication class, allowed operations, egress policy, request/response schema and error mapping.

Provision secret reference server-side; prohibit raw secret display and copying in template exports.

Run connectivity probe and capability discovery with bounded timeouts; do not silently invoke side-effect tools.

Assign tool risk classification (read, write, financial, privileged) and required human approval/policy.

Publish connector version and bind it into workflows/agents by version and scoped permissions.

MCP-specific rule. Tool discovery is not permission. An approved MCP server has an explicit tool allowlist; each write-capable tool gets action-level guardrails, audit, and optional approval before invocation.

### Journey G — Testing Studio, Debug & Evaluation

Select test target: single agent, single node, full workflow, release regression or customer smoke test.

Choose sample dataset, replay case (redacted), or mock API/MCP response; separate tests from live production credentials.

Execution viewer shows state timeline, payload schema validation, agent version, tool request, source references, retries and approvals.

Reviewer compares expected outputs, policy violations, evaluation scores, token budget and latency; annotate defect root cause.

Create test report and gate decision. Failed critical test blocks release, but saves reproducible trace and test inputs within retention policy.

UX. Testing mode is not a generic log dump: show each node as “not started, running, waiting for human, succeeded, failed, skipped”, with expandable evidence and safe retry choices.

### Journey H — Self-Hosted Deployment & Delivery

Figure 9. Customer runtime packaging and controlled handover.

Choose deployment mode: customer runtime only or full private AI Workspaces platform.

Run preflight: versions, dependency graph, environment config, model availability, vector/index needs, secrets, ingress, DNS/TLS and storage.

Approve release; build reproducible images/manifests plus checksums, SBOM, rollback plan and operator runbook.

Transfer package through approved artifact delivery channel; no user PII, production KMS or credentials in bundle.

Customer injects secrets and endpoint config locally; deploy to VM/Docker Compose or Kubernetes with least privilege.

Perform health checks, live connector verification, safe smoke tests, rollback readiness, backup drill and owner sign-off.

Expose Operations UI and handover documents; track installed version and approved support access.

Blocking conditions. Missing model/provider, missing KMS, unapproved external endpoint, weak TLS, absent secret mapping, failed test gate, or no named support owner must block production-ready status.

### Journey I — Business User / Operator

Open a domain-facing case application or form, not Workflow Studio.

Submit business input (invoice, CV, document, procurement request) with metadata and visible required fields.

Track case status: intake, AI processing, pending review, integration action, completed, exception.

Reviewer receives assigned approval with evidence, relevant policy, version, risk and clear approve/reject/request-change actions.

If exception occurs, operator sees retry or escalation action only when authorized; no generic “rerun all” that duplicates side effects.

Case closes with output artifact, execution trace, audit log, and optional structured feedback to solution owner.

Access separation. Operator should never see model keys, agent system prompts, connector credentials, or engineering-only configuration. Builder should not automatically have authority to approve sensitive business tasks.

### Journey J — Platform Admin / Governance

Create organization/workspace, establish identity provider and role templates.

Configure model registry, allowed providers/regions, resource quotas, secrets and egress policy.

Assign fine-grained capability grants to teams, solution owners, reviewers and operators.

Publish approved integration/agent/template policies; manage certifications and version compatibility.

Audit access grants, release approvals, security events and tenant/resource consumption.

Suspension/revocation propagates to future actions; exceptions follow documented break-glass procedures.

## 07  |  Screen-by-Screen UX Specifications

Fokus UX: futuristic AI engineering cockpit dengan visual identity Elenio asli, white/navy/cobalt gradient, subtle luminous accents, dan interaksi yang mendukung engineering. Hindari visual backend admin generik: berikan solution graph, context-aware layers, stepper, dependency readiness, dan progressive inspector. Logo tidak dimodifikasi.

Figure 10. Existing standalone prototype: AI Architect concept. Illustrative frontend, not confirmation of live backend connection.

### 7.0 Screen catalogue — UX states and actions

#### S01 · Home / Launchpad

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | IT Manager, Solution Architect |
| Tujuan | Pick a starting action and resume work. |
| Komponen UI | Prompt entry; six entry points; recent projects; pinned templates; tenant switch; health summary. |
| Primary CTA | Create with AI / Create blank / Use template |
| State / Error | Empty state: no projects; no permission: hide create actions; service unavailable: degraded banner. |

#### S02 · AI Architect Chat

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Solution Architect, IT |
| Tujuan | Capture complete requirement and propose solution. |
| Komponen UI | Conversational thread, upload sources, brief progress, assumptions, questions, solution sidebar. |
| Primary CTA | Generate Blueprint |
| State / Error | No scope → guided discovery; invalid file → upload error; stale blueprint → rebuild warning. |

#### S03 · Blueprint Review

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | IT Manager, Architect, SME |
| Tujuan | Understand plan before build. |
| Komponen UI | Process map; agent count rationale; reused assets; integrations; KMS; risk; human gates; estimated dependencies. |
| Primary CTA | Approve & Build Workflow |
| State / Error | Conflicting requirements → ask to resolve; permission blocked → access request. |

#### S04 · Project Overview

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Project owner, Team |
| Tujuan | Track completeness and readiness. |
| Komponen UI | Owner, requirement revision, pipeline stepper, resource readiness, release history, recent runs. |
| Primary CTA | Continue Build / Open Last Run |
| State / Error | No project assets → starter tasks; broken dependency → readiness banner. |

#### S05 · Workflow Canvas

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Workflow engineer |
| Tujuan | Build editable executable graph. |
| Komponen UI | Node library, graph, zoom, mini-map, node inspector, AI copilot, validation drawer. |
| Primary CTA | Validate / Test / Save Draft |
| State / Error | Schema mismatch, cycle, unbound tool, orphan node, bad permission → inline errors. |

#### S06 · Node Inspector

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Workflow engineer |
| Tujuan | Set one node safely. |
| Komponen UI | Node type, schema mapper, mappings, retries, timeout, conditional branches, resource binding, permissions. |
| Primary CTA | Apply Change |
| State / Error | High-risk config → diff approval; invalid schema → block save. |

#### S07 · Agent Studio

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | AI Engineer |
| Tujuan | Create/reuse evaluated agent. |
| Komponen UI | Agent list, model policy, prompt/schema, skill/tool tabs, sandbox, evaluation, releases. |
| Primary CTA | Run Sandbox / Request Release |
| State / Error | No approved model → blocked; critical eval failed → cannot publish. |

#### S08 · Skills Registry

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | AI Engineer |
| Tujuan | Manage reusable agent skills. |
| Komponen UI | Search, lifecycle, compatibility, input/output contracts, versions, evaluator. |
| Primary CTA | Create Skill / Bind to Agent |
| State / Error | Deprecated skills marked; incompatible version → explicit conflict. |

#### S09 · Knowledge Hub

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Knowledge steward, AI Engineer |
| Tujuan | Publish governed company evidence. |
| Komponen UI | Collections, upload/sync, ingestion jobs, source review, ACL, retrieval playground. |
| Primary CTA | Upload Sources / Publish Version |
| State / Error | OCR failed, revoked source, missing embedding → truthful status. |

#### S10 · Integration Hub

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Integration Engineer |
| Tujuan | Create governed reusable integrations. |
| Komponen UI | Connector grid, credential refs, API/MCP discovery, scope, contract test, status. |
| Primary CTA | Add Integration / Test Connection |
| State / Error | Unapproved egress/tool action → deny; test failure → retry with diagnostic. |

#### S11 · Template Repository

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | All builders |
| Tujuan | Discover/clone/import reusable solution. |
| Komponen UI | Filter, preview graph, dependency map, versions, owner, certification, usage. |
| Primary CTA | Use Template / Import |
| State / Error | Unsupported formats → report; clone permission missing → request access. |

#### S12 · Testing Studio

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Engineer, QA |
| Tujuan | Prove workflow safety and correctness. |
| Komponen UI | Dataset picker, execution timeline, expected-vs-actual, traces, policy gates, defects. |
| Primary CTA | Run Test / Create Report |
| State / Error | Partial run / timeout → labelled; no success claim for incomplete steps. |

#### S13 · Deployment Manager

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | DevOps, Architect |
| Tujuan | Release to approved customer target. |
| Komponen UI | Targets, topology, preflight checklist, manifest, secrets map, health, rollback. |
| Primary CTA | Build Package / Deploy |
| State / Error | Missing dependency or target readiness → blocked production release. |

#### S14 · Operations Center

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | IT Ops, Support |
| Tujuan | Monitor deployed workflows. |
| Komponen UI | Run matrix, SLA, errors, retries, queue lag, cost, alerts, audit, version. |
| Primary CTA | Inspect Run / Authorized Retry |
| State / Error | Failed downstream → clear failed service; do not infer “completed” from UI. |

#### S15 · Case Workspace / Business App

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Business SME, End User |
| Tujuan | Operate solution without engineering views. |
| Komponen UI | Domain form, case list, approval inbox, artifacts, comments, status timeline. |
| Primary CTA | Submit / Approve / Request Changes |
| State / Error | Missing authority → 403; pending human action remains explicit. |

#### S16 · Control Center

| Dimensi | Spesifikasi |
| --- | --- |
| Pengguna utama | Platform Admin, Security |
| Tujuan | Govern identity and capabilities. |
| Komponen UI | Tenants, teams, RBAC, OIDC, models, budgets, audit, secrets, trust boundaries. |
| Primary CTA | Review Grant / Publish Policy |
| State / Error | Self-grant and cross-tenant access rejected; all sensitive changes audited. |

### 7.1 Responsive behavior

| Viewport | Experience rule | Prohibited anti-pattern |
| --- | --- | --- |
| Desktop ≥ 1280px | Full 3-pane builder, graph, inspector and trace dock. | Pinned overlays covering node graph or chat input. |
| Laptop 960–1279px | Collapsible navigation, resizeable chat/inspector, graph pan. | Graph compressed until unreadable. |
| Tablet 640–959px | Single dominant work area, inspector as drawer, touch-target controls. | Automatic crop of key stepper or CTA. |
| Mobile < 640px | Review status, approve case, chat, search templates; graph list/preview alternative. | Pretend complex node editor is fully usable in 1 narrow column. |

### 7.2 Visual design system

| Token | Direction |
| --- | --- |
| Brand | Original Elenio logo assets only. Never redraw or generate an alternative mark. |
| Primary palette | Navy + Cobalt; warm white / subtle blue surface; teal/cyan highlights used sparingly. |
| Look and feel | Futuristic professional cockpit; luminous borders, controlled depth and purposeful gradients. |
| Typography | Readable modern sans. Dense technical data uses tabular numerals and clear hierarchy. |
| UI motion | Light animated flow/pulse for running nodes, progress and state transitions; respect reduced-motion settings. |
| Semantics | Color never sole status indicator; use icon, title, text status, trace and actionable feedback. |
| Accessibility | Keyboard support, focus visible, contrast target WCAG AA, descriptive node list alternative. |

## 08  |  Capability Catalogue & Functional Requirements

Requirements below define target product behavior. P0 = required for pilot; P1 = enterprise v1; P2 = advanced. Each item requires a QA test and source owner; “exists in code” does not waive E2E acceptance.

### Requirements · FR-PRJ

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-PRJ-001 | P0 | Create organization-scoped workspace and project with durable owner, description, domain and status. |
| FR-PRJ-002 | P0 | Persist project-linked chat, requirements, blueprint, graph, resource refs, test suites and release refs. |
| FR-PRJ-003 | P0 | Display role-appropriate primary CTA, readiness progress and blocking dependencies. |
| FR-PRJ-004 | P0 | Switch tenant/workspace without cross-tenant leakage or silent inherited access. |
| FR-PRJ-005 | P1 | Duplicate project as draft with remapped resources and no secrets/customer data copied. |
| FR-PRJ-006 | P1 | Track activity, comments, assignments and review decisions at project level. |

### Requirements · FR-ARC

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-ARC-001 | P0 | Ingest a business prompt and supported requirement attachments into an editable requirement brief. |
| FR-ARC-002 | P0 | Ask targeted follow-up questions only for material missing or contradictory inputs. |
| FR-ARC-003 | P0 | Generate typed process blueprint with triggers, tasks, human gates, integrations and outcomes. |
| FR-ARC-004 | P0 | Explain recommendation for agent count and separate deterministic nodes from AI reasoning tasks. |
| FR-ARC-005 | P0 | Discover matching approved templates/agents/skills/connectors before proposing net-new assets. |
| FR-ARC-006 | P0 | Display assumptions, evidence, unresolved dependencies and security risks. |
| FR-ARC-007 | P0 | Require explicit blueprint approval before committing generated graph into project. |
| FR-ARC-008 | P1 | Allow conversational patch with graph diff/undo and change request commentary. |
| FR-ARC-009 | P1 | Estimate external API/model and human approval touchpoints; clearly mark estimate uncertainty. |
| FR-ARC-010 | P1 | Generate HLD, solution scope, deployment dependencies and acceptance test draft. |

### Requirements · FR-WFL

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-WFL-001 | P0 | Provide visual graph builder with trigger, branch, transform, agent, tool, human, wait and output nodes. |
| FR-WFL-002 | P0 | Maintain typed input/output schema and validate bindings between connected nodes. |
| FR-WFL-003 | P0 | Support manual, webhook, scheduled and authorized event trigger configuration. |
| FR-WFL-004 | P0 | Support explicit retries, timeouts, idempotency keys and per-node failure policies. |
| FR-WFL-005 | P0 | Persist drafts and publish immutable numbered workflow versions. |
| FR-WFL-006 | P0 | Enforce server-side authorization and SoD for human approval nodes. |
| FR-WFL-007 | P0 | Support safe dry-run/sandbox execution separate from production writes. |
| FR-WFL-008 | P0 | Provide node version/reference pinning for agents, skills, connectors and KMS scope. |
| FR-WFL-009 | P1 | Provide subworkflow invocation with contract/version compatibility checks. |
| FR-WFL-010 | P1 | Support controlled parallel branches, joins, waits and resumable human handoffs. |
| FR-WFL-011 | P1 | Allow accessible list-based editor and graph diff/undo history. |
| FR-WFL-012 | P1 | Provide workflow export as normalized signed definition excluding secrets. |

### Requirements · FR-AGT

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-AGT-001 | P0 | Create/edit agent draft with role, objective, prompt policy and JSON output schema. |
| FR-AGT-002 | P0 | Select only approved model deployments based on tenant policy and capability match. |
| FR-AGT-003 | P0 | Attach versioned skills/tools under explicit least-privilege permissions. |
| FR-AGT-004 | P0 | Bind KMS collections with delegated end-user or service scope and visible citation policy. |
| FR-AGT-005 | P0 | Run sandbox agent with trace and labelled non-production fixtures. |
| FR-AGT-006 | P0 | Block release on schema, citation, prompt injection or self-approval critical failures. |
| FR-AGT-007 | P0 | Publish immutable agent version only after independent review; prevent self-approval. |
| FR-AGT-008 | P1 | Enable reusable agent skill registry with semver, owner, side-effect type and test suite. |
| FR-AGT-009 | P1 | Support tool cost/time quotas, cancellation, policy-based model fallback. |
| FR-AGT-010 | P1 | Track evaluation drift and prompt/schema compatibility across releases. |

### Requirements · FR-KMS

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-KMS-001 | P0 | Create collection with tenant/domain owner, classification, ACL and retention metadata. |
| FR-KMS-002 | P0 | Upload documents, process parsing/OCR, show ingestion job states and retries. |
| FR-KMS-003 | P0 | Manage immutable document versions, review, publish, revoke and expiration. |
| FR-KMS-004 | P0 | Support ACL-aware retrieval with source/version/anchor citations. |
| FR-KMS-005 | P0 | Prohibit agent retrieval across ungranted collections/cases/tenants. |
| FR-KMS-006 | P0 | Show reliable degradation if embeddings provider is unavailable. |
| FR-KMS-007 | P1 | Support source sync adapters with change detection and orphan cleanup. |
| FR-KMS-008 | P1 | Track indexed source health, freshness, drift, audit and reindex controls. |

### Requirements · FR-INT

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-INT-001 | P0 | Create tenant-scoped REST API connector with operation schemas and endpoint allowlist. |
| FR-INT-002 | P0 | Register MCP server and discover tools without automatically granting execution rights. |
| FR-INT-003 | P0 | Store credentials only in backend secret manager and reference them by opaque identifier. |
| FR-INT-004 | P0 | Provide bounded connection test, error diagnostics and permission validation. |
| FR-INT-005 | P0 | Define read/write/sensitive action type and approval policy per operation. |
| FR-INT-006 | P0 | Expose connection status, version, owner and last health probe. |
| FR-INT-007 | P1 | Implement reusable webhook and supported database connector packs. |
| FR-INT-008 | P1 | Map source to canonical schemas with typed transforms and validation fixtures. |
| FR-INT-009 | P1 | Implement connector SDK/spec and schema backward-compatibility checks. |
| FR-INT-010 | P2 | Publish enterprise packs for SAP, HRIS, Microsoft 365 and other approved ecosystems. |

### Requirements · FR-TPL

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-TPL-001 | P0 | Browse/search templates by domain, outcome, complexity, agent count, data and dependency. |
| FR-TPL-002 | P0 | Preview published template graph, owner, supported version and required resources. |
| FR-TPL-003 | P0 | Clone published template to tenant-scoped project as independent draft. |
| FR-TPL-004 | P0 | Resolve missing agent, model, connector, KMS and approval dependencies before test. |
| FR-TPL-005 | P0 | Strip secrets, PII and internal tenant identifiers from shared exports. |
| FR-TPL-006 | P1 | Import supported structured templates via adapters with compatibility report. |
| FR-TPL-007 | P1 | Publish organization-approved templates with independent review and semver. |
| FR-TPL-008 | P1 | Certify and deprecate reusable capability assets with compatibility notices. |

### Requirements · FR-TST

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-TST-001 | P0 | Run full workflow with test fixture and visible status of each node. |
| FR-TST-002 | P0 | Show input/output contract validation and agent/tool/KMS trace references. |
| FR-TST-003 | P0 | Mock side-effect operations in non-production test environment. |
| FR-TST-004 | P0 | Report failure, timeout, waiting approval and partial execution distinctly. |
| FR-TST-005 | P0 | Record reproducible failure data with redaction and retention policy. |
| FR-TST-006 | P0 | Gate release on critical tests and open high-risk defects. |
| FR-TST-007 | P1 | Run regression suites pinned to workflow and dependency versions. |
| FR-TST-008 | P1 | Compare expected/actual behavior, quality, latency, tool denials and budgets. |
| FR-TST-009 | P1 | Generate signed test report and reviewer decision for handover. |

### Requirements · FR-DEP

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-DEP-001 | P0 | Offer runtime-only customer deployment and full private platform as distinct targets. |
| FR-DEP-002 | P0 | Build reproducible runtime package with version lock and dependency manifest. |
| FR-DEP-003 | P0 | Never bundle live secrets, customer knowledge or production personal data. |
| FR-DEP-004 | P0 | Generate customer configuration/secret reference checklist and preflight status. |
| FR-DEP-005 | P0 | Support first target deployment via Docker Compose on customer VM. |
| FR-DEP-006 | P0 | Provide health/readiness/smoke tests and verifiable release status. |
| FR-DEP-007 | P0 | Provide release/rollback plan and operator runbook. |
| FR-DEP-008 | P1 | Support Helm/Kubernetes target, multi-env promotion and TLS ingress templates. |
| FR-DEP-009 | P1 | Support signed artifacts, checksums, SBOM and local license / support configuration. |
| FR-DEP-010 | P2 | Support air-gapped installation and private model/embedding stack guidance. |

### Requirements · FR-OPS

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-OPS-001 | P0 | List executions by tenant/project/workflow/version/status with scoped visibility. |
| FR-OPS-002 | P0 | Expose run timeline, node state, evidence, model/tool cost and errors. |
| FR-OPS-003 | P0 | Provide approval inbox with server-side authority check and decision audit. |
| FR-OPS-004 | P0 | Support idempotent retry or operator escalation without duplicating side effects. |
| FR-OPS-005 | P0 | Show dependency health and clear degraded states instead of false-green UI. |
| FR-OPS-006 | P1 | Show SLO/SLI for run success, queue age, time-to-approval, latency and throughput. |
| FR-OPS-007 | P1 | Aggregate token/provider/API cost by project, tenant and version. |
| FR-OPS-008 | P1 | Issue alerts for stalled/failed runs, exhausted budgets and disconnected services. |

### Requirements · FR-APP

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-APP-001 | P0 | Render safe case forms, artifact views, tasks, approvals and status from approved domain schema. |
| FR-APP-002 | P0 | Ensure business users need not access workflow/agent engineering navigation. |
| FR-APP-003 | P0 | Enforce case-level RBAC, approval delegation, immutable revision and audit. |
| FR-APP-004 | P1 | Generate input form and list/detail views from normalized workflow/domain-pack metadata. |
| FR-APP-005 | P1 | Support configurable branding, fields, artifact download and output dashboard. |
| FR-APP-006 | P1 | Expose workflow as authorized API/webhook endpoint with rate limit. |
| FR-APP-007 | P1 | Provide guided end-user feedback and escalation to solution owner. |

### Requirements · FR-GOV

| ID | Priority | Functional requirement / acceptance |
| --- | --- | --- |
| FR-GOV-001 | P0 | Enforce tenant/workspace/project isolation on every core service endpoint. |
| FR-GOV-002 | P0 | Provide OIDC/JWT based verified identity; reject spoofed tenant/user headers. |
| FR-GOV-003 | P0 | Separate builder, approver, publisher, runtime operator and platform admin authorities. |
| FR-GOV-004 | P0 | Record immutable/auditable security, release, approval and privileged tool actions. |
| FR-GOV-005 | P0 | Define secure secret storage, rotation and non-disclosure in logs/UI. |
| FR-GOV-006 | P0 | Prevent self-grant/self-approval for sensitive permissions. |
| FR-GOV-007 | P1 | Add policy-as-code/approval matrices and environment-specific egress rules. |
| FR-GOV-008 | P1 | Manage model/provider data residency, retention, quotas and usage budgets. |
| FR-GOV-009 | P1 | Integrate production alerting, backups, restore tests and incident procedures. |

## 09  |  Sample End-to-End Business Workflows

### 9.1 Vendor Payment — Purchase-to-Pay workflow

Figure 11. Proposed vendor-payment orchestrator, with finance authorization.

| Component | Target design |
| --- | --- |
| Trigger / inputs | Invoice via mailbox/portal/API; PO, goods-receipt and vendor master references from ERP. |
| Deterministic steps | Schema validation, invoice duplicate check, PO/GR matching, monetary tolerance, posting eligibility. |
| Agent(s) | Document extraction / discrepancy analyst only when reasoning materially improves handling. |
| Knowledge | Published Finance SOP, vendor onboarding policies, payment terms and exceptions. |
| Integration | Mail, ERP PO/GR/AP API, optional notification, optional bank interface only within authorized control. |
| Human gate | Finance approval, SoD between initiator/checker/approver, override policy by amount/type. |
| Output | Case status, approved posting instruction, ERP confirmation, exception record, evidence and audit trail. |
| Must not occur | Unreviewed payment execution, autonomous approval, copied ERP secrets, claims of reconciliation without source evidence. |

#### Step-level acceptance example

New invoice creates a unique case; duplicate invoice ID + vendor + amount is detected and routed for review.

OCR/agent extracted fields are shown with confidence/provenance and validated before ERP actions.

Exact match with policy threshold may proceed to approval; a mismatch triggers exception analysis and SME routing.

Only authorized Finance approver can approve; creator/engineer cannot bypass delegation.

ERP write is idempotent; retry cannot create two postings for same approved case.

Audit stores decision actor, policy version, financial entity, result, timestamp and source references.

### 9.2 Recruitment Intelligence — HR workflow

Figure 12. Human-reviewed recruitment support workflow.

| Component | Target design |
| --- | --- |
| Trigger / inputs | CV from email/form/ATS; job description, required skills, screening criteria, retention/consent metadata. |
| Deterministic steps | File parsing, required field validation, dedupe, interview slot matching, ATS status update. |
| Agent(s) | CV profile extraction and grounded skills-fit rationale; number may be 1–2 depending on needs. |
| Knowledge | Recruitment SOP, approved job descriptions, interview policies. |
| Integration | Mail/calendar/ATS/HRIS API with scoped access. |
| Human gate | HR owns advance/reject decision; AI-generated rationale is advisory. |
| Output | Candidate review queue, structured summary, interview request, ATS update, audit. |
| Risk boundary | No protected-trait inference, undisclosed auto rejection or unconsented broad retention. |

#### Step-level acceptance example

Each candidate record is accessible only to the authorized HR scope, with source and processing status.

AI score, if used, must include policy explanation, skill evidence and uncertainty; not a standalone employment decision.

HR reviewer can correct extracted fields, request follow-up or reject the AI recommendation with rationale.

Calendar integration proposes slots; no invitation sent until policy-authorized review/action.

ATS update uses scoped token and logs the exact record status change.

## 10  |  Technical & Service Architecture

Figure 13. Conceptual Control Plane / customer runtime split, based on existing service boundaries.

### 10.1 Service responsibilities

| Service / layer | Owns | Does not own |
| --- | --- | --- |
| Control Plane | SSO/BFF, tenant/workspace/access, mini-app routing, registry view, governance. | Business process approvals, KMS indexing, agent execution engine. |
| AI Architect (proposed) | Requirements intake, solution blueprint, recommendation, project graph patch proposals. | Unreviewed production mutations; raw credential handling. |
| Flowcase | Workflow definitions, run/case state, human tasks, approvals, SoD and orchestration. | LLM model provider registry, evidence indexing. |
| Agent Studio | Agent/skill/model definitions, runtime invocations, evals and governed releases. | ERP record authority; business approval bypass. |
| KMS | Knowledge collections, source versions, ingestion, ACL retrieval and citations. | Agent reasoning and general workflow sequencing. |
| Integration Hub (proposed) | Approved connectors, MCP registry, schema mapping, credential refs, probe. | Workflow business policy or user identity truth. |
| Universal Case Workspace | Domain-facing case/approval UX and structured artifact views. | Duplicated workflow engine or independent second login. |
| Customer runtime | Pinned workflow/agent execution, connector operations, logs, health, local storage. | Implicit reliance on Elenio development credentials. |

### 10.2 Logical data/control plane split

Build / control plane. Hosts authoring, registries, identity, audit and release orchestration. In fully private installations it resides within customer environment; in runtime-only model it may run under Elenio-managed engineering with sensitive data excluded as agreed.

Execution plane. Runs workflows and agents near customer systems, with its own durable queue, DB, approved model endpoint, secrets and monitoring. Must preserve pinned versions and support outage/backpressure handling.

Operational plane. Case Workspace and Operations surfaces connect to deployed runtime through authorized gateway and verified service identity. Browser never calls arbitrary connectors directly.

### 10.3 Service-to-service patterns

| Pattern | Required behavior |
| --- | --- |
| Trusted identity | OIDC login at host, aud-scoped signed token to downstream service, independent authorization at service. |
| Agent invocation | Workflow run creates idempotent agent run with pinned agent version and delegated subject scope. |
| Callbacks/events | Signed completion callback or authenticated event with idempotency, timestamp and correlation ID. |
| KMS delegation | Agent retrieval limited by service grant and delegated user/case scope; deny missing claims. |
| Connector action | Tool requests validated against connector/operation version; enforced egress/scope/approval. |
| Audit/outbox | State update + outbox in one transaction where required; retry with dedupe instead of dropping outcomes. |

## 11  |  Contracts, Events, Data & Reusability

### 11.1 Core domain objects

| Object | Essential attributes | Authority |
| --- | --- | --- |
| Tenant / Workspace | tenant_id, workspace_id, owner, membership, policy version | Control Plane |
| Project | project_id, domain, description, state, blueprint_ref, owner | Project Experience service |
| Requirement / Blueprint | revision, source refs, assumptions, process steps, risk, decision | AI Architect / project service |
| Workflow Definition | key, version, nodes, edges, schemas, human gates, policy refs | Flowcase |
| Agent / Skill Version | agent_key, skill/version, model policy, tools, eval gate, release | Agent Studio |
| Connector Version | operation id, action type, schema, scopes, endpoint policy, secret_ref | Integration Hub |
| Knowledge Collection | collection_id, document versions, steward, ACL, evidence trust | KMS |
| Template Manifest | template_key, source, version, asset refs, dependency lock, tests | Template Registry |
| Case / Run / Task | case_id, run_id, definition pins, state, actor, evidence, idempotency | Flowcase / Runtime |
| Deployment Release | artifact digest, target, config schema, environment, health, rollback | Deployment Manager |
| Audit Event | actor, action, resource, before/after ref, correlation, time, result | Owning service + audit pipeline |

### 11.2 Example solution manifest (illustrative, not current API)

{
  "apiVersion": "elenio.ai/v1alpha1",
  "kind": "SolutionBlueprint",
  "metadata": {
    "name": "vendor-payment",
    "tenantRef": "tenant-local-ref",
    "revision": "draft-04"
  },
  "spec": {
    "workflowRef": {
      "key": "finance.vendor-payment",
      "version": "1.0.0"
    },
    "agents": [
      {
        "key": "finance.exception-analyst",
        "version": "1.0.0"
      }
    ],
    "knowledge": [
      {
        "collectionRef": "finance-sop",
        "permission": "read"
      }
    ],
    "connectors": [
      {
        "key": "sap-erp",
        "operation": "po.lookup",
        "version": "v1"
      }
    ],
    "approvalPolicies": [
      "finance.payment-review"
    ],
    "deploymentTarget": "customer-runtime"
  },
  "secrets": "EXCLUDED — environment secret refs only"
}

### 11.3 Version pinning and compatibility

Workflow runtime pins workflow version, agent/skill versions, connector operations, template/domain pack version and knowledge retrieval policy as distinct identities.

Existing case/run must not silently adopt new agent prompts or approval policies while executing.

Publish checks input/output schema compatibility and forbids a major breaking change without new version & migration.

Cross-service resource references are identifiers, not direct database writes across service boundaries.

Template clone copies metadata/structure only and resolves tenant assets explicitly.

### 11.4 Event catalogue (target)

| Event | Produced by | Consumer / behavior |
| --- | --- | --- |
| solution.blueprint.approved | AI Architect | Project/Workflow creates editable draft |
| workflow.version.published | Flowcase | Deployment / Registry updates release candidate |
| agent.release.published | Agent Studio | Registry updates compatibility and pinned release |
| knowledge.version.published_or_revoked | KMS | Invalidate allowed retrieval and notify affected project owners |
| connector.health.degraded | Integration Hub | Operations alert, preflight blocks release |
| run.awaiting_approval | Flowcase | Case Workspace inbox + notification |
| run.completed_or_failed | Runtime / Flowcase | Operations, audit and analytics |
| deployment.health.changed | Deployment Manager | Operations / customer handover state |

### 11.5 System of record and reliability rules

Each service owns its authoritative records. Avoid multiple status tables pretending to be the source of truth. Use transactional outbox, correlation_id, idempotency_key, immutable version refs, signed callbacks, replay windows and bounded retries for cross-service integrity. At-least-once delivery is acceptable only when all downstream side effects are idempotent or review-gated.

## 12  |  Self-Hosted Deployment & Customer Handover

### 12.1 Two supported delivery models

| Dimension | Mode A · Build by Elenio, run by customer | Mode B · Full Private Platform |
| --- | --- | --- |
| Control plane | Elenio build environment, per contract / data policy | Installed inside customer tenant/data center |
| Workflow/agent runtime | Deployed to customer environment | Installed in customer environment |
| Business data / KMS | Customer-side unless explicitly governed otherwise | Customer-side |
| Developer UX access | Elenio engineering / controlled support | Customer IT with optional Elenio delegation |
| Upgrade model | Versioned release package + support agreement | Platform upgrade/rollback and runtime updates |
| Ideal customer | “Terima beres” automation deployment | Regulated / enterprise self-build teams |

### 12.2 Customer package content

| Artifact | Minimum content |
| --- | --- |
| Runtime image set | Versioned images, immutable digests, artifact signing/checksum, SBOM target. |
| Workflow/agent config | Published pinned manifests, schemas, release notes, no real secrets. |
| Deployment manifests | Docker Compose base; optional Helm for K8s; resource/health configuration. |
| Dependency lock | Model/embedding provider, storage, database, queue, API/MCP, network routes, TLS. |
| Configuration template | Environment variables, secret references, ports, hosts, allowed outbound domains. |
| Acceptance fixtures | Redacted sample requests, expected outputs, smoke and error cases. |
| Operations handover | Runbook, backup/restore, rollback, alert rules, escalation and support RACI. |

### 12.3 Customer-side release verification checklist

Target infrastructure approved: VM sizing, OS, container runtime, disk/encryption, database/queue/object store, DNS/TLS.

Network approved: inbound/outbound allowlists; local ERP/HRIS reachability; MCP server governance; no unintended egress.

Model availability verified: approved cloud endpoint or on-prem/local model; embedding model and vector dims compatible.

Configuration validation passed: secret refs exist, token scopes minimum, tenant ID mapped, KMS ACL and case ownership tested.

Functional smoke passed: valid happy path, missing knowledge, connector unavailable, approval wait, denial and retry behavior.

Security and operational gates passed: no dev mock, audit enabled, metrics/log retention set, backup/recovery tested, rollback supported.

Customer signs environment acceptance, service owner, operational access and support boundaries.

### 12.4 Deployment success states

| State | Criteria |
| --- | --- |
| Package ready | Bundle builds with checksum; all required dependencies named and locked. |
| Target configured | Local secrets, storage, policies and endpoints validated; no live credentials in artifact. |
| Healthy | Readiness/liveness passing and required integration probes verified. |
| Production accepted | Business UAT passed, audit/monitoring on, customer owner sign-off. |
| Degraded | Noncritical integration unavailable; impacted features named; no false success claim. |
| Rolled back | New executions route to previous approved version; in-flight cases evaluated for compatibility. |

### 12.5 Handover RACI (illustrative)

| Activity | Elenio Delivery | Customer IT | Business Owner |
| --- | --- | --- | --- |
| Solution design and blueprint | R | C | A |
| API/MCP integration testing | R | A | C |
| Infrastructure and secrets | C | R/A | I |
| Business acceptance | C | C | R/A |
| Release into customer production | R | A | I |
| Day-2 monitoring and incident | Support by contract | R/A | I |
| Policy and approval ownership | C | C | R/A |

R=Responsible; A=Accountable; C=Consulted; I=Informed. Final RACI must be contractually agreed per customer; platform permissions must implement actual delegated authority, not this illustrative table.

## 13  |  Security, Risk & Governance

### 13.1 Trust and permission boundaries

| Boundary | Control requirement |
| --- | --- |
| Identity & tenant | Verified issuer/audience/signature/expiry; server-side tenant membership; deny spoofed headers. |
| Agent tool execution | Tool allowlist, operation-level grants, egress policy and side-effect classification. |
| Knowledge sources | ACL re-check, source status/published/version/expiry, document/case scope, traceable evidence. |
| Human approvals | Independent delegated reviewer; SoD; no AI self-approval; audit of rationale and exact artifact version. |
| Secrets | Vault or equivalent encryption; no plaintext UI, logs, prompts, exported template or deployment bundle. |
| Supply chain | Pinned containers/dependencies, SBOM/checksum, signing, vulnerability gate, rollout/rollback. |
| Monitoring data | Redacted logs, limited retention, no raw CV/invoice payloads in generic metrics. |
| Model locality | Per-customer residency and permitted model providers; explicit third-party data transfer policy. |

### 13.2 AI-specific risks

| Risk | Mitigation / test |
| --- | --- |
| Prompt injection via documents | Treat source as untrusted data; tool/action boundaries enforced outside model; negative eval fixtures. |
| Fabricated citations / unsupported claims | Validate source/version/anchor against KMS; mark unsupported as needs_review. |
| Unsafe side effects | Read/write classification, idempotency, human gate, separate ERP permission. |
| Model schema drift | Constrained outputs, schema validation, regression evaluation and version pinning. |
| Tenant leakage | Strict tenant scopes, delegated user identity, cross-tenant negative tests. |
| Sensitive HR decisions | Human-owned evaluation; transparent evidence; protect candidate data and restrict retention. |
| Financial fraud / wrong payment | Finance SoD, ERP approval checks, no direct unapproved transfer, anomaly review. |
| Unbounded token/API spend | Model/tool call quotas, rate limits, budgets, timeout, circuit breaker. |

### 13.3 Governance operating model

Proposed review gates: G0 intake complete; G1 blueprint reviewed; G2 workflow/resource validation; G3 evaluation passed; G4 security/operations release approval; G5 production deployment acceptance. These are product gates, not claims that the existing SIM G0–G5 policies apply to every business domain; domain owners must ratify their own policies.

Incident behavior. Critical policy violations, unauthorized tool use, audit storage failure or unexpected cross-tenant visibility block the affected execution/release. Degraded noncritical integrations remain visible and must not be labelled healthy.

## 14  |  Non-Functional Requirements & Analytics

The following are proposed initial engineering targets, not measured guarantees. They must be finalized after real pilot performance, customer hardware sizing and risk assessment.

| NFR ID | Area | Acceptance target / requirement |
| --- | --- | --- |
| NFR-01 | Availability | Define SLO per deployment tier; pilot target 99.5% monthly runtime API availability excluding approved maintenance. |
| NFR-02 | Interactive UX | p95 non-LLM CRUD < 2s in qualified reference environment; LLM tasks stream progress / async run ID. |
| NFR-03 | Execution durability | Persist state transitions; retryable outbox; idempotent external writes; recover after worker crash. |
| NFR-04 | Data isolation | Zero unauthorized cross-tenant access in security negative suite. |
| NFR-05 | Authorization | 100% sensitive endpoints server-side verify subject, resource, action and policy. |
| NFR-06 | Encryption | TLS at exposed endpoints; encrypted persisted secrets; customer-specific KMS options. |
| NFR-07 | Audit | Record privileged mutations, approval actions, release decisions and tool writes with correlation ID. |
| NFR-08 | Observability | Structured logs, tracing, Prometheus metrics; trace each workflow/agent/tool chain. |
| NFR-09 | Performance budgets | Per-tenant quota: concurrent runs, tokens, tool calls, queue depth and storage. |
| NFR-10 | Backups | Define RPO/RTO per contract (pilot candidate: RPO ≤24h, RTO ≤4h); verify restore drill. |
| NFR-11 | Accessibility | Target WCAG 2.2 AA for major flows, including keyboard navigation, status text and contrast. |
| NFR-12 | Responsive UX | Desktop builder readable ≥960px; mobile case review and key actions supported. |
| NFR-13 | Portability | Runtime bundle builds and runs in a clean approved customer environment using published manifest. |
| NFR-14 | Trace retention | Configurable by tenant/security policy; default values ratified before production. |
| NFR-15 | Release integrity | Immutable artifact/version IDs, checksum and deterministic dependency lock. |
| NFR-16 | Model failure | Circuit-breaker/timeout/fallback policy; no false success if unavailable. |
| NFR-17 | Browser security | CSP, CSRF, secure cookies, safe redirects, no direct browser secret exposure. |
| NFR-18 | Localization | Bahasa Indonesia default; architecture supports EN copy and locale-aware formats. |

### 14.1 Analytics event design

| Event | Properties | Use |
| --- | --- | --- |
| solution_created | entry_source, domain, tenant, project | Measure intake preference |
| blueprint_approved | iterations, asset_reuse_count, missing_dependencies | Time to approved design |
| workflow_test_completed | status, failing_nodes, runtime_ms | Reliability and UX bottlenecks |
| agent_evaluation_completed | suite, critical_fail, model_mode | Quality and safety gate trend |
| deployment_completed | target_mode, duration, result | Customer delivery KPI |
| run_completed | workflow_version, status, tokens, api_calls, latency | Unit economics and operating success |
| approval_decision | role, elapsed, disposition, policy_version | Human bottlenecks, compliance |

### 14.2 Dashboard KPI cards

Portfolio: active projects, published workflows, production runtimes, recent releases. Delivery: time to first valid workflow, unresolved dependencies, deployment acceptance. Operation: executions, success/error/needs_review rates, approvals aging, time saved estimate (only with defensible baseline), cost/1,000 runs. Quality: regression results, unsupported claims, citation coverage, policy violations, resource reuse rate.

## 15  |  QA / UAT Scenarios & Acceptance

End-to-end success is demonstrated against actual connected services in a controlled customer-like environment. Passing component tests alone does not equal product acceptance.

| Test ID | Journey | Expected acceptance |
| --- | --- | --- |
| UAT-01 | Chat-to-blueprint | Complete recruitment request yields process, agent rationale, integrations and unresolved fields. |
| UAT-02 | Requirements gap | AI asks one relevant question for a missing mandatory approval authority. |
| UAT-03 | Blueprint review | Graph generation cannot finalize without authorized approval. |
| UAT-04 | Chat graph patch | User requests a branch change; system previews diff and supports reject. |
| UAT-05 | Manual workflow | Node library creates valid trigger→agent→human→API path. |
| UAT-06 | Typed schema | Mismatched output-input mapping blocks validation with field-level reason. |
| UAT-07 | Human SoD | Workflow owner cannot approve own privileged business action. |
| UAT-08 | Run version pin | Agent updated after run starts does not mutate in-flight run version. |
| UAT-09 | Agent sandbox | Output includes valid schema, cited evidence and trace. |
| UAT-10 | Agent negative eval | Prompt injection attempting unauthorized payment fails the release gate. |
| UAT-11 | Model unavailable | No approved provider yields blocked run with explicit dependency state. |
| UAT-12 | Knowledge upload | PDF ingestion shows progress and structured source version. |
| UAT-13 | KMS revoked source | Revoked source cannot be retrieved in new query. |
| UAT-14 | Tenant ACL | Agent from tenant A cannot read B collection even if URL/source ID guessed. |
| UAT-15 | API connection | Approved REST connector supports schema validate and read-only test. |
| UAT-16 | MCP allowlist | Unapproved discovered MCP tool cannot be invoked. |
| UAT-17 | Secrets | Export/template does not contain live key or token. |
| UAT-18 | Template reuse | Clone creates editable project with missing dependency checklist. |
| UAT-19 | Untrusted import | Unsupported/unsafe script produces report, not runnable node. |
| UAT-20 | Workflow test | Execution visualizes statuses, step traces, approval wait and errors. |
| UAT-21 | ERP write retry | Repeated callback/retry with same idempotency key creates one action. |
| UAT-22 | Release review | Critical eval failure or unapproved connection blocks publish. |
| UAT-23 | Docker release | Package deploys on clean VM with prescribed DB/config/secret refs. |
| UAT-24 | Customer isolation | Runtime can execute without accessing Elenio development credentials. |
| UAT-25 | Broken dependency | Missing customer model or KMS makes readiness fail truthfully. |
| UAT-26 | Rollback | New runs route to previous release; historical runs preserve version pins. |
| UAT-27 | Case UI | Business user can submit form and track case without engineering access. |
| UAT-28 | Approval permissions | Unauthorized browser/user cannot approve via direct API call. |
| UAT-29 | Operations trace | Ops can find error, correlation ID, affected version and recommended action. |
| UAT-30 | Responsive | Primary screens render on desktop/mobile without overflow or hidden CTAs. |
| UAT-31 | Audit | Privileged action logs actor, action, resource, version and outcome. |
| UAT-32 | Cross-project reuse | Published agent/skill connector reused in another project without data leakage. |

### 15.1 Pilot acceptance definition

| Pilot go/no-go; Pilot may be accepted only if Recruitment and Vendor Payment journeys both complete from requirement intake to tested runtime; at least one real customer-like API/MCP integration works; human approval and tenant isolation pass; customer VM deployment, health, rollback and signed handover are demonstrated; high-risk defects are closed or explicitly waived by owner with documented risk acceptance. |
| --- |


## 16  |  Delivery Roadmap & Dependencies

### 16.1 Workstreams

| Phase / stream | Deliverables | Exit gate |
| --- | --- | --- |
| P0-A: Product spine | Unified project, navigation context, AI Architect requirement/blueprint, template discovery. | S01–S04 complete; UAT-01–04 pass. |
| P0-B: Builder | Workflow canvas, agent/skill/KMS/connector bindings, versioning. | UAT-05–19 pass on integration environment. |
| P0-C: Validate | Unified simulation, trace, evaluation, approval/release gates. | Critical test suite green; UAT-20–22 pass. |
| P0-D: Deliver | Docker customer runtime bundle, manifest, secrets, operational release and rollback. | UAT-23–26 pass in clean VM. |
| P0-E: Business operation | Case Workspace UI, approval inbox, audit, operations dashboard. | UAT-27–32 pass; named service owner. |
| P1: Enterprise hardening | Helm/K8s, full private stack, template import adapters, SSO, cost governance, regression. | Enterprise launch readiness checklist. |
| P2: Ecosystem | Certified marketplace, domain packs, scaled control tower, air-gap. | Measured reuse and operating economics. |

### 16.2 Recommended backlog order (avoid rework)

Freeze core ownership and common project/workflow/agent/connector/knowledge reference contracts.

Create unified project shell and cross-mini-app navigation before redesigning every screen independently.

Generalize chat intake from SIM into multi-domain requirement schema and blueprint with human approval.

Implement capability discovery + dependency resolver using existing registries; avoid duplicative asset storage.

Attach Workflow Studio to normalized blueprint compiler, node validator and runtime versioning.

Connect API/MCP registry and end-to-end test traces before claiming customer-ready integrations.

Create portable deployment package from actual runtime images and validate in isolated customer-like VM.

Finalize Business App/operator UX, operations monitors, docs and acceptance with two pilots.

### 16.3 Dependencies and engineering decisions

| Dependency | Why it blocks | Owner to decide |
| --- | --- | --- |
| Generic blueprint JSON schema | Needed to bridge chat, UI, graph compiler, template and deployment. | Product + Solution Architecture |
| Service contract ownership | Avoid multiple services editing same workflow/agent/artifact state. | Tech Lead + each service owner |
| MCP tool policies and credential vault | Cannot responsibly offer enterprise writes without access and egress control. | Security + Integration Engineer |
| Target self-host reference environment | Needed to size, test, deploy, and hand over a reproducible runtime. | DevOps + customer IT |
| Approval role authority | Business outcome cannot be approved by generic builder permissions. | Domain owner + Governance |
| Real test providers / integrations | Mocks are not evidence of live connector reliability. | Engineering + customer sponsor |

## 17  |  Decisions, Assumptions & Open Questions

### 17.1 Decisions recommended to lock

| Decision | Recommendation | Rationale |
| --- | --- | --- |
| Primary product | AI Workspaces = enterprise IT solution engineering platform. | Avoid confusion with end-user chatbot SaaS. |
| Default UX | Home launchpad + AI Architect; manual tool paths remain first-class. | Serve varying builder maturity and reuse. |
| Execution logic | Deterministic orchestrator + selected reasoning agents + human gates. | Control cost, latency and safe business actions. |
| Service ownership | Keep Control Plane/Flowcase/Agent Studio/KMS/UCW independent with typed contracts. | Build on existing investment and isolate domain policy. |
| Delivery default | Self-hosted customer runtime, optional full private platform. | Meet buyer and customer ownership needs. |
| Business app | Use safe schema/domain-pack generated UI, not arbitrary remote code. | Security and maintainability. |
| Import strategy | Normalize and validate source-specific adapters incrementally. | Avoid impossible universal compatibility promise. |

### 17.2 Open questions for product owner / engineering

Q01. Apakah setiap customer akan memiliki full platform license, atau runtime-only default dengan optional authoring entitlement?

Q02. Apa batasan teknis source import pertama (native JSON, BPMN, n8n, Dify)? Pilih 1–2, bukan universal.

Q03. Model apa yang resmi di-support dan bagaimana fallback ketika customer harus memakai LLM lokal?

Q04. Apakah setiap workflow harus menghasilkan case app, atau hanya workflow dengan human task/form?

Q05. Siapa authoritative source untuk project metadata versus Flowcase case/artifact schema?

Q06. Apakah deployment bundle memuat runner generik + definitions atau code-generated service per solution?

Q07. Apa standar secret manager dan packaging infrastruktur on-prem yang disupport?

Q08. Berapa NFR minimal runtime footprint dan concurrency untuk reference VM?

Q09. Siapa yang menyetujui publikasi global template dan apakah customer templates boleh dishare ke tenant lain?

Q10. Bagaimana lisensi offline dan support diagnostics bekerja pada customer restricted network?

Q11. Apakah retrieval untuk case private selalu membutuhkan delegated user atau juga service-only workflows?

Q12. Apakah approval kebijakan vendor payment harus memakai ERP authority atau internal Flowcase authority, atau keduanya?

Q13. Apa domain pack kedua setelah SIM yang akan jadi official demo: Recruitment atau Vendor Payment?

Q14. Apakah customer operator memiliki UI dalam server customer atau melalui Elenio managed portal?

Q15. Siapa yang memiliki tanggung jawab patching dan incident response untuk managed versus self-hosted mode?

### 17.3 Key assumptions

PRD ini mengasumsikan Control Plane tetap berfungsi sebagai trusted identity shell; Flowcase tetap menjadi workflow/case orchestrator; Agent Studio tetap menjadi agent runtime/release authority; KMS tetap authoritative untuk knowledge/evidence; Universal Case Workspace menjadi presentation layer untuk business users. Integrasi di antara layanan ini wajib diuji ulang pada commit/environment yang akan dipakai sebelum development dimulai.

## 18  |  Glossary & References

### 18.1 Glossary

| Term | Meaning |
| --- | --- |
| AI Architect | Conversation-based solution engineering interface, not an autonomous security approver. |
| Workflow | Deterministic/agentic business process graph with transitions and runtime state. |
| Agent | Versioned reasoning/execution component with a bounded role, model, skills and permissions. |
| Skill | Reusable capability/behavior contract exposed to agents under version and governance. |
| Tool / Connector | Authorized bridge to an external service or function (API/MCP/DB/webhook). |
| KMS | Enterprise knowledge/evidence source with content ownership and ACL. |
| Template | Validated reusable solution/workflow manifest with declared dependencies. |
| Blueprint | Reviewed architectural plan before materializing a workflow definition. |
| Case | Business instance of a process; contains tasks, artifacts, approvals and run history. |
| Run | Execution instance of a pinned workflow or agent version. |
| Control Plane | Identity, workspace control, mini-app navigation and governance orchestration. |
| Execution Runtime | Software that executes workflow/agents under customer environment policies. |
| Self-hosted | Customer operates one or more platform/runtime components in customer infrastructure. |
| SoD | Separation of Duties; critical role and approval separation. |
| MCP | Model Context Protocol; standardized agent-to-tool/server interface, not an authorization grant. |
| RAG | Retrieval-Augmented Generation; model uses scoped knowledge retrieval as evidence. |

### 18.2 Repository paths reviewed

https://github.com/kawakibireku/ai-workspace

control_plane/README.md; docs/INTEGRATION_MATRIX.md; docs/IMPLEMENTATION_REPORT.md

flowcase_platform/README.md; docs/PHASE4_REPORT.md; docs/ARCHITECTURE.md

ai_agent_studio/README_START_HERE.md; docs/PHASE2_4_REPORT.md; apps/agent-studio-web/README.md

kms_go_package/README.md; backend/openapi.yaml

universal_case_workspace/README_START_HERE.md; docs/INTEGRATION_MATRIX.md

Elenio_AI_Workspaces_Interactive_Journey.html (locally supplied frontend UX concept)

Elenio brand assets: Logo Default.png; Visual Languange.jpeg; Social and identity.jpeg

| Final drafting note; This document is an implementation-oriented product proposal grounded in reviewed repository documentation and the supplied UI concept. It is not a declaration that the full platform, cross-service workflow, production model calls, or customer deployments have passed live end-to-end validation. |
| --- |
