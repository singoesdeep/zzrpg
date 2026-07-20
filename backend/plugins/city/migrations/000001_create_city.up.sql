-- City-builder game schema, owned entirely by the city plugin (module "city").
CREATE TABLE IF NOT EXISTS cities (
    owner     TEXT PRIMARY KEY,
    last_tick TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS city_buildings (
    owner       TEXT NOT NULL,
    building_id TEXT NOT NULL,
    level       INT  NOT NULL DEFAULT 0,
    PRIMARY KEY (owner, building_id)
);
CREATE TABLE IF NOT EXISTS city_resources (
    owner       TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    amount      BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (owner, resource_id)
);
