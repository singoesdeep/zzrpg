CREATE TABLE IF NOT EXISTS quest_definitions (
    id VARCHAR(50) PRIMARY KEY, -- e.g., "kill_wolves_1"
    title VARCHAR(100) NOT NULL,
    description TEXT,
    min_level INTEGER DEFAULT 1,
    steps JSONB NOT NULL DEFAULT '[]', -- e.g., [{"type": "KILL_MOB", "target": "wolf", "count": 10}]
    rewards JSONB NOT NULL DEFAULT '{}', -- e.g., {"gold": 1000, "experience": 5000, "items": [{"id": "red_potion_1", "qty": 10}]}
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS character_quests (
    character_id INTEGER NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    quest_id VARCHAR(50) NOT NULL REFERENCES quest_definitions(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE', -- 'ACTIVE', 'COMPLETED'
    current_step_index INTEGER NOT NULL DEFAULT 0,
    progress JSONB NOT NULL DEFAULT '[]', -- tracking values: [3, 0]
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, quest_id)
);

CREATE INDEX IF NOT EXISTS idx_character_quests_char_id ON character_quests(character_id);
CREATE INDEX IF NOT EXISTS idx_character_quests_status ON character_quests(character_id, status);
