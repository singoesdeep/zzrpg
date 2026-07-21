-- gamekit entity foundation: a bare identity to which components attach.
CREATE TABLE IF NOT EXISTS entities (
    id         BIGSERIAL PRIMARY KEY,
    kind       VARCHAR(50) NOT NULL,
    owner_id   BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_entities_owner ON entities(owner_id);
CREATE INDEX IF NOT EXISTS idx_entities_kind  ON entities(kind);
