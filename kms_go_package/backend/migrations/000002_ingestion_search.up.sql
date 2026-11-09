CREATE TABLE collection_memberships (
    tenant_id UUID NOT NULL,
    collection_id UUID NOT NULL,
    principal_id UUID NOT NULL,
    membership_role TEXT NOT NULL CHECK (membership_role IN ('owner', 'steward', 'contributor', 'reader')),
    granted_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, collection_id, principal_id),
    FOREIGN KEY (tenant_id, collection_id) REFERENCES collections(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE case_scopes (
    tenant_id UUID NOT NULL,
    case_id UUID NOT NULL DEFAULT gen_random_uuid(),
    collection_id UUID NOT NULL,
    name TEXT NOT NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, case_id),
    UNIQUE (tenant_id, case_id, collection_id),
    FOREIGN KEY (tenant_id, collection_id) REFERENCES collections(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE case_memberships (
    tenant_id UUID NOT NULL,
    case_id UUID NOT NULL,
    principal_id UUID NOT NULL,
    membership_role TEXT NOT NULL CHECK (membership_role IN ('owner', 'steward', 'contributor', 'reader')),
    granted_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, case_id, principal_id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES case_scopes(tenant_id, case_id) ON DELETE CASCADE
);

ALTER TABLE documents ADD COLUMN case_id UUID;
ALTER TABLE documents ADD CONSTRAINT documents_case_scope_fk
    FOREIGN KEY (tenant_id, case_id, collection_id) REFERENCES case_scopes(tenant_id, case_id, collection_id);
ALTER TABLE document_versions ADD COLUMN original_filename TEXT NOT NULL DEFAULT 'document.pdf';

CREATE TABLE ingestion_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    collection_id UUID NOT NULL,
    document_id UUID NOT NULL,
    version_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'failed', 'indexed')),
    stage TEXT NOT NULL DEFAULT 'queued',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_expires_at TIMESTAMPTZ,
    docling_task_id TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, version_id),
    FOREIGN KEY (tenant_id, collection_id) REFERENCES collections(tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id),
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_versions(tenant_id, id)
);

CREATE INDEX ingestion_jobs_claim_idx ON ingestion_jobs(status, available_at, lease_expires_at, created_at);
CREATE INDEX ingestion_jobs_document_idx ON ingestion_jobs(tenant_id, document_id, created_at DESC);

CREATE TABLE parser_artifacts (
    tenant_id UUID NOT NULL,
    version_id UUID NOT NULL,
    artifact_type TEXT NOT NULL CHECK (artifact_type IN ('markdown', 'docling_json')),
    object_key TEXT NOT NULL,
    media_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, version_id, artifact_type),
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_versions(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE document_chunks (
    chunk_id TEXT PRIMARY KEY,
    tenant_id UUID NOT NULL,
    collection_id UUID NOT NULL,
    case_id UUID,
    document_id UUID NOT NULL,
    version_id UUID NOT NULL,
    chunk_number INTEGER NOT NULL CHECK (chunk_number >= 0),
    content TEXT NOT NULL,
    heading_path TEXT[] NOT NULL DEFAULT '{}',
    page_start INTEGER,
    page_end INTEGER,
    source_anchor TEXT,
    embedding_model_id TEXT,
    indexed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, version_id, chunk_number),
    FOREIGN KEY (tenant_id, collection_id) REFERENCES collections(tenant_id, id),
    FOREIGN KEY (tenant_id, case_id, collection_id) REFERENCES case_scopes(tenant_id, case_id, collection_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id),
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_versions(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX document_chunks_search_auth_idx ON document_chunks(tenant_id, collection_id, document_id, version_id);
CREATE INDEX case_memberships_principal_idx ON case_memberships(tenant_id, principal_id, case_id);
