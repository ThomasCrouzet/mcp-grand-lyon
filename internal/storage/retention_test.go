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

func TestHistoryRetentionAndScopedDeletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	for index := 0; index < 4; index++ {
		for _, profile := range []string{"keep", "other"} {
			_, err := store.DB.ExecContext(ctx, `INSERT INTO briefing_snapshots(profile_key,captured_at,payload_json) VALUES (?,?,?)`,
				profile, now.AddDate(0, 0, -index).Format(time.RFC3339Nano), `{"facts":{"value":1}}`)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for index := 0; index < 5; index++ {
		_, err := store.DB.ExecContext(ctx, `INSERT INTO velov_snapshots(station_id,observed_at,bikes_available,docks_available,hour_slot) VALUES (?,?,?,?,?)`,
			"station", now.AddDate(0, 0, -index).Format(time.RFC3339Nano), index, 5, "08")
		if err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.PruneHistory(ctx, HistoryRetentionPolicy{BriefingMaxAgeDays: 2, BriefingMaxCount: 1, VelovMaxAgeDays: 2, VelovMaxCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if removed["briefing_age"] == 0 || removed["briefing_count"] == 0 || removed["velov_age"] == 0 || removed["velov_count"] == 0 {
		t.Fatalf("retention result = %#v", removed)
	}
	info, err := store.HistoryInfo(ctx, HistoryRetentionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if info["briefing_snapshots"] != 2 || info["velov_snapshots"] != 1 {
		t.Fatalf("history info = %#v", info)
	}
	if deleted, err := store.DeleteBriefingHistory(ctx, "keep"); err != nil || deleted != 1 {
		t.Fatalf("DeleteBriefingHistory = %d, %v", deleted, err)
	}
	if deleted, err := store.DeleteVelovHistory(ctx, "station"); err != nil || deleted != 1 {
		t.Fatalf("DeleteVelovHistory = %d, %v", deleted, err)
	}
	info, err = store.HistoryInfo(ctx, HistoryRetentionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if info["briefing_snapshots"] != 1 || info["velov_snapshots"] != 0 {
		t.Fatalf("scoped deletion changed unrelated rows: %#v", info)
	}
}

func TestSaveSnapshotDeduplicatesEquivalentFacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	first := map[string]any{"facts": map[string]any{"value": 1, "generated_at": "2026-07-20T08:00:00Z"}}
	second := map[string]any{"facts": map[string]any{"value": 1, "generated_at": "2026-07-20T09:00:00Z"}}
	if err := store.SaveSnapshot(ctx, "profile", first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, "profile", second); err != nil {
		t.Fatal(err)
	}
	info, err := store.HistoryInfo(ctx, HistoryRetentionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if info["briefing_snapshots"] != 1 {
		t.Fatalf("equivalent facts created duplicate snapshots: %#v", info)
	}
	if err := store.SaveSnapshot(ctx, "profile", map[string]any{"facts": map[string]any{"value": 2}}); err != nil {
		t.Fatal(err)
	}
	info, err = store.HistoryInfo(ctx, HistoryRetentionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if info["briefing_snapshots"] != 2 {
		t.Fatalf("changed facts were not retained: %#v", info)
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
