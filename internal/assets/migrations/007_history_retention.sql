-- Deduplicate historical samples before enforcing a stable observation identity.
DELETE FROM velov_snapshots WHERE id NOT IN (
    SELECT MIN(id) FROM velov_snapshots
    GROUP BY station_id, observed_at, bikes_available, docks_available
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_velov_snapshots_identity
    ON velov_snapshots(station_id, observed_at, bikes_available, docks_available);

CREATE INDEX IF NOT EXISTS idx_briefing_snapshots_captured
    ON briefing_snapshots(captured_at);
CREATE INDEX IF NOT EXISTS idx_velov_snapshots_observed
    ON velov_snapshots(observed_at);
