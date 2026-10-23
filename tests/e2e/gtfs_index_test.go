package e2e_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestGTFSIndexProcess(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("gtfs-index")
	if err != nil {
		t.Fatal(err)
	}
	valid := filepath.Join(testHarness.fixtures, "gtfs", "mini_gtfs.zip")
	result, err := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", valid)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		t.Fatal(err)
	}
	if output["entities_indexed"] != float64(3) {
		t.Fatalf("entities_indexed = %#v, want 3", output["entities_indexed"])
	}

	database, err := sql.Open("sqlite", values[databaseEnv])
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	assertGTFSIndexName(t, database, "Bellecour")

	if _, err := database.Exec(`DELETE FROM gtfs_stops WHERE stop_id='BEL1'`); err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	facilities := client.callTool("lyon_nearby_facilities", map[string]any{
		"location":   map[string]any{"place_id": "gtfs:stop:BEL1"},
		"categories": []string{"toilet"}, "radius_m": 2000,
	})
	assertStatus(t, facilities, "partial")
	assertOfflineFixture(t, facilities)
	client.close(true)
	if _, err := database.Exec(`INSERT INTO gtfs_stops
		(stop_id,stop_code,stop_name,stop_lat,stop_lon,location_type,parent_station,wheelchair_boarding)
		VALUES ('BEL1','BEL','Bellecour',45.7578,4.8320,1,NULL,1)`); err != nil {
		t.Fatal(err)
	}

	invalid := filepath.Join(folder, "invalid-index.zip")
	transform := func(name string, data []byte) []byte {
		switch name {
		case "stops.txt":
			return []byte(strings.ReplaceAll(string(data), "Bellecour", "Broken Bellecour"))
		case "stop_times.txt":
			return []byte("trip_id,arrival_time,departure_time,stop_id,stop_sequence\nmorning,not-a-time,not-a-time,BEL1,1\n")
		default:
			return data
		}
	}
	if err := makeZip(invalid, filepath.Join(testHarness.fixtures, "gtfs", "dated"), transform); err != nil {
		t.Fatal(err)
	}
	failed, runErr := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", invalid)
	if runErr == nil || failed.ReturnCode == 0 {
		t.Fatal("invalid GTFS import succeeded")
	}
	assertGTFSIndexName(t, database, "Bellecour")
}

func assertGTFSIndexName(t *testing.T, database *sql.DB, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var entityName, stopName string
	if err := database.QueryRowContext(ctx, `SELECT name FROM entities WHERE logical_id='gtfs:stop:BEL1'`).Scan(&entityName); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT stop_name FROM gtfs_stops WHERE stop_id='BEL1'`).Scan(&stopName); err != nil {
		t.Fatal(err)
	}
	if entityName != want || stopName != want {
		t.Fatalf("GTFS index mismatch: entity=%q stop=%q want=%q", entityName, stopName, want)
	}
}
