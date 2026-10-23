package e2e_test

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestStorageFailureResilience(t *testing.T) {
	runCase(t, "storage-retention-startup", testMissingRetentionTableStartup)
	runCase(t, "storage-missing-fts", testMissingFTSIndex)
	runCase(t, "storage-missing-gtfs", testMissingGTFSTables)
	runCase(t, "storage-missing-rtree", testMissingRTreeIndex)
}

func testMissingRetentionTableStartup(t *testing.T) {
	folder, env, values, err := preparedEnvironment("storage-retention-startup")
	if err != nil {
		t.Fatal(err)
	}
	database := openAuditDatabase(t, values[databaseEnv])
	execAuditSQL(t, database, `DROP TABLE http_cache`)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, runErr := testHarness.runCommand(folder, env, 15*time.Second,
		testHarness.binary, "serve", "--transport", "stdio", "--quiet")
	if runErr == nil || result.ReturnCode == 0 {
		t.Fatal("server started with a missing retention table")
	}
	if len(bytes.TrimSpace(result.Stdout)) != 0 {
		t.Fatalf("startup failure wrote to stdout: %q", result.Stdout)
	}
	detail := strings.ToLower(string(result.Stderr))
	if !strings.Contains(detail, "http_cache") || !strings.Contains(detail, "no such table") {
		t.Fatalf("startup error does not identify the missing retention table: %q", result.Stderr)
	}
}

func testMissingFTSIndex(t *testing.T) {
	folder, env, values, err := preparedEnvironment("storage-missing-fts")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	database := openAuditDatabase(t, values[databaseEnv])
	execAuditSQL(t, database, `DROP TABLE entity_fts`)

	result := client.callTool("lyon_resolve_place", map[string]any{
		"query": "Part-Dieu", "limit": 5,
	})
	assertStatus(t, result, "partial")
	assertWarningSource(t, result, "SOURCE_UNAVAILABLE", "entities")
	candidates := objectList(t, dataObject(t, result), "candidates")
	if !containsFieldValue(candidates, "id", "gtfs:stop:PD1") {
		t.Fatalf("GTFS candidate was lost after the FTS failure: %#v", candidates)
	}
	hasFixtureGeocoder := false
	for _, candidate := range candidates {
		id, _ := candidate["id"].(string)
		if strings.HasPrefix(id, "photon:") {
			hasFixtureGeocoder = true
			break
		}
	}
	if !hasFixtureGeocoder {
		t.Fatalf("fixture geocoder candidates were lost after the FTS failure: %#v", candidates)
	}
	assertProcessRemainsHealthy(t, client)
	client.close(true)
}

func testMissingGTFSTables(t *testing.T) {
	for _, table := range []string{"gtfs_stop_times", "gtfs_calendar"} {
		t.Run(table, func(t *testing.T) {
			folder, env, values, err := preparedEnvironment("storage-missing-" + table)
			if err != nil {
				t.Fatal(err)
			}
			client := startMCP(t, folder, env)
			initializeMCP(t, client)
			database := openAuditDatabase(t, values[databaseEnv])
			execAuditSQL(t, database, `DROP TABLE `+table)

			result := client.callTool("lyon_next_departures", map[string]any{
				"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A",
				"at": "2026-07-20T08:00:00+02:00", "limit": 3,
			})
			assertStatus(t, result, "unavailable")
			assertWarningSource(t, result, "SOURCE_UNAVAILABLE", "gtfs")
			if departures := objectList(t, dataObject(t, result), "departures"); len(departures) != 0 {
				t.Fatalf("missing %s returned departures: %#v", table, departures)
			}
			assertProcessRemainsHealthy(t, client)
			client.close(true)
		})
	}
}

func testMissingRTreeIndex(t *testing.T) {
	folder, env, values, err := preparedEnvironment("storage-missing-rtree")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	database := openAuditDatabase(t, values[databaseEnv])
	execAuditSQL(t, database, `DROP TABLE entity_rtree`)

	result := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"latitude": 45.7578, "longitude": 4.8320},
		"at":   "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	assertStatus(t, result, "unavailable")
	assertWarningSource(t, result, "SOURCE_UNAVAILABLE", "entities")
	if result["summary"] != "Nearby stops are unavailable." {
		t.Fatalf("RTree failure summary = %q", result["summary"])
	}
	if departures := objectList(t, dataObject(t, result), "departures"); len(departures) != 0 {
		t.Fatalf("missing RTree returned departures: %#v", departures)
	}
	assertProcessRemainsHealthy(t, client)
	client.close(true)
}
