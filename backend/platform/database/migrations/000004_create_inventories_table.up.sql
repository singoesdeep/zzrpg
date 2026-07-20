CREATE TABLE IF NOT EXISTS inventories (
    id BIGSERIAL PRIMARY KEY,
    character_id INTEGER NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    slot_index INTEGER NOT NULL, -- 0..99 for bag, 1000+ for equipped
    item_definition_id VARCHAR(50) NOT NULL REFERENCES item_definitions(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL DEFAULT 1,
    durability INTEGER NOT NULL DEFAULT 100,
    custom_modifiers JSONB NOT NULL DEFAULT '[]', -- Random bonus stats when dropped (7th/8th bonus)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (character_id, slot_index)
);

CREATE INDEX IF NOT EXISTS idx_inventories_character_id ON inventories(character_id);
CREATE INDEX IF NOT EXISTS idx_inventories_slot_index ON inventories(character_id, slot_index);
