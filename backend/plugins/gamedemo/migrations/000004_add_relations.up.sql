-- Entity relation graph for the built-in gamekit relation toolkit.
CREATE TABLE IF NOT EXISTS entity_relations (
    from_id BIGINT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    edge_type VARCHAR(50) NOT NULL,
    to_id BIGINT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    PRIMARY KEY (from_id, edge_type, to_id)
);
CREATE INDEX IF NOT EXISTS idx_entity_relations_to ON entity_relations(to_id, edge_type);
