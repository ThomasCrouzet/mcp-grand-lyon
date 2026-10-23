CREATE TABLE IF NOT EXISTS entities (
    logical_id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    properties_json TEXT,
    source_updated_at TEXT,
    indexed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS entity_aliases (
    logical_id TEXT NOT NULL,
    alias TEXT NOT NULL,
    PRIMARY KEY (logical_id, alias)
);

CREATE VIRTUAL TABLE IF NOT EXISTS entity_fts USING fts5(
    logical_id UNINDEXED,
    name,
    aliases,
    normalized_name,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE VIRTUAL TABLE IF NOT EXISTS entity_rtree USING rtree(
    id,
    minx, maxx,
    miny, maxy
);

CREATE TABLE IF NOT EXISTS entity_rtree_map (
    rtree_id INTEGER PRIMARY KEY,
    logical_id TEXT NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(entity_type);
CREATE INDEX IF NOT EXISTS idx_entities_normalized ON entities(normalized_name);
