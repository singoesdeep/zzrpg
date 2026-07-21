-- gamekit standard schema: entities + built-in component tables + system runs.
CREATE TABLE IF NOT EXISTS entities (
    id BIGSERIAL PRIMARY KEY, kind VARCHAR(50) NOT NULL, owner_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_entities_kind ON entities(kind);
CREATE INDEX IF NOT EXISTS idx_entities_owner ON entities(owner_id);
CREATE TABLE IF NOT EXISTS entity_stats       (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS entity_progression (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS entity_inventory   (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS entity_wallet      (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS entity_system_runs (system VARCHAR(50) NOT NULL, entity_id BIGINT NOT NULL, ran_at TIMESTAMP WITH TIME ZONE NOT NULL, PRIMARY KEY (system, entity_id));
CREATE TABLE IF NOT EXISTS entity_relations (
    from_id BIGINT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    edge_type VARCHAR(50) NOT NULL,
    to_id BIGINT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    PRIMARY KEY (from_id, edge_type, to_id)
);
CREATE INDEX IF NOT EXISTS idx_entity_relations_to ON entity_relations(to_id, edge_type);
