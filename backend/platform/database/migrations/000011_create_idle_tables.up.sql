-- Idle progression persistence: the character's active focus, lifeskill levels,
-- RTS building levels, and a fungible resource wallet.

-- Active focus: exactly one stage OR lifeskill per character.
CREATE TABLE IF NOT EXISTS character_idle_assignment (
    character_id  INTEGER PRIMARY KEY REFERENCES characters(id) ON DELETE CASCADE,
    activity_type VARCHAR(20) NOT NULL, -- 'stage' | 'lifeskill'
    activity_id   VARCHAR(50) NOT NULL, -- content id (goblin_forest, mining, ...)
    assigned_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Lifeskill levels: a progression axis independent of combat level.
CREATE TABLE IF NOT EXISTS character_lifeskills (
    character_id INTEGER NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    skill_id     VARCHAR(50) NOT NULL,
    level        INTEGER NOT NULL DEFAULT 1,
    xp           BIGINT  NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, skill_id)
);

-- RTS buildings: generator levels (0 = not built). Produce passively in parallel.
CREATE TABLE IF NOT EXISTS character_buildings (
    character_id INTEGER NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    generator_id VARCHAR(50) NOT NULL,
    level        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, generator_id)
);

-- Resource wallet: fungible resources (wood, stone, ...) — distinct from items.
CREATE TABLE IF NOT EXISTS character_resources (
    character_id INTEGER NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    resource_id  VARCHAR(50) NOT NULL,
    amount       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, resource_id)
);
