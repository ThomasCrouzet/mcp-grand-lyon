-- Store payload size once so each prune does not scan all cached response bodies.
ALTER TABLE http_cache ADD COLUMN payload_bytes INTEGER NOT NULL DEFAULT 0;
UPDATE http_cache SET payload_bytes = length(CAST(payload AS BLOB));
CREATE INDEX idx_http_cache_stale ON http_cache(maximum_stale_at);
CREATE INDEX idx_http_cache_retrieved ON http_cache(retrieved_at);
CREATE INDEX idx_source_health_latest ON source_health(
    MAX(COALESCE(last_success_at, ''), COALESCE(last_failure_at, ''))
);
