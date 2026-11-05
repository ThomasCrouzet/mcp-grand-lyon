package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func TestGTFSCoverageUsesCalendarExceptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO gtfs_imports(source_url,imported_at,file_hash,status) VALUES ('test','2026-01-01T00:00:00Z','hash','ok')`); err != nil {
		t.Fatal(err)
	}
	today := model.Now()
	key := today.Format("20060102")
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO gtfs_calendar_dates(service_id,date,exception_type) VALUES ('ADDED',?,1),('REMOVED',?,2)`, key, key); err != nil {
		t.Fatal(err)
	}
	coverage, err := store.GTFSCoverage(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	if coverage["status"] != "current" || coverage["active_service_count"] != 1 || coverage["realtime"] != false {
		t.Fatalf("added exception coverage = %#v", coverage)
	}
	active, _ := coverage["active_services"].([]string)
	if len(active) != 1 || active[0] != "ADDED" {
		t.Fatalf("active services = %#v", active)
	}
	var removedRan bool
	calendars := map[string]gtfsCalendar{"REMOVED": {weekdays: [7]int{1, 1, 1, 1, 1, 1, 1}, startDate: "20200101", endDate: "20401231"}}
	exceptions := map[string]int{"REMOVED\x00" + key: 2}
	if serviceRuns("REMOVED", today, key, calendars, exceptions) {
		removedRan = true
	}
	if removedRan {
		t.Fatal("removed exception still runs")
	}
}

func TestGTFSCoverageReportsMissingAndDeletedData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	missing, err := store.GTFSCoverage(ctx, time.Now())
	if err != nil || missing["status"] != "missing" || missing["imported"] != false {
		t.Fatalf("missing coverage = %#v, %v", missing, err)
	}
	if _, err := store.DB.ExecContext(ctx, `DROP TABLE gtfs_imports`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GTFSCoverage(ctx, time.Now()); err == nil {
		t.Fatal("missing table was not reported")
	}
}
