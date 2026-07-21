-- Wallet component for the built-in gamekit economy toolkit.
CREATE TABLE IF NOT EXISTS entity_wallet (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
