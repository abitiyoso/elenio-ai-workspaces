# MASTER PROMPT — Implement Knowledge Workspace Mini-App

Use this prompt with Codex, Claude Code or another coding agent. Attach/reference:

- `KMS_TECHNICAL_PRD.md` — primary functional and engineering contract.
- `Knowledge_Workspace_Navy_Cobalt.html` — **UI visual reference only**, not backend/auth implementation.
- SIM B2B Solution Workspace PRD v3.1 if available — consumer integration and governance context.

---

You are the lead full-stack engineer responsible for shipping a maintainable enterprise Knowledge Management System as a host-wrapped mini-app. Read the attached **KMS_TECHNICAL_PRD.md** fully before generating code. Treat the supplied Navy+Cobalt HTML as the required visual reference: preserve its look/feel, navigation, document-oriented interactions, responsive behavior, and high-contrast palette. Refactor it into production components. Do not replace the design with generic dashboards or AI-themed gradients.

## Mandatory technology and constraints

- Backend in **Go**; use clear modular architecture, constructor-based dependency injection, request-scoped `context.Context`, strict errors, instrumentation, and tests.
- PostgreSQL is the authoritative transactional database for documents, versions, collections, users/memberships, ACL, review decisions, job status, audit logs and outbox. **Never use pgvector or PostgreSQL for vector search.**
- Elasticsearch handles BM25 keyword search and `dense_vector` semantic kNN; implement hybrid retrieval (RRF if installed Elasticsearch supports it; otherwise a well-tested fusion fallback). Use the official Go Elasticsearch client compatible with the installed server major version. Search API always enforces tenant/case/collection/document access and publication policy.
- Integrate the **existing self-hosted Docling Serve** over HTTP; **DO NOT** deploy/duplicate Docling or attempt Go-native PDF extraction in place of it. Discover the exact enabled API through the configured service `/docs`. Support async conversion/retries/result retrieval; persist parser provenance and source anchors. Assume Docling is available via `DOCLING_BASE_URL` with optional server-side `DOCLING_API_KEY`.
- Embedding is a **separate pluggable HTTP provider**, configured with endpoint/model/dimension. Do not assume Docling returns embeddings; do not hard-code model dimensions or implement a phantom embedding service. Support index migration/reindex when model changes.
- S3-compatible storage (MinIO in local dev) for file originals and parsed artifacts. Files remain protected by KMS authorization.
- Frontend preferred: **React + TypeScript + Vite**, with the provided HTML/CSS as design reference. Keep a compact component system and avoid unnecessary UI libraries. Backend must remain fully Go. Preserve all major pages and actions from reference.
- No standalone username/password login. This is a **mini-app** authenticated by parent host/wrapper. Build an `AuthAdapter` interface with verified SSO session/JWT or signed one-time launch exchange; local mock identity only in dev and refused in prod. Never trust browser-provided `tenant_id`, `user_id`, role headers or unsigned `postMessage` payloads.
- No production secrets in frontend or repo; no direct browser access to PostgreSQL, Elasticsearch, Docling admin API, or S3 credentials.
- Do not claim a feature works unless you implemented and tested it. Keep unresolved integration choices behind adapters/env configuration; do not fake production answers or data.

## Design and architecture

Build a **modular monolith** initially with:
- `cmd/kms-api`: HTTP API, auth, authorization, collections, document/version management, reviews, search, retrieval and audit endpoints.
- `cmd/kms-worker`: ingestion/job pipeline: file -> Docling -> structured normalization -> chunking -> embedding -> Elasticsearch indexing; use durable jobs and idempotency.
- `web`: mini-app frontend.

Suggested Go packages/interfaces: `internal/domain`, `internal/httpapi`, `internal/auth`, `internal/policy`, `internal/collections`, `internal/documents`, `internal/reviews`, `internal/ingestion`, `internal/docling`, `internal/chunking`, `internal/embeddings`, `internal/search`, `internal/storage`, `internal/jobs`, `internal/audit`, `internal/db`, `internal/observability`. Adjust names if helpful but preserve responsibility boundaries. Prefer `pgx`, SQL migrations, explicit repositories, the official Elasticsearch client, `net/http` + lightweight router, and OpenAPI documentation. Avoid premature microservices.

### Ingestion contract

1. Browser uploads document to secured KMS endpoint.
2. API verifies authenticated user/tenant, filename/MIME/size/scope, records version/checksum and original file location.
3. Worker picks durable job. Call existing Docling Serve using HTTP (`/v1/convert/file/async`, status polling and result retrieval if supported); record the task ID, errors and parser output.
4. Store normalized Markdown and structured JSON as derived files. Create structure-aware chunks preserving headings and verifiable page/section/table provenance; never invent page numbers or trust levels.
5. Call configured embedding service for chunks. Insert metadata/versions in PostgreSQL and index chunk text and vectors in Elasticsearch under versioned index alias.
6. Publication remains human-controlled. Search visibility depends on source scope and trust policy, not merely indexing success. Retry safely; remove/reindex on revocation or version replacement.

### Search contract

`POST /api/v1/search` accepts query, mode `keyword|semantic|hybrid`, filters, topK, and optional case context. Derive tenant and user from verified auth context. Elasticsearch candidate retrieval must use relevant filters in lexical **and** vector branches. **Post-authorize all hits against PostgreSQL** before returning snippets or download links. Return document ID, version ID, chunk ID, source title, source page/section anchors, trust/expiry status and a readable citation. Keep business content confidential in logs.

### Host authentication contract

Implement interface for:
- JWT/OIDC validation and trusted claim mapping when host provides a compatible issuer;
- alternatively short-lived one-time launch code exchanged server-to-server for session;
- GET `/api/v1/session` showing effective capabilities (not secrets);
- logout/session-expiry UX; 401 for absent identity; 403 for insufficient resource permissions;
- `AUTH_MODE=mock` only for local development with explicit startup rejection in production.

For iframe deployment enforce exact `postMessage` origin/source, `frame-ancestors` CSP, same-origin preferred; avoid long-lived tokens in localStorage or URL. Cookie/session solution must account for browser third-party-cookie restrictions and CSRF.

## Deliverables

1. Proposed project tree and short architecture decision record (ADR) listing unresolved assumptions.
2. PostgreSQL migration SQL, relevant indexes, ownership and foreign-key constraints.
3. Elasticsearch index template/mapping for text+vector+metadata and index alias migration procedure.
4. OpenAPI v1 spec, standardized errors, auth and API sample requests/responses.
5. Go backend with feature-specific handlers/service/repository adapters; configuration via `.env.example` and strict validation.
6. Async job processing, Docling HTTP integration, embedding provider adapter, Elasticsearch writer/searcher, reindex/retry logic.
7. React + TypeScript frontend migrated from the provided HTML styling with these pages: Overview, Library, Collections, Document Detail/Preview, Review Center, Search, Ingestion Jobs, Audit/Settings.
8. Docker Compose for **local PostgreSQL, Elasticsearch, MinIO and app**; external Docling remains configurable. Do not start second Docling by default.
9. Unit tests and integration tests including case isolation, unauthorized search, approval versioning, Docling partial parsing, indexing retry/idempotence, revocation and source citation correctness.
10. README runbook: setup, required env, dependency versions, sample dataset import, API examples, troubleshooting, reindexing and deployment/wrapper integration.

## Implementation sequence

**Phase 0:** inspect existing repo and UI HTML; inspect live Docling `/docs`, ES version/capabilities and host auth assumptions. If credentials/endpoints missing, build adapters/config contracts without claiming live integrations worked. Produce small ADR and plan.

**Phase 1:** migrations, auth adapter, tenant/resource authorization, collections, document storage, upload and list UI. Ship vertical slice with tests.

**Phase 2:** worker, Docling async, chunk normalization, embeddings, Elasticsearch BM25/kNN/hybrid, search UI with citations. Include indexing and security integration tests.

**Phase 3:** reviews, publication, versioning, audit, ingestion status, revoke/delete, agent retrieval API and permission regression tests.

**Phase 4:** production hygiene (observability, health, retries, backfills, UI responsiveness, accessibility, configuration and docs).

After each phase, report concrete files changed, working endpoints, how to run/test, failing tests and remaining risks. **Start coding Phase 1 after inspecting the repository.** If no repository exists, scaffold the specified project. Do not stop after providing a plan unless blocked by missing essential credentials or host API contracts; develop using adapter/test doubles only where the real services cannot be contacted and label them clearly.

## Strict acceptance criteria

- No pgvector, no separate production login screen, no own Docling deployment, no fake Elasticsearch or self-approval path in production.
- Security is enforced on the server for each API and object, regardless of frontend state.
- Demonstrate one PDF source going through Docling -> structured chunks -> embedding -> Elasticsearch -> authorized search -> document/page citation.
- No cross-tenant or cross-case result from lexical, vector, hybrid, or direct document URLs.
- Approval is tied to an immutable version; new versions require deliberate review.
- Frontend retains Navy/Cobalt visual style, usable desktop/mobile tables, forms and review UX.
- CI can run unit tests; integration tests work with ephemeral Elasticsearch/PostgreSQL/MinIO or docker compose.

---

**Important:** This KMS is a reusable governed source/evidence system. Keep SIM-specific agent skills, commercial signoffs, proposal gates and bid decisions outside KMS, integrated via explicit APIs only.
