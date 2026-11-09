-- Documents the user uploaded into an intake chat (e.g. a KAK/RFP). The original file is not kept: only its
-- checksum and the text Docling extracted, which fills the pack's document field of the draft. The text is
-- never written to the audit trail or logs.
CREATE TABLE workspace_intake_documents (
    id              uuid PRIMARY KEY,
    session_id      uuid NOT NULL REFERENCES workspace_intake_sessions (id) ON DELETE CASCADE,
    tenant_key      text NOT NULL,
    filename        text NOT NULL CHECK (char_length(filename) BETWEEN 1 AND 200),
    media_type      text NOT NULL,
    size_bytes      integer NOT NULL CHECK (size_bytes > 0),
    sha256          text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    status          text NOT NULL CHECK (status IN ('processing', 'ready', 'failed')),
    task_id         text,
    content         text,
    chars           integer CHECK (chars >= 0),
    error           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_id, sha256),
    CHECK ((status = 'ready') = (content IS NOT NULL)),
    CHECK (status <> 'failed' OR error IS NOT NULL)
);
CREATE INDEX workspace_intake_documents_session_idx ON workspace_intake_documents (session_id, created_at);
