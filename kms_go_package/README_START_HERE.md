# Knowledge Workspace mini-app — implementation package

## Files

- `KMS_TECHNICAL_PRD.md`: engineering product spec, architecture, data model, security, pipeline, API, acceptance tests and build order.
- `MASTER_PROMPT_FOR_CODEX.md`: ready-to-paste agent instruction for building the system.
- `Knowledge_Workspace_Navy_Cobalt.html`: existing UI mockup / visual reference (browser demo, **not** production backend).

## Recommended workflow

1. Create an empty repo or choose the new KMS repo.
2. Give coding agent all three files; paste `MASTER_PROMPT_FOR_CODEX.md` as the initial instruction.
3. Configure host auth contract and existing Docling endpoint; record ES version/licensing and embedding model/dimensions.
4. Implement vertical slices; execute acceptance tests in PRD.

## Major decisions

- Backend: Go.
- PostgreSQL: canonical structured data and transaction workflows.
- Elasticsearch: keyword, semantic kNN, hybrid search (NO pgvector).
- Docling Serve: existing self-hosted REST service for PDF conversion.
- Embedding endpoint: independently configured provider.
- Files: S3-compatible storage.
- Mini-app: host SSO/session and KMS API authorization; no separate login screen.

## What is not included

No production backend code is bundled here. The HTML is demo UI with illustrative data. PRD is a recommended specification pending host/auth/platform-specific decisions.
