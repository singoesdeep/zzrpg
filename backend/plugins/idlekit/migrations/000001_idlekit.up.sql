-- idlekit ships the gamekit standard tables it needs (idempotent — a sibling
-- gamekit plugin may have created them) plus its own producer component.
CREATE TABLE IF NOT EXISTS entities (
    id BIGSERIAL PRIMARY KEY, kind VARCHAR(50) NOT NULL, owner_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_entities_owner ON entities(owner_id);
CREATE TABLE IF NOT EXISTS entity_wallet      (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS entity_system_runs (system VARCHAR(50) NOT NULL, entity_id BIGINT NOT NULL, ran_at TIMESTAMP WITH TIME ZONE NOT NULL, PRIMARY KEY (system, entity_id));
CREATE TABLE IF NOT EXISTS idlekit_producer   (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
