-- Core schema: migrations table is created by migrator; source registry + health + cache

CREATE TABLE IF NOT EXISTS source_registry (
    source_id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    required INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'UNRESOLVED',
    service TEXT,
    schema_name TEXT,
    table_name TEXT,
    collection TEXT,
    resolved_url TEXT,
    parser TEXT,
    realtime INTEGER NOT NULL DEFAULT 0,
    ttl_seconds INTEGER NOT NULL DEFAULT 60,
    maximum_stale_seconds INTEGER NOT NULL DEFAULT 300,
    authentication TEXT,
    attribution TEXT,
    license TEXT,
    metadata_json TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS source_health (
    source_id TEXT PRIMARY KEY,
    last_success_at TEXT,
    last_failure_at TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_status TEXT,
    last_latency_ms INTEGER,
    last_error_code TEXT,
    last_schema_hash TEXT
);

CREATE TABLE IF NOT EXISTS http_cache (
    cache_key TEXT PRIMARY KEY,
    status_code INTEGER NOT NULL,
    content_type TEXT,
    etag TEXT,
    last_modified TEXT,
    payload TEXT NOT NULL,
    retrieved_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    maximum_stale_at TEXT NOT NULL
);
