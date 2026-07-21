-- idlekit now stores an activity Assignment per mirrored entity (the producer
-- rate is derived live from character state, so the old producer table is gone).
CREATE TABLE IF NOT EXISTS idlekit_assignment (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
DROP TABLE IF EXISTS idlekit_producer;
