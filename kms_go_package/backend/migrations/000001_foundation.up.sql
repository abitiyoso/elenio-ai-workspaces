CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE collections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    classification TEXT NOT NULL DEFAULT 'internal',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    collection_id UUID NOT NULL,
    title TEXT NOT NULL,
    source_type TEXT NOT NULL DEFAULT 'upload',
    lifecycle_status TEXT NOT NULL DEFAULT 'draft' CHECK (lifecycle_status IN ('draft','processing','ready_for_review','published','expired','archived','revoked')),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, collection_id) REFERENCES collections(tenant_id, id)
);

CREATE TABLE document_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    document_id UUID NOT NULL,
    version_number INTEGER NOT NULL CHECK (version_number > 0),
    checksum_sha256 CHAR(64) NOT NULL,
    original_object_key TEXT NOT NULL,
    media_type TEXT NOT NULL,
    parser_status TEXT NOT NULL DEFAULT 'pending',
    index_status TEXT NOT NULL DEFAULT 'pending',
    review_status TEXT NOT NULL DEFAULT 'draft',
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    valid_from TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, document_id, version_number),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id)
);

CREATE TABLE knowledge_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    collection_id UUID NOT NULL,
    document_id UUID,
    source_uri TEXT NOT NULL,
    source_type TEXT NOT NULL,
    trust_status TEXT NOT NULL DEFAULT 'unverified' CHECK (trust_status IN ('approved','unverified','customer_source')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, collection_id) REFERENCES collections(tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id)
);

CREATE INDEX documents_tenant_collection_created_idx ON documents(tenant_id, collection_id, created_at DESC);
CREATE INDEX versions_tenant_document_idx ON document_versions(tenant_id, document_id, version_number DESC);
CREATE INDEX knowledge_sources_tenant_collection_idx ON knowledge_sources(tenant_id, collection_id, created_at DESC);
