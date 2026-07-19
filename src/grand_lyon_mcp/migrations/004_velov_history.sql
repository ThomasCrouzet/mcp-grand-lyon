CREATE TABLE IF NOT EXISTS velov_stations (
    station_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    capacity INTEGER DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS velov_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    bikes_available INTEGER NOT NULL,
    docks_available INTEGER NOT NULL,
    hour_slot TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_velov_snapshots_station ON velov_snapshots(station_id, hour_slot);
