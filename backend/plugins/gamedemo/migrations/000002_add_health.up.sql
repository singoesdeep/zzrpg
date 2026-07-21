-- Health component for the combat System.
CREATE TABLE IF NOT EXISTS entity_health (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
