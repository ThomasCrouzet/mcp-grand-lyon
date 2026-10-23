package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRetentionKeepsNewestRowsWithinBothLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	rows := []struct {
		key       string
		retrieved string
		bytes     int
	}{{"old", "2026-10-23T08:00:00Z", 4}, {"new", "2026-10-23T10:00:00Z", 4}, {"middle", "2026-10-23T09:00:00Z", 4}}
	for _, row := range rows {
		_, err := store.DB.ExecContext(ctx, `INSERT INTO http_cache
			(cache_key,status_code,payload,retrieved_at,expires_at,maximum_stale_at,payload_bytes)
			VALUES (?,200,'data',?,'2099-01-01T00:00:00Z','2099-01-02T00:00:00Z',?)`, row.key, row.retrieved, row.bytes)
		if err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.PruneCache(ctx, 3, 8)
	if err != nil || removed != 1 {
		t.Fatalf("PruneCache = %d, %v", removed, err)
	}
	var keys string
	if err := store.DB.QueryRowContext(ctx, "SELECT group_concat(cache_key, ',') FROM http_cache ORDER BY retrieved_at DESC").Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != "new,middle" && keys != "middle,new" {
		t.Fatalf("retained cache keys = %q", keys)
	}
}

func TestSourceHealthRetentionUsesLatestEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.DB.ExecContext(ctx, `INSERT INTO source_health
		(source_id,last_success_at,last_failure_at,consecutive_failures,last_status)
		VALUES ('mixed','2026-10-23T10:00:00Z','2020-01-01T00:00:00Z',0,'ok'),
		       ('old','2020-01-01T00:00:00Z',NULL,0,'ok')`)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.PruneSourceHealth(ctx, 10, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	if err != nil || removed != 1 {
		t.Fatalf("PruneSourceHealth = %d, %v", removed, err)
	}
	var count int
	if err := store.DB.QueryRowContext(ctx, "SELECT count(*) FROM source_health WHERE source_id='mixed'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("newest source health event was removed")
	}
}
