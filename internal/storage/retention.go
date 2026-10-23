package storage

import (
	"context"
	"fmt"
	"time"
)

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
