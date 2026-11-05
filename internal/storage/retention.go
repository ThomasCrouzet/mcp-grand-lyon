package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// HistoryRetentionPolicy bounds personal briefing and bicycle history.
// A zero age or count disables that dimension; defaults are applied by config.
type HistoryRetentionPolicy struct {
	BriefingMaxAgeDays int `json:"briefing_max_age_days"`
	BriefingMaxCount   int `json:"briefing_max_count"`
	VelovMaxAgeDays    int `json:"velov_max_age_days"`
	VelovMaxCount      int `json:"velov_max_count"`
}

func (policy HistoryRetentionPolicy) Validate() error {
	if policy.BriefingMaxAgeDays < 0 || policy.BriefingMaxCount < 0 || policy.VelovMaxAgeDays < 0 || policy.VelovMaxCount < 0 {
		return fmt.Errorf("history retention limits must not be negative")
	}
	return nil
}

// PruneHistory applies age and count limits inside one transaction. Deleting
// rows does not compact the SQLite file.
func (s *Store) PruneHistory(ctx context.Context, policy HistoryRetentionPolicy) (map[string]int64, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := map[string]int64{"briefing_age": 0, "briefing_count": 0, "velov_age": 0, "velov_count": 0}
	if policy.BriefingMaxAgeDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -policy.BriefingMaxAgeDays).Format(time.RFC3339Nano)
		removed, err := tx.ExecContext(ctx, `DELETE FROM briefing_snapshots WHERE captured_at<?`, cutoff)
		if err != nil {
			return nil, err
		}
		result["briefing_age"], _ = removed.RowsAffected()
	}
	if policy.BriefingMaxCount > 0 {
		removed, err := tx.ExecContext(ctx, `DELETE FROM briefing_snapshots WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY profile_key ORDER BY captured_at DESC,id DESC) AS position
				FROM briefing_snapshots
			) WHERE position>?
		)`, policy.BriefingMaxCount)
		if err != nil {
			return nil, err
		}
		result["briefing_count"], _ = removed.RowsAffected()
	}
	if policy.VelovMaxAgeDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -policy.VelovMaxAgeDays).Format(time.RFC3339Nano)
		removed, err := tx.ExecContext(ctx, `DELETE FROM velov_snapshots WHERE observed_at<?`, cutoff)
		if err != nil {
			return nil, err
		}
		result["velov_age"], _ = removed.RowsAffected()
	}
	if policy.VelovMaxCount > 0 {
		removed, err := tx.ExecContext(ctx, `DELETE FROM velov_snapshots WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (ORDER BY observed_at DESC,id DESC) AS position
				FROM velov_snapshots
			) WHERE position>?
		)`, policy.VelovMaxCount)
		if err != nil {
			return nil, err
		}
		result["velov_count"], _ = removed.RowsAffected()
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// HistoryInfo reports row counts and the effective retention policy.
func (s *Store) HistoryInfo(ctx context.Context, policy HistoryRetentionPolicy) (map[string]any, error) {
	briefingRows, err := countRows(ctx, s.DB, "briefing_snapshots")
	if err != nil {
		return nil, err
	}
	velovRows, err := countRows(ctx, s.DB, "velov_snapshots")
	if err != nil {
		return nil, err
	}
	var profileCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT profile_key) FROM briefing_snapshots`).Scan(&profileCount); err != nil {
		return nil, err
	}
	return map[string]any{
		"briefing_snapshots": briefingRows,
		"briefing_profiles":  profileCount,
		"velov_snapshots":    velovRows,
		"retention":          policy,
		"note":               "Deleting rows does not immediately shrink the SQLite file.",
	}, nil
}

func countRows(ctx context.Context, database *sql.DB, table string) (int, error) {
	var count int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteBriefingHistory removes snapshots for one profile and leaves other
// profiles, places, commutes, and timetable data intact.
func (s *Store) DeleteBriefingHistory(ctx context.Context, profile string) (int64, error) {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM briefing_snapshots WHERE profile_key=?`, profile)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteVelovHistory removes bicycle observations for one station or, with an
// empty station ID, for all stations. Station metadata and other tables remain.
func (s *Store) DeleteVelovHistory(ctx context.Context, stationID string) (int64, error) {
	var result sql.Result
	var err error
	if stationID == "" {
		result, err = s.DB.ExecContext(ctx, `DELETE FROM velov_snapshots`)
	} else {
		result, err = s.DB.ExecContext(ctx, `DELETE FROM velov_snapshots WHERE station_id=?`, stationID)
	}
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) PruneCache(ctx context.Context, maxEntries int, maxBytes int64) (int64, error) {
	if maxEntries < 0 || maxBytes < 0 {
		return 0, fmt.Errorf("cache retention limits must not be negative")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	expired, err := tx.ExecContext(ctx, "DELETE FROM http_cache WHERE maximum_stale_at<=?", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	evicted, err := tx.ExecContext(ctx, `DELETE FROM http_cache WHERE cache_key IN (
		SELECT cache_key FROM (
			SELECT cache_key,ROW_NUMBER() OVER (ORDER BY retrieved_at DESC,rowid DESC) AS position,
			SUM(payload_bytes) OVER (ORDER BY retrieved_at DESC,rowid DESC ROWS UNBOUNDED PRECEDING) AS retained_bytes
			FROM http_cache
		) WHERE position>? OR retained_bytes>?
	)`, maxEntries, maxBytes)
	if err != nil {
		return 0, err
	}
	expiredCount, _ := expired.RowsAffected()
	evictedCount, _ := evicted.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return expiredCount + evictedCount, nil
}

func (s *Store) PruneSourceHealth(ctx context.Context, maxEntries int, cutoff time.Time) (int64, error) {
	if maxEntries < 0 {
		return 0, fmt.Errorf("source health retention limit must not be negative")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	expired, err := tx.ExecContext(ctx, `DELETE FROM source_health
		WHERE MAX(COALESCE(last_success_at,''),COALESCE(last_failure_at,''))<=?`, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	evicted, err := tx.ExecContext(ctx, `DELETE FROM source_health WHERE source_id IN (
		SELECT source_id FROM source_health
		ORDER BY MAX(COALESCE(last_success_at,''),COALESCE(last_failure_at,'')) DESC,rowid DESC
		LIMIT -1 OFFSET ?
	)`, maxEntries)
	if err != nil {
		return 0, err
	}
	expiredCount, _ := expired.RowsAffected()
	evictedCount, _ := evicted.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return expiredCount + evictedCount, nil
}
