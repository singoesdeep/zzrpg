CREATE TABLE IF NOT EXISTS loot_tables (
    id VARCHAR(50) PRIMARY KEY, -- e.g. "mob_wild_dog_drops", "dummy_drops"
    description TEXT,
    entries JSONB NOT NULL DEFAULT '[]' -- e.g. [{"item_definition_id": "dragon_sword_0", "rate": 1000, "min": 1, "max": 1}]
);
