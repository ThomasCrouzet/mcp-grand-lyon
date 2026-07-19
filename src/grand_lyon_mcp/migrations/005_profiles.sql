CREATE TABLE IF NOT EXISTS user_places (
    place_key TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    address TEXT,
    latitude REAL,
    longitude REAL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS commute_profiles (
    profile_key TEXT PRIMARY KEY,
    config_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS briefing_profiles (
    profile_key TEXT PRIMARY KEY,
    config_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS briefing_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_key TEXT NOT NULL,
    captured_at TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_briefing_snapshots_profile ON briefing_snapshots(profile_key);
