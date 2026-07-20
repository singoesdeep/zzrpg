CREATE TABLE IF NOT EXISTS item_definitions (
    id VARCHAR(50) PRIMARY KEY, -- e.g., "dragon_sword_0", "red_potion_1"
    name VARCHAR(100) NOT NULL,
    description TEXT,
    slot_type VARCHAR(20) NOT NULL, -- "WEAPON", "BODY_ARMOR", "HELMET", "SHIELD", "SHOES", "ACCESSORY", "NONE"
    min_level INTEGER DEFAULT 1,
    class_restrictions VARCHAR(20)[] DEFAULT '{}', -- e.g. '{"WARRIOR", "SURA"}'
    stats_modifiers JSONB NOT NULL DEFAULT '[]', 
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_item_definitions_slot_type ON item_definitions(slot_type);
CREATE INDEX IF NOT EXISTS idx_item_definitions_modifiers ON item_definitions USING gin (stats_modifiers);
