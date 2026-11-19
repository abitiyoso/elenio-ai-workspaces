# MASTER PROMPT ADDENDUM
## Elenio AI OS — Product Delivery & Enterprise Deployment

This addendum extends the existing Master Prompt, PRD, architecture, design system, and development plan. It does not replace previous requirements.

### 1. Product Identity and Objective

The product is named **Elenio AI OS — Enterprise Agentic AI Platform**.

Its primary purpose is to enable Elenio engineers to rapidly design, build, evaluate, optimize, and deliver complete agentic business systems to enterprise customers.

Elenio AI OS is a Solution Factory, not merely a workflow automation tool, chatbot builder, or agent management dashboard.

The ultimate output must be a working business application or an API-enabled business service that can become part of the customer's existing enterprise application ecosystem.

### 2. Three Operating Modes

Support three operating modes using the same shared core architecture.

**Mode A — Elenio Internal Engineering Platform**

- Full access to AI Architect, Workflow Studio, Agent Studio, Model Hub, Knowledge Hub, Skills, Integrations, Testing, AI Economics, and Solution Publisher.
- Manage multiple customer projects with isolated configurations, credentials, and data.
- Reuse approved templates, agents, skills, and integration components across projects.

**Mode B — Customer Solution Deployment**

- Deliver an independently installable customer-specific business application and its required runtime services.
- Deploy into customer-owned infrastructure.
- Provide a dedicated application URL, customer branding, authentication, business roles, dashboards, forms, tasks, approvals, reports, and APIs as required.
- Exclude internal engineering tools unless explicitly included.
- Execute independently from Elenio's development environment.

**Mode C — Enterprise AI OS Deployment**

- Install the complete platform within the customer's infrastructure.
- Allow enterprise IT teams to independently create, configure, test, and publish AI Workspaces for their business units.
- Support organization, business unit, project, workspace, and application-level access isolation.
- Reuse shared models, knowledge, agents, skills, connectors, and runtime infrastructure subject to authorization.
- Publish additional workspaces without reinstalling the platform.

### 3. Solution Publisher

Elevate Solution Publisher into a first-class product capability.

The complete engineering journey must be:

Discover → Design → Build → Test → Evaluate → Cost Analysis → Publish → Deploy → Operate → Improve.

Solution Publisher must support:

- Customer business application composition using configurable UI components and domain packs.
- Business application preview before publishing.
- Dependency resolution for workflows, agents, skills, tools, models, knowledge sources, permissions, and integrations.
- Deployment configuration, validation, and release approval.
- Immutable, versioned solution releases.
- Installation, upgrade, health checks, rollback, and operational handover.

Support publishing an application with a business UI, an API-only service, or both.

Do not limit customer applications to a single generic case management interface.

### 4. Deployment Artifact Architecture

Separate reusable platform software from customer-specific solution configuration.

Use reusable OCI-compatible container images for the application runtime, workflow engine, agent runtime, control plane, knowledge services, and required integrations.

Generate versioned solution bundles containing:

- Release manifest and dependency versions.
- Workflow and agent definitions.
- Application UI configuration and domain packs.
- Model, knowledge, and integration bindings.
- Identity and permission policies.
- Docker Compose deployment configuration.
- Environment configuration templates.
- Installation and database migration scripts.
- Health checks, smoke tests, backup and rollback procedures.
- Deployment and operation documentation.

Never include plaintext credentials, unauthorized customer data, or development environment secrets in exported packages.

Use Docker Compose as the initial deployment baseline. Design extension points for Kubernetes, private cloud, and offline installation.

Do not rebuild every runtime image when only workflow or application configuration changes.

### 5. Application Provisioning and URLs

Published applications must have usable access endpoints.

Illustrative examples:

- Internal engineering: studio.elenio.id
- Standalone customer solution: finance.customer.example
- Enterprise platform: ai.customer.example
- Published departmental workspace: ai.customer.example/workspaces/hr

Support customer-controlled domains, reverse proxy configuration, HTTPS, corporate SSO, and authorization.

Actual DNS, certificates, and network access must be configured and validated in the target environment.

A generated URL alone does not constitute a successful deployment.

### 6. Customer Independence and Data Sovereignty

Customer-hosted deployments must not require connectivity to Elenio's development environment or external control plane for normal execution.

Support customer-owned databases, document storage, credentials, identity providers, approved AI model endpoints, and enterprise integrations.

Support external model APIs, private inference endpoints, and hybrid model strategies.

On-premises deployment does not automatically imply offline inference. Clearly identify all external dependencies and required network access.

### 7. Enterprise Licensing Readiness

Design a centralized entitlement architecture that can later support:

- Solution-only licenses.
- Full enterprise platform licenses.
- Published workspace limits.
- Builder and user entitlements.
- Runtime capacity and concurrent executions.
- Enterprise feature access.
- Maintenance and support periods.
- Signed offline license activation.

Keep entitlements separate from authentication and authorization. Enforce applicable limits server-side.

Do not introduce mandatory external license connectivity for isolated customer environments.

Full commercial billing and licensing implementation may follow later. Prepare the architecture without delaying core product development.

### 8. AI Economics Across Deployment Modes

Maintain cost visibility at agent, workflow, execution, workspace, business unit, and organization levels where applicable.

Distinguish estimated costs, measured test costs, and actual production costs.

Include model inference, tools, retries, OCR, infrastructure, human review, and applicable licensing costs.

Allow customers to compare the cost of conventional business processes with AI-enabled processes.

Do not represent simulated costs as actual production data.

### 9. End-to-End Acceptance Criteria

The final product must demonstrate two complete delivery scenarios.

**Scenario 1: Standalone Customer Application**

An Elenio engineer builds a representative business solution, tests it, publishes a versioned release, installs it into a clean environment using the generated package, and accesses its dedicated URL.

The application must successfully execute an authorized business case, persist results, expose artifacts, enforce human approvals where required, and record execution costs.

Demonstrate backup, restart, upgrade, and rollback without losing customer operational data.

**Scenario 2: Enterprise AI OS**

Install the full platform into a separate customer environment.

Demonstrate an enterprise IT administrator independently creating and publishing at least two departmental workspaces using shared runtime capabilities, with distinct access permissions and isolated business data.

Prove that both workspaces can execute independently of Elenio's development environment.

A working canvas, exported JSON file, generated manifest, or simulated deployment is not sufficient to satisfy these acceptance criteria.

### 10. Development Instructions

Review the current implementation against this addendum. Classify each requirement as existing, partially implemented, missing, or verified.

Update the relevant architecture documents, PRD, FEATURE_MATRIX_LIVE.md, STATUS.md, and phased acceptance criteria.

Preserve existing backend contracts, governance, security controls, team contributions, UI direction, and all previously agreed capabilities.

Do not restart completed work unnecessarily. Implement missing capabilities according to the existing development phases.

Prioritize Docker Compose deployment and one complete working customer solution before advanced licensing, Kubernetes automation, or commercial billing.

Keep all changes within the authorized repository and feature branches. Never modify the original upstream repository, force-push, or deploy to production without approval.

Use this addendum as the permanent product outcome and deployment architecture reference for all future development sessions.