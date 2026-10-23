package storage

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportGTFSIsAtomicAndQueryable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	source := filepath.Join("..", "assets", "fixtures", "gtfs", "mini_gtfs.zip")
	counts, err := store.ImportGTFS(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if counts["stops"] != 3 || counts["stop_times"] != 4 {
		t.Fatalf("unexpected import counts: %#v", counts)
	}

	stops, err := store.SearchStops(ctx, `Bellecour" OR x`, 5)
	if err != nil {
		t.Fatalf("search must treat FTS syntax as text: %v", err)
	}
	if len(stops) == 0 || stops[0].Name != "Bellecour" {
		t.Fatalf("unexpected stops: %#v", stops)
	}

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 20, 8, 0, 0, 0, paris)
	departures, err := store.ScheduledDepartures(ctx, stops[0].ID, at, "A", "Vaulx", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(departures) != 2 {
		t.Fatalf("departures = %#v", departures)
	}
	if departures[1].ExpectedAt == nil || departures[1].ExpectedAt.Day() != 21 || departures[1].ExpectedAt.Hour() != 1 {
		t.Fatalf("GTFS time after 24:00 parsed incorrectly: %#v", departures[1].ExpectedAt)
	}

	malformed := filepath.Join(t.TempDir(), "malformed.zip")
	copyGTFSWithInvalidLatitude(t, source, malformed)
	if _, err := store.ImportGTFS(ctx, malformed); err == nil {
		t.Fatal("malformed GTFS import succeeded")
	}
	stop, err := store.GetStop(ctx, "gtfs:stop:BEL1")
	if err != nil {
		t.Fatal(err)
	}
	if stop == nil || stop.Name != "Bellecour" {
		t.Fatalf("failed refresh replaced the installed feed: %#v", stop)
	}

	incomplete := filepath.Join(t.TempDir(), "incomplete.zip")
	writeIncompleteGTFS(t, incomplete)
	if _, err := store.ImportGTFS(ctx, incomplete); err == nil {
		t.Fatal("incomplete GTFS import succeeded")
	}
	stop, err = store.GetStop(ctx, "gtfs:stop:BEL1")
	if err != nil || stop == nil || stop.Name != "Bellecour" {
		t.Fatalf("incomplete refresh replaced the installed feed: %#v, %v", stop, err)
	}
}

func writeIncompleteGTFS(t *testing.T, destination string) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	member, err := archive.Create("agency.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("agency_id,agency_name\nA,Agency\n")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestScheduledDeparturesHonorCalendarException(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	source := filepath.Join("..", "assets", "fixtures", "gtfs", "mini_gtfs.zip")
	if _, err := store.ImportGTFS(ctx, source); err != nil {
		t.Fatal(err)
	}
	paris, _ := time.LoadLocation("Europe/Paris")
	// Start after the prior service day's valid 25:30 trip.
	at := time.Date(2026, 7, 14, 2, 0, 0, 0, paris)
	departures, err := store.ScheduledDepartures(ctx, "BEL1", at, "A", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(departures) != 0 {
		t.Fatalf("removed service returned departures: %#v", departures)
	}
}

func TestScheduledDeparturesUseCivilTimeAcrossDST(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	statements := []string{
		`INSERT INTO gtfs_routes(route_id,route_short_name) VALUES ('R','R')`,
		`INSERT INTO gtfs_stops(stop_id,stop_name,stop_lat,stop_lon) VALUES ('S','Stop',45.75,4.85)`,
		`INSERT INTO gtfs_trips(trip_id,route_id,service_id,trip_headsign) VALUES ('T','R','SUN','End')`,
		`INSERT INTO gtfs_stop_times(trip_id,arrival_time,departure_time,stop_id,stop_sequence) VALUES ('T','02:30:00','02:30:00','S',1)`,
		`INSERT INTO gtfs_calendar(service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date)
		 VALUES ('SUN',0,0,0,0,0,0,1,'20260329','20260329')`,
	}
	for _, statement := range statements {
		if _, err := store.DB.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	departures, err := store.ScheduledDepartures(ctx, "S", at, "R", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(departures) != 1 || departures[0].ScheduledAt == nil {
		t.Fatalf("departures = %#v", departures)
	}
	want := time.Date(2026, 3, 29, 1, 30, 0, 0, time.FixedZone("CET", 3600))
	if !departures[0].ScheduledAt.Equal(want) {
		t.Fatalf("scheduled time = %s, want instant %s", departures[0].ScheduledAt, want)
	}
}

func copyGTFSWithInvalidLatitude(t *testing.T, source, destination string) {
	t.Helper()
	input, err := zip.OpenReader(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	outputFile, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	output := zip.NewWriter(outputFile)
	for _, file := range input.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(file.Name, "stops.txt") {
			payload = []byte(strings.Replace(string(payload), "45.7578", "not-a-latitude", 1))
		}
		writer, err := output.CreateHeader(&file.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := outputFile.Close(); err != nil {
		t.Fatal(err)
	}
}
